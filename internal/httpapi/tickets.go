package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyPattern.MatchString(key) {
		writeError(w, 400, *validationError("Idempotency-Key", "8–128 karakterlik harf, rakam, tire veya alt çizgi içeren işlem anahtarı gereklidir."))
		return "", false
	}
	return key, true
}

func (s *Server) wallet(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	profile, err := s.store.GetUserByID(r.Context(), user)
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"balance": profile.TicketBalance, "ticketValueKurus": s.cfg.TicketValueKurus, "currency": "TRY"})
}

func (s *Server) ticketHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	limit, offset, ok := marketPagination(w, r)
	if !ok {
		return
	}
	items, total, err := s.store.ListTicketEntries(r.Context(), user, limit, offset)
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, page(items, total, limit, offset))
}

func ticketCount(w http.ResponseWriter, r *http.Request, confirm bool) (int64, bool) {
	var req struct {
		Tickets   int64 `json:"tickets"`
		Confirmed bool  `json:"confirmed"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return 0, false
	}
	if req.Tickets < 1 || req.Tickets > 200_000_000 {
		writeError(w, 400, *validationError("tickets", "Bilet sayısı 1–200000000 aralığında tam sayı olmalıdır."))
		return 0, false
	}
	if confirm && !req.Confirmed {
		writeError(w, 400, *validationError("confirmed", "Çekilişe katılmak için açık onay gereklidir."))
		return 0, false
	}
	return req.Tickets, true
}

func (s *Server) participationPreview(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	tickets, ok := ticketCount(w, r, false)
	if !ok {
		return
	}
	raffle, ok := s.visibleRaffle(w, r, user)
	if !ok {
		return
	}
	if raffle.SellerID == user {
		s.marketError(w, store.ErrForbidden)
		return
	}
	if raffle.Status != domain.RaffleActive || tickets > raffle.Quote.TotalTickets-raffle.SoldTickets {
		s.marketError(w, store.ErrRaffleUnavailable)
		return
	}
	profile, err := s.store.GetUserByID(r.Context(), user)
	if err != nil {
		s.marketError(w, err)
		return
	}
	if profile.TicketBalance < tickets {
		s.marketError(w, store.ErrInsufficientTickets)
		return
	}
	writeJSON(w, 200, map[string]any{"raffleId": raffle.ID, "tickets": tickets, "balanceBefore": profile.TicketBalance, "balanceAfter": profile.TicketBalance - tickets,
		"remainingTickets": raffle.Quote.TotalTickets - raffle.SoldTickets, "requiresConfirmation": true, "reserved": false,
		"message": fmt.Sprintf("%d bilet kullanarak çekilişe katılacaksın. Onaylıyor musun?", tickets)})
}

func (s *Server) participate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	tickets, ok := ticketCount(w, r, true)
	if !ok {
		return
	}
	entry, replayed, err := s.store.ApplyTickets(r.Context(), domain.TicketEntry{ID: security.NewID(), UserID: user, Key: key, Kind: "participation", RaffleID: r.PathValue("raffleId"), Delta: -tickets, CreatedAt: s.now().UTC()})
	if err != nil {
		s.marketError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"transaction": entry, "replayed": replayed})
}

func (s *Server) creditTickets(w http.ResponseWriter, r *http.Request) {
	_, operator, e := s.authenticateOperator(r)
	if e != nil {
		writeError(w, 401, *e)
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Tickets int64  `json:"tickets"`
		Reason  string `json:"reason"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Tickets < 1 || req.Tickets > 1_000_000 {
		writeError(w, 400, *validationError("tickets", "1–1000000 bilet olmalıdır."))
		return
	}
	if n := utf8.RuneCountInString(req.Reason); n < 3 || n > 500 {
		writeError(w, 400, *validationError("reason", "3–500 karakterlik işlem gerekçesi zorunludur."))
		return
	}
	entry, replayed, err := s.store.ApplyTickets(r.Context(), domain.TicketEntry{ID: security.NewID(), UserID: r.PathValue("userId"), Key: key, Kind: "credit", Delta: req.Tickets, Reason: req.Reason, Actor: operator, CreatedAt: s.now().UTC()})
	if err != nil {
		s.marketError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"transaction": entry, "replayed": replayed})
}
