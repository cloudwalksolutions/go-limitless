package auth

type Response struct {
	Token string `json:"token,omitempty"`
	User  User   `json:"user,omitempty"`
}
