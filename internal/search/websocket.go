package search

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/indexer"
)

const (
	wsWriteWait      = 10 * time.Second
	wsPongWait       = 60 * time.Second
	wsPingPeriod     = wsPongWait * 9 / 10
	wsMaxMessageSize = 512 * 1024
)

type Hub struct {
	manager    *Manager
	sessions   map[string]*WSSession
	conns      map[*websocket.Conn]*WSSession
	mu         sync.RWMutex
	register   chan *WSSession
	unregister chan *websocket.Conn
}

type WSSession struct {
	ID        string
	Query     string
	MediaType db.MediaType
	MediaID   uint32
	Results   []SearchResult
	conn      *websocket.Conn
	Done      chan struct{}
	Cancel    context.CancelFunc
	manager   *Manager
}

type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

func NewHub(mgr *Manager) *Hub {
	return &Hub{
		manager:    mgr,
		sessions:   make(map[string]*WSSession),
		conns:      make(map[*websocket.Conn]*WSSession),
		register:   make(chan *WSSession),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case session := <-h.register:
			h.mu.Lock()
			h.sessions[session.ID] = session
			h.conns[session.conn] = session
			h.mu.Unlock()
			slog.Debug("websocket session registered", "session_id", session.ID)

		case conn := <-h.unregister:
			h.mu.Lock()
			if session, ok := h.conns[conn]; ok {
				delete(h.sessions, session.ID)
				delete(h.conns, conn)
				if session.Cancel != nil {
					session.Cancel()
				}
				h.manager.ClearSession(session.ID)
				slog.Debug("websocket session unregistered", "session_id", session.ID)
			}
			h.mu.Unlock()
			conn.Close()
		}
	}
}

func (h *Hub) HandleSearch(conn *websocket.Conn, query string, mediaType db.MediaType, mediaID uint32) {
	session := &WSSession{
		ID:        generateSessionID(),
		Query:     query,
		MediaType: mediaType,
		MediaID:   mediaID,
		Results:   make([]SearchResult, 0),
		conn:      conn,
		Done:      make(chan struct{}),
		manager:   h.manager,
	}

	h.register <- session
	defer func() {
		h.unregister <- conn
	}()

	session.sendJSON(WSMessage{
		Type: "session_start",
		Payload: map[string]interface{}{
			"session_id": session.ID,
			"query":      session.Query,
		},
	})

	session.startSearch()

	<-session.Done
}

func (s *WSSession) startSearch() {
	ctx, cancel := context.WithCancel(context.Background())
	s.Cancel = cancel
	defer close(s.Done)

	var wg sync.WaitGroup

	s.manager.IndexerManager.mu.RLock()
	indexers := make([]indexer.Indexer, 0, len(s.manager.IndexerManager.Indexers))
	for _, idx := range s.manager.IndexerManager.Indexers {
		if idx.GetConfig().Enabled {
			indexers = append(indexers, idx)
		}
	}
	s.manager.IndexerManager.mu.RUnlock()

	category := s.manager.mediaTypeToCategory(s.MediaType)

	s.sendJSON(WSMessage{
		Type: "search_started",
		Payload: map[string]interface{}{
			"indexers_count": len(indexers),
			"query":          s.Query,
		},
	})

	resultsCh := make(chan SearchResult, 100)

	for _, idx := range indexers {
		wg.Add(1)
		go func(i indexer.Indexer) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			indexerID := i.GetConfig().ID
			indexerName := i.GetConfig().Name

			if !indexer.GlobalSearchLimiter.Allow(indexerID) {
				indexer.GlobalSearchLimiter.Wait(indexerID)
			}

			select {
			case <-ctx.Done():
				return
			default:
			}

			results, err := i.Search(ctx, s.Query, category, 50)
			if err != nil {
				slog.Warn("indexer search failed", "indexer", indexerName, "error", err)
				indexer.GlobalSearchLimiter.RecordFailure(indexerID)
				s.sendJSON(WSMessage{
					Type: "indexer_error",
					Payload: map[string]interface{}{
						"indexer": indexerName,
						"error":   err.Error(),
					},
				})
				return
			}

			indexer.GlobalSearchLimiter.RecordSuccess(indexerID)

			s.sendJSON(WSMessage{
				Type: "indexer_complete",
				Payload: map[string]interface{}{
					"indexer": indexerName,
					"found":   len(results),
				},
			})

			for _, r := range results {
				select {
				case <-ctx.Done():
					return
				case resultsCh <- SearchResult{
					Guid:        r.Guid,
					Title:       r.Title,
					Link:        r.Link,
					Size:        r.Size,
					Seeders:     r.Seeders,
					Leechers:    r.Leechers,
					InfoHash:    r.InfoHash,
					MagnetURI:   r.MagnetURI,
					TorrentURL:  r.TorrentURL,
					Quality:     r.Quality,
					Codec:       r.Codec,
					Resolution:  r.Resolution,
					Indexer:     indexerName,
					IndexerID:   indexerID,
					IsFreeleech: r.IsFreeleech,
					IsRepack:    r.IsRepack,
					PublishDate: r.PublishDate,
					Score:       r.Score,
				}:
				}
			}
		}(idx)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	seenHashes := make(map[string]bool)
	for result := range resultsCh {
		hash := result.InfoHash + result.Guid
		if seenHashes[hash] {
			continue
		}
		seenHashes[hash] = true

		s.Results = append(s.Results, result)
		s.sendJSON(WSMessage{
			Type:    "result",
			Payload: result,
		})
	}

	s.sortResults()

	s.sendJSON(WSMessage{
		Type: "search_complete",
		Payload: map[string]interface{}{
			"total_results": len(s.Results),
			"session_id":    s.ID,
		},
	})
}

func (s *WSSession) sortResults() {
	sort.Slice(s.Results, func(i, j int) bool {
		a, b := s.Results[i], s.Results[j]

		if a.IsFreeleech && !b.IsFreeleech {
			return true
		}
		if !a.IsFreeleech && b.IsFreeleech {
			return false
		}

		if a.Score != b.Score {
			return a.Score > b.Score
		}

		if a.Seeders != b.Seeders {
			return a.Seeders > b.Seeders
		}

		if a.Size != b.Size {
			return a.Size > b.Size
		}

		return a.PublishDate.After(b.PublishDate)
	})
}

func (s *WSSession) sendJSON(msg WSMessage) {
	if s.conn == nil {
		return
	}

	s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	if err := s.conn.WriteJSON(msg); err != nil {
		slog.Debug("failed to send websocket message", "session_id", s.ID, "error", err)
	}
}

func (s *WSSession) GetResult(idx int) *SearchResult {
	s.manager.mu.RLock()
	defer s.manager.mu.RUnlock()

	if idx < 0 || idx >= len(s.Results) {
		return nil
	}
	return &s.Results[idx]
}

func (s *WSSession) GetAllResults() []SearchResult {
	return s.Results
}
