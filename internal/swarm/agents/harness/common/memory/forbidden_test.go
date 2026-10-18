package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── GetForbiddenMemoryPrompt 测试 ────────────────────────────

// TestGetForbiddenMemoryPrompt_未配置返回空 验证未配置时 enabled=false 返回空串和 nil error
func TestGetForbiddenMemoryPrompt_未配置返回空(t *testing.T) {
	prompt, err := GetForbiddenMemoryPrompt("cn")
	if err != nil {
		t.Errorf("未配置时 GetForbiddenMemoryPrompt 不应返回 error，实际: %v", err)
	}
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

// isolateForbiddenConfig 隔离全局配置路径到临时目录（forbidden 走 config.New("") 全局路径）。
// 返回 config.yaml 完整路径。
func isolateForbiddenConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	pathutil.ResetCache()
	return filepath.Join(configDir, "config.yaml")
}

// TestGetForbiddenMemoryPrompt_完整配置 对齐 Python：enabled=true 时生成完整提示词
func TestGetForbiddenMemoryPrompt_完整配置(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	yaml := `
memory:
  forbidden_memory_definition:
    enabled: true
    patterns: ["密码", "API密钥"]
    description:
      zh: "禁止记忆敏感信息"
      en: "Forbidden sensitive info"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	// 中文（zh 命中 description）
	prompt, err := GetForbiddenMemoryPrompt("zh")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !strings.Contains(prompt, "记忆限制规则") {
		t.Errorf("应含中文标题: %q", prompt)
	}
	if !strings.Contains(prompt, "禁止记忆敏感信息") {
		t.Errorf("应含 zh 描述: %q", prompt)
	}
	if !strings.Contains(prompt, "`密码`") {
		t.Errorf("应含 pattern: %q", prompt)
	}

	// 英文
	promptEN, err := GetForbiddenMemoryPrompt("en")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !strings.Contains(promptEN, "Memory Restriction Rules") {
		t.Errorf("应含英文标题: %q", promptEN)
	}
	if !strings.Contains(promptEN, "Forbidden sensitive info") {
		t.Errorf("应含 en 描述: %q", promptEN)
	}

	// 未知语言：描述回退 zh（"禁止记忆敏感信息"），但模板走英文（对齐 Python：
	// 仅 "zh"/"cn" 走中文模板，其他语言走英文模板）
	promptFR, _ := GetForbiddenMemoryPrompt("fr")
	if !strings.Contains(promptFR, "Memory Restriction Rules") {
		t.Errorf("未知语言应走英文模板: %q", promptFR)
	}
	if !strings.Contains(promptFR, "禁止记忆敏感信息") {
		t.Errorf("未知语言描述应回退 zh: %q", promptFR)
	}
}

// TestGetForbiddenMemoryPrompt_cn别名 对齐 Python："cn" 同样走中文模板
func TestGetForbiddenMemoryPrompt_cn别名(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	yaml := `
memory:
  forbidden_memory_definition:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	prompt, err := GetForbiddenMemoryPrompt("cn")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !strings.Contains(prompt, "记忆限制规则") {
		t.Errorf("cn 应走中文模板: %q", prompt)
	}
}

// TestGetMemoryForbiddenConfigSafe_段缺失 对齐 Python：memory 段/键缺失返回 Enabled=false
func TestGetMemoryForbiddenConfigSafe_段缺失(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	// memory 段存在但无 forbidden_memory_definition
	if err := os.WriteFile(cfgPath, []byte("memory:\n  other: 1\n"), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := getMemoryForbiddenConfigSafe()
	if err != nil {
		t.Fatalf("段缺失不应报错: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("键缺失应 Enabled=false")
	}
}

// TestGetMemoryForbiddenConfigSafe_memory段类型错误 对齐 Python：类型不匹配返回默认
func TestGetMemoryForbiddenConfigSafe_memory段类型错误(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	if err := os.WriteFile(cfgPath, []byte("memory: not-a-map\n"), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := getMemoryForbiddenConfigSafe()
	if err != nil {
		t.Fatalf("类型错误不应报错: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("类型错误应 Enabled=false")
	}
}

// TestGetMemoryForbiddenConfigSafe_键类型错误 对齐 Python：键值非 map 返回默认
func TestGetMemoryForbiddenConfigSafe_键类型错误(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	if err := os.WriteFile(cfgPath, []byte("memory:\n  forbidden_memory_definition: [1, 2]\n"), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := getMemoryForbiddenConfigSafe()
	if err != nil {
		t.Fatalf("键类型错误不应报错: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("键类型错误应 Enabled=false")
	}
}

// TestGetMemoryForbiddenConfigSafe_默认描述 对齐 Python：description 为空时补默认双语描述
func TestGetMemoryForbiddenConfigSafe_默认描述(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	yaml := `
memory:
  forbidden_memory_definition:
    enabled: true
    patterns: "not-a-list"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := getMemoryForbiddenConfigSafe()
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !cfg.Enabled {
		t.Errorf("enabled=true 应生效")
	}
	if len(cfg.Patterns) != 0 {
		t.Errorf("非列表 patterns 应被忽略: %v", cfg.Patterns)
	}
	if cfg.Description == nil || cfg.Description["zh"] == "" || cfg.Description["en"] == "" {
		t.Errorf("description 空时应补默认双语: %v", cfg.Description)
	}
}

// TestGetMemoryForbiddenConfigSafe_混合类型patterns 对齐 Python：列表内非字符串元素被忽略
func TestGetMemoryForbiddenConfigSafe_混合类型patterns(t *testing.T) {
	cfgPath := isolateForbiddenConfig(t)
	yaml := `
memory:
  forbidden_memory_definition:
    enabled: true
    patterns: ["密码", 123, "Token"]
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := getMemoryForbiddenConfigSafe()
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(cfg.Patterns) != 2 || cfg.Patterns[0] != "密码" || cfg.Patterns[1] != "Token" {
		t.Errorf("非字符串元素应被忽略: %v", cfg.Patterns)
	}
}

// TestGetMemoryForbiddenConfigSafe_配置不存在 对齐 Python：文件不存在返回空配置（Enabled=false）
func TestGetMemoryForbiddenConfigSafe_配置不存在(t *testing.T) {
	isolateForbiddenConfig(t) // 目录存在但无 config.yaml
	cfg, err := getMemoryForbiddenConfigSafe()
	if err != nil {
		t.Fatalf("配置不存在不应报错: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("无配置应 Enabled=false")
	}
}

// TestGetMemoryForbiddenConfig_安全版 对齐 Python：getMemoryForbiddenConfig 吞错返回默认
func TestGetMemoryForbiddenConfig_安全版(t *testing.T) {
	isolateForbiddenConfig(t) // 无配置文件
	cfg := getMemoryForbiddenConfig()
	if cfg == nil {
		t.Fatalf("不应返回 nil")
	}
	if cfg.Enabled {
		t.Errorf("无配置应 Enabled=false")
	}
}
