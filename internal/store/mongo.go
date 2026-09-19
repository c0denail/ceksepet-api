package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ceksepet-api/internal/domain"
)

const (
	usersCollection         = "users"
	registrationsCollection = "registration_sessions"
	loginsCollection        = "login_sessions"
	accessCollection        = "access_sessions"
	operatorCollection      = "operator_sessions"
	loginAttemptsCollection = "login_attempts"
)

type Store struct {
	client        *mongo.Client
	database      *mongo.Database
	users         *mongo.Collection
	registrations *mongo.Collection
	logins        *mongo.Collection
	access        *mongo.Collection
	operators     *mongo.Collection
	loginAttempts *mongo.Collection
}

type accessDocument struct {
	ID         string     `bson:"_id"`
	UserID     string     `bson:"user_id"`
	TokenHash  string     `bson:"token_hash"`
	CreatedAt  time.Time  `bson:"created_at"`
	LastUsedAt time.Time  `bson:"last_used_at"`
	RevokedAt  *time.Time `bson:"revoked_at,omitempty"`
}

type loginAttemptDocument struct {
	Phone           string     `bson:"_id"`
	FailureCount    int        `bson:"failure_count"`
	WindowStartedAt time.Time  `bson:"window_started_at"`
	LockedUntil     *time.Time `bson:"locked_until,omitempty"`
	CleanupAt       time.Time  `bson:"cleanup_at"`
}

func Open(uri, databaseName string) (*Store, error) {
	if strings.TrimSpace(uri) == "" {
		return nil, errors.New("MONGODB_URI is required")
	}
	if strings.TrimSpace(databaseName) == "" {
		return nil, errors.New("MONGODB_DATABASE is required")
	}

	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	client, err := mongo.Connect(options.Client().
		ApplyURI(uri).
		SetServerAPIOptions(serverAPI).
		SetAppName("ceksepet-api").
		SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect mongodb: %w", err)
	}

	database := client.Database(databaseName)
	dataStore := &Store{
		client:        client,
		database:      database,
		users:         database.Collection(usersCollection),
		registrations: database.Collection(registrationsCollection),
		logins:        database.Collection(loginsCollection),
		access:        database.Collection(accessCollection),
		operators:     database.Collection(operatorCollection),
		loginAttempts: database.Collection(loginAttemptsCollection),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := dataStore.Ping(ctx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}
	if err := dataStore.ensureIndexes(ctx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("create mongodb indexes: %w", err)
	}
	return dataStore, nil
}

func (s *Store) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *Store) Ping(ctx context.Context) error {
	return s.database.RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err()
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	definitions := []struct {
		collection *mongo.Collection
		indexes    []mongo.IndexModel
	}{
		{s.users, []mongo.IndexModel{
			{Keys: bson.D{{Key: "phone", Value: 1}}, Options: options.Index().SetName("uq_users_phone").SetUnique(true)},
			{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetName("uq_users_username").SetUnique(true)},
		}},
		{s.registrations, []mongo.IndexModel{
			{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetName("uq_registration_token").SetUnique(true)},
			{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetName("uq_active_registration_username").SetUnique(true).SetPartialFilterExpression(bson.D{{Key: "username", Value: bson.D{{Key: "$type", Value: "string"}}}})},
			{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetName("ttl_registration_expiry").SetExpireAfterSeconds(0)},
		}},
		{s.logins, []mongo.IndexModel{
			{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetName("uq_login_token").SetUnique(true)},
			{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetName("ttl_login_expiry").SetExpireAfterSeconds(0)},
		}},
		{s.access, []mongo.IndexModel{
			{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetName("uq_access_token").SetUnique(true)},
			{Keys: bson.D{{Key: "user_id", Value: 1}}, Options: options.Index().SetName("idx_access_user")},
		}},
		{s.operators, []mongo.IndexModel{
			{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetName("uq_operator_token").SetUnique(true)},
			{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetName("ttl_operator_expiry").SetExpireAfterSeconds(0)},
		}},
		{s.loginAttempts, []mongo.IndexModel{
			{Keys: bson.D{{Key: "cleanup_at", Value: 1}}, Options: options.Index().SetName("ttl_login_attempt_cleanup").SetExpireAfterSeconds(0)},
		}},
	}
	for _, definition := range definitions {
		if _, err := definition.collection.Indexes().CreateMany(ctx, definition.indexes); err != nil {
			return err
		}
	}
	return s.ensureMarketplaceIndexes(ctx)
}

func (s *Store) CreateRegistration(ctx context.Context, registration domain.Registration) error {
	_, err := s.registrations.InsertOne(ctx, registration)
	return mapError(err)
}

func (s *Store) GetRegistration(ctx context.Context, tokenHash string) (domain.Registration, error) {
	var registration domain.Registration
	err := s.registrations.FindOne(ctx, bson.D{{Key: "token_hash", Value: tokenHash}}).Decode(&registration)
	return registration, mapError(err)
}

func (s *Store) PhoneExists(ctx context.Context, phone string) (bool, error) {
	count, err := s.users.CountDocuments(ctx, bson.D{{Key: "phone", Value: phone}}, options.Count().SetLimit(1))
	return count > 0, err
}

func (s *Store) UsernameTaken(ctx context.Context, username, excludeRegistrationID string, now time.Time) (bool, error) {
	count, err := s.users.CountDocuments(ctx, bson.D{{Key: "username", Value: username}}, options.Count().SetLimit(1))
	if err != nil || count > 0 {
		return count > 0, err
	}
	count, err = s.registrations.CountDocuments(ctx, bson.D{
		{Key: "username", Value: username},
		{Key: "_id", Value: bson.D{{Key: "$ne", Value: excludeRegistrationID}}},
		{Key: "expires_at", Value: bson.D{{Key: "$gt", Value: now}}},
	}, options.Count().SetLimit(1))
	return count > 0, err
}

func (s *Store) SetRegistrationProfileAndOTP(
	ctx context.Context, tokenHash, username, avatarID, otpHash string, sentAt, otpExpiresAt time.Time,
) error {
	return updateOne(ctx, s.registrations,
		bson.D{{Key: "token_hash", Value: tokenHash}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "username", Value: username}, {Key: "avatar_id", Value: avatarID},
			{Key: "current_step", Value: 3}, {Key: "otp_hash", Value: otpHash},
			{Key: "otp_sent_at", Value: sentAt}, {Key: "otp_expires_at", Value: otpExpiresAt},
			{Key: "otp_attempts", Value: 0},
		}}})
}

func (s *Store) SetRegistrationOTP(ctx context.Context, tokenHash, otpHash string, sentAt, expiresAt time.Time) error {
	return updateOne(ctx, s.registrations,
		bson.D{{Key: "token_hash", Value: tokenHash}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "otp_hash", Value: otpHash}, {Key: "otp_sent_at", Value: sentAt},
			{Key: "otp_expires_at", Value: expiresAt}, {Key: "otp_attempts", Value: 0},
		}}})
}

func (s *Store) IncrementRegistrationOTPAttempts(ctx context.Context, tokenHash string) error {
	return updateOne(ctx, s.registrations,
		bson.D{{Key: "token_hash", Value: tokenHash}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "otp_attempts", Value: 1}}}})
}

func (s *Store) CompleteRegistration(
	ctx context.Context, tokenHash, expectedOTPHash, userID string, access domain.AccessSession, now time.Time,
) (domain.User, error) {
	session, err := s.client.StartSession()
	if err != nil {
		return domain.User{}, err
	}
	defer session.EndSession(ctx)

	var created domain.User
	_, err = session.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		var registration domain.Registration
		err := s.registrations.FindOne(txCtx, bson.D{
			{Key: "token_hash", Value: tokenHash}, {Key: "otp_hash", Value: expectedOTPHash},
		}).Decode(&registration)
		if err != nil {
			return nil, mapError(err)
		}
		created = domain.User{
			ID: userID, FirstName: registration.FirstName, LastName: registration.LastName,
			Phone: registration.Phone, Username: registration.Username, AvatarID: registration.AvatarID,
			PINHash: registration.PINHash, CreatedAt: now,
		}
		if _, err := s.users.InsertOne(txCtx, created); err != nil {
			return nil, mapError(err)
		}
		access.UserID = userID
		if err := s.insertAccessSession(txCtx, access); err != nil {
			return nil, err
		}
		result, err := s.registrations.DeleteOne(txCtx, bson.D{{Key: "_id", Value: registration.ID}})
		if err != nil {
			return nil, err
		}
		if result.DeletedCount == 0 {
			return nil, ErrNotFound
		}
		return nil, nil
	})
	return created, mapError(err)
}

func (s *Store) GetUserByPhone(ctx context.Context, phone string) (domain.User, error) {
	var user domain.User
	err := s.users.FindOne(ctx, bson.D{{Key: "phone", Value: phone}}).Decode(&user)
	return user, mapError(err)
}

func (s *Store) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&user)
	return user, mapError(err)
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int64) ([]domain.User, int64, error) {
	total, err := s.users.CountDocuments(ctx, bson.D{})
	if err != nil {
		return nil, 0, err
	}
	cursor, err := s.users.Find(ctx, bson.D{}, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	users := make([]domain.User, 0, limit)
	if err := cursor.All(ctx, &users); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	session, err := s.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		var user domain.User
		if err := s.users.FindOne(txCtx, bson.D{{Key: "_id", Value: userID}}).Decode(&user); err != nil {
			return nil, mapError(err)
		}
		if err := s.checkMarketplaceDeletion(txCtx, userID); err != nil {
			return nil, err
		}
		if _, err := s.access.DeleteMany(txCtx, bson.D{{Key: "user_id", Value: user.ID}}); err != nil {
			return nil, err
		}
		if _, err := s.logins.DeleteMany(txCtx, bson.D{{Key: "user_id", Value: user.ID}}); err != nil {
			return nil, err
		}
		if _, err := s.registrations.DeleteMany(txCtx, bson.D{{Key: "phone", Value: user.Phone}}); err != nil {
			return nil, err
		}
		if _, err := s.loginAttempts.DeleteOne(txCtx, bson.D{{Key: "_id", Value: user.Phone}}); err != nil {
			return nil, err
		}
		result, err := s.users.DeleteOne(txCtx, bson.D{{Key: "_id", Value: user.ID}})
		if err != nil {
			return nil, err
		}
		if result.DeletedCount == 0 {
			return nil, ErrNotFound
		}
		return nil, nil
	})
	return mapError(err)
}

func (s *Store) CreateLoginSession(ctx context.Context, login domain.LoginSession) error {
	_, err := s.logins.InsertOne(ctx, login)
	return mapError(err)
}

func (s *Store) LoginRetryAfter(ctx context.Context, phone string, now time.Time) (int, error) {
	var attempt loginAttemptDocument
	err := s.loginAttempts.FindOne(ctx, bson.D{{Key: "_id", Value: phone}}).Decode(&attempt)
	if errors.Is(err, mongo.ErrNoDocuments) || (err == nil && attempt.LockedUntil == nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	remaining := attempt.LockedUntil.Sub(now)
	if remaining <= 0 {
		return 0, nil
	}
	return max(1, int(math.Ceil(remaining.Seconds()))), nil
}

func (s *Store) RecordLoginFailure(
	ctx context.Context, phone string, now time.Time, maxAttempts int,
	window, lockDuration time.Duration,
) (int, error) {
	windowMillis := window.Milliseconds()
	lockedUntil := now.Add(lockDuration)
	cleanupAt := now.Add(24 * time.Hour)
	resetWindow := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$window_started_at"}}, "missing"}}},
		bson.D{{Key: "$gte", Value: bson.A{bson.D{{Key: "$subtract", Value: bson.A{now, "$window_started_at"}}}, windowMillis}}},
	}}}
	pipeline := mongo.Pipeline{
		{{Key: "$set", Value: bson.D{
			{Key: "window_started_at", Value: bson.D{{Key: "$cond", Value: bson.A{resetWindow, now, "$window_started_at"}}}},
			{Key: "failure_count", Value: bson.D{{Key: "$cond", Value: bson.A{resetWindow, 1, bson.D{{Key: "$add", Value: bson.A{bson.D{{Key: "$ifNull", Value: bson.A{"$failure_count", 0}}}, 1}}}}}}},
			{Key: "cleanup_at", Value: cleanupAt},
		}}},
		{{Key: "$set", Value: bson.D{
			{Key: "locked_until", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$gte", Value: bson.A{"$failure_count", maxAttempts}}}, lockedUntil, "$locked_until",
			}}}},
		}}},
	}
	var attempt loginAttemptDocument
	err := s.loginAttempts.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: phone}}, pipeline,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&attempt)
	if err != nil {
		return 0, err
	}
	if attempt.FailureCount >= maxAttempts && attempt.LockedUntil != nil {
		return max(1, int(math.Ceil(attempt.LockedUntil.Sub(now).Seconds()))), nil
	}
	return 0, nil
}

func (s *Store) ClearLoginFailures(ctx context.Context, phone string) error {
	_, err := s.loginAttempts.DeleteOne(ctx, bson.D{{Key: "_id", Value: phone}})
	return err
}

func (s *Store) GetLoginSession(ctx context.Context, tokenHash string) (domain.LoginSession, error) {
	var login domain.LoginSession
	err := s.logins.FindOne(ctx, bson.D{{Key: "token_hash", Value: tokenHash}}).Decode(&login)
	return login, mapError(err)
}

func (s *Store) SetLoginOTP(ctx context.Context, tokenHash, otpHash string, sentAt, expiresAt time.Time) error {
	return updateOne(ctx, s.logins,
		bson.D{{Key: "token_hash", Value: tokenHash}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "otp_hash", Value: otpHash}, {Key: "otp_sent_at", Value: sentAt},
			{Key: "otp_expires_at", Value: expiresAt}, {Key: "otp_attempts", Value: 0},
		}}})
}

func (s *Store) IncrementLoginOTPAttempts(ctx context.Context, tokenHash string) error {
	return updateOne(ctx, s.logins,
		bson.D{{Key: "token_hash", Value: tokenHash}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "otp_attempts", Value: 1}}}})
}

func (s *Store) CompleteLogin(
	ctx context.Context, tokenHash, expectedOTPHash string, access domain.AccessSession,
) error {
	session, err := s.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		var login domain.LoginSession
		err := s.logins.FindOne(txCtx, bson.D{
			{Key: "token_hash", Value: tokenHash}, {Key: "otp_hash", Value: expectedOTPHash},
		}).Decode(&login)
		if err != nil {
			return nil, mapError(err)
		}
		access.UserID = login.UserID
		if err := s.insertAccessSession(txCtx, access); err != nil {
			return nil, err
		}
		result, err := s.logins.DeleteOne(txCtx, bson.D{{Key: "_id", Value: login.ID}})
		if err != nil {
			return nil, err
		}
		if result.DeletedCount == 0 {
			return nil, ErrNotFound
		}
		return nil, nil
	})
	return mapError(err)
}

func (s *Store) ResolveAccessToken(ctx context.Context, tokenHash string, now time.Time) (string, error) {
	var session accessDocument
	err := s.access.FindOneAndUpdate(ctx,
		bson.D{{Key: "token_hash", Value: tokenHash}, {Key: "revoked_at", Value: bson.D{{Key: "$exists", Value: false}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "last_used_at", Value: now}}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&session)
	if err != nil {
		return "", mapError(err)
	}
	return session.UserID, nil
}

func (s *Store) RevokeAccessToken(ctx context.Context, tokenHash string, now time.Time) error {
	return updateOne(ctx, s.access,
		bson.D{{Key: "token_hash", Value: tokenHash}, {Key: "revoked_at", Value: bson.D{{Key: "$exists", Value: false}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "revoked_at", Value: now}}}})
}

func (s *Store) CreateOperatorSession(ctx context.Context, session domain.OperatorSession) error {
	_, err := s.operators.InsertOne(ctx, session)
	return mapError(err)
}

func (s *Store) ResolveOperatorToken(ctx context.Context, tokenHash string, now time.Time) (string, error) {
	var session domain.OperatorSession
	err := s.operators.FindOneAndUpdate(ctx, bson.D{
		{Key: "token_hash", Value: tokenHash},
		{Key: "expires_at", Value: bson.D{{Key: "$gt", Value: now}}},
		{Key: "revoked_at", Value: bson.D{{Key: "$exists", Value: false}}},
	}, bson.D{{Key: "$set", Value: bson.D{{Key: "last_used_at", Value: now}}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&session)
	if err != nil {
		return "", mapError(err)
	}
	return session.Username, nil
}

func (s *Store) RevokeOperatorToken(ctx context.Context, tokenHash string, now time.Time) error {
	return updateOne(ctx, s.operators,
		bson.D{{Key: "token_hash", Value: tokenHash}, {Key: "revoked_at", Value: bson.D{{Key: "$exists", Value: false}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "revoked_at", Value: now}}}})
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := s.registrations.DeleteMany(ctx, bson.D{{Key: "expires_at", Value: bson.D{{Key: "$lte", Value: now}}}}); err != nil {
		return err
	}
	if _, err := s.logins.DeleteMany(ctx, bson.D{{Key: "expires_at", Value: bson.D{{Key: "$lte", Value: now}}}}); err != nil {
		return err
	}
	if _, err := s.operators.DeleteMany(ctx, bson.D{{Key: "expires_at", Value: bson.D{{Key: "$lte", Value: now}}}}); err != nil {
		return err
	}
	_, err := s.loginAttempts.DeleteMany(ctx, bson.D{{Key: "cleanup_at", Value: bson.D{{Key: "$lte", Value: now}}}})
	return err
}

func (s *Store) insertAccessSession(ctx context.Context, access domain.AccessSession) error {
	document := accessDocument{
		ID: access.ID, UserID: access.UserID, TokenHash: access.TokenHash,
		CreatedAt: access.CreatedAt, LastUsedAt: access.CreatedAt,
	}
	_, err := s.access.InsertOne(ctx, document)
	return mapError(err)
}

func updateOne(ctx context.Context, collection *mongo.Collection, filter, update any) error {
	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return mapError(err)
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	if mongo.IsDuplicateKeyError(err) {
		return ErrConflict
	}
	return err
}

var _ DataStore = (*Store)(nil)
