package sniff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SniffResult holds the outcome of a sniff operation.
type SniffResult struct {
	BaseURL        string
	OpenAICap      bool
	AnthropicCap   bool
	ModelCount     int
	Models         []string
	DetectedFormat string
	Notes          string
}

// ModelsResponse mirrors the OpenAI /v1/models JSON shape.
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelItem `json:"data"`
}

// ModelItem represents one model in the list response.
type ModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ChatCompletionResponse mirrors the OpenAI /v1/chat/completions JSON shape.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice mirrors one completion choice.
type Choice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatMessage represents a single message in a chat turn.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MessagesResponse mirrors the Anthropic /v1/messages JSON shape.
type MessagesResponse struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Model      string    `json:"model"`
	Content    []Content `json:"content"`
	StopReason string    `json:"stop_reason"`
	Usage      Usage     `json:"usage"`
}

// Content mirrors one content item in Anthropic messages response.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Usage mirrors Anthropic usage field.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Sniff connects to the given LLM endpoint, probes supported message formats,
// lists available models, and returns a structured result.
func Sniff(baseURL, apiKey string) (*SniffResult, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("--url 为必选参数")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("--key 为必选参数")
	}

	// Normalise: strip trailing slash, then check if /v1 is already present.
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Ensure the URL ends with /v1. Do not double /v1 if already present.
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	// Probe the resolved /v1 path. All three probes always run.
	result := sniffPath(baseURL, apiKey)
	return result, nil
}

// sniffPath probes a single base URL that is guaranteed to end with /v1.
// All three probes (models, OpenAI chat, Anthropic messages) are always executed.
func sniffPath(baseURL, apiKey string) *SniffResult {
	client := &http.Client{Timeout: 15 * time.Second}

	result := &SniffResult{
		BaseURL: baseURL,
	}

	modelsURL := baseURL + "/models"

	// Probe 1: GET /models to list available models.
	modelsBody, _ := doRequest(client, "GET", modelsURL, apiKey, nil)
	var testModel string
	if len(modelsBody) > 0 {
		models, count := parseModels(modelsBody)
		if count > 0 {
			result.Models = models
			result.ModelCount = count
			result.DetectedFormat = "OpenAI Compatible (models endpoint)"
			// Use the first real model from the list for subsequent probes.
			// This avoids 404 from hardcoded non-existent model names.
			testModel = models[0]
		}
	}

	// If no real model was fetched, fall back to a generic placeholder.
	if testModel == "" {
		testModel = "gpt-3.5-turbo"
	}

	// Probe 2: POST /v1/chat/completions — OpenAI format.
	chatURL := baseURL + "/chat/completions"
	chatReq := map[string]interface{}{
		"model": testModel,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "say hello"},
		},
		"max_tokens": 4,
	}
	chatBody, _ := json.Marshal(chatReq)
	chatResp, chatErr := doRequest(client, "POST", chatURL, apiKey, bytes.NewReader(chatBody))

	if chatErr != nil {
		result.Notes = fmt.Sprintf("OpenAI chat 端点: %v", chatErr)
	} else if len(chatResp) > 0 {
		var ccr ChatCompletionResponse
		if err := json.Unmarshal(chatResp, &ccr); err == nil && ccr.ID != "" {
			result.OpenAICap = true
			result.DetectedFormat += " + OpenAI chat completions"
		} else {
			result.Notes = fmt.Sprintf("OpenAI chat 端点返回非标准响应: %s", truncate(string(chatResp), 120))
		}
	}

	// Probe 3: POST /v1/messages — Anthropic Messages API format.
	messagesURL := baseURL + "/messages"
	msgsReq := map[string]interface{}{
		"model": testModel,
		"max_tokens": 4,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "say hello"},
		},
	}
	msgsBody, _ := json.Marshal(msgsReq)
	msgsResp, msgsErr := doRequest(client, "POST", messagesURL, apiKey, bytes.NewReader(msgsBody))

	if msgsErr != nil {
		if result.Notes != "" {
			result.Notes += fmt.Sprintf("; Anthropic messages 端点: %v", msgsErr)
		} else {
			// If Notes is empty, replace it entirely (avoid empty prefix).
			result.Notes = fmt.Sprintf("Anthropic messages 端点: %v", msgsErr)
		}
	} else if len(msgsResp) > 0 {
		var mr MessagesResponse
		if err := json.Unmarshal(msgsResp, &mr); err == nil && mr.ID != "" {
			result.AnthropicCap = true
			result.DetectedFormat += " + Anthropic Messages API"
		} else {
			if result.Notes != "" {
				result.Notes += fmt.Sprintf("; Anthropic messages 端点返回非标准响应: %s", truncate(string(msgsResp), 120))
			} else {
				result.Notes = fmt.Sprintf("Anthropic messages 端点返回非标准响应: %s", truncate(string(msgsResp), 120))
			}
		}
	}

	// If no formats detected at all, note it.
	if result.ModelCount == 0 && result.OpenAICap == false && result.AnthropicCap == false {
		if result.Notes == "" {
			// Neither probes worked; try a final GET on the root to see if we can reach it.
			result.Notes = "未从该 endpoint 探测到可用模型，可能是自定义格式或需要特殊认证"
		} else if !strings.Contains(result.Notes, "未从该 endpoint") {
			result.Notes += "; 未探测到标准格式"
		}
	}

	return result
}

// doRequest performs an HTTP request and returns the response body bytes.
// Non-2xx responses return an error; 2xx responses return the body bytes.
func doRequest(client *http.Client, method, urlStr, apiKey string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "agent-nexus/0.1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %v", err)
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	return buf.Bytes(), nil
}

// parseModels tries to parse the /v1/models response.
func parseModels(body []byte) ([]string, int) {
	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, 0
	}
	names := make([]string, len(mr.Data))
	for i, m := range mr.Data {
		names[i] = m.ID
	}
	return names, len(names)
}

// truncate cuts s to n characters, appending "..." when truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// IsOpenAICompatible returns true if the sniff result indicates
// the endpoint speaks an OpenAI-compatible protocol.
func (r *SniffResult) IsOpenAICompatible() bool {
	return r.OpenAICap || r.ModelCount > 0 || r.DetectedFormat != ""
}

// HasMultipleFormats returns true if both OpenAI and Anthropic formats are supported.
func (r *SniffResult) HasMultipleFormats() bool {
	return r.OpenAICap && r.AnthropicCap
}
