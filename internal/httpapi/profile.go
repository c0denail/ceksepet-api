package httpapi

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

func (s *Server) profileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/sellers/{sellerId}", s.sellerProfile)
	mux.HandleFunc("GET /api/v1/sellers/{sellerId}/raffles", func(w http.ResponseWriter, r *http.Request) { s.raffleList(w, r, "seller") })
	mux.HandleFunc("GET /api/v1/seller-report-reasons", s.sellerReportReasons)
	mux.HandleFunc("POST /api/v1/sellers/{sellerId}/reports", s.reportSeller)
	mux.HandleFunc("GET /api/v1/operator/seller-reports", s.operatorSellerReports)
	mux.HandleFunc("POST /api/v1/operator/seller-reports/{reportId}/resolution", s.resolveSellerReport)
	mux.HandleFunc("POST /api/v1/operator/raffles/{raffleId}/cancel", s.cancelRaffle)
	mux.HandleFunc("GET /api/v1/faqs", s.faqs)
}

func (s *Server) sellerProfile(w http.ResponseWriter, r *http.Request) {
	viewer, ok := s.marketUser(w, r, false)
	if !ok {
		return
	}
	id := r.PathValue("sellerId")
	user, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		s.marketError(w, err)
		return
	}
	profile := marketProfile{ID: user.ID, Username: user.Username, DisplayName: user.FirstName + " " + user.LastName, AvatarURL: s.avatarURL(user.AvatarID)}
	_, count, average, err := s.store.ListReviews(r.Context(), id, 1, 0)
	if err != nil {
		s.marketError(w, err)
		return
	}
	_, raffles, err := s.store.ListRaffles(r.Context(), store.RaffleFilter{SellerID: id, Limit: 1})
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"seller": profile, "rating": map[string]any{"average": average, "count": count}, "raffleCount": raffles, "canReport": viewer != "" && viewer != id})
}

func (s *Server) sellerReportReasons(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"items": domain.SellerReportReasons, "maxDescriptionLength": 2000})
}

func (s *Server) reportSeller(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	seller := r.PathValue("sellerId")
	if seller == user {
		s.marketError(w, store.ErrForbidden)
		return
	}
	var req struct {
		ReasonID    string `json:"reasonId"`
		Description string `json:"description"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	valid := false
	for _, reason := range domain.SellerReportReasons {
		if reason.ID == req.ReasonID {
			valid = true
			break
		}
	}
	if !valid {
		writeError(w, 400, *validationError("reasonId", "Listeden bir şikâyet nedeni seçin."))
		return
	}
	req.Description = strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(req.Description) > 2000 {
		writeError(w, 400, *validationError("description", "Açıklama en fazla 2000 karakter olabilir."))
		return
	}
	report := domain.SellerReport{ID: security.NewID(), ReporterID: user, SellerID: seller, ReasonID: req.ReasonID, Description: req.Description, Status: "open", CreatedAt: s.now().UTC()}
	if err := s.store.CreateSellerReport(r.Context(), report); err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 201, report)
}

func (s *Server) operatorSellerReports(w http.ResponseWriter, r *http.Request) {
	if _, _, e := s.authenticateOperator(r); e != nil {
		writeError(w, 401, *e)
		return
	}
	limit, offset, ok := marketPagination(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if !contains([]string{"open", "resolved", "dismissed", "all"}, status) {
		writeError(w, 400, *validationError("status", "Şikâyet durumu geçersiz."))
		return
	}
	items, total, err := s.store.ListSellerReports(r.Context(), status, limit, offset)
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, page(items, total, limit, offset))
}

func (s *Server) resolveSellerReport(w http.ResponseWriter, r *http.Request) {
	_, actor, e := s.authenticateOperator(r)
	if e != nil {
		writeError(w, 401, *e)
		return
	}
	var req struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	if req.Status != "resolved" && req.Status != "dismissed" {
		writeError(w, 400, *validationError("status", "resolved veya dismissed olmalıdır."))
		return
	}
	req.Resolution = strings.TrimSpace(req.Resolution)
	if n := utf8.RuneCountInString(req.Resolution); n < 3 || n > 1000 {
		writeError(w, 400, *validationError("resolution", "Sonuç açıklaması 3–1000 karakter olmalıdır."))
		return
	}
	report, err := s.store.ResolveSellerReport(r.Context(), r.PathValue("reportId"), req.Status, req.Resolution, actor, s.now().UTC())
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, report)
}

func (s *Server) cancelRaffle(w http.ResponseWriter, r *http.Request) {
	_, actor, e := s.authenticateOperator(r)
	if e != nil {
		writeError(w, 401, *e)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if n := utf8.RuneCountInString(req.Reason); n < 3 || n > 1000 {
		writeError(w, 400, *validationError("reason", "İptal gerekçesi 3–1000 karakter olmalıdır."))
		return
	}
	raffle, err := s.store.CancelRaffle(r.Context(), r.PathValue("raffleId"), req.Reason, actor, s.now().UTC())
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, raffle)
}

type faqItem struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func (s *Server) faqs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"title": "Torba", "aboutTitle": "Torba nedir?", "about": "Torba, ikinci el ürünlerini çekiliş yoluyla sunabileceğin ve bilet kullanarak çekilişlere katılabileceğin bir platformdur.", "items": []faqItem{
		{ID: "participation", Question: "Nasıl çekilişe katılırım?", Answer: "Katılmak istediğin ürünün sayfasını aç, kullanmak istediğin bilet sayısını seç ve katılımını onayla. Onaylanan biletler cüzdanından düşer."},
		{ID: "ticket-limit", Question: "Çekilişler için bir bilet sınırı var mı?", Answer: "En az 1 biletle katılabilirsin. Katılımın cüzdanındaki bakiye ve çekilişte kalan bilet sayısıyla sınırlıdır."},
		{ID: "delivery", Question: "Ürünüm ne zaman elime geçer?", Answer: "Teslimat bilgisi ürün ve satıcıya göre değişir. Ürün sayfasındaki kargo bilgisini kontrol edebilir, soru-cevap bölümünden satıcıya teslimat süresini sorabilirsin."},
		{ID: "organizing", Question: "Nasıl çekiliş düzenlerim?", Answer: "Artı düğmesinden ürününü ekle. Fotoğrafları ve ürün bilgilerini tamamla, bilet hesabını kontrol ederek onaya gönder. Onaylanan ürünün çekiliş listesinde görünür."},
	}})
}
