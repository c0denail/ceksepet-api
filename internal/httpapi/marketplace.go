package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

func (s *Server) marketplaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/catalog", s.catalog)
	mux.HandleFunc("GET /api/v1/content/{slug}", s.content)
	mux.HandleFunc("POST /api/v1/raffles/quote", s.priceQuote)
	mux.HandleFunc("POST /api/v1/photos", s.uploadPhoto)
	mux.HandleFunc("GET /api/v1/photos/{photoId}", s.servePhoto)
	mux.HandleFunc("GET /api/v1/raffles", s.listRaffles)
	mux.HandleFunc("POST /api/v1/raffles", s.createRaffle)
	mux.HandleFunc("GET /api/v1/raffles/{raffleId}", s.raffleDetail)
	mux.HandleFunc("GET /api/v1/raffles/{raffleId}/related", s.relatedRaffles)
	mux.HandleFunc("GET /api/v1/users/me/raffles", s.myRaffles)
	mux.HandleFunc("GET /api/v1/users/me/participations", s.joinedRaffles)
	mux.HandleFunc("GET /api/v1/users/me/favorites", s.favoriteRaffles)
	mux.HandleFunc("PUT /api/v1/raffles/{raffleId}/favorite", s.favorite)
	mux.HandleFunc("DELETE /api/v1/raffles/{raffleId}/favorite", s.favorite)
	mux.HandleFunc("GET /api/v1/users/me/wallet", s.wallet)
	mux.HandleFunc("GET /api/v1/users/me/wallet/transactions", s.ticketHistory)
	mux.HandleFunc("POST /api/v1/raffles/{raffleId}/participation-preview", s.participationPreview)
	mux.HandleFunc("POST /api/v1/raffles/{raffleId}/participations", s.participate)
	mux.HandleFunc("GET /api/v1/raffles/{raffleId}/questions", s.listQuestions)
	mux.HandleFunc("POST /api/v1/raffles/{raffleId}/questions", s.askQuestion)
	mux.HandleFunc("GET /api/v1/raffles/{raffleId}/questions/{questionId}/replies", s.listReplies)
	mux.HandleFunc("POST /api/v1/raffles/{raffleId}/questions/{questionId}/replies", s.replyQuestion)
	mux.HandleFunc("GET /api/v1/sellers/{sellerId}/reviews", s.sellerReviews)
	mux.HandleFunc("POST /api/v1/raffles/{raffleId}/reviews", s.createReview)
	mux.HandleFunc("GET /api/v1/users/me/notifications", s.notifications)
	mux.HandleFunc("PUT /api/v1/users/me/notifications/{notificationId}/read", s.readNotification)
	mux.HandleFunc("GET /api/v1/operator/raffles", s.operatorRaffles)
	mux.HandleFunc("POST /api/v1/operator/raffles/{raffleId}/moderation", s.moderateRaffle)
	mux.HandleFunc("POST /api/v1/operator/users/{userId}/tickets", s.creditTickets)
}

func (s *Server) marketUser(w http.ResponseWriter, r *http.Request, required bool) (string, bool) {
	if !required && r.Header.Get("Authorization") == "" {
		return "", true
	}
	_, id, e := s.authenticate(r)
	if e != nil {
		writeError(w, http.StatusUnauthorized, *e)
		return "", false
	}
	return id, true
}

func (s *Server) marketError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "Beklenmeyen bir hata oluştu."
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Kayıt bulunamadı."
	case errors.Is(err, store.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Bu işlem için yetkiniz yok."
	case errors.Is(err, store.ErrInsufficientTickets):
		status, code, message = 409, "INSUFFICIENT_TICKETS", "Bilet bakiyeniz yetersiz."
	case errors.Is(err, store.ErrRaffleUnavailable):
		status, code, message = 409, "RAFFLE_UNAVAILABLE", "Çekiliş bu işlem için uygun değil veya yeterli bilet kalmadı."
	case errors.Is(err, store.ErrIdempotencyConflict):
		status, code, message = 409, "IDEMPOTENCY_CONFLICT", "Bu işlem anahtarı farklı bir istek için kullanılmış."
	case errors.Is(err, store.ErrAccountInUse):
		status, code, message = 409, "ACCOUNT_HAS_MARKETPLACE_RECORDS", "Hesabın çekiliş veya bilet kayıtları var. Kapatma işlemi için destek gerekli."
	case errors.Is(err, store.ErrConflict):
		status, code, message = 409, "CONFLICT", "Kayıt zaten var veya işlem mevcut durumla çelişiyor."
	default:
		s.logger.Error("marketplace operation", "error", err)
	}
	writeError(w, status, apiError{Code: code, Message: message})
}

func marketPagination(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	limit, e := paginationValue(r, "limit", 20, 1, 100)
	if e != nil {
		writeError(w, 400, *e)
		return 0, 0, false
	}
	offset, e := paginationValue(r, "offset", 0, 0, 1_000_000)
	if e != nil {
		writeError(w, 400, *e)
		return 0, 0, false
	}
	return limit, offset, true
}

func page(items any, total, limit, offset int64) map[string]any {
	return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset, "hasMore": offset+limit < total}
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	brands := domain.Brands
	if id := r.URL.Query().Get("categoryId"); id != "" {
		if _, ok := domain.CategoryByID(id); !ok {
			writeError(w, 400, *validationError("categoryId", "Kategori geçersiz."))
			return
		}
		brands = []domain.Brand{}
		for _, b := range domain.Brands {
			for _, child := range domain.CategoryIDs(id) {
				if domain.BrandFits(b.ID, child) {
					brands = append(brands, b)
					break
				}
			}
		}
	}
	writeJSON(w, 200, map[string]any{"categories": domain.Categories, "brands": brands,
		"filters":        []map[string]string{{"id": "popular", "label": "Popüler"}, {"id": "newest", "label": "En Yeniler"}},
		"shippingPayers": []map[string]string{{"id": "buyer", "label": "Alıcı öder"}, {"id": "seller", "label": "Satıcı öder"}},
		"pricing":        map[string]any{"currency": "TRY", "ticketValueKurus": s.cfg.TicketValueKurus, "commissionBps": s.cfg.CommissionBPS},
		"photoRules":     map[string]any{"minCount": 1, "maxCount": 5, "maxBytes": 5 << 20, "contentTypes": []string{"image/jpeg", "image/png"}},
		"help":           map[string]string{"raffle": "/api/v1/content/bag-raffle", "safety": "/api/v1/content/raffle-safety", "selling": "/api/v1/content/selling"}})
}

func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	items := map[string]map[string]string{
		"bag-raffle":    {"title": "Torba çekilişi nedir?", "body": "Bir ürünün toplam bilet sayısı vardır. İstediğin sayıda biletle katılabilirsin. Katılım onayı verdiğinde biletler bakiyenden düşer. Toplam bilet sayısına ulaşıldığında yeni katılım kapanır."},
		"raffle-safety": {"title": "Çekilişler güvenli mi?", "body": "Ürünler yayına alınmadan önce incelenir. Katılım için onayın gerekir; harcadığın biletleri işlem geçmişinde görebilirsin. Satıcı bilgilerini, değerlendirmelerini ve soru-cevap bölümünü inceleyebilirsin."},
		"selling":       {"title": "Ürün ekleme", "body": "1–5 fotoğraf ekle; başlık, açıklama, kategori, marka ve kargoyu ödeyecek tarafı seç. Fiyatına göre bilet ve komisyon hesabını kontrol ederek ürünü onaya gönder. Hesaplanan kazanç tahminidir; bu aşamada cüzdanına ödeme yapılmaz."},
	}
	value, ok := items[r.PathValue("slug")]
	if !ok {
		s.marketError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) priceQuote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PriceKurus int64 `json:"priceKurus"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	quote, err := domain.CalculateQuote(req.PriceKurus, s.cfg.TicketValueKurus, s.cfg.CommissionBPS)
	if err != nil {
		writeError(w, 400, *validationError("priceKurus", "Fiyat 1–100000000 kuruş aralığında bir tam sayı olmalıdır."))
		return
	}
	writeJSON(w, 200, quote)
}

type createRaffleRequest struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	CategoryID    string   `json:"categoryId"`
	BrandID       string   `json:"brandId"`
	ShippingPayer string   `json:"shippingPayer"`
	PhotoIDs      []string `json:"photoIds"`
	PriceKurus    int64    `json:"priceKurus"`
}

func (s *Server) createRaffle(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	var req createRaffleRequest
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	fields := map[string]string{}
	if n := utf8.RuneCountInString(req.Title); n < 3 || n > 120 {
		fields["title"] = "Başlık 3–120 karakter olmalıdır."
	}
	if n := utf8.RuneCountInString(req.Description); n < 10 || n > 5000 {
		fields["description"] = "Açıklama 10–5000 karakter olmalıdır."
	}
	if _, ok := domain.CategoryByID(req.CategoryID); !ok {
		fields["categoryId"] = "Geçerli bir kategori seçin."
	}
	if !domain.BrandFits(req.BrandID, req.CategoryID) {
		fields["brandId"] = "Bu kategoriye uygun bir marka seçin."
	}
	if req.ShippingPayer != "buyer" && req.ShippingPayer != "seller" {
		fields["shippingPayer"] = "buyer veya seller olmalıdır."
	}
	if len(req.PhotoIDs) < 1 || len(req.PhotoIDs) > 5 {
		fields["photoIds"] = "1–5 fotoğraf gereklidir."
	}
	seen := map[string]bool{}
	for _, id := range req.PhotoIDs {
		if id == "" || seen[id] {
			fields["photoIds"] = "Fotoğraf kimlikleri boş veya tekrarlı olamaz."
		}
		seen[id] = true
	}
	quote, err := domain.CalculateQuote(req.PriceKurus, s.cfg.TicketValueKurus, s.cfg.CommissionBPS)
	if err != nil {
		fields["priceKurus"] = "Fiyat 1–100000000 kuruş olmalıdır."
	}
	if e := collectValidation(fields); e != nil {
		writeError(w, 400, *e)
		return
	}
	raffle := domain.Raffle{ID: security.NewID(), SellerID: user, Title: req.Title, Description: req.Description,
		CategoryID: req.CategoryID, BrandID: req.BrandID, ShippingPayer: req.ShippingPayer, PhotoIDs: req.PhotoIDs,
		Quote: quote, Status: domain.RafflePending, CreatedAt: s.now().UTC()}
	if err := s.store.CreateRaffle(r.Context(), raffle); err != nil {
		s.marketError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/raffles/"+raffle.ID)
	writeJSON(w, 201, raffle)
}

type raffleCard struct {
	domain.Raffle
	BrandName         string   `json:"brandName"`
	CategoryName      string   `json:"categoryName"`
	PhotoURLs         []string `json:"photoUrls"`
	IsFavorite        bool     `json:"isFavorite"`
	IsNew             bool     `json:"isNew"`
	Badge             string   `json:"badge,omitempty"`
	ProgressPercent   float64  `json:"progressPercent"`
	RemainingTickets  int64    `json:"remainingTickets"`
	MyTickets         int64    `json:"myTickets"`
	MyRefundedTickets int64    `json:"myRefundedTickets"`
}

func (s *Server) card(r *http.Request, raffle domain.Raffle, user string) (raffleCard, error) {
	brand, _ := domain.BrandByID(raffle.BrandID)
	category, _ := domain.CategoryByID(raffle.CategoryID)
	result := raffleCard{Raffle: raffle, BrandName: brand.Name, CategoryName: category.Name, PhotoURLs: []string{}, RemainingTickets: max(0, raffle.Quote.TotalTickets-raffle.SoldTickets)}
	for _, id := range raffle.PhotoIDs {
		result.PhotoURLs = append(result.PhotoURLs, s.cfg.PublicBaseURL+"/api/v1/photos/"+id)
	}
	var err error
	result.IsFavorite, err = s.store.IsFavorite(r.Context(), user, raffle.ID)
	if err != nil {
		return result, err
	}
	if user != "" {
		p, e := s.store.GetParticipation(r.Context(), user, raffle.ID)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return result, e
		}
		result.MyTickets = p.Tickets
		result.MyRefundedTickets = p.RefundedTickets
	}
	if raffle.Quote.TotalTickets > 0 {
		result.ProgressPercent = float64(raffle.SoldTickets) * 100 / float64(raffle.Quote.TotalTickets)
	}
	result.IsNew = raffle.PublishedAt != nil && s.now().Sub(*raffle.PublishedAt).Hours() < 7*24
	if raffle.Status == domain.RaffleCancelled {
		result.Badge = "İptal edildi"
		result.RemainingTickets = 0
	} else if raffle.Status == domain.RaffleCompleted {
		result.Badge = "Sonlandı"
	} else if result.ProgressPercent >= 80 {
		result.Badge = "Bitmek üzere"
	} else if result.IsNew {
		result.Badge = "Yeni"
	}
	return result, nil
}

func (s *Server) visibleRaffle(w http.ResponseWriter, r *http.Request, user string) (domain.Raffle, bool) {
	raffle, err := s.store.GetRaffle(r.Context(), r.PathValue("raffleId"))
	if err != nil {
		s.marketError(w, err)
		return raffle, false
	}
	if !raffle.Public() && raffle.SellerID != user {
		s.marketError(w, store.ErrNotFound)
		return raffle, false
	}
	return raffle, true
}

func (s *Server) raffleDetail(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, false)
	if !ok {
		return
	}
	raffle, ok := s.visibleRaffle(w, r, user)
	if !ok {
		return
	}
	card, err := s.card(r, raffle, user)
	if err != nil {
		s.marketError(w, err)
		return
	}
	seller, err := s.marketProfile(r, raffle.SellerID)
	if err != nil {
		s.marketError(w, err)
		return
	}
	_, count, average, err := s.store.ListReviews(r.Context(), raffle.SellerID, 1, 0)
	if err != nil {
		s.marketError(w, err)
		return
	}
	questions, total, err := s.questionViews(r, raffle, "", 2, 0)
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"raffle": card, "seller": seller, "sellerRating": map[string]any{"average": average, "count": count},
		"questions": page(questions, total, 2, 0), "canParticipate": user != "" && user != raffle.SellerID && raffle.Status == domain.RaffleActive,
		"participationRules": map[string]any{"minTickets": 1, "maxTickets": card.RemainingTickets, "requiresConfirmation": true}})
}

func (s *Server) listRaffles(w http.ResponseWriter, r *http.Request)   { s.raffleList(w, r, "public") }
func (s *Server) myRaffles(w http.ResponseWriter, r *http.Request)     { s.raffleList(w, r, "mine") }
func (s *Server) joinedRaffles(w http.ResponseWriter, r *http.Request) { s.raffleList(w, r, "joined") }
func (s *Server) favoriteRaffles(w http.ResponseWriter, r *http.Request) {
	s.raffleList(w, r, "favorites")
}
func (s *Server) operatorRaffles(w http.ResponseWriter, r *http.Request) {
	s.raffleList(w, r, "operator")
}
func (s *Server) relatedRaffles(w http.ResponseWriter, r *http.Request) {
	s.raffleList(w, r, "related")
}

func (s *Server) raffleList(w http.ResponseWriter, r *http.Request, mode string) {
	user := ""
	if mode == "operator" {
		if _, _, e := s.authenticateOperator(r); e != nil {
			writeError(w, 401, *e)
			return
		}
	} else {
		var ok bool
		user, ok = s.marketUser(w, r, mode != "public" && mode != "related" && mode != "seller")
		if !ok {
			return
		}
	}
	limit, offset, ok := marketPagination(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := store.RaffleFilter{Search: strings.TrimSpace(q.Get("q")), CategoryID: q.Get("categoryId"), BrandID: q.Get("brandId"), Sort: q.Get("sort"), Status: q.Get("status"), Limit: limit, Offset: offset}
	if utf8.RuneCountInString(f.Search) > 100 {
		writeError(w, 400, *validationError("q", "Arama en fazla 100 karakter olmalıdır."))
		return
	}
	if f.Sort == "" {
		f.Sort = "newest"
	}
	if f.Sort != "newest" && f.Sort != "popular" {
		writeError(w, 400, *validationError("sort", "newest veya popular olmalıdır."))
		return
	}
	if f.CategoryID != "" {
		if _, ok := domain.CategoryByID(f.CategoryID); !ok {
			writeError(w, 400, *validationError("categoryId", "Kategori geçersiz."))
			return
		}
	}
	if f.BrandID != "" {
		if _, ok := domain.BrandByID(f.BrandID); !ok {
			writeError(w, 400, *validationError("brandId", "Marka geçersiz."))
			return
		}
	}
	switch mode {
	case "public":
		if f.Status == "" {
			f.Status = domain.RaffleActive
		}
	case "mine":
		f.SellerID = user
		if f.Status == "" {
			f.Status = "all"
		}
	case "joined":
		f.JoinedBy = user
		if f.Status == "all" {
			f.Status = ""
		}
	case "seller":
		f.SellerID = r.PathValue("sellerId")
		if _, err := s.store.GetUserByID(r.Context(), f.SellerID); err != nil {
			s.marketError(w, err)
			return
		}
		if f.Status == "all" {
			f.Status = ""
		}
	case "favorites":
		f.FavoritedBy = user
	case "operator":
		if f.Status == "" {
			f.Status = domain.RafflePending
		}
	case "related":
		raffle, ok := s.visibleRaffle(w, r, user)
		if !ok {
			return
		}
		f.CategoryID = raffle.CategoryID
		f.ExcludeID = raffle.ID
		f.Status = domain.RaffleActive
	}
	allowed := []string{"", domain.RaffleActive, domain.RaffleCompleted}
	if mode == "mine" || mode == "operator" {
		allowed = append(allowed, "all", domain.RafflePending, domain.RaffleRejected, domain.RaffleCancelled)
	}
	if mode == "joined" {
		allowed = append(allowed, domain.RaffleCancelled)
	}
	if !contains(allowed, f.Status) {
		writeError(w, 400, *validationError("status", "Durum bu liste için geçersiz."))
		return
	}
	raffles, total, err := s.store.ListRaffles(r.Context(), f)
	if err != nil {
		s.marketError(w, err)
		return
	}
	items := make([]raffleCard, 0, len(raffles))
	for _, raffle := range raffles {
		card, err := s.card(r, raffle, user)
		if err != nil {
			s.marketError(w, err)
			return
		}
		items = append(items, card)
	}
	writeJSON(w, 200, page(items, total, limit, offset))
}

func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	raffle, ok := s.visibleRaffle(w, r, user)
	if !ok {
		return
	}
	if r.Method == http.MethodPut && raffle.Status != domain.RaffleActive && raffle.Status != domain.RaffleCompleted {
		s.marketError(w, store.ErrRaffleUnavailable)
		return
	}
	enabled := r.Method == http.MethodPut
	if err := s.store.SetFavorite(r.Context(), user, raffle.ID, enabled); err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"raffleId": raffle.ID, "isFavorite": enabled})
}

func (s *Server) moderateRaffle(w http.ResponseWriter, r *http.Request) {
	if _, _, e := s.authenticateOperator(r); e != nil {
		writeError(w, 401, *e)
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Decision != "approve" && req.Decision != "reject" {
		writeError(w, 400, *validationError("decision", "approve veya reject olmalıdır."))
		return
	}
	if utf8.RuneCountInString(req.Reason) > 1000 || (req.Decision == "reject" && req.Reason == "") {
		writeError(w, 400, *validationError("reason", "Ret için en fazla 1000 karakterlik gerekçe gereklidir."))
		return
	}
	status := domain.RaffleActive
	if req.Decision == "reject" {
		status = domain.RaffleRejected
	} else {
		req.Reason = ""
	}
	raffle, err := s.store.ModerateRaffle(r.Context(), r.PathValue("raffleId"), status, req.Reason, s.now().UTC())
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, raffle)
}
