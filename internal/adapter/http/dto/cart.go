package dto

import (
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

type CartItem struct {
	ProductID      string `json:"product_id"`
	ProductName    string `json:"product_name"`
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
	SubtotalMinor  int64  `json:"subtotal_minor"`
	Currency       string `json:"currency"`
}

type CartResponse struct {
	CartID     string     `json:"cart_id"`
	Items      []CartItem `json:"items"`
	TotalMinor int64      `json:"total_minor"`
	Currency   string     `json:"currency"`
}

func FromCartView(v service.CartView) CartResponse {
	items := make([]CartItem, len(v.Items))
	for i, item := range v.Items {
		items[i] = CartItem{
			ProductID:      item.ProductID.String(),
			ProductName:    item.ProductName,
			SKU:            item.SKU,
			Quantity:       item.Quantity,
			UnitPriceMinor: item.UnitPriceMinor,
			SubtotalMinor:  item.SubtotalMinor,
			Currency:       item.Currency,
		}
	}
	return CartResponse{
		CartID:     v.CartID.String(),
		Items:      items,
		TotalMinor: v.TotalMinor,
		Currency:   v.Currency,
	}
}

func CartIDString(id domain.ID) string {
	return id.String()
}
