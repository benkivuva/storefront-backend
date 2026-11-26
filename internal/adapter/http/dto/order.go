package dto

import (
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type OrderItem struct {
	ProductID   string `json:"product_id"`
	SKU         string `json:"sku"`
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	UnitPrice   int64  `json:"unit_price_minor"`
	TaxClass    string `json:"tax_class"`
	TaxRateBp   int    `json:"tax_rate_bp"`
	LineVat     int64  `json:"line_vat_minor"`
	LineGross   int64  `json:"line_gross_minor"`
}

type Order struct {
	ID               string      `json:"id"`
	OrderNumber      int64       `json:"order_number"`
	Status           string      `json:"status"`
	SubtotalMinor    int64       `json:"subtotal_minor"`
	TaxMinor         int64       `json:"tax_minor"`
	DeliveryFeeMinor int64       `json:"delivery_fee_minor"`
	TotalMinor       int64       `json:"total_minor"`
	Currency         string      `json:"currency"`
	Items            []OrderItem `json:"items"`
	PaymentExpiresAt time.Time   `json:"payment_expires_at"`
	CreatedAt        time.Time   `json:"created_at"`
}

func FromOrder(o *domain.Order) Order {
	var items []OrderItem
	for _, i := range o.Items() {
		items = append(items, OrderItem{
			ProductID:   i.ProductID.String(),
			SKU:         i.SKU,
			ProductName: i.ProductName,
			Quantity:    i.Quantity,
			UnitPrice:   i.UnitPrice.AmountMinor(),
			TaxClass:    string(i.TaxClass),
			TaxRateBp:   i.TaxRateBp,
			LineVat:     i.LineVat.AmountMinor(),
			LineGross:   i.LineGrossMinor,
		})
	}
	return Order{
		ID:               o.ID().String(),
		OrderNumber:      o.OrderNumber(),
		Status:           string(o.Status()),
		SubtotalMinor:    o.SubtotalMinor(),
		TaxMinor:         o.TaxMinor(),
		DeliveryFeeMinor: o.DeliveryFee().AmountMinor(),
		TotalMinor:       o.TotalMinor(),
		Currency:         string(o.DeliveryFee().Currency()),
		Items:            items,
		PaymentExpiresAt: o.PaymentExpiresAt(),
		CreatedAt:        o.CreatedAt(),
	}
}
