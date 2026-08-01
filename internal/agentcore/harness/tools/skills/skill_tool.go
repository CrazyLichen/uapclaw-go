package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	ptools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillTool 查看特定技能内容的工具。
//
// 通过 SysOperation.Fs().ReadFile() 读取技能目录下的指定文件（默认 SKILL.md），
// 通过 getSkills 回调查找技能。
//
// 对应 Python: SkillTool (openjiuwen/harness/tools/skills/skill_tool.py)
type SkillTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// operation 系统操作，用于读取技能文件
	operation sys_operation.SysOperation
	// getSkills 返回当前已启用技能列表的回调函数
	getSkills func() []*skillpkg.Skill
	// language 语言标识（"cn"/"en"）
	language string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// defaultSkillFileName 默认技能文件名
	// 一比一复刻 Python: SKILL.md by default (skill_tool.py L44)
	defaultSkillFileName = "SKILL.md"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillTool 创建 SkillTool 实例。
//
// 对应 Python: SkillTool.__init__(operation, get_skills, language, agent_id)
func NewSkillTool(
	operation sys_operation.SysOperation,
	getSkills func() []*skillpkg.Skill,
	language string,
	agentID string,
) *SkillTool {
	card, _ := ptools.BuildToolCard("skill_tool", "SkillTool", language, nil, agentID)

	return &SkillTool{
		card:      card,
		operation: operation,
		getSkills: getSkills,
		language:  language,
	}
}

// Invoke 执行 skill_tool 工具，查看特定技能内容。
//
// 一比一复刻 Python SkillTool.invoke()：
//  1. 按 skill_name 查找技能 → 未找到返回 success=false, "Skill not found: {name}"
//  2. 构建文件路径（skill.Directory + relative_file_path），默认 SKILL.md
//  3. 通过 SysOperation.Fs().ReadFile() 读取文件
//  4. Code != 0 → success=false, error=readFileResult.Message
//  5. 成功 → success=true, data={skill_directory, skill_content}
//  6. 异常 → success=false, error=err.Error()
//
// 对应 Python: SkillTool.invoke(inputs, **kwargs)
func (t *SkillTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	skillName := ""
	if v, ok := inputs["skill_name"]; ok {
		skillName = fmt.Sprintf("%v", v)
	}
	skillName = trimSpace(skillName)

	relativeFilePath := defaultSkillFileName
	if v, ok := inputs["relative_file_path"]; ok {
		raw := fmt.Sprintf("%v", v)
		raw = trimSpace(raw)
		if raw != "" {
			relativeFilePath = raw
		}
	}

	skill := t.getSkillByName(skillName)
	if skill == nil {
		logger.Warn(logComponent).
			Str("skill_name", skillName).
			Msg("SkillTool 技能未找到")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Skill not found: %s", skillName),
		}, nil
	}

	filePath := filepath.Join(skill.Directory, relativeFilePath)
	readFileResult, readErr := t.operation.Fs().ReadFile(ctx, filePath)

	if readErr != nil {
		logger.Error(logComponent).
			Str("skill_name", skillName).
			Str("file_path", filePath).
			Err(readErr).
			Msg("SkillTool 读取文件异常")
		return map[string]any{
			"success": false,
			"error":   readErr.Error(),
		}, nil
	}

	if readFileResult.Code != 0 {
		logger.Error(logComponent).
			Str("skill_name", skillName).
			Str("file_path", filePath).
			Str("message", readFileResult.Message).
			Msg("SkillTool 读取文件失败")
		return map[string]any{
			"success": false,
			"error":   readFileResult.Message,
		}, nil
	}

	skillFileContent := ""
	if readFileResult.Data != nil {
		skillFileContent = readFileResult.Data.Content
	}

	logger.Info(logComponent).
		Str("skill_name", skillName).
		Str("file_path", filePath).
		Msg("SkillTool 读取技能文件成功")

	return map[string]any{
		"success": true,
		"data": map[string]any{
			"skill_directory": skill.Directory,
			"skill_content":   skillFileContent,
		},
	}, nil
}

// Stream SkillTool 不支持流式调用。
//
// 对应 Python: SkillTool.stream(inputs, **kwargs) — if False: yield None
func (t *SkillTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.ID)
}

// Card 返回工具配置卡片。
func (t *SkillTool) Card() *tool.ToolCard { return t.card }

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSkillByName 按名称查找技能。
//
// 对应 Python: SkillTool._get_skill_by_name(skill_name)
// 一比一复刻: name 为空返回 None；遍历 get_skills() 构建 name→skill 映射
func (t *SkillTool) getSkillByName(name string) *skillpkg.Skill {
	if name == "" {
		return nil
	}
	allSkills := t.getSkills()
	if allSkills == nil {
		return nil
	}
	for _, s := range allSkills {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// trimSpace 去除字符串两端空白。
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
