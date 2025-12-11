package provider

import (
	"context"
	"math/big"
	"sync"

	"cosmossdk.io/log"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type Provider interface {
	ID() string
	Categories() []int32
	Fetch(ctx context.Context, symbol string) (*big.Float, error)
}

type Registry struct {
	mu        sync.RWMutex
	logger    log.Logger
	providers map[int32][]Provider
}

func New(logger log.Logger, categories []oracletypes.Category, providers ...Provider) *Registry {
	registry := &Registry{
		logger:    logger,
		providers: make(map[int32][]Provider),
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, category := range categories {
		registry.providers[int32(category)] = make([]Provider, 0)
	}
	for _, provider := range providers {
		for _, category := range provider.Categories() {
			registry.providers[category] = append(registry.providers[category], provider)
			registry.logger.Info("registered provider", "provider", provider.ID(), "category", category)
		}
	}

	return registry
}

func (r *Registry) GetProviders(category int32) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[category]
}
