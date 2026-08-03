package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"agent-nexus/internal/db"
	"agent-nexus/internal/sniff"

	"github.com/spf13/cobra"
)

// dbCmd is the top-level database management command.
// Subcommands: add, list, show, rm, query, check.
var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "管理已嗅探的代理配置数据库（嵌入式 SQLite）",
	Long: `管理已嗅探的代理配置数据库。

子命令：
  add     嗅探代理并保存到数据库
  list    列出已保存的代理配置
  show    显示指定代理配置详情
  rm      删除指定代理配置
  query   查询代理配置（可选按 ID 或 URL 过滤）
  check   [已弃用] 检查代理有效性，请使用 'proxy check'

用法：
  agent-nexus db add -u https://api.example.com/v1 -k sk-xxx
  agent-nexus db list
  agent-nexus db show <id>
  agent-nexus db rm <id>
  agent-nexus db query [filter]
  agent-nexus proxy check <id>
`,
}

var dbAddCmd = &cobra.Command{
	Use:   "add -u <url> -k <key>",
	Short: "嗅探代理并保存到数据库",
	Long: `嗅探指定的 LLM 代理 endpoint，如果成功则自动保存到嵌入式 SQLite 数据库中。

用法：
  agent-nexus db add -u https://api.example.com/v1 -k sk-xxx
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := sniff.Sniff(sniffURL, sniffKey)
		if err != nil {
			fmt.Printf("嗅探失败: %v\n", err)
			return err
		}

		dbInst, err := db.New()
		if err != nil {
			return fmt.Errorf("打开数据库失败: %v", err)
		}
		defer dbInst.Close()
		if err := dbInst.Init(); err != nil {
			return fmt.Errorf("初始化数据库失败: %v", err)
		}

		modelIDs := make([]string, 0, len(result.Models))
		for _, m := range result.Models {
			modelIDs = append(modelIDs, m.ID)
		}

		if err := dbInst.Add(result.BaseURL, sniffKey, result.DetectedFormat, result.OpenAICap, result.AnthropicCap, result.ResponsesCap, result.ModelCount, modelIDs, time.Now()); err != nil {
			fmt.Printf("保存到数据库失败: %v\n", err)
			return err
		}

		fmt.Printf("\n✅ 已保存到数据库: %s\n", result.BaseURL)
		fmt.Printf("  检测格式: %s\n", result.DetectedFormat)
		fmt.Printf("  OpenAI chat: %v | Anthropic messages: %v | Responses API: %v\n", result.OpenAICap, result.AnthropicCap, result.ResponsesCap)
		fmt.Printf("  模型数量: %d\n", result.ModelCount)
		fmt.Printf("  时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println()
		return nil
	},
}

var dbListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出已保存的代理配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbInst, err := db.New()
		if err != nil {
			return fmt.Errorf("打开数据库失败: %v", err)
		}
		defer dbInst.Close()
		if err := dbInst.Init(); err != nil {
			return fmt.Errorf("初始化数据库失败: %v", err)
		}
		records, err := dbInst.List()
		if err != nil {
			return fmt.Errorf("读取数据库失败: %v", err)
		}
		if len(records) == 0 {
			fmt.Println("数据库为空，没有已保存的代理配置。")
			return nil
		}
		fmt.Printf("\n已保存的代理配置 (%d 条):\n", len(records))
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("  %-6s  %-45s  %-30s  %s\n", "ID", "URL", "检测格式", "时间")
		fmt.Println(strings.Repeat("-", 80))
		for _, r := range records {
			fmt.Printf("  %-6d  %-45s  %-30s  %s\n", r.ID, r.URL, r.DetectedFormat, r.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
		return nil
	},
}

var dbShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "显示指定代理配置详情",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("请指定代理配置 ID\n\n用法: agent-nexus db show <id>")
		}
		dbInst, err := db.New()
		if err != nil {
			return fmt.Errorf("打开数据库失败: %v", err)
		}
		defer dbInst.Close()
		if err := dbInst.Init(); err != nil {
			return fmt.Errorf("初始化数据库失败: %v", err)
		}
		record, err := dbInst.GetByID(parseInt(args[0]))
		if err != nil {
			fmt.Printf("查询失败: %v\n", err)
			return err
		}
		if record == nil {
			fmt.Printf("未找到 ID 为 %s 的代理配置\n", args[0])
			return nil
		}
		fmt.Printf("\n代理配置详情:\n")
		fmt.Printf("  ID:        %d\n", record.ID)
		fmt.Printf("  URL:       %s\n", record.URL)
		fmt.Printf("  Key:       %s\n", record.Key)
		fmt.Printf("  检测格式:  %s\n", record.DetectedFormat)
		fmt.Printf("  OpenAI:    %v\n", record.OpenAICap)
		fmt.Printf("  Anthropic: %v\n", record.AnthropicCap)
		fmt.Printf("  模型数量:  %d\n", record.ModelCount)
		fmt.Printf("  时间:      %s\n", record.CreatedAt.Format("2006-01-02 15:04:05"))
		var modelNames []string
		if record.ModelsJSON != "" {
			json.Unmarshal([]byte(record.ModelsJSON), &modelNames)
		}
		if len(modelNames) > 0 {
			fmt.Printf("  模型列表 (%d):\n", len(modelNames))
			for i, name := range modelNames {
				fmt.Printf("    %3d. %s\n", i+1, name)
			}
		}
		fmt.Println()
		return nil
	},
}

var dbRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "删除指定代理配置（使用 --all 删除全部）",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rmAll {
			dbInst, err := db.New()
			if err != nil {
				return fmt.Errorf("打开数据库失败: %v", err)
			}
			defer dbInst.Close()
			if err := dbInst.Init(); err != nil {
				return fmt.Errorf("初始化数据库失败: %v", err)
			}
			fmt.Printf("⚠  这将删除数据库中全部代理配置记录，自增 ID 将重置为 1。")
			fmt.Print("  确定删除？(yes/no): ")
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("读取输入失败: %v", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "yes" {
				fmt.Println("操作已取消。")
				return nil
			}
			if err := dbInst.TruncateReset(); err != nil {
				fmt.Printf("删除全部记录失败: %v\n", err)
				return err
			}
			fmt.Printf("✅ 已删除全部代理配置\n")
			return nil
		}
		if len(args) < 1 {
			return fmt.Errorf("请指定代理配置 ID\n\n用法: agent-nexus db rm <id>")
		}
		dbInst, err := db.New()
		if err != nil {
			return fmt.Errorf("打开数据库失败: %v", err)
		}
		defer dbInst.Close()
		if err := dbInst.Init(); err != nil {
			return fmt.Errorf("初始化数据库失败: %v", err)
		}
		if err := dbInst.Delete(parseInt(args[0])); err != nil {
			fmt.Printf("删除失败: %v\n", err)
			return err
		}
		fmt.Printf("✅ 已删除 ID 为 %s 的代理配置\n", args[0])
		return nil
	},
}

var dbQueryCmd = &cobra.Command{
	Use:   "query [filter]",
	Short: "查询代理配置（可选按 ID 或 URL 过滤）",
	Long: `查询已保存的代理配置记录。可指定过滤条件：
  - 数字 ID：按 ID 精确查询
  - 字符串：按 URL 子串模糊查询
  - 空：列出所有记录

用法：
  agent-nexus db query
  agent-nexus db query 1
  agent-nexus db query example.com
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbInst, err := db.New()
		if err != nil {
			return fmt.Errorf("打开数据库失败: %v", err)
		}
		defer dbInst.Close()
		if err := dbInst.Init(); err != nil {
			return fmt.Errorf("初始化数据库失败: %v", err)
		}

		filter := ""
		if len(args) > 0 {
			filter = args[0]
		}
		records, err := dbInst.Query(filter)
		if err != nil {
			return fmt.Errorf("查询失败: %v", err)
		}
		if len(records) == 0 {
			if filter != "" {
				fmt.Printf("未找到匹配 '%s' 的代理配置。\n", filter)
			} else {
				fmt.Println("数据库为空，没有已保存的代理配置。")
			}
			return nil
		}

		if filter != "" {
			fmt.Printf("\n查询结果（过滤条件: %s）(%d 条):\n", filter, len(records))
		} else {
			fmt.Printf("\n已保存的代理配置 (%d 条):\n", len(records))
		}
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("  %-6s  %-45s  %-30s  %s\n", "ID", "URL", "检测格式", "时间")
		fmt.Println(strings.Repeat("-", 80))
		for _, r := range records {
			fmt.Printf("  %-6d  %-45s  %-30s  %s\n", r.ID, r.URL, r.DetectedFormat, r.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
		return nil
	},
}

var dbCheckCmd = &cobra.Command{
	Use:   "check <id>",
	Short: "[已弃用] 请使用 proxy check",
	Long: `检查 db 中保存的代理配置是否仍然有效。

[已弃用] 推荐使用:
  agent-nexus proxy check <id>
  agent-nexus proxy check --all
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProxyCheck(args, checkAll)
	},
}

// initDbCmd registers the db command and its subcommands.
// This is called from root.go's init() before initProxyCmd().
func initDbCmd() {
	dbAddCmd.Flags().StringVar(&sniffURL, "url", "", "LLM provider endpoint URL（必选）")
	dbAddCmd.Flags().StringVar(&sniffKey, "key", "", "LLM provider API key（必选）")
	dbAddCmd.MarkFlagRequired("url")
	dbAddCmd.MarkFlagRequired("key")
	dbAddCmd.Flags().BoolVarP(&sniffVerbose, "verbose", "v", false, "显示每个模型的详细信息")

	dbRmCmd.Flags().BoolVar(&rmAll, "all", false, "删除全部代理配置并重置 ID 计数器")
	dbCheckCmd.Flags().BoolVar(&checkAll, "all", false, "[已弃用] 请使用 proxy check --all")

	dbCmd.AddCommand(dbAddCmd)
	dbCmd.AddCommand(dbListCmd)
	dbCmd.AddCommand(dbShowCmd)
	dbCmd.AddCommand(dbRmCmd)
	dbCmd.AddCommand(dbQueryCmd)
	dbCmd.AddCommand(dbCheckCmd)
}
