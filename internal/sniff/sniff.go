package sniff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SniffResult holds the outcome of a sniff operation.
type SniffResult struct {
	BaseURL        string
	ModelCount     int
	Models         []ModelItem
	DetectedFormat string
	Notes          string
	Caps           []ProtocolCap
	// Direct capability booleans derived from Caps for convenient access.
	OpenAICap    bool
	AnthropicCap bool
	ResponsesCap bool
}

// ProtocolCap describes one detected protocol.
type ProtocolCap struct {
	Label string
}

// HasCap returns true if the result contains the given protocol label.
func (r *SniffResult) HasCap(label string) bool {
	for _, c := range r.Caps {
		if c.Label == label {
			return true
		}
	}
	return false
}

// syncCaps updates the direct bool fields to match Caps.
// Called by Sniff after probing so both representations stay in sync.
func (r *SniffResult) syncCaps() {
	r.OpenAICap = r.HasCap("📝 Chat Completions") || r.HasCap("🤖 OpenAI Responses")
	r.AnthropicCap = r.HasCap("💬 Anthropic Messages")
	r.ResponsesCap = r.HasCap("🤖 OpenAI Responses")
}

// ModelItem represents one model in the /v1/models list response.
type ModelItem struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	OwnedBy string                 `json:"owned_by"`
	Raw     map[string]interface{} `json:"-"`
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
		_ = m.Created
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
	result.syncCaps()
	return result, nil
}

// sniffPath probes a single base URL that is guaranteed to end with /v1.
// All protocol probes are always executed: models, OpenAI Chat, Anthropic Messages,
// Gemini Generations, and OpenAI Responses.
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
			testModel = models[0].ID
			result.DetectedFormat = "OpenAI Compatible (models endpoint)"
		}
	}

	if testModel == "" {
		_ = result
		testModel = "gpt-3.5-turbo"
	}

	notes := []string{}

	// Build the chat request payload (used by multiple probes).
	// NOTE: omit max_tokens — some providers (e.g. Sensenova) reject it.
	chatReq := map[string]interface{}{
		"model": testModel,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "say hello"},
		},
	}
	chatBody, _ := json.Marshal(chatReq)

	// Probe 2: POST /v1/chat/completions — OpenAI Chat Completions.
	chatURL := baseURL + "/chat/completions"
	chatResp, chatErr := doRequest(client, "POST", chatURL, apiKey, bytes.NewReader(chatBody))
	if chatErr != nil {
		notes = append(notes, fmt.Sprintf("OpenAI chat 端点: %v", chatErr))
	} else if len(chatResp) > 0 {
		var ccr ChatCompletionResponse
		if err := json.Unmarshal(chatResp, &ccr); err == nil && ccr.ID != "" {
			_ = ccr
			result.Caps = append(result.Caps, ProtocolCap{Label: "📝 Chat Completions"})
			result.DetectedFormat += " + Chat Completions"
		} else {
			notes = append(notes, fmt.Sprintf("OpenAI chat 端点返回非标准响应: %s", truncate(string(chatResp), 120)))
		}
	}

	// Probe 3: POST /v1/messages — Anthropic Messages API.
	messagesURL := baseURL + "/messages"
	msgsReq := map[string]interface{}{
		"model": testModel,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "say hello"},
		},
	}
	msgsBody, _ := json.Marshal(msgsReq)
	msgsResp, msgsErr := doRequest(client, "POST", messagesURL, apiKey, bytes.NewReader(msgsBody))
	if msgsErr != nil {
		notes = append(notes, fmt.Sprintf("Anthropic messages 端点: %v", msgsErr))
	} else if len(msgsResp) > 0 {
		var mr MessagesResponse
		if err := json.Unmarshal(msgsResp, &mr); err == nil && mr.ID != "" {
			result.Caps = append(result.Caps, ProtocolCap{Label: "💬 Anthropic Messages"})
			result.DetectedFormat += " + Anthropic Messages"
		} else {
			notes = append(notes, fmt.Sprintf("Anthropic messages 端点返回非标准响应: %s", truncate(string(msgsResp), 120)))
		}
	}

	// Probe 4: POST /v1/models/{model}:generateContent — Gemini Generations.
	geminiURL := baseURL + "/models/" + testModel + ":generateContent"
	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]interface{}{{"text": "say hello"}}},
		},
		"generationConfig": map[string]interface{}{
			"maxOutputTokens": 4,
		},
	}
	geminiBody, _ := json.Marshal(geminiReq)
	geminiResp, geminiErr := doRequest(client, "POST", geminiURL, apiKey, bytes.NewReader(geminiBody))
	if geminiErr != nil {
		notes = append(notes, fmt.Sprintf("Gemini generations 端点: %v", geminiErr))
	} else if len(geminiResp) > 0 {
		var geminiRr map[string]interface{}
		if err := json.Unmarshal(geminiResp, &geminiRr); err == nil && geminiRr["candidates"] != nil {
			result.Caps = append(result.Caps, ProtocolCap{Label: "🔮 Gemini Generations"})
			result.DetectedFormat += " + Gemini Generations"
		} else {
			notes = append(notes, fmt.Sprintf("Gemini generations 端点返回非标准响应: %s", truncate(string(geminiResp), 120)))
		}
	}

	// Probe 5: POST /v1/responses — OpenAI Responses API.
	// The Responses protocol requires "input" to be an array of InputItem objects,
	// not a raw string: [{"type":"message","role":"user","content":"..."}].
	respURL := baseURL + "/responses"
	respReq := map[string]interface{}{
		"model": testModel,
		"input": []map[string]interface{}{
			{"type": "message", "role": "user", "content": "say hello"},
		},
	}
	respBody, _ := json.Marshal(respReq)
	respResp, respErr := doRequest(client, "POST", respURL, apiKey, bytes.NewReader(respBody))
	if respErr != nil {
		notes = append(notes, fmt.Sprintf("OpenAI responses 端点: %v", respErr))
	} else if len(respResp) > 0 {
		var rr map[string]interface{}
		if err := json.Unmarshal(respResp, &rr); err == nil && rr["id"] != nil {
			_ = rr
			result.Caps = append(result.Caps, ProtocolCap{Label: "🤖 OpenAI Responses"})
			result.DetectedFormat += " + OpenAI Responses"
		} else {
			notes = append(notes, fmt.Sprintf("OpenAI responses 端点返回非标准响应: %s", truncate(string(respResp), 120)))
		}
	}

	result.Notes = strings.Join(notes, "; ")

	// If nothing detected, add a summary note.
	if result.ModelCount == 0 && len(result.Caps) == 0 {
		if result.Notes == "" {
			result.Notes = "未从该 endpoint 探测到可用模型，可能是自定义格式或需要特殊认证"
		} else if !strings.Contains(result.Notes, "未探测到") {
			result.Notes += "; 未探测到标准格式"
		}
	}

	return result
}

// doRequest performs an HTTP request and returns the response body bytes.
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
			continue
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
	return r.ModelCount > 0 || r.DetectedFormat != "" || r.HasCap("📝 Chat Completions")
}

// HasMultipleFormats returns true if multiple protocols are detected.
func (r *SniffResult) HasMultipleFormats() bool {
	return len(r.Caps) > 1
}

// ResponsesProbe is a standalone probe that checks whether an endpoint
// supports the OpenAI Responses API (/v1/responses). Used by codex Configure
// to refuse writing config when the upstream does not support the protocol.
//
// A connection error (e.g. the proxy is offline) does NOT mean the endpoint
// lacks support — it just means the probe couldn't verify it. In that case
// the probe returns true ("unknown, assume OK") so codex config is still
// written. Only an explicit HTTP 4xx/5xx from the endpoint causes a
// rejection, because that means the endpoint exists but does not implement
// /v1/responses (e.g. SenseNova with only chat/completions).
func ResponsesProbe(baseURL, apiKey string) bool {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	respReq := map[string]interface{}{
		"model": "gpt-4o",
		"input": "say hello",
	}
	reqBody, err := json.Marshal(respReq)
	if err != nil {
		return true // JSON failure shouldn't block config
	}
	// Use an explicit no-proxy transport so we reach the target directly,
	// not through any configured global (CCX / Socks) proxy.
	// Transport.Proxy = nil still honours HTTP_PROXY env vars via
	// ProxyFromEnvironment; returning nil explicitly disables it.
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		},
	}
	_, err = doRequest(client, "POST", baseURL+"/responses", apiKey, bytes.NewReader(reqBody))
	if err == nil {
		return true // explicit success
	}
	// doRequest wraps HTTP errors as "HTTP 404: ..." or "HTTP 401: ..."
	// and network errors as "HTTP 请求失败: <conn-error>". The former means
	// the endpoint responded (but without /v1/responses). The latter means
	// we couldn't reach it — treat as "unknown" and allow config.
	if strings.Contains(err.Error(), "HTTP 请求失败") {
		return true // network error → can't prove incompatibility
	}
	return false // HTTP response with non-2xx → no /v1/responses
}

// GeminiProtocolProbe checks whether an endpoint supports the Gemini native
// API protocol (/v1beta/...). Gemini CLI requires this protocol; most
// OpenAI-compatible gateways (SenseNova, etc.) do not support it.
//
// A connection error does NOT mean the endpoint lacks support — it just means
// the probe couldn't verify. In that case the probe returns true ("unknown,
// assume OK"). Only an explicit HTTP 4xx/5xx from the endpoint causes a
// rejection, because that means the endpoint exists but does not implement
// /v1beta/models.
func GeminiProtocolProbe(baseURL, apiKey string) bool {
	// Strip /v1 suffix if present; Gemini uses /v1beta
	u := strings.TrimSuffix(baseURL, "/")
	if strings.HasSuffix(u, "/v1") {
		u = u[:len(u)-3]
	}
	reqBody, _ := json.Marshal(map[string]interface{}{"pageSize": 5})
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		},
	}
	_, err := doRequest(client, "POST", u+"/v1beta/models", apiKey, bytes.NewReader(reqBody))
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "HTTP 请求失败") {
		return true
	}
	return false
}

// UpstreamModelList fetches the list of available model IDs from the proxy's
// /v1/models endpoint. Returns a slice of model IDs (empty slice on failure).
// Pass the full base URL including /v1 (e.g. "http://127.0.0.1:3688/v1").
func UpstreamModelList(baseURL, apiKey string) []string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	modelsURL := baseURL + "/models"
	client := &http.Client{Timeout: 10 * time.Second}
	body, err := doRequest(client, "GET", modelsURL, apiKey, nil)
	if err != nil {
		return nil
	}
	models, _ := parseModels(body)
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	return ids
}
