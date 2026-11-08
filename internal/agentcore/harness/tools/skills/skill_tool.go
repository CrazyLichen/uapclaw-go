package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	ptools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// skillGetter 返回会话可见技能视图；session 为 nil 时由 rail 退化为当前加载集合。
// Python: get_skills=lambda session=None: rail.get_skills_for_session(session)
type skillGetter func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill

// SkillTool 查看特定技能内容的工具。
// 通过 SysOperation.Fs().ReadFile() 读取技能目录下的指定文件（默认 SKILL.md），
// 通过 getSkills 回调查找技能。
// Python: SkillTool (openjiuwen/harness/tools/skills/skill_tool.py)
type SkillTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// operation 系统操作，用于读取技能文件
	operation sys_operation.SysOperation
	// getSkills 返回会话可见技能视图的回调函数
	getSkills skillGetter
	// language 语言标识（"cn"/"en"）
	language string
	// enableReadImageMultimodal 读图多模态是否可用（决定媒体提示档位）
	// Python: enable_read_image_multimodal (skill_tool.py:347)
	enableReadImageMultimodal bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// defaultSkillFileName 默认技能文件名
	// 一比一复刻 Python: SKILL.md by default (skill_tool.py L44)
	defaultSkillFileName = "SKILL.md"

	// skillToolMarkdownImagesHint 图片提示（读图多模态可用）
	// Python: SKILL_TOOL_MARKDOWN_IMAGES_HINT (skill_tool.py:30-34)
	skillToolMarkdownImagesHint = "Embedded figures in this skill are markdown links (paths/URLs) only; pixel data is not " +
		"attached. Call read_file on the image path under skills/<skill-name>/… when you need " +
		"to inspect a reference screenshot."
	// skillToolMarkdownImagesVisionHint 图片提示（读图多模态关闭）
	// Python: SKILL_TOOL_MARKDOWN_IMAGES_VISION_HINT (skill_tool.py:36-41)
	skillToolMarkdownImagesVisionHint = "Embedded figures in this skill are markdown links (paths/URLs) only; pixel data is not " +
		"attached. read_file native image multimodal input is disabled. If a vision tool is " +
		"configured, call visual_question_answering on the image path under " +
		"skills/<skill-name>/… when you need to inspect a reference screenshot."
	// skillToolMarkdownVideosHint 视频提示
	// Python: SKILL_TOOL_MARKDOWN_VIDEOS_HINT (skill_tool.py:43-46)
	skillToolMarkdownVideosHint = "Embedded videos in this skill are link references only. Skill videos are consumed in " +
		"branch mode (multimodal_skill_mode=branch); do not read_file skill videos on the " +
		"main agent loop as it makes the context window too large."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillTool 创建 SkillTool 实例。
// Python: SkillTool.__init__(operation, get_skills, language, agent_id,
// multimodal_skill_mode, enable_read_image_multimodal) (skill_tool.py:337-361)
func NewSkillTool(
	operation sys_operation.SysOperation,
	getSkills skillGetter,
	language string,
	agentID string,
	enableReadImageMultimodal bool,
) *SkillTool {
	card, _ := ptools.BuildToolCard("skill_tool", "SkillTool", language, nil, agentID)

	return &SkillTool{
		card:                      card,
		operation:                 operation,
		getSkills:                 getSkills,
		language:                  language,
		enableReadImageMultimodal: enableReadImageMultimodal,
	}
}

// Invoke 执行 skill_tool 工具，查看特定技能内容。
// 一比一复刻 Python SkillTool.invoke()：
//  1. 按 skill_name 查找技能，未找到返回 success=false 与 "Skill not found: {name}"
//  2. 构建文件路径（skill.Directory + relative_file_path），默认 SKILL.md
//  3. 通过 SysOperation.Fs().ReadFile() 读取文件
//  4. Code != 0 时返回 success=false，error 取 readFileResult.Message
//  5. 成功时返回 success=true，data 含 skill_directory、skill_content
//  6. 异常时返回 success=false，error 取 err.Error()
//
// Python: SkillTool.invoke(inputs, **kwargs)
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

	// Python: skill_tool.py:385-393 路径安全校验
	if !isSafeRelativeFilePath(relativeFilePath) {
		return map[string]any{
			"success": false,
			"error":   "Invalid relative_file_path: absolute paths and '..' traversal components are not allowed",
		}, nil
	}

	callOpts := tool.NewToolCallOptions(opts...)
	var sess sessioninterfaces.SessionFacade
	if callOpts != nil {
		sess = callOpts.Session
	}

	skill := t.getSkillByName(skillName, sess)
	if skill == nil {
		logger.Warn(logComponent).
			Str("skill_name", skillName).
			Msg("SkillTool skill not found")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Skill not found: %s", skillName),
		}, nil
	}

	filePath := filepath.Join(skill.Directory, filepath.FromSlash(strings.ReplaceAll(relativeFilePath, "\\", "/")))
	readFileResult, readErr := t.operation.Fs().ReadFile(ctx, filePath)

	if readErr != nil {
		logger.Error(logComponent).
			Str("skill_name", skillName).
			Str("file_path", filePath).
			Err(readErr).
			Msg("SkillTool error reading file")
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
			Msg("SkillTool failed to read file")
		return map[string]any{
			"success": false,
			"error":   readFileResult.Message,
		}, nil
	}

	body := ""
	if readFileResult.Data != nil {
		body = readFileResult.Data.Content
	}
	// Python: hint 模式对 markdown 媒体引用加提示（skill_tool.py:412-419）
	if skillMarkdownHasMedia(body) {
		body = applySkillToolMarkdownImagesHint(body, t.enableReadImageMultimodal)
	}

	layout := skillLayoutMetadata(skill.Directory)
	data := map[string]any{
		"skill_directory": skill.Directory,
		"skill_content":   body,
	}
	for key, value := range layout {
		data[key] = value
	}
	// content 必须无条件设置：AbilityManager 优先取 data["content"]，
	// 否则退化为 JSON dump，把正文与附录一起埋掉（Python: skill_tool.py:432-436）
	if appendix := formatLayoutAppendixForModel(layout, skill.Name); appendix != "" {
		data["content"] = strings.TrimRight(body, "\n") + "\n\n" + appendix
	} else {
		data["content"] = body
	}

	logger.Info(logComponent).
		Str("skill_name", skillName).
		Str("file_path", filePath).
		Msg("SkillTool read skill file")

	return map[string]any{"success": true, "data": data}, nil
}

// applySkillToolMarkdownImagesHint 剥离旧提示后按媒体类型前置提示（最多一次）。
// Python: apply_skill_tool_markdown_images_hint (skill_tool.py:68-86)
func applySkillToolMarkdownImagesHint(body string, enableReadImageMultimodal bool) string {
	normalized := stripSkillToolInjectedHints(body)
	var hints []string
	if markdownHasImageReference(normalized) {
		if enableReadImageMultimodal {
			hints = append(hints, skillToolMarkdownImagesHint)
		} else {
			hints = append(hints, skillToolMarkdownImagesVisionHint)
		}
	}
	if markdownHasVideoReference(normalized) {
		hints = append(hints, skillToolMarkdownVideosHint)
	}
	if len(hints) == 0 {
		return normalized
	}
	return strings.Join(hints, "\n\n") + "\n\n" + normalized
}

// skillMarkdownHasMedia 判断技能正文是否含图片或视频引用。
// Python: skill_markdown_has_media (skill_tool.py:88-92)
func skillMarkdownHasMedia(content string) bool {
	return markdownHasImageReference(content) || markdownHasVideoReference(content)
}

// stripSkillToolInjectedHints 反复剥离已注入的三类提示前缀。
// Python: _strip_skill_tool_injected_hints (skill_tool.py:47-66)
func stripSkillToolInjectedHints(body string) string {
	s := body
	for {
		stripped := false
		for _, hint := range []string{skillToolMarkdownImagesHint, skillToolMarkdownImagesVisionHint, skillToolMarkdownVideosHint} {
			prefix := hint + "\n\n"
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimPrefix(s, prefix)
				stripped = true
			}
		}
		if !stripped {
			break
		}
	}
	return s
}

// Stream SkillTool 不支持流式调用。
// Python: SkillTool.stream(inputs, **kwargs) — if False: yield None
func (t *SkillTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.ID)
}

// Card 返回工具配置卡片。
func (t *SkillTool) Card() *tool.ToolCard { return t.card }

// GetCallTimeout 返回外层调用预算，沿用卡片级策略。
func (t *SkillTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return tool.BaseCallTimeout(inputs, defaultTimeout)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSkillByName 按名称在会话视图中查找技能。
// Python: SkillTool._get_skill_by_name (skill_tool.py:456-465)
func (t *SkillTool) getSkillByName(name string, sess sessioninterfaces.SessionFacade) *skillpkg.Skill {
	if name == "" || t.getSkills == nil {
		return nil
	}
	for _, s := range t.getSkills(sess) {
		if s != nil && s.Name == name {
			return s
		}
	}
	return nil
}

// trimSpace 去除字符串两端空白。
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
