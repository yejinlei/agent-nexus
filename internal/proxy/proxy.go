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
	u, urlErr := url.Parse(cliURL)
	if urlErr == nil && u.Host != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	}
	if port == 0 {
		if idx := strings.LastIndex(cliURL, ":"); idx != -1 {
			portStr := cliURL[idx+1:]
			for i, c := range portStr {
				if c < '0' || c > '9' {
					portStr = portStr[:i]
					break
				}
			}
			if p, err := parsePort(portStr); err == nil && p != 0 {
				port = p
			}
		}
	}
	if port == 0 {
		if u, urlErr := url.Parse(cliURL); urlErr == nil && u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	return &Proxy{
		BaseURL:  baseURL,
		APIKey:   cliKey,
		Port:     port,
		Source:   ProxyTypeManual,
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
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintln(os.Stderr, "warning: detectCCXDesktop: failed to parse config.json: "+err.Error())
		} else {
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
				if p, err := parsePort(portStr); err == nil {
					port = p
				} else {
					fmt.Fprintln(os.Stderr, "warning: CCX Desktop PORT value is invalid: "+err.Error())
				}
			}
		}
	}
	apiKey, keyFound := readEnvFile(ccxEnv, "API_KEY")
	if !keyFound {
		apiKey, keyFound = readEnvFile(ccxEnv, "OPENAI_API_KEY")
	}
	if !keyFound {
		return nil, fmt.Errorf("CCX Desktop proxy found but no API key configured: check %s", ccxEnv)
	}
	return &Proxy{
		BaseURL:  "http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/v1",
		APIKey:   apiKey,
		Port:     port,
		Source:   ProxyTypeCCX,
		ModelMap: modelMap,
	}, nil
}

// detectCCSwitch scans known locations for CC-Switch proxy config.
func detectCCSwitch() (*Proxy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	ccswitchConfig := filepath.Join(home, "AppData", "Roaming", "cc-switch", ".config", "config.json")
	ccswitchEnv := filepath.Join(home, "AppData", "Roaming", "cc-switch", ".env")
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
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintln(os.Stderr, "warning: detectCCSwitch: failed to parse config.json: "+err.Error())
		} else {
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
			_ = strings.TrimSpace(line)
			if strings.HasPrefix(line, "PORT=") {
				portStr := strings.TrimPrefix(line, "PORT=")
				if p, err := parsePort(portStr); err == nil {
					port = p
				} else {
					fmt.Fprintln(os.Stderr, "warning: CC-Switch PORT value is invalid: "+err.Error())
				}
			}
		}
	}
	apiKey, keyFound := readEnvFile(ccswitchEnv, "API_KEY")
	if !keyFound {
		apiKey, keyFound = readEnvFile(ccswitchEnv, "CCSWITCH_API_KEY")
	}
	if !keyFound {
		return nil, fmt.Errorf("CC-Switch proxy found but no API key configured: check %s", ccswitchEnv)
	}
	return &Proxy{
		BaseURL:  "http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/v1",
		APIKey:   apiKey,
		Port:     port,
		Source:   ProxyTypeCCSwitch,
		ModelMap: modelMap,
	}, nil
}

func Detect() (*Proxy, error) {
	if p, err := detectCCXDesktop(); err == nil {
		return p, nil
	}
	if p, err := detectCCSwitch(); err == nil {
		return p, nil
	}
	return nil, fmt.Errorf("no supported proxy found (CCX Desktop or CC-Switch)")
}

func readEnvFile(path string, key string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+"=")), true
		}
		if strings.HasPrefix(line, key+":=") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+":=")), true
		}
	}
	return "", false
}

// parsePort parses a strict numeric port string.
// Rejects any value that is not purely digits (with optional surrounding whitespace).
// Returns (port, nil) on success or (0, error) if the value contains non-numeric suffixes.
func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid port: %q", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port: %q", s)
	}
	return n, nil
}
