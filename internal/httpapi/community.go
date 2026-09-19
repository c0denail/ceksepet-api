package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

type marketProfile struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

func (s *Server) marketProfile(r *http.Request, id string) (marketProfile, error) {
	u, err := s.store.GetUserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return marketProfile{ID: id, DisplayName: "Silinmiş kullanıcı"}, nil
	}
	if err != nil {
		return marketProfile{}, err
	}
	return marketProfile{ID: u.ID, Username: u.Username, DisplayName: u.FirstName + " " + u.LastName, AvatarURL: s.avatarURL(u.AvatarID)}, nil
}

type questionView struct {
	domain.Question
	Author     marketProfile  `json:"author"`
	IsSeller   bool           `json:"isSeller"`
	ReplyCount int64          `json:"replyCount"`
	Replies    []questionView `json:"replies"`
}

func (s *Server) questionViews(r *http.Request, raffle domain.Raffle, parent string, limit, offset int64) ([]questionView, int64, error) {
	items, total, err := s.store.ListQuestions(r.Context(), raffle.ID, parent, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	views := make([]questionView, 0, len(items))
	for _, q := range items {
		author, err := s.marketProfile(r, q.UserID)
		if err != nil {
			return nil, 0, err
		}
		v := questionView{Question: q, Author: author, IsSeller: q.UserID == raffle.SellerID, Replies: []questionView{}}
		if parent == "" {
			v.Replies, v.ReplyCount, err = s.questionViews(r, raffle, q.ID, 2, 0)
			if err != nil {
				return nil, 0, err
			}
		}
		views = append(views, v)
	}
	return views, total, nil
}

func (s *Server) listQuestions(w http.ResponseWriter, r *http.Request) { s.questionList(w, r, false) }
func (s *Server) listReplies(w http.ResponseWriter, r *http.Request)   { s.questionList(w, r, true) }
func (s *Server) askQuestion(w http.ResponseWriter, r *http.Request)   { s.writeQuestion(w, r, false) }
func (s *Server) replyQuestion(w http.ResponseWriter, r *http.Request) { s.writeQuestion(w, r, true) }

func (s *Server) questionParent(w http.ResponseWriter, r *http.Request, raffle string) (string, bool) {
	q, err := s.store.GetQuestion(r.Context(), r.PathValue("questionId"))
	if err != nil {
		s.marketError(w, err)
		return "", false
	}
	if q.RaffleID != raffle || q.ParentID != "" {
		s.marketError(w, store.ErrNotFound)
		return "", false
	}
	return q.ID, true
}

func (s *Server) questionList(w http.ResponseWriter, r *http.Request, replies bool) {
	user, ok := s.marketUser(w, r, false)
	if !ok {
		return
	}
	raffle, ok := s.visibleRaffle(w, r, user)
	if !ok {
		return
	}
	limit, offset, ok := marketPagination(w, r)
	if !ok {
		return
	}
	parent := ""
	if replies {
		parent, ok = s.questionParent(w, r, raffle.ID)
		if !ok {
			return
		}
	}
	items, total, err := s.questionViews(r, raffle, parent, limit, offset)
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 200, page(items, total, limit, offset))
}

func textBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Body string `json:"body"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return "", false
	}
	req.Body = strings.TrimSpace(req.Body)
	if n := utf8.RuneCountInString(req.Body); n < 1 || n > 2000 {
		writeError(w, 400, *validationError("body", "Metin 1–2000 karakter olmalıdır."))
		return "", false
	}
	return req.Body, true
}

func (s *Server) writeQuestion(w http.ResponseWriter, r *http.Request, replies bool) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	raffle, ok := s.visibleRaffle(w, r, user)
	if !ok {
		return
	}
	body, ok := textBody(w, r)
	if !ok {
		return
	}
	parent := ""
	if replies {
		parent, ok = s.questionParent(w, r, raffle.ID)
		if !ok {
			return
		}
	}
	question := domain.Question{ID: security.NewID(), RaffleID: raffle.ID, UserID: user, ParentID: parent, Body: body, CreatedAt: s.now().UTC()}
	if err := s.store.CreateQuestion(r.Context(), question); err != nil {
		s.marketError(w, err)
		return
	}
	author, err := s.marketProfile(r, user)
	if err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 201, questionView{Question: question, Author: author, IsSeller: user == raffle.SellerID, Replies: []questionView{}})
}

type reviewView struct {
	domain.Review
	Author marketProfile `json:"author"`
}

func (s *Server) sellerReviews(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.marketUser(w, r, false); !ok {
		return
	}
	limit, offset, ok := marketPagination(w, r)
	if !ok {
		return
	}
	seller := r.PathValue("sellerId")
	if _, err := s.store.GetUserByID(r.Context(), seller); err != nil {
		s.marketError(w, err)
		return
	}
	items, total, average, err := s.store.ListReviews(r.Context(), seller, limit, offset)
	if err != nil {
		s.marketError(w, err)
		return
	}
	views := make([]reviewView, 0, len(items))
	for _, item := range items {
		author, err := s.marketProfile(r, item.UserID)
		if err != nil {
			s.marketError(w, err)
			return
		}
		views = append(views, reviewView{Review: item, Author: author})
	}
	result := page(views, total, limit, offset)
	result["averageRating"] = average
	writeJSON(w, 200, result)
}

func (s *Server) createReview(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	raffle, ok := s.visibleRaffle(w, r, user)
	if !ok {
		return
	}
	var req struct {
		Rating float64 `json:"rating"`
		Body   string  `json:"body"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeError(w, 400, *e)
		return
	}
	if req.Rating < 1 || req.Rating > 5 || math.Mod(req.Rating*2, 1) != 0 {
		writeError(w, 400, *validationError("rating", "Puan 1–5 arasında, 0.5 adımlı olmalıdır."))
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if n := utf8.RuneCountInString(req.Body); n < 1 || n > 2000 {
		writeError(w, 400, *validationError("body", "Değerlendirme 1–2000 karakter olmalıdır."))
		return
	}
	review := domain.Review{ID: security.NewID(), SellerID: raffle.SellerID, UserID: user, RaffleID: raffle.ID, Rating: req.Rating, Body: req.Body, CreatedAt: s.now().UTC()}
	if err := s.store.CreateReview(r.Context(), review); err != nil {
		s.marketError(w, err)
		return
	}
	writeJSON(w, 201, review)
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	limit, offset, ok := marketPagination(w, r)
	if !ok {
		return
	}
	unread := r.URL.Query().Get("unreadOnly")
	if unread != "" && unread != "true" && unread != "false" {
		writeError(w, 400, *validationError("unreadOnly", "true veya false olmalıdır."))
		return
	}
	items, total, err := s.store.ListNotifications(r.Context(), user, unread == "true", limit, offset)
	if err != nil {
		s.marketError(w, err)
		return
	}
	_, count, err := s.store.ListNotifications(r.Context(), user, true, 1, 0)
	if err != nil {
		s.marketError(w, err)
		return
	}
	result := page(items, total, limit, offset)
	result["unreadCount"] = count
	writeJSON(w, 200, result)
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	user, ok := s.marketUser(w, r, true)
	if !ok {
		return
	}
	if err := s.store.ReadNotification(r.Context(), user, r.PathValue("notificationId"), s.now().UTC()); err != nil {
		s.marketError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
