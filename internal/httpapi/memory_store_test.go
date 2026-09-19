package httpapi

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/store"
)

type memoryAccess struct {
	session   domain.AccessSession
	lastUsed  time.Time
	revokedAt *time.Time
}

type memoryAttempt struct {
	count         int
	windowStarted time.Time
	lockedUntil   *time.Time
}

type memoryStore struct {
	memoryMarket
	mu            sync.Mutex
	registrations map[string]domain.Registration
	users         map[string]domain.User
	userByPhone   map[string]string
	logins        map[string]domain.LoginSession
	access        map[string]memoryAccess
	operators     map[string]domain.OperatorSession
	attempts      map[string]memoryAttempt
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		memoryMarket:  newMemoryMarket(),
		registrations: make(map[string]domain.Registration),
		users:         make(map[string]domain.User),
		userByPhone:   make(map[string]string),
		logins:        make(map[string]domain.LoginSession),
		access:        make(map[string]memoryAccess),
		operators:     make(map[string]domain.OperatorSession),
		attempts:      make(map[string]memoryAttempt),
	}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) CreateRegistration(_ context.Context, registration domain.Registration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.registrations[registration.TokenHash]; exists {
		return store.ErrConflict
	}
	s.registrations[registration.TokenHash] = registration
	return nil
}

func (s *memoryStore) GetRegistration(_ context.Context, tokenHash string) (domain.Registration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.registrations[tokenHash]
	if !ok {
		return domain.Registration{}, store.ErrNotFound
	}
	return registration, nil
}

func (s *memoryStore) PhoneExists(_ context.Context, phone string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.userByPhone[phone]
	return exists, nil
}

func (s *memoryStore) UsernameTaken(_ context.Context, username, excludedID string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.users {
		if user.Username == username {
			return true, nil
		}
	}
	for _, registration := range s.registrations {
		if registration.ID != excludedID && registration.Username == username && registration.ExpiresAt.After(now) {
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryStore) SetRegistrationProfileAndOTP(
	_ context.Context, tokenHash, username, avatarID, otpHash string, sentAt, otpExpiresAt time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.registrations[tokenHash]
	if !ok {
		return store.ErrNotFound
	}
	registration.Username = username
	registration.AvatarID = avatarID
	registration.CurrentStep = 3
	registration.OTPHash = otpHash
	registration.OTPSentAt = &sentAt
	registration.OTPExpiresAt = &otpExpiresAt
	registration.OTPAttempts = 0
	s.registrations[tokenHash] = registration
	return nil
}

func (s *memoryStore) SetRegistrationOTP(
	_ context.Context, tokenHash, otpHash string, sentAt, expiresAt time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.registrations[tokenHash]
	if !ok {
		return store.ErrNotFound
	}
	registration.OTPHash = otpHash
	registration.OTPSentAt = &sentAt
	registration.OTPExpiresAt = &expiresAt
	registration.OTPAttempts = 0
	s.registrations[tokenHash] = registration
	return nil
}

func (s *memoryStore) IncrementRegistrationOTPAttempts(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.registrations[tokenHash]
	if !ok {
		return store.ErrNotFound
	}
	registration.OTPAttempts++
	s.registrations[tokenHash] = registration
	return nil
}

func (s *memoryStore) CompleteRegistration(
	_ context.Context, tokenHash, expectedOTPHash, userID string, access domain.AccessSession, now time.Time,
) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	registration, ok := s.registrations[tokenHash]
	if !ok || registration.OTPHash != expectedOTPHash {
		return domain.User{}, store.ErrNotFound
	}
	if _, exists := s.userByPhone[registration.Phone]; exists {
		return domain.User{}, store.ErrConflict
	}
	for _, existing := range s.users {
		if existing.Username == registration.Username {
			return domain.User{}, store.ErrConflict
		}
	}
	user := domain.User{
		ID: userID, FirstName: registration.FirstName, LastName: registration.LastName,
		Phone: registration.Phone, Username: registration.Username, AvatarID: registration.AvatarID,
		PINHash: registration.PINHash, CreatedAt: now,
	}
	s.users[user.ID] = user
	s.userByPhone[user.Phone] = user.ID
	access.UserID = user.ID
	s.access[access.TokenHash] = memoryAccess{session: access, lastUsed: access.CreatedAt}
	delete(s.registrations, tokenHash)
	return user, nil
}

func (s *memoryStore) GetUserByPhone(_ context.Context, phone string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.userByPhone[phone]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	return s.users[id], nil
}

func (s *memoryStore) GetUserByID(_ context.Context, id string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	return user, nil
}

func (s *memoryStore) ListUsers(_ context.Context, limit, offset int64) ([]domain.User, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]domain.User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].CreatedAt.Equal(users[j].CreatedAt) {
			return users[i].ID < users[j].ID
		}
		return users[i].CreatedAt.After(users[j].CreatedAt)
	})
	total := int64(len(users))
	if offset >= total {
		return []domain.User{}, total, nil
	}
	end := min(offset+limit, total)
	return users[int(offset):int(end)], total, nil
}

func (s *memoryStore) DeleteUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkMarketDelete(userID); err != nil {
		return err
	}
	user, ok := s.users[userID]
	if !ok {
		return store.ErrNotFound
	}
	delete(s.users, userID)
	delete(s.userByPhone, user.Phone)
	delete(s.attempts, user.Phone)
	for tokenHash, registration := range s.registrations {
		if registration.Phone == user.Phone {
			delete(s.registrations, tokenHash)
		}
	}
	for tokenHash, login := range s.logins {
		if login.UserID == user.ID {
			delete(s.logins, tokenHash)
		}
	}
	for tokenHash, access := range s.access {
		if access.session.UserID == user.ID {
			delete(s.access, tokenHash)
		}
	}
	return nil
}

func (s *memoryStore) CreateLoginSession(_ context.Context, login domain.LoginSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.logins[login.TokenHash]; exists {
		return store.ErrConflict
	}
	s.logins[login.TokenHash] = login
	return nil
}

func (s *memoryStore) LoginRetryAfter(_ context.Context, phone string, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[phone]
	if !ok || attempt.lockedUntil == nil || !attempt.lockedUntil.After(now) {
		return 0, nil
	}
	return max(1, int(math.Ceil(attempt.lockedUntil.Sub(now).Seconds()))), nil
}

func (s *memoryStore) RecordLoginFailure(
	_ context.Context, phone string, now time.Time, maxAttempts int,
	window, lockDuration time.Duration,
) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, exists := s.attempts[phone]
	if !exists || now.Sub(attempt.windowStarted) >= window {
		attempt = memoryAttempt{count: 1, windowStarted: now}
	} else {
		attempt.count++
	}
	if attempt.count >= maxAttempts {
		lockedUntil := now.Add(lockDuration)
		attempt.lockedUntil = &lockedUntil
		s.attempts[phone] = attempt
		return max(1, int(math.Ceil(lockDuration.Seconds()))), nil
	}
	s.attempts[phone] = attempt
	return 0, nil
}

func (s *memoryStore) ClearLoginFailures(_ context.Context, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, phone)
	return nil
}

func (s *memoryStore) GetLoginSession(_ context.Context, tokenHash string) (domain.LoginSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	login, ok := s.logins[tokenHash]
	if !ok {
		return domain.LoginSession{}, store.ErrNotFound
	}
	return login, nil
}

func (s *memoryStore) SetLoginOTP(_ context.Context, tokenHash, otpHash string, sentAt, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	login, ok := s.logins[tokenHash]
	if !ok {
		return store.ErrNotFound
	}
	login.OTPHash = otpHash
	login.OTPSentAt = sentAt
	login.OTPExpiresAt = expiresAt
	login.OTPAttempts = 0
	s.logins[tokenHash] = login
	return nil
}

func (s *memoryStore) IncrementLoginOTPAttempts(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	login, ok := s.logins[tokenHash]
	if !ok {
		return store.ErrNotFound
	}
	login.OTPAttempts++
	s.logins[tokenHash] = login
	return nil
}

func (s *memoryStore) CompleteLogin(
	_ context.Context, tokenHash, expectedOTPHash string, access domain.AccessSession,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	login, ok := s.logins[tokenHash]
	if !ok || login.OTPHash != expectedOTPHash {
		return store.ErrNotFound
	}
	access.UserID = login.UserID
	s.access[access.TokenHash] = memoryAccess{session: access, lastUsed: access.CreatedAt}
	delete(s.logins, tokenHash)
	return nil
}

func (s *memoryStore) ResolveAccessToken(_ context.Context, tokenHash string, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	access, ok := s.access[tokenHash]
	if !ok || access.revokedAt != nil {
		return "", store.ErrNotFound
	}
	access.lastUsed = now
	s.access[tokenHash] = access
	return access.session.UserID, nil
}

func (s *memoryStore) RevokeAccessToken(_ context.Context, tokenHash string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	access, ok := s.access[tokenHash]
	if !ok || access.revokedAt != nil {
		return store.ErrNotFound
	}
	access.revokedAt = &now
	s.access[tokenHash] = access
	return nil
}

func (s *memoryStore) CreateOperatorSession(_ context.Context, session domain.OperatorSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.operators[session.TokenHash]; exists {
		return store.ErrConflict
	}
	s.operators[session.TokenHash] = session
	return nil
}

func (s *memoryStore) ResolveOperatorToken(_ context.Context, tokenHash string, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.operators[tokenHash]
	if !ok || session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return "", store.ErrNotFound
	}
	session.LastUsedAt = now
	s.operators[tokenHash] = session
	return session.Username, nil
}

func (s *memoryStore) RevokeOperatorToken(_ context.Context, tokenHash string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.operators[tokenHash]
	if !ok || session.RevokedAt != nil {
		return store.ErrNotFound
	}
	session.RevokedAt = &now
	s.operators[tokenHash] = session
	return nil
}

func (s *memoryStore) PurgeExpiredSessions(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, registration := range s.registrations {
		if !registration.ExpiresAt.After(now) {
			delete(s.registrations, token)
		}
	}
	for token, login := range s.logins {
		if !login.ExpiresAt.After(now) {
			delete(s.logins, token)
		}
	}
	for phone, attempt := range s.attempts {
		if now.Sub(attempt.windowStarted) >= 24*time.Hour || (attempt.lockedUntil != nil && !attempt.lockedUntil.After(now)) {
			delete(s.attempts, phone)
		}
	}
	for tokenHash, session := range s.operators {
		if !session.ExpiresAt.After(now) {
			delete(s.operators, tokenHash)
		}
	}
	return nil
}

var _ store.DataStore = (*memoryStore)(nil)
