package tracker

import (
	"fmt"
)

// TrackerFactory creates tracker instances from configuration.
// New tracker implementations must register a factory to be available
// for use via the Create function.
type TrackerFactory func(cfg *TrackerConfig) (Tracker, error)

// registry holds all registered tracker factories.
// The key is the tracker type (e.g., "red", "btn", "torrentleech").
var registry = make(map[TrackerType]TrackerFactory)

// Register adds a tracker factory to the global registry.
// Call this in an init() function in each tracker implementation file.
// Panics if a tracker of the same type is already registered.
func Register(t TrackerType, factory TrackerFactory) {
	registry[t] = factory
}

// Create instantiates a tracker from configuration using the registered factory.
// Returns ErrUnknownTrackerType if the tracker type is not registered.
func Create(cfg *TrackerConfig) (Tracker, error) {
	factory, ok := registry[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTrackerType, cfg.Type)
	}
	return factory(cfg)
}

// MustCreate is like Create but panics on error.
// Useful for testing or initialization where failure is fatal.
func MustCreate(cfg *TrackerConfig) Tracker {
	t, err := Create(cfg)
	if err != nil {
		panic(err)
	}
	return t
}

// AllTrackerTypes returns a list of all registered tracker types.
// Useful for WebUI to show available options.
func AllTrackerTypes() []TrackerType {
	types := make([]TrackerType, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}

// IsValidTrackerType checks if a tracker type has a registered factory.
func IsValidTrackerType(t TrackerType) bool {
	_, ok := registry[t]
	return ok
}
