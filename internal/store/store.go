package store

import (
	"context"
	"errors"
	"time"

	"ceksepet-api/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// DataStore is the persistence contract used by the HTTP layer. The production
// implementation is MongoDB; keeping the contract explicit also lets tests run
// without requiring a networked database.
type DataStore interface {
	MarketplaceStore
	ProfileStore
	Ping(context.Context) error
	CreateRegistration(context.Context, domain.Registration) error
	GetRegistration(context.Context, string) (domain.Registration, error)
	PhoneExists(context.Context, string) (bool, error)
	UsernameTaken(context.Context, string, string, time.Time) (bool, error)
	SetRegistrationProfileAndOTP(context.Context, string, string, string, string, time.Time, time.Time) error
	SetRegistrationOTP(context.Context, string, string, time.Time, time.Time) error
	IncrementRegistrationOTPAttempts(context.Context, string) error
	CompleteRegistration(context.Context, string, string, string, domain.AccessSession, time.Time) (domain.User, error)
	GetUserByPhone(context.Context, string) (domain.User, error)
	GetUserByID(context.Context, string) (domain.User, error)
	ListUsers(context.Context, int64, int64) ([]domain.User, int64, error)
	DeleteUser(context.Context, string) error
	CreateLoginSession(context.Context, domain.LoginSession) error
	LoginRetryAfter(context.Context, string, time.Time) (int, error)
	RecordLoginFailure(context.Context, string, time.Time, int, time.Duration, time.Duration) (int, error)
	ClearLoginFailures(context.Context, string) error
	GetLoginSession(context.Context, string) (domain.LoginSession, error)
	SetLoginOTP(context.Context, string, string, time.Time, time.Time) error
	IncrementLoginOTPAttempts(context.Context, string) error
	CompleteLogin(context.Context, string, string, domain.AccessSession) error
	ResolveAccessToken(context.Context, string, time.Time) (string, error)
	RevokeAccessToken(context.Context, string, time.Time) error
	CreateOperatorSession(context.Context, domain.OperatorSession) error
	ResolveOperatorToken(context.Context, string, time.Time) (string, error)
	RevokeOperatorToken(context.Context, string, time.Time) error
	PurgeExpiredSessions(context.Context, time.Time) error
}
