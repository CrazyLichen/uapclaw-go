package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 常量 ────────────────────────────

// PlanSystemPrompt PLAN 阶段 Agent 系统 Prompt。
const PlanSystemPrompt = `你是一个 Skill 架构师。根据用户需求，设计一份结构化的 Skill 开发计划。

## 第一步：Capture Intent（需求理解）

在设计之前，先从需求中提取以下关键信息：
1. 这个 Skill 要让模型能做什么？
2. 什么用户场景/措辞应触发这个 Skill？
3. 预期的输出格式是什么？
4. 输出是否可客观验证（适合自动化测试），还是主观的（更适合人工评审）？

## 第二步：Interview & Research（深入调研）

主动识别并记录：
- 边缘案例
- 输入/输出格式约束
- 依赖工具或 MCP
- 成功标准
- 可能的领域知识来源

## 第三步：输出 JSON Plan

` + "```json" + `
{
  "skill_name": "kebab-case 标识名",
  "display_name": "用户可见名称",
  "description": "触发描述——用祈使句，覆盖触发场景，稍微'激进'以避免欠触发",
  "purpose": "这个 skill 解决什么问题",
  "intent_capture": {
    "what": "Skill 赋予模型的能力",
    "when": "触发场景",
    "output_format": "预期输出格式",
    "testable": true
  },
  "directory_structure": {
    "SKILL.md": "主指令文件",
    "scripts/xxx.py": "文件职责说明"
  },
  "key_decisions": [
    "决策1：为什么选择 X 而不是 Y"
  ],
  "test_strategy": {
    "approach": "测试方法描述",
    "test_cases_outline": ["场景1", "场景2", "场景3"]
  },
  "estimated_complexity": "low | medium | high"
}
` + "```" + `

## 设计原则

### 目录结构决策
- 有重复性确定步骤 → 放 scripts/（每次调用省去重新发明轮子）
- 有领域知识文档 → 放 references/（按需加载，不膨胀主文件）
- 有模板/图标/字体 → 放 assets/（输出时直接引用）
- SKILL.md 目标 <500 行；超过则拆分到 references/ 并标明查阅时机

### 描述的触发性
当前模型倾向于不够主动触发 Skill。description 应略微"推进式"：
- 除了说明功能，还要列举具体使用场景
- 即使用户没有明确提到 skill 名称也应触发
- 对标相似能力的区分点

### 修改模式
如果是修改已有 skill，先分析现有结构的优劣，plan 侧重差量而非全量重写。
`

// ──────────────────────────── 结构体 ────────────────────────────

// PlanStageHandler PLAN 阶段：Agent 生成开发计划，随后进入 PLAN_CONFIRM 挂起点。
type PlanStageHandler struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 PLAN 阶段逻辑。
func (h *PlanStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*StageResult, error) {
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在分析需求并生成开发计划..."})

	plan, err := h.generatePlan(sctx)
	if err != nil {
		return nil, fmt.Errorf("生成开发计划失败: %w", err)
	}
	sctx.State.Plan = plan

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "开发计划已生成，等待确认"})
	return &StageResult{NextStage: skilldev.SkillDevStagePlanConfirm}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generatePlan 调用 ReActAgent 生成 plan JSON。
//
// 待实现: 接入 create_stage_agent + Runner.run_agent，流式推送 AGENT_THINKING 事件
func (h *PlanStageHandler) generatePlan(sctx *skilldev.SkillDevContext) (map[string]any, error) {
	// 待实现:
	// agent, err := sctx.CreateStageAgent("plan", PlanSystemPrompt, []string{"web_search"}, 15)
	// messages := h.buildMessages(sctx)
	// planText := ""
	// async for chunk in agent.stream(messages):
	//     sctx.Emit(skilldev.SkillDevEventTypeAgentThinking, map[string]any{"delta": chunk.Content})
	//     planText += chunk.Content
	// plan = parsePlanJSON(planText)
	// if sctx.State.ExistingSkillMD != nil {
	//     plan["diff_analysis"] = "待实现: 差量分析"
	// }
	// return plan

	logger.Warn(logComponent).Msg("[PlanStage] generatePlan 尚未实现，返回占位 plan")
	query := ""
	if q, ok := sctx.State.Input["query"].(string); ok {
		query = q
	}
	return map[string]any{
		"skill_name":   "placeholder-skill",
		"display_name": "占位 Skill",
		"description":  fmt.Sprintf("根据需求『%s』生成的 skill（待实现）", query),
		"purpose":      "待实现",
		"directory_structure": map[string]any{
			"SKILL.md": "主指令文件",
		},
		"key_decisions": []any{},
		"test_strategy": map[string]any{
			"approach":           "待实现",
			"test_cases_outline": []any{},
		},
		"estimated_complexity": "medium",
	}, nil
}

// buildMessages 构造发送给 PLAN Agent 的消息列表。
func (h *PlanStageHandler) buildMessages(sctx *skilldev.SkillDevContext) []map[string]string {
	query := ""
	if q, ok := sctx.State.Input["query"].(string); ok {
		query = q
	}
	parts := []string{fmt.Sprintf("需求：%s", query)}

	if len(sctx.State.ReferenceTexts) > 0 {
		limit := 3
		if len(sctx.State.ReferenceTexts) < limit {
			limit = len(sctx.State.ReferenceTexts)
		}
		refs := strings.Join(sctx.State.ReferenceTexts[:limit], "\n\n")
		parts = append(parts, fmt.Sprintf("参考资料：\n%s", refs))
	}

	if sctx.State.ExistingSkillMD != nil {
		parts = append(parts, fmt.Sprintf("已有 SKILL.md：\n%s", *sctx.State.ExistingSkillMD))
	}

	return []map[string]string{
		{"role": "user", "content": strings.Join(parts, "\n\n")},
	}
}

// parsePlanJSON 从 Agent 输出中提取 JSON plan。
//
// 待实现: 加入容错解析（Agent 可能在 JSON 前后输出额外文本）
func parsePlanJSON(text string) (map[string]any, error) {
	// 简单实现：找到第一个 { 到最后一个 }
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("Agent 未输出有效的 JSON plan")
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(text[start:end+1]), &plan); err != nil {
		return nil, fmt.Errorf("解析 JSON plan 失败: %w", err)
	}
	return plan, nil
}
