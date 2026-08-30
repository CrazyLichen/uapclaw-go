package memory

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryForbiddenConfig 记忆禁止配置。
// 对齐 Python: _get_memory_forbidden_config() 返回的 dict
type MemoryForbiddenConfig struct {
	// Enabled 是否启用禁止记忆规则
	Enabled bool `json:"enabled"`
	// Patterns 禁止记忆的敏感信息类型列表
	Patterns []string `json:"patterns"`
	// Description 多语言描述："zh"/"en" → 描述文本
	Description map[string]string `json:"description"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var forbiddenLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// GetForbiddenMemoryPrompt 格式化禁止记忆提示词。enabled=false 时返回空串。
// 对齐 Python: get_forbidden_memory_prompt(language)
func GetForbiddenMemoryPrompt(language string) string {
	cfg := getMemoryForbiddenConfig()

	if !cfg.Enabled {
		return ""
	}

	descText := ""
	if cfg.Description != nil {
		// 优先使用请求语言，回退到 "zh"
		if v, ok := cfg.Description[language]; ok && v != "" {
			descText = v
		} else if v, ok := cfg.Description["zh"]; ok {
			descText = v
		}
	}

	if language == "zh" || language == "cn" {
		return buildForbiddenPromptCN(descText, cfg.Patterns)
	}
	return buildForbiddenPromptEN(descText, cfg.Patterns)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getMemoryForbiddenConfig 从 config 读取 memory.forbidden_memory_definition。
// 对齐 Python: _get_memory_forbidden_config()
func getMemoryForbiddenConfig() *MemoryForbiddenConfig {
	cfg, err := config.New("")
	if err != nil {
		logger.Warn(forbiddenLogComponent).Err(err).Msg("加载配置失败")
		return &MemoryForbiddenConfig{Enabled: false}
	}
	configBase, err := cfg.Load()
	if err != nil {
		logger.Warn(forbiddenLogComponent).Err(err).Msg("读取配置失败")
		return &MemoryForbiddenConfig{Enabled: false}
	}

	memoryRaw, ok := configBase["memory"]
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}
	memoryMap, ok := memoryRaw.(map[string]any)
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}
	forbiddenRaw, ok := memoryMap["forbidden_memory_definition"]
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}
	forbiddenMap, ok := forbiddenRaw.(map[string]any)
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}

	result := &MemoryForbiddenConfig{}

	if v, ok := forbiddenMap["enabled"].(bool); ok {
		result.Enabled = v
	}
	if patternsRaw, ok := forbiddenMap["patterns"]; ok {
		if arr, ok := patternsRaw.([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result.Patterns = append(result.Patterns, s)
				}
			}
		}
	}
	if descRaw, ok := forbiddenMap["description"]; ok {
		if m, ok := descRaw.(map[string]any); ok {
			result.Description = make(map[string]string, len(m))
			for k, v := range m {
				if s, ok := v.(string); ok {
					result.Description[k] = s
				}
			}
		}
	}

	return result
}

// buildForbiddenPromptCN 构建中文禁止记忆提示词。
// 对齐 Python: get_forbidden_memory_prompt("zh") 的格式化输出
func buildForbiddenPromptCN(descText string, patterns []string) string {
	parts := []string{"### 记忆限制规则", ""}
	if descText != "" {
		parts = append(parts, descText, "")
	}
	if len(patterns) > 0 {
		parts = append(parts, "**禁止记忆的敏感信息类型包括：**", "")
		for i, p := range patterns {
			parts = append(parts, fmt.Sprintf("%d. `%s`", i+1, p))
		}
		parts = append(parts, "")
	}
	parts = append(parts, "**执行要求：**")
	parts = append(parts, "- 在调用 `experience_learn` 或 `write_memory` 存储记忆前，必须检查内容是否包含上述敏感信息")
	parts = append(parts, "- 如果检测到敏感信息，必须对其进行脱敏处理（如替换为 ***）或拒绝存储")
	parts = append(parts, "- 用户明确要求的密码、密钥等敏感信息不得存入记忆系统")
	parts = append(parts, "")
	return strings.Join(parts, "\n")
}

// buildForbiddenPromptEN 构建英文禁止记忆提示词。
// 对齐 Python: get_forbidden_memory_prompt("en") 的格式化输出
func buildForbiddenPromptEN(descText string, patterns []string) string {
	parts := []string{"### Memory Restriction Rules", ""}
	if descText != "" {
		parts = append(parts, descText, "")
	}
	if len(patterns) > 0 {
		parts = append(parts, "**Types of sensitive information forbidden to remember:**", "")
		for i, p := range patterns {
			parts = append(parts, fmt.Sprintf("%d. `%s`", i+1, p))
		}
		parts = append(parts, "")
	}
	parts = append(parts, "**Requirements:**")
	parts = append(parts, "- Before calling `experience_learn` or `write_memory` to store memories, you must check if the content contains the above sensitive information")
	parts = append(parts, "- If sensitive information is detected, it must be desensitized (e.g., replaced with ***) or storage must be refused")
	parts = append(parts, "- Sensitive information such as passwords and keys explicitly provided by the user must not be stored in the memory system")
	parts = append(parts, "")
	return strings.Join(parts, "\n")
}
