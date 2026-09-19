package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("list public users", "error", err)
		internalError(w)
		return
	}
	responseUsers := make([]publicUserResponse, 0, len(users))
	for _, user := range users {
		responseUsers = append(responseUsers, s.publicUserResponse(user))
	}
	writeJSON(w, http.StatusOK, userListResponse{
		Users: responseUsers, Total: total, Limit: limit, Offset: offset,
	})
}

func paginationValue(r *http.Request, key string, fallback, minimum, maximum int64) (int64, *apiError) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, validationError(key, "Değer izin verilen aralıkta bir tam sayı olmalıdır.")
	}
	return value, nil
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	_, userID, apiErr := s.authenticate(r)
	if apiErr != nil {
		writeError(w, http.StatusUnauthorized, *apiErr)
		return
	}

	var request deleteAccountRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	fields := make(map[string]string)
	if request.Username == "" {
		fields["username"] = "Kullanıcı adı zorunludur."
	}
	if !validPIN(request.PIN) {
		fields["pin"] = "PIN yalnızca 6 rakamdan oluşmalıdır."
	}
	if apiErr := collectValidation(fields); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}

	user, err := s.store.GetUserByID(r.Context(), userID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, apiError{Code: "UNAUTHORIZED", Message: "Kullanıcı bulunamadı."})
		return
	}
	if err != nil {
		s.logger.Error("get account for deletion", "error", err)
		internalError(w)
		return
	}
	now := s.now().UTC()
	retryAfter, err := s.store.LoginRetryAfter(r.Context(), user.Phone, now)
	if err != nil {
		s.logger.Error("check account deletion PIN rate limit", "error", err)
		internalError(w)
		return
	}
	if retryAfter > 0 {
		writePINRateLimit(w, retryAfter)
		return
	}

	if !strings.EqualFold(user.Username, request.Username) || !security.ComparePIN(user.PINHash, request.PIN) {
		retryAfter, recordErr := s.store.RecordLoginFailure(r.Context(), user.Phone, now,
			s.cfg.PINMaxAttempts, s.cfg.PINAttemptWindow, s.cfg.PINLockDuration)
		if recordErr != nil {
			s.logger.Error("record account deletion PIN failure", "error", recordErr)
			internalError(w)
			return
		}
		if retryAfter > 0 {
			writePINRateLimit(w, retryAfter)
			return
		}
		writeError(w, http.StatusUnauthorized, apiError{
			Code: "INVALID_CREDENTIALS", Message: "Kullanıcı adı veya PIN hatalı.",
		})
		return
	}

	if err := s.store.DeleteUser(r.Context(), user.ID); err != nil {
		if errors.Is(err, store.ErrAccountInUse) {
			s.marketError(w, err)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, apiError{Code: "UNAUTHORIZED", Message: "Kullanıcı bulunamadı."})
			return
		}
		s.logger.Error("delete account", "error", err)
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
