package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"ceksepet-api/internal/config"
	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

type OTPNotifier interface {
	Send(ctx context.Context, channel, destination, code string) error
}

type DevelopmentNotifier struct {
	Logger *slog.Logger
}

func (n DevelopmentNotifier) Send(_ context.Context, channel, destination, code string) error {
	n.Logger.Info("development OTP generated", "channel", channel, "destination", maskPhone(destination), "code", code)
	return nil
}

type Server struct {
	cfg      config.Config
	store    store.DataStore
	notifier OTPNotifier
	logger   *slog.Logger
	now      func() time.Time
}

func New(cfg config.Config, dataStore store.DataStore, notifier OTPNotifier, logger *slog.Logger) (*Server, error) {
	if dataStore == nil || notifier == nil || logger == nil {
		return nil, errors.New("store, notifier and logger are required")
	}
	return &Server{cfg: cfg, store: dataStore, notifier: notifier, logger: logger, now: time.Now}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.marketplaceRoutes(mux)
	s.profileRoutes(mux)
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /api/v1/avatars", s.listAvatars)
	mux.HandleFunc("GET /avatars/{filename}", s.serveAvatar)
	mux.HandleFunc("POST /api/v1/auth/register/step-1", s.registerStep1)
	mux.HandleFunc("POST /api/v1/auth/register/step-2", s.registerStep2)
	mux.HandleFunc("POST /api/v1/auth/register/otp/verify", s.verifyRegistrationOTP)
	mux.HandleFunc("POST /api/v1/auth/register/otp/resend", s.resendRegistrationOTP)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/login/otp/verify", s.verifyLoginOTP)
	mux.HandleFunc("POST /api/v1/auth/login/otp/resend", s.resendLoginOTP)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("POST /api/v1/operator/login", s.operatorLogin)
	mux.HandleFunc("POST /api/v1/operator/logout", s.operatorLogout)
	mux.HandleFunc("GET /api/v1/operator/users", s.operatorListUsers)
	mux.HandleFunc("DELETE /api/v1/operator/users/{userId}", s.operatorDeleteUser)
	mux.HandleFunc("GET /api/v1/users", s.listUsers)
	mux.HandleFunc("GET /api/v1/users/me", s.me)
	mux.HandleFunc("DELETE /api/v1/users/me", s.deleteAccount)
	mux.HandleFunc("GET /swagger/openapi.yaml", s.openAPISpec)
	mux.HandleFunc("GET /swagger/index.html", s.swaggerUI)
	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusTemporaryRedirect)
	})
	return s.recoverMiddleware(s.corsMiddleware(s.loggingMiddleware(mux)))
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, apiError{Code: "SERVICE_UNAVAILABLE", Message: "Veritabanı hazır değil."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) authenticate(r *http.Request) (tokenHash, userID string, apiErr *apiError) {
	rawToken, err := security.ParseBearer(r.Header.Get("Authorization"))
	if err != nil {
		return "", "", &apiError{Code: "UNAUTHORIZED", Message: "Geçerli bir access token gerekli."}
	}
	tokenHash = security.HashValue(s.cfg.TokenSecret, "access-token", rawToken)
	userID, err = s.store.ResolveAccessToken(r.Context(), tokenHash, s.now().UTC())
	if err != nil {
		return "", "", &apiError{Code: "UNAUTHORIZED", Message: "Access token geçersiz veya logout ile iptal edilmiş."}
	}
	return tokenHash, userID, nil
}

func (s *Server) newAccessSession(userID string, now time.Time) (string, domain.AccessSession) {
	rawToken := security.RandomToken(32)
	return rawToken, domain.AccessSession{
		ID: security.NewID(), UserID: userID,
		TokenHash: security.HashValue(s.cfg.TokenSecret, "access-token", rawToken), CreatedAt: now,
	}
}

func (s *Server) userResponse(user domain.User) userResponse {
	return userResponse{
		DisplayName: user.FirstName + " " + user.LastName, TicketBalance: user.TicketBalance,
		ID: user.ID, FirstName: user.FirstName, LastName: user.LastName,
		Phone: user.Phone, Username: user.Username, AvatarID: user.AvatarID,
		AvatarURL: s.avatarURL(user.AvatarID), CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) publicUserResponse(user domain.User) publicUserResponse {
	return publicUserResponse{
		ID: user.ID, FirstName: user.FirstName, LastName: user.LastName,
		Username: user.Username, AvatarID: user.AvatarID,
		AvatarURL: s.avatarURL(user.AvatarID), CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) debugOTP(code string) string {
	if s.cfg.Environment == "production" {
		return ""
	}
	return code
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path,
			"status", recorder.status, "duration", time.Since(started).String())
	})
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "error", recovered, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, apiError{Code: "INTERNAL_ERROR", Message: "Beklenmeyen bir hata oluştu."})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			allowedOrigin := origin
			if contains(s.cfg.CORSAllowedOrigins, "*") {
				allowedOrigin = "*"
			}
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin != "" && !s.originAllowed(origin) {
				writeError(w, http.StatusForbidden, apiError{Code: "ORIGIN_NOT_ALLOWED", Message: "İstek kaynağına izin verilmiyor."})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	return contains(s.cfg.CORSAllowedOrigins, "*") || contains(s.cfg.CORSAllowedOrigins, origin)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, payload apiError) { writeJSON(w, status, payload) }

func internalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, apiError{Code: "INTERNAL_ERROR", Message: "Beklenmeyen bir hata oluştu."})
}

func maskPhone(phone string) string {
	if len(phone) < 5 {
		return "****"
	}
	return phone[:min(3, len(phone)-4)] + " ** *** **" + phone[len(phone)-2:]
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
