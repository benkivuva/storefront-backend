package domain

import (
	"encoding/json"
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

type categoryJSON struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
}

func (c Category) MarshalJSON() ([]byte, error) {
	var parent string
	if !c.parentID.IsZero() {
		parent = c.parentID.String()
	}
	return json.Marshal(categoryJSON{
		ID: c.id.String(), ParentID: parent, Slug: c.slug, Name: c.name,
	})
}

func (c *Category) UnmarshalJSON(data []byte) error {
	var raw categoryJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	id, err := ParseID(raw.ID)
	if err != nil {
		return err
	}
	var parentID ID
	if raw.ParentID != "" {
		parentID, err = ParseID(raw.ParentID)
		if err != nil {
			return err
		}
	}
	*c, err = NewCategory(id, parentID, raw.Slug, raw.Name)
	return err
}

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
	p.version++
	return p, nil
}

func (p Product) SoftDelete(at time.Time) Product {
	p.isActive = false
	p.deletedAt = at
	p.updatedAt = at
	p.version++
	return p
}

// JSON Marshaling
type productJSON struct {
	ID            string    `json:"id"`
	SKU           string    `json:"sku"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	CategoryID    string    `json:"category_id,omitempty"`
	Price         Money     `json:"price"`
	TaxClass      string    `json:"tax_class"`
	EtimsItemCode string    `json:"etims_item_code,omitempty"`
	IsActive      bool      `json:"is_active"`
	Version       int       `json:"version"`
	DeletedAt     time.Time `json:"deleted_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (p Product) MarshalJSON() ([]byte, error) {
	var catID string
	if !p.categoryID.IsZero() {
		catID = p.categoryID.String()
	}
	return json.Marshal(productJSON{
		ID: p.id.String(), SKU: p.sku, Name: p.name, Description: p.description,
		CategoryID: catID, Price: p.price, TaxClass: string(p.taxClass),
		EtimsItemCode: p.etimsItemCode, IsActive: p.isActive, Version: p.version,
		DeletedAt: p.deletedAt, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt,
	})
}

func (p *Product) UnmarshalJSON(data []byte) error {
	var raw productJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	id, err := ParseID(raw.ID)
	if err != nil {
		return err
	}
	var catID ID
	if raw.CategoryID != "" {
		catID, err = ParseID(raw.CategoryID)
		if err != nil {
			return err
		}
	}
	rec := ProductRecord{
		ID: id, SKU: raw.SKU, Name: raw.Name, Description: raw.Description,
		CategoryID: catID, Price: raw.Price, TaxClass: TaxClass(raw.TaxClass),
		EtimsItemCode: raw.EtimsItemCode, IsActive: raw.IsActive, Version: raw.Version,
		DeletedAt: raw.DeletedAt, CreatedAt: raw.CreatedAt, UpdatedAt: raw.UpdatedAt,
	}
	*p, err = RehydrateProduct(rec)
	return err
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
