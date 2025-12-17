package provider

import (
	"context"
	"fmt"
	"net/http"

	"cosmossdk.io/log"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type Provider interface {
	ID() string
	Categories() []int32
	// SetHTTPClient replaces the underlying HTTP client. Providers that do not use HTTP may no-op.
	SetHTTPClient(client *http.Client)
	// Fetch returns a decimal string that must be parseable by big.Float.SetString (chain validation).
	Fetch(ctx context.Context, symbol string) (raw string, err error)
}

type Registry struct {
	logger log.Logger

	// providers is immutable after construction.
	providers map[int32][]Provider
}

const MaxProvidersPerCategory = 10

func New(logger log.Logger, categories []oracletypes.Category, providers ...Provider) (*Registry, error) {
	registry := &Registry{
		logger:    logger,
		providers: make(map[int32][]Provider),
	}

	for _, category := range categories {
		registry.providers[int32(category)] = make([]Provider, 0)
	}

	for _, provider := range providers {
		for _, category := range provider.Categories() {
			// Only register for known categories (those returned by chain).
			if _, ok := registry.providers[category]; !ok {
				registry.logger.Warn("provider category not in chain categories, skipping",
					"provider", provider.ID(),
					"category", category,
				)
				continue
			}

			if len(registry.providers[category]) >= MaxProvidersPerCategory {
				registry.logger.Warn("too many providers for category, skipping",
					"provider", provider.ID(),
					"category", category,
					"limit", MaxProvidersPerCategory,
				)
				continue
			}

			registry.providers[category] = append(registry.providers[category], provider)
			registry.logger.Debug("provider registered", "provider", provider.ID(), "category", category)
		}
	}

	// Validate: each category must have at least one provider.
	for category, pvs := range registry.providers {
		if len(pvs) == 0 {
			return nil, fmt.Errorf("no providers configured for category %d", category)
		}
	}

	return registry, nil
}

func (r *Registry) GetProviders(category int32) []Provider {
	// Return a copy to protect registry immutability.
	return append([]Provider(nil), r.providers[category]...)
}
