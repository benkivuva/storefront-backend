package apitest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

func TestOpenAPIContract_Me(t *testing.T) {
	t.Parallel()

	specPath := "../../../api/openapi.yaml"
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed to read spec: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specBytes)
	if err != nil {
		t.Fatalf("failed to parse spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spec is invalid: %v", err)
	}

	s := newTestServer(t, serverOpts{})
	_, token, _ := registerAndLogin(t, s)

	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/me", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, r)

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("failed to build openapi router: %v", err)
	}
	route, pathParams, err := router.FindRoute(r)
	if err != nil {
		t.Fatalf("failed to find route in spec: %v", err)
	}

	response := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    r,
			PathParams: pathParams,
			Route:      route,
		},
		Status: rec.Code,
		Header: rec.Header(),
		Body:   io.NopCloser(rec.Body),
	}

	if err := openapi3filter.ValidateResponse(context.Background(), response); err != nil {
		t.Fatalf("response did not match OpenAPI spec: %v", err)
	}
}
