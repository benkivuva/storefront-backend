package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidTaxClass   = errors.New("domain: invalid tax class")
	ErrEmptySKU          = errors.New("domain: sku is required")
	ErrEmptyProductName  = errors.New("domain: product name is required")
	ErrZeroProductID     = errors.New("domain: product id is zero")
	ErrZeroCategoryID    = errors.New("domain: category id is zero")
	ErrEmptyCategorySlug = errors.New("domain: category slug is required")
)

type TaxClass string

const (
	TaxStandard16 TaxClass = "STANDARD_16"
	TaxZeroRated  TaxClass = "ZERO_RATED"
	TaxExempt     TaxClass = "EXEMPT"
)

func (t TaxClass) IsValid() bool {
	switch t {
	case TaxStandard16, TaxZeroRated, TaxExempt:
		return true
	}
	return false
}

type Category struct {
	id       ID
	parentID ID
	slug     string
	name     string
}

func NewCategory(id, parentID ID, slug, name string) (Category, error) {
	if id.IsZero() {
		return Category{}, ErrZeroCategoryID
	}
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return Category{}, ErrEmptyCategorySlug
	}
	if name == "" {
		return Category{}, fmt.Errorf("domain: category name is required")
	}
	return Category{id: id, parentID: parentID, slug: slug, name: name}, nil
}

func (c Category) ID() ID       { return c.id }
func (c Category) ParentID() ID { return c.parentID }
func (c Category) Slug() string { return c.slug }
func (c Category) Name() string { return c.name }

type Product struct {
	id            ID
	sku           string
	name          string
	description   string
	categoryID    ID
	price         Money
	taxClass      TaxClass
	etimsItemCode string
	isActive      bool
	version       int
	deletedAt     time.Time
	createdAt     time.Time
	updatedAt     time.Time
}

type ProductRecord struct {
	ID            ID
	SKU           string
	Name          string
	Description   string
	CategoryID    ID
	Price         Money
	TaxClass      TaxClass
	EtimsItemCode string
	IsActive      bool
	Version       int
	DeletedAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewProduct(id ID, sku, name, description string, categoryID ID, price Money, taxClass TaxClass, etimsCode string, at time.Time) (Product, error) {
	if id.IsZero() {
		return Product{}, ErrZeroProductID
	}
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return Product{}, ErrEmptySKU
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Product{}, ErrEmptyProductName
	}
	if !taxClass.IsValid() {
		return Product{}, fmt.Errorf("%w: %q", ErrInvalidTaxClass, taxClass)
	}
	return Product{
		id:            id,
		sku:           sku,
		name:          name,
		description:   description,
		categoryID:    categoryID,
		price:         price,
		taxClass:      taxClass,
		etimsItemCode: etimsCode,
		isActive:      true,
		version:       1,
		createdAt:     at,
		updatedAt:     at,
	}, nil
}

func RehydrateProduct(r ProductRecord) (Product, error) {
	p, err := NewProduct(r.ID, r.SKU, r.Name, r.Description, r.CategoryID, r.Price, r.TaxClass, r.EtimsItemCode, r.CreatedAt)
	if err != nil {
		return Product{}, err
	}
	p.isActive = r.IsActive
	p.version = r.Version
	p.deletedAt = r.DeletedAt
	p.updatedAt = r.UpdatedAt
	return p, nil
}

func (p Product) UpdateDetails(name, description string, categoryID ID, price Money, taxClass TaxClass, etimsCode string, at time.Time) (Product, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Product{}, ErrEmptyProductName
	}
	if !taxClass.IsValid() {
		return Product{}, fmt.Errorf("%w: %q", ErrInvalidTaxClass, taxClass)
	}
	p.name = name
	p.description = description
	p.categoryID = categoryID
	p.price = price
	p.taxClass = taxClass
	p.etimsItemCode = etimsCode
	p.updatedAt = at
	p.version++ // Increment version for optimistic concurrency (If-Match)
	return p, nil
}

func (p Product) SoftDelete(at time.Time) Product {
	p.isActive = false
	p.deletedAt = at
	p.updatedAt = at
	p.version++
	return p
}

func (p Product) ID() ID                { return p.id }
func (p Product) SKU() string           { return p.sku }
func (p Product) Name() string          { return p.name }
func (p Product) Description() string   { return p.description }
func (p Product) CategoryID() ID        { return p.categoryID }
func (p Product) Price() Money          { return p.price }
func (p Product) TaxClass() TaxClass    { return p.taxClass }
func (p Product) EtimsItemCode() string { return p.etimsItemCode }
func (p Product) IsActive() bool        { return p.isActive }
func (p Product) Version() int          { return p.version }
func (p Product) DeletedAt() time.Time  { return p.deletedAt }
func (p Product) CreatedAt() time.Time  { return p.createdAt }
func (p Product) UpdatedAt() time.Time  { return p.updatedAt }
