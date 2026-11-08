package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanStageHandler PLAN 阶段：Agent 生成开发计划，随后进入 PLAN_CONFIRM 挂起点。
type PlanStageHandler struct{}

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

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 PLAN 阶段逻辑。
func (h *PlanStageHandler) Execute(ctx context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "正在分析需求并生成开发计划..."})

	plan, err := h.generatePlan(ctx, sctx)
	if err != nil {
		return nil, fmt.Errorf("生成开发计划失败: %w", err)
	}
	sctx.State.Plan = plan

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{"message": "开发计划已生成，等待确认"})
	return &skilldev.StageResult{NextStage: skilldev.SkillDevStagePlanConfirm}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generatePlan 调用阶段 Agent 生成 plan JSON。
// 对齐 Python: PlanStageHandler._generate_plan(ctx)
// 上游 _generate_plan 仍是占位（返回 placeholder plan），这里按注释伪代码接入 Agent：
// 构造 PLAN 消息 → 调模型 → 容错解析 JSON → 有既有 SKILL.md 时补差量分析。
func (h *PlanStageHandler) generatePlan(ctx context.Context, sctx *skilldev.SkillDevContext) (map[string]any, error) {
	messages := h.buildMessages(sctx)
	userPrompt := ""
	for _, msg := range messages {
		if msg["role"] == "user" {
			userPrompt = msg["content"]
		}
	}

	planText, err := sctx.RunStageAgent(ctx, "plan", PlanSystemPrompt, []string{"web_search"}, 15, userPrompt)
	if err != nil {
		return nil, err
	}
	sctx.Emit(skilldev.SkillDevEventTypeAgentThinking, map[string]any{"delta": planText})

	plan, err := parsePlanJSON(planText)
	if err != nil {
		logger.Error(logComponent).
			Str("task_id", sctx.TaskID).
			Err(err).
			Msg("[PlanStage] parse plan json failed")
		return nil, err
	}

	if sctx.State.ExistingSkillMD != nil {
		plan["diff_analysis"] = buildDiffAnalysis(plan, *sctx.State.ExistingSkillMD)
	}
	if plan["directory_structure"] == nil {
		plan["directory_structure"] = map[string]any{"SKILL.md": "主指令文件"}
	}
	return plan, nil
}

// buildDiffAnalysis 生成「已有技能 → 新计划」的差量说明。
// Python: plan["diff_analysis"]（上游标为待实现，此处给出可读的字段级对比）
func buildDiffAnalysis(plan map[string]any, existingSkillMD string) string {
	newName, _ := plan["skill_name"].(string)
	newDesc, _ := plan["description"].(string)

	oldName, oldDesc := parseSkillFrontmatterFields(existingSkillMD)
	parts := make([]string, 0, 2)
	parts = append(parts, fmt.Sprintf("原技能名：%s；新技能名：%s", oldName, newName))
	parts = append(parts, fmt.Sprintf("原描述：%s；新描述：%s", oldDesc, newDesc))
	return strings.Join(parts, "\n")
}

// parseSkillFrontmatterFields 从 SKILL.md 文本中提取 name/description。
func parseSkillFrontmatterFields(content string) (string, string) {
	fmRe := regexp.MustCompile(`(?s)^---\n(.*?)\n---`)
	match := fmRe.FindStringSubmatch(content)
	if match == nil {
		return "", ""
	}
	name := ""
	description := ""
	for _, line := range strings.Split(match[1], "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		switch key {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return name, description
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
// 待实现: 加入容错解析（Agent 可能在 JSON 前后输出额外文本）
func parsePlanJSON(text string) (map[string]any, error) {
	// 简单实现：找到第一个 { 到最后一个 }
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("agent 未输出有效的 JSON plan")
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(text[start:end+1]), &plan); err != nil {
		return nil, fmt.Errorf("解析 JSON plan 失败: %w", err)
	}
	return plan, nil
}
