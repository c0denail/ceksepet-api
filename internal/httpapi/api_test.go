package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"ceksepet-api/internal/config"
	"ceksepet-api/internal/store"
)

type captureNotifier struct {
	mu    sync.Mutex
	codes map[string]string
}

func (n *captureNotifier) Send(_ context.Context, _, destination, code string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.codes[destination] = code
	return nil
}

func (n *captureNotifier) code(destination string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.codes[destination]
}

type testApp struct {
	handler  http.Handler
	server   *Server
	notifier *captureNotifier
}

func newTestApp(t *testing.T) *testApp {
	return newTestAppWithStore(t, newMemoryStore())
}

func newTestAppWithStore(t *testing.T, dataStore store.DataStore) *testApp {
	t.Helper()
	cfg := config.Config{
		Environment: "test", HTTPAddr: ":0",
		PublicBaseURL:    "http://example.test",
		TokenSecret:      "test-token-secret-with-at-least-32-characters",
		OTPSecret:        "test-otp-secret-with-at-least-32-characters",
		OperatorUsername: "operator", OperatorPassword: "test-operator-password-with-32-characters",
		OperatorTokenTTL: 8 * time.Hour,
		RegistrationTTL:  30 * time.Minute, LoginTTL: 10 * time.Minute,
		OTPTTL: 4 * time.Minute, OTPRetryAfter: time.Minute,
		PINMaxAttempts: 5, PINAttemptWindow: 15 * time.Minute, PINLockDuration: 15 * time.Minute,
		CORSAllowedOrigins: []string{"*"},
		TicketValueKurus:   100, CommissionBPS: 1000,
	}
	notifier := &captureNotifier{codes: make(map[string]string)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(cfg, dataStore, notifier, logger)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return &testApp{handler: server.Handler(), server: server, notifier: notifier}
}

func TestMobileRegistrationLoginAndLogoutFlow(t *testing.T) {
	app := newTestApp(t)
	phone := "+905551112233"

	avatars := requestJSON(t, app.handler, http.MethodGet, "/api/v1/avatars", nil, "")
	assertStatus(t, avatars, http.StatusOK)
	if !bytes.Contains(avatars.Body.Bytes(), []byte("avatar_01")) {
		t.Fatal("avatar catalog is missing avatar_01")
	}

	registered := registerUser(t, app, phone, "123456")
	if registered.AccessToken == "" {
		t.Fatal("registration OTP verification did not return access token")
	}
	if !regexp.MustCompile(`^test_kullanici@[0-9]{5}$`).MatchString(registered.User.Username) {
		t.Fatalf("unexpected generated username: %s", registered.User.Username)
	}

	me := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users/me", nil, registered.AccessToken)
	assertStatus(t, me, http.StatusOK)

	login := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone": "0555 111 22 33", "pin": "123456",
	}, "")
	assertStatus(t, login, http.StatusOK)
	if bytes.Contains(login.Body.Bytes(), []byte("accessToken")) {
		t.Fatal("login must not issue access token before SMS verification")
	}
	var loginBody struct {
		LoginToken string `json:"loginToken"`
	}
	decodeResponse(t, login, &loginBody)
	loginCode := app.notifier.code(phone)
	verifiedLogin := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login/otp/verify", map[string]any{
		"loginToken": loginBody.LoginToken, "otpCode": loginCode,
	}, "")
	assertStatus(t, verifiedLogin, http.StatusOK)
	var loginAuth authResponse
	decodeResponse(t, verifiedLogin, &loginAuth)

	// Mobile uygulamanın kapanması backend sessionını düşürmez; aynı token yeniden kullanılabilir.
	stillAuthenticated := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users/me", nil, loginAuth.AccessToken)
	assertStatus(t, stillAuthenticated, http.StatusOK)

	logout := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/logout", nil, loginAuth.AccessToken)
	assertStatus(t, logout, http.StatusNoContent)
	afterLogout := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users/me", nil, loginAuth.AccessToken)
	assertStatus(t, afterLogout, http.StatusUnauthorized)
}

func TestRegistrationValidationAndResendRateLimit(t *testing.T) {
	app := newTestApp(t)
	invalidPIN := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/step-1", map[string]any{
		"firstName": "Test", "lastName": "Kullanıcı", "phone": "05551112233", "pin": "12ab56",
	}, "")
	assertStatus(t, invalidPIN, http.StatusBadRequest)

	step1 := startRegistration(t, app, "+905559998877", "654321")
	step2 := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/step-2", map[string]any{
		"registrationToken": step1.RegistrationToken, "avatarId": "avatar_02",
	}, "")
	assertStatus(t, step2, http.StatusOK)
	resend := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/otp/resend", map[string]any{
		"registrationToken": step1.RegistrationToken,
	}, "")
	assertStatus(t, resend, http.StatusTooManyRequests)
}

func TestDuplicatePhoneInvalidCredentialsAndSwagger(t *testing.T) {
	app := newTestApp(t)
	phone := "+905557770011"
	_ = registerUser(t, app, phone, "112233")
	duplicate := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/step-1", map[string]any{
		"firstName": "Başka", "lastName": "Kullanıcı", "phone": phone, "pin": "445566",
	}, "")
	assertStatus(t, duplicate, http.StatusConflict)
	wrongPIN := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone": phone, "pin": "000000",
	}, "")
	assertStatus(t, wrongPIN, http.StatusUnauthorized)
	for attempt := 2; attempt < 5; attempt++ {
		wrongPIN = requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]any{
			"phone": phone, "pin": "000000",
		}, "")
		assertStatus(t, wrongPIN, http.StatusUnauthorized)
	}
	locked := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone": phone, "pin": "000000",
	}, "")
	assertStatus(t, locked, http.StatusTooManyRequests)

	spec := requestJSON(t, app.handler, http.MethodGet, "/swagger/openapi.yaml", nil, "")
	assertStatus(t, spec, http.StatusOK)
	if !bytes.Contains(spec.Body.Bytes(), []byte("/api/v1/auth/login/otp/verify:")) {
		t.Fatal("OpenAPI spec is missing login OTP verification")
	}
	if bytes.Contains(spec.Body.Bytes(), []byte("refreshToken")) {
		t.Fatal("OpenAPI spec must not contain refresh token")
	}
	if !bytes.Contains(spec.Body.Bytes(), []byte("operationId: deleteAccount")) {
		t.Fatal("OpenAPI spec is missing account deletion")
	}
	if !bytes.Contains(spec.Body.Bytes(), []byte("operationId: operatorLogin")) ||
		!bytes.Contains(spec.Body.Bytes(), []byte("operatorBearerAuth")) {
		t.Fatal("OpenAPI spec is missing operator authentication")
	}
}

func TestDeleteAccountRequiresBearerUsernameAndPIN(t *testing.T) {
	app := newTestApp(t)
	phone := "+905554440011"
	auth := registerUser(t, app, phone, "321654")

	withoutBearer := requestJSON(t, app.handler, http.MethodDelete, "/api/v1/users/me", map[string]any{
		"username": auth.User.Username, "pin": "321654",
	}, "")
	assertStatus(t, withoutBearer, http.StatusUnauthorized)

	wrongCredentials := requestJSON(t, app.handler, http.MethodDelete, "/api/v1/users/me", map[string]any{
		"username": auth.User.Username, "pin": "000000",
	}, auth.AccessToken)
	assertStatus(t, wrongCredentials, http.StatusUnauthorized)

	deleted := requestJSON(t, app.handler, http.MethodDelete, "/api/v1/users/me", map[string]any{
		"username": auth.User.Username, "pin": "321654",
	}, auth.AccessToken)
	assertStatus(t, deleted, http.StatusNoContent)

	afterDelete := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users/me", nil, auth.AccessToken)
	assertStatus(t, afterDelete, http.StatusUnauthorized)
	loginDeletedUser := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone": phone, "pin": "321654",
	}, "")
	assertStatus(t, loginDeletedUser, http.StatusUnauthorized)
}

func TestPublicUserListIsPaginatedAndOmitsSensitiveFields(t *testing.T) {
	app := newTestApp(t)
	_ = registerUser(t, app, "+905554440021", "123456")
	_ = registerUser(t, app, "+905554440022", "654321")

	listed := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users?limit=1&offset=0", nil, "")
	assertStatus(t, listed, http.StatusOK)
	var response userListResponse
	decodeResponse(t, listed, &response)
	if response.Total != 2 || response.Limit != 1 || response.Offset != 0 || len(response.Users) != 1 {
		t.Fatalf("unexpected paginated user list: %+v", response)
	}
	for _, forbidden := range [][]byte{[]byte(`"phone"`), []byte(`"pin"`), []byte(`"pinHash"`), []byte(`"accessToken"`)} {
		if bytes.Contains(listed.Body.Bytes(), forbidden) {
			t.Fatalf("public user list exposed forbidden field %s", forbidden)
		}
	}

	secondPage := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users?limit=1&offset=1", nil, "")
	assertStatus(t, secondPage, http.StatusOK)
	decodeResponse(t, secondPage, &response)
	if len(response.Users) != 1 || response.Offset != 1 {
		t.Fatalf("unexpected second page: %+v", response)
	}

	invalidLimit := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users?limit=101", nil, "")
	assertStatus(t, invalidLimit, http.StatusBadRequest)
}

func TestOperatorCanListAndDeleteUsersWithoutCustomerToken(t *testing.T) {
	app := newTestApp(t)
	customer := registerUser(t, app, "+905554440031", "123456")

	withoutOperatorToken := requestJSON(t, app.handler, http.MethodGet, "/api/v1/operator/users", nil, "")
	assertStatus(t, withoutOperatorToken, http.StatusUnauthorized)
	withCustomerToken := requestJSON(t, app.handler, http.MethodGet, "/api/v1/operator/users", nil, customer.AccessToken)
	assertStatus(t, withCustomerToken, http.StatusUnauthorized)

	wrongLogin := requestJSON(t, app.handler, http.MethodPost, "/api/v1/operator/login", map[string]any{
		"username": "operator", "password": "wrong-password",
	}, "")
	assertStatus(t, wrongLogin, http.StatusUnauthorized)
	login := requestJSON(t, app.handler, http.MethodPost, "/api/v1/operator/login", map[string]any{
		"username": "operator", "password": "test-operator-password-with-32-characters",
	}, "")
	assertStatus(t, login, http.StatusOK)
	var operatorAuth operatorAuthResponse
	decodeResponse(t, login, &operatorAuth)
	if operatorAuth.AccessToken == "" || operatorAuth.TokenType != "Bearer" {
		t.Fatalf("unexpected operator auth response: %+v", operatorAuth)
	}

	listed := requestJSON(t, app.handler, http.MethodGet, "/api/v1/operator/users", nil, operatorAuth.AccessToken)
	assertStatus(t, listed, http.StatusOK)
	if !bytes.Contains(listed.Body.Bytes(), []byte(`"phone"`)) {
		t.Fatal("operator user list must include customer phone")
	}
	deleted := requestJSON(t, app.handler, http.MethodDelete,
		"/api/v1/operator/users/"+customer.User.ID, nil, operatorAuth.AccessToken)
	assertStatus(t, deleted, http.StatusNoContent)
	afterDelete := requestJSON(t, app.handler, http.MethodGet, "/api/v1/users/me", nil, customer.AccessToken)
	assertStatus(t, afterDelete, http.StatusUnauthorized)

	logout := requestJSON(t, app.handler, http.MethodPost, "/api/v1/operator/logout", nil, operatorAuth.AccessToken)
	assertStatus(t, logout, http.StatusNoContent)
	afterLogout := requestJSON(t, app.handler, http.MethodGet, "/api/v1/operator/users", nil, operatorAuth.AccessToken)
	assertStatus(t, afterLogout, http.StatusUnauthorized)
}

type registrationStart struct {
	RegistrationToken string `json:"registrationToken"`
}

func startRegistration(t *testing.T, app *testApp, phone, pin string) registrationStart {
	t.Helper()
	response := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/step-1", map[string]any{
		"firstName": "Test", "lastName": "Kullanıcı", "phone": phone, "pin": pin,
	}, "")
	assertStatus(t, response, http.StatusOK)
	if bytes.Contains(response.Body.Bytes(), []byte("accessToken")) {
		t.Fatal("step 1 must not issue access token")
	}
	var body registrationStart
	decodeResponse(t, response, &body)
	return body
}

func registerUser(t *testing.T, app *testApp, phone, pin string) authResponse {
	t.Helper()
	step1 := startRegistration(t, app, phone, pin)
	step2 := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/step-2", map[string]any{
		"registrationToken": step1.RegistrationToken, "avatarId": "avatar_01",
	}, "")
	assertStatus(t, step2, http.StatusOK)
	code := app.notifier.code(phone)
	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "999999"
	}
	wrong := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/otp/verify", map[string]any{
		"registrationToken": step1.RegistrationToken, "otpCode": wrongCode,
	}, "")
	assertStatus(t, wrong, http.StatusBadRequest)
	verify := requestJSON(t, app.handler, http.MethodPost, "/api/v1/auth/register/otp/verify", map[string]any{
		"registrationToken": step1.RegistrationToken, "otpCode": code,
	}, "")
	assertStatus(t, verify, http.StatusOK)
	var auth authResponse
	decodeResponse(t, verify, &auth)
	return auth
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, body any, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	var encoded io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		encoded = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, encoded)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response (%d): %v; body=%s", response.Code, err, response.Body.String())
	}
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("expected status %d, got %d: %s", expected, response.Code, response.Body.String())
	}
}
