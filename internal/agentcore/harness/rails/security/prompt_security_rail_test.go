package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockSystemPromptBuilder 用于测试的 SystemPromptBuilder mock
type mockSystemPromptBuilder struct {
	sections map[string]saprompt.PromptSection
	language string
}

func newMockSystemPromptBuilder() *mockSystemPromptBuilder {
	return &mockSystemPromptBuilder{
		sections: make(map[string]saprompt.PromptSection),
		language: "cn",
	}
}

func (m *mockSystemPromptBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}

func (m *mockSystemPromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	return nil
}

func (m *mockSystemPromptBuilder) Language() string                               { return m.language }
func (m *mockSystemPromptBuilder) GetSection(name string) *saprompt.PromptSection { return nil }
func (m *mockSystemPromptBuilder) HasSection(name string) bool                    { return false }

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewSafetyPromptRail 测试创建 SafetyPromptRail
func TestNewSafetyPromptRail(t *testing.T) {
	r := NewSafetyPromptRail()
	assert.Equal(t, safetyPromptRailPriority, r.Priority())
	assert.True(t, r.supportedEvents[agentinterfaces.CallbackBeforeModelCall])
}

// TestSafetyPromptRail_InitUninit 测试初始化和反初始化
func TestSafetyPromptRail_InitUninit(t *testing.T) {
	r := NewSafetyPromptRail()
	builder := newMockSystemPromptBuilder()

	// 设置 systemPromptBuilder（模拟 Init）
	r.systemPromptBuilder = builder
	assert.NotNil(t, r.systemPromptBuilder)

	// Uninit
	r.Uninit(nil)
	assert.Nil(t, r.systemPromptBuilder)
}

// TestSafetyPromptRail_RunSecurityCheck_注入测试 测试注入安全提示词
// 对齐 Python: SafetyPromptRail.run_security_check — add_section(safety_section)
func TestSafetyPromptRail_RunSecurityCheck_注入测试(t *testing.T) {
	r := NewSafetyPromptRail()
	builder := newMockSystemPromptBuilder()
	r.systemPromptBuilder = builder

	decision, err := r.runSecurityCheck(nil, nil)
	require.NoError(t, err)
	allow, ok := decision.(*SecurityAllow)
	require.True(t, ok)
	assert.NotNil(t, allow)

	// 验证 section 被注入
	section, exists := builder.sections[sections.SectionSafety]
	assert.True(t, exists, "safety section 应被注入到 system prompt builder")
	assert.Equal(t, sections.SectionSafety, section.Name)
	assert.Equal(t, 20, section.Priority)
}

// TestSafetyPromptRail_RunSecurityCheck_无Builder 测试无 systemPromptBuilder 时返回 Allow
// 对齐 Python: if self.system_prompt_builder is None: return self.allow()
func TestSafetyPromptRail_RunSecurityCheck_无Builder(t *testing.T) {
	r := NewSafetyPromptRail()
	r.systemPromptBuilder = nil

	decision, err := r.runSecurityCheck(nil, nil)
	require.NoError(t, err)
	_, ok := decision.(*SecurityAllow)
	require.True(t, ok, "无 systemPromptBuilder 应返回 SecurityAllow")
}

// TestSafetyPromptRail_SecurityRailAlias 测试 SecurityRail 类型别名
// 对齐 Python: SecurityRail = SafetyPromptRail
func TestSafetyPromptRail_SecurityRailAlias(t *testing.T) {
	// SecurityRail = SafetyPromptRail 类型别名
	// 编译时验证：NewSafetyPromptRail 返回的 *SafetyPromptRail 可赋值给 AgentRail
	r := NewSafetyPromptRail()
	var _ agentinterfaces.AgentRail = r
	assert.NotNil(t, r)
}
