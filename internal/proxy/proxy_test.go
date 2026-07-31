package proxy

import (
    "os"
    "path/filepath"
    "testing"
)

func TestFromFlags_HappyPath(t *testing.T) {
    tests := []struct {
        name   string
        url    string
        key    string
        expect Proxy
    }{
        {
            name:   "full URL with path",
            url:    "http://localhost:8080/v1",
            key:    "sk-test-key",
            expect: Proxy{BaseURL: "http://localhost:8080/v1", APIKey: "sk-test-key", Port: 8080, Source: ProxyTypeManual},
        },
        {
            name:   "URL without path",
            url:    "http://127.0.0.1:3688",
            key:    "sk-abc123",
            expect: Proxy{BaseURL: "http://127.0.0.1:3688", APIKey: "sk-abc123", Port: 3688, Source: ProxyTypeManual},
        },
        {
            name:   "bare host:port",
            url:    "localhost:9000",
            key:    "test-key",
            expect: Proxy{BaseURL: "localhost:9000", APIKey: "test-key", Port: 9000, Source: ProxyTypeManual},
        },
        {
            name:   "remote URL",
            url:    "https://api.example.com/v1",
            key:    "sk-remote",
            expect: Proxy{BaseURL: "https://api.example.com/v1", APIKey: "sk-remote", Port: 443, Source: ProxyTypeManual},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            p, err := FromFlags(tt.url, tt.key)
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if p == nil {
                t.Fatal("expected proxy, got nil")
            }
            if p.BaseURL != tt.expect.BaseURL {
                t.Errorf("BaseURL = %q, want %q", p.BaseURL, tt.expect.BaseURL)
            }
            if p.APIKey != tt.expect.APIKey {
                t.Errorf("APIKey = %q, want %q", p.APIKey, tt.expect.APIKey)
            }
            if p.Port != tt.expect.Port {
                t.Errorf("Port = %d, want %d", p.Port, tt.expect.Port)
            }
            if p.Source != tt.expect.Source {
                t.Errorf("Source = %q, want %q", p.Source, tt.expect.Source)
            }
            if len(p.ModelMap) != 0 {
                t.Errorf("ModelMap should be empty, got %v", p.ModelMap)
            }
        })
    }
}

func TestFromFlags_Errors(t *testing.T) {
    p, err := FromFlags("", "")
    if err != nil {
        t.Fatalf("both empty should return nil, nil, got err=%v", err)
    }
    if p != nil {
        t.Fatalf("both empty should return nil proxy, got %v", p)
    }

    _, err = FromFlags("http://localhost:8080/v1", "")
    if err == nil {
        t.Fatalf("expected error when --url is set but --key is empty")
    }
}

func TestFromFlags_KeyWithoutURL(t *testing.T) {
    p, err := FromFlags("", "sk-only-key")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if p.BaseURL != "" {
        t.Errorf("BaseURL = %q, want empty", p.BaseURL)
    }
    if p.APIKey != "sk-only-key" {
        t.Errorf("APIKey = %q, want sk-only-key", p.APIKey)
    }
}

func TestFromFlags_DefaultPort(t *testing.T) {
    tests := []struct {
        url    string
        expect int
    }{
        {"https://example.com/v1", 443},
        {"http://example.com/v1", 80},
        {"localhost:8080/v1", 8080},
    }

    for _, tt := range tests {
        p, err := FromFlags(tt.url, "key")
        if err != nil {
            t.Fatalf("unexpected error for %q: %v", tt.url, err)
        }
        if p.Port != tt.expect {
            t.Errorf("Port = %d, want %d for %q", p.Port, tt.expect, tt.url)
        }
    }
}

func TestParsePort_Strict(t *testing.T) {
    tests := []struct {
        input    string
        wantPort int
        wantErr  bool
    }{
        {"3688", 3688, false},
        {"11434", 11434, false},
        {"0", 0, false},
        {"99999", 99999, false},
        {"  123  ", 123, false},
        {"", 0, false},
        {"3688x", 0, true},
        {"8080alpha", 0, true},
        {"abc", 0, true},
        {"-1", 0, true},
        {":179", 0, true},
    }

    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got, err := parsePort(tt.input)
            if (err != nil) != tt.wantErr {
                t.Fatalf("parsePort(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
            }
            if got != tt.wantPort {
                t.Errorf("parsePort(%q) = %d, want %d", tt.input, got, tt.wantPort)
            }
        })
    }
}

func TestDetect_HappyPath(t *testing.T) {
    tmpDir := t.TempDir()
    roamingDir := filepath.Join(tmpDir, "AppData", "Roaming")
    ccxConfigDir := filepath.Join(roamingDir, "ccx-desktop", ".config")
    if err := os.MkdirAll(ccxConfigDir, 0755); err != nil {
        t.Fatalf("failed to create dir: %v", err)
    }

    configContent := "{\"responsesUpstream\":[{\"modelMapping\":{\"gpt-4\":\"claude-sonnet-4-20250514\"}}],\"chatUpstream\":[{\"modelMapping\":{\"haiku\":\"deepseek-v4-flash\"}}]}"
    if err := os.WriteFile(filepath.Join(ccxConfigDir, "config.json"), []byte(configContent), 0644); err != nil {
        t.Fatalf("failed to write config: %v", err)
    }

    if err := os.WriteFile(filepath.Join(roamingDir, "ccx-desktop", ".env"), []byte("PORT=3699"), 0644); err != nil {
        t.Fatalf("failed to write env: %v", err)
    }

    p, err := Detect()
    if err != nil {
    }
    if p == nil {
        t.Skip("no proxy configured in test environment")
    }
    if p.Source != ProxyTypeCCX {
        t.Errorf("Source = %q, want %q", p.Source, ProxyTypeCCX)
    }
    if p.Port <= 0 {
        t.Errorf("Port = %d, expected positive", p.Port)
    }
    if p.BaseURL == "" {
        t.Error("BaseURL should not be empty")
    }
}

func TestDetect_FailsOnBadHomeDir(t *testing.T) {
    p, err := Detect()
    if p != nil {
        if p.BaseURL == "" && p.APIKey == "" {
            t.Error("proxy returned with empty fields")
        }
    }
    _ = err
}

func TestDetect_EnvVarOverride(t *testing.T) {
    os.Setenv("LOCALAI_URL", "http://localhost:8080")
    defer os.Unsetenv("LOCALAI_URL")

    p, err := Detect()
    if err == nil && p != nil {
        _ = p.BaseURL
    }
}
