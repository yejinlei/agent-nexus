package model

import (
	"strings"
	"testing"
)

func TestIsNonChatModel(t *testing.T) {
	nonChat := []string{
		"ACE-Step-v1-3.5B", "whisper-1", "tts-1-hd", "SenseVoiceSmall",
		"BAAI/bge-m3", "bge-reranker-v2", "text-embedding-3-small",
		"dall-e-3", "flux-dev", "stable-diffusion-xl", "sensenova-stt-v1",
		"paraformer-asr-1", "moonshot-v1-audio", "Qwen2.5-VL-Image",
	}
	for _, m := range nonChat {
		if !isNonChatModel(m) {
			t.Errorf("isNonChatModel(%q) = false, want true", m)
		}
	}
	chat := []string{
		"fable", "claude-opus-4", "claude-sonnet-5", "gpt-5.5", "glm-5.2",
		"deepseek-v4-flash", "qwen3-max", "kimi-k2", "sensenova-6.7-flash-lite",
		"gemini-2.5-pro", "MiniMax-M1", "grok-4",
	}
	for _, m := range chat {
		if isNonChatModel(m) {
			t.Errorf("isNonChatModel(%q) = true, want false", m)
		}
	}
}

func TestRecommendModelExactAndKeyword(t *testing.T) {
	// claude default is "fable".
	if got, src := RecommendModel("claude", []string{"ACE-Step-v1-3.5B", "fable"}); got != "fable" || src != "精确匹配" {
		t.Errorf("exact: got %q/%q, want fable/精确匹配", got, src)
	}
	// codex default is "gpt-5.5"; substring hit.
	if got, src := RecommendModel("codex", []string{"gpt-5.5-chat-latest", "glm-5.2"}); got != "gpt-5.5-chat-latest" || src != "关键字匹配" {
		t.Errorf("keyword: got %q/%q, want gpt-5.5-chat-latest/关键字匹配", got, src)
	}
}

func TestRecommendModelFamilyScoring(t *testing.T) {
	// Conservative contract: kimi wants gpt-5.5; the list is heterogeneous
	// (deepseek/kimi/qwen/glm — no dominant family, no gpt). Returning "" is
	// correct — callers skip the agent rather than mis-configure it. The one
	// absolute rule is that the audio head-model must never win.
	upstream := []string{"ACE-Step-v1-3.5B", "deepseek-v3", "kimi-k2", "qwen3-max", "glm-5.2"}
	got, src := RecommendModel("kimi", upstream)
	if strings.Contains(strings.ToLower(got), "ace-step") {
		t.Errorf("must never recommend the audio model, got %q", got)
	}
	if got != "" && src == "" {
		t.Errorf("non-empty pick %q must carry a reason label", got)
	}
	t.Logf("kimi -> %q (%q)", got, src)

	// A recognised family with no plausible match also returns "".
	if got, _ := RecommendModel("kimi", []string{"llama-3.3-70b", "mistral-large-2"}); got != "" {
		t.Errorf("heterogeneous known-family list should yield no pick, got %q", got)
	}
}

func TestRecommendModelClaudePrefersTopTier(t *testing.T) {
	// want "fable" (claude family, top tier). Among claude-family options the
	// closest-tier model should win over haiku/mini siblings.
	upstream := []string{"claude-haiku-4.5", "claude-opus-4", "glm-5.2"}
	got, src := RecommendModel("claude", upstream)
	if got != "claude-opus-4" {
		t.Errorf("got %q (%s), want claude-opus-4", got, src)
	}
}

func TestRecommendModelNoFirstModelFallback(t *testing.T) {
	// Heterogeneous list with zero plausible matches for openclaw's
	// sensenova default must NOT return the first entry.
	upstream := []string{"llama-3.3-70b", "mistral-large-2", "ernie-4.0"}
	got, _ := RecommendModel("openclaw", upstream)
	if got == "llama-3.3-70b" && dominantFamily(upstream) == "" {
		t.Errorf("fell back to first model on heterogeneous list: %q", got)
	}
}

func TestRecommendModelDominantFamilyFallback(t *testing.T) {
	// All candidates are one coherent family absent from the wanted model:
	// same-family fallback is allowed ("同类兜底").
	upstream := []string{"sensenova-6.7-max", "sensenova-6.7-flash-lite", "sensenova-u1-fast"}
	got, src := RecommendModel("codex", upstream) // wants gpt-5.5
	if got == "" || src != "同类兜底" {
		t.Errorf("got %q/%q, want sensenova-*/同类兜底", got, src)
	}
}

func TestPickCustomModelNeverReturnsAudio(t *testing.T) {
	upstream := []string{"ACE-Step-v1-3.5B", "whisper-1", "glm-5.2"}
	for _, agent := range []string{"codex", "kimi", "opencode", "openclaw", "hermes", "gemini", "openclaude", "claude"} {
		if got := PickCustomModel(agent, upstream); strings.Contains(strings.ToLower(got), "ace-step") || strings.Contains(strings.ToLower(got), "whisper") {
			t.Errorf("PickCustomModel(%q) returned non-chat model %q", agent, got)
		}
	}
}

func TestMoarkRegression(t *testing.T) {
	// Regression guard for the real incident: conf set --db 10 (moark, 241
	// models, first entry ACE-Step-v1-3.5B) gave EVERY agent that audio model.
	upstream := moarkSample()
	for _, agent := range []string{"codex", "kimi", "opencode", "openclaw", "hermes", "gemini", "openclaude", "claude"} {
		got, src := RecommendModel(agent, upstream)
		lower := strings.ToLower(got)
		if strings.Contains(lower, "ace-step") || strings.Contains(lower, "whisper") || strings.Contains(lower, "embed") {
			t.Errorf("%s: recommended non-chat model %q (%s)", agent, got, src)
		}
		t.Logf("%s -> %s (%s)", agent, got, src)
	}
}

// moarkSample is a representative slice of the moark /v1/models listing:
// audio/image/embedding models interleaved with chat models from several
// families. Order matters — the old code picked index 0 blindly.
func moarkSample() []string {
	return []string{
		"ACE-Step/ACE-Step-v1-3.5B", "Qwen/Qwen2.5-VL-72B-Instruct",
		"funasr/cam++", "stabilityai/stable-video-diffusion-img2vid-xt",
		"BAAI/bge-m3", "openai/whisper-large-v3-turbo",
		"deepseek-ai/DeepSeek-V3.2-Exp", "zhipu/glm-4.6",
		"moonshotai/Kimi-K2-Instruct", "Qwen/Qwen3-Coder-Plus",
		"sensenova/SenseNova-6.7-Flash-Lite", "openai/gpt-5.2",
		"anthropic/claude-sonnet-4.5", "google/gemini-3-pro",
		"meta-llama/Llama-3.3-70B-Instruct", "mistralai/Mistral-Large-Ingest",
	}
}

func TestRecommendModelBorrowedBrandNames(t *testing.T) {
	// Real-world brand-borrowing ids from the moark catalog: a Qwen distillate
	// with "Claude-Opus" in its name, and a DeepSeek derivative with "GPT-OS".
	// Neither may win as a same-family match for claude/codex.
	distilled := "Qwen/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled"
	// Lone item = coherent family, so 同类兜底 is allowed; the contract that
	// matters is that the scored path never treats it as a claude match.
	if s := scoreCandidate(distilled, "fable"); s >= 40 {
		t.Errorf("Claude-branded Qwen distillate scored %d on the same-family path, want < 40", s)
	}
	if got, src := RecommendModel("claude", []string{distilled, "deepseek-v3", "kimi-k2"}); got != "" {
		t.Errorf("claude must not recommend borrowed-brand id %q (%s)", got, src)
	}
	// A single coherent-family list may surface via 同类兜底, but the scored
	// path must never treat it as a gpt same-family match (score < 40).
	if got, src := RecommendModel("codex", []string{"deepseek-ai/DeepSeek-V3.2-Exp-GPT-OS"}); got != "deepseek-ai/DeepSeek-V3.2-Exp-GPT-OS" || src != "同类兜底" {
		t.Errorf("codex: lone deepseek should only reach 同类兜底, got %q/%q", got, src)
	}
	if s := scoreCandidate("deepseek-ai/DeepSeek-V3.2-Exp-GPT-OS", "gpt-5.5"); s >= 40 {
		t.Errorf("GPT-branded deepseek scored %d on the same-family path, want < 40", s)
	}
	// Genuine leading-position ids still score strong.
	if got, src := RecommendModel("codex", []string{"openai/gpt-oss-120b", distilled}); got != "openai/gpt-oss-120b" || src != "同族匹配" {
		t.Errorf("codex: got %q/%q, want openai/gpt-oss-120b/同族匹配", got, src)
	}
}
