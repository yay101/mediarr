package usenet

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type ServerConfig struct {
	Host     string
	Port     int
	UseTLS   bool
	Username string
	Password string
	Timeout  time.Duration
}

type ArticleInfo struct {
	MessageID string
	Subject   string
	From      string
	Date      time.Time
	Size      int64
	Lines     int
}

type FilePart struct {
	Subject   string
	MessageID string
	Number    int
	Total     int
	Data      []byte
}

type Client struct {
	config ServerConfig
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.RWMutex
	auth   bool
}

func NewClient(cfg ServerConfig) *Client {
	if cfg.Port == 0 {
		if cfg.UseTLS {
			cfg.Port = 563
		} else {
			cfg.Port = 119
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{
		config: cfg,
	}
}

func (c *Client) Connect() error {
	var address string
	if net.ParseIP(c.config.Host) != nil {
		address = fmt.Sprintf("[%s]:%d", c.config.Host, c.config.Port)
	} else {
		address = fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	}

	var conn net.Conn
	var err error

	if c.config.UseTLS {
		tlsConfig := &tls.Config{
			ServerName:         c.config.Host,
			InsecureSkipVerify: false,
		}
		dialer := &net.Dialer{Timeout: c.config.Timeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", address, c.config.Timeout)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to usenet server: %w", err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)

	if _, err := c.readResponse(); err != nil {
		return fmt.Errorf("failed to read welcome: %w", err)
	}

	return nil
}

func (c *Client) Authenticate(username, password string) error {
	if username == "" {
		username = c.config.Username
	}
	if password == "" {
		password = c.config.Password
	}

	if username == "" || password == "" {
		return nil
	}

	authStr := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	if err := c.sendCommand("AUTHINFO USER %s", username); err != nil {
		return err
	}

	resp, err := c.readResponse()
	if err != nil {
		return err
	}

	if !strings.HasPrefix(resp, "381") {
		return fmt.Errorf("AUTHINFO USER failed: %s", resp)
	}

	if err := c.sendCommand("AUTHINFO PASS %s", authStr); err != nil {
		return err
	}

	resp, err = c.readResponse()
	if err != nil {
		return err
	}

	if strings.HasPrefix(resp, "281") {
		c.mu.Lock()
		c.auth = true
		c.mu.Unlock()
		return nil
	}

	return fmt.Errorf("authentication failed: %s", resp)
}

func (c *Client) AuthenticateSasl(username, password string) error {
	if username == "" {
		username = c.config.Username
	}
	if password == "" {
		password = c.config.Password
	}

	authStr := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	if err := c.sendCommand("AUTHINFO SASL %s", authStr); err != nil {
		return err
	}

	resp, err := c.readResponse()
	if err != nil {
		return err
	}

	if strings.HasPrefix(resp, "281") {
		c.mu.Lock()
		c.auth = true
		c.mu.Unlock()
		return nil
	}

	return fmt.Errorf("SASL authentication failed: %s", resp)
}

func (c *Client) ListGroups() ([]string, error) {
	if err := c.sendCommand("LIST"); err != nil {
		return nil, err
	}

	resp, err := c.readResponseMultiLine()
	if err != nil {
		return nil, err
	}

	var groups []string
	scanner := bufio.NewScanner(strings.NewReader(resp))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "211") || line == "." {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			groups = append(groups, parts[0])
		}
	}

	return groups, nil
}

func (c *Client) Group(name string) (int64, error) {
	if err := c.sendCommand("GROUP %s", name); err != nil {
		return 0, err
	}

	resp, err := c.readResponse()
	if err != nil {
		return 0, err
	}

	if !strings.HasPrefix(resp, "211") {
		return 0, fmt.Errorf("group not found: %s", resp)
	}

	parts := strings.Fields(resp)
	if len(parts) >= 2 {
		var count int64
		fmt.Sscanf(parts[1], "%d", &count)
		return count, nil
	}

	return 0, nil
}

func (c *Client) ListActive(group string, limit int) ([]ArticleInfo, error) {
	if err := c.sendCommand("LISTACTIVE %s %d", group, limit); err != nil {
		return nil, err
	}

	resp, err := c.readResponseMultiLine()
	if err != nil {
		return nil, err
	}

	var articles []ArticleInfo
	scanner := bufio.NewScanner(strings.NewReader(resp))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "223") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				articles = append(articles, ArticleInfo{
					MessageID: parts[1],
				})
			}
		}
	}

	return articles, nil
}

func (c *Client) Article(group, articleID string) (map[string]string, []byte, error) {
	var cmd string
	if strings.HasPrefix(articleID, "<") {
		cmd = fmt.Sprintf("ARTICLE %s", articleID)
	} else {
		cmd = fmt.Sprintf("ARTICLE %s %s", group, articleID)
	}

	if err := c.sendRaw(cmd); err != nil {
		return nil, nil, err
	}

	resp, err := c.readResponseMultiLine()
	if err != nil {
		return nil, nil, err
	}

	headerEnd := strings.Index(resp, "\r\n\r\n")
	if headerEnd == -1 {
		headerEnd = strings.Index(resp, "\n\n")
	}

	var headers map[string]string
	var body []byte

	if headerEnd != -1 {
		headerText := resp[:headerEnd]
		bodyText := resp[headerEnd+4:]
		if strings.HasPrefix(bodyText, "\r\n") {
			bodyText = bodyText[2:]
		} else if strings.HasPrefix(bodyText, "\n") {
			bodyText = bodyText[1:]
		}

		headers = parseHeaders(headerText)
		body = []byte(bodyText)
	} else {
		headers = make(map[string]string)
		body = []byte(resp)
	}

	return headers, body, nil
}

func (c *Client) Body(group, articleID string) ([]byte, error) {
	var cmd string
	if strings.HasPrefix(articleID, "<") {
		cmd = fmt.Sprintf("BODY %s", articleID)
	} else {
		cmd = fmt.Sprintf("BODY %s %s", group, articleID)
	}

	if err := c.sendRaw(cmd); err != nil {
		return nil, err
	}

	resp, err := c.readResponseMultiLine()
	if err != nil {
		return nil, err
	}

	return []byte(resp), nil
}

func (c *Client) Head(group, articleID string) (map[string]string, error) {
	var cmd string
	if strings.HasPrefix(articleID, "<") {
		cmd = fmt.Sprintf("HEAD %s", articleID)
	} else {
		cmd = fmt.Sprintf("HEAD %s %s", group, articleID)
	}

	if err := c.sendRaw(cmd); err != nil {
		return nil, err
	}

	resp, err := c.readResponseMultiLine()
	if err != nil {
		return nil, err
	}

	headers := parseHeaders(resp)
	return headers, nil
}

func parseHeaders(headerText string) map[string]string {
	headers := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(headerText))
	for scanner.Scan() {
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 {
			key := strings.TrimSpace(line[:colonIdx])
			value := strings.TrimSpace(line[colonIdx+1:])
			headers[strings.ToLower(key)] = value
		}
	}
	return headers
}

func (c *Client) XOver(start, end int64, group string) ([]ArticleInfo, error) {
	if err := c.sendCommand("XOVER %d-%d", start, end); err != nil {
		return nil, err
	}

	resp, err := c.readResponseMultiLine()
	if err != nil {
		return nil, err
	}

	var articles []ArticleInfo
	scanner := bufio.NewScanner(strings.NewReader(resp))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "224") || line == "." {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 8 {
			articles = append(articles, ArticleInfo{
				MessageID: parts[0],
				Subject:   parts[1],
				From:      parts[2],
				Date:      time.Now(),
				Size:      int64(parseInt(parts[6])),
				Lines:     parseInt(parts[7]),
			})
		}
	}

	return articles, nil
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func (c *Client) sendCommand(format string, args ...interface{}) error {
	cmd := fmt.Sprintf(format, args...)
	return c.sendRaw(cmd)
}

func (c *Client) sendRaw(cmd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	_, err := c.conn.Write([]byte(cmd + "\r\n"))
	return err
}

func (c *Client) readResponse() (string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (c *Client) readResponseMultiLine() (string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	code := line[:3]
	if !strings.HasPrefix(code, "1") && !strings.HasPrefix(code, "2") && !strings.HasPrefix(code, "3") {
		return strings.TrimSpace(line), nil
	}

	var resp strings.Builder
	resp.WriteString(line)

	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		if strings.HasPrefix(line, ".") {
			break
		}

		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}

		resp.WriteString(line)
	}

	return resp.String(), nil
}

func (c *Client) Quit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	c.conn.Write([]byte("QUIT\r\n"))
	c.conn.Close()
	c.conn = nil

	return nil
}

func (c *Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.auth
}

type NZBClient struct {
	servers   []ServerConfig
	current   int
	mu        sync.RWMutex
	fileCache map[string][]FilePart
	client    *Client
}

func NewNZBClient(servers []ServerConfig) *NZBClient {
	return &NZBClient{
		servers:   servers,
		current:   0,
		fileCache: make(map[string][]FilePart),
	}
}

func (c *NZBClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.servers) == 0 {
		return fmt.Errorf("no servers configured")
	}

	for i := 0; i < len(c.servers); i++ {
		server := c.servers[c.current]
		client := NewClient(server)
		if err := client.Connect(); err == nil {
			c.client = client
			return nil
		}
		c.current = (c.current + 1) % len(c.servers)
	}

	return fmt.Errorf("failed to connect to any usenet server")
}

func (c *NZBClient) DownloadSegment(group, messageID string) ([]byte, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	_, data, err := client.Article(group, messageID)
	return data, err
}

func (c *NZBClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Quit()
		c.client = nil
	}
	return nil
}
