package ai

import (
	"fmt"

	"github.com/yay101/mediarr/config"
)

type ProviderFactory func(cfg *config.AIConfig) (Provider, error)

var registry = make(map[string]ProviderFactory)

func Register(providerType string, factory ProviderFactory) {
	registry[providerType] = factory
}

func Available() []string {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}

func Create(cfg *config.AIConfig) (Provider, error) {
	factory, ok := registry[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown AI provider: %s", cfg.Provider)
	}
	return factory(cfg)
}

func MustCreate(cfg *config.AIConfig) Provider {
	p, err := Create(cfg)
	if err != nil {
		panic(err)
	}
	return p
}
