package indexer

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type TorznabClient struct {
	config     *IndexerConfig
	httpClient *http.Client
	caps       IndexerCapability
}

func init() {
	Register(IndexerTypeTorznab, func(cfg *IndexerConfig) (Indexer, error) {
		return NewTorznabClient(cfg)
	})
}

func NewTorznabClient(config *IndexerConfig) (*TorznabClient, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("%w: URL is required", ErrInvalidConfig)
	}

	client := &TorznabClient{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	if err := client.fetchCapabilities(); err != nil {
		return nil, fmt.Errorf("failed to fetch capabilities: %w", err)
	}

	return client, nil
}

func (c *TorznabClient) Name() string {
	return c.config.Name
}

func (c *TorznabClient) GetCapabilities() IndexerCapability {
	return c.caps
}

func (c *TorznabClient) GetConfig() *IndexerConfig {
	return c.config
}

func (c *TorznabClient) Search(ctx context.Context, query string, category Category, limit int) ([]SearchResult, error) {
	baseURL, err := url.Parse(c.config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	queryParams := baseURL.Query()
	queryParams.Set("apikey", c.config.APIKey)
	queryParams.Set("t", "search")
	queryParams.Set("q", query)

	if limit > 0 {
		queryParams.Set("limit", strconv.Itoa(limit))
	}

	if category != CategoryAll {
		catIDs := MapCategoryToTorznab(category)
		if len(catIDs) > 0 {
			queryParams.Set("cat", strings.Join(catIDs, ","))
		}
	}

	baseURL.RawQuery = queryParams.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return c.parseResponse(body)
}

func (c *TorznabClient) Test(ctx context.Context) error {
	baseURL, err := url.Parse(c.config.URL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	queryParams := baseURL.Query()
	queryParams.Set("apikey", c.config.APIKey)
	queryParams.Set("t", "search")
	queryParams.Set("q", "test")
	queryParams.Set("limit", "1")

	baseURL.RawQuery = queryParams.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: invalid API key", ErrTestFailed)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status code %d", ErrTestFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var response torznabResponse
	if err := xml.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != "" {
		return fmt.Errorf("indexer error: %s", response.Error)
	}

	return nil
}

func (c *TorznabClient) fetchCapabilities() error {
	baseURL, err := url.Parse(c.config.URL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	queryParams := baseURL.Query()
	queryParams.Set("apikey", c.config.APIKey)
	queryParams.Set("t", "caps")

	baseURL.RawQuery = queryParams.Encode()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	return c.parseCapabilities(body)
}

func (c *TorznabClient) parseCapabilities(body []byte) error {
	var caps torznabCapsResponse
	if err := xml.Unmarshal(body, &caps); err != nil {
		return fmt.Errorf("failed to parse capabilities: %w", err)
	}

	c.caps = IndexerCapability{
		SupportsTVSearch:     true,
		SupportsMovieSearch:  true,
		SupportsMusicSearch:  true,
		SupportsBookSearch:   true,
		SupportsAnimeSearch:  true,
		SupportsGameSearch:   true,
		SupportsAdultSearch:  true,
		SupportsRssSearch:    true,
		SupportsAudioSearch:  true,
		SupportsManualSearch: true,
		SupportsApiKey:       true,
		SupportedCategories:  []Category{},
	}

	for _, cat := range caps.Categories {
		if cat.ID == "5000" {
			c.caps.SupportsTVSearch = true
		}
		if cat.ID == "2000" {
			c.caps.SupportsMovieSearch = true
		}
		if cat.ID == "3000" {
			c.caps.SupportsMusicSearch = true
		}
		if cat.ID == "7000" {
			c.caps.SupportsBookSearch = true
		}
		if cat.ID == "5070" {
			c.caps.SupportsAnimeSearch = true
		}
	}

	return nil
}

func (c *TorznabClient) parseResponse(body []byte) ([]SearchResult, error) {
	var response torznabResponse
	if err := xml.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != "" {
		return nil, fmt.Errorf("indexer error: %s", response.Error)
	}

	results := make([]SearchResult, 0, len(response.Items))

	for _, item := range response.Items {
		result := SearchResult{
			Title:       item.Title,
			Link:        item.Link,
			Guid:        item.Guid,
			PublishDate: item.PublishDate,
			Indexer:     c.Name(),
		}

		if item.Enclosure != nil {
			result.Quality = item.Enclosure.Attributes().Get("type")
			if size, err := strconv.ParseInt(item.Enclosure.Length, 10, 64); err == nil {
				result.Size = size
			}
			result.TorrentURL = item.Enclosure.URL
		}

		if item.TorznabAttributes != nil {
			for _, attr := range item.TorznabAttributes {
				switch attr.Name {
				case "size":
					if size, err := strconv.ParseInt(attr.Value, 10, 64); err == nil {
						result.Size = size
					}
				case "files":
					if files, err := strconv.Atoi(attr.Value); err == nil {
						result.Files = files
					}
				case "grabs":
					if grabs, err := strconv.Atoi(attr.Value); err == nil {
						result.Grabs = grabs
					}
				case "seeders":
					if seeders, err := strconv.Atoi(attr.Value); err == nil {
						result.Seeders = seeders
					}
				case "leechers":
					if leechers, err := strconv.Atoi(attr.Value); err == nil {
						result.Leechers = leechers
					}
				case "infohash":
					result.InfoHash = attr.Value
				case "magneturl":
					result.MagnetURI = attr.Value
				case "category":
					result.Category = MapTorznabToCategory(attr.Value)
				case "categorydesc":
					result.Category = ParseCategory(attr.Value)
				case "group":
					result.Group = attr.Value
				case "filename":
					// Could be used for additional parsing
				case "genre":
					result.Tags = strings.Split(attr.Value, "/")
				case "imdb":
					// Could store for later use
				case "tvairdate":
					// Could parse for display
				case "tvdbid":
					// Could store for later use
				case "tvradbid":
					// Could store for later use
				case "tvmazeid":
					// Could store for later use
				case "season":
					result.Season = attr.Value
				case "episode":
					result.Episode = attr.Value
				}
			}
		}

		for _, cat := range item.Category {
			result.Categories = append(result.Categories, MapTorznabToCategory(cat))
		}

		if result.Category == CategoryAll && len(result.Categories) > 0 {
			result.Category = result.Categories[0]
		}

		results = append(results, result)
	}

	return results, nil
}

type torznabResponse struct {
	XMLName xml.Name       `xml:"rss"`
	Channel torznabChannel `xml:"channel"`
	Error   string         `xml:"error"`
	Items   []torznabItem  `xml:"channel>item"`
}

type torznabChannel struct {
	Title string `xml:"title"`
}

type torznabItem struct {
	Title             string            `xml:"title"`
	Link              string            `xml:"link"`
	Guid              string            `xml:"guid"`
	Category          []string          `xml:"category"`
	PublishDate       time.Time         `xml:"pubDate"`
	Enclosure         *torznabEnclosure `xml:"enclosure"`
	TorznabAttributes []torznabAttr     `xml:"torznab:attr"`
}

type torznabEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

func (e *torznabEnclosure) Attributes() torznabEnclosureAttr {
	if e == nil {
		return torznabEnclosureAttr{}
	}
	return torznabEnclosureAttr{e}
}

type torznabEnclosureAttr struct {
	*torznabEnclosure
}

func (a torznabEnclosureAttr) Get(name string) string {
	if a.torznabEnclosure == nil {
		return ""
	}
	switch strings.ToLower(name) {
	case "url":
		return a.URL
	case "type":
		return a.Type
	case "length":
		return a.Length
	}
	return ""
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type torznabCapsResponse struct {
	XMLName    xml.Name          `xml:"caps"`
	Categories []torznabCategory `xml:"categories>category"`
}

type torznabCategory struct {
	ID      string            `xml:"id,attr"`
	Name    string            `xml:"name,attr"`
	Subcats []torznabCategory `xml:"subcat"`
}
