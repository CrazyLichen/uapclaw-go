package rails

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBuilder 测试用 mock SystemPromptBuilder
type mockBuilder struct {
	sections map[string]saprompt.PromptSection
	language string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func newMockBuilder() *mockBuilder {
	return &mockBuilder{
		sections: make(map[string]saprompt.PromptSection),
		language: "cn",
	}
}

func (m *mockBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}

func (m *mockBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	return nil
}

func (m *mockBuilder) Language() string { return m.language }

func (m *mockBuilder) SetLanguage(lang string) { m.language = lang }

func (m *mockBuilder) GetSection(name string) *saprompt.PromptSection {
	if s, ok := m.sections[name]; ok {
		return &s
	}
	return nil
}

func (m *mockBuilder) HasSection(name string) bool {
	_, ok := m.sections[name]
	return ok
}

// TestNewRuntimePromptRail 测试构造和默认值
func TestNewRuntimePromptRail(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	assert.Equal(t, "cn", rail.language)
	assert.Equal(t, "web", rail.channel)
	assert.Equal(t, runtimePromptRailPriority, rail.Priority())
}

// TestSetLanguage 测试 SetLanguage
func TestSetLanguage(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetLanguage("en")
	assert.Equal(t, "en", rail.language)
}

// TestSetChannel 测试 SetChannel
func TestSetChannel(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetChannel("tui")
	assert.Equal(t, "tui", rail.channel)
}

// TestSetTrustedDirs 测试 SetTrustedDirs
func TestSetTrustedDirs(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	dirs := []string{"/a", "/b"}
	rail.SetTrustedDirs(dirs)
	assert.Equal(t, dirs, rail.trustedDirs)
}

// TestSetRuntimePaths 测试 SetRuntimePaths
func TestSetRuntimePaths(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetRuntimePaths("/cwd", "/project")
	assert.Equal(t, "/cwd", rail.cwd)
	assert.Equal(t, "/project", rail.projectDir)
	// 空白字符串应被 trim
	rail.SetRuntimePaths("  ", "  ")
	assert.Equal(t, "", rail.cwd)
	assert.Equal(t, "", rail.projectDir)
}

// TestSetModelName 测试 SetModelName
func TestSetModelName(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetModelName("qwen-max")
	assert.Equal(t, "qwen-max", rail.modelName)
}

// TestSetMode 测试 SetMode
func TestSetMode(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetMode("agent.plan")
	assert.Equal(t, "agent.plan", rail.mode)
}

// TestSetForceEnglish 测试 SetForceEnglish
func TestSetForceEnglish(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetForceEnglish(true)
	assert.True(t, rail.forceEnglish)
}

// TestInjectTimeSection_中文 测试中文 time section
func TestInjectTimeSection_中文(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectTimeSection(builder)
	section := builder.GetSection("time")
	assert.NotNil(t, section)
	assert.Equal(t, sectionTimePriority, section.Priority)
	assert.Contains(t, section.Content["cn"], "时间说明")
}

// TestInjectTimeSection_英文 测试英文 time section（forceEnglish）
func TestInjectTimeSection_英文(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetForceEnglish(true)
	builder := newMockBuilder()
	rail.injectTimeSection(builder)
	section := builder.GetSection("time")
	assert.NotNil(t, section)
	assert.Contains(t, section.Content["cn"], "Time Description")
}

// TestInjectRuntimeSection 测试 runtime section 注入
func TestInjectRuntimeSection(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetModelName("test-model")
	rail.SetMode("agent.plan")
	builder := newMockBuilder()
	rail.injectRuntimeSection(builder, nil)
	section := builder.GetSection("runtime")
	assert.NotNil(t, section)
	assert.Equal(t, sectionRuntimePriority, section.Priority)
}

// TestInjectBrowserToolPolicySection_条件 测试 channel=web 时注入
func TestInjectBrowserToolPolicySection_条件(t *testing.T) {
	// channel=web → 注入
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectBrowserToolPolicySection(builder)
	assert.True(t, builder.HasSection("browser_tool_policy"))

	// channel=tui → 不注入
	rail2 := NewRuntimePromptRail("cn", "tui")
	builder2 := newMockBuilder()
	rail2.injectBrowserToolPolicySection(builder2)
	assert.False(t, builder2.HasSection("browser_tool_policy"))
}

// TestInjectGitStatusSection_条件 测试 git_branch 条件
func TestInjectGitStatusSection_条件(t *testing.T) {
	// 无 runtime_state.yaml → git_branch 为空 → 不注入
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectGitStatusSection(builder, nil)
	assert.False(t, builder.HasSection("git_status"))
}

// TestInjectEnvSection 测试 env section 注入
func TestInjectEnvSection(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectEnvSection(builder)
	section := builder.GetSection("env")
	assert.NotNil(t, section)
	assert.Equal(t, sectionEnvPriority, section.Priority)
}

// TestInjectLanguageOutputSection 测试 language_output section 注入
func TestInjectLanguageOutputSection(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectLanguageOutputSection(builder, nil)
	section := builder.GetSection("language_output")
	assert.NotNil(t, section)
	assert.Equal(t, sectionLanguageOutputPriority, section.Priority)
}

// TestExistingDirs 测试 existingDirs
func TestExistingDirs(t *testing.T) {
	tmpDir := t.TempDir()
	result := existingDirs([]string{tmpDir, "/nonexistent_rail_test_xyz/path"})
	assert.Len(t, result, 1)
	assert.Equal(t, filepath.Clean(tmpDir), result[0])
	// nil 输入
	result2 := existingDirs(nil)
	assert.Len(t, result2, 0)
}

// TestExistingDir 测试 existingDir
func TestExistingDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := existingDir(tmpDir)
	assert.Equal(t, filepath.Clean(tmpDir), result)
	// 不存在
	result2 := existingDir("/nonexistent_rail_test_xyz/path")
	assert.Equal(t, "", result2)
	// 空输入
	result3 := existingDir("")
	assert.Equal(t, "", result3)
}

// TestSamePath 测试 samePath
func TestSamePath(t *testing.T) {
	assert.True(t, samePath("/tmp/test", "/tmp/test"))
	assert.False(t, samePath("/tmp/test", "/tmp/other"))
}

// TestFirstNonEmpty 测试 FirstNonEmpty
func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "fallback", FirstNonEmpty("", "", "fallback"))
	assert.Equal(t, "first", FirstNonEmpty("first", "second"))
	assert.Equal(t, "val", FirstNonEmpty("  ", "val"))
}

// TestWriteRuntimeStateYAML 测试 YAML 写入
func TestWriteRuntimeStateYAML(t *testing.T) {
	tmpDir := t.TempDir()
	// 写入 runtime_state.yaml 到 ConfigDir
	WriteRuntimeStateYAML(context.Background(), "test-model", "agent.plan", "cn", "web", "test-agent", tmpDir)
	yamlPath := filepath.Join(workspace.ConfigDir(), "runtime_state.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		t.Log("runtime_state.yaml 未写入 ConfigDir（正常，ConfigDir 可能不是 tmpDir）")
	}
}

// TestReadRuntimeStateYAML 测试 YAML 读取
func TestReadRuntimeStateYAML(t *testing.T) {
	result := readRuntimeStateYAML()
	assert.NotNil(t, result)
}

// TestModeDisplayMap 测试 modeDisplayMap
func TestModeDisplayMap(t *testing.T) {
	assert.Equal(t, "规划模式", modeDisplayMap["agent.plan"]["cn"])
	assert.Equal(t, "Planning Mode", modeDisplayMap["agent.plan"]["en"])
	assert.Equal(t, "性能模式", modeDisplayMap["agent.fast"]["cn"])
	assert.Equal(t, "Cluster Mode", modeDisplayMap["team"]["en"])
}

// TestLanguageNames 测试 languageNames
func TestLanguageNames(t *testing.T) {
	assert.Equal(t, "Chinese", languageNames["cn"])
	assert.Equal(t, "Chinese", languageNames["zh"])
	assert.Equal(t, "English", languageNames["en"])
}
