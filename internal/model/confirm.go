package model

import (
    "bufio"
    "fmt"
    "os"
    "strings"
)

// ConfirmModelMappings displays the resolved model mappings and lets the user
// confirm or override them.
//
// The function presents a table of agent → resolved model with source info.
// The user is prompted:
//   - Press Enter to accept all current mappings
//   - Type an agent name to change its model
//   - Type "q" to abort the configure operation
//
// Returns (proceed bool, overrides map[agentName]modelName).
// If proceed is false, the caller should abort configuration.
// overrides is a map of agentName -> desiredModelName; entries not present
// keep their auto-resolved values.
func ConfirmModelMappings(resolutions []Resolution) (bool, map[string]string) {
    overrides := make(map[string]string)

    fmt.Println()
    fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    fmt.Println("  模型配置预览（请先阅读）")
    fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    fmt.Printf("  %-14s %-30s → %-30s [%s]\n", "Agent", "配置模型", "实际模型", "来源")
    fmt.Println(strings.Repeat("-", 110))

    for _, r := range resolutions {
        sourceLabel := r.Source
        if sourceLabel == "upstream" {
            sourceLabel = "上游直接"
        } else if sourceLabel == "proxy-map" {
            sourceLabel = "代理重定向"
        } else {
            sourceLabel = "默认"
        }
        fmt.Printf("  %-14s %-30s → %-30s [%s]\n",
            r.Agent, r.Model, r.Model, sourceLabel)
        if r.Notes != "" {
            fmt.Printf("    └─ %s\n", r.Notes)
        }
    }
    fmt.Println()
    fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    fmt.Println("  操作说明:")
    fmt.Println("    回车:  使用以上映射，继续配置")
    fmt.Println("    输入 agent 名: 修改该 agent 的模型")
    fmt.Println("    q:  取消配置")
    fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    fmt.Print("请确认映射关系（回车确认 / 输入 agent 名修改 / q 取消）: ")

    reader := bufio.NewReader(os.Stdin)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println()
            return false, nil
        }
        input := strings.TrimSpace(line)

        if input == "" {
            // Accept all
            return true, overrides
        }

        if strings.EqualFold(input, "q") {
            fmt.Println("配置已取消。")
            return false, nil
        }

        // Look up agent by name
        agentFound := false
        for _, r := range resolutions {
            if strings.EqualFold(r.Agent, input) {
                agentFound = true
                break
            }
        }
        if !agentFound {
            // Check if input is a model name
            if _, ok := overrides[input]; !ok {
                // Treat as an invalid agent name
                fmt.Printf("未知 agent: %s\n", input)
                fmt.Print("请确认映射关系（回车确认 / 输入 agent 名修改 / q 取消）: ")
                continue
            }
        }

        // Valid agent name — prompt for new model
        fmt.Printf("\n为 [%s] 指定新模型: ", input)
        newModel, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println()
            return false, nil
        }
        newModel = strings.TrimSpace(newModel)
        if newModel == "" {
            fmt.Printf("[%s] 保持原模型。\n", input)
        } else {
            overrides[input] = newModel
            fmt.Printf("已将 [%s] 模型修改为: %s\n", input, newModel)
        }
        fmt.Println()
        fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        // Re-display with overrides
        for _, r := range resolutions {
            displayModel := r.Model
            sourceLabel := r.Source
            if v, ok := overrides[r.Agent]; ok {
                displayModel = v
            }
            if sourceLabel == "upstream" {
                sourceLabel = "上游直接"
            } else if sourceLabel == "proxy-map" {
                sourceLabel = "代理重定向"
            } else {
                sourceLabel = "默认"
            }
            fmt.Printf("  %-14s %-30s → %-30s [%s]\n",
                r.Agent, displayModel, displayModel, sourceLabel)
        }
        fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        fmt.Print("请确认映射关系（回车确认 / 输入 agent 名修改 / q 取消）: ")
    }
}

// ModelToWrite returns the final model name for an agent, applying user
// overrides on top of the auto-resolved mapping.
func ModelToWrite(resolutions []Resolution, overrides map[string]string, agentName string) (string, bool) {
    for _, r := range resolutions {
        if strings.EqualFold(r.Agent, agentName) {
            if override, ok := overrides[agentName]; ok {
                return override, true
            }
            return r.Model, true
        }
    }
    return "", false
}
