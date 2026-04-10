package storage

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/yay101/mediarr/db"
)

type Factory func(loc *db.StorageLocation) (StorageBackend, error)

var (
	registry = make(map[string]Factory)
	mu       sync.RWMutex
)

func Register(storageType string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[storageType] = factory
	slog.Debug("registered storage backend", "type", storageType)
}

func Create(loc *db.StorageLocation) (StorageBackend, error) {
	mu.RLock()
	factory, ok := registry[loc.Type]
	mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown storage type: %s", loc.Type)
	}

	return factory(loc)
}

func AvailableTypes() []string {
	mu.RLock()
	defer mu.RUnlock()

	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}
