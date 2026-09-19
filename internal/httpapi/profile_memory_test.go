package httpapi

import (
	"context"
	"sort"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

func (s *memoryStore) CancelRaffle(_ context.Context, id, reason, actor string, now time.Time) (domain.Raffle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.raffles[id]
	if !ok {
		return r, store.ErrNotFound
	}
	if r.Status == domain.RaffleCancelled {
		return r, nil
	}
	if r.Status != domain.RafflePending && r.Status != domain.RaffleActive {
		return r, store.ErrRaffleUnavailable
	}
	r.Status, r.CancelledAt, r.CancellationReason = domain.RaffleCancelled, &now, reason
	s.raffles[id] = r
	for key, p := range s.participations {
		if p.RaffleID != id {
			continue
		}
		refund := p.Tickets - p.RefundedTickets
		if refund <= 0 {
			continue
		}
		u := s.users[p.UserID]
		u.TicketBalance += refund
		s.users[u.ID] = u
		e := domain.TicketEntry{ID: security.NewID(), UserID: u.ID, Key: "raffle-refund:" + id, Kind: "raffle_refund", RaffleID: id, Delta: refund, BalanceAfter: u.TicketBalance, RaffleStatus: domain.RaffleCancelled, Reason: reason, Actor: actor, CreatedAt: now}
		s.entries[u.ID+":"+e.Key] = e
		p.RefundedTickets, p.UpdatedAt = p.Tickets, now
		s.participations[key] = p
		s.marketNotify(u.ID, id, "raffle_cancelled", "Biletlerin iade edildi.", now)
	}
	s.marketNotify(r.SellerID, id, "raffle_cancelled", reason, now)
	return r, nil
}

func (s *memoryStore) CreateSellerReport(_ context.Context, r domain.SellerReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ReporterID == r.SellerID {
		return store.ErrForbidden
	}
	if _, ok := s.users[r.SellerID]; !ok {
		return store.ErrNotFound
	}
	if _, ok := s.users[r.ReporterID]; !ok {
		return store.ErrNotFound
	}
	for _, old := range s.reports {
		if old.ReporterID == r.ReporterID && old.SellerID == r.SellerID && old.Status == "open" {
			return store.ErrConflict
		}
	}
	s.reports[r.ID] = r
	return nil
}

func (s *memoryStore) ListSellerReports(_ context.Context, status string, limit, offset int64) ([]domain.SellerReport, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []domain.SellerReport{}
	for _, r := range s.reports {
		if status == "all" || r.Status == status {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	return slicePage(rows, limit, offset)
}

func (s *memoryStore) ResolveSellerReport(_ context.Context, id, status, resolution, actor string, now time.Time) (domain.SellerReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reports[id]
	if !ok {
		return r, store.ErrNotFound
	}
	if r.Status != "open" {
		if r.Status == status && r.Resolution == resolution {
			return r, nil
		}
		return r, store.ErrConflict
	}
	r.Status, r.Resolution, r.ResolvedBy, r.ResolvedAt = status, resolution, actor, &now
	s.reports[id] = r
	return r, nil
}
