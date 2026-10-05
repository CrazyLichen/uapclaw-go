//go:build integration

package browser_move

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	browser "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/browser_move"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserToolsSuite 浏览器运行时工具集的集成测试。
// 验证 BrowserRuntimeRail 构造、回调映射、配置默认值及 AgentRail 接口满足性。
type BrowserToolsSuite struct {
	isuite.AgentSuite
	// runtime 浏览器运行时实例
	runtime *browser.BrowserAgentRuntime
	// rail 浏览器进度追踪 Rail 实例
	rail *browser.BrowserRuntimeRail
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBrowserToolsSuite(t *testing.T) {
	suite.Run(t, new(BrowserToolsSuite))
}

// SetupSuite 初始化测试环境，创建 BrowserAgentRuntime 和 BrowserRuntimeRail。
func (s *BrowserToolsSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()

	// 创建运行时（不连接真实浏览器，仅验证结构）
	guardrails := browser.BuildBrowserGuardrails()
	s.runtime = browser.NewBrowserAgentRuntime(
		"test_provider", "test_key", "https://test.api", "test_model",
		nil, guardrails,
	)
	s.rail = browser.NewBrowserRuntimeRail(s.runtime)
}

// TestBrowserRuntimeRail_构造 验证 BrowserRuntimeRail 构造后非空且持有运行时引用。
func (s *BrowserToolsSuite) TestBrowserRuntimeRail_构造() {
	s.Require().NotNil(s.rail)
	s.Require().NotNil(s.rail.Runtime())
	s.Equal(s.runtime, s.rail.Runtime())
}

// TestBrowserRuntimeRail_Priority 验证 BrowserRuntimeRail 默认优先级为 50（BaseRail 默认值）。
func (s *BrowserToolsSuite) TestBrowserRuntimeRail_Priority() {
	s.Equal(50, s.rail.Priority())
}

// TestBrowserRuntimeRail_GetCallbacks 验证 GetCallbacks 返回 4 个期望的回调事件。
func (s *BrowserToolsSuite) TestBrowserRuntimeRail_GetCallbacks() {
	callbacks := s.rail.GetCallbacks()
	s.Require().NotNil(callbacks)

	// 验证包含 4 个回调事件
	expectedEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackAfterInvoke,
	}
	for _, event := range expectedEvents {
		_, exists := callbacks[event]
		s.True(exists, "GetCallbacks 缺少回调事件: %v", event)
	}
	s.Len(callbacks, 4, "GetCallbacks 应返回 4 个回调事件")
}

// TestBrowserConfig_默认值 验证 BrowserRunGuardrails 默认值与常量一致。
func (s *BrowserToolsSuite) TestBrowserConfig_默认值() {
	guardrails := browser.BuildBrowserGuardrails()
	s.Equal(browser.DefaultGuardrailMaxSteps, guardrails.MaxSteps)
	s.Equal(browser.DefaultGuardrailMaxFailures, guardrails.MaxFailures)
	s.Equal(browser.DefaultBrowserTimeoutS, guardrails.TimeoutS)
	s.Equal(browser.DefaultGuardrailRetryOnce, guardrails.RetryOnce)
	s.False(guardrails.ResumeOnMaxIterations)
}

// TestBrowserConfig_校验 验证 BuildRuntimeSettings 返回完整配置。
func (s *BrowserToolsSuite) TestBrowserConfig_校验() {
	settings := browser.BuildRuntimeSettings()
	s.Require().NotNil(settings)
	s.NotEmpty(settings.Provider)
	s.NotEmpty(settings.ModelName)
	s.Require().NotNil(settings.Guardrails)
	s.Require().NotNil(settings.MCPCfg)
}

// TestToolCard_配置验证 验证 MCP 配置的工具卡片结构完整。
func (s *BrowserToolsSuite) TestToolCard_配置验证() {
	mcpCfg := browser.BuildPlaywrightMCPConfig()
	s.Require().NotNil(mcpCfg)
	// 验证 MCP 配置参数包含 command
	params := mcpCfg.Params
	s.Require().NotNil(params)
	s.Equal(browser.DefaultPlaywrightMCPCommand, params["command"])
}

// TestCallbackFrom_回调构造 验证 BaseRail.CallbackFrom 可正确构造回调映射条目。
func (s *BrowserToolsSuite) TestCallbackFrom_回调构造() {
	// 验证 GetCallbacks 返回的是有效的 PerAgentCallbackFunc
	callbacks := s.rail.GetCallbacks()
	for event, fn := range callbacks {
		s.NotNil(fn, "事件 %v 的回调函数不应为 nil", event)
	}
}

// TestIsBrowserProgressTool_工具判断 验证浏览器进度工具名判断逻辑。
func (s *BrowserToolsSuite) TestIsBrowserProgressTool_工具判断() {
	// 浏览器工具前缀应返回 true
	s.True(browser.IsBrowserProgressTool("browser_navigate"))
	s.True(browser.IsBrowserProgressTool("browser_click"))
	s.True(browser.IsBrowserProgressTool("BROWSER_TYPE"))

	// 排除列表中的工具应返回 false
	s.False(browser.IsBrowserProgressTool("browser_cancel_run"))
	s.False(browser.IsBrowserProgressTool("browser_clear_cancel"))
	s.False(browser.IsBrowserProgressTool("browser_list_custom_actions"))
	s.False(browser.IsBrowserProgressTool("browser_runtime_health"))

	// 非浏览器工具应返回 false
	s.False(browser.IsBrowserProgressTool("read_file"))
	s.False(browser.IsBrowserProgressTool("execute_code"))
	s.False(browser.IsBrowserProgressTool(""))
}

// TestExtractProgressPayload_提取 验证从文本中提取 <browser_progress> 标签。
func (s *BrowserToolsSuite) TestExtractProgressPayload_提取() {
	// 包含 progress 标签
	text := `任务完成<browser_progress>{"status":"completed","completed_steps":["步骤1"]}</browser_progress>`
	clean, payload := browser.ExtractProgressPayload(text)
	s.NotEmpty(clean)
	s.Require().NotNil(payload)
	s.Equal("completed", payload["status"])

	// 不包含 progress 标签
	plainText := "普通输出文本"
	clean2, payload2 := browser.ExtractProgressPayload(plainText)
	s.Equal(plainText, clean2)
	s.Nil(payload2)

	// 空字符串
	clean3, payload3 := browser.ExtractProgressPayload("")
	s.Empty(clean3)
	s.Nil(payload3)
}

// TestAgentRail_接口满足 验证 BrowserRuntimeRail 满足 AgentRail 接口。
func (s *BrowserToolsSuite) TestAgentRail_接口满足() {
	// 编译时接口断言
	var _ agentinterfaces.AgentRail = s.rail

	// 运行时验证关键方法存在
	s.NotNil(s.rail)
	s.Equal(50, s.rail.Priority())

	// 验证回调映射可正常获取
	callbacks := s.rail.GetCallbacks()
	s.NotEmpty(callbacks)
}

// TestBrowserRuntimeRail_Init 验证 Init 方法可调用不 panic。
func (s *BrowserToolsSuite) TestBrowserRuntimeRail_Init() {
	err := s.rail.Init(context.Background(), nil)
	// Init 基类默认实现返回 nil
	s.NoError(err)
}

// TestToolInterface_类型验证 验证 Tool 接口和 ToolCard 类型可正常引用。
func (s *BrowserToolsSuite) TestToolInterface_类型验证() {
	// 验证 tool.Tool 接口存在（编译时检查）
	var _ tool.Tool
	// 验证 cb.PerAgentCallbackFunc 类型存在
	var _ cb.PerAgentCallbackFunc
}
