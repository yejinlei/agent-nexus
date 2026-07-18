package proxy

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProxyType identifies the source/type of the proxy
type ProxyType string

const (
	ProxyTypeCCX    ProxyType = "ccx/Desktop"
	ProxyTypeLocal  ProxyType = "local"
	ProxyTypeCloud  ProxyType = "cloud"
	ProxyTypeManual ProxyType = "manual"
)

// Proxy represents a CCX Desktop / CCX-Switch style protocol proxy
type Proxy struct {
	BaseURL  string            `json:"base_url"`
	APIKey   string            `json:"api_key"`
	Port     int               `json:"port"`
	Source   ProxyType         `json:"source"`
	ModelMap map[string]string `json:"model_map"`
}

// FromFlags creates a Proxy from explicit --url and --key flags.
// Accepts both full URLs and bare host:port strings.
// Returns (nil, nil) if both url and key are empty (caller should fall back to Detect).
func FromFlags(cliURL, cliKey string) (*Proxy, error) {
	if cliURL == "" && cliKey == "" {
		return nil, nil
	}

	if cliURL != "" && cliKey == "" {
		return nil, fmt.Errorf("--key is required when --url is specified")
	}

	baseURL := cliURL
	port := 0
	// Try parsed URL host first
	u, err := url.Parse(cliURL)
	if err == nil && u.Host != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	}
	if port == 0 {
		if idx := strings.LastIndex(cliURL, ":"); idx != -1 {
			// Fallback for bare host:port or unparsable strings.
			// Only take the numeric portion (port may be followed by path like "/v1").
			portStr := cliURL[idx+1:]
			if n, _ := strconv.Atoi(portStr); n != 0 {
				port = n
			} else {
				// portStr is like "9000/v1" - strip non-numeric suffix
				for i, c := range portStr {
					if c < '0' || c > '9' {
						if num, err := strconv.Atoi(portStr[:i]); err == nil {
							port = num
						}
						break
					}
				}
			}
		}
	}
	// Default port based on scheme when not explicitly provided
	if port == 0 {
		if u, err := url.Parse(cliURL); err == nil && u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}

	return &Proxy{
		BaseURL: baseURL,
		APIKey:  cliKey,
		Port:    port,
		Source:  ProxyTypeManual,
		ModelMap: map[string]string{},
	}, nil
}

// Detect scans known locations for CCX Desktop proxy config and returns the proxy settings
func Detect() (*Proxy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	ccxConfig := filepath.Join(home, "AppData", "Roaming", "ccx-desktop", ".config", "config.json")
	ccxEnv := filepath.Join(home, "AppData", "Roaming", "ccx-desktop", ".env")

	var modelMap map[string]string

	if data, err := os.ReadFile(ccxConfig); err == nil {
		var cfg struct {
			ResponsesUpstream []struct {
				ModelMapping map[string]string `json:"modelMapping"`
			} `json:"responsesUpstream"`
			ChatUpstream []struct {
				ModelMapping map[string]string `json:"modelMapping"`
			} `json:"chatUpstream"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			modelMap = make(map[string]string)
			for _, u := range cfg.ResponsesUpstream {
				for k, v := range u.ModelMapping {
					modelMap[k] = v
				}
			}
			for _, u := range cfg.ChatUpstream {
				for k, v := range u.ModelMapping {
					modelMap[k] = v
				}
			}
		}
	}

	port := 3688
	if data, err := os.ReadFile(ccxEnv); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PORT=") {
				portStr := strings.TrimPrefix(line, "PORT=")
				port = parsePort(portStr)
			}
		}
	}

	return &Proxy{
		BaseURL: "http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/v1",
		APIKey:  "ccx-dff3eccc518d9830",
		Port:    port,
		Source:  ProxyTypeCCX,
		ModelMap: modelMap,
	}, nil
}

func parsePort(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}


