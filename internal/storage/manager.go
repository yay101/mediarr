package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sync"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/storage/local"
	"github.com/yay101/mediarr/internal/storage/s3"
)

type StorageBackend interface {
	Type() string
	Write(ctx context.Context, key string, reader io.Reader, size int64) error
	Read(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	GetSize(ctx context.Context, key string) (int64, error)
	Close() error
}

type ConfigLibrary interface {
	Movies() []StorageConfigLocation
	TV() []StorageConfigLocation
	Music() []StorageConfigLocation
	Books() []StorageConfigLocation
}

type StorageConfigLocation interface {
	Path() string
	Type() string
	Bucket() string
	Region() string
	Endpoint() string
	AccessKey() string
	SecretKey() string
	ForcePathStyle() bool
	Priority() int
}

type Manager struct {
	backends    map[uint32]StorageBackend
	locations   map[uint32]*db.StorageLocation
	preferences map[db.MediaType]uint32
	mu          sync.RWMutex
	db          *db.Database
	cfg         ConfigLibrary
}

func NewManager(cfg ConfigLibrary, database *db.Database) *Manager {
	return &Manager{
		backends:    make(map[uint32]StorageBackend),
		locations:   make(map[uint32]*db.StorageLocation),
		preferences: make(map[db.MediaType]uint32),
		db:          database,
		cfg:         cfg,
	}
}

func (m *Manager) Initialize(ctx context.Context) error {
	if err := m.loadConfigLocations(ctx); err != nil {
		slog.Warn("failed to load config storage locations", "error", err)
	}

	if err := m.loadDatabaseLocations(ctx); err != nil {
		slog.Warn("failed to load database storage locations", "error", err)
	}

	if err := m.loadPreferences(ctx); err != nil {
		slog.Warn("failed to load storage preferences", "error", err)
	}

	return nil
}

func (m *Manager) loadConfigLocations(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mediaTypes := []struct {
		mt       db.MediaType
		locsFunc func() []StorageConfigLocation
	}{
		{db.MediaTypeMovie, func() []StorageConfigLocation { return m.cfg.Movies() }},
		{db.MediaTypeTV, func() []StorageConfigLocation { return m.cfg.TV() }},
		{db.MediaTypeMusic, func() []StorageConfigLocation { return m.cfg.Music() }},
		{db.MediaTypeBook, func() []StorageConfigLocation { return m.cfg.Books() }},
	}

	for _, mt := range mediaTypes {
		locs := mt.locsFunc()
		for i, loc := range locs {
			storageLoc := &db.StorageLocation{
				MediaType:      mt.mt,
				Name:           loc.Path(),
				Type:           loc.Type(),
				Path:           loc.Path(),
				Bucket:         loc.Bucket(),
				Region:         loc.Region(),
				Endpoint:       loc.Endpoint(),
				AccessKey:      loc.AccessKey(),
				SecretKey:      loc.SecretKey(),
				ForcePathStyle: loc.ForcePathStyle(),
				Priority:       loc.Priority(),
				IsDefault:      i == 0,
			}

			backend, err := m.createBackend(storageLoc)
			if err != nil {
				slog.Error("failed to create backend", "path", loc.Path(), "error", err)
				continue
			}

			m.locations[storageLoc.ID] = storageLoc
			m.backends[storageLoc.ID] = backend
		}
	}

	return nil
}

func (m *Manager) loadDatabaseLocations(ctx context.Context) error {
	table, err := m.db.StorageLocations()
	if err != nil {
		return err
	}

	locations, err := table.Filter(func(loc db.StorageLocation) bool { return true })
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range locations {
		loc := &locations[i]

		if _, exists := m.locations[loc.ID]; exists {
			continue
		}

		backend, err := m.createBackend(loc)
		if err != nil {
			slog.Error("failed to create backend for DB location", "id", loc.ID, "error", err)
			continue
		}

		m.locations[loc.ID] = loc
		m.backends[loc.ID] = backend
	}

	return nil
}

func (m *Manager) loadPreferences(ctx context.Context) error {
	table, err := m.db.StoragePreferences()
	if err != nil {
		return err
	}

	prefs, err := table.Filter(func(p db.StoragePreference) bool { return true })
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, pref := range prefs {
		m.preferences[pref.MediaType] = pref.StorageID
	}

	return nil
}

func (m *Manager) createBackend(loc *db.StorageLocation) (StorageBackend, error) {
	switch loc.Type {
	case "local":
		return local.New(loc.Path), nil
	case "s3":
		return s3.New(s3.Options{
			Bucket:         loc.Bucket,
			Region:         loc.Region,
			Endpoint:       loc.Endpoint,
			AccessKey:      loc.AccessKey,
			SecretKey:      loc.SecretKey,
			ForcePathStyle: loc.ForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("unknown storage type: %s", loc.Type)
	}
}

func (m *Manager) GetLocation(id uint32) (*db.StorageLocation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loc, ok := m.locations[id]
	return loc, ok
}

func (m *Manager) GetBackend(id uint32) (StorageBackend, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	backend, ok := m.backends[id]
	return backend, ok
}

func (m *Manager) GetBackendForLocation(loc *db.StorageLocation) (StorageBackend, error) {
	m.mu.RLock()
	backend, ok := m.backends[loc.ID]
	m.mu.RUnlock()

	if ok {
		return backend, nil
	}

	newBackend, err := m.createBackend(loc)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.backends[loc.ID] = newBackend
	m.locations[loc.ID] = loc
	m.mu.Unlock()

	return newBackend, nil
}

func (m *Manager) GetPreferredStorage(mediaType db.MediaType) (*db.StorageLocation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if storageID, ok := m.preferences[mediaType]; ok {
		if loc, ok := m.locations[storageID]; ok {
			return loc, true
		}
	}

	for _, loc := range m.locations {
		if loc.MediaType == mediaType && loc.IsDefault {
			return loc, true
		}
	}

	return nil, false
}

func (m *Manager) SetPreferredStorage(mediaType db.MediaType, storageID uint32) error {
	table, err := m.db.StoragePreferences()
	if err != nil {
		return err
	}

	pref := db.StoragePreference{
		MediaType:   mediaType,
		StorageID:   storageID,
		UseAutoPick: storageID == 0,
	}

	existing, _ := table.Filter(func(p db.StoragePreference) bool {
		return p.MediaType == mediaType
	})
	if len(existing) > 0 {
		pref.ID = existing[0].ID
		return table.Update(pref.ID, &pref)
	}

	_, err = table.Insert(&pref)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if storageID == 0 {
		delete(m.preferences, mediaType)
	} else {
		m.preferences[mediaType] = storageID
	}
	m.mu.Unlock()

	return nil
}

func (m *Manager) GetLocationsForMediaType(mediaType db.MediaType) []*db.StorageLocation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*db.StorageLocation
	for _, loc := range m.locations {
		if loc.MediaType == mediaType {
			result = append(result, loc)
		}
	}
	return result
}

func (m *Manager) MoveFile(ctx context.Context, fromLoc, toLoc *db.StorageLocation, fromKey, toKey string) error {
	fromBackend, err := m.GetBackendForLocation(fromLoc)
	if err != nil {
		return fmt.Errorf("get source backend: %w", err)
	}

	toBackend, err := m.GetBackendForLocation(toLoc)
	if err != nil {
		return fmt.Errorf("get dest backend: %w", err)
	}

	reader, err := fromBackend.Read(ctx, fromKey)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	defer reader.Close()

	size, err := fromBackend.GetSize(ctx, fromKey)
	if err != nil {
		slog.Warn("could not get source size, proceeding without verification", "error", err)
		size = 0
	}

	if err := toBackend.Write(ctx, toKey, reader, size); err != nil {
		return fmt.Errorf("write dest: %w", err)
	}

	if err := fromBackend.Delete(ctx, fromKey); err != nil {
		slog.Warn("failed to delete source after move", "key", fromKey, "error", err)
	}

	return nil
}

func (m *Manager) GetVirtualPath(loc *db.StorageLocation, key string) string {
	return path.Join(loc.Path, key)
}

func (m *Manager) ParseVirtualPath(virtualPath string) (locID uint32, key string, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, loc := range m.locations {
		if len(virtualPath) >= len(loc.Path) && virtualPath[:len(loc.Path)] == loc.Path {
			key = virtualPath[len(loc.Path):]
			if len(key) > 0 && key[0] == '/' {
				key = key[1:]
			}
			return loc.ID, key, nil
		}
	}

	return 0, "", fmt.Errorf("virtual path not found: %s", virtualPath)
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, backend := range m.backends {
		backend.Close()
	}
	m.backends = nil
	m.locations = nil
}
