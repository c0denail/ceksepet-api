package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"ceksepet-api/internal/domain"
)

func openIntegrationStore(t *testing.T) *Store {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI is not set; skipping MongoDB integration test")
	}
	databaseName := fmt.Sprintf("ceksepet_test_%d", time.Now().UTC().UnixNano())
	dataStore, err := Open(uri, databaseName)
	if err != nil {
		t.Fatalf("open MongoDB store: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := dataStore.database.Drop(ctx); err != nil {
			t.Errorf("drop test database %q: %v", databaseName, err)
		}
		if err := dataStore.Close(); err != nil {
			t.Errorf("close MongoDB store: %v", err)
		}
	})
	return dataStore
}

func TestMongoStoreCreatesIndexesAndPurgesExpiredSessions(t *testing.T) {
	dataStore := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UTC()
	registration := domain.Registration{
		ID: "expired-registration", TokenHash: "expired-token", FirstName: "A", LastName: "B",
		Phone: "+905551234567", PINHash: "hash", CurrentStep: 2,
		ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour),
	}
	if err := dataStore.CreateRegistration(ctx, registration); err != nil {
		t.Fatalf("create expired registration: %v", err)
	}
	if err := dataStore.PurgeExpiredSessions(ctx, now); err != nil {
		t.Fatalf("purge sessions: %v", err)
	}
	_, err := dataStore.GetRegistration(ctx, registration.TokenHash)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired registration was not deleted: %v", err)
	}

	indexNames, err := dataStore.users.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list users indexes: %v", err)
	}
	defer indexNames.Close(ctx)
	foundPhoneIndex := false
	for indexNames.Next(ctx) {
		var index struct {
			Name string `bson:"name"`
		}
		if err := indexNames.Decode(&index); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		if index.Name == "uq_users_phone" {
			foundPhoneIndex = true
		}
	}
	if err := indexNames.Err(); err != nil {
		t.Fatalf("iterate indexes: %v", err)
	}
	if !foundPhoneIndex {
		t.Fatal("unique users.phone index was not created")
	}
}

func TestMongoStoreRegistrationLoginAndTokenTransactions(t *testing.T) {
	dataStore := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC()
	otpExpiry := now.Add(4 * time.Minute)
	otpSent := now
	registration := domain.Registration{
		ID: "registration-1", TokenHash: "registration-token-hash", FirstName: "Test", LastName: "User",
		Phone: "+905551110001", PINHash: "bcrypt-pin-hash", Username: "test_user@10001", AvatarID: "avatar_01",
		CurrentStep: 3, OTPHash: "registration-otp-hash", OTPExpiresAt: &otpExpiry, OTPSentAt: &otpSent,
		ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now,
	}
	if err := dataStore.CreateRegistration(ctx, registration); err != nil {
		t.Fatalf("create registration: %v", err)
	}
	registrationAccess := domain.AccessSession{
		ID: "access-1", TokenHash: "access-token-hash-1", CreatedAt: now,
	}
	user, err := dataStore.CompleteRegistration(ctx, registration.TokenHash, registration.OTPHash,
		"user-1", registrationAccess, now)
	if err != nil {
		t.Fatalf("complete registration transaction: %v", err)
	}
	if user.Phone != registration.Phone || user.Username != registration.Username {
		t.Fatalf("unexpected registered user: %+v", user)
	}
	listedUsers, total, err := dataStore.ListUsers(ctx, 20, 0)
	if err != nil || total != 1 || len(listedUsers) != 1 || listedUsers[0].ID != user.ID {
		t.Fatalf("list users: total=%d users=%+v error=%v", total, listedUsers, err)
	}
	if _, err := dataStore.GetRegistration(ctx, registration.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed registration session still exists: %v", err)
	}
	resolvedUserID, err := dataStore.ResolveAccessToken(ctx, registrationAccess.TokenHash, now.Add(time.Second))
	if err != nil || resolvedUserID != user.ID {
		t.Fatalf("resolve registration access token: user=%q error=%v", resolvedUserID, err)
	}

	login := domain.LoginSession{
		ID: "login-1", TokenHash: "login-token-hash", UserID: user.ID, Phone: user.Phone,
		OTPHash: "login-otp-hash", OTPExpiresAt: otpExpiry, OTPSentAt: now,
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now,
	}
	if err := dataStore.CreateLoginSession(ctx, login); err != nil {
		t.Fatalf("create login session: %v", err)
	}
	loginAccess := domain.AccessSession{ID: "access-2", TokenHash: "access-token-hash-2", CreatedAt: now}
	if err := dataStore.CompleteLogin(ctx, login.TokenHash, login.OTPHash, loginAccess); err != nil {
		t.Fatalf("complete login transaction: %v", err)
	}
	resolvedUserID, err = dataStore.ResolveAccessToken(ctx, loginAccess.TokenHash, now.Add(2*time.Second))
	if err != nil || resolvedUserID != user.ID {
		t.Fatalf("resolve login access token: user=%q error=%v", resolvedUserID, err)
	}
	if err := dataStore.RevokeAccessToken(ctx, loginAccess.TokenHash, now.Add(3*time.Second)); err != nil {
		t.Fatalf("revoke access token: %v", err)
	}
	if _, err := dataStore.ResolveAccessToken(ctx, loginAccess.TokenHash, now.Add(4*time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked access token still resolves: %v", err)
	}

	for attempt := 1; attempt <= 5; attempt++ {
		retryAfter, err := dataStore.RecordLoginFailure(ctx, user.Phone, now, 5, 15*time.Minute, 15*time.Minute)
		if err != nil {
			t.Fatalf("record login failure %d: %v", attempt, err)
		}
		if attempt < 5 && retryAfter != 0 {
			t.Fatalf("attempt %d locked too early: retryAfter=%d", attempt, retryAfter)
		}
		if attempt == 5 && retryAfter <= 0 {
			t.Fatal("fifth failed PIN attempt did not lock login")
		}
	}
	pendingRegistration := domain.Registration{
		ID: "registration-2", TokenHash: "pending-registration-token", FirstName: user.FirstName,
		LastName: user.LastName, Phone: user.Phone, PINHash: user.PINHash, CurrentStep: 2,
		ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now,
	}
	if err := dataStore.CreateRegistration(ctx, pendingRegistration); err != nil {
		t.Fatalf("create pending registration before deletion: %v", err)
	}
	pendingLogin := domain.LoginSession{
		ID: "login-2", TokenHash: "pending-login-token", UserID: user.ID, Phone: user.Phone,
		OTPHash: "pending-login-otp", OTPExpiresAt: otpExpiry, OTPSentAt: now,
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now,
	}
	if err := dataStore.CreateLoginSession(ctx, pendingLogin); err != nil {
		t.Fatalf("create pending login before deletion: %v", err)
	}

	if err := dataStore.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("delete user transaction: %v", err)
	}
	if _, err := dataStore.GetUserByID(ctx, user.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user still exists: %v", err)
	}
	if _, err := dataStore.ResolveAccessToken(ctx, registrationAccess.TokenHash, now.Add(5*time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user's access session still exists: %v", err)
	}
	if _, err := dataStore.GetRegistration(ctx, pendingRegistration.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user's pending registration still exists: %v", err)
	}
	if _, err := dataStore.GetLoginSession(ctx, pendingLogin.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user's pending login still exists: %v", err)
	}
	if retryAfter, err := dataStore.LoginRetryAfter(ctx, user.Phone, now); err != nil || retryAfter != 0 {
		t.Fatalf("deleted user's PIN attempt state still exists: retryAfter=%d error=%v", retryAfter, err)
	}
	exists, err := dataStore.PhoneExists(ctx, user.Phone)
	if err != nil || exists {
		t.Fatalf("deleted user's phone still exists: exists=%v error=%v", exists, err)
	}
	listedUsers, total, err = dataStore.ListUsers(ctx, 20, 0)
	if err != nil || total != 0 || len(listedUsers) != 0 {
		t.Fatalf("deleted user still appears in list: total=%d users=%+v error=%v", total, listedUsers, err)
	}
}

func TestMongoStoreOperatorSessionLifecycle(t *testing.T) {
	dataStore := openIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC()
	session := domain.OperatorSession{
		ID: "operator-session-1", Username: "operator", TokenHash: "operator-token-hash",
		CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(8 * time.Hour),
	}
	if err := dataStore.CreateOperatorSession(ctx, session); err != nil {
		t.Fatalf("create operator session: %v", err)
	}
	username, err := dataStore.ResolveOperatorToken(ctx, session.TokenHash, now.Add(time.Minute))
	if err != nil || username != session.Username {
		t.Fatalf("resolve operator token: username=%q error=%v", username, err)
	}
	if err := dataStore.RevokeOperatorToken(ctx, session.TokenHash, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("revoke operator token: %v", err)
	}
	if _, err := dataStore.ResolveOperatorToken(ctx, session.TokenHash, now.Add(3*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked operator token still resolves: %v", err)
	}

	expired := domain.OperatorSession{
		ID: "operator-session-2", Username: "operator", TokenHash: "expired-operator-token",
		CreatedAt: now.Add(-9 * time.Hour), LastUsedAt: now.Add(-9 * time.Hour), ExpiresAt: now.Add(-time.Hour),
	}
	if err := dataStore.CreateOperatorSession(ctx, expired); err != nil {
		t.Fatalf("create expired operator session: %v", err)
	}
	if err := dataStore.PurgeExpiredSessions(ctx, now); err != nil {
		t.Fatalf("purge expired operator session: %v", err)
	}
	if _, err := dataStore.ResolveOperatorToken(ctx, expired.TokenHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired operator token still resolves: %v", err)
	}
}
