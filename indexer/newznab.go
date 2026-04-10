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

type NewznabClient struct {
	config     *IndexerConfig
	httpClient *http.Client
	caps       IndexerCapability
}

func init() {
	Register(IndexerTypeNewznab, func(cfg *IndexerConfig) (Indexer, error) {
		return NewNewznabClient(cfg)
	})
}

func NewNewznabClient(config *IndexerConfig) (*NewznabClient, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("%w: URL is required", ErrInvalidConfig)
	}

	client := &NewznabClient{
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

func (c *NewznabClient) Name() string {
	return c.config.Name
}

func (c *NewznabClient) GetCapabilities() IndexerCapability {
	return c.caps
}

func (c *NewznabClient) GetConfig() *IndexerConfig {
	return c.config
}

func (c *NewznabClient) Search(ctx context.Context, query string, category Category, limit int) ([]SearchResult, error) {
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

func (c *NewznabClient) Test(ctx context.Context) error {
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

	var response newznabResponse
	if err := xml.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != "" {
		return fmt.Errorf("indexer error: %s", response.Error)
	}

	return nil
}

func (c *NewznabClient) fetchCapabilities() error {
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

func (c *NewznabClient) parseCapabilities(body []byte) error {
	var caps newznabCapsResponse
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
		catID := cat.ID
		switch {
		case strings.HasPrefix(catID, "50"):
			c.caps.SupportsTVSearch = true
		case strings.HasPrefix(catID, "20"):
			c.caps.SupportsMovieSearch = true
		case strings.HasPrefix(catID, "30"):
			c.caps.SupportsMusicSearch = true
		case strings.HasPrefix(catID, "70"):
			c.caps.SupportsBookSearch = true
		case strings.HasPrefix(catID, "60"):
			c.caps.SupportsAdultSearch = true
		}
	}

	return nil
}

func (c *NewznabClient) parseResponse(body []byte) ([]SearchResult, error) {
	var response newznabResponse
	if err := xml.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != "" {
		return nil, fmt.Errorf("indexer error: %s", response.Error)
	}

	results := make([]SearchResult, 0, len(response.Items))

	for _, item := range response.Items {
		result := SearchResult{
			Title:    item.Title,
			Link:     item.Link,
			Guid:     item.Guid,
			Category: CategoryAll,
			Indexer:  c.Name(),
		}

		if item.Enclosure != nil {
			result.NZBLink = item.Enclosure.URL
			if size, err := strconv.ParseInt(item.Enclosure.Length, 10, 64); err == nil {
				result.Size = size
			}
		}

		for _, attr := range item.NewznabAttributes {
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
			case "category":
				result.Category = MapTorznabToCategory(attr.Value)
			case "categorydesc":
				result.Category = ParseCategory(attr.Value)
			case "group":
				result.Group = attr.Value
			case "guid":
				result.Guid = attr.Value
			case "pubDate":
				if t, err := time.Parse(time.RFC1123Z, attr.Value); err == nil {
					result.PublishDate = t
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

type newznabResponse struct {
	XMLName xml.Name       `xml:"rss"`
	Channel newznabChannel `xml:"channel"`
	Error   string         `xml:"error"`
	Items   []newznabItem  `xml:"channel>item"`
}

type newznabChannel struct {
	Title string `xml:"title"`
}

type newznabItem struct {
	Title             string            `xml:"title"`
	Link              string            `xml:"link"`
	Guid              string            `xml:"guid"`
	Category          []string          `xml:"category"`
	PublishDate       time.Time         `xml:"pubDate"`
	Enclosure         *newznabEnclosure `xml:"enclosure"`
	NewznabAttributes []newznabAttr     `xml:"newznab:attr"`
}

type newznabEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type newznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type newznabCapsResponse struct {
	XMLName    xml.Name          `xml:"caps"`
	Categories []newznabCategory `xml:"categories>category"`
}

type newznabCategory struct {
	ID      string            `xml:"id,attr"`
	Name    string            `xml:"name,attr"`
	Subcats []newznabCategory `xml:"subcat"`
}
