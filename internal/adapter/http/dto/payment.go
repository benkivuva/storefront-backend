package dto

type InitiatePaymentRequest struct {
	Method string `json:"method"`
	Phone  string `json:"phone,omitempty"`
}

type NextAction struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

type InitiatePaymentResponse struct {
	PaymentID  string     `json:"payment_id"`
	Status     string     `json:"status"`
	NextAction NextAction `json:"next_action"`
}
