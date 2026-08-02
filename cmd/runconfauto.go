package cmd

import (
	"agent-nexus/internal/sniff"
	"fmt"
	"github.com/spf13/cobra"
	"strings"
)

// confAutoCmd is the legacy "conf auto" command, now a thin alias for "conf set".
//
// Legacy behaviour:
//   - auto-detect proxy only (no --url/--key/--db-id/--db on this command)
//   - branch fixed to "main"
//   - message: "conf auto: <agents>"
//   - no interactive model confirmation
//   - --dry-run supported
//
// Implementation: builds a runConfSetOpts with branch="main", the legacy
// message prefix, and forwards to the unified runConfSet(opts). This keeps
// "conf auto" backwards-compatible without duplicating the config pipeline.
var confAutoCmd = &cobra.Command{
	Use:   "auto --agents <list|all>",
	Short: "自动配置 agent（别名，建议迁移到 conf set）",
	Long: `自动配置指定 agent（通过 proxy.Detect 检测代理）。

此命令是 "conf set" 的别名，保留以兼容旧用法。
新代码请优先使用: agent-nexus conf set --agents all

代理来源：自动检测（proxy.Detect），不读取 --db / --db-id。

示例：
  agent-nexus conf auto --agents all
  agent-nexus conf auto --agents codex --models "codex=gpt-5.5"
  agent-nexus conf auto --agents all --dry-run
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfSetOpts{
			agents:  caAgents,
			models:  caModels,
			branch:  "main",
			message: "conf auto: " + caAgents,
			dryRun:  caDryRun,
		}

		// Legacy: warn if a --models override is absent from upstream model list.
		overrides := parseModelsStr(caModels)
		p, _ := getProxySettings()
		var upstreamModels []string
		if p != nil && p.BaseURL != "" && p.APIKey != "" {
			upstreamModels = sniff.UpstreamModelList(p.BaseURL, p.APIKey)
		}
		for _, m := range overrides {
			found := false
			for _, up := range upstreamModels {
				if strings.EqualFold(up, m) {
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("  [WARNING] 模型 %q 不存在于上游模型列表，请确认模型名正确\n", m)
			}
		}

		return runConfSet(opts)
	},
}

// Flags for confAutoCmd.
var (
	caAgents string
	caModels string
	caDryRun bool
)

func initConfAutoCmd() {
	caFlags := confAutoCmd.Flags()
	caFlags.StringVar(&caAgents, "agents", "", "要配置的 agent（逗号分隔），用 all 配置所有（必选）")
	caFlags.StringVar(&caModels, "models", "", "覆盖模型映射: \"agent=模型名,agent2=模型名\"")
	caFlags.BoolVar(&caDryRun, "dry-run", false, "预览模式，不实际写入")
	confAutoCmd.MarkFlagRequired("agents")
}

// runConfAuto is the original standalone implementation kept as a thin alias
// for backwards compatibility and for test fixtures (e.g. TestConfAutoDryRun_NoWrite).
//
// Delegates to runConfSet with branch="main" and the legacy message prefix.
func runConfAuto(agentsStr, modelsStr string, dryRun bool) error {
	opts := runConfSetOpts{
		agents:  agentsStr,
		models:  modelsStr,
		branch:  "main",
		message: "conf auto: " + agentsStr,
		dryRun:  dryRun,
	}
	return runConfSet(opts)
}

// parseAgentsStr splits and validates the --agents string argument.
// Returns (selectedNames, toConfigureNames, nil), where selectedNames is the
// user-specified list and toConfigureNames is the subset that are configurable.
func parseAgentsStr(agentsStr, configurableSet string) ([]string, []string, error) {
	if agentsStr == "" {
		return nil, nil, nil
	}

	selectedNames := strings.Split(agentsStr, ",")
	for i, n := range selectedNames {
		selectedNames[i] = strings.TrimSpace(n)
	}

	configurableNames := strings.Split(configurableSet, ",")

	var toConfigure []string
	if strings.EqualFold(agentsStr, "all") {
		for _, n := range configurableNames {
			toConfigure = append(toConfigure, strings.TrimSpace(n))
		}
	} else {
		selectedSet := make(map[string]bool)
		for _, name := range selectedNames {
			selectedSet[strings.TrimSpace(name)] = true
		}
		for _, n := range configurableNames {
			if name := strings.TrimSpace(n); selectedSet[name] {
				toConfigure = append(toConfigure, name)
			}
		}
	}

	return selectedNames, toConfigure, nil
}

// validateOverridesAgainstUpstream checks override values against the upstream
// model list. Returns warnings for any override model not found upstream.
func validateOverridesAgainstUpstream(overrides map[string]string, upstreamModels []string) []string {
	var warnings []string
	for _, modelName := range overrides {
		found := false
		for _, up := range upstreamModels {
			if strings.EqualFold(up, modelName) {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, modelName)
		}
	}
	return warnings
}
