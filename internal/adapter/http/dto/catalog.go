package dto

import (
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type Product struct {
	ID          string    `json:"id"`
	SKU         string    `json:"sku"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CategoryID  string    `json:"category_id,omitempty"`
	PriceMinor  int64     `json:"price_minor"`
	Currency    string    `json:"currency"`
	TaxClass    string    `json:"tax_class"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
}

func FromProduct(p domain.Product) Product {
	var catID string
	if !p.CategoryID().IsZero() {
		catID = p.CategoryID().String()
	}
	return Product{
		ID:          p.ID().String(),
		SKU:         p.SKU(),
		Name:        p.Name(),
		Description: p.Description(),
		CategoryID:  catID,
		PriceMinor:  p.Price().AmountMinor(),
		Currency:    string(p.Price().Currency()),
		TaxClass:    string(p.TaxClass()),
		Version:     p.Version(),
		CreatedAt:   p.CreatedAt(),
	}
}

func FromProducts(ps []domain.Product) []Product {
	out := make([]Product, len(ps))
	for i, p := range ps {
		out[i] = FromProduct(p)
	}
	return out
}

type ProductsResponse struct {
	Products []Product `json:"products"`
}

type Category struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
}

func FromCategory(c domain.Category) Category {
	var parent string
	if !c.ParentID().IsZero() {
		parent = c.ParentID().String()
	}
	return Category{
		ID:       c.ID().String(),
		ParentID: parent,
		Slug:     c.Slug(),
		Name:     c.Name(),
	}
}

func FromCategories(cs []domain.Category) []Category {
	out := make([]Category, len(cs))
	for i, c := range cs {
		out[i] = FromCategory(c)
	}
	return out
}

type CategoriesResponse struct {
	Categories []Category `json:"categories"`
}

type LowStockItem struct {
	ProductID string    `json:"product_id"`
	Available int       `json:"available"`
	Threshold int       `json:"threshold"`
	UpdatedAt time.Time `json:"updated_at"`
}

func FromInventories(invs []domain.Inventory) []LowStockItem {
	out := make([]LowStockItem, len(invs))
	for i, inv := range invs {
		out[i] = LowStockItem{
			ProductID: inv.ProductID().String(),
			Available: inv.Available(),
			Threshold: inv.Threshold(),
			UpdatedAt: inv.UpdatedAt(),
		}
	}
	return out
}
