package cmd

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/shared"
	"agent-nexus/internal/sniff"
)

// dbRef describes which DB proxy record to use for a conf set operation.
type dbRef struct {
	Mode string // "auto" or "id"
	ID   int    // for Mode=="id"
}

// resolveDBArg parses the --db flag into a dbRef. Returns an error on invalid input.
func resolveDBArg(flag string) (dbRef, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return dbRef{}, fmt.Errorf("--db 参数无效（需要 'auto' 或数字 id）")
	}
	if strings.EqualFold(flag, "auto") {
		return dbRef{Mode: "auto"}, nil
	}
	n, err := strconv.Atoi(flag)
	if err != nil {
		return dbRef{}, fmt.Errorf("--db 参数无效: %q（需要 'auto' 或数字 id）", flag)
	}
	if n < 1 {
		return dbRef{}, fmt.Errorf("--db 参数需要 >= 1 的整数（得到 %d）", n)
	}
	return dbRef{Mode: "id", ID: n}, nil
}

// resolveAgentList parses --agent into a sorted list of agent names.
// The authoritative source for "is this agent auto-configurable" is
// discover.IsConfigurableMap() — agents like gemini that declare
// IsConfigurable=false (Google OAuth/API key, not proxy-configurable) are
// excluded from the --agent all list and surface a warning when explicitly
// named.
func resolveAgentList(agentsStr string) ([]string, error) {
	isConfigurable := discover.IsConfigurableMap()

	configurable := make([]string, 0, len(shared.DefaultModels))
	for name := range shared.DefaultModels {
		if !isConfigurable[name] {
			continue
		}
		configurable = append(configurable, name)
	}
	sort.Strings(configurable)

	if strings.EqualFold(agentsStr, "all") {
		return configurable, nil
	}

	selected := strings.Split(agentsStr, ",")
	selectedSet := make(map[string]bool)
	var names []string
	for _, s := range selected {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		selectedSet[s] = true
	}

	for _, n := range configurable {
		if selectedSet[n] {
			names = append(names, n)
		}
	}

	for s := range selectedSet {
		if !slices.Contains(configurable, s) {
			fmt.Printf("⚠ 未知或不可配置 agent: %s（将被跳过）\n", s)
		}
	}
	return names, nil
}

func probeUpstreamModels(baseURL, apiKey string) []string {
	if baseURL == "" || apiKey == "" {
		return nil
	}
	fmt.Println("正在查询上游模型列表...")
	ids := sniff.UpstreamModelList(baseURL, apiKey)
	fmt.Printf("上游可用模型: %d\n", len(ids))
	return ids
}

func idsList(records []db.ProxyRecord) string {
	var parts []string
	for _, r := range records {
		parts = append(parts, fmt.Sprintf("%d", r.ID))
	}
	return strings.Join(parts, ", ")
}

func parseModelsStr(input string) map[string]string {
	m := make(map[string]string)
	for pair := range strings.SplitSeq(input, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		agent := strings.TrimSpace(parts[0])
		mod := strings.TrimSpace(parts[1])
		if agent != "" && mod != "" {
			m[agent] = mod
		}
	}
	return m
}

func getProxySource(cliURL, cliKey string) (*proxy.Proxy, string, error) {
	if cliURL != "" || cliKey != "" {
		p, err := proxy.FromFlags(cliURL, cliKey)
		if err != nil {
			return nil, "flags", err
		}
		if p != nil {
			return p, "flags", nil
		}
	}
	p, err := proxy.Detect()
	if err == nil && p != nil {
		return p, "auto-detect", nil
	}
	return nil, "auto-detect", err
}

// ModelSelectAction is the user's choice in an interactive model-selection prompt.
type ModelSelectAction int

const (
	// ActionAuto means the user accepted the recommended model (Enter or a number).
	ActionAuto ModelSelectAction = iota
	// ActionSkip means the user chose to skip this agent entirely.
	ActionSkip
	// ActionAcceptAll means the user accepted the recommendation and wants the
	// same model applied to every remaining agent (subsequent prompts are skipped).
	ActionAcceptAll
	// ActionQuit means the user aborted the entire conf set operation.
	ActionQuit
)

// isTerminalStdin reports whether os.Stdin looks like an interactive terminal.
// It returns false for pipes and redirections, so callers can skip prompting.
func isTerminalStdin() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// PromptModelSelection shows an interactive numbered list of upstream models and
// lets the user pick one for the given agent. Returns (chosenModel, action).
//
// If promptReader is nil (non-interactive mode), len(upstreamModels) <= 1, or
// the agent has no recommended model, it returns ("", ActionAuto) so the
// caller falls back to model.PickCustomModel.
func PromptModelSelection(agentName, sourceLabel string, upstreamModels []string, promptReader *bufio.Reader) (string, ModelSelectAction) {
	if promptReader == nil || len(upstreamModels) <= 1 {
		return "", ActionAuto
	}

	pickModel := model.PickCustomModel(agentName, upstreamModels)
	if pickModel == "" {
		return "", ActionAuto
	}

	// Find the recommended index and how PickCustomModel matched it.
	recIdx := 0
	for i, m := range upstreamModels {
		if m == pickModel {
			recIdx = i
			break
		}
	}
	pickSource := pickMatchSource(agentName, upstreamModels)

	fmt.Printf("\n[%s] 上游模型 (共 %d 个, %s):\n", agentName, len(upstreamModels), sourceLabel)
	for i, m := range upstreamModels {
		line := fmt.Sprintf("  %2d. %s", i+1, m)
		if m == pickModel {
			line += "                    ← 推荐 (" + pickSource + ")"
		}
		fmt.Println(line)
	}
	fmt.Printf("\n  选择 [%d-%d] (默认 %d, 直接回车使用推荐); s=跳过, a=接受并应用到后续, q=退出: ",
		1, len(upstreamModels), recIdx+1)

	input, err := promptReader.ReadString('\n')
	if err != nil {
		return "", ActionQuit
	}
	input = strings.TrimSpace(input)

	switch {
	case input == "":
		return pickModel, ActionAuto
	case strings.EqualFold(input, "s"):
		return "", ActionSkip
	case strings.EqualFold(input, "a"):
		return pickModel, ActionAcceptAll
	case strings.EqualFold(input, "q"):
		return "", ActionQuit
	default:
		n, convErr := strconv.Atoi(input)
		if convErr != nil || n < 1 || n > len(upstreamModels) {
			fmt.Printf("无效输入，使用推荐模型 %s。\n", pickModel)
			return pickModel, ActionAuto
		}
		return upstreamModels[n-1], ActionAuto
	}
}

// pickMatchSource reports which step of PickCustomModel matched the recommended model:
// "精确匹配" (exact), "关键字匹配" (keyword), or "兜底" (fallback first).
func pickMatchSource(agentName string, upstreamModels []string) string {
	defaultModel, _ := shared.GetDefaultModel(agentName)
	if defaultModel == "" {
		return "兜底"
	}
	for _, m := range upstreamModels {
		if strings.EqualFold(m, defaultModel) {
			return "精确匹配"
		}
	}
	for _, m := range upstreamModels {
		if strings.Contains(strings.ToLower(m), strings.ToLower(defaultModel)) {
			return "关键字匹配"
		}
	}
	return "兜底"
}