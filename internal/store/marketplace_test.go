package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"ceksepet-api/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func seedMarket(t *testing.T, s *Store, ctx context.Context, total int64) domain.Raffle {
	t.Helper()
	now := time.Now().UTC()
	for i, id := range []string{"seller", "buyer", "other"} {
		_, err := s.users.InsertOne(ctx, domain.User{ID: id, Username: id, Phone: fmt.Sprintf("+90555123456%d", i), TicketBalance: 100, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SavePhoto(ctx, domain.Photo{ID: "photo", OwnerID: "seller", Data: []byte("test-image"), ContentType: "image/png", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	r := domain.Raffle{ID: "raffle", SellerID: "seller", Title: "Tablet", Description: "Temiz tablet", CategoryID: "tablet", BrandID: "samsung", PhotoIDs: []string{"photo"}, ShippingPayer: "buyer", Quote: domain.PriceQuote{TotalTickets: total}, Status: domain.RafflePending, CreatedAt: now}
	if err := s.CreateRaffle(ctx, r); err != nil {
		t.Fatal(err)
	}
	r, err := s.ModerateRaffle(ctx, r.ID, domain.RaffleActive, "", now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMongoMarketplaceAtomicParticipationAndQueries(t *testing.T) {
	s := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	r := seedMarket(t, s, ctx, 10)
	now := time.Now().UTC()
	// Rejection must roll back the raffle increment as well as the wallet write.
	if _, err := s.users.UpdateOne(ctx, bson.M{"_id": "buyer"}, bson.M{"$set": bson.M{"ticket_balance": 0}}); err != nil {
		t.Fatal(err)
	}
	entry := domain.TicketEntry{ID: "entry", UserID: "buyer", Key: "join-key", Kind: "participation", RaffleID: r.ID, Delta: -4, CreatedAt: now}
	if _, _, err := s.ApplyTickets(ctx, entry); !errors.Is(err, ErrInsufficientTickets) {
		t.Fatalf("expected insufficient tickets: %v", err)
	}
	after, err := s.GetRaffle(ctx, r.ID)
	if err != nil || after.SoldTickets != 0 {
		t.Fatalf("failed transaction changed inventory: %+v %v", after, err)
	}
	if _, err := s.users.UpdateOne(ctx, bson.M{"_id": "buyer"}, bson.M{"$set": bson.M{"ticket_balance": 100}}); err != nil {
		t.Fatal(err)
	}
	first, replayed, err := s.ApplyTickets(ctx, entry)
	if err != nil || replayed || first.BalanceAfter != 96 {
		t.Fatalf("first receipt: %+v %v %v", first, replayed, err)
	}
	second, replayed, err := s.ApplyTickets(ctx, entry)
	if err != nil || !replayed || second.ID != first.ID {
		t.Fatalf("retry receipt: %+v %v %v", second, replayed, err)
	}
	changed := entry
	changed.Delta = -2
	if _, _, err := s.ApplyTickets(ctx, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting key accepted: %v", err)
	}
	if err := s.SetFavorite(ctx, "buyer", r.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []RaffleFilter{{JoinedBy: "buyer", Limit: 20}, {FavoritedBy: "buyer", Limit: 20}, {CategoryID: "electronics", Search: "Samsung", Limit: 20}} {
		items, total, err := s.ListRaffles(ctx, filter)
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("list %+v: %v %d %v", filter, items, total, err)
		}
	}
	q := domain.Question{ID: "question", RaffleID: r.ID, UserID: "buyer", Body: "Garanti?", CreatedAt: now}
	if err := s.CreateQuestion(ctx, q); err != nil {
		t.Fatal(err)
	}
	reply := domain.Question{ID: "reply", RaffleID: r.ID, UserID: "seller", ParentID: q.ID, Body: "Evet", CreatedAt: now}
	if err := s.CreateQuestion(ctx, reply); err != nil {
		t.Fatal(err)
	}
	questions, total, err := s.ListQuestions(ctx, r.ID, q.ID, 20, 0)
	if err != nil || total != 1 || questions[0].ID != reply.ID {
		t.Fatalf("replies: %v %v", questions, err)
	}
	notes, total, err := s.ListNotifications(ctx, "buyer", true, 20, 0)
	if err != nil || total != 1 {
		t.Fatalf("notifications: %v %d %v", notes, total, err)
	}
	if err := s.ReadNotification(ctx, "other", notes[0].ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read another user's notification: %v", err)
	}
	if err := s.ReadNotification(ctx, "buyer", notes[0].ID, now); err != nil {
		t.Fatal(err)
	}
	finish := entry
	finish.ID = "finish"
	finish.Key = "finish-key"
	finish.Delta = -6
	if _, _, err := s.ApplyTickets(ctx, finish); err != nil {
		t.Fatal(err)
	}
	review := domain.Review{ID: "review", SellerID: "seller", UserID: "buyer", RaffleID: r.ID, Rating: 4, Body: "İyi", CreatedAt: now}
	if err := s.CreateReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateReview(ctx, review); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate review: %v", err)
	}
	_, total, average, err := s.ListReviews(ctx, "seller", 20, 0)
	if err != nil || total != 1 || average != 4 {
		t.Fatalf("reviews: %d %f %v", total, average, err)
	}
	if err := s.DeleteUser(ctx, "buyer"); !errors.Is(err, ErrAccountInUse) {
		t.Fatalf("deleted participant account: %v", err)
	}
	if err := s.DeleteUser(ctx, "seller"); !errors.Is(err, ErrAccountInUse) {
		t.Fatalf("deleted seller account: %v", err)
	}
}

func TestMongoMarketplaceConcurrentLastTickets(t *testing.T) {
	s := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r := seedMarket(t, s, ctx, 7)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := "buyer"
			if i%2 == 1 {
				user = "other"
			}
			_, _, err := s.ApplyTickets(ctx, domain.TicketEntry{ID: fmt.Sprintf("entry-%d", i), UserID: user, Key: fmt.Sprintf("key-%d", i), Kind: "participation", RaffleID: r.ID, Delta: -1, CreatedAt: time.Now().UTC()})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrRaffleUnavailable) {
			t.Errorf("unexpected concurrency error: %v", err)
		}
	}
	if success != 7 {
		t.Fatalf("expected 7 successful entries, got %d", success)
	}
	r, err := s.GetRaffle(ctx, r.ID)
	if err != nil || r.SoldTickets != 7 || r.Status != domain.RaffleCompleted {
		t.Fatalf("oversold or invalid status: %+v %v", r, err)
	}
	buyer, err := s.GetUserByID(ctx, "buyer")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.GetUserByID(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	if buyer.TicketBalance+other.TicketBalance != 193 {
		t.Fatal("wallet debit differs from sold tickets")
	}
	n, err := s.market("ticket_entries").CountDocuments(ctx, bson.M{})
	if err != nil || n != 7 {
		t.Fatalf("ledger count: %d %v", n, err)
	}
}

func TestMongoMarketplaceConcurrentIdempotency(t *testing.T) {
	s := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	r := seedMarket(t, s, ctx, 50)
	var wg sync.WaitGroup
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipt, _, err := s.ApplyTickets(ctx, domain.TicketEntry{ID: fmt.Sprintf("replay-%d", i), UserID: "buyer", Key: "same-key", Kind: "participation", RaffleID: r.ID, Delta: -3, CreatedAt: time.Now().UTC()})
			if err == nil && receipt.BalanceAfter != 97 {
				err = fmt.Errorf("unexpected balance %d", receipt.BalanceAfter)
			}
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
	u, err := s.GetUserByID(ctx, "buyer")
	if err != nil || u.TicketBalance != 97 {
		t.Fatalf("double debit: %+v %v", u, err)
	}
	r, err = s.GetRaffle(ctx, r.ID)
	if err != nil || r.SoldTickets != 3 {
		t.Fatalf("double sale: %+v %v", r, err)
	}
}
