package dto

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

// Problem is an RFC 7807 document. Code is the stable machine-readable
// identifier the frontend branches on; Title is human-readable and may
// change without notice.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
}

type ErrorMapping struct {
	Status int
	Code   string
	Title  string
}

// errorMappings is the allowlist of errors safe to expose to the client.
// Errors not in this table map to 500 INTERNAL, so internal-only sentinels
// (ErrZeroUserID, ErrInvalidMoney, and the like) never leak their messages.
var errorMappings = []struct {
	err    error
	Status int
	Code   string
	Title  string
}{
	// Service-layer errors
	{service.ErrInvalidCredentials, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials"},
	{service.ErrTokenReused, http.StatusUnauthorized, "TOKEN_REUSED", "refresh token reuse detected"},
	{service.ErrTokenExpired, http.StatusUnauthorized, "TOKEN_EXPIRED", "token expired"},
	{service.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"},
	{service.ErrEmailTaken, http.StatusConflict, "EMAIL_TAKEN", "email already registered"},
	{service.ErrEmptyPatch, http.StatusBadRequest, "EMPTY_PATCH", "no fields to update"},
	{service.ErrAddressLimit, http.StatusConflict, "ADDRESS_LIMIT", "address limit reached"},
	{service.ErrEmptyCart, http.StatusBadRequest, "EMPTY_CART", "cart is empty"},
	{service.ErrPriceChanged, http.StatusConflict, "PRICE_CHANGED", "prices have changed"},
	{service.ErrInvalidCursor, http.StatusBadRequest, "INVALID_CURSOR", "invalid pagination cursor"},
	{service.ErrInvalidSort, http.StatusBadRequest, "INVALID_SORT", "unsupported sort order"},
	{service.ErrOrderNotPayable, http.StatusConflict, "ORDER_NOT_PAYABLE", "order is not awaiting payment"},
	{service.ErrOrderExpired, http.StatusConflict, "ORDER_EXPIRED", "order payment window has expired"},
	{service.ErrOrderNotCancellable, http.StatusConflict, "ORDER_NOT_CANCELLABLE", "order can only be cancelled while awaiting payment"},
	{service.ErrInvalidWebhook, http.StatusBadRequest, "INVALID_WEBHOOK", "invalid webhook payload"},
	{service.ErrInvalidPaymentMethod, http.StatusBadRequest, "INVALID_PAYMENT_METHOD", "unsupported payment method"},

	// Domain errors safe to expose
	{domain.ErrNotFound, http.StatusNotFound, "NOT_FOUND", "resource not found"},
	{domain.ErrConflict, http.StatusConflict, "CONFLICT", "conflict"},
	{domain.ErrInvalidEmail, http.StatusBadRequest, "INVALID_EMAIL", "invalid email"},
	{domain.ErrInvalidPhone, http.StatusBadRequest, "INVALID_PHONE", "invalid phone number"},
	{domain.ErrInvalidName, http.StatusBadRequest, "INVALID_NAME", "invalid name"},
	{domain.ErrInvalidRole, http.StatusBadRequest, "INVALID_ROLE", "invalid role"},
	{domain.ErrInvalidRecipient, http.StatusBadRequest, "INVALID_RECIPIENT", "invalid recipient"},
	{domain.ErrInvalidLine1, http.StatusBadRequest, "INVALID_LINE1", "invalid address line 1"},
	{domain.ErrInvalidTown, http.StatusBadRequest, "INVALID_TOWN", "invalid town"},
	{domain.ErrInvalidID, http.StatusBadRequest, "INVALID_ID", "invalid identifier"},
	{domain.ErrPasswordTooShort, http.StatusBadRequest, "PASSWORD_TOO_SHORT", "password too short"},
	{domain.ErrPasswordTooLong, http.StatusBadRequest, "PASSWORD_TOO_LONG", "password too long"},
	{domain.ErrPasswordCommon, http.StatusBadRequest, "PASSWORD_TOO_COMMON", "password too common"},

	// Cart and product validation
	{domain.ErrInvalidQuantity, http.StatusBadRequest, "INVALID_QUANTITY", "quantity must be positive"},
	{domain.ErrMaxQuantityExceeded, http.StatusBadRequest, "QUANTITY_LIMIT_EXCEEDED", "quantity exceeds the per-item limit"},
	{domain.ErrItemNotFound, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found in cart"},
	{domain.ErrInvalidTaxClass, http.StatusBadRequest, "INVALID_TAX_CLASS", "invalid tax class"},
	{domain.ErrEmptySKU, http.StatusBadRequest, "INVALID_SKU", "sku is required"},
	{domain.ErrEmptyProductName, http.StatusBadRequest, "INVALID_PRODUCT_NAME", "product name is required"},

	// Inventory
	{domain.ErrNegativeStock, http.StatusConflict, "INSUFFICIENT_STOCK", "insufficient stock"},
	{domain.ErrInvalidReason, http.StatusBadRequest, "INVALID_STOCK_REASON", "invalid stock movement reason"},

	// Order and payment state
	{domain.ErrInvalidStatus, http.StatusBadRequest, "INVALID_STATUS", "invalid status"},
	{domain.ErrInvalidTransition, http.StatusConflict, "INVALID_TRANSITION", "illegal status transition"},
	{domain.ErrEmptyOrderItems, http.StatusBadRequest, "EMPTY_ORDER", "order must contain at least one item"},
	{domain.ErrPaymentNotFound, http.StatusNotFound, "PAYMENT_NOT_FOUND", "payment not found"},
	{domain.ErrInvalidPaymentStatus, http.StatusConflict, "INVALID_PAYMENT_STATUS", "invalid payment status transition"},
	{domain.ErrPaymentAlreadyOpen, http.StatusConflict, "PAYMENT_ALREADY_OPEN", "an open payment already exists for this order"},
	{domain.ErrAmountMismatch, http.StatusConflict, "AMOUNT_MISMATCH", "payment amount does not match the order total"},

	// Money and tax. ErrInvalidMoney is a programmer error (zero-value Money)
	// and deliberately stays internal; the others can be caused by user input.
	{domain.ErrInvalidCurrency, http.StatusBadRequest, "INVALID_CURRENCY", "unsupported currency"},
	{domain.ErrNegativeAmount, http.StatusBadRequest, "INVALID_AMOUNT", "amount must be non-negative"},
	{domain.ErrCurrencyMismatch, http.StatusBadRequest, "CURRENCY_MISMATCH", "currency mismatch"},
	{domain.ErrInvalidRateBp, http.StatusBadRequest, "INVALID_TAX_RATE", "invalid tax rate"},
}

// FromError maps err to (status, code, title). Uses errors.Is, so wrapped
// errors match. Unmapped errors return 500 INTERNAL with a generic title —
// the client never sees internal detail.
func FromError(err error) ErrorMapping {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			return ErrorMapping{Status: m.Status, Code: m.Code, Title: m.Title}
		}
	}
	return ErrorMapping{
		Status: http.StatusInternalServerError,
		Code:   "INTERNAL",
		Title:  "internal server error",
	}
}

func WriteProblem(w http.ResponseWriter, status int, code, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Code:   code,
	})
}

// WriteProblemExtra writes a problem with additional top-level fields —
// used for e.g. 413 with max_bytes.
func WriteProblemExtra(w http.ResponseWriter, status int, code, title string, extras map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	body := map[string]any{
		"type":   "about:blank",
		"title":  title,
		"status": status,
		"code":   code,
	}
	for k, v := range extras {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError maps err and writes the corresponding problem response.
func WriteError(w http.ResponseWriter, err error) {
	m := FromError(err)
	WriteProblem(w, m.Status, m.Code, m.Title)
}
