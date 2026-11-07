package dto

// MeResponse is the body of GET /me and PATCH /me.
type MeResponse struct {
	User User `json:"user"`
}

// AddressesResponse is the body of GET /me/addresses.
type AddressesResponse struct {
	Addresses []Address `json:"addresses"`
}

// SessionsResponse is the body of GET /me/sessions.
type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
}
