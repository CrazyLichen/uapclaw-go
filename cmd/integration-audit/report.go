// Package main 提供 CLI 参数解析与报告输出。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// runCLI CLI 主入口：解析参数 → 读豁免清单 → 执行审计 →（可选）覆盖交叉验证 → 输出报告。
// 返回进程退出码（0=通过，1=存在违规，2=参数/运行错误）。
func runCLI(args []string) int {
	fs := flag.NewFlagSet("integration-audit", flag.ContinueOnError)
	root := fs.String("root", "tests/integration", "测试根目录")
	minSpan := fs.Int("min-call-span", 3, "真实集成测试要求的调用跨度下限")
	minAssertions := fs.Int("min-assertions", 2, "每个有效测试方法的最少断言数")
	minEffective := fs.Int("min-effective-tests", 1, "每个目录的最少有效测试方法数")
	exemptFile := fs.String("exempt-file", ".integration-scope-exempt", "豁免目录清单文件")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	failFast := fs.Bool("fail-fast", false, "首个违规即退出（默认输出全部）")
	deferSpan := fs.Bool("defer-span", false, "调用跨度不足的目录不判罚，标记 PendingCoverage（供覆盖验证步骤消费）")
	verify := fs.Bool("verify", false, "对灰区目录执行覆盖交叉验证（算法 2：逐目录 go test + 子系统覆盖行数门禁；隐含 -defer-span）")
	minSubsys := fs.Int("min-subsystems", defaultVerifySubsystems, "覆盖验证：真实覆盖子系统数下限")
	minLines := fs.Int("min-lines", defaultVerifyLines, "覆盖验证：每子系统覆盖行数下限")
	verifyWorkDir := fs.String("verify-workdir", "", "覆盖验证档案输出目录（默认临时目录）")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "参数解析失败: %v\n", err)
		return 2
	}

	cfg := &AuditConfig{
		RootDir:             *root,
		MinCallSpan:         *minSpan,
		MinAssertions:       *minAssertions,
		MinEffectiveTests:   *minEffective,
		ExemptDirs:          loadExemptFile(*exemptFile),
		DeferSpanToCoverage: *deferSpan || *verify,
	}

	result, err := Audit(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "审计失败: %v\n", err)
		return 2
	}

	// 覆盖交叉验证层：对灰区目录（PendingCoverage）逐目录运行 go test 裁决
	var verifyResult *VerifyResult
	if *verify {
		verifyResult, err = VerifyCoverage(&VerifyConfig{
			RepoRoot:      ".",
			MinSubsystems: *minSubsys,
			MinLines:      *minLines,
			WorkDir:       *verifyWorkDir,
		}, result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "覆盖交叉验证失败: %v\n", err)
			return 2
		}
		if !verifyResult.Passed {
			result.Passed = false
		}
	}

	if *jsonOut {
		payload := struct {
			Audit  *AuditResult  `json:"audit"`
			Verify *VerifyResult `json:"verify,omitempty"`
		}{Audit: result, Verify: verifyResult}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fmt.Fprintf(os.Stderr, "JSON 输出失败: %v\n", err)
			return 2
		}
	} else {
		printReport(result, *failFast)
		if verifyResult != nil {
			printVerifyReport(verifyResult)
		}
	}

	if result.Passed {
		return 0
	}
	return 1
}

// loadExemptFile 读取豁免清单：每行一个目录路径，# 注释与空行过滤。
func loadExemptFile(path string) map[string]string {
	exempts := make(map[string]string)
	if path == "" {
		return exempts
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return exempts // 文件不存在视为无豁免
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		exempts[line] = "registered"
	}
	return exempts
}

// printReport 输出人类可读的审计报告。
func printReport(result *AuditResult, failFast bool) {
	var violators, exempted, passing int
	var lines []string

	for _, da := range result.Dirs {
		if da.Exempt {
			exempted++
			lines = append(lines, fmt.Sprintf("  [豁免] %s", da.Dir))
			continue
		}
		if len(da.Violations) == 0 {
			passing++
			lines = append(lines, fmt.Sprintf("  [通过] %s（调用跨度 %d，有效测试 %d）",
				da.Dir, len(da.CallSpan), da.EffectiveTests))
			continue
		}
		violators++
		lines = append(lines, fmt.Sprintf("  [违规] %s", da.Dir))
		for _, v := range da.Violations {
			lines = append(lines, fmt.Sprintf("         - %s", v))
		}
		// 文件级违规（build tag / suite.Run / import 数）一并展示
		for _, fa := range da.Files {
			for _, v := range fa.Violations {
				lines = append(lines, fmt.Sprintf("         - %s: %s", fa.Path, v))
			}
		}
		if failFast {
			break
		}
	}

	fmt.Println("集成测试真实性审计（AST 调用分析 + 断言密度）")
	fmt.Println(strings.Repeat("─", 60))
	for _, l := range lines {
		fmt.Println(l)
	}
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("目录总计 %d：通过 %d，违规 %d，豁免 %d\n",
		len(result.Dirs), passing, violators, exempted)

	if !result.Passed {
		fmt.Println()
		fmt.Println("处置指引：")
		fmt.Println("  1. 符号引用凑数（调用跨度不足）→ 补真实跨子系统调用断言，或迁回 internal 包单元测试")
		fmt.Println("  2. 空壳目录（有效测试不足）→ 补充真实测试或删除")
		fmt.Println("  3. 确属集成性质但跨度不足 → 在 .integration-scope-exempt 登记并注明理由")
	}
}

// printVerifyReport 输出覆盖交叉验证报告（算法 2 层）。
func printVerifyReport(vr *VerifyResult) {
	fmt.Println()
	fmt.Println("覆盖交叉验证（灰区目录 → 逐目录 go test → 子系统覆盖行数门禁）")
	fmt.Println(strings.Repeat("─", 60))
	for _, dv := range vr.Dirs {
		if dv.Skipped {
			fmt.Printf("  [跳过] %s：%s\n", dv.Dir, dv.SkipReason)
			continue
		}
		status := "通过"
		if !dv.Passed {
			status = "不通过"
		}
		fmt.Printf("  [%s] %s：%s\n", status, dv.Dir, dv.Reason)
		// 展示各子系统覆盖行数（降序前 8 个）
		type sl struct {
			sub   string
			lines int
		}
		rows := make([]sl, 0, len(dv.SubsystemLines))
		for sub, lines := range dv.SubsystemLines {
			rows = append(rows, sl{sub, lines})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].lines > rows[j].lines })
		limit := 8
		if len(rows) < limit {
			limit = len(rows)
		}
		var parts []string
		for i := 0; i < limit; i++ {
			mark := ""
			if rows[i].lines >= 30 {
				mark = "*"
			}
			parts = append(parts, fmt.Sprintf("%s:%d%s", rows[i].sub, rows[i].lines, mark))
		}
		if len(rows) > limit {
			parts = append(parts, fmt.Sprintf("…共%d个子系统", len(rows)))
		}
		fmt.Printf("         覆盖分布：%s（* = ≥30行计入真实覆盖）\n", strings.Join(parts, ", "))
	}
	fmt.Println(strings.Repeat("─", 60))
	if !vr.Passed {
		fmt.Println("处置指引：")
		fmt.Println("  1. 覆盖行数集中于单一子系统（单模块测试性质）→ 迁回 internal 包单元测试")
		fmt.Println("  2. 确属集成性质但覆盖不足 → 补真实跨子系统测试路径")
		fmt.Println("  3. 确属集成性质但仅跨 1 个子系统（如真跨进程）→ 在 .integration-scope-exempt 登记并注明理由")
	}
}
