package store

import (
	"context"
	"errors"
	"regexp"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *Store) market(name string) *mongo.Collection { return s.database.Collection(name) }

func (s *Store) ensureMarketplaceIndexes(ctx context.Context) error {
	definitions := map[string][]mongo.IndexModel{
		"raffles": {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}},
			{Keys: bson.D{{Key: "seller_id", Value: 1}, {Key: "status", Value: 1}}},
			{Keys: bson.D{{Key: "category_id", Value: 1}, {Key: "status", Value: 1}}},
		},
		"photos":    {{Keys: bson.D{{Key: "owner_id", Value: 1}}}},
		"favorites": {{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "raffle_id", Value: 1}}, Options: options.Index().SetUnique(true)}},
		"ticket_entries": {
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "key", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
		},
		"questions": {{Keys: bson.D{{Key: "raffle_id", Value: 1}, {Key: "parent_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		"reviews": {
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "raffle_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "seller_id", Value: 1}, {Key: "created_at", Value: -1}}},
		},
		"notifications": {{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		"seller_reports": {
			{Keys: bson.D{{Key: "reporter_id", Value: 1}, {Key: "seller_id", Value: 1}}, Options: options.Index().SetName("uq_open_seller_report").SetUnique(true).SetPartialFilterExpression(bson.M{"status": "open"})},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: -1}}},
		},
		"participations": {{Keys: bson.D{{Key: "raffle_id", Value: 1}}}, {Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "raffle_id", Value: 1}}, Options: options.Index().SetUnique(true)}},
	}
	for name, indexes := range definitions {
		if _, err := s.market(name).Indexes().CreateMany(ctx, indexes); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) transaction(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	session, err := s.client.StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctx)
	value, err := session.WithTransaction(ctx, fn)
	return value, mapError(err)
}

// Touching the user serializes writes with account deletion.
func (s *Store) touchUser(ctx context.Context, id string) error {
	return updateOne(ctx, s.users, bson.M{"_id": id}, bson.M{"$inc": bson.M{"market_version": 1}})
}

func (s *Store) SavePhoto(ctx context.Context, photo domain.Photo) error {
	_, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if err := s.touchUser(tx, photo.OwnerID); err != nil {
			return nil, err
		}
		_, err := s.market("photos").InsertOne(tx, photo)
		return nil, err
	})
	return err
}

func (s *Store) GetPhoto(ctx context.Context, id string) (domain.Photo, error) {
	var result domain.Photo
	err := s.market("photos").FindOne(ctx, bson.M{"_id": id}).Decode(&result)
	return result, mapError(err)
}

func (s *Store) CreateRaffle(ctx context.Context, raffle domain.Raffle) error {
	_, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if err := s.touchUser(tx, raffle.SellerID); err != nil {
			return nil, err
		}
		for _, id := range raffle.PhotoIDs {
			if err := updateOne(tx, s.market("photos"), bson.M{"_id": id, "owner_id": raffle.SellerID, "raffle_id": ""},
				bson.M{"$set": bson.M{"raffle_id": raffle.ID}}); err != nil {
				if errors.Is(err, ErrNotFound) {
					return nil, ErrForbidden
				}
				return nil, err
			}
		}
		_, err := s.market("raffles").InsertOne(tx, raffle)
		return nil, err
	})
	return err
}

func (s *Store) GetRaffle(ctx context.Context, id string) (domain.Raffle, error) {
	var result domain.Raffle
	err := s.market("raffles").FindOne(ctx, bson.M{"_id": id}).Decode(&result)
	return result, mapError(err)
}

func (s *Store) ListRaffles(ctx context.Context, f RaffleFilter) ([]domain.Raffle, int64, error) {
	filter := bson.M{}
	if f.Status != "" {
		filter["status"] = f.Status
	} else {
		statuses := []string{domain.RaffleActive, domain.RaffleCompleted}
		if f.JoinedBy != "" {
			statuses = append(statuses, domain.RaffleCancelled)
		}
		filter["status"] = bson.M{"$in": statuses}
	}
	if f.Status == "all" {
		delete(filter, "status")
	}
	if f.SellerID != "" {
		filter["seller_id"] = f.SellerID
	}
	if f.CategoryID != "" {
		filter["category_id"] = bson.M{"$in": domain.CategoryIDs(f.CategoryID)}
	}
	if f.BrandID != "" {
		filter["brand_id"] = f.BrandID
	}
	if f.Search != "" {
		pattern := bson.Regex{Pattern: regexp.QuoteMeta(f.Search), Options: "i"}
		brands := []string{}
		rx := regexp.MustCompile("(?i)" + regexp.QuoteMeta(f.Search))
		for _, b := range domain.Brands {
			if rx.MatchString(b.Name) {
				brands = append(brands, b.ID)
			}
		}
		filter["$or"] = bson.A{bson.M{"title": pattern}, bson.M{"brand_id": bson.M{"$in": brands}}}
	}
	if f.ExcludeID != "" {
		filter["_id"] = bson.M{"$ne": f.ExcludeID}
	}
	for _, relation := range []struct{ collection, user string }{{"participations", f.JoinedBy}, {"favorites", f.FavoritedBy}} {
		if relation.user == "" {
			continue
		}
		var ids []string
		if err := s.market(relation.collection).Distinct(ctx, "raffle_id", bson.M{"user_id": relation.user}).Decode(&ids); err != nil {
			return nil, 0, err
		}
		if ids == nil {
			ids = []string{}
		}
		if existing, ok := filter["_id"].(bson.M); ok {
			existing["$in"] = ids
		} else {
			filter["_id"] = bson.M{"$in": ids}
		}
	}
	sort := bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}
	if f.Sort == "popular" {
		sort = bson.D{{Key: "sold_tickets", Value: -1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}
	}
	return listDocuments[domain.Raffle](ctx, s.market("raffles"), filter, sort, f.Limit, f.Offset)
}

func listDocuments[T any](ctx context.Context, collection *mongo.Collection, filter any, sort bson.D, limit, offset int64) ([]T, int64, error) {
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := collection.Find(ctx, filter, options.Find().SetSort(sort).SetLimit(limit).SetSkip(offset))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	result := make([]T, 0)
	err = cursor.All(ctx, &result)
	return result, total, err
}

func (s *Store) notify(ctx context.Context, user, raffle, kind, message string, now time.Time) error {
	_, err := s.market("notifications").InsertOne(ctx, domain.Notification{ID: security.NewID(), UserID: user, RaffleID: raffle, Kind: kind, Message: message, CreatedAt: now})
	return err
}

func (s *Store) ModerateRaffle(ctx context.Context, id, status, reason string, now time.Time) (domain.Raffle, error) {
	value, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		fields := bson.M{"status": status, "rejection_reason": reason}
		if status == domain.RaffleActive {
			fields["published_at"] = now
		}
		var raffle domain.Raffle
		err := s.market("raffles").FindOneAndUpdate(tx, bson.M{"_id": id, "status": domain.RafflePending}, bson.M{"$set": fields}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&raffle)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrRaffleUnavailable
		}
		if err != nil {
			return nil, err
		}
		if err := s.touchUser(tx, raffle.SellerID); err != nil {
			return nil, err
		}
		message := "Ürününüz onaylandı ve çekiliş yayına alındı."
		if status == domain.RaffleRejected {
			message = "Ürününüz onaylanmadı: " + reason
		}
		if err := s.notify(tx, raffle.SellerID, id, "moderation", message, now); err != nil {
			return nil, err
		}
		return raffle, nil
	})
	if err != nil {
		return domain.Raffle{}, err
	}
	return value.(domain.Raffle), nil
}

func (s *Store) SetFavorite(ctx context.Context, user, raffle string, enabled bool) error {
	filter := bson.M{"user_id": user, "raffle_id": raffle}
	if !enabled {
		_, err := s.market("favorites").DeleteOne(ctx, filter)
		return err
	}
	_, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if err := s.touchUser(tx, user); err != nil {
			return nil, err
		}
		_, err := s.market("favorites").UpdateOne(tx, filter, bson.M{"$setOnInsert": filter}, options.UpdateOne().SetUpsert(true))
		return nil, err
	})
	// Concurrent idempotent PUTs may race on the unique index.
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

func (s *Store) IsFavorite(ctx context.Context, user, raffle string) (bool, error) {
	if user == "" {
		return false, nil
	}
	n, err := s.market("favorites").CountDocuments(ctx, bson.M{"user_id": user, "raffle_id": raffle}, options.Count().SetLimit(1))
	return n > 0, err
}

func (s *Store) GetParticipation(ctx context.Context, user, raffle string) (domain.Participation, error) {
	var p domain.Participation
	err := s.market("participations").FindOne(ctx, bson.M{"user_id": user, "raffle_id": raffle}).Decode(&p)
	return p, mapError(err)
}

func sameTicketRequest(a, b domain.TicketEntry) bool {
	return a.Kind == b.Kind && a.RaffleID == b.RaffleID && a.Delta == b.Delta && a.Reason == b.Reason && a.Actor == b.Actor
}

func (s *Store) ticketReceipt(ctx context.Context, entry domain.TicketEntry) (domain.TicketEntry, error) {
	var previous domain.TicketEntry
	err := s.market("ticket_entries").FindOne(ctx, bson.M{"user_id": entry.UserID, "key": entry.Key}).Decode(&previous)
	if err == nil && !sameTicketRequest(previous, entry) {
		return previous, ErrIdempotencyConflict
	}
	return previous, mapError(err)
}

func (s *Store) ApplyTickets(ctx context.Context, entry domain.TicketEntry) (domain.TicketEntry, bool, error) {
	if previous, err := s.ticketReceipt(ctx, entry); !errors.Is(err, ErrNotFound) {
		return previous, err == nil, err
	}
	type outcome struct {
		entry  domain.TicketEntry
		replay bool
	}
	value, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if previous, err := s.ticketReceipt(tx, entry); !errors.Is(err, ErrNotFound) {
			return outcome{previous, true}, err
		}
		result := entry // Transaction callbacks may be retried.
		userFilter := bson.M{"_id": entry.UserID}
		if entry.Kind == "participation" {
			if entry.Delta >= 0 || entry.Delta < -200_000_000 {
				return nil, ErrConflict
			}
			raffle, err := s.GetRaffle(tx, entry.RaffleID)
			if err != nil {
				return nil, err
			}
			if raffle.SellerID == entry.UserID {
				return nil, ErrForbidden
			}
			if raffle.Status != domain.RaffleActive || raffle.Quote.TotalTickets-raffle.SoldTickets < -entry.Delta {
				return nil, ErrRaffleUnavailable
			}
			result.SoldTickets = raffle.SoldTickets - entry.Delta
			result.RaffleStatus = domain.RaffleActive
			if result.SoldTickets == raffle.Quote.TotalTickets {
				result.RaffleStatus = domain.RaffleCompleted
			}
			if err := updateOne(tx, s.market("raffles"), bson.M{"_id": raffle.ID, "status": domain.RaffleActive, "sold_tickets": raffle.SoldTickets}, bson.M{"$set": bson.M{"sold_tickets": result.SoldTickets, "status": result.RaffleStatus}}); err != nil {
				return nil, err
			}
			userFilter["ticket_balance"] = bson.M{"$gte": -entry.Delta}
		} else if entry.Kind != "credit" || entry.Delta <= 0 || entry.Delta > 1_000_000 {
			return nil, ErrConflict
		}
		// Balance has a fixed upper bound to avoid numeric overflow on repeated credits.
		if entry.Delta > 0 {
			userFilter["$or"] = bson.A{bson.M{"ticket_balance": bson.M{"$lte": 1_000_000_000 - entry.Delta}}, bson.M{"ticket_balance": bson.M{"$exists": false}}}
		}
		var user domain.User
		err := s.users.FindOneAndUpdate(tx, userFilter, bson.M{"$inc": bson.M{"ticket_balance": entry.Delta}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&user)
		if errors.Is(err, mongo.ErrNoDocuments) {
			if _, e := s.GetUserByID(tx, entry.UserID); e != nil {
				return nil, e
			}
			if entry.Delta < 0 {
				return nil, ErrInsufficientTickets
			}
			return nil, ErrConflict
		}
		if err != nil {
			return nil, err
		}
		result.BalanceAfter = user.TicketBalance
		if entry.Kind == "participation" {
			filter := bson.M{"user_id": entry.UserID, "raffle_id": entry.RaffleID}
			_, err = s.market("participations").UpdateOne(tx, filter, bson.M{
				"$setOnInsert": bson.M{"_id": security.NewID(), "user_id": entry.UserID, "raffle_id": entry.RaffleID},
				"$inc":         bson.M{"tickets": -entry.Delta}, "$set": bson.M{"updated_at": entry.CreatedAt}}, options.UpdateOne().SetUpsert(true))
			if err != nil {
				return nil, err
			}
		}
		if _, err = s.market("ticket_entries").InsertOne(tx, result); err != nil {
			return nil, err
		}
		return outcome{result, false}, nil
	})
	// A simultaneous request can lose the unique-index race after the winner commits.
	if errors.Is(err, ErrConflict) {
		if previous, e := s.ticketReceipt(ctx, entry); !errors.Is(e, ErrNotFound) {
			return previous, e == nil, e
		}
	}
	if err != nil {
		return domain.TicketEntry{}, false, err
	}
	result := value.(outcome)
	return result.entry, result.replay, nil
}

func (s *Store) ListTicketEntries(ctx context.Context, user string, limit, offset int64) ([]domain.TicketEntry, int64, error) {
	return listDocuments[domain.TicketEntry](ctx, s.market("ticket_entries"), bson.M{"user_id": user}, bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}, limit, offset)
}

func (s *Store) GetQuestion(ctx context.Context, id string) (domain.Question, error) {
	var q domain.Question
	err := s.market("questions").FindOne(ctx, bson.M{"_id": id}).Decode(&q)
	return q, mapError(err)
}

func (s *Store) CreateQuestion(ctx context.Context, q domain.Question) error {
	_, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if err := s.touchUser(tx, q.UserID); err != nil {
			return nil, err
		}
		raffle, err := s.GetRaffle(tx, q.RaffleID)
		if err != nil {
			return nil, err
		}
		if raffle.Status != domain.RaffleActive && raffle.Status != domain.RaffleCompleted {
			return nil, ErrRaffleUnavailable
		}
		recipient := raffle.SellerID
		if q.ParentID != "" {
			parent, err := s.GetQuestion(tx, q.ParentID)
			if err != nil {
				return nil, err
			}
			if parent.RaffleID != q.RaffleID || parent.ParentID != "" {
				return nil, ErrConflict
			}
			recipient = parent.UserID
		}
		if _, err := s.market("questions").InsertOne(tx, q); err != nil {
			return nil, err
		}
		if recipient != q.UserID {
			return nil, s.notify(tx, recipient, q.RaffleID, "question", "Bir çekilişte yeni bir soru veya yanıt var.", q.CreatedAt)
		}
		return nil, nil
	})
	return err
}

func (s *Store) ListQuestions(ctx context.Context, raffle, parent string, limit, offset int64) ([]domain.Question, int64, error) {
	return listDocuments[domain.Question](ctx, s.market("questions"), bson.M{"raffle_id": raffle, "parent_id": parent}, bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}, limit, offset)
}

func (s *Store) CreateReview(ctx context.Context, review domain.Review) error {
	_, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if err := s.touchUser(tx, review.UserID); err != nil {
			return nil, err
		}
		raffle, err := s.GetRaffle(tx, review.RaffleID)
		if err != nil {
			return nil, err
		}
		if raffle.SellerID != review.SellerID || raffle.SellerID == review.UserID || raffle.Status != domain.RaffleCompleted {
			return nil, ErrForbidden
		}
		if _, err := s.GetParticipation(tx, review.UserID, review.RaffleID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrForbidden
			}
			return nil, err
		}
		_, err = s.market("reviews").InsertOne(tx, review)
		return nil, err
	})
	return err
}

func (s *Store) ListReviews(ctx context.Context, seller string, limit, offset int64) ([]domain.Review, int64, float64, error) {
	filter := bson.M{"seller_id": seller}
	rows, total, err := listDocuments[domain.Review](ctx, s.market("reviews"), filter, bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}, limit, offset)
	if err != nil || total == 0 {
		return rows, total, 0, err
	}
	cursor, err := s.market("reviews").Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: filter}}, {{Key: "$group", Value: bson.M{"_id": nil, "average": bson.M{"$avg": "$rating"}}}}})
	if err != nil {
		return nil, 0, 0, err
	}
	defer cursor.Close(ctx)
	var stats []struct {
		Average float64 `bson:"average"`
	}
	if err := cursor.All(ctx, &stats); err != nil {
		return nil, 0, 0, err
	}
	if len(stats) == 0 {
		return rows, total, 0, nil
	}
	return rows, total, stats[0].Average, nil
}

func (s *Store) ListNotifications(ctx context.Context, user string, unread bool, limit, offset int64) ([]domain.Notification, int64, error) {
	filter := bson.M{"user_id": user}
	if unread {
		filter["read_at"] = nil
	}
	return listDocuments[domain.Notification](ctx, s.market("notifications"), filter, bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}, limit, offset)
}

func (s *Store) ReadNotification(ctx context.Context, user, id string, now time.Time) error {
	return updateOne(ctx, s.market("notifications"), bson.M{"_id": id, "user_id": user}, bson.M{"$set": bson.M{"read_at": now}})
}

// Retain accounts with financial records until an explicit settlement/closure flow exists.
func (s *Store) checkMarketplaceDeletion(ctx context.Context, user string) error {
	for _, entry := range []struct {
		name   string
		filter bson.M
	}{
		{"raffles", bson.M{"seller_id": user, "$or": bson.A{bson.M{"status": bson.M{"$in": []string{domain.RaffleActive, domain.RaffleCompleted}}}, bson.M{"sold_tickets": bson.M{"$gt": 0}}}}},
		{"ticket_entries", bson.M{"user_id": user}},
	} {
		n, err := s.market(entry.name).CountDocuments(ctx, entry.filter, options.Count().SetLimit(1))
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrAccountInUse
		}
	}
	for _, entry := range []struct{ name, key string }{{"raffles", "seller_id"}, {"photos", "owner_id"}, {"favorites", "user_id"}, {"notifications", "user_id"}} {
		if _, err := s.market(entry.name).DeleteMany(ctx, bson.M{entry.key: user}); err != nil {
			return err
		}
	}
	return nil
}
