package store

import (
	"ceksepet-api/internal/domain"
	"context"
	"errors"
	"time"
)

var (
	ErrInsufficientTickets = errors.New("insufficient tickets")
	ErrRaffleUnavailable   = errors.New("raffle unavailable")
	ErrForbidden           = errors.New("forbidden")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrAccountInUse        = errors.New("account has marketplace records")
)

type RaffleFilter struct {
	Search, CategoryID, BrandID, Sort, Status, SellerID, JoinedBy, FavoritedBy, ExcludeID string
	Limit, Offset                                                                         int64
}

type MarketplaceStore interface {
	SavePhoto(context.Context, domain.Photo) error
	GetPhoto(context.Context, string) (domain.Photo, error)
	CreateRaffle(context.Context, domain.Raffle) error
	GetRaffle(context.Context, string) (domain.Raffle, error)
	ListRaffles(context.Context, RaffleFilter) ([]domain.Raffle, int64, error)
	ModerateRaffle(context.Context, string, string, string, time.Time) (domain.Raffle, error)
	SetFavorite(context.Context, string, string, bool) error
	IsFavorite(context.Context, string, string) (bool, error)
	GetParticipation(context.Context, string, string) (domain.Participation, error)
	ApplyTickets(context.Context, domain.TicketEntry) (domain.TicketEntry, bool, error)
	ListTicketEntries(context.Context, string, int64, int64) ([]domain.TicketEntry, int64, error)
	CreateQuestion(context.Context, domain.Question) error
	GetQuestion(context.Context, string) (domain.Question, error)
	ListQuestions(context.Context, string, string, int64, int64) ([]domain.Question, int64, error)
	CreateReview(context.Context, domain.Review) error
	ListReviews(context.Context, string, int64, int64) ([]domain.Review, int64, float64, error)
	ListNotifications(context.Context, string, bool, int64, int64) ([]domain.Notification, int64, error)
	ReadNotification(context.Context, string, string, time.Time) error
}
