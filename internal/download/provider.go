package download

import (
	"context"
	"io"
	"sync"

	"github.com/yay101/mediarr/internal/db"
)

type DownloadProviderType string

const (
	ProviderTypeTorrent DownloadProviderType = "torrent"
	ProviderTypeUsenet  DownloadProviderType = "usenet"
)

type DownloadProvider interface {
	Type() DownloadProviderType
	Name() string

	Download(ctx context.Context, job *db.DownloadJob) (io.ReadCloser, error)
	Pause(ctx context.Context, jobID uint32) error
	Resume(ctx context.Context, jobID uint32) error
	Cancel(ctx context.Context, jobID uint32) error

	GetProgress(ctx context.Context, jobID uint32) (float32, error)
	GetStats(ctx context.Context) ProviderStats

	Test(ctx context.Context) error
	Close() error
}

type ProviderStats struct {
	ActiveDownloads int
	TotalDownloaded int64
	TotalUploaded   int64
	DownloadRate    int64
	UploadRate      int64
	ConnectedPeers  int
}

type ProviderFactory func(cfg Config) (DownloadProvider, error)

var (
	providerRegistry = make(map[DownloadProviderType]ProviderFactory)
	providerMu       sync.RWMutex
)

func RegisterProvider(pType DownloadProviderType, factory ProviderFactory) {
	providerMu.Lock()
	defer providerMu.Unlock()
	providerRegistry[pType] = factory
}

func CreateProvider(pType DownloadProviderType, cfg Config) (DownloadProvider, error) {
	providerMu.RLock()
	factory, ok := providerRegistry[pType]
	providerMu.RUnlock()

	if !ok {
		return nil, nil
	}

	return factory(cfg)
}

func AvailableProviders() []DownloadProviderType {
	providerMu.RLock()
	defer providerMu.RUnlock()

	types := make([]DownloadProviderType, 0, len(providerRegistry))
	for t := range providerRegistry {
		types = append(types, t)
	}
	return types
}
