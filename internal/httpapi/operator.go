package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

const operatorAttemptKey = "__operator_login__"

func (s *Server) operatorLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.OperatorEnabled() {
		writeError(w, http.StatusNotFound, apiError{Code: "OPERATOR_DISABLED", Message: "Operatör girişi etkin değil."})
		return
	}
	var request operatorLoginRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	fields := make(map[string]string)
	if request.Username == "" {
		fields["username"] = "Operatör kullanıcı adı zorunludur."
	}
	if request.Password == "" {
		fields["password"] = "Operatör parolası zorunludur."
	}
	if apiErr := collectValidation(fields); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}

	now := s.now().UTC()
	retryAfter, err := s.store.LoginRetryAfter(r.Context(), operatorAttemptKey, now)
	if err != nil {
		s.logger.Error("check operator rate limit", "error", err)
		internalError(w)
		return
	}
	if retryAfter > 0 {
		writeOperatorRateLimit(w, retryAfter)
		return
	}
	if !security.StringsEqual(s.cfg.OperatorUsername, request.Username) ||
		!security.StringsEqual(s.cfg.OperatorPassword, request.Password) {
		retryAfter, recordErr := s.store.RecordLoginFailure(r.Context(), operatorAttemptKey, now,
			s.cfg.PINMaxAttempts, s.cfg.PINAttemptWindow, s.cfg.PINLockDuration)
		if recordErr != nil {
			s.logger.Error("record operator login failure", "error", recordErr)
			internalError(w)
			return
		}
		if retryAfter > 0 {
			writeOperatorRateLimit(w, retryAfter)
			return
		}
		writeError(w, http.StatusUnauthorized, apiError{Code: "INVALID_OPERATOR_CREDENTIALS", Message: "Operatör bilgileri hatalı."})
		return
	}
	if err := s.store.ClearLoginFailures(r.Context(), operatorAttemptKey); err != nil {
		s.logger.Error("clear operator login failures", "error", err)
		internalError(w)
		return
	}

	rawToken := security.RandomToken(32)
	session := domain.OperatorSession{
		ID: security.NewID(), Username: s.cfg.OperatorUsername,
		TokenHash: security.HashValue(s.cfg.TokenSecret, "operator-access-token", rawToken),
		CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(s.cfg.OperatorTokenTTL),
	}
	if err := s.store.CreateOperatorSession(r.Context(), session); err != nil {
		s.logger.Error("create operator session", "error", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, operatorAuthResponse{
		AccessToken: rawToken, TokenType: "Bearer", Username: session.Username,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) authenticateOperator(r *http.Request) (tokenHash, username string, apiErr *apiError) {
	if !s.cfg.OperatorEnabled() {
		return "", "", &apiError{Code: "UNAUTHORIZED", Message: "Geçerli bir operatör tokenı gerekli."}
	}
	rawToken, err := security.ParseBearer(r.Header.Get("Authorization"))
	if err != nil {
		return "", "", &apiError{Code: "UNAUTHORIZED", Message: "Geçerli bir operatör tokenı gerekli."}
	}
	tokenHash = security.HashValue(s.cfg.TokenSecret, "operator-access-token", rawToken)
	username, err = s.store.ResolveOperatorToken(r.Context(), tokenHash, s.now().UTC())
	if err != nil {
		return "", "", &apiError{Code: "UNAUTHORIZED", Message: "Operatör tokenı geçersiz veya süresi dolmuş."}
	}
	return tokenHash, username, nil
}

func (s *Server) operatorLogout(w http.ResponseWriter, r *http.Request) {
	tokenHash, _, apiErr := s.authenticateOperator(r)
	if apiErr != nil {
		writeError(w, http.StatusUnauthorized, *apiErr)
		return
	}
	if err := s.store.RevokeOperatorToken(r.Context(), tokenHash, s.now().UTC()); err != nil {
		writeError(w, http.StatusUnauthorized, apiError{Code: "UNAUTHORIZED", Message: "Operatör tokenı geçersiz veya iptal edilmiş."})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) operatorListUsers(w http.ResponseWriter, r *http.Request) {
	if _, _, apiErr := s.authenticateOperator(r); apiErr != nil {
		writeError(w, http.StatusUnauthorized, *apiErr)
		return
	}
	limit, apiErr := paginationValue(r, "limit", 20, 1, 100)
	if apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	offset, apiErr := paginationValue(r, "offset", 0, 0, 1_000_000)
	if apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	users, total, err := s.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error("operator list users", "error", err)
		internalError(w)
		return
	}
	responses := make([]userResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, s.userResponse(user))
	}
	writeJSON(w, http.StatusOK, operatorUserListResponse{
		Users: responses, Total: total, Limit: limit, Offset: offset,
	})
}

func (s *Server) operatorDeleteUser(w http.ResponseWriter, r *http.Request) {
	if _, _, apiErr := s.authenticateOperator(r); apiErr != nil {
		writeError(w, http.StatusUnauthorized, *apiErr)
		return
	}
	userID := strings.TrimSpace(r.PathValue("userId"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, *validationError("userId", "Kullanıcı kimliği zorunludur."))
		return
	}
	if err := s.store.DeleteUser(r.Context(), userID); err != nil {
		if errors.Is(err, store.ErrAccountInUse) {
			s.marketError(w, err)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, apiError{Code: "USER_NOT_FOUND", Message: "Kullanıcı bulunamadı."})
			return
		}
		s.logger.Error("operator delete user", "error", err)
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeOperatorRateLimit(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	writeError(w, http.StatusTooManyRequests, apiError{
		Code: "OPERATOR_RATE_LIMITED", Message: "Çok fazla hatalı operatör girişi denendi.", RetryAfter: retryAfter,
	})
}
