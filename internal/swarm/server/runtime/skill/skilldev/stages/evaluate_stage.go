package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvaluateStageHandler EVALUATE 阶段：Grader 评分 → Benchmark 聚合 → Analyst 分析。
type EvaluateStageHandler struct{}

// ──────────────────────────── 常量 ────────────────────────────

// GraderSystemPrompt Grader Agent 系统 Prompt。
const GraderSystemPrompt = `你是一个评测 Grader。读取执行 transcript 和 output 文件，评估每条 expectation 是否通过。

## 评分标准

**PASS**：
- transcript / outputs 中有明确证据证明 expectation 为真
- 证据反映的是真实完成，不是表面合规（文件存在 ≠ 内容正确）

**FAIL**：
- 找不到证据，或证据与 expectation 矛盾
- 证据是表面的（文件名对但内容错/空）
- 无法从可用信息中验证

**不确定时**：按 FAIL 处理（举证责任在 expectation 一方）。

## 输出要求

对每条 expectation 输出：
- text: expectation 原文
- passed: true/false
- evidence: 引用的具体文本/描述

还需输出：
- summary: {passed, failed, total, pass_rate}
`

// AnalystSystemPrompt Analyst Agent 系统 Prompt。
const AnalystSystemPrompt = `你是一个 Benchmark 分析师。分析所有评测运行结果，发现 aggregate 统计隐藏的模式。

关注维度：
- 某 expectation 在 with_skill 和 baseline 都 100% pass → 不具区分力
- 某 expectation 在两者都 fail → 超出能力或 expectation 本身有问题
- 某 eval 高方差 → 可能是 flaky 测试
- with_skill 反而劣于 baseline 的指标 → skill 可能在某方面产生负面影响
- 时间/token 开销 vs 通过率的权衡

输出一个 JSON 字符串数组，每条是一句简洁的观察（用中文）：
["观察1", "观察2", ...]
`

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 EVALUATE 阶段逻辑。
func (h *EvaluateStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	iteration := sctx.State.Iteration
	iterDir := filepath.Join(sctx.Workspace, "evals", fmt.Sprintf("iteration-%d", iteration))

	// --- Step 1: Grader 评分 ---
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在对测试结果进行评分..."})
	h.gradeAllEvals(sctx, iterDir)

	// --- Step 2: Benchmark 聚合 ---
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在聚合 benchmark 统计..."})
	benchmark := h.aggregateBenchmark(sctx, iterDir)

	// --- Step 3: Analyst 分析 ---
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在分析评测模式..."})
	analystNotes := h.analyzePatterns(sctx, benchmark)
	benchmark.Notes = analystNotes

	// 持久化
	benchmarkDict := benchmark.ToDict()
	benchmarkData, _ := json.MarshalIndent(benchmarkDict, "", "  ")
	_ = os.WriteFile(filepath.Join(iterDir, "benchmark.json"), benchmarkData, 0o644)

	reportMD := renderBenchmarkMD(benchmark)
	_ = os.WriteFile(filepath.Join(iterDir, "benchmark.md"), []byte(reportMD), 0o644)

	sctx.State.EvalResults = map[string]any{"benchmark": benchmarkDict, "report": reportMD}

	// 推送给前端 — 前端根据 benchmark JSON 渲染评测面板
	sctx.Emit(skilldev.SkillDevEventTypeEvalReady, map[string]any{
		"benchmark": benchmarkDict,
		"iteration": iteration,
	})
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageReview}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// gradeAllEvals 为每个 eval 的 with_skill / baseline 结果执行评分。
//
// 待实现: 接入 create_stage_agent，用 GraderSystemPrompt 调用 Agent
func (h *EvaluateStageHandler) gradeAllEvals(sctx *skilldev.SkillDevContext, iterDir string) {
	evals := sctx.State.Evals
	if evals == nil {
		return
	}
	evalCases, _ := evals["evals"].([]any)

	for _, ec := range evalCases {
		caseMap, _ := ec.(map[string]any)
		evalName := "eval-0"
		if name, ok := caseMap["name"].(string); ok {
			evalName = name
		}
		caseDir := filepath.Join(iterDir, evalName)

		expectations, _ := caseMap["expectations"].([]any)

		for _, config := range []string{"with_skill", "baseline"} {
			runDir := filepath.Join(caseDir, config)
			if _, err := os.Stat(runDir); os.IsNotExist(err) {
				continue
			}

			// 待实现: 实际调用 Agent 评分
			// 待实现：创建评分Agent agent, err := sctx.CreateStageAgent("grader", GraderSystemPrompt, ...)
			// 待实现：读取评分记录 transcript := os.ReadFile(filepath.Join(runDir, "transcript.md"))
			// 待实现：调用Agent评分 grading := await agent.grade(expectations, transcript, filepath.Join(runDir, "outputs"))

			gradingExpectations := make([]skilldev.GradingExpectation, 0)
			for _, exp := range expectations {
				expStr, _ := exp.(string)
				gradingExpectations = append(gradingExpectations, skilldev.GradingExpectation{
					Text:     expStr,
					Passed:   false,
					Evidence: "待 Agent 实现",
				})
			}

			grading := skilldev.GradingResult{
				Expectations: gradingExpectations,
				PassRate:     0.0,
				PassedCount:  0,
				FailedCount:  len(expectations),
			}

			gradingData, _ := json.MarshalIndent(grading.ToDict(), "", "  ")
			_ = os.WriteFile(filepath.Join(runDir, "grading.json"), gradingData, 0o644)
		}
	}
}

// aggregateBenchmark 遍历所有 grading.json + timing.json，聚合为 Benchmark。
func (h *EvaluateStageHandler) aggregateBenchmark(sctx *skilldev.SkillDevContext, iterDir string) *skilldev.Benchmark {
	evals := sctx.State.Evals
	if evals == nil {
		evals = make(map[string]any)
	}
	evalCases, _ := evals["evals"].([]any)

	skillName := ""
	if sctx.State.Plan != nil {
		if sn, ok := sctx.State.Plan["skill_name"].(string); ok {
			skillName = sn
		}
	}

	configs := make(map[string][]skilldev.BenchmarkRun)

	for _, ec := range evalCases {
		caseMap, _ := ec.(map[string]any)
		evalName := "eval-0"
		if name, ok := caseMap["name"].(string); ok {
			evalName = name
		}
		evalID := 0
		if id, ok := caseMap["id"]; ok {
			evalID = toIntFromAny(id)
		}

		caseDir := filepath.Join(iterDir, evalName)
		if _, err := os.Stat(caseDir); os.IsNotExist(err) {
			continue
		}

		// 遍历 caseDir 下的子目录（with_skill, baseline）
		entries, err := os.ReadDir(caseDir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			config := entry.Name()
			configDir := filepath.Join(caseDir, config)

			gradingFile := filepath.Join(configDir, "grading.json")
			timingFile := filepath.Join(configDir, "timing.json")
			if _, err := os.Stat(gradingFile); os.IsNotExist(err) {
				continue
			}

			grading := readJSONMap(gradingFile)
			timing := readJSONMap(timingFile)

			passRate := 0.0
			if summary, ok := grading["summary"].(map[string]any); ok {
				if pr, ok := summary["pass_rate"].(float64); ok {
					passRate = pr
				}
			}

			timeSeconds := 0.0
			if tds, ok := timing["total_duration_seconds"].(float64); ok {
				timeSeconds = tds
			}

			tokens := 0
			if t, ok := timing["total_tokens"].(float64); ok {
				tokens = int(t)
			}

			var expectations []map[string]any
			if exp, ok := grading["expectations"].([]any); ok {
				expectations = make([]map[string]any, 0, len(exp))
				for _, e := range exp {
					if m, ok := e.(map[string]any); ok {
						expectations = append(expectations, m)
					}
				}
			}

			run := skilldev.BenchmarkRun{
				EvalID:        evalID,
				EvalName:      evalName,
				Configuration: config,
				PassRate:      passRate,
				TimeSeconds:   timeSeconds,
				Tokens:        tokens,
				Expectations:  expectations,
			}
			configs[config] = append(configs[config], run)
		}
	}

	// 聚合统计
	runSummary := make(map[string]any)
	for config, runs := range configs {
		passRates := make([]float64, len(runs))
		timeSeconds := make([]float64, len(runs))
		tokens := make([]float64, len(runs))
		for i, r := range runs {
			passRates[i] = r.PassRate
			timeSeconds[i] = r.TimeSeconds
			tokens[i] = float64(r.Tokens)
		}
		prStats := skilldev.CalcStats(passRates)
		tsStats := skilldev.CalcStats(timeSeconds)
		tokStats := skilldev.CalcStats(tokens)
		runSummary[config] = map[string]any{
			"pass_rate":    prStats.ToDict(),
			"time_seconds": tsStats.ToDict(),
			"tokens":       tokStats.ToDict(),
		}
	}

	// 计算 delta
	configNames := sortedKeys(configs)
	if len(configNames) >= 2 {
		a, _ := runSummary[configNames[0]].(map[string]any)
		b, _ := runSummary[configNames[1]].(map[string]any)
		if a != nil && b != nil {
			aPR, _ := a["pass_rate"].(map[string]any)
			bPR, _ := b["pass_rate"].(map[string]any)
			aTS, _ := a["time_seconds"].(map[string]any)
			bTS, _ := b["time_seconds"].(map[string]any)
			aTok, _ := a["tokens"].(map[string]any)
			bTok, _ := b["tokens"].(map[string]any)

			delta := make(map[string]any)
			if aPR != nil && bPR != nil {
				aMean, _ := aPR["mean"].(float64)
				bMean, _ := bPR["mean"].(float64)
				delta["pass_rate"] = fmt.Sprintf("%+.2f", aMean-bMean)
			}
			if aTS != nil && bTS != nil {
				aMean, _ := aTS["mean"].(float64)
				bMean, _ := bTS["mean"].(float64)
				delta["time_seconds"] = fmt.Sprintf("%+.1f", aMean-bMean)
			}
			if aTok != nil && bTok != nil {
				aMean, _ := aTok["mean"].(float64)
				bMean, _ := bTok["mean"].(float64)
				delta["tokens"] = fmt.Sprintf("%+.0f", aMean-bMean)
			}
			runSummary["delta"] = delta
		}
	}

	// 收集所有 runs
	allRuns := make([]skilldev.BenchmarkRun, 0)
	for _, runs := range configs {
		allRuns = append(allRuns, runs...)
	}

	return &skilldev.Benchmark{
		SkillName:  skillName,
		Runs:       allRuns,
		RunSummary: runSummary,
		Timestamp:  nowISO(),
	}
}

// analyzePatterns 分析 benchmark 结果，发现隐藏模式。
//
// 待实现: 接入 create_stage_agent，用 AnalystSystemPrompt 调用 Agent
func (h *EvaluateStageHandler) analyzePatterns(_ *skilldev.SkillDevContext, _ *skilldev.Benchmark) []string {
	// 待实现: 实际调用 Agent
	// 待实现：创建分析Agent agent, err := sctx.CreateStageAgent("analyst", AnalystSystemPrompt, ...)
	// 待实现：调用Agent分析 notes := await agent.analyze(json.dumps(benchmark.ToDict()))
	// 待实现：解析结果 return json.loads(notes)

	logger.Warn(logComponent).Msg("[EvaluateStage] analyzePatterns 待接入 Agent")
	return []string{"评测分析 Agent 尚未接入"}
}

// readJSONMap 读取 JSON 文件为 map[string]any。
func readJSONMap(filePath string) map[string]any {
	result := make(map[string]any)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return result
	}
	_ = json.Unmarshal(data, &result)
	return result
}

// sortedKeys 返回 map 的已排序键列表。
func sortedKeys(m map[string][]skilldev.BenchmarkRun) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nowISO 返回当前 UTC 时间的 ISO 8601 字符串。
func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// renderBenchmarkMD 把 Benchmark 渲染为 Markdown 报告。
func renderBenchmarkMD(benchmark *skilldev.Benchmark) string {
	rs := benchmark.RunSummary
	configs := make([]string, 0)
	for k := range rs {
		if k != "delta" {
			configs = append(configs, k)
		}
	}
	sort.Strings(configs)

	lines := []string{
		fmt.Sprintf("# Skill Benchmark: %s", benchmark.SkillName),
		"",
		fmt.Sprintf("**Date**: %s", benchmark.Timestamp),
		"",
		"## Summary",
		"",
	}

	if len(configs) >= 2 {
		aName, bName := configs[0], configs[1]
		a, _ := rs[aName].(map[string]any)
		b, _ := rs[bName].(map[string]any)
		if a != nil && b != nil {
			delta, _ := rs["delta"].(map[string]any)

			lines = append(lines, fmt.Sprintf("| Metric | %s | %s | Delta |", aName, bName))
			lines = append(lines, "|--------|---------|---------|-------|")

			aPR, _ := a["pass_rate"].(map[string]any)
			bPR, _ := b["pass_rate"].(map[string]any)
			if aPR != nil && bPR != nil {
				aMean, _ := aPR["mean"].(float64)
				aStddev, _ := aPR["stddev"].(float64)
				bMean, _ := bPR["mean"].(float64)
				bStddev, _ := bPR["stddev"].(float64)
				deltaPR := "—"
				if delta != nil {
					if d, ok := delta["pass_rate"].(string); ok {
						deltaPR = d
					}
				}
				lines = append(lines, fmt.Sprintf(
					"| Pass Rate | %.0f%% ± %.0f%% | %.0f%% ± %.0f%% | %s |",
					aMean*100, aStddev*100, bMean*100, bStddev*100, deltaPR,
				))
			}
		}
	}

	if len(benchmark.Notes) > 0 {
		lines = append(lines, "", "## Analyst Notes", "")
		for _, note := range benchmark.Notes {
			lines = append(lines, fmt.Sprintf("- %s", note))
		}
	}

	return strings.Join(lines, "\n")
}
