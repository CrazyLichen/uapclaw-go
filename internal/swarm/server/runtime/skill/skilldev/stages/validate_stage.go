package stages

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ValidateStageHandler VALIDATE 阶段：校验 SKILL.md 格式合规性。
type ValidateStageHandler struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行 VALIDATE 阶段逻辑。
func (h *ValidateStageHandler) Execute(_ context.Context, sctx *skilldev.SkillDevContext) (*skilldev.StageResult, error) {
	skillMDPath := fmt.Sprintf("%s/skill/SKILL.md", sctx.Workspace)

	if _, err := os.Stat(skillMDPath); os.IsNotExist(err) {
		sctx.Emit(skilldev.SkillDevEventTypeValidateResult, map[string]any{
			"valid":   false,
			"message": "SKILL.md 未生成",
		})
		return &skilldev.StageResult{NextStage: skilldev.SkillDevStageGenerate}, nil
	}

	valid, message := ValidateSkillMD(skillMDPath)
	sctx.Emit(skilldev.SkillDevEventTypeValidateResult, map[string]any{
		"valid":   valid,
		"message": message,
	})

	if !valid {
		logger.Warn(logComponent).
			Str("message", message).
			Msg("[ValidateStage] 校验失败，回退到 GENERATE")
		return &skilldev.StageResult{NextStage: skilldev.SkillDevStageGenerate}, nil
	}

	return &skilldev.StageResult{NextStage: skilldev.SkillDevStageTestDesign}, nil
}

// ValidateSkillMD 校验 SKILL.md 的 YAML frontmatter 格式。
//
// 返回 (isValid, message)。
func ValidateSkillMD(skillMDPath string) (bool, string) {
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return false, fmt.Sprintf("读取 SKILL.md 失败: %v", err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "---") {
		return false, "SKILL.md 缺少 YAML frontmatter（应以 --- 开头）"
	}

	fmRe := regexp.MustCompile(`(?s)^---\n(.*?)\n---`)
	match := fmRe.FindStringSubmatch(content)
	if match == nil {
		return false, "YAML frontmatter 格式无效"
	}

	frontmatter := parseFrontmatter(match[1])

	if _, ok := frontmatter["name"]; !ok {
		return false, "frontmatter 缺少必填字段 'name'"
	}
	if _, ok := frontmatter["description"]; !ok {
		return false, "frontmatter 缺少必填字段 'description'"
	}

	// 检查未允许的字段
	unexpected := make([]string, 0)
	for key := range frontmatter {
		if !skilldev.AllowedFrontmatterKeys[key] {
			unexpected = append(unexpected, key)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return false, fmt.Sprintf("frontmatter 包含未允许的字段: %s", strings.Join(unexpected, ", "))
	}

	// 命名格式：kebab-case
	name := strings.TrimSpace(frontmatter["name"])
	if name != "" {
		kebabRe := regexp.MustCompile(`^[a-z0-9-]+$`)
		if !kebabRe.MatchString(name) {
			return false, fmt.Sprintf("name '%s' 必须是 kebab-case（小写字母、数字、连字符）", name)
		}
		if hasInvalidHyphenUsage(name) {
			return false, fmt.Sprintf("name '%s' 不能以连字符开头/结尾或包含连续连字符", name)
		}
		if len(name) > skilldev.SkillNameMaxLen {
			return false, fmt.Sprintf("name 过长（%d 字符，最大 %d）", len(name), skilldev.SkillNameMaxLen)
		}
	}

	// 技能描述
	desc := strings.TrimSpace(frontmatter["description"])
	if strings.Contains(desc, "<") || strings.Contains(desc, ">") {
		return false, "description 不能包含尖括号 (< 或 >)"
	}
	if len(desc) > skilldev.SkillDescMaxLen {
		return false, fmt.Sprintf("description 过长（%d 字符，最大 %d）", len(desc), skilldev.SkillDescMaxLen)
	}

	return true, "SKILL.md 校验通过"
}

// ParseSkillFrontmatter 从 SKILL.md 解析出 (name, description, bodyContent)。
//
// 轻量解析器，无 YAML 库依赖。
func ParseSkillFrontmatter(skillMDPath string) (string, string, string) {
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return "", "", ""
	}
	content := string(data)

	fmRe := regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)`)
	match := fmRe.FindStringSubmatch(content)
	if match == nil {
		return "", "", content
	}

	fm := parseFrontmatter(match[1])
	name := fm["name"]
	desc := fm["description"]
	return name, desc, match[2]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseFrontmatter 极简 YAML frontmatter 解析（key: value 单行 + block scalar）。
//
// 生产环境可替换为 yaml 库。
func parseFrontmatter(text string) map[string]string {
	result := make(map[string]string)
	currentKey := ""
	currentLines := make([]string, 0)

	keyRe := regexp.MustCompile(`^([a-zA-Z_-]+):\s*(.*)`)

	for _, line := range strings.Split(text, "\n") {
		keyMatch := keyRe.FindStringSubmatch(line)
		if keyMatch != nil {
			if currentKey != "" {
				result[currentKey] = strings.TrimSpace(strings.Join(currentLines, "\n"))
			}
			currentKey = keyMatch[1]
			value := keyMatch[2]
			if value == "|" || value == ">" {
				currentLines = []string{}
			} else {
				currentLines = []string{value}
			}
		} else if currentKey != "" {
			currentLines = append(currentLines, line)
		}
	}

	if currentKey != "" {
		result[currentKey] = strings.TrimSpace(strings.Join(currentLines, "\n"))
	}

	return result
}

// hasInvalidHyphenUsage 校验 name 中连字符使用是否非法。
func hasInvalidHyphenUsage(name string) bool {
	return strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--")
}
