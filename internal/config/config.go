package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Environment        string
	HTTPAddr           string
	MongoURI           string
	MongoDatabase      string
	PublicBaseURL      string
	TokenSecret        string
	OTPSecret          string
	OTPWebhookURL      string
	OTPWebhookToken    string
	OperatorUsername   string
	OperatorPassword   string
	OperatorTokenTTL   time.Duration
	RegistrationTTL    time.Duration
	LoginTTL           time.Duration
	OTPTTL             time.Duration
	OTPRetryAfter      time.Duration
	PINAttemptWindow   time.Duration
	PINLockDuration    time.Duration
	PINMaxAttempts     int
	CORSAllowedOrigins []string
	TicketValueKurus   int64
	CommissionBPS      int64
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	cfg := Config{
		Environment:        getenv("APP_ENV", "development"),
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		MongoURI:           strings.TrimSpace(os.Getenv("MONGODB_URI")),
		MongoDatabase:      getenv("MONGODB_DATABASE", "ceksepet"),
		PublicBaseURL:      strings.TrimRight(getenv("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		TokenSecret:        getenv("TOKEN_SECRET", "development-only-token-secret-change-me"),
		OTPSecret:          getenv("OTP_SECRET", "development-only-otp-secret-change-me"),
		OTPWebhookURL:      strings.TrimSpace(os.Getenv("OTP_WEBHOOK_URL")),
		OTPWebhookToken:    strings.TrimSpace(os.Getenv("OTP_WEBHOOK_TOKEN")),
		OperatorUsername:   strings.TrimSpace(os.Getenv("OPERATOR_USERNAME")),
		OperatorPassword:   os.Getenv("OPERATOR_PASSWORD"),
		CORSAllowedOrigins: splitCSV(getenv("CORS_ALLOWED_ORIGINS", "*")),
		PINMaxAttempts:     5,
	}

	var err error
	if cfg.TicketValueKurus, err = integer("TICKET_VALUE_KURUS", 100, 1, 1_000_000); err != nil {
		return Config{}, err
	}
	if cfg.CommissionBPS, err = integer("COMMISSION_BPS", 1000, 0, 10_000); err != nil {
		return Config{}, err
	}
	if cfg.RegistrationTTL, err = duration("REGISTRATION_TTL", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.LoginTTL, err = duration("LOGIN_TTL", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.OTPTTL, err = duration("OTP_TTL", 4*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.OTPRetryAfter, err = duration("OTP_RETRY_AFTER", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.PINAttemptWindow, err = duration("PIN_ATTEMPT_WINDOW", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.PINLockDuration, err = duration("PIN_LOCK_DURATION", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.OperatorTokenTTL, err = duration("OPERATOR_TOKEN_TTL", 8*time.Hour); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("PIN_MAX_ATTEMPTS")); raw != "" {
		cfg.PINMaxAttempts, err = strconv.Atoi(raw)
		if err != nil || cfg.PINMaxAttempts < 1 {
			return Config{}, errors.New("PIN_MAX_ATTEMPTS must be a positive integer")
		}
	}
	if (cfg.OperatorUsername == "") != (cfg.OperatorPassword == "") {
		return Config{}, errors.New("OPERATOR_USERNAME and OPERATOR_PASSWORD must be configured together")
	}

	if cfg.Environment == "production" {
		if cfg.MongoURI == "" {
			return Config{}, errors.New("MONGODB_URI is required in production")
		}
		if len(cfg.TokenSecret) < 32 || strings.Contains(cfg.TokenSecret, "change-me") {
			return Config{}, errors.New("TOKEN_SECRET must be at least 32 characters in production")
		}
		if len(cfg.OTPSecret) < 32 || strings.Contains(cfg.OTPSecret, "change-me") {
			return Config{}, errors.New("OTP_SECRET must be at least 32 characters in production")
		}
		if !strings.HasPrefix(cfg.OTPWebhookURL, "https://") {
			return Config{}, errors.New("OTP_WEBHOOK_URL must be an HTTPS URL in production")
		}
		if cfg.OperatorUsername != "" && len(cfg.OperatorPassword) < 32 {
			return Config{}, errors.New("OPERATOR_PASSWORD must be at least 32 characters in production")
		}
	}
	if cfg.MongoURI == "" {
		return Config{}, errors.New("MONGODB_URI is required")
	}

	return cfg, nil
}

func loadDotEnv() error {
	if explicit := strings.TrimSpace(os.Getenv("ENV_FILE")); explicit != "" {
		return godotenv.Load(explicit)
	}
	directory, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		path := filepath.Join(directory, ".env")
		if _, err := os.Stat(path); err == nil {
			return godotenv.Load(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return nil
		}
		directory = parent
	}
}

func (c Config) OperatorEnabled() bool {
	return c.OperatorUsername != "" && c.OperatorPassword != ""
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func integer(key string, fallback, minimum, maximum int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
	}
	return value, nil
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", key)
	}
	return d, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
