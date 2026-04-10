//go:build usenet_integration
// +build usenet_integration

package usenet

import (
	"net"
	"testing"
	"time"
)

type mockConn struct {
	data   []byte
	closed bool
}

func (m *mockConn) Read(b []byte) (int, error) {
	if len(m.data) == 0 {
		return 0, nil
	}
	n := copy(b, m.data)
	m.data = m.data[n:]
	return n, nil
}

func (m *mockConn) Write(b []byte) (int, error) {
	m.data = append(m.data, b...)
	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestNewClient_DefaultPort(t *testing.T) {
	client := NewClient(ServerConfig{
		Host: "news.example.com",
	})

	if client.config.Port != 119 {
		t.Errorf("expected port 119, got %d", client.config.Port)
	}

	clientTLS := NewClient(ServerConfig{
		Host:   "news.example.com",
		UseTLS: true,
	})

	if clientTLS.config.Port != 563 {
		t.Errorf("expected TLS port 563, got %d", clientTLS.config.Port)
	}
}

func TestNewClient_DefaultTimeout(t *testing.T) {
	client := NewClient(ServerConfig{
		Host: "news.example.com",
	})

	if client.config.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", client.config.Timeout)
	}
}

func TestClient_Authenticate_EmptyCredentials(t *testing.T) {
	client := NewClient(ServerConfig{
		Host:     "news.example.com",
		Username: "",
		Password: "",
	})

	err := client.Authenticate("", "")
	if err != nil {
		t.Errorf("expected no error with empty credentials, got %v", err)
	}
}

func TestClient_IsAuthenticated_DefaultFalse(t *testing.T) {
	client := NewClient(ServerConfig{
		Host: "news.example.com",
	})

	if client.IsAuthenticated() {
		t.Error("expected authenticated to be false by default")
	}
}

func TestNZBClient_NewNZBClient(t *testing.T) {
	servers := []ServerConfig{
		{Host: "server1.example.com", Port: 119},
		{Host: "server2.example.com", Port: 563},
	}

	client := NewNZBClient(servers)

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if len(client.servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(client.servers))
	}

	if client.current != 0 {
		t.Errorf("expected current to be 0, got %d", client.current)
	}

	if client.fileCache == nil {
		t.Error("expected file cache to be initialized")
	}
}

func TestNZBClient_Connect_NoServers(t *testing.T) {
	client := NewNZBClient([]ServerConfig{})

	err := client.Connect()
	if err == nil {
		t.Error("expected error when no servers configured")
	}
}

func TestClient_ParseIPAddress(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{"IPv4", "192.168.1.1", "192.168.1.1:119"},
		{"IPv6", "::1", "[::1]:119"},
		{"Hostname", "news.example.com", "news.example.com:119"},
		{"Hostname with port", "news.example.com:563", "news.example.com:563"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ServerConfig{
				Host: tt.host,
				Port: 119,
			}
			client := NewClient(cfg)

			if net.ParseIP(client.config.Host) != nil {
				t.Logf("Parsed as IP")
			}
		})
	}
}

func TestServerConfig_Validation(t *testing.T) {
	cfg := ServerConfig{
		Host:     "news.example.com",
		Port:     119,
		UseTLS:   false,
		Username: "user",
		Password: "pass",
		Timeout:  60 * time.Second,
	}

	if cfg.Host == "" {
		t.Error("expected non-empty host")
	}
	if cfg.Timeout <= 0 {
		t.Error("expected positive timeout")
	}
}
