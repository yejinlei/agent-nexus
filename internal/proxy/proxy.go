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
	ProxyTypeCCX       ProxyType = "ccx/Desktop"
	ProxyTypeCCSwitch  ProxyType = "ccx/Switch"
	ProxyTypeLocal     ProxyType = "local"
	ProxyTypeCloud     ProxyType = "cloud"
	ProxyTypeManual    ProxyType = "manual"
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
// Returns (nil, nil) if both url and key are empty.
func FromFlags(cliURL, cliKey string) (*Proxy, error) {
	if cliURL == "" && cliKey == "" {
		return nil, nil
	}

	if cliURL != "" && cliKey == "" {
		return nil, fmt.Errorf("--key is required when --url is specified")
	}

	baseURL := cliURL
	port := 0
	u, err := url.Parse(cliURL)
	if err == nil && u.Host != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	}
	if port == 0 {
		if idx := strings.LastIndex(cliURL, ":"); idx != -1 {
			portStr := cliURL[idx+1:]
			if n, _ := strconv.Atoi(portStr); n != 0 {
				port = n
			} else {
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

// detectCCXDesktop scans known locations for CCX Desktop proxy config
func detectCCXDesktop() (*Proxy, error) {
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

// detectCCSwitch scans known locations for CC-Switch proxy config.
// CC-Switch stores its config at:
//   ~\AppData\Roaming\cc-switch\.config\config.json
//   ~\AppData\Roaming\cc-switch\.env
// It supports model mapping and uses a configurable port (default 3688).
func detectCCSwitch() (*Proxy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	ccswitchConfig := filepath.Join(home, "AppData", "Roaming", "cc-switch", ".config", "config.json")
	ccswitchEnv := filepath.Join(home, "AppData", "Roaming", "cc-switch", ".env")

	// Check if CC-Switch config exists
	if _, err := os.Stat(ccswitchConfig); os.IsNotExist(err) {
		return nil, nil
	}

	var modelMap map[string]string

	if data, err := os.ReadFile(ccswitchConfig); err == nil {
		var cfg struct {
			ResponsesUpstream []struct {
				ModelMapping map[string]string `json:"modelMapping"`
			} `json:"responsesUpstream"`
			ChatUpstream []struct {
				ModelMapping map[string]string `json:"modelMapping"`
			} `json:"chatUpstream"`
			Upstreams []struct {
				ModelMapping map[string]string `json:"modelMapping"`
			} `json:"upstreams"`
			ModelMapping map[string]string `json:"modelMapping"`
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
			for _, u := range cfg.Upstreams {
				for k, v := range u.ModelMapping {
					modelMap[k] = v
				}
			}
			for k, v := range cfg.ModelMapping {
				modelMap[k] = v
			}
		}
	}

	port := 3688
	if data, err := os.ReadFile(ccswitchEnv); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "PORT=") {
				portStr := strings.TrimPrefix(line, "PORT=")
				port = parsePort(portStr)
			}
		}
	}

	return &Proxy{
		BaseURL: "http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/v1",
		APIKey:  "ccswitch-default-key",
		Port:    port,
		Source:  ProxyTypeCCSwitch,
		ModelMap: modelMap,
	}, nil
}

// Detect scans known locations for CCX Desktop and CC-Switch proxy config.
// Priority: CCX Desktop -> CC-Switch -> return nil if neither found.
func Detect() (*Proxy, error) {
	// Try CCX Desktop first
	if p, err := detectCCXDesktop(); err == nil {
		return p, nil
	}

	// Fall back to CC-Switch
	if p, err := detectCCSwitch(); err == nil {
		return p, nil
	}

	return nil, fmt.Errorf("no supported proxy found (CCX Desktop or CC-Switch)")
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

