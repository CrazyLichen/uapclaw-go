package locales

import (
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestMakeTranslator_中文 测试中文翻译器加载 Markdown 描述
func TestMakeTranslator_中文(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	desc := tFunc("workspace_meta")
	if desc == "" {
		t.Error("workspace_meta 中文描述不应为空")
	}
	if !strings.Contains(desc, "文件锁管理") {
		t.Error("workspace_meta 中文描述应包含'文件锁管理'")
	}
}

// TestMakeTranslator_英文 测试英文翻译器加载 Markdown 描述
func TestMakeTranslator_英文(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageEN)
	desc := tFunc("workspace_meta")
	if desc == "" {
		t.Error("workspace_meta 英文描述不应为空")
	}
	if !strings.Contains(desc, "file lock management") {
		t.Error("workspace_meta 英文描述应包含'file lock management'")
	}
}

// TestMakeTranslator_STRINGS参数 测试从 STRINGS 映射查询参数描述
func TestMakeTranslator_STRINGS参数(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	actionDesc := tFunc("workspace_meta", "action")
	if actionDesc == "" {
		t.Error("workspace_meta.action 不应为空")
	}
	if !strings.Contains(actionDesc, "lock") {
		t.Error("workspace_meta.action 应包含'lock'")
	}
}

// TestMakeTranslator_Desc缺失 测试不存在的工具返回空字符串
func TestMakeTranslator_Desc缺失(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	desc := tFunc("nonexistent_tool")
	if desc != "" {
		t.Errorf("不存在的工具描述应为空字符串，实际: %s", desc)
	}
}

// TestMakeTranslator_Key缺失 测试不存在的 key 返回 key 本身
func TestMakeTranslator_Key缺失(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	result := tFunc("workspace_meta", "nonexistent_key")
	if result != "workspace_meta.nonexistent_key" {
		t.Errorf("缺失 key 应返回 'workspace_meta.nonexistent_key'，实际: %s", result)
	}
}
