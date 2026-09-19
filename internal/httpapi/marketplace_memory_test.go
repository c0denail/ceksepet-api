package httpapi

import (
	"context"
	"sort"
	"strings"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

type memoryMarket struct {
	photos         map[string]domain.Photo
	raffles        map[string]domain.Raffle
	favorites      map[string]bool
	participations map[string]domain.Participation
	entries        map[string]domain.TicketEntry
	questions      map[string]domain.Question
	reviews        map[string]domain.Review
	notifications  map[string]domain.Notification
	reports        map[string]domain.SellerReport
}

func newMemoryMarket() memoryMarket {
	return memoryMarket{
		photos: map[string]domain.Photo{}, raffles: map[string]domain.Raffle{}, favorites: map[string]bool{},
		participations: map[string]domain.Participation{}, entries: map[string]domain.TicketEntry{},
		questions: map[string]domain.Question{}, reviews: map[string]domain.Review{}, notifications: map[string]domain.Notification{},
		reports: map[string]domain.SellerReport{},
	}
}

func slicePage[T any](items []T, limit, offset int64) ([]T, int64, error) {
	total := int64(len(items))
	if offset >= total {
		return []T{}, total, nil
	}
	return items[offset:min(total, offset+limit)], total, nil
}

func (s *memoryStore) SavePhoto(_ context.Context, p domain.Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.photos[p.ID] = p
	return nil
}
func (s *memoryStore) GetPhoto(_ context.Context, id string) (domain.Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.photos[id]
	if !ok {
		return p, store.ErrNotFound
	}
	return p, nil
}
func (s *memoryStore) CreateRaffle(_ context.Context, r domain.Raffle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range r.PhotoIDs {
		p, ok := s.photos[id]
		if !ok || p.OwnerID != r.SellerID || p.RaffleID != "" {
			return store.ErrForbidden
		}
	}
	for _, id := range r.PhotoIDs {
		p := s.photos[id]
		p.RaffleID = r.ID
		s.photos[id] = p
	}
	s.raffles[r.ID] = r
	return nil
}
func (s *memoryStore) GetRaffle(_ context.Context, id string) (domain.Raffle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.raffles[id]
	if !ok {
		return r, store.ErrNotFound
	}
	return r, nil
}
func (s *memoryStore) ListRaffles(_ context.Context, f store.RaffleFilter) ([]domain.Raffle, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []domain.Raffle{}
	for _, r := range s.raffles {
		if f.Status == "" && r.Status != domain.RaffleActive && r.Status != domain.RaffleCompleted && !(f.JoinedBy != "" && r.Status == domain.RaffleCancelled) {
			continue
		}
		if f.Status != "" && f.Status != "all" && r.Status != f.Status {
			continue
		}
		if f.SellerID != "" && f.SellerID != r.SellerID {
			continue
		}
		if f.ExcludeID == r.ID {
			continue
		}
		if f.CategoryID != "" && !contains(domain.CategoryIDs(f.CategoryID), r.CategoryID) {
			continue
		}
		if f.BrandID != "" && f.BrandID != r.BrandID {
			continue
		}
		brand, _ := domain.BrandByID(r.BrandID)
		if f.Search != "" && !strings.Contains(strings.ToLower(r.Title+" "+brand.Name), strings.ToLower(f.Search)) {
			continue
		}
		if f.FavoritedBy != "" && !s.favorites[f.FavoritedBy+":"+r.ID] {
			continue
		}
		if f.JoinedBy != "" {
			if _, ok := s.participations[f.JoinedBy+":"+r.ID]; !ok {
				continue
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if f.Sort == "popular" && rows[i].SoldTickets != rows[j].SoldTickets {
			return rows[i].SoldTickets > rows[j].SoldTickets
		}
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	return slicePage(rows, f.Limit, f.Offset)
}
func (s *memoryStore) marketNotify(user, raffle, kind, message string, now time.Time) {
	n := domain.Notification{ID: security.NewID(), UserID: user, RaffleID: raffle, Kind: kind, Message: message, CreatedAt: now}
	s.notifications[n.ID] = n
}
func (s *memoryStore) ModerateRaffle(_ context.Context, id, status, reason string, now time.Time) (domain.Raffle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.raffles[id]
	if !ok || r.Status != domain.RafflePending {
		return r, store.ErrRaffleUnavailable
	}
	r.Status = status
	r.RejectionReason = reason
	if status == domain.RaffleActive {
		r.PublishedAt = &now
	}
	s.raffles[id] = r
	s.marketNotify(r.SellerID, id, "moderation", status, now)
	return r, nil
}
func (s *memoryStore) SetFavorite(_ context.Context, user, raffle string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := user + ":" + raffle
	if enabled {
		s.favorites[key] = true
	} else {
		delete(s.favorites, key)
	}
	return nil
}
func (s *memoryStore) IsFavorite(_ context.Context, user, raffle string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.favorites[user+":"+raffle], nil
}
func (s *memoryStore) GetParticipation(_ context.Context, user, raffle string) (domain.Participation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.participations[user+":"+raffle]
	if !ok {
		return p, store.ErrNotFound
	}
	return p, nil
}
func (s *memoryStore) ApplyTickets(_ context.Context, e domain.TicketEntry) (domain.TicketEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := e.UserID + ":" + e.Key
	if old, ok := s.entries[key]; ok {
		if old.Kind != e.Kind || old.RaffleID != e.RaffleID || old.Delta != e.Delta || old.Reason != e.Reason || old.Actor != e.Actor {
			return old, false, store.ErrIdempotencyConflict
		}
		return old, true, nil
	}
	u, ok := s.users[e.UserID]
	if !ok {
		return e, false, store.ErrNotFound
	}
	if e.Kind == "participation" {
		r, ok := s.raffles[e.RaffleID]
		if !ok {
			return e, false, store.ErrNotFound
		}
		if r.SellerID == e.UserID {
			return e, false, store.ErrForbidden
		}
		if r.Status != domain.RaffleActive || r.Quote.TotalTickets-r.SoldTickets < -e.Delta {
			return e, false, store.ErrRaffleUnavailable
		}
		if u.TicketBalance < -e.Delta {
			return e, false, store.ErrInsufficientTickets
		}
		r.SoldTickets -= e.Delta
		if r.SoldTickets == r.Quote.TotalTickets {
			r.Status = domain.RaffleCompleted
		}
		s.raffles[r.ID] = r
		e.SoldTickets = r.SoldTickets
		e.RaffleStatus = r.Status
		pkey := e.UserID + ":" + r.ID
		p := s.participations[pkey]
		if p.ID == "" {
			p = domain.Participation{ID: security.NewID(), UserID: e.UserID, RaffleID: r.ID}
		}
		p.Tickets -= e.Delta
		p.UpdatedAt = e.CreatedAt
		s.participations[pkey] = p
	} else if e.Kind != "credit" || e.Delta <= 0 || u.TicketBalance+e.Delta > 1_000_000_000 {
		return e, false, store.ErrConflict
	}
	u.TicketBalance += e.Delta
	e.BalanceAfter = u.TicketBalance
	s.users[u.ID] = u
	s.entries[key] = e
	return e, false, nil
}
func (s *memoryStore) ListTicketEntries(_ context.Context, user string, limit, offset int64) ([]domain.TicketEntry, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []domain.TicketEntry{}
	for _, e := range s.entries {
		if e.UserID == user {
			items = append(items, e)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return slicePage(items, limit, offset)
}
func (s *memoryStore) GetQuestion(_ context.Context, id string) (domain.Question, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.questions[id]
	if !ok {
		return q, store.ErrNotFound
	}
	return q, nil
}
func (s *memoryStore) CreateQuestion(_ context.Context, q domain.Question) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.raffles[q.RaffleID]
	if !ok {
		return store.ErrNotFound
	}
	if r.Status != domain.RaffleActive && r.Status != domain.RaffleCompleted {
		return store.ErrRaffleUnavailable
	}
	recipient := r.SellerID
	if q.ParentID != "" {
		p, ok := s.questions[q.ParentID]
		if !ok {
			return store.ErrNotFound
		}
		if p.RaffleID != q.RaffleID || p.ParentID != "" {
			return store.ErrConflict
		}
		recipient = p.UserID
	}
	s.questions[q.ID] = q
	if recipient != q.UserID {
		s.marketNotify(recipient, q.RaffleID, "question", q.Body, q.CreatedAt)
	}
	return nil
}
func (s *memoryStore) ListQuestions(_ context.Context, raffle, parent string, limit, offset int64) ([]domain.Question, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []domain.Question{}
	for _, q := range s.questions {
		if q.RaffleID == raffle && q.ParentID == parent {
			items = append(items, q)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return slicePage(items, limit, offset)
}
func (s *memoryStore) CreateReview(_ context.Context, r domain.Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raffle, ok := s.raffles[r.RaffleID]
	if !ok {
		return store.ErrNotFound
	}
	if raffle.Status != domain.RaffleCompleted || raffle.SellerID != r.SellerID || r.UserID == r.SellerID {
		return store.ErrForbidden
	}
	key := r.UserID + ":" + r.RaffleID
	if _, ok := s.participations[key]; !ok {
		return store.ErrForbidden
	}
	if _, ok := s.reviews[key]; ok {
		return store.ErrConflict
	}
	s.reviews[key] = r
	return nil
}
func (s *memoryStore) ListReviews(_ context.Context, seller string, limit, offset int64) ([]domain.Review, int64, float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []domain.Review{}
	sum := 0.0
	for _, r := range s.reviews {
		if r.SellerID == seller {
			items = append(items, r)
			sum += float64(r.Rating)
		}
	}
	average := 0.0
	if len(items) > 0 {
		average = sum / float64(len(items))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	items, total, err := slicePage(items, limit, offset)
	return items, total, average, err
}
func (s *memoryStore) ListNotifications(_ context.Context, user string, unread bool, limit, offset int64) ([]domain.Notification, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []domain.Notification{}
	for _, n := range s.notifications {
		if n.UserID == user && (!unread || n.ReadAt == nil) {
			items = append(items, n)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return slicePage(items, limit, offset)
}
func (s *memoryStore) ReadNotification(_ context.Context, user, id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notifications[id]
	if !ok || n.UserID != user {
		return store.ErrNotFound
	}
	n.ReadAt = &now
	s.notifications[id] = n
	return nil
}

func (s *memoryStore) checkMarketDelete(user string) error {
	for _, e := range s.entries {
		if e.UserID == user {
			return store.ErrAccountInUse
		}
	}
	for _, r := range s.raffles {
		if r.SellerID == user && (r.Status == domain.RaffleActive || r.Status == domain.RaffleCompleted || r.SoldTickets > 0) {
			return store.ErrAccountInUse
		}
	}
	for id, r := range s.raffles {
		if r.SellerID == user {
			delete(s.raffles, id)
		}
	}
	for id, p := range s.photos {
		if p.OwnerID == user {
			delete(s.photos, id)
		}
	}
	for id, n := range s.notifications {
		if n.UserID == user {
			delete(s.notifications, id)
		}
	}
	for key := range s.favorites {
		if strings.HasPrefix(key, user+":") {
			delete(s.favorites, key)
		}
	}
	return nil
}
