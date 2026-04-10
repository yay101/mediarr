package download

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/download/torrent"
	"github.com/yay101/mediarr/internal/download/usenet"
	"github.com/yay101/mediarr/internal/organize"
)

// Worker manages download jobs for both BitTorrent and Usenet.
// It polls the database for queued downloads, starts active downloads,
// tracks progress, and triggers post-download organization.
type Worker struct {
	db            *db.Database
	torrentClient *torrent.Client
	usenetClient  *usenet.NZBClient
	organizer     *organize.Organizer
	manager       *Manager
	activeJobs    map[uint32]*activeDownload
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	interval      time.Duration

	organizeOpts organize.OrganizeOptions
}

// activeDownload tracks an in-progress download job.
type activeDownload struct {
	job         *db.DownloadJob
	infoHash    [20]byte
	torrent     *torrent.Torrent
	storagePath string
	startTime   time.Time
	progress    float32
	done        chan struct{}
	err         error
	cancelFunc  context.CancelFunc
}

// NewWorker creates a download worker with the specified clients and database.
func NewWorker(db *db.Database, tm *torrent.Client, um *usenet.NZBClient, mgr *Manager) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		db:            db,
		torrentClient: tm,
		usenetClient:  um,
		organizer:     organize.NewOrganizer(db),
		manager:       mgr,
		activeJobs:    make(map[uint32]*activeDownload),
		ctx:           ctx,
		cancel:        cancel,
		interval:      5 * time.Second,
		organizeOpts: organize.OrganizeOptions{
			UseHardlink:     true,
			DeleteLeftovers: true,
			Overwrite:       false,
		},
	}
}

func (w *Worker) SetOrganizeOptions(opts organize.OrganizeOptions) {
	w.organizeOpts = opts
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
	slog.Info("Download worker started")
}

func (w *Worker) Stop() {
	slog.Info("Stopping download worker...")
	w.cancel()
	w.wg.Wait()
	slog.Info("Download worker stopped")
}

func (w *Worker) run() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.processQueue()
		}
	}
}

func (w *Worker) getQueuedJobs() ([]db.DownloadJob, error) {
	table, err := w.db.Downloads()
	if err != nil {
		return nil, err
	}

	return table.Filter(func(job db.DownloadJob) bool {
		return job.Status == db.DownloadStatusQueued
	})
}

func (w *Worker) updateJobStatus(id uint32, status db.DownloadStatus) error {
	table, err := w.db.Downloads()
	if err != nil {
		return err
	}

	job, err := table.Get(id)
	if err != nil {
		return err
	}

	job.Status = status
	job.UpdatedAt = time.Now()

	return table.Update(id, job)
}

func (w *Worker) updateJobProgress(id uint32, bytesDone, bytesTotal uint64, progress float32) error {
	table, err := w.db.Downloads()
	if err != nil {
		return err
	}

	job, err := table.Get(id)
	if err != nil {
		return err
	}

	job.BytesDone = bytesDone
	job.BytesTotal = bytesTotal
	job.Progress = progress
	job.UpdatedAt = time.Now()

	return table.Update(id, job)
}

func (w *Worker) processQueue() {
	jobs, err := w.getQueuedJobs()
	if err != nil {
		slog.Error("error fetching queued downloads", "error", err)
		return
	}

	for i := range jobs {
		w.startDownload(&jobs[i])
	}

	w.updateProgress()
}

func parseInfoHash(s string) ([20]byte, error) {
	var hash [20]byte
	if len(s) != 40 {
		return hash, fmt.Errorf("invalid info hash length")
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return hash, err
	}
	if len(decoded) != 20 {
		return hash, fmt.Errorf("invalid decoded length")
	}
	copy(hash[:], decoded)
	return hash, nil
}

func (w *Worker) startDownload(job *db.DownloadJob) {
	w.mu.Lock()
	if _, exists := w.activeJobs[job.ID]; exists {
		w.mu.Unlock()
		return
	}

	if job.Provider == db.DownloadProviderTorrent && job.InfoHash != "" {
		hash, err := parseInfoHash(job.InfoHash)
		if err == nil {
			for _, active := range w.activeJobs {
				if active.torrent != nil && active.infoHash == hash {
					slog.Info("linking job to existing active download", "job_id", job.ID, "infohash", job.InfoHash)

					linkedDone := active.done
					linkedErr := active.err

					newActive := &activeDownload{
						job:        job,
						infoHash:   active.infoHash,
						torrent:    active.torrent,
						startTime:  active.startTime,
						done:       linkedDone,
						cancelFunc: func() {},
					}
					w.activeJobs[job.ID] = newActive
					w.mu.Unlock()

					_ = w.updateJobStatus(job.ID, db.DownloadStatusDownloading)

					go func(done <-chan struct{}, err error) {
						select {
						case <-done:
							w.handleCompletion(job.ID, err)
						case <-w.ctx.Done():
							w.handleCancellation(job.ID)
						}
					}(linkedDone, linkedErr)
					return
				}
			}
		}
	}
	w.mu.Unlock()

	_, cancel := context.WithCancel(w.ctx)
	active := &activeDownload{
		job:        job,
		startTime:  time.Now(),
		done:       make(chan struct{}),
		cancelFunc: cancel,
	}

	w.mu.Lock()
	w.activeJobs[job.ID] = active
	w.mu.Unlock()

	if err := w.updateJobStatus(job.ID, db.DownloadStatusDownloading); err != nil {
		slog.Error("error updating job status", "error", err)
		w.cleanupJob(job.ID)
		return
	}

	go func() {
		var err error
		switch job.Provider {
		case db.DownloadProviderTorrent:
			err = w.processTorrentDownload(job, active)
		case db.DownloadProviderUsenet:
			err = w.processUsenetDownload(job, active)
		default:
			err = fmt.Errorf("unknown provider: %d", job.Provider)
		}

		active.err = err
		close(active.done)
	}()

	go func() {
		select {
		case <-active.done:
			w.handleCompletion(job.ID, active.err)
		case <-w.ctx.Done():
			w.handleCancellation(job.ID)
		}
	}()
}

func (w *Worker) processTorrentDownload(job *db.DownloadJob, active *activeDownload) error {
	if w.torrentClient == nil {
		return fmt.Errorf("torrent client not configured")
	}

	var err error

	if job.MagnetURI != "" {
		t, err := w.torrentClient.AddMagnet(job.MagnetURI)
		if err != nil {
			return fmt.Errorf("failed to add magnet: %w", err)
		}
		active.torrent = t
		active.infoHash = t.InfoHash
	}

	if job.InfoHash != "" && active.infoHash == ([20]byte{}) {
		hash, err := parseInfoHash(job.InfoHash)
		if err != nil {
			return fmt.Errorf("invalid info hash: %w", err)
		}
		active.infoHash = hash
	}

	if active.torrent == nil && active.infoHash != ([20]byte{}) {
		active.torrent, err = w.torrentClient.GetTorrent(active.infoHash)
		if err != nil {
			return fmt.Errorf("torrent not found: %w", err)
		}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return fmt.Errorf("cancelled")
		case <-ticker.C:
			if active.torrent == nil {
				continue
			}
			status := active.torrent.GetStatus()

			progress := status.Progress
			active.progress = progress

			if status.State == "seeding" || status.State == "complete" {
				return nil
			}

			if status.State == "error" {
				return fmt.Errorf("download error")
			}
		}
	}
}

func (w *Worker) processUsenetDownload(job *db.DownloadJob, active *activeDownload) error {
	if w.usenetClient == nil {
		return fmt.Errorf("usenet client not configured")
	}

	if err := w.usenetClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to usenet: %w", err)
	}

	var nzbData []byte
	var err error

	if strings.HasPrefix(job.NZBData, "http") {
		resp, err := http.Get(job.NZBData)
		if err != nil {
			return fmt.Errorf("failed to fetch NZB: %w", err)
		}
		defer resp.Body.Close()
		nzbData, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read NZB: %w", err)
		}
	} else {
		nzbData = []byte(job.NZBData)
	}

	nzb, err := usenet.ParseNZB(strings.NewReader(string(nzbData)))
	if err != nil {
		return fmt.Errorf("failed to parse NZB: %w", err)
	}

	destDir := filepath.Join(w.manager.GetConfig().DownloadDir, job.Title)

	path, err := w.usenetClient.DownloadAndAssembleNZB(nzb, destDir, func(p float32) {
		active.progress = p
	})
	if err != nil {
		return fmt.Errorf("failed to download NZB: %w", err)
	}

	active.storagePath = path
	return nil
}

func (w *Worker) updateProgress() {
	w.mu.RLock()
	activeList := make([]*activeDownload, 0, len(w.activeJobs))
	for _, a := range w.activeJobs {
		activeList = append(activeList, a)
	}
	w.mu.RUnlock()

	for _, active := range activeList {
		job := active.job

		var bytesDone, bytesTotal uint64
		var progress float32

		if active.torrent != nil && w.torrentClient != nil {
			status := active.torrent.GetStatus()
			bytesDone = uint64(status.BytesDone)
			bytesTotal = uint64(status.BytesTotal)
			progress = status.Progress
		}

		if err := w.updateJobProgress(job.ID, bytesDone, bytesTotal, progress); err != nil {
			slog.Debug("error updating progress", "job_id", job.ID, "error", err)
		}
	}
}

func (w *Worker) handleCompletion(jobID uint32, err error) {
	w.mu.Lock()
	active, ok := w.activeJobs[jobID]
	w.mu.Unlock()

	if !ok {
		return
	}

	active.cancelFunc()

	if err != nil {
		slog.Info("download job failed", "job_id", jobID, "error", err)
		_ = w.updateJobStatus(jobID, db.DownloadStatusFailed)

		table, err := w.db.Downloads()
		if err == nil {
			job, jobErr := table.Get(jobID)
			if jobErr == nil {
				job.ErrorMsg = err.Error()
				job.UpdatedAt = time.Now()
				_ = table.Update(jobID, job)
			}
		}
	} else {
		slog.Info("download job completed", "job_id", jobID)
		_ = w.updateJobStatus(jobID, db.DownloadStatusSeeding)

		table, err := w.db.Downloads()
		if err == nil {
			job, jobErr := table.Get(jobID)
			if jobErr == nil {
				job.CompletedAt = time.Now()
				job.UpdatedAt = time.Now()
				_ = table.Update(jobID, job)

				go w.organizeJob(job, active)
			}
		}
	}

	w.cleanupJob(jobID)
}

func (w *Worker) organizeJob(job *db.DownloadJob, active *activeDownload) {
	slog.Info("organizing job", "id", job.ID, "title", job.Title)

	var storagePath string
	if active.storagePath != "" {
		storagePath = active.storagePath
	} else if active.torrent != nil {
		if len(active.torrent.Files) > 0 {
			storagePath = filepath.Dir(active.torrent.Files[0].Path)
		}
	}

	if storagePath == "" {
		slog.Error("cannot organize job: storage path not found", "id", job.ID)
		return
	}

	cfg := w.manager.GetConfig()

	switch job.MediaType {
	case db.MediaTypeMovie:
		w.organizeMovie(job, active, storagePath, cfg.Library.Movies)

	case db.MediaTypeTV:
		w.organizeEpisode(job, active, storagePath, cfg.Library.TV)
	}

	w.updateJobOrganized(job.ID, true)
}

func (w *Worker) organizeMovie(job *db.DownloadJob, active *activeDownload, sourceDir, destDir string) {
	table, err := w.db.Movies()
	if err != nil {
		return
	}

	var movie *db.Movie
	if job.MediaID > 0 {
		movie, err = table.Get(job.MediaID)
	} else {
		info := w.organizer.DetectMedia(job.Title)
		movies, _ := table.Query("Title", info.Title)
		if len(movies) > 0 {
			movie = &movies[0]
		}
	}

	if movie == nil {
		files := w.organizer.FindMediaFiles(sourceDir)
		if len(files) > 0 {
			info := w.organizer.DetectMedia(filepath.Base(files[0]))
			movie = &db.Movie{
				Title:  info.Title,
				Year:   info.Year,
				Status: db.MediaStatusQueued,
			}
			if id, err := table.Insert(movie); err == nil {
				movie.ID = id
			}
		}
	}

	if movie == nil {
		slog.Warn("cannot organize movie: item not found", "title", job.Title)
		return
	}

	result := w.organizer.OrganizeMovie(movie, sourceDir, destDir, w.organizeOpts)
	if !result.Success {
		if result.Error != nil {
			slog.Error("failed to organize movie", "error", result.Error)
		}
		return
	}

	slog.Info("organized movie", "title", movie.Title, "path", result.DestPath)

	allMovies, _ := table.Filter(func(m db.Movie) bool {
		return m.TMDBID == movie.TMDBID && m.Status != db.MediaStatusAvailable
	})
	for _, m := range allMovies {
		m.Status = db.MediaStatusAvailable
		m.Path = movie.Path
		m.UpdatedAt = time.Now()
		_ = table.Update(m.ID, &m)
	}

	if active.torrent != nil && active.infoHash != ([20]byte{}) {
		w.stopSeeding(active.infoHash)
	}
}

func (w *Worker) organizeEpisode(job *db.DownloadJob, active *activeDownload, sourceDir, destDir string) {
	table, err := w.db.TVEpisodes()
	if err != nil {
		return
	}

	var episode *db.TVEpisode
	if job.MediaID > 0 {
		episode, err = table.Get(job.MediaID)
	} else {
		info := w.organizer.DetectMedia(job.Title)
		if info.Season > 0 && info.Episode > 0 {
			eps, _ := table.Query("Season", info.Season)
			for _, e := range eps {
				if e.Episode == info.Episode {
					episode = &e
					break
				}
			}
		}
	}

	if episode == nil {
		info := w.organizer.DetectMedia(job.Title)
		if info.Season > 0 && info.Episode > 0 {
			episode = &db.TVEpisode{
				Season:  info.Season,
				Episode: info.Episode,
				Status:  db.MediaStatusQueued,
			}
			if id, err := table.Insert(episode); err == nil {
				episode.ID = id
			}
		}
	}

	if episode == nil {
		slog.Warn("cannot organize episode: item not found", "title", job.Title)
		return
	}

	showTable, err := w.db.TVShows()
	if err != nil {
		return
	}
	show, err := showTable.Get(episode.ShowID)
	if err != nil {
		slog.Warn("cannot organize episode: show not found", "show_id", episode.ShowID)
		return
	}

	result := w.organizer.OrganizeEpisode(show, episode, sourceDir, destDir, w.organizeOpts)
	if !result.Success {
		slog.Error("failed to organize episode", "error", result.Error)
		return
	}

	slog.Info("organized episode", "show", show.Title, "path", result.DestPath)

	allEpisodes, _ := table.Filter(func(e db.TVEpisode) bool {
		if e.Season != episode.Season || e.Episode != episode.Episode || e.Status == db.MediaStatusAvailable {
			return false
		}
		s, _ := showTable.Get(e.ShowID)
		return s != nil && s.TMDBID == show.TMDBID
	})
	for _, e := range allEpisodes {
		e.Status = db.MediaStatusAvailable
		e.Path = episode.Path
		e.UpdatedAt = time.Now()
		_ = table.Update(e.ID, &e)
	}

	if active.torrent != nil && active.infoHash != ([20]byte{}) {
		w.stopSeeding(active.infoHash)
	}
}

func (w *Worker) stopSeeding(infoHash [20]byte) {
	if w.torrentClient == nil {
		return
	}

	if err := w.torrentClient.RemoveTorrent(infoHash); err != nil {
		slog.Debug("failed to stop seeding", "infohash", fmt.Sprintf("%x", infoHash), "error", err)
	}
}

func (w *Worker) updateJobOrganized(jobID uint32, organized bool) {
	table, err := w.db.Downloads()
	if err != nil {
		return
	}

	job, err := table.Get(jobID)
	if err != nil {
		return
	}

	job.UpdatedAt = time.Now()
	_ = table.Update(jobID, job)
}

func (w *Worker) handleCancellation(jobID uint32) {
	w.mu.Lock()
	active, ok := w.activeJobs[jobID]
	w.mu.Unlock()

	if !ok {
		return
	}

	active.cancelFunc()

	if active.infoHash != ([20]byte{}) && w.torrentClient != nil {
		w.torrentClient.RemoveTorrent(active.infoHash)
	}

	_ = w.updateJobStatus(jobID, db.DownloadStatusQueued)

	w.cleanupJob(jobID)
}

func (w *Worker) cleanupJob(jobID uint32) {
	w.mu.Lock()
	delete(w.activeJobs, jobID)
	w.mu.Unlock()
}

func (w *Worker) PauseDownload(jobID uint32) error {
	w.mu.RLock()
	active, ok := w.activeJobs[jobID]
	w.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job not found")
	}

	active.cancelFunc()

	return w.updateJobStatus(jobID, db.DownloadStatusPaused)
}

func (w *Worker) ResumeDownload(jobID uint32) error {
	table, err := w.db.Downloads()
	if err != nil {
		return err
	}

	job, err := table.Get(jobID)
	if err != nil {
		return err
	}

	w.mu.RLock()
	_, alreadyActive := w.activeJobs[jobID]
	w.mu.RUnlock()

	if alreadyActive {
		return nil
	}

	w.startDownload(job)
	return nil
}

func (w *Worker) CancelDownload(jobID uint32) error {
	w.mu.RLock()
	active, ok := w.activeJobs[jobID]
	w.mu.RUnlock()

	if ok {
		active.cancelFunc()
	}

	if active != nil && active.infoHash != ([20]byte{}) && w.torrentClient != nil {
		w.torrentClient.RemoveTorrent(active.infoHash)
	}

	w.cleanupJob(jobID)

	table, err := w.db.Downloads()
	if err != nil {
		return err
	}

	return table.Delete(jobID)
}

func (w *Worker) GetActiveJobs() map[uint32]*activeDownload {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make(map[uint32]*activeDownload)
	for k, v := range w.activeJobs {
		result[k] = v
	}
	return result
}

func (w *Worker) StopSeeding(jobID uint32) error {
	w.mu.RLock()
	active, ok := w.activeJobs[jobID]
	w.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job not found")
	}

	if active.infoHash != ([20]byte{}) && w.torrentClient != nil {
		return w.torrentClient.RemoveTorrent(active.infoHash)
	}

	return nil
}

func (w *Worker) GetStoragePath(jobID uint32) string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	active, ok := w.activeJobs[jobID]
	if !ok {
		return ""
	}

	if active.storagePath != "" {
		return active.storagePath
	}

	if active.torrent != nil && len(active.torrent.Files) > 0 {
		return filepath.Dir(active.torrent.Files[0].Path)
	}

	return ""
}

func (w *Worker) IsOrganizeComplete(jobID uint32) bool {
	w.mu.RLock()
	active, ok := w.activeJobs[jobID]
	w.mu.RUnlock()

	if !ok {
		return false
	}

	if active.storagePath == "" {
		return false
	}

	info, err := os.Stat(active.storagePath)
	if err != nil {
		return false
	}

	if info.IsDir() {
		files := w.organizer.FindMediaFiles(active.storagePath)
		return len(files) > 0
	}

	return true
}
