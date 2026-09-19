package domain

import "time"

type User struct {
	ID            string    `bson:"_id"`
	FirstName     string    `bson:"first_name"`
	LastName      string    `bson:"last_name"`
	Phone         string    `bson:"phone"`
	Username      string    `bson:"username"`
	AvatarID      string    `bson:"avatar_id"`
	PINHash       string    `bson:"pin_hash"`
	CreatedAt     time.Time `bson:"created_at"`
	TicketBalance int64     `bson:"ticket_balance"`
}

type Registration struct {
	ID           string     `bson:"_id"`
	TokenHash    string     `bson:"token_hash"`
	FirstName    string     `bson:"first_name"`
	LastName     string     `bson:"last_name"`
	Phone        string     `bson:"phone"`
	PINHash      string     `bson:"pin_hash"`
	Username     string     `bson:"username,omitempty"`
	AvatarID     string     `bson:"avatar_id,omitempty"`
	CurrentStep  int        `bson:"current_step"`
	OTPHash      string     `bson:"otp_hash,omitempty"`
	OTPExpiresAt *time.Time `bson:"otp_expires_at,omitempty"`
	OTPSentAt    *time.Time `bson:"otp_sent_at,omitempty"`
	OTPAttempts  int        `bson:"otp_attempts"`
	ExpiresAt    time.Time  `bson:"expires_at"`
	CreatedAt    time.Time  `bson:"created_at"`
}

type LoginSession struct {
	ID           string    `bson:"_id"`
	TokenHash    string    `bson:"token_hash"`
	UserID       string    `bson:"user_id"`
	Phone        string    `bson:"phone"`
	OTPHash      string    `bson:"otp_hash"`
	OTPExpiresAt time.Time `bson:"otp_expires_at"`
	OTPSentAt    time.Time `bson:"otp_sent_at"`
	OTPAttempts  int       `bson:"otp_attempts"`
	ExpiresAt    time.Time `bson:"expires_at"`
	CreatedAt    time.Time `bson:"created_at"`
}

type AccessSession struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"user_id"`
	TokenHash string    `bson:"token_hash"`
	CreatedAt time.Time `bson:"created_at"`
}

type OperatorSession struct {
	ID         string     `bson:"_id"`
	Username   string     `bson:"username"`
	TokenHash  string     `bson:"token_hash"`
	CreatedAt  time.Time  `bson:"created_at"`
	LastUsedAt time.Time  `bson:"last_used_at"`
	ExpiresAt  time.Time  `bson:"expires_at"`
	RevokedAt  *time.Time `bson:"revoked_at,omitempty"`
}
