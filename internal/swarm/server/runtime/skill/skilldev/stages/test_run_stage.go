package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TestRunStageHandler TEST_RUN 阶段：子 Agent 并行执行测试用例（with_skill vs baseline）。
type TestRunStageHandler struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 TEST_RUN 阶段逻辑。
func (h *TestRunStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	evals := sctx.State.Evals
	if evals == nil {
		return nil, fmt.Errorf("TEST_RUN 阶段缺少测试用例，请先完成 TEST_DESIGN 阶段")
	}
	evalCases, _ := evals["evals"].([]any)
	if len(evalCases) == 0 {
		return nil, fmt.Errorf("TEST_RUN 阶段缺少测试用例，请先完成 TEST_DESIGN 阶段")
	}

	iteration := sctx.State.Iteration
	iterDir := filepath.Join(sctx.Workspace, "evals", fmt.Sprintf("iteration-%d", iteration))
	_ = os.MkdirAll(iterDir, 0o755)

	totalTasks := len(evalCases) * 2 // with_skill + baseline
	sctx.Emit(skilldev.SkillDevEventTypeTestProgress, map[string]any{
		"total":     totalTasks,
		"completed": 0,
		"message":   fmt.Sprintf("开始执行 %d 个测试用例...", len(evalCases)),
	})

	_ = h.runAllEvals(sctx, evalCases, iterDir)

	sctx.Emit(skilldev.SkillDevEventTypeTestProgress, map[string]any{
		"total":     totalTasks,
		"completed": totalTasks,
		"message":   "测试执行完成",
	})

	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageEvaluate}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runAllEvals 并行执行所有测试用例。
//
// 待实现: 接入 SkillDevTestRunner，为每个用例创建 with_skill + baseline 子 Agent
func (h *TestRunStageHandler) runAllEvals(sctx *skilldev.SkillDevContext, evalCases []any, iterDir string) []map[string]any {
	// 待实现:
	// tasks = []
	// for case in evalCases:
	//     caseDir := filepath.Join(iterDir, case["name"])
	//     os.MkdirAll(caseDir, 0o755)
	//     tasks.append(runSingleEval(sctx, case, caseDir))
	// results := await asyncio.gather(*tasks, return_exceptions=True)
	// return results

	logger.Warn(logComponent).Msg("[TestRunStage] runAllEvals 尚未实现，写入占位结果")
	results := make([]map[string]any, 0)
	timingPlaceholder := map[string]any{
		"total_tokens":           0,
		"duration_ms":            0,
		"total_duration_seconds": 0.0,
	}

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
		withSkillDir := filepath.Join(caseDir, "with_skill")
		baselineDir := filepath.Join(caseDir, "baseline")
		_ = os.MkdirAll(withSkillDir, 0o755)
		_ = os.MkdirAll(baselineDir, 0o755)

		// 写入 eval_metadata.json（对齐官方格式）
		evalMetadata := map[string]any{
			"eval_id":    evalID,
			"eval_name":  evalName,
			"prompt":     getStrFromAny(caseMap["prompt"]),
			"assertions": getSliceFromAny(caseMap["assertions"]),
		}
		metaData, _ := json.MarshalIndent(evalMetadata, "", "  ")
		_ = os.WriteFile(filepath.Join(caseDir, "eval_metadata.json"), metaData, 0o644)

		// 占位 result.json + timing.json
		for _, config := range []string{"with_skill", "baseline"} {
			configDir := filepath.Join(caseDir, config)
			_ = os.WriteFile(filepath.Join(configDir, "result.json"), []byte(`{"status": "待实现", "output": "待实现"}`), 0o644)
			timingData, _ := json.MarshalIndent(timingPlaceholder, "", "  ")
			_ = os.WriteFile(filepath.Join(configDir, "timing.json"), timingData, 0o644)
		}

		results = append(results, map[string]any{"eval_id": evalID, "status": "placeholder"})
		sctx.Emit(skilldev.SkillDevEventTypeTestProgress, map[string]any{
			"message": fmt.Sprintf("已完成（占位）：%s", evalName),
		})
	}
	return results
}

// toIntFromAny 从 any 转为 int。
func toIntFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// getStrFromAny 从 any 获取字符串值。
func getStrFromAny(v any) string {
	s, _ := v.(string)
	return s
}

// getSliceFromAny 从 any 获取 []any。
func getSliceFromAny(v any) []any {
	s, _ := v.([]any)
	return s
}
