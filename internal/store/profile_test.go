package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ceksepet-api/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoCancellationRefundsOnceWithConcurrentParticipation(t *testing.T) {
	s := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	raffle := seedMarket(t, s, ctx, 1000)
	now := time.Now().UTC()
	initial := domain.TicketEntry{ID: "initial", UserID: "buyer", Key: "initial-key", Kind: "participation", RaffleID: raffle.ID, Delta: -10, CreatedAt: now}
	if _, _, err := s.ApplyTickets(ctx, initial); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CancelRaffle(ctx, raffle.ID, "Ürün temin edilemiyor.", "operator", now)
			errs <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := s.ApplyTickets(ctx, domain.TicketEntry{ID: "racing", UserID: "other", Key: "racing-key", Kind: "participation", RaffleID: raffle.ID, Delta: -7, CreatedAt: now})
		if errors.Is(err, ErrRaffleUnavailable) {
			err = nil
		}
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.GetRaffle(ctx, raffle.ID)
	if err != nil || after.Status != domain.RaffleCancelled {
		t.Fatalf("cancel: %+v %v", after, err)
	}
	for _, id := range []string{"buyer", "other"} {
		user, err := s.GetUserByID(ctx, id)
		if err != nil || user.TicketBalance != 100 {
			t.Fatalf("refund %s: %+v %v", id, user, err)
		}
		p, err := s.GetParticipation(ctx, id, raffle.ID)
		if errors.Is(err, ErrNotFound) && id == "other" {
			continue
		}
		if err != nil || p.RefundedTickets != p.Tickets {
			t.Fatalf("refund participation: %+v %v", p, err)
		}
		n, err := s.market("ticket_entries").CountDocuments(ctx, bson.M{"user_id": id, "kind": "raffle_refund"})
		if err != nil || n != 1 {
			t.Fatalf("refund count %s = %d: %v", id, n, err)
		}
	}
	if _, _, err := s.ApplyTickets(ctx, domain.TicketEntry{ID: "after", UserID: "buyer", Key: "after-key", Kind: "participation", RaffleID: raffle.ID, Delta: -1, CreatedAt: now}); !errors.Is(err, ErrRaffleUnavailable) {
		t.Fatalf("joined cancelled: %v", err)
	}
	rows, total, err := s.ListRaffles(ctx, RaffleFilter{JoinedBy: "buyer", Limit: 20})
	if err != nil || total != 1 || rows[0].Status != domain.RaffleCancelled {
		t.Fatalf("joined cancelled list: %+v %d %v", rows, total, err)
	}
	if err := s.DeleteUser(ctx, "seller"); !errors.Is(err, ErrAccountInUse) {
		t.Fatalf("seller history deleted: %v", err)
	}
}

func TestMongoCancellationRollback(t *testing.T) {
	s := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raffle := seedMarket(t, s, ctx, 1000)
	now := time.Now().UTC()
	if _, _, err := s.ApplyTickets(ctx, domain.TicketEntry{ID: "join", UserID: "buyer", Key: "join-key", Kind: "participation", RaffleID: raffle.ID, Delta: -10, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// Simulate an inconsistent preexisting refund receipt. Any failure after the
	// wallet increment must roll back the increment and raffle cancellation.
	if _, err := s.market("ticket_entries").InsertOne(ctx, domain.TicketEntry{ID: "collision", UserID: "buyer", Key: "raffle-refund:" + raffle.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelRaffle(ctx, raffle.ID, "Test iptali", "operator", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	user, err := s.GetUserByID(ctx, "buyer")
	if err != nil || user.TicketBalance != 90 {
		t.Fatalf("partial refund: %+v %v", user, err)
	}
	after, err := s.GetRaffle(ctx, raffle.ID)
	if err != nil || after.Status != domain.RaffleActive {
		t.Fatalf("partial cancellation: %+v %v", after, err)
	}
	p, err := s.GetParticipation(ctx, "buyer", raffle.ID)
	if err != nil || p.RefundedTickets != 0 {
		t.Fatalf("partial participation: %+v %v", p, err)
	}
}

func TestMongoSellerReportUniquenessAndResolution(t *testing.T) {
	s := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seedMarket(t, s, ctx, 1000)
	now := time.Now().UTC()
	r := domain.SellerReport{ID: "report-1", ReporterID: "buyer", SellerID: "seller", ReasonID: "misleading_listing", Status: "open", CreatedAt: now}
	if err := s.CreateSellerReport(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.ID = "report-2"
	if err := s.CreateSellerReport(ctx, r); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate open report: %v", err)
	}
	rows, total, err := s.ListSellerReports(ctx, "open", 20, 0)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("report inbox: %+v %d %v", rows, total, err)
	}
	for i := 0; i < 2; i++ {
		resolved, err := s.ResolveSellerReport(ctx, "report-1", "resolved", "Açıklama düzeltildi.", "operator", now)
		if err != nil || resolved.Status != "resolved" || resolved.ResolvedAt == nil {
			t.Fatalf("resolution: %+v %v", resolved, err)
		}
	}
	if _, err := s.ResolveSellerReport(ctx, "report-1", "dismissed", "Farklı sonuç", "operator", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("overwrote resolution: %v", err)
	}
	if err := s.CreateSellerReport(ctx, r); err != nil {
		t.Fatalf("new report after resolution: %v", err)
	}
}
