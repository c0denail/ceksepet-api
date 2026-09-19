package httpapi

type registerStep1Request struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Phone     string `json:"phone"`
	PIN       string `json:"pin"`
}

type registerStep2Request struct {
	RegistrationToken string `json:"registrationToken"`
	AvatarID          string `json:"avatarId"`
}

type registrationTokenRequest struct {
	RegistrationToken string `json:"registrationToken"`
}

type verifyRegistrationOTPRequest struct {
	RegistrationToken string `json:"registrationToken"`
	OTPCode           string `json:"otpCode"`
}

type loginRequest struct {
	Phone string `json:"phone"`
	PIN   string `json:"pin"`
}

type deleteAccountRequest struct {
	Username string `json:"username"`
	PIN      string `json:"pin"`
}

type operatorLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type operatorAuthResponse struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
	ExpiresAt   string `json:"expiresAt"`
	Username    string `json:"username"`
}

type loginTokenRequest struct {
	LoginToken string `json:"loginToken"`
}

type verifyLoginOTPRequest struct {
	LoginToken string `json:"loginToken"`
	OTPCode    string `json:"otpCode"`
}

type userResponse struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Phone     string `json:"phone"`
	Username  string `json:"username"`
	AvatarID  string `json:"avatarId"`
	AvatarURL string `json:"avatarUrl"`
	CreatedAt string `json:"createdAt"`
}

type publicUserResponse struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Username  string `json:"username"`
	AvatarID  string `json:"avatarId"`
	AvatarURL string `json:"avatarUrl"`
	CreatedAt string `json:"createdAt"`
}

type userListResponse struct {
	Users  []publicUserResponse `json:"users"`
	Total  int64                `json:"total"`
	Limit  int64                `json:"limit"`
	Offset int64                `json:"offset"`
}

type operatorUserListResponse struct {
	Users  []userResponse `json:"users"`
	Total  int64          `json:"total"`
	Limit  int64          `json:"limit"`
	Offset int64          `json:"offset"`
}

type authResponse struct {
	User        userResponse `json:"user"`
	AccessToken string       `json:"accessToken"`
}

type apiError struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"fieldErrors,omitempty"`
	RetryAfter  int               `json:"retryAfter,omitempty"`
}
