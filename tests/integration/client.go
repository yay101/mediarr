package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type APIClient struct {
	BaseURL   string
	Token     string
	Client    *http.Client
	DebugMode bool
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *APIClient) SetToken(token string) {
	c.Token = token
}

func (c *APIClient) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.Client.Do(req)
}

func (c *APIClient) Get(path string) (*http.Response, error) {
	return c.doRequest(http.MethodGet, path, nil)
}

func (c *APIClient) Post(path string, body interface{}) (*http.Response, error) {
	return c.doRequest(http.MethodPost, path, body)
}

func (c *APIClient) Put(path string, body interface{}) (*http.Response, error) {
	return c.doRequest(http.MethodPut, path, body)
}

func (c *APIClient) Delete(path string) (*http.Response, error) {
	return c.doRequest(http.MethodDelete, path, nil)
}

func (c *APIClient) ParseResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	return json.Unmarshal(body, v)
}

func (c *APIClient) AuthLogin(username, password string) (map[string]interface{}, error) {
	resp, err := c.Post("/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	if token, ok := result["token"].(string); ok {
		c.Token = token
	}

	return result, nil
}

type AddMediaRequest struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Year        uint16 `json:"year"`
	TMDBID      uint32 `json:"tmdb_id,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	ExternalSrc string `json:"external_src,omitempty"`
	Quality     string `json:"quality,omitempty"`
}

type AddMediaResponse struct {
	ID     uint32 `json:"id"`
	Status string `json:"status"`
}

func (c *APIClient) AddMedia(req AddMediaRequest) (*AddMediaResponse, error) {
	resp, err := c.Post("/api/v1/media", req)
	if err != nil {
		return nil, err
	}

	var result AddMediaResponse
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type MediaItem struct {
	ID     uint32 `json:"ID"`
	Title  string `json:"Title"`
	Year   uint16 `json:"Year"`
	Status uint8  `json:"Status"`
	Path   string `json:"Path"`
}

type MediaListResponse struct {
	Movies  []MediaItem `json:"movies"`
	TVShows []MediaItem `json:"tv_shows"`
	Music   []MediaItem `json:"music"`
	Books   []MediaItem `json:"books"`
	Manga   []MediaItem `json:"manga"`
}

func (c *APIClient) GetMedia(mediaType string) (*MediaListResponse, error) {
	resp, err := c.Get(fmt.Sprintf("/api/v1/media?type=%s", mediaType))
	if err != nil {
		return nil, err
	}

	var result MediaListResponse
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *APIClient) DeleteMedia(mediaType string, id uint32) error {
	resp, err := c.Delete(fmt.Sprintf("/api/v1/media/%s/%d", mediaType, id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

type SearchMetadataResponse struct {
	Type    string      `json:"type"`
	Results interface{} `json:"results"`
}

func (c *APIClient) SearchMetadata(query string) (*SearchMetadataResponse, error) {
	resp, err := c.Get(fmt.Sprintf("/api/v1/search?q=%s", query))
	if err != nil {
		return nil, err
	}

	var result SearchMetadataResponse
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type ManualSearchRequest struct {
	Query   string `json:"query"`
	Type    string `json:"type"`
	MediaID uint32 `json:"media_id,omitempty"`
	Quality string `json:"quality,omitempty"`
}

type ManualSearchResponse struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	Status    string `json:"status"`
}

func (c *APIClient) StartManualSearch(req ManualSearchRequest) (*ManualSearchResponse, error) {
	resp, err := c.Post("/api/v1/search/manual", req)
	if err != nil {
		return nil, err
	}

	var result ManualSearchResponse
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type SearchResultItem struct {
	Guid        string `json:"Guid"`
	Title       string `json:"Title"`
	Size        int64  `json:"Size"`
	Seeders     int    `json:"Seeders"`
	Leechers    int    `json:"Leechers"`
	Quality     string `json:"Quality"`
	Resolution  string `json:"Resolution"`
	Indexer     string `json:"Indexer"`
	IsFreeleech bool   `json:"IsFreeleech"`
	IsRepack    bool   `json:"IsRepack"`
}

type ManualSearchResultsResponse struct {
	SessionID string             `json:"session_id"`
	Query     string             `json:"query"`
	Results   []SearchResultItem `json:"results"`
}

func (c *APIClient) GetManualSearchResults(sessionID string) (*ManualSearchResultsResponse, error) {
	resp, err := c.Get(fmt.Sprintf("/api/v1/search/manual/%s", sessionID))
	if err != nil {
		return nil, err
	}

	var result ManualSearchResultsResponse
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type DownloadRequest struct {
	SessionID string      `json:"session_id,omitempty"`
	ResultIdx int         `json:"result_index,omitempty"`
	MediaID   uint32      `json:"media_id,omitempty"`
	MediaType string      `json:"media_type,omitempty"`
	Title     string      `json:"title"`
	Quality   string      `json:"quality,omitempty"`
	Force     bool        `json:"force,omitempty"`
	Result    interface{} `json:"result,omitempty"`
}

type DownloadResponse struct {
	JobID  uint32 `json:"job_id"`
	Status string `json:"status"`
}

func (c *APIClient) DownloadSearchResult(req DownloadRequest) (*DownloadResponse, error) {
	resp, err := c.Post("/api/v1/search/manual/download", req)
	if err != nil {
		return nil, err
	}

	var result DownloadResponse
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type DownloadJob struct {
	ID       uint32  `json:"ID"`
	Title    string  `json:"Title"`
	Status   uint8   `json:"Status"`
	Progress float32 `json:"Progress"`
}

func (c *APIClient) GetDownloads() ([]DownloadJob, error) {
	resp, err := c.Get("/api/v1/downloads")
	if err != nil {
		return nil, err
	}

	var result []DownloadJob
	if err := c.ParseResponse(resp, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *APIClient) CancelDownload(id uint32) error {
	resp, err := c.Delete(fmt.Sprintf("/api/v1/downloads/%d", id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *APIClient) ClearSearchSession(sessionID string) error {
	resp, err := c.Delete(fmt.Sprintf("/api/v1/search/manual/%s", sessionID))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
