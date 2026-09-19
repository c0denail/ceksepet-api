package store

import (
	"context"
	"time"

	"ceksepet-api/internal/domain"
)

type ProfileStore interface {
	CancelRaffle(context.Context, string, string, string, time.Time) (domain.Raffle, error)
	CreateSellerReport(context.Context, domain.SellerReport) error
	ListSellerReports(context.Context, string, int64, int64) ([]domain.SellerReport, int64, error)
	ResolveSellerReport(context.Context, string, string, string, string, time.Time) (domain.SellerReport, error)
}
