package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CatalogService interface {
	ListProducts(ctx context.Context, filters domain.ListFilters, limit int, cursor string) ([]domain.Product, error)
	GetProduct(ctx context.Context, id domain.ID) (domain.Product, error)
	ListCategories(ctx context.Context) ([]domain.Category, error)
	CreateProduct(ctx context.Context, sku, name, desc string, catID domain.ID, price domain.Money, taxClass domain.TaxClass, actorID domain.ID, ip string) (domain.Product, error)
	UpdateProduct(ctx context.Context, id domain.ID, name, desc string, catID domain.ID, price domain.Money, taxClass domain.TaxClass, expectedVersion int, actorID domain.ID, ip string) (domain.Product, error)
	DeleteProduct(ctx context.Context, id domain.ID, actorID domain.ID) error
	AdjustStock(ctx context.Context, productID domain.ID, delta int, reason domain.StockReason) error
	ListLowStock(ctx context.Context) ([]domain.Inventory, error)
}

type CatalogHandler struct {
	svc CatalogService
}

func NewCatalogHandler(svc CatalogService) *CatalogHandler {
	return &CatalogHandler{svc: svc}
}

func (h *CatalogHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	cursor := r.URL.Query().Get("cursor")

	filters := domain.ListFilters{
		CategoryID: r.URL.Query().Get("category"),
		Query:      r.URL.Query().Get("q"),
		InStock:    r.URL.Query().Get("in_stock") == "true",
	}
	if minStr := r.URL.Query().Get("min_price"); minStr != "" {
		filters.MinPrice, _ = strconv.ParseInt(minStr, 10, 64)
	}
	if maxStr := r.URL.Query().Get("max_price"); maxStr != "" {
		filters.MaxPrice, _ = strconv.ParseInt(maxStr, 10, 64)
	}

	products, err := h.svc.ListProducts(r.Context(), filters, limit, cursor)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ProductsResponse{Products: dto.FromProducts(products)})
}

func (h *CatalogHandler) GetProduct(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}

	p, err := h.svc.GetProduct(r.Context(), id)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromProduct(p))
}

func (h *CatalogHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.svc.ListCategories(r.Context())
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.CategoriesResponse{Categories: dto.FromCategories(cats)})
}

type createProductRequest struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CategoryID  string `json:"category_id"`
	PriceMinor  int64  `json:"price_minor"`
	TaxClass    string `json:"tax_class"`
}

func (h *CatalogHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var req createProductRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	u, _ := middleware.UserFrom(r.Context())

	var catID domain.ID
	if req.CategoryID != "" {
		var err error
		catID, err = domain.ParseID(req.CategoryID)
		if err != nil {
			dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid category id")
			return
		}
	}

	price, err := domain.New(req.PriceMinor, domain.KES)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_PRICE", err.Error())
		return
	}

	p, err := h.svc.CreateProduct(r.Context(), req.SKU, req.Name, req.Description, catID, price, domain.TaxClass(req.TaxClass), u.UserID, "")
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.FromProduct(p))
}

type updateProductRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	CategoryID  string `json:"category_id"`
	PriceMinor  int64  `json:"price_minor"`
	TaxClass    string `json:"tax_class"`
}

func (h *CatalogHandler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}

	u, _ := middleware.UserFrom(r.Context())

	versionStr := r.Header.Get("If-Match")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "MISSING_IF_MATCH", "If-Match header required")
		return
	}

	var req updateProductRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	var catID domain.ID
	if req.CategoryID != "" {
		catID, err = domain.ParseID(req.CategoryID)
		if err != nil {
			dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid category id")
			return
		}
	}

	price, err := domain.New(req.PriceMinor, domain.KES)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_PRICE", err.Error())
		return
	}

	p, err := h.svc.UpdateProduct(r.Context(), id, req.Name, req.Description, catID, price, domain.TaxClass(req.TaxClass), version, u.UserID, "")
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromProduct(p))
}

func (h *CatalogHandler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}
	u, _ := middleware.UserFrom(r.Context())

	if err := h.svc.DeleteProduct(r.Context(), id, u.UserID); err != nil {
		dto.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type adjustStockRequest struct {
	Delta  int    `json:"delta"`
	Reason string `json:"reason"`
}

func (h *CatalogHandler) AdjustStock(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}

	var req adjustStockRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	if err := h.svc.AdjustStock(r.Context(), id, req.Delta, domain.StockReason(req.Reason)); err != nil {
		dto.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CatalogHandler) ListLowStock(w http.ResponseWriter, r *http.Request) {
	invs, err := h.svc.ListLowStock(r.Context())
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromInventories(invs))
}
