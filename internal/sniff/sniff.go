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
	Models         []ModelItem
	DetectedFormat string
	Notes          string
}

// ModelItem represents one model in the /v1/models list response.
type ModelItem struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	OwnedBy string                 `json:"owned_by"`
	Raw     map[string]interface{} `json:"-"` // extra provider-specific fields
}

// FormatVerbose returns a human-readable multi-line string of all known fields.
func (m *ModelItem) FormatVerbose() string {
	var lines []string
	lines = append(lines, fmt.Sprintf("    %-40s", m.ID))
	if m.Object != "" {
		lines = append(lines, fmt.Sprintf("    %-40s %s", "type:", m.Object))
	}
	if m.OwnedBy != "" {
		lines = append(lines, fmt.Sprintf("    %-40s %s", "owner:", m.OwnedBy))
	}
	if m.Created > 0 {
		lines = append(lines, fmt.Sprintf("    %-40s %s", "created:", time.Unix(m.Created, 0).Format("2006-01-02")))
	}
	for k, v := range m.Raw {
		lines = append(lines, fmt.Sprintf("    %-40s %v", k+":", v))
	}
	return strings.Join(lines, "\n")
}

// ModelCapabilities returns a short capability summary from extra fields or inferred from the model ID.
func (m *ModelItem) ModelCapabilities() []string {
	caps := []string{}

	if v, ok := m.Raw["capabilities"]; ok {
		if cap, ok := v.(map[string]interface{}); ok {
			for name := range cap {
				caps = append(caps, "capability:"+name)
			}
		}
	}
	if v, ok := m.Raw["context_window"]; ok {
		caps = append(caps, fmt.Sprintf("context:%v", v))
	}
	if v, ok := m.Raw["context_length"]; ok {
		caps = append(caps, fmt.Sprintf("context:%v", v))
	}
	if v, ok := m.Raw["max_tokens"]; ok {
		caps = append(caps, fmt.Sprintf("max_tokens:%v", v))
	}
	if v, ok := m.Raw["max_output_length"]; ok {
		caps = append(caps, fmt.Sprintf("max_output:%v", v))
	}
	if v, ok := m.Raw["input_tokens"]; ok {
		caps = append(caps, fmt.Sprintf("input_tokens:%v", v))
	}
	if v, ok := m.Raw["output_tokens"]; ok {
		caps = append(caps, fmt.Sprintf("output_tokens:%v", v))
	}
	if v, ok := m.Raw["input_modalities"]; ok {
		caps = append(caps, fmt.Sprintf("input:%v", v))
	}
	if v, ok := m.Raw["output_modalities"]; ok {
		caps = append(caps, fmt.Sprintf("output:%v", v))
	}
	if v, ok := m.Raw["supported_features"]; ok {
		caps = append(caps, fmt.Sprintf("features:%v", v))
	}
	if v, ok := m.Raw["quantization"]; ok {
		caps = append(caps, fmt.Sprintf("quant:%v", v))
	}

	id := strings.ToLower(m.ID)
	if strings.Contains(id, "image") || strings.Contains(id, "vision") {
		caps = append(caps, "inferred:vision")
	}
	if strings.Contains(id, "audio") || strings.Contains(id, "whisper") {
		caps = append(caps, "inferred:audio")
	}
	if strings.Contains(id, "completion") || strings.Contains(id, "text") {
		caps = append(caps, "inferred:completion")
	}

	if len(caps) == 0 {
		caps = append(caps, "(无扩展能力字段)")
	}
	return caps
}

// ModelsResponse mirrors the OpenAI /v1/models JSON shape.
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelItem `json:"data"`
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

	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

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
			fillExtraFields(models, modelsBody)
			result.ModelCount = count
			// Use the first real model from the list for subsequent probes.
			testModel = models[0].ID
			result.DetectedFormat = "OpenAI Compatible (models endpoint)"
		}
	}

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
			if strings.HasSuffix(result.Notes, "; ") {
				result.Notes = result.Notes[:len(result.Notes)-2]
			}
			result.Notes += fmt.Sprintf("; Anthropic messages 端点: %v", msgsErr)
		} else {
			result.Notes = fmt.Sprintf("Anthropic messages 端点: %v", msgsErr)
		}
	} else if len(msgsResp) > 0 {
		var mr MessagesResponse
		if err := json.Unmarshal(msgsResp, &mr); err == nil && mr.ID != "" {
			result.AnthropicCap = true
			result.DetectedFormat += " + Anthropic Messages API"
		} else {
			if result.Notes != "" {
				if strings.HasSuffix(result.Notes, "; ") {
					result.Notes = result.Notes[:len(result.Notes)-2]
				}
				result.Notes += fmt.Sprintf("; Anthropic messages 端点返回非标准响应: %s", truncate(string(msgsResp), 120))
			} else {
				result.Notes = fmt.Sprintf("Anthropic messages 端点返回非标准响应: %s", truncate(string(msgsResp), 120))
			}
		}
	}

	// If no formats detected at all, note it.
	if result.ModelCount == 0 && result.OpenAICap == false && result.AnthropicCap == false {
		if result.Notes == "" {
			result.Notes = "未从该 endpoint 探测到可用模型，可能是自定义格式或需要特殊认证"
		} else if !strings.Contains(result.Notes, "未从该 endpoint") {
			if strings.HasSuffix(result.Notes, "; ") {
				result.Notes = result.Notes[:len(result.Notes)-2]
			}
			result.Notes += "; 未探测到标准格式"
		}
	}

	return result
}

// doRequest performs an HTTP request and returns the response body bytes.
func doRequest(client *http.Client, method, urlStr, apiKey string, body io.Reader) ([ ]byte, error) {
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
func parseModels(body []byte) ([]ModelItem, int) {
	var mr ModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, 0
	}
	return mr.Data, len(mr.Data)
}

// fillExtraFields populates the Raw extra fields for each model item.
func fillExtraFields(models []ModelItem, rawBody []byte) {
	var raw map[string]interface{}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		return
	}

	data, ok := raw["data"].([]interface{})
	if !ok {
		return
	}

	knownFields := map[string]bool{
		"id": true, "object": true, "created": true, "owned_by": true,
	}

	for _, entry := range data {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		modelID, ok := m["id"].(string)
		if !ok {
			}

		extras := map[string]interface{}{}
		for k, v := range m {
			if !knownFields[k] {
				extras[k] = v
			}
		}

		for i := range models {
			if models[i].ID == modelID {
				models[i].Raw = extras
			}
		}
	}
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
