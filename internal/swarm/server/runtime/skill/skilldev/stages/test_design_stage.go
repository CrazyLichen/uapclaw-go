package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TestDesignStageHandler TEST_DESIGN 阶段：Agent 设计测试用例，输出 evals.json。
type TestDesignStageHandler struct{}

// ──────────────────────────── 常量 ────────────────────────────

// TestDesignSystemPrompt TEST_DESIGN 阶段 Agent 系统 Prompt。
const TestDesignSystemPrompt = `根据以下 Skill 内容，设计 {count} 个测试用例。

## 测试用例设计原则

### prompt 要求（对齐官方标准）
- 模拟真实用户输入：包含文件路径、个人背景、具体数据名称等细节
- 混合不同长度和表达风格（正式/随意/简短/详细）
- 覆盖不同复杂度和边缘场景
- 有些用户不会明确提到 skill 名称，但确实需要这个 skill 的功能

### expectations（assertions）要求
- 每条 expectation 是一个可客观验证的声明（字符串）
- 使用描述性名称，让阅读者一眼理解检查的内容
- 好的 expectation 是 *区分性的*：使用 skill 时通过，不使用时大概率失败
- 避免太容易通过的检查（如只检查文件名存在，不检查内容）
- 主观性输出（写作风格、设计质量）更适合人工评审，不强加 expectations

### 输出 JSON 格式（对齐官方 evals.json schema）
{
  "skill_name": "{skill_name}",
  "evals": [
    {
      "id": 1,
      "prompt": "模拟用户的真实输入...",
      "expected_output": "预期结果的人类可读描述",
      "files": [],
      "expectations": [
        "输出中包含 X 的结构化数据",
        "使用了 scripts/ 中的 Y 脚本"
      ]
    }
  ]
}
`

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 TEST_DESIGN 阶段逻辑。
func (h *TestDesignStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在设计测试用例..."})

	skillContent := h.readSkillFiles(filepath.Join(sctx.Workspace, "skill"))
	evals := h.designEvals(sctx, skillContent)

	sctx.State.Evals = evals

	// 写入 evals.json
	evalsDir := filepath.Join(sctx.Workspace, "evals")
	_ = os.MkdirAll(evalsDir, 0o755)
	evalsFile := filepath.Join(evalsDir, "evals.json")
	data, _ := json.MarshalIndent(evals, "", "  ")
	_ = os.WriteFile(evalsFile, data, 0o644)

	// 统计测试用例数量
	count := 0
	if evalCases, ok := evals["evals"].([]any); ok {
		count = len(evalCases)
	}
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message": fmt.Sprintf("已设计 %d 个测试用例", count),
	})
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageTestRun}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readSkillFiles 读取 skill 目录下所有文件，拼接为字符串供 Agent 分析。
func (h *TestDesignStageHandler) readSkillFiles(skillDir string) string {
	var parts []string
	entries, err := walkFiles(skillDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			logger.Warn(logComponent).
				Str("file", entry).
				Err(err).
				Msg("[TestDesignStage] 读取文件失败")
			continue
		}
		rel, _ := filepath.Rel(skillDir, entry)
		parts = append(parts, fmt.Sprintf("=== %s ===\n%s", rel, string(data)))
	}
	return joinStrings(parts, "\n\n")
}

// designEvals 调用 Agent 设计测试用例。
//
// 待实现: 接入 create_stage_agent + Runner.run_agent，解析输出 JSON
func (h *TestDesignStageHandler) designEvals(sctx *skilldev.SkillDevContext, _ string) map[string]any {
	// 待实现:
	// 待实现：创建测试设计Agent agent, err := sctx.CreateStageAgent("test_design", strings.Replace(TestDesignSystemPrompt, "{count}", "3", 1), []string{}, 10)
	// 待实现：运行Agent result := await Runner.run_agent(agent, {"skill_content": skillContent})
	// 待实现：解析输出结果 return json.loads(result["output"])

	logger.Warn(logComponent).Msg("[TestDesignStage] designEvals 尚未实现，返回占位测试用例")
	skillName := "skill"
	if sctx.State.Plan != nil {
		if sn, ok := sctx.State.Plan["skill_name"].(string); ok {
			skillName = sn
		}
	}
	return map[string]any{
		"skill_name": skillName,
		"evals": []any{
			map[string]any{
				"id":              1,
				"name":            "basic-usage",
				"prompt":          fmt.Sprintf("请使用 %s 完成基础功能测试", skillName),
				"expected_output": "待实现: 预期结果",
				"files":           []any{},
				"expectations":    []any{"待实现: 可验证的预期声明"},
			},
		},
	}
}

// walkFiles 递归获取目录下所有文件路径（排序）。
func walkFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// joinStrings 用 sep 拼接字符串切片。
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
