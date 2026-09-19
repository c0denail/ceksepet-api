package store

import (
	"context"
	"errors"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Cancellation and all refunds commit together. Participation also writes the
// raffle document, so a concurrent last-ticket purchase cannot escape a refund.
func (s *Store) CancelRaffle(ctx context.Context, id, reason, actor string, now time.Time) (domain.Raffle, error) {
	value, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		raffle, err := s.GetRaffle(tx, id)
		if err != nil {
			return nil, err
		}
		if raffle.Status == domain.RaffleCancelled {
			return raffle, nil
		}
		if raffle.Status != domain.RafflePending && raffle.Status != domain.RaffleActive {
			return nil, ErrRaffleUnavailable
		}
		if err := s.touchUser(tx, raffle.SellerID); err != nil {
			return nil, err
		}
		if err := updateOne(tx, s.market("raffles"), bson.M{"_id": id, "status": raffle.Status}, bson.M{"$set": bson.M{"status": domain.RaffleCancelled, "cancelled_at": now, "cancellation_reason": reason}}); err != nil {
			return nil, err
		}
		cursor, err := s.market("participations").Find(tx, bson.M{"raffle_id": id})
		if err != nil {
			return nil, err
		}
		defer cursor.Close(tx)
		for cursor.Next(tx) {
			var p domain.Participation
			if err := cursor.Decode(&p); err != nil {
				return nil, err
			}
			refund := p.Tickets - p.RefundedTickets
			if refund <= 0 {
				continue
			}
			var user domain.User
			if err := s.users.FindOneAndUpdate(tx, bson.M{"_id": p.UserID}, bson.M{"$inc": bson.M{"ticket_balance": refund}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&user); err != nil {
				return nil, err
			}
			entry := domain.TicketEntry{ID: security.NewID(), UserID: p.UserID, Key: "raffle-refund:" + id, Kind: "raffle_refund", RaffleID: id, Delta: refund, BalanceAfter: user.TicketBalance, RaffleStatus: domain.RaffleCancelled, Reason: reason, Actor: actor, CreatedAt: now}
			if _, err := s.market("ticket_entries").InsertOne(tx, entry); err != nil {
				return nil, err
			}
			if err := updateOne(tx, s.market("participations"), bson.M{"_id": p.ID}, bson.M{"$set": bson.M{"refunded_tickets": p.Tickets, "updated_at": now}}); err != nil {
				return nil, err
			}
			if err := s.notify(tx, p.UserID, id, "raffle_cancelled", "Çekiliş iptal edildi. Kullandığın biletler cüzdanına iade edildi.", now); err != nil {
				return nil, err
			}
		}
		if err := cursor.Err(); err != nil {
			return nil, err
		}
		if err := s.notify(tx, raffle.SellerID, id, "raffle_cancelled", "Çekilişin iptal edildi: "+reason, now); err != nil {
			return nil, err
		}
		raffle.Status = domain.RaffleCancelled
		raffle.CancelledAt = &now
		raffle.CancellationReason = reason
		return raffle, nil
	})
	if err != nil {
		return domain.Raffle{}, err
	}
	return value.(domain.Raffle), nil
}

func (s *Store) CreateSellerReport(ctx context.Context, report domain.SellerReport) error {
	if report.ReporterID == report.SellerID {
		return ErrForbidden
	}
	_, err := s.transaction(ctx, func(tx context.Context) (any, error) {
		if err := s.touchUser(tx, report.ReporterID); err != nil {
			return nil, err
		}
		if err := s.touchUser(tx, report.SellerID); err != nil {
			return nil, err
		}
		_, err := s.market("seller_reports").InsertOne(tx, report)
		return nil, err
	})
	return err
}

func (s *Store) ListSellerReports(ctx context.Context, status string, limit, offset int64) ([]domain.SellerReport, int64, error) {
	filter := bson.M{}
	if status != "all" {
		filter["status"] = status
	}
	return listDocuments[domain.SellerReport](ctx, s.market("seller_reports"), filter, bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}, limit, offset)
}

func (s *Store) ResolveSellerReport(ctx context.Context, id, status, resolution, actor string, now time.Time) (domain.SellerReport, error) {
	var report domain.SellerReport
	err := s.market("seller_reports").FindOneAndUpdate(ctx, bson.M{"_id": id, "status": "open"}, bson.M{"$set": bson.M{"status": status, "resolution": resolution, "resolved_by": actor, "resolved_at": now}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&report)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if e := s.market("seller_reports").FindOne(ctx, bson.M{"_id": id}).Decode(&report); e != nil {
			return report, mapError(e)
		}
		if report.Status == status && report.Resolution == resolution {
			return report, nil
		}
		return report, ErrConflict
	}
	return report, mapError(err)
}
