package download

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/download/torrent"
	"github.com/yay101/mediarr/internal/download/usenet"
)

type Manager struct {
	db            *db.Database
	torrentClient *torrent.Client
	usenetClient  *usenet.NZBClient
	worker        *Worker
	config        Config
	mu            sync.RWMutex
}

type Config struct {
	ListenPort         int
	DataDir            string
	DownloadDir        string
	MaxConnections     int
	MaxPeersPerTorrent int
	UsenetServers      []usenet.ServerConfig
	Library            LibraryConfig
}

type LibraryConfig struct {
	Movies string
	TV     string
}

func (m *Manager) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func NewManager(cfg Config, database *db.Database) (*Manager, error) {
	m := &Manager{
		db:     database,
		config: cfg,
	}

	if cfg.DataDir != "" {
		torrentCfg := torrent.Config{
			ListenPort:         cfg.ListenPort,
			DataDir:            cfg.DataDir,
			DownloadDir:        cfg.DownloadDir,
			MaxConnections:     cfg.MaxConnections,
			MaxPeersPerTorrent: cfg.MaxPeersPerTorrent,
		}

		tc, err := torrent.NewClient(torrentCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create torrent client: %w", err)
		}
		m.torrentClient = tc
	}

	if len(cfg.UsenetServers) > 0 {
		m.usenetClient = usenet.NewNZBClient(cfg.UsenetServers)
	}

	m.worker = NewWorker(database, m.torrentClient, m.usenetClient, m)

	return m, nil
}

func (m *Manager) Start() {
	if m.worker != nil {
		m.worker.Start()
	}
	log.Println("Download manager started")
}

func (m *Manager) Stop() {
	log.Println("Stopping download manager...")

	if m.worker != nil {
		m.worker.Stop()
	}

	if m.torrentClient != nil {
		m.torrentClient.Close()
	}

	if m.usenetClient != nil {
		m.usenetClient.Close()
	}

	log.Println("Download manager stopped")
}

func (m *Manager) AddDownload(job *db.DownloadJob) error {
	table, err := m.db.Downloads()
	if err != nil {
		return fmt.Errorf("failed to get downloads table: %w", err)
	}

	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	_, err = table.Insert(job)
	if err != nil {
		return fmt.Errorf("failed to add download job: %w", err)
	}

	return nil
}

func (m *Manager) PauseDownload(id uint32) error {
	if m.worker == nil {
		return fmt.Errorf("worker not running")
	}
	return m.worker.PauseDownload(id)
}

func (m *Manager) ResumeDownload(id uint32) error {
	if m.worker == nil {
		return fmt.Errorf("worker not running")
	}
	return m.worker.ResumeDownload(id)
}

func (m *Manager) CancelDownload(id uint32) error {
	if m.worker == nil {
		return fmt.Errorf("worker not running")
	}
	return m.worker.CancelDownload(id)
}

func (m *Manager) GetProgress(id uint32) (float32, int64, int64, error) {
	m.mu.RLock()
	activeJobs := m.worker.GetActiveJobs()
	m.mu.RUnlock()

	active, ok := activeJobs[id]
	if !ok {
		table, err := m.db.Downloads()
		if err != nil {
			return 0, 0, 0, err
		}

		job, err := table.Get(id)
		if err != nil {
			return 0, 0, 0, err
		}
		return job.Progress, int64(job.BytesDone), int64(job.BytesTotal), nil
	}

	progress := active.progress
	bytesDone := int64(active.job.BytesDone)
	bytesTotal := int64(active.job.BytesTotal)

	return progress, bytesDone, bytesTotal, nil
}

func (m *Manager) GetAllDownloads() ([]db.DownloadJob, error) {
	table, err := m.db.Downloads()
	if err != nil {
		return nil, err
	}

	return table.Filter(func(job db.DownloadJob) bool {
		return true
	})
}

func (m *Manager) GetDownload(id uint32) (*db.DownloadJob, error) {
	table, err := m.db.Downloads()
	if err != nil {
		return nil, err
	}

	return table.Get(id)
}

func (m *Manager) SetTorrentRateLimit(download, upload int) {
	if m.torrentClient != nil {
		m.torrentClient.SetDownloadLimit(download)
		m.torrentClient.SetUploadLimit(upload)
	}
}

func (m *Manager) GetTorrentClient() *torrent.Client {
	return m.torrentClient
}

func (m *Manager) GetUsenetClient() *usenet.NZBClient {
	return m.usenetClient
}
