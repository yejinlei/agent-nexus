package cmd

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"agent-nexus/internal/db"
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
func resolveAgentList(agentsStr string) ([]string, error) {
	configurable := make([]string, 0, len(shared.DefaultModels))
	for name := range shared.DefaultModels {
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