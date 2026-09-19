package httpapi

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"ceksepet-api/internal/domain"
	"ceksepet-api/internal/security"
	"ceksepet-api/internal/store"
)

func (s *Server) registerStep1(w http.ResponseWriter, r *http.Request) {
	var request registerStep1Request
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	request.FirstName = strings.TrimSpace(request.FirstName)
	request.LastName = strings.TrimSpace(request.LastName)
	phone, phoneOK := normalizePhone(request.Phone)
	fields := make(map[string]string)
	if !validName(request.FirstName) {
		fields["firstName"] = "Ad 1-100 karakter olmalıdır."
	}
	if !validName(request.LastName) {
		fields["lastName"] = "Soyad 1-100 karakter olmalıdır."
	}
	if !phoneOK {
		fields["phone"] = "Geçerli bir telefon numarası girin."
	}
	if !validPIN(request.PIN) {
		fields["pin"] = "PIN yalnızca 6 rakamdan oluşmalıdır."
	}
	if apiErr := collectValidation(fields); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	exists, err := s.store.PhoneExists(r.Context(), phone)
	if err != nil {
		s.logger.Error("check phone", "error", err)
		internalError(w)
		return
	}
	if exists {
		writeError(w, http.StatusConflict, apiError{Code: "PHONE_ALREADY_REGISTERED", Message: "Bu telefon numarası zaten kayıtlı."})
		return
	}
	pinHash, err := security.HashPIN(request.PIN)
	if err != nil {
		s.logger.Error("hash PIN", "error", err)
		internalError(w)
		return
	}
	now := s.now().UTC()
	_ = s.store.PurgeExpiredSessions(r.Context(), now)
	rawToken := security.RandomToken(32)
	registration := domain.Registration{
		ID:        security.NewID(),
		TokenHash: security.HashValue(s.cfg.TokenSecret, "registration-token", rawToken),
		FirstName: request.FirstName, LastName: request.LastName, Phone: phone, PINHash: pinHash,
		CurrentStep: 2, ExpiresAt: now.Add(s.cfg.RegistrationTTL), CreatedAt: now,
	}
	if err := s.store.CreateRegistration(r.Context(), registration); err != nil {
		s.logger.Error("create registration", "error", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"registrationToken": rawToken,
		"currentStep":       2,
		"expiresAt":         registration.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) registerStep2(w http.ResponseWriter, r *http.Request) {
	var request registerStep2Request
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	fields := make(map[string]string)
	if strings.TrimSpace(request.RegistrationToken) == "" {
		fields["registrationToken"] = "Kayıt tokenı zorunludur."
	}
	if _, ok := findAvatar(request.AvatarID); !ok {
		fields["avatarId"] = "Avatar katalogdaki geçerli bir değer olmalıdır."
	}
	if apiErr := collectValidation(fields); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	registration, tokenHash, ok := s.activeRegistration(w, r, request.RegistrationToken, 2)
	if !ok {
		return
	}
	if registration.CurrentStep != 2 {
		writeError(w, http.StatusConflict, apiError{Code: "INVALID_REGISTRATION_STEP", Message: "Profil adımı daha önce tamamlanmış."})
		return
	}
	now := s.now().UTC()
	username, err := s.generateUsername(r, registration, now)
	if err != nil {
		s.logger.Error("generate username", "error", err)
		internalError(w)
		return
	}
	otpCode := security.RandomOTP()
	if err := s.notifier.Send(r.Context(), "sms", registration.Phone, otpCode); err != nil {
		s.logger.Error("send register SMS OTP", "error", err)
		writeError(w, http.StatusBadGateway, apiError{Code: "OTP_DELIVERY_FAILED", Message: "Kayıt SMS kodu gönderilemedi."})
		return
	}
	otpHash := security.HashValue(s.cfg.OTPSecret, "register-otp:"+registration.ID, otpCode)
	if err := s.store.SetRegistrationProfileAndOTP(r.Context(), tokenHash, username, request.AvatarID,
		otpHash, now, now.Add(s.cfg.OTPTTL)); err != nil {
		s.logger.Error("save registration profile", "error", err)
		internalError(w)
		return
	}
	response := map[string]any{
		"registrationToken": request.RegistrationToken,
		"currentStep":       3,
		"username":          username,
		"avatarId":          request.AvatarID,
		"avatarUrl":         s.avatarURL(request.AvatarID),
		"otpSent":           true,
		"maskedPhone":       maskPhone(registration.Phone),
		"otpExpiresIn":      int(s.cfg.OTPTTL.Seconds()),
		"retryAfter":        int(s.cfg.OTPRetryAfter.Seconds()),
	}
	if debugCode := s.debugOTP(otpCode); debugCode != "" {
		response["debugOtpCode"] = debugCode
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) verifyRegistrationOTP(w http.ResponseWriter, r *http.Request) {
	var request verifyRegistrationOTPRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	fields := make(map[string]string)
	if request.RegistrationToken == "" {
		fields["registrationToken"] = "Kayıt tokenı zorunludur."
	}
	if !security.IsSixDigits(request.OTPCode) {
		fields["otpCode"] = "SMS kodu 6 rakam olmalıdır."
	}
	if apiErr := collectValidation(fields); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	registration, tokenHash, ok := s.activeRegistration(w, r, request.RegistrationToken, 3)
	if !ok {
		return
	}
	now := s.now().UTC()
	if registration.OTPExpiresAt == nil || !registration.OTPExpiresAt.After(now) {
		writeError(w, http.StatusBadRequest, apiError{Code: "OTP_EXPIRED", Message: "Kayıt SMS kodunun süresi dolmuş."})
		return
	}
	if registration.OTPAttempts >= 5 {
		writeError(w, http.StatusTooManyRequests, apiError{Code: "OTP_ATTEMPTS_EXCEEDED", Message: "Çok fazla hatalı kod denendi. Yeni kod isteyin."})
		return
	}
	actualHash := security.HashValue(s.cfg.OTPSecret, "register-otp:"+registration.ID, request.OTPCode)
	if !security.ValuesEqual(registration.OTPHash, actualHash) {
		_ = s.store.IncrementRegistrationOTPAttempts(r.Context(), tokenHash)
		writeError(w, http.StatusBadRequest, apiError{Code: "OTP_INVALID", Message: "Kayıt SMS kodu hatalı."})
		return
	}
	userID := security.NewID()
	rawAccessToken, accessSession := s.newAccessSession(userID, now)
	user, err := s.store.CompleteRegistration(r.Context(), tokenHash, actualHash, userID, accessSession, now)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, apiError{Code: "REGISTRATION_CONFLICT", Message: "Telefon numarası veya kullanıcı adı artık kullanılamıyor."})
		return
	}
	if err != nil {
		s.logger.Error("complete registration", "error", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, authResponse{User: s.userResponse(user), AccessToken: rawAccessToken})
}

func (s *Server) resendRegistrationOTP(w http.ResponseWriter, r *http.Request) {
	var request registrationTokenRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	registration, tokenHash, ok := s.activeRegistration(w, r, request.RegistrationToken, 3)
	if !ok {
		return
	}
	now := s.now().UTC()
	if retryAfter := retrySeconds(registration.OTPSentAt, s.cfg.OTPRetryAfter, now); retryAfter > 0 {
		writeRateLimit(w, retryAfter)
		return
	}
	code := security.RandomOTP()
	if err := s.notifier.Send(r.Context(), "sms", registration.Phone, code); err != nil {
		writeError(w, http.StatusBadGateway, apiError{Code: "OTP_DELIVERY_FAILED", Message: "Kayıt SMS kodu gönderilemedi."})
		return
	}
	hash := security.HashValue(s.cfg.OTPSecret, "register-otp:"+registration.ID, code)
	if err := s.store.SetRegistrationOTP(r.Context(), tokenHash, hash, now, now.Add(s.cfg.OTPTTL)); err != nil {
		s.logger.Error("store resent registration OTP", "error", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, s.otpResponse(code, registration.Phone))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	phone, phoneOK := normalizePhone(request.Phone)
	if !phoneOK || !validPIN(request.PIN) {
		writeError(w, http.StatusUnauthorized, apiError{Code: "INVALID_CREDENTIALS", Message: "Telefon numarası veya PIN hatalı."})
		return
	}
	now := s.now().UTC()
	retryAfter, err := s.store.LoginRetryAfter(r.Context(), phone, now)
	if err != nil {
		s.logger.Error("check PIN rate limit", "error", err)
		internalError(w)
		return
	}
	if retryAfter > 0 {
		writePINRateLimit(w, retryAfter)
		return
	}
	user, err := s.store.GetUserByPhone(r.Context(), phone)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Error("get login user", "error", err)
		internalError(w)
		return
	}
	if err != nil || !security.ComparePIN(user.PINHash, request.PIN) {
		retryAfter, recordErr := s.store.RecordLoginFailure(r.Context(), phone, now,
			s.cfg.PINMaxAttempts, s.cfg.PINAttemptWindow, s.cfg.PINLockDuration)
		if recordErr != nil {
			s.logger.Error("record PIN failure", "error", recordErr)
			internalError(w)
			return
		}
		if retryAfter > 0 {
			writePINRateLimit(w, retryAfter)
			return
		}
		writeError(w, http.StatusUnauthorized, apiError{Code: "INVALID_CREDENTIALS", Message: "Telefon numarası veya PIN hatalı."})
		return
	}
	if err := s.store.ClearLoginFailures(r.Context(), phone); err != nil {
		s.logger.Error("clear PIN failures", "error", err)
		internalError(w)
		return
	}
	rawLoginToken := security.RandomToken(32)
	code := security.RandomOTP()
	loginSession := domain.LoginSession{
		ID:        security.NewID(),
		TokenHash: security.HashValue(s.cfg.TokenSecret, "login-token", rawLoginToken),
		UserID:    user.ID, Phone: user.Phone,
		OTPHash:      security.HashValue(s.cfg.OTPSecret, "login-otp:"+user.ID+":"+rawLoginToken, code),
		OTPExpiresAt: now.Add(s.cfg.OTPTTL), OTPSentAt: now,
		ExpiresAt: now.Add(s.cfg.LoginTTL), CreatedAt: now,
	}
	if err := s.notifier.Send(r.Context(), "sms", user.Phone, code); err != nil {
		writeError(w, http.StatusBadGateway, apiError{Code: "OTP_DELIVERY_FAILED", Message: "Giriş SMS kodu gönderilemedi."})
		return
	}
	if err := s.store.CreateLoginSession(r.Context(), loginSession); err != nil {
		s.logger.Error("create login session", "error", err)
		internalError(w)
		return
	}
	response := s.otpResponse(code, user.Phone)
	response["loginToken"] = rawLoginToken
	response["expiresAt"] = loginSession.ExpiresAt.Format(time.RFC3339)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) verifyLoginOTP(w http.ResponseWriter, r *http.Request) {
	var request verifyLoginOTPRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	fields := make(map[string]string)
	if request.LoginToken == "" {
		fields["loginToken"] = "Giriş tokenı zorunludur."
	}
	if !security.IsSixDigits(request.OTPCode) {
		fields["otpCode"] = "SMS kodu 6 rakam olmalıdır."
	}
	if apiErr := collectValidation(fields); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	loginSession, tokenHash, ok := s.activeLogin(w, r, request.LoginToken)
	if !ok {
		return
	}
	now := s.now().UTC()
	if !loginSession.OTPExpiresAt.After(now) {
		writeError(w, http.StatusBadRequest, apiError{Code: "OTP_EXPIRED", Message: "Giriş SMS kodunun süresi dolmuş."})
		return
	}
	if loginSession.OTPAttempts >= 5 {
		writeError(w, http.StatusTooManyRequests, apiError{Code: "OTP_ATTEMPTS_EXCEEDED", Message: "Çok fazla hatalı kod denendi. Yeni kod isteyin."})
		return
	}
	actualHash := security.HashValue(s.cfg.OTPSecret, "login-otp:"+loginSession.UserID+":"+request.LoginToken, request.OTPCode)
	if !security.ValuesEqual(loginSession.OTPHash, actualHash) {
		_ = s.store.IncrementLoginOTPAttempts(r.Context(), tokenHash)
		writeError(w, http.StatusBadRequest, apiError{Code: "OTP_INVALID", Message: "Giriş SMS kodu hatalı."})
		return
	}
	rawAccessToken, accessSession := s.newAccessSession(loginSession.UserID, now)
	if err := s.store.CompleteLogin(r.Context(), tokenHash, actualHash, accessSession); err != nil {
		s.logger.Error("complete login", "error", err)
		internalError(w)
		return
	}
	user, err := s.store.GetUserByID(r.Context(), loginSession.UserID)
	if err != nil {
		s.logger.Error("get logged in user", "error", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, authResponse{User: s.userResponse(user), AccessToken: rawAccessToken})
}

func (s *Server) resendLoginOTP(w http.ResponseWriter, r *http.Request) {
	var request loginTokenRequest
	if apiErr := decodeJSON(w, r, &request); apiErr != nil {
		writeError(w, http.StatusBadRequest, *apiErr)
		return
	}
	loginSession, tokenHash, ok := s.activeLogin(w, r, request.LoginToken)
	if !ok {
		return
	}
	now := s.now().UTC()
	if retryAfter := retrySeconds(&loginSession.OTPSentAt, s.cfg.OTPRetryAfter, now); retryAfter > 0 {
		writeRateLimit(w, retryAfter)
		return
	}
	code := security.RandomOTP()
	if err := s.notifier.Send(r.Context(), "sms", loginSession.Phone, code); err != nil {
		writeError(w, http.StatusBadGateway, apiError{Code: "OTP_DELIVERY_FAILED", Message: "Giriş SMS kodu gönderilemedi."})
		return
	}
	hash := security.HashValue(s.cfg.OTPSecret, "login-otp:"+loginSession.UserID+":"+request.LoginToken, code)
	if err := s.store.SetLoginOTP(r.Context(), tokenHash, hash, now, now.Add(s.cfg.OTPTTL)); err != nil {
		s.logger.Error("store resent login OTP", "error", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, s.otpResponse(code, loginSession.Phone))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	tokenHash, _, apiErr := s.authenticate(r)
	if apiErr != nil {
		writeError(w, http.StatusUnauthorized, *apiErr)
		return
	}
	if err := s.store.RevokeAccessToken(r.Context(), tokenHash, s.now().UTC()); err != nil {
		writeError(w, http.StatusUnauthorized, apiError{Code: "UNAUTHORIZED", Message: "Access token geçersiz veya daha önce iptal edilmiş."})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	_, userID, apiErr := s.authenticate(r)
	if apiErr != nil {
		writeError(w, http.StatusUnauthorized, *apiErr)
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, apiError{Code: "UNAUTHORIZED", Message: "Kullanıcı bulunamadı."})
		return
	}
	writeJSON(w, http.StatusOK, s.userResponse(user))
}

func (s *Server) activeRegistration(
	w http.ResponseWriter, r *http.Request, rawToken string, minimumStep int,
) (domain.Registration, string, bool) {
	if strings.TrimSpace(rawToken) == "" {
		writeError(w, http.StatusBadRequest, *validationError("registrationToken", "Kayıt tokenı zorunludur."))
		return domain.Registration{}, "", false
	}
	tokenHash := security.HashValue(s.cfg.TokenSecret, "registration-token", rawToken)
	registration, err := s.store.GetRegistration(r.Context(), tokenHash)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !registration.ExpiresAt.After(s.now().UTC())) {
		writeError(w, http.StatusBadRequest, apiError{Code: "REGISTRATION_SESSION_EXPIRED", Message: "Kayıt tokenı geçersiz veya süresi dolmuş. Kayıt baştan başlatılmalıdır."})
		return domain.Registration{}, "", false
	}
	if err != nil {
		s.logger.Error("get registration", "error", err)
		internalError(w)
		return domain.Registration{}, "", false
	}
	if registration.CurrentStep < minimumStep {
		writeError(w, http.StatusConflict, apiError{Code: "INVALID_REGISTRATION_STEP", Message: "Önceki kayıt adımı tamamlanmalıdır."})
		return domain.Registration{}, "", false
	}
	return registration, tokenHash, true
}

func (s *Server) activeLogin(w http.ResponseWriter, r *http.Request, rawToken string) (domain.LoginSession, string, bool) {
	if strings.TrimSpace(rawToken) == "" {
		writeError(w, http.StatusBadRequest, *validationError("loginToken", "Giriş tokenı zorunludur."))
		return domain.LoginSession{}, "", false
	}
	tokenHash := security.HashValue(s.cfg.TokenSecret, "login-token", rawToken)
	loginSession, err := s.store.GetLoginSession(r.Context(), tokenHash)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !loginSession.ExpiresAt.After(s.now().UTC())) {
		writeError(w, http.StatusBadRequest, apiError{Code: "LOGIN_SESSION_EXPIRED", Message: "Giriş tokenı geçersiz veya süresi dolmuş. Tekrar giriş yapın."})
		return domain.LoginSession{}, "", false
	}
	if err != nil {
		s.logger.Error("get login session", "error", err)
		internalError(w)
		return domain.LoginSession{}, "", false
	}
	return loginSession, tokenHash, true
}

func (s *Server) generateUsername(r *http.Request, registration domain.Registration, now time.Time) (string, error) {
	base := usernameBase(registration.FirstName, registration.LastName)
	for attempt := 0; attempt < 100; attempt++ {
		candidate := base + "@" + security.RandomUsernameSuffix()
		taken, err := s.store.UsernameTaken(r.Context(), candidate, registration.ID, now)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", errors.New("could not generate a unique username")
}

func (s *Server) otpResponse(code, phone string) map[string]any {
	response := map[string]any{
		"otpSent": true, "maskedPhone": maskPhone(phone),
		"otpExpiresIn": int(s.cfg.OTPTTL.Seconds()), "retryAfter": int(s.cfg.OTPRetryAfter.Seconds()),
	}
	if debugCode := s.debugOTP(code); debugCode != "" {
		response["debugOtpCode"] = debugCode
	}
	return response
}

func retrySeconds(sentAt *time.Time, retryWindow time.Duration, now time.Time) int {
	if sentAt == nil {
		return 0
	}
	remaining := sentAt.Add(retryWindow).Sub(now)
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Seconds()))
}

func writeRateLimit(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	writeError(w, http.StatusTooManyRequests, apiError{
		Code: "OTP_RATE_LIMITED", Message: "Yeni SMS kodu istemeden önce bekleyin.", RetryAfter: retryAfter,
	})
}

func writePINRateLimit(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	writeError(w, http.StatusTooManyRequests, apiError{
		Code: "PIN_RATE_LIMITED", Message: "Çok fazla hatalı PIN denendi. Daha sonra tekrar deneyin.", RetryAfter: retryAfter,
	})
}
