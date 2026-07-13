package runtime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// skillsUseRegex /skills use 斜杠命令匹配正则。
var skillsUseRegex = regexp.MustCompile(`^/skills use\s+(?P<skill_names>[^,]+)\s*,\s*(?P<query>.*)$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildUserPrompt 将用户 query 包装为结构化 JSON prompt。
//
// 返回格式: interactionPrefix + promptPrefix + json.dumps(userMessageContext)
//
// 对齐 Python: build_user_prompt(content, files, channel, language, *, trusted_dirs, metadata)
func BuildUserPrompt(content string, files map[string]any, channel string, language string,
	trustedDirs []string, metadata map[string]any) string {
	// 1. interaction_context 前缀
	interactionPrefix := ""
	if metadata != nil {
		if ctx, ok := metadata["interaction_context"]; ok {
			if ctxStr, ok := ctx.(string); ok && strings.TrimSpace(ctxStr) != "" {
				interactionPrefix = fmt.Sprintf("\n%s\n\n", ctxStr)
			}
		}
	}

	// 2. /skills use 斜杠命令解析
	skillsToUse, newContent := handleSkillsUseSlashCommand(content)
	if newContent != "" {
		content = newContent
	}

	// 3. 按语言+channel 构建 prompt 前缀
	var prompt string
	if language == "zh" {
		prompt = "你收到一条消息：\n"
		if channel == "cron" {
			prompt = "你收到一条消息，对于查询类任务必须输出查询到的内容，不要只回复确认或只记录到memory：\n"
		}
	} else {
		prompt = "You receive a new message:\n"
		if channel == "cron" {
			prompt = "You receive a new message. For query tasks, you must output the queried content—don't just reply with confirmation or only record to memory:\n"
		}
	}

	// 4. 构建 userMessageContext
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	nowStr := now.Format("2006-01-02 15:04:05")

	userMessageContext := map[string]any{
		"source":                      channel,
		"timezone":                    "Asia/Shanghai",
		"timestamp":                   nowStr,
		"preferred_response_language": language,
		"content":                     content,
		"type":                        "user input",
	}

	// cron/heartbeat 特殊处理
	if channel == "cron" || channel == "heartbeat" {
		userMessageContext["source"] = "system"
		userMessageContext["type"] = channel
	}

	// files_updated_by_user
	if channel != "cron" && channel != "heartbeat" {
		if filesJSON, err := json.Marshal(files); err == nil {
			userMessageContext["files_updated_by_user"] = string(filesJSON)
		}
	}

	// skills_to_use
	if len(skillsToUse) > 0 {
		userMessageContext["skills_to_use"] = skillsToUse
	}

	// trusted_dirs
	if len(trustedDirs) > 0 {
		if dirsJSON, err := json.Marshal(trustedDirs); err == nil {
			userMessageContext["trusted_dirs"] = string(dirsJSON)
		}
	}

	// 5. 序列化并返回
	contextJSON, err := json.Marshal(userMessageContext)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("BuildUserPrompt 序列化失败")
		return content
	}

	finalPrompt := interactionPrefix + prompt + string(contextJSON)

	// 对齐 Python：interaction_prefix 存在时记录 debug 日志
	if interactionPrefix != "" {
		logger.Info(logComponent).
			Str("event_type", "build_user_prompt_debug").
			Str("final_prompt", finalPrompt).
			Msg("[build_user_prompt][DEBUG] interaction_context 存在，最终 prompt")
	}

	return finalPrompt
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleSkillsUseSlashCommand 解析 /skills use 斜杠命令。
//
// 对齐 Python: _handle_skills_use_slash_command(query)
func handleSkillsUseSlashCommand(query string) (skillsToUse []string, newQuery string) {
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/skills use") {
		return nil, ""
	}

	matches := skillsUseRegex.FindStringSubmatch(stripped)
	if len(matches) > 0 {
		skillNamesIdx := skillsUseRegex.SubexpIndex("skill_names")
		queryIdx := skillsUseRegex.SubexpIndex("query")
		if skillNamesIdx >= 0 && queryIdx >= 0 {
			return []string{matches[skillNamesIdx]}, matches[queryIdx]
		}
	}

	logger.Warn(logComponent).Str("query", stripped).Msg("无法解析 /skills use 命令")
	return nil, ""
}
