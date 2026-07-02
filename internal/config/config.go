package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultMaxBodySizeMB = 10
	MaxMaxBodySizeMB     = 100
)

type Config struct {
	Relay             string `json:"relay"`
	URLProvider       string `json:"url_provider"`
	APIKey            string `json:"api_key"`
	LocalHost         string `json:"local_host"`
	LocalPort         int    `json:"local_port"`
	AllowExternalHost bool   `json:"allow_external_host"`
	MaxBodySizeMB     int    `json:"max_body_size_mb"`
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, ".sekai-server", "config.json"), nil
}

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func (c *Config) Validate() error {
	if c.Relay == "" {
		return fmt.Errorf("relay is required")
	}
	if c.URLProvider == "" {
		return fmt.Errorf("url_provider is required")
	}
	if c.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}
	if c.LocalPort <= 0 || c.LocalPort > 65535 {
		return fmt.Errorf("local_port must be between 1 and 65535")
	}
	if c.LocalHost == "" {
		c.LocalHost = "localhost"
	}
	if err := c.validateLocalHost(); err != nil {
		return err
	}
	if c.MaxBodySizeMB <= 0 {
		c.MaxBodySizeMB = DefaultMaxBodySizeMB
	}
	if c.MaxBodySizeMB > MaxMaxBodySizeMB {
		return fmt.Errorf("max_body_size_mb no puede superar %d", MaxMaxBodySizeMB)
	}
	return nil
}

func isLinkLocal(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 169 && ip4[1] == 254
	}
	return false
}

func (c *Config) validateLocalHost() error {
	ips, err := net.LookupHost(c.LocalHost)
	if err != nil {
		if c.AllowExternalHost {
			fmt.Fprintf(os.Stderr, "WARNING: cannot resolve local_host %q: %v\n", c.LocalHost, err)
			return nil
		}
		return fmt.Errorf("resolving local_host %q: %w", c.LocalHost, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.Equal(net.ParseIP("169.254.169.254")) {
			return fmt.Errorf("local_host cannot be cloud metadata IP (169.254.169.254)")
		}
		if isLinkLocal(ip) {
			return fmt.Errorf("local_host cannot be a link-local address (169.254.x.x)")
		}
	}

	if c.AllowExternalHost {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.IsPrivate() {
				fmt.Fprintf(os.Stderr, "WARNING: local_host %s (%s) is a private IP — SSRF risk if relay is compromised\n", c.LocalHost, ip.String())
			}
		}
		return nil
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("local_host %q is not a loopback address (use --allow-external-host to allow)", c.LocalHost)
}

func (c *Config) ExtractCode() string {
	path := c.URLProvider
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func (c *Config) RelayHost() string {
	raw := c.Relay
	raw = strings.TrimPrefix(raw, "wss://")
	raw = strings.TrimPrefix(raw, "ws://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if idx := strings.Index(raw, "/"); idx != -1 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, ":"); idx != -1 {
		raw = raw[:idx]
	}
	return raw
}

func (c *Config) RelayScheme() string {
	if strings.HasPrefix(c.Relay, "ws://") || strings.HasPrefix(c.Relay, "http://") {
		return "ws"
	}
	return "wss"
}

func MergeFlags(base *Config, flags map[string]string) {
	if v, ok := flags["relay"]; ok && v != "" {
		base.Relay = v
	}
	if v, ok := flags["url-provider"]; ok && v != "" {
		base.URLProvider = v
	}
	if v, ok := flags["api-key"]; ok && v != "" {
		base.APIKey = v
	}
	if v, ok := flags["local-host"]; ok && v != "" {
		base.LocalHost = v
	}
	if v, ok := flags["local-port"]; ok && v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
			base.LocalPort = p
		}
	}
	if v, ok := flags["config"]; ok && v != "" {
	}
}
