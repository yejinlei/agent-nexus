package model

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"agent-nexus/internal/shared"
)

// Model recommendation: a scored replacement for the old
// exact → keyword → first-model fallback in PickCustomModel.
//
// Pipeline:
//  1. Hard-filter candidates by task category (non-chat models are never
//     recommended for coding agents — an audio/image/embedding model must
//     never win just because /v1/models listed it first).
//  2. Exact / keyword-substring match wins outright (old behavior kept).
//  3. Score the survivors against the agent's default model name
//     (family affinity, tier alignment, version proximity).
//  4. Same-family fallback when the gateway serves one coherent family.
//  5. When nothing passes, return "" so callers skip the agent instead of
//     mis-configuring it with a random model.

// nonChatSubstrings are matched case-insensitively against the whole model
// id. Any hit removes the candidate from consideration entirely.
var nonChatSubstrings = []string{
	"embed", "bge-", "stella", "bce-", "rerank", "jina-",
	"whisper", "tts", "-asr", "asr-", "sensevoice", "ace-step", "step-audio",
	"speech", "voice", "audio",
	"image", "video", "dall-e", "dalle", "flux", "sdxl", "stable-diffusion",
	"midjourney", "wanx", "hunyuan-3d", "clip-vit",
	"moderation", "transcribe", "ocr",
}

// nonChatTokens are delimiter-split tokens matched exactly.
// ("stt"/"asr" as standalone tokens; "-asr"/"asr-" cover embedded forms.)
var nonChatTokens = map[string]bool{"stt": true, "asr": true}

// isNonChatModel reports whether a model id looks like something other than a
// text-generation chat model (audio, vision-generation, embedding, rerank…).
func isNonChatModel(id string) bool {
	lower := strings.ToLower(id)
	for _, sub := range nonChatSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	for _, tok := range splitTokens(lower) {
		if nonChatTokens[tok] {
			return true
		}
	}
	return false
}

// ChatCandidates returns the upstream models that plausibly serve a coding
// agent (used for the interactive shortlist and the auto recommendation).
func ChatCandidates(upstreamModels []string) []string {
	var out []string
	for _, m := range upstreamModels {
		if !isNonChatModel(m) {
			out = append(out, m)
		}
	}
	return out
}

// NonChatCandidates returns the upstream models that are NOT plausible for a
// coding agent (audio / image / embedding / rerank …), preserving order.
func NonChatCandidates(upstreamModels []string) []string {
	var out []string
	for _, m := range upstreamModels {
		if isNonChatModel(m) {
			out = append(out, m)
		}
	}
	return out
}

// familySynonyms maps a canonical family to its equivalent tokens. Vendor
// prefixes ("openai/", "anthropic/", "zhipu/"…) fold into the model family so
// gateway ids score like their bare-name equivalents.
var familySynonyms = map[string][]string{
	"claude":    {"claude", "anthropic", "fable", "opus", "sonnet", "haiku"},
	"gpt":       {"gpt", "chatgpt", "openai", "o1", "o3", "o4"},
	"glm":       {"glm", "chatglm", "zhipu", "zai"},
	"qwen":      {"qwen", "qwq", "tongyi"},
	"kimi":      {"kimi", "moonshot", "moonshotai"},
	"deepseek":  {"deepseek"},
	"llama":     {"llama", "meta-llama"},
	"sensenova": {"sensenova", "sense"},
	"gemini":    {"gemini", "google"},
	"mistral":   {"mistral", "mixtral", "mistralai"},
	"doubao":    {"doubao", "bytedance"},
	"hunyuan":   {"hunyuan", "tencent"},
	"ernie":     {"ernie", "baidu"},
	"minimax":   {"minimax", "abab"},
	"grok":      {"grok", "xai"},
}

// tierRank orders role words on one capability axis: bigger = stronger.
// Unknown tiers sit at 0; distance between ranks is the mismatch penalty.
var tierRank = map[string]int{
	"nano": -3, "haiku": -3, "tiny": -3,
	"mini": -2, "flash": -2, "lite": -2, "small": -2, "air": -2, "e": -2,
	"turbo": -1, "7b": -1,
	"": 0, "base": 0,
	"pro": 1, "plus": 1, "thinking": 1, "reasoning": 1, "r1": 1, "preview": 1, "moe": 1,
	"max": 2, "big": 2, "large": 2, "70b": 2, "235b": 2, "k": 2,
	"ultra": 3, "opus": 3, "top": 3,
}

// nonTierTokens are words that look like tiers by position but are naming
// noise; they never participate in tier alignment.
var nonTierTokens = map[string]bool{"v1": true, "v2": true, "v3": true, "v4": true, "v5": true, "v6": true}

func splitTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '-', '_', '.', '/', ':', ' ', '\t':
			return true
		}
		return false
	})
}

// strongFamilies own models whose name merely CONTAINS the family word —
// domain models that borrow the name in a compound ("HuatuoGPT" is a medical
// model, "Claude-Distilled" is a Qwen model). For these families an embedded
// substring hit counts as borrowed (weak); for the rest ("…-glm-4.6",
// "kimi-k2") embedded naming is normal and stays strong.
//
// The canonical token of each of these families ("gpt" itself) is likewise a
// borrowed-marketing word: "DeepSeek-V3.2-Exp-GPT-OS" names a DeepSeek
// derivative, not an OpenAI model. Only a vendor-prefixed id ("openai/gpt-oss")
// or a leading bare "gpt…" proves the family outright.
var strongFamilies = map[string]bool{
	"gpt": true, "gemini": true, "grok": true,
	"llama": true, "minimax": true,
}

// leadingFamilyRe requires the family word to START the id's last path
// segment ("gpt-5.5", "sensenova-6.7", "myccx/glm-5.2"), which is genuine
// product naming; words appearing mid-name are branding.
func leadingFamilyRe(id string) (string, bool) {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	for fam, syns := range familySynonyms {
		for _, s := range syns {
			if len(s) >= 3 && strings.HasPrefix(id, s) {
				return fam, true
			}
		}
	}
	return "", false
}

// borrowedFamilyWords are tokens that appear in other vendors' model names as
// marketing/provenance words ("Qwen3.5-27B-Claude-4.6-Opus-Reasoning-
// Distilled"). A standalone match on one of these still counts as a borrowed
// hint: only the vendor's own canonical token ("claude" itself) proves the
// family.
var borrowedFamilyWords = map[string]bool{
	"anthropic": true, "fable": true, "opus": true, "sonnet": true, "haiku": true,
	"openai": true, "chatgpt": true, "o1": true, "o3": true, "o4": true,
	"zhipu": true, "zai": true, "chatglm": true,
	"tongyi": true, "qwq": true,
	"moonshot": true, "moonshotai": true,
	"meta-llama": true, "mistralai": true, "mixtral": true,
	"bytedance": true, "tencent": true, "baidu": true, "xai": true,
	"google": true, "sense": true, "abab": true,
}

// parsedName is the structural view of one model id.
type parsedName struct {
	family      string
	famEmbedded bool // family matched inside a larger token, not standalone
	version     float64
	hasVersion  bool
	tier        string
}

// parseModelName extracts the family token, the first standalone version
// number and the tier token from a model id. The family may come from a
// standalone delimiter-split token ("gpt-5.2") or, failing that, from a
// substring inside a larger token ("HuatuoGPT-o1-7B"); the latter is flagged
// as embedded so scoring can discount it.
func parseModelName(id string) parsedName {
	lower := strings.ToLower(id)
	toks := splitTokens(lower)
	var p parsedName
	// Leading position ("gpt-5.2", "sensenova-6.7") proves the family even
	// for strong families whose word is also marketing noise.
	if fam, ok := leadingFamilyRe(lower); ok {
		p.family = fam
	}

	for _, t := range toks {
		if p.family == "" {
			fam, standalone := canonicalFamily(t)
			borrowed := standalone && borrowedFamilyWords[t]
			if !standalone {
				var emb string
				if emb, standalone = embeddedFamily(t); !standalone || !strongFamilies[emb] {
					continue
				}
				fam, borrowed = emb, true
			} else if strongFamilies[fam] {
				// Bare canonical token of a strong family, not in leading
				// position: borrowed branding ("…-GPT-OS").
				borrowed = true
			}
			switch {
			case p.family != "" && fam != p.family:
				// Compound naming: a second distinct family token after the
				// first ("Qwen…Claude…") marks a distilled/branded id — the
				// leading token is the lineage, so keep it and flag weak.
				p.famEmbedded = true
			case p.family != "":
				// Same family again ("…Claude…Opus…"): confirm, stay weak if
				// the earlier match was borrowed.
				if borrowed {
					p.famEmbedded = true
				}
			default:
				p.family = fam
				p.famEmbedded = borrowed
			}
			continue
		}
		if !p.hasVersion {
			if v, err := strconv.ParseFloat(t, 64); err == nil {
				p.version, p.hasVersion = v, true
				continue
			}
		}
		if p.tier == "" && t != "base" && !nonTierTokens[t] {
			if _, ok := tierRank[t]; ok {
				p.tier = t
			}
		}
	}
	return p
}

// embeddedFamily finds a family synonym appearing inside a larger token
// ("huatuogpt" → gpt). Synonyms shorter than three letters never match to
// avoid noise.
func embeddedFamily(tok string) (string, bool) {
	for fam, syns := range familySynonyms {
		for _, s := range syns {
			if len(s) >= 3 && strings.Contains(tok, s) {
				return fam, true
			}
		}
	}
	return "", false
}

func canonicalFamily(tok string) (string, bool) {
	for fam, syns := range familySynonyms {
		if slices.Contains(syns, tok) {
			return fam, true
		}
	}
	return "", false
}

// topTierFamily lists families whose flagship tier is implied when the wanted
// name carries no explicit tier token (e.g. claude's "fable"). A same-family
// candidate at that tier beats lower-tier siblings. Families without role
// words (gpt, glm…) are absent from this map: version alone expresses
// capability there.
var topTierFamily = map[string]string{
	"claude": "opus",
}

// scoreCandidate rates one upstream model against the wanted default.
// Higher is better. Family identity dominates; tier and version refine.
func scoreCandidate(candidate, want string) int {
	c := parseModelName(candidate)
	w := parseModelName(want)

	score := 0

	// Family affinity is the strongest signal. A family carried by an EMBEDDED
	// substring ("HuatuoGPT", "HealthGPT") is weaker than a standalone token
	// ("gpt-5.2"): domain models borrow the family name inside a compound word.
	switch {
	case w.family != "" && c.family == w.family:
		if c.famEmbedded {
			score += 30 // borrowed-name hint, not proof
		} else {
			score += 60
		}
	case c.family != "":
		score -= 25 // known but wrong family
	}

	// An unrecognised id (no family at all) is a wild card: it may be a
	// chat model under an opaque name, so it is neither rewarded nor
	// penalised — only tier/version can move it.
	unclassified := c.family == ""

	// Tier alignment along the capability axis.
	switch {
	case w.tier != "" && c.tier != "":
		dist := tierRank[c.tier] - tierRank[w.tier]
		if dist < 0 {
			dist = -dist
		}
		score += 25 - 8*dist
	case w.tier != "" && c.tier == "":
		score += 5 // untiered sibling of the wanted model
	case w.tier == "" && c.tier != "" && c.family == w.family:
		// Wanted name implies a tier without saying it ("fable" → opus-class
		// claude). Prefer the candidate closest to the family's flagship.
		if top, ok := topTierFamily[w.family]; ok {
			dist := tierRank[c.tier] - tierRank[top]
			if dist < 0 {
				dist = -dist
			}
			score += 25 - 8*dist
		} else if unclassified {
			score += 5 // unknown candidate tier vs. a tiered want
		}
	case w.tier == "" && c.tier == "":
		if unclassified {
			score += 5 // both untiered — no information, no penalty
		}
	}

	// Version proximity: one major version step costs 10 points.
	if c.hasVersion && w.hasVersion {
		d := c.version - w.version
		if d < 0 {
			d = -d
		}
		score -= 10 * int(d+0.5)
	}

	return score
}

// RecommendModel picks the best upstream model for agentName's default model.
// It returns the chosen id plus a human-readable reason label
// ("精确匹配" / "关键字匹配" / "同族匹配" / "同类兜底"), or ("", "") when no
// chat-capable candidate fits — callers should then skip the agent rather
// than fall back to whatever the gateway happened to list first.
func RecommendModel(agentName string, upstreamModels []string) (string, string) {
	want, ok := shared.GetDefaultModel(agentName)
	if !ok || want == "" || len(upstreamModels) == 0 {
		return "", ""
	}

	cands := ChatCandidates(upstreamModels)
	if len(cands) == 0 {
		return "", ""
	}

	// 1. Exact match wins outright.
	for _, m := range cands {
		if strings.EqualFold(m, want) {
			return m, "精确匹配"
		}
	}

	// 2. Keyword containment (substring either way), old behavior retained.
	wantLower := strings.ToLower(want)
	for _, m := range cands {
		lower := strings.ToLower(m)
		if strings.Contains(lower, wantLower) || strings.Contains(wantLower, lower) {
			return m, "关键字匹配"
		}
	}

	// 3. Scored ranking. Stable sort keeps listing order on ties.
	type sc struct {
		id    string
		score int
	}
	ranked := make([]sc, 0, len(cands))
	for _, m := range cands {
		ranked = append(ranked, sc{id: m, score: scoreCandidate(m, want)})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	if len(ranked) > 0 && ranked[0].score >= 40 {
		return ranked[0].id, "同族匹配"
	}

	// 4. Same-family fallback ONLY when the gateway clearly serves one
	// coherent family and the wanted model is absent (e.g. a sensenova-only
	// proxy asked for a claude default). Never crosses the chat filter.
	if fam := dominantFamily(cands); fam != "" {
		best, bestScore := "", -1<<30
		for _, m := range cands {
			if parseModelName(m).family == fam {
				if s := scoreCandidate(m, want); s > bestScore {
					best, bestScore = m, s
				}
			}
		}
		if best != "" {
			return best, "同类兜底"
		}
	}

	return "", ""
}

// dominantFamily returns the family token covering >half of the candidates,
// or "" when the list is heterogeneous (then there is no safe fallback).
func dominantFamily(cands []string) string {
	counts := map[string]int{}
	for _, m := range cands {
		if cf := parseModelName(m).family; cf != "" {
			counts[cf]++
		}
	}
	best, bestN := "", 0
	for fam, n := range counts {
		if n > bestN {
			best, bestN = fam, n
		}
	}
	if bestN*2 > len(cands) {
		return best
	}
	return ""
}
