package provider

import (
	"context"
	"net/http"
	"testing"

	"cosmossdk.io/log"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type mockProvider struct {
	id         string
	categories []int32
}

func (m mockProvider) ID() string          { return m.id }
func (m mockProvider) Categories() []int32 { return m.categories }
func (m mockProvider) SetHTTPClient(client *http.Client) {
	// no-op
}
func (m mockProvider) Fetch(ctx context.Context, symbol string) (string, error) {
	return "1", nil
}

func TestRegistry_GetProviders_ReturnsCopy(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	cat := int32(2)
	reg, err := New(logger, []oracletypes.Category{oracletypes.Category(cat)}, mockProvider{id: "p1", categories: []int32{cat}})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	p1 := reg.GetProviders(cat)
	if len(p1) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(p1))
	}

	// Mutate returned slice and ensure registry isn't affected.
	p1 = append(p1, mockProvider{id: "evil", categories: []int32{cat}})
	p2 := reg.GetProviders(cat)
	if len(p2) != 1 {
		t.Fatalf("expected registry unchanged (1 provider), got %d", len(p2))
	}
}

func TestRegistry_New_UnknownCategorySkipped(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	cat := int32(2)

	// provider returns category not present in chain categories.
	_, err := New(logger, []oracletypes.Category{oracletypes.Category(cat)}, mockProvider{id: "p1", categories: []int32{999}})
	if err == nil {
		t.Fatalf("expected error due to missing provider for category %d", cat)
	}
}

func TestRegistry_New_MaxProvidersPerCategory(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	cat := int32(2)

	var pvs []Provider
	for i := 0; i < MaxProvidersPerCategory+5; i++ {
		pvs = append(pvs, mockProvider{id: "p", categories: []int32{cat}})
	}

	reg, err := New(logger, []oracletypes.Category{oracletypes.Category(cat)}, pvs...)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	got := reg.GetProviders(cat)
	if len(got) != MaxProvidersPerCategory {
		t.Fatalf("expected %d providers, got %d", MaxProvidersPerCategory, len(got))
	}
}
