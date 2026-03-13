package download

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/download/torrent"
	"github.com/yay101/mediarr/internal/download/usenet"
	"github.com/yay101/mediarr/internal/organize"
)

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
}

type activeDownload struct {
	job         *db.DownloadJob
	infoHash    string
	storagePath string
	startTime   time.Time
	progress    float32
	done        chan struct{}
	err         error
	cancelFunc  context.CancelFunc
}

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
	}
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
	log.Println("Download worker started")
}

func (w *Worker) Stop() {
	log.Println("Stopping download worker...")
	w.cancel()
	w.wg.Wait()
	log.Println("Download worker stopped")
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
		log.Printf("Error fetching queued downloads: %v", err)
		return
	}

	for i := range jobs {
		w.startDownload(&jobs[i])
	}

	w.updateProgress()
}

func (w *Worker) startDownload(job *db.DownloadJob) {
	w.mu.Lock()
	if _, exists := w.activeJobs[job.ID]; exists {
		w.mu.Unlock()
		return
	}

	// Shared Global Pool: Check if another job is already downloading the same thing
	for _, active := range w.activeJobs {
		if active.infoHash != "" && active.infoHash == job.InfoHash {
			slog.Info("linking job to existing active download", "job_id", job.ID, "infohash", job.InfoHash)

			// Reference the existing active state
			sharedActive := active

			newActive := &activeDownload{
				job:        job,
				infoHash:   sharedActive.infoHash,
				startTime:  sharedActive.startTime,
				done:       sharedActive.done,
				cancelFunc: func() {}, // Don't cancel the shared one
			}
			w.activeJobs[job.ID] = newActive
			w.mu.Unlock()

			_ = w.updateJobStatus(job.ID, db.DownloadStatusDownloading)

			go func() {
				select {
				case <-newActive.done:
					w.handleCompletion(job.ID, sharedActive.err)
				case <-w.ctx.Done():
					w.handleCancellation(job.ID)
				}
			}()
			return
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
		log.Printf("Error updating job status: %v", err)
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
	var err error

	if job.MagnetURI != "" {
		active.infoHash, err = w.torrentClient.AddMagnet(job.MagnetURI)
		if err != nil {
			return fmt.Errorf("failed to add magnet: %w", err)
		}
	}

	if job.InfoHash != "" && active.infoHash == "" {
		active.infoHash = job.InfoHash
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return fmt.Errorf("cancelled")
		case <-ticker.C:
			if active.infoHash == "" {
				continue
			}
			status, err := w.torrentClient.GetStatus(active.infoHash)
			if err != nil {
				continue
			}

			progress := float32(status.BytesDone) / float32(status.BytesTotal)
			active.progress = progress

			if status.State == "seeding" || status.State == "complete" {
				return nil
			}

			if status.Error != "" {
				return fmt.Errorf("download error: %s", status.Error)
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

		if active.infoHash != "" && w.torrentClient != nil {
			status, err := w.torrentClient.GetStatus(active.infoHash)
			if err == nil {
				bytesDone = uint64(status.BytesDone)
				bytesTotal = uint64(status.BytesTotal)
				progress = status.Progress
			}
		}

		if err := w.updateJobProgress(job.ID, bytesDone, bytesTotal, progress); err != nil {
			log.Printf("Error updating progress for job %d: %v", job.ID, err)
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
		log.Printf("Download job %d failed: %v", jobID, err)
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
		log.Printf("Download job %d completed successfully", jobID)
		_ = w.updateJobStatus(jobID, db.DownloadStatusComplete)

		table, err := w.db.Downloads()
		if err == nil {
			job, jobErr := table.Get(jobID)
			if jobErr == nil {
				job.CompletedAt = time.Now()
				job.UpdatedAt = time.Now()
				_ = table.Update(jobID, job)

				// Trigger organization
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
	} else if active.infoHash != "" {
		status, err := w.torrentClient.GetStatus(active.infoHash)
		if err == nil {
			storagePath = status.StoragePath
		}
	}

	if storagePath == "" {
		slog.Error("cannot organize job: storage path not found", "id", job.ID)
		return
	}

	cfg := w.manager.GetConfig()

	switch job.MediaType {
	case db.MediaTypeMovie:
		table, err := w.db.Movies()
		if err != nil {
			return
		}

		var movie *db.Movie
		if job.MediaID > 0 {
			movie, err = table.Get(job.MediaID)
		} else {
			// Try to match by title
			info := w.organizer.DetectMedia(job.Title)
			movies, _ := table.Query("Title", info.Title)
			if len(movies) > 0 {
				movie = &movies[0]
			}
		}

		if movie == nil {
			slog.Warn("cannot organize movie: item not found in database", "title", job.Title)
			return
		}

		destDir := cfg.Library.Movies
		err = w.organizer.OrganizeMovie(movie, storagePath, destDir, true)
		if err != nil {
			slog.Error("failed to organize movie", "error", err)
			return
		}

		// Shared Global Pool: Satisfy all other users who want this movie
		allMovies, _ := table.Filter(func(m db.Movie) bool {
			return m.TMDBID == movie.TMDBID && m.Status != db.MediaStatusAvailable
		})
		for _, m := range allMovies {
			m.Status = db.MediaStatusAvailable
			m.Path = movie.Path
			m.UpdatedAt = time.Now()
			_ = table.Update(m.ID, &m)
		}

	case db.MediaTypeTV:
		table, err := w.db.TVEpisodes()
		if err != nil {
			return
		}

		var episode *db.TVEpisode
		if job.MediaID > 0 {
			episode, err = table.Get(job.MediaID)
		} else {
			// Try to match episode
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
			slog.Warn("cannot organize episode: item not found in database", "title", job.Title)
			return
		}

		showTable, err := w.db.TVShows()
		if err != nil {
			return
		}
		show, err := showTable.Get(episode.ShowID)
		if err != nil {
			return
		}

		destDir := cfg.Library.TV
		err = w.organizer.OrganizeEpisode(show, episode, storagePath, destDir, true)
		if err != nil {
			slog.Error("failed to organize episode", "error", err)
			return
		}

		// Shared Global Pool: Satisfy all other users who want this episode
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
	}
}

func (w *Worker) handleCancellation(jobID uint32) {
	w.mu.Lock()
	active, ok := w.activeJobs[jobID]
	w.mu.Unlock()

	if !ok {
		return
	}

	if active.infoHash != "" && w.torrentClient != nil {
		w.torrentClient.RemoveTorrent(active.infoHash)
	}

	_ = w.updateJobStatus(jobID, db.DownloadStatusQueued)

	w.cleanupJob(jobID)
}

func (w *Worker) cleanupJob(jobID uint32) {
	w.mu.Lock()
	active, ok := w.activeJobs[jobID]
	if ok {
		active.cancelFunc()
		// Only close done if it's not a shared channel (or handle carefully)
		// For simplicity, we check if it's the primary job
		// But closing a shared channel is fine if all listeners are prepared
	}
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

	if active.infoHash != "" && w.torrentClient != nil {
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
