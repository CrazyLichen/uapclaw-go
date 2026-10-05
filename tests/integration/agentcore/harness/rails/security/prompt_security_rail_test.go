//go:build integration

package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	rails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	security "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSecurityRailSuite SafetyPromptRail 集成测试套件。
//
// 对齐 Python: tests/system_tests/rail/test_guardrail.py
// 验证 SafetyPromptRail（别名 SecurityRail）的优先级、构造、回调注册、
// 安全提示词注入和 AgentRail 接口满足性。
type PromptSecurityRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPromptSecurityRailSuite(t *testing.T) {
	suite.Run(t, new(PromptSecurityRailSuite))
}

// TestPromptSecurityRailPriority 测试 SafetyPromptRail 优先级。
// 对齐 Python: SafetyPromptRail.priority = 85
func (s *PromptSecurityRailSuite) TestPromptSecurityRailPriority() {
	r := security.NewSafetyPromptRail()
	s.Equal(85, r.Priority(), "SafetyPromptRail 优先级应为 85")
}

// TestNewPromptSecurityRailConstruction 测试 SafetyPromptRail 构造。
// 对齐 Python: SafetyPromptRail.__init__()
func (s *PromptSecurityRailSuite) TestNewPromptSecurityRailConstruction() {
	r := security.NewSafetyPromptRail()
	s.NotNil(r, "SafetyPromptRail 不应为 nil")
	// 验证订阅了 BeforeModelCall 事件
	callbacks := r.GetCallbacks()
	_, hasBeforeModelCall := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBeforeModelCall, "应订阅 BeforeModelCall 事件")
}

// TestGetCallbacksReturnsExpectedCallback 测试 GetCallbacks 返回期望的回调。
// 对齐 Python: SafetyPromptRail.get_callbacks() 返回 before_model_call 钩子。
func (s *PromptSecurityRailSuite) TestGetCallbacksReturnsExpectedCallback() {
	r := security.NewSafetyPromptRail()
	callbacks := r.GetCallbacks()
	s.NotNil(callbacks, "回调映射不应为 nil")
	// SafetyPromptRail 只订阅 BeforeModelCall
	s.Contains(callbacks, agentinterfaces.CallbackBeforeModelCall, "应包含 BeforeModelCall 回调")
}

// TestBeforeModelCallCallbackExists 测试 BeforeModelCall 回调存在。
// 对齐 Python: SafetyPromptRail 在 BeforeModelCall 事件上注入安全提示词。
func (s *PromptSecurityRailSuite) TestBeforeModelCallCallbackExists() {
	r := security.NewSafetyPromptRail()
	callbacks := r.GetCallbacks()
	cb, exists := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(exists, "BeforeModelCall 回调应存在")
	s.NotNil(cb, "BeforeModelCall 回调函数不应为 nil")
}

// TestWithCustomPatterns 测试自定义模式匹配。
// 对齐 Python: BaseSecurityRail._contains_any_pattern — 检测文本是否匹配正则。
func (s *PromptSecurityRailSuite) TestWithCustomPatterns() {
	r := security.NewBaseSecurityRail()
	// 验证正则模式匹配
	s.True(r.ContainsAnyPattern("secret API key=12345", []string{`secret`}), "应匹配 'secret' 模式")
	s.True(r.ContainsAnyPattern("password: abc123", []string{`password`}), "应匹配 'password' 模式")
	s.False(r.ContainsAnyPattern("normal safe content", []string{`secret`, `password`}), "不应匹配任何模式")
}

// TestWithDenyPatterns 测试拒绝模式匹配。
// 对齐 Python: BaseSecurityRail._contains_any_pattern — 多模式拒绝列表。
func (s *PromptSecurityRailSuite) TestWithDenyPatterns() {
	r := security.NewBaseSecurityRail()
	denyPatterns := []string{`api[_-]?key`, `secret[_-]?key`, `password`}
	s.True(r.ContainsAnyPattern("api_key=sk-xxx", denyPatterns), "应匹配 api_key 模式")
	s.True(r.ContainsAnyPattern("secret-key=value", denyPatterns), "应匹配 secret-key 模式")
	s.True(r.ContainsAnyPattern("password=12345", denyPatterns), "应匹配 password 模式")
	s.False(r.ContainsAnyPattern("normal public data", denyPatterns), "不应匹配拒绝模式")
}

// TestDefaultDenyPatterns 测试默认拒绝模式。
// 对齐 Python: BaseSecurityRail._contains_any_pattern 默认行为。
func (s *PromptSecurityRailSuite) TestDefaultDenyPatterns() {
	r := security.NewBaseSecurityRail()
	// 空模式列表不应匹配任何内容
	s.False(r.ContainsAnyPattern("any text", []string{}), "空模式列表不应匹配")
	// 无效正则不应 panic
	s.False(r.ContainsAnyPattern("test", []string{`[invalid`}), "无效正则应返回 false 不 panic")
}

// TestInterfaceSatisfaction 测试 SafetyPromptRail 满足 AgentRail 接口。
// 对齐 Python: isinstance(rail, AgentRail)
func (s *PromptSecurityRailSuite) TestInterfaceSatisfaction() {
	r := security.NewSafetyPromptRail()
	var _ agentinterfaces.AgentRail = r
	s.NotNil(r, "SafetyPromptRail 应满足 AgentRail 接口")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时验证各 Rail 类型满足 AgentRail 接口
var (
	_ agentinterfaces.AgentRail = (*security.SafetyPromptRail)(nil)
	_ agentinterfaces.AgentRail = (*security.BaseSecurityRail)(nil)
)

// 编译时验证 rails.DeepAgentRailProvider 满足
var _ rails.DeepAgentRailProvider = (*security.BaseSecurityRail)(nil)

// unusedContext 供测试使用（避免每次声明）
var unusedContext = context.Background()
