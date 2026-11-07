package dto

// RegisterResponse is the body of POST /auth/register.
type RegisterResponse struct {
	User User `json:"user"`
}

// LoginResponse is the body of POST /auth/login and POST /auth/refresh.
// Refresh token is set as a cookie, not in the body.
type LoginResponse struct {
	AccessToken string `json:"access_token"`
	User        User   `json:"user"`
}
