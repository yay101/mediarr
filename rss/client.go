package rss

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yay101/mediarr/db"
)

type Client struct {
	db     *db.Database
	feeds  map[string]*FeedState
	mu     sync.RWMutex
	client *http.Client
}

type FeedState struct {
	FeedID     uint32
	URL        string
	LastItem   string
	LastUpdate time.Time
}

type FeedConfig struct {
	ID          uint32
	Name        string
	URL         string
	Filter      string
	Interval    time.Duration
	Enabled     bool
	DownloadDir string
	MediaType   db.MediaType
}

func NewClient(database *db.Database) *Client {
	return &Client{
		db:     database,
		feeds:  make(map[string]*FeedState),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) AddFeed(config *FeedConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := &FeedState{
		FeedID: config.ID,
		URL:    config.URL,
	}
	c.feeds[config.URL] = state

	return c.saveFeedConfig(config)
}

func (c *Client) RemoveFeed(url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.feeds, url)

	table, err := c.db.RSSFeeds()
	if err != nil {
		return err
	}

	feeds, _ := table.Filter(func(f db.RSSFeed) bool {
		return f.URL == url
	})

	for _, feed := range feeds {
		table.Delete(feed.ID)
	}

	return nil
}

func (c *Client) CheckFeed(url string) ([]FeedItem, error) {
	items, err := FetchAndParse(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed: %w", err)
	}

	c.mu.Lock()
	state, exists := c.feeds[url]
	c.mu.Unlock()

	if !exists {
		return items, nil
	}

	newItems := make([]FeedItem, 0)
	for _, item := range items {
		if item.GUID != state.LastItem {
			newItems = append(newItems, item)
		}
	}

	if len(items) > 0 {
		state.LastItem = items[0].GUID
		state.LastUpdate = time.Now()
	}

	return newItems, nil
}

func (c *Client) CheckAllFeeds() (map[string][]FeedItem, error) {
	c.mu.RLock()
	urls := make([]string, 0, len(c.feeds))
	for url := range c.feeds {
		urls = append(urls, url)
	}
	c.mu.RUnlock()

	results := make(map[string][]FeedItem)
	for _, url := range urls {
		items, err := c.CheckFeed(url)
		if err != nil {
			continue
		}
		if len(items) > 0 {
			results[url] = items
		}
	}

	return results, nil
}

func (c *Client) GetFeedState(url string) *FeedState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, _ := c.feeds[url]
	return state
}

func (c *Client) ListFeeds() ([]FeedConfig, error) {
	table, err := c.db.RSSFeeds()
	if err != nil {
		return nil, err
	}

	var feeds []db.RSSFeed
	table.Scan(func(f db.RSSFeed) bool {
		feeds = append(feeds, f)
		return true
	})

	configs := make([]FeedConfig, len(feeds))
	for i, feed := range feeds {
		configs[i] = FeedConfig{
			ID:          feed.ID,
			Name:        feed.Name,
			URL:         feed.URL,
			Filter:      feed.Filter,
			Interval:    time.Duration(feed.Interval) * time.Minute,
			Enabled:     feed.Enabled,
			DownloadDir: feed.DownloadDir,
			MediaType:   feed.MediaType,
		}
	}

	return configs, nil
}

func (c *Client) saveFeedConfig(config *FeedConfig) error {
	table, err := c.db.RSSFeeds()
	if err != nil {
		return err
	}

	feed := &db.RSSFeed{
		Name:        config.Name,
		URL:         config.URL,
		Filter:      config.Filter,
		Interval:    uint32(config.Interval.Minutes()),
		Enabled:     config.Enabled,
		DownloadDir: config.DownloadDir,
		MediaType:   config.MediaType,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_, err = table.Insert(feed)
	return err
}

func (c *Client) LoadFeeds() error {
	table, err := c.db.RSSFeeds()
	if err != nil {
		return err
	}

	var feeds []db.RSSFeed
	table.Scan(func(f db.RSSFeed) bool {
		feeds = append(feeds, f)
		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, feed := range feeds {
		c.feeds[feed.URL] = &FeedState{
			FeedID:     feed.ID,
			URL:        feed.URL,
			LastItem:   feed.LastGUID,
			LastUpdate: feed.LastCheck,
		}
	}

	return nil
}
