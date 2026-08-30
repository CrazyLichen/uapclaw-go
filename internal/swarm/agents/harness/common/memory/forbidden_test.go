package memory

import (
	"strings"
	"testing"
)

// ──────────────────────────── GetForbiddenMemoryPrompt 测试 ────────────────────────────

// TestGetForbiddenMemoryPrompt_未配置返回空 验证未配置时 enabled=false 返回空串
func TestGetForbiddenMemoryPrompt_未配置返回空(t *testing.T) {
	prompt := GetForbiddenMemoryPrompt("cn")
	if prompt != "" {
		t.Errorf("未配置时 GetForbiddenMemoryPrompt 应返回空串，实际: %q", prompt)
	}
}

// TestGetMemoryForbiddenConfig_配置缺失 验证无配置文件时返回默认值
func TestGetMemoryForbiddenConfig_配置缺失(t *testing.T) {
	cfg := getMemoryForbiddenConfig()
	if cfg.Enabled {
		t.Error("无配置时 Enabled 应为 false")
	}
	if len(cfg.Patterns) != 0 {
		t.Error("无配置时 Patterns 应为空")
	}
}

// ──────────────────────────── 提示词格式化测试 ────────────────────────────

// TestBuildForbiddenPromptCN_有Patterns 验证中文提示词包含关键内容
func TestBuildForbiddenPromptCN_有Patterns(t *testing.T) {
	patterns := []string{"密码", "API密钥", "Secret", "Token"}
	prompt := buildForbiddenPromptCN("以下内容禁止记忆：密码、API密钥等敏感信息", patterns)

	if !strings.Contains(prompt, "记忆限制规则") {
		t.Error("中文提示词应包含 '记忆限制规则'")
	}
	if !strings.Contains(prompt, "密码") {
		t.Error("中文提示词应包含 pattern '密码'")
	}
	if !strings.Contains(prompt, "experience_learn") {
		t.Error("中文提示词应包含执行要求中的 experience_learn")
	}
	if !strings.Contains(prompt, "write_memory") {
		t.Error("中文提示词应包含执行要求中的 write_memory")
	}
	if !strings.Contains(prompt, "禁止记忆的敏感信息类型包括") {
		t.Error("有 patterns 时应包含列表标题")
	}
	if !strings.Contains(prompt, "1. `密码`") {
		t.Error("中文提示词应包含编号的 pattern")
	}
}

// TestBuildForbiddenPromptCN_无Patterns 验证无 patterns 时不输出列表
func TestBuildForbiddenPromptCN_无Patterns(t *testing.T) {
	prompt := buildForbiddenPromptCN("以下内容禁止记忆：敏感信息", nil)

	if !strings.Contains(prompt, "记忆限制规则") {
		t.Error("无 patterns 时仍应包含标题")
	}
	if strings.Contains(prompt, "禁止记忆的敏感信息类型包括") {
		t.Error("无 patterns 时不应输出 pattern 列表")
	}
	if !strings.Contains(prompt, "执行要求") {
		t.Error("无 patterns 时仍应包含执行要求")
	}
}

// TestBuildForbiddenPromptEN_有Patterns 验证英文提示词包含关键内容
func TestBuildForbiddenPromptEN_有Patterns(t *testing.T) {
	patterns := []string{"passwords", "API keys"}
	prompt := buildForbiddenPromptEN("The following content is forbidden to remember", patterns)

	if !strings.Contains(prompt, "Memory Restriction Rules") {
		t.Error("英文提示词应包含 'Memory Restriction Rules'")
	}
	if !strings.Contains(prompt, "passwords") {
		t.Error("英文提示词应包含 pattern 'passwords'")
	}
	if !strings.Contains(prompt, "experience_learn") {
		t.Error("英文提示词应包含执行要求中的 experience_learn")
	}
	if !strings.Contains(prompt, "Types of sensitive information forbidden to remember") {
		t.Error("有 patterns 时应包含列表标题")
	}
}

// TestBuildForbiddenPromptEN_无Patterns 验证英文无 patterns
func TestBuildForbiddenPromptEN_无Patterns(t *testing.T) {
	prompt := buildForbiddenPromptEN("sensitive information", nil)

	if !strings.Contains(prompt, "Memory Restriction Rules") {
		t.Error("无 patterns 时仍应包含标题")
	}
	if strings.Contains(prompt, "Types of sensitive information") {
		t.Error("无 patterns 时不应输出 pattern 列表")
	}
}

// TestBuildForbiddenPromptCN_空描述 验证无描述时跳过描述段
func TestBuildForbiddenPromptCN_空描述(t *testing.T) {
	prompt := buildForbiddenPromptCN("", []string{"密码"})
	// 应有标题 + patterns + 执行要求，但无描述段
	if !strings.Contains(prompt, "记忆限制规则") {
		t.Error("应有标题")
	}
	if !strings.Contains(prompt, "密码") {
		t.Error("应有 patterns")
	}
}
