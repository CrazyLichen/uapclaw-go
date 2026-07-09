package stages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 常量 ────────────────────────────

// GenerateSystemPrompt GENERATE 阶段 Agent 系统 Prompt。
const GenerateSystemPrompt = `你是一个 Skill 开发专家。根据已确认的开发计划，生成完整的 Skill 文件集。

## SKILL.md 格式要求（必须严格遵守）

**YAML Frontmatter（必填）：**
` + "```" + `
---
name: skill-name-here
description: 用祈使句描述何时触发、做什么。描述应聚焦用户意图而非实现细节。≤1024 字符。
---
` + "```" + `

规则：
- name 必须是 kebab-case（小写字母、数字、连字符），≤64 字符
- description 不能包含 < 或 >
- 仅允许的 frontmatter key: name, description, license, allowed-tools, metadata, compatibility

## Skill 目录结构

` + "```" + `
skill-name/
├── SKILL.md (必需)
├── scripts/    - 确定性/重复性任务的可执行脚本
├── references/ - 按需加载的领域文档
└── assets/     - 输出中使用的模板、图标、字体等
` + "```" + `

## 写作原则（对齐官方 Skill Writing Guide）

### 渐进式信息展示 (Progressive Disclosure)
1. **元数据**（name + description）— 始终在上下文中（~100 词）
2. **SKILL.md 正文** — 触发时加载（<500 行为佳）
3. **捆绑资源** — 按需加载（无大小限制，脚本可不加载直接执行）

### 写作风格
- 使用祈使句式（"执行 X" 而非 "这个 skill 会执行 X"）
- 解释 **为什么** 而非堆砌规则；避免过度使用 MUST/NEVER/ALWAYS
- 使用心理模型让模型理解意图，比死板指令更有效
- 保持 SKILL.md ≤500 行；超过时拆分到 references/ 并标明何时查阅

### 输出格式定义
明确定义预期输出结构，使用模板或示例：
` + "```markdown" + `
## 报告结构
ALWAYS use this exact template:
# [Title]
## Executive summary
## Key findings
## Recommendations
` + "```" + `

### 发现重复工作 → 捆绑脚本
如果测试中发现模型反复独立编写类似的辅助脚本，应将其捆绑到 scripts/ 中。

### description 的触发性
当前模型倾向于"不够主动触发"skill。description 应略微"推进式"——
除了说明 skill 做什么，还要列举具体触发场景，即使用户没有明确提到 skill 名称。
`

// ──────────────────────────── 结构体 ────────────────────────────

// GenerateStageHandler GENERATE 阶段：Agent 按 plan 生成完整 skill 文件集。
type GenerateStageHandler struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 GENERATE 阶段逻辑。
func (h *GenerateStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*StageResult, error) {
	plan := sctx.State.Plan
	if plan == nil {
		return nil, fmt.Errorf("GENERATE 阶段缺少 plan，请先完成 PLAN 阶段")
	}

	skillDir := filepath.Join(sctx.Workspace, "skill")
	generationOrder := h.resolveGenerationOrder(plan)

	sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
		"message":     fmt.Sprintf("正在生成 %d 个文件...", len(generationOrder)),
		"files_total": len(generationOrder),
		"files_done":  0,
	})

	generatedFiles := h.generateAllFiles(sctx, skillDir, generationOrder)

	// 获取 skill_name
	skillName := "skill"
	if sn, ok := plan["skill_name"].(string); ok {
		skillName = sn
	}

	sctx.Emit(skilldev.SkillDevEventTypeArtifactReady, map[string]any{
		"artifact": map[string]any{
			"id":          "skill_files",
			"name":        skillName,
			"type":        "skill_md",
			"files":       generatedFiles,
			"browsable":   true,
			"downloadable": false,
		},
	})
	return &StageResult{NextStage: skilldev.SkillDevStageValidate}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generationItem 文件生成项（文件路径 + 角色描述）。
type generationItem struct {
	// FilePath 文件路径
	FilePath string
	// Role 角色描述
	Role string
}

// resolveGenerationOrder 确定文件生成顺序：SKILL.md 优先，scripts/ 其次，其余最后。
func (h *GenerateStageHandler) resolveGenerationOrder(plan map[string]any) []generationItem {
	directoryStructure, _ := plan["directory_structure"].(map[string]any)
	if directoryStructure == nil {
		return nil
	}

	var order []generationItem

	// SKILL.md 必须最先生成（其他文件生成时需参考它）
	if role, ok := directoryStructure["SKILL.md"].(string); ok {
		order = append(order, generationItem{FilePath: "SKILL.md", Role: role})
	}

	// scripts/ 次之
	for path, role := range directoryStructure {
		if path == "SKILL.md" {
			continue
		}
		if strings.HasPrefix(path, "scripts/") {
			if r, ok := role.(string); ok {
				order = append(order, generationItem{FilePath: path, Role: r})
			}
		}
	}

	// 其余文件
	for path, role := range directoryStructure {
		if path == "SKILL.md" {
			continue
		}
		if strings.HasPrefix(path, "scripts/") {
			continue
		}
		if r, ok := role.(string); ok {
			order = append(order, generationItem{FilePath: path, Role: r})
		}
	}

	return order
}

// generateAllFiles 逐文件调用 Agent 生成内容。
//
// 待实现: 接入 create_stage_agent + 逐文件生成逻辑
func (h *GenerateStageHandler) generateAllFiles(_ *skilldev.SkillDevContext, skillDir string, generationOrder []generationItem) []string {
	// 待实现:
	// agent, err := sctx.CreateStageAgent("generate", GenerateSystemPrompt, []string{"file_read", "file_write"}, 30)
	// for idx, item := range generationOrder {
	//     os.MkdirAll(filepath.Dir(filepath.Join(skillDir, item.FilePath)), 0o755)
	//     content := await generateSingleFile(agent, sctx, item.FilePath, item.Role)
	//     os.WriteFile(filepath.Join(skillDir, item.FilePath), []byte(content), 0o644)
	//     sctx.Emit(skilldev.SkillDevEventTypeProgress, map[string]any{
	//         "message":     fmt.Sprintf("已生成: %s", item.FilePath),
	//         "files_done":  idx + 1,
	//         "files_total": len(generationOrder),
	//     })
	// }

	logger.Warn(logComponent).Msg("[GenerateStage] generateAllFiles 尚未实现，创建占位文件")
	generated := make([]string, 0, len(generationOrder))
	for _, item := range generationOrder {
		fullPath := filepath.Join(skillDir, item.FilePath)
		_ = os.MkdirAll(filepath.Dir(fullPath), 0o755)
		content := fmt.Sprintf("# %s\n\n<!-- 待实现: 由 Agent 生成，职责：%s -->\n", item.FilePath, item.Role)
		_ = os.WriteFile(fullPath, []byte(content), 0o644)
		generated = append(generated, item.FilePath)
	}
	return generated
}

// validateScripts 验证生成的 Python 脚本语法正确性。
//
// 待实现: 使用 py_compile 或 ast.parse 检查语法
func validateScripts(_ string) {
	// 待实现
}
