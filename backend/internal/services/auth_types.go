package services

type RegisterRequest struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

type LoginRequest struct {
	Email    string
	Password string
}

type AuthResponse struct {
	AccessToken string `json:"access_token"`
}
