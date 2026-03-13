package rss

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yay101/mediarr/internal/indexer"
)

type Feed struct {
	Channel Channel `xml:"channel"`
}

type Channel struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Items       []Item `xml:"item"`
}

type Item struct {
	Title       string        `xml:"title"`
	Link        string        `xml:"link"`
	GUID        string        `xml:"guid"`
	Description string        `xml:"description"`
	PubDate     string        `xml:"pubDate"`
	Enclosure   *Enclosure    `xml:"enclosure"`
	Torznab     []TorznabAttr `xml:"attr"`
}

type TorznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type Enclosure struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Length int64  `xml:"length,attr"`
}

type FeedItem struct {
	Title       string
	Link        string
	GUID        string
	Description string
	PublishDate time.Time
	Size        int64
	URL         string
	MagnetURI   string
	TorrentURL  string
	NZBLink     string
	Categories  []indexer.Category
	Seeders     int
	Leechers    int
	Grabs       int
	InfoHash    string
	Quality     string
	Resolution  string
	Codec       string
}

func Parse(data []byte) (*Feed, error) {
	var feed Feed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("failed to parse RSS: %w", err)
	}
	return &feed, nil
}

func ParseFeed(data []byte) ([]FeedItem, error) {
	feed, err := Parse(data)
	if err != nil {
		return nil, err
	}

	items := make([]FeedItem, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		feedItem := convertItem(item)
		items = append(items, feedItem)
	}

	return items, nil
}

func convertItem(item Item) FeedItem {
	feedItem := FeedItem{
		Title:       item.Title,
		Link:        item.Link,
		GUID:        item.GUID,
		Description: item.Description,
	}

	if item.PubDate != "" {
		if t, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
			feedItem.PublishDate = t
		} else if t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", item.PubDate); err == nil {
			feedItem.PublishDate = t
		}
	}

	if item.Enclosure != nil {
		feedItem.URL = item.Enclosure.URL
		feedItem.Size = item.Enclosure.Length
		if item.Enclosure.Type == "application/x-bittorrent" {
			feedItem.TorrentURL = item.Enclosure.URL
		} else if item.Enclosure.Type == "application/x-nzb" {
			feedItem.NZBLink = item.Enclosure.URL
		}
	}

	for _, attr := range item.Torznab {
		name := attr.Name
		value := attr.Value

		switch name {
		case "category":
			if cat := parseCategory(value); cat != indexer.CategoryAll {
				feedItem.Categories = append(feedItem.Categories, cat)
			}
		case "categoryDesc":
			if cat := indexer.ParseCategory(value); cat != indexer.CategoryAll {
				feedItem.Categories = append(feedItem.Categories, cat)
			}
		case "seeds":
			fmt.Sscanf(value, "%d", &feedItem.Seeders)
		case "peers":
			fmt.Sscanf(value, "%d", &feedItem.Leechers)
		case "grabs":
			fmt.Sscanf(value, "%d", &feedItem.Grabs)
		case "size":
			fmt.Sscanf(value, "%d", &feedItem.Size)
		case "infohash":
			feedItem.InfoHash = value
		case "magneturl":
			feedItem.MagnetURI = value
		case "downloadurl":
			feedItem.TorrentURL = value
		case "quality":
			feedItem.Quality = value
		case "resolution":
			feedItem.Resolution = value
		case "codec":
			feedItem.Codec = value
		}
	}

	return feedItem
}

func parseCategory(catStr string) indexer.Category {
	var cat int
	if _, err := fmt.Sscanf(catStr, "%d", &cat); err != nil {
		return indexer.CategoryAll
	}
	return indexer.MapTorznabToCategory(fmt.Sprintf("%d", cat))
}

type AtomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Link    []AtomLink  `xml:"link"`
	Entries []AtomEntry `xml:"entry"`
}

type AtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type AtomEntry struct {
	Title   string     `xml:"title"`
	Link    []AtomLink `xml:"link"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content"`
}

func ParseAtom(data []byte) ([]FeedItem, error) {
	var feed AtomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("failed to parse Atom feed: %w", err)
	}

	items := make([]FeedItem, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		item := FeedItem{
			Title:       entry.Title,
			GUID:        entry.ID,
			Description: entry.Summary,
		}

		for _, link := range entry.Link {
			if link.Rel == "enclosure" || link.Type != "" {
				item.URL = link.Href
				if link.Type == "application/x-bittorrent" {
					item.TorrentURL = link.Href
				}
			}
			if link.Rel == "alternate" {
				item.Link = link.Href
			}
		}

		if entry.Updated != "" {
			if t, err := time.Parse(time.RFC3339, entry.Updated); err == nil {
				item.PublishDate = t
			}
		}

		items = append(items, item)
	}

	return items, nil
}

func FetchAndParse(url string) ([]FeedItem, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if isAtom(data) {
		return ParseAtom(data)
	}

	return ParseFeed(data)
}

func isAtom(data []byte) bool {
	return len(data) > 5 && string(data[1:5]) == "feed"
}
