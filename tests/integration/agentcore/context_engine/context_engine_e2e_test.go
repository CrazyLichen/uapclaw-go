//go:build integration

package context_engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/offloader"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── mock 类型 ────────────────────────────

// mockSessionFacade 简单的 SessionFacade mock，用于测试
type mockSessionFacade struct {
	sessionID string
	states    map[string]any
}

func newMockSessionFacade(sessionID string) *mockSessionFacade {
	return &mockSessionFacade{
		sessionID: sessionID,
		states:    make(map[string]any),
	}
}

func (m *mockSessionFacade) GetSessionID() string { return m.sessionID }
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.states[k] = v
	}
}
func (m *mockSessionFacade) GetState(key state.StateKey) (any, error) {
	v, ok := m.states[key.String()]
	if !ok {
		return nil, nil
	}
	return v, nil
}
func (m *mockSessionFacade) DumpState() map[string]any {
	result := make(map[string]any, len(m.states))
	for k, v := range m.states {
		result[k] = v
	}
	return result
}
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (m *mockSessionFacade) GetEnv(key string, defaultValue ...any) any { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error    { return nil }
func (m *mockSessionFacade) ClearSession(_ context.Context) {
	m.states = make(map[string]any)
}

// 确保编译期接口合规
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// mockTokenCounter 简单的 Token 计数器 mock，按字符数/4 估算
type mockTokenCounter struct {
	// factor 估算系数，默认 4（字符数/factor）
	factor int
}

func newMockTokenCounter() *mockTokenCounter {
	return &mockTokenCounter{factor: 4}
}

func (m *mockTokenCounter) Count(text string, _ string) (int, error) {
	return len(text) / m.factor, nil
}

func (m *mockTokenCounter) CountMessages(messages []llm_schema.BaseMessage, _ string) (int, error) {
	total := 0
	for _, msg := range messages {
		total += len(msg.GetContent().Text()) / m.factor
	}
	return total, nil
}

func (m *mockTokenCounter) CountTools(tools []schema.ToolInfoInterface, _ string) (int, error) {
	// 简化：工具 token 数为 0
	return 0, nil
}

// 确保编译期接口合规
var _ token.TokenCounter = (*mockTokenCounter)(nil)

// ──────────────────────────── 测试套件 ────────────────────────────

// ContextEngineE2ESuite 上下文引擎 E2E 集成测试套件
type ContextEngineE2ESuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
}

func TestContextEngineE2ESuite(t *testing.T) {
	suite.Run(t, new(ContextEngineE2ESuite))
}

func (s *ContextEngineE2ESuite) SetupTest() {
	s.ctx = context.Background()
}

// ──────────────────────────── ContextEngine 门面 ────────────────────────────

// TestContextEngine_创建默认配置 测试 ContextEngine 默认配置创建
func (s *ContextEngineE2ESuite) TestContextEngine_创建默认配置() {
	config := ceschema.NewContextEngineConfig()
	s.Equal(0, config.MaxContextMessageNum)
	s.Equal(0, config.DefaultWindowMessageNum)
	s.Equal(0, config.DefaultWindowRoundNum)
	s.Empty(config.ModelName)

	ce := context_engine.NewContextEngine(config)
	s.NotNil(ce, "ContextEngine 应成功创建")
}

// TestContextEngine_创建自定义配置 测试自定义配置创建
func (s *ContextEngineE2ESuite) TestContextEngine_创建自定义配置() {
	config := ceschema.ContextEngineConfig{
		MaxContextMessageNum:     100,
		DefaultWindowMessageNum:  50,
		DefaultWindowRoundNum:    10,
		EnableKVCacheRelease:     true,
		EnableReload:             true,
		ModelName:                "qwen-max",
		ContextWindowTokens:      32000,
		ModelContextWindowTokens: map[string]int{"qwen-max": 32000},
	}
	s.NoError(config.Validate())

	ce := context_engine.NewContextEngine(config)
	s.NotNil(ce)
}

// TestContextEngine_CreateContext_新建 测试创建新上下文
func (s *ContextEngineE2ESuite) TestContextEngine_CreateContext_新建() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess)
	s.NoError(err)
	s.NotNil(mc)
	s.Equal("ctx-1", mc.ContextID())
	s.Equal("sess-1", mc.SessionID())
}

// TestContextEngine_CreateContext_空contextID使用默认值 测试空 contextID 回退到默认值
func (s *ContextEngineE2ESuite) TestContextEngine_CreateContext_空contextID使用默认值() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	mc, err := ce.CreateContext(s.ctx, "", sess)
	s.NoError(err)
	s.NotNil(mc)
	// 空字符串使用 defaultContextID
	s.Equal("default_context_id", mc.ContextID())
}

// TestContextEngine_CreateContext_复用已有上下文 测试重复 CreateContext 返回同一实例
func (s *ContextEngineE2ESuite) TestContextEngine_CreateContext_复用已有上下文() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	mc1, err := ce.CreateContext(s.ctx, "ctx-1", sess)
	s.NoError(err)

	mc2, err := ce.CreateContext(s.ctx, "ctx-1", sess)
	s.NoError(err)

	// 同一 sessionID + contextID 返回同一实例
	s.Equal(mc1, mc2, "相同 contextID 应返回同一 ModelContext")
}

// TestContextEngine_GetContext 测试 GetContext 获取上下文
func (s *ContextEngineE2ESuite) TestContextEngine_GetContext() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	// 不存在时返回 nil
	mc := ce.GetContext("nonexist", "sess-1")
	s.Nil(mc)

	// 创建后可获取
	_, err := ce.CreateContext(s.ctx, "ctx-1", sess)
	s.NoError(err)
	mc = ce.GetContext("ctx-1", "sess-1")
	s.NotNil(mc)
	s.Equal("ctx-1", mc.ContextID())
}

// TestContextEngine_ClearContext_全清 测试清空所有上下文
func (s *ContextEngineE2ESuite) TestContextEngine_ClearContext_全清() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	ce.CreateContext(s.ctx, "ctx-1", sess)
	ce.CreateContext(s.ctx, "ctx-2", sess)

	err := ce.ClearContext(s.ctx)
	s.NoError(err)

	s.Nil(ce.GetContext("ctx-1", "sess-1"))
	s.Nil(ce.GetContext("ctx-2", "sess-1"))
}

// TestContextEngine_ClearContext_按SessionID 测试按 session 清除上下文
func (s *ContextEngineE2ESuite) TestContextEngine_ClearContext_按SessionID() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess1 := newMockSessionFacade("sess-1")
	sess2 := newMockSessionFacade("sess-2")

	ce.CreateContext(s.ctx, "ctx-1", sess1)
	ce.CreateContext(s.ctx, "ctx-1", sess2)

	err := ce.ClearContext(s.ctx, iface.WithSessionID("sess-1"))
	s.NoError(err)

	s.Nil(ce.GetContext("ctx-1", "sess-1"))
	s.NotNil(ce.GetContext("ctx-1", "sess-2"), "sess-2 的上下文应保留")
}

// TestContextEngine_ClearContext_精确删除 测试按 sessionID + contextID 精确删除
func (s *ContextEngineE2ESuite) TestContextEngine_ClearContext_精确删除() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	ce.CreateContext(s.ctx, "ctx-1", sess)
	ce.CreateContext(s.ctx, "ctx-2", sess)

	err := ce.ClearContext(s.ctx, iface.WithSessionID("sess-1"), iface.WithContextID("ctx-1"))
	s.NoError(err)

	s.Nil(ce.GetContext("ctx-1", "sess-1"))
	s.NotNil(ce.GetContext("ctx-2", "sess-1"), "ctx-2 应保留")
}

// TestContextEngine_SaveContexts 测试批量持久化上下文状态
func (s *ContextEngineE2ESuite) TestContextEngine_SaveContexts() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess)
	s.NoError(err)

	// 添加消息后再保存
	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("world"),
	})

	states, err := ce.SaveContexts(s.ctx, sess, nil)
	s.NoError(err)
	s.NotNil(states)
	// 验证 session state 中写入了 context 键
	contextState, err := sess.GetState(state.StringKey("context"))
	s.NoError(err)
	s.NotNil(contextState, "session 中应包含 context 状态")
}

// TestContextEngine_SaveContexts_nilSession 测试 nil session 不 panic
func (s *ContextEngineE2ESuite) TestContextEngine_SaveContexts_nilSession() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)

	states, err := ce.SaveContexts(s.ctx, nil, nil)
	s.NoError(err)
	s.Nil(states, "nil session 应返回 nil")
}

// ──────────────────────────── SessionModelContext 核心 ────────────────────────────

// TestSessionModelContext_AddMessages 测试消息添加
func (s *ContextEngineE2ESuite) TestSessionModelContext_AddMessages() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好，有什么可以帮助你的？"),
	}
	result, err := mc.AddMessages(s.ctx, msgs)
	s.NoError(err)
	s.Len(result, 2)
	s.Equal(2, mc.Len(), "AddMessages 后消息数应为 2")
}

// TestSessionModelContext_GetContextWindow 测试上下文窗口构建
func (s *ContextEngineE2ESuite) TestSessionModelContext_GetContextWindow() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	// 添加消息
	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一个问题"),
		llm_schema.NewAssistantMessage("第一个回答"),
		llm_schema.NewUserMessage("第二个问题"),
		llm_schema.NewAssistantMessage("第二个回答"),
	})

	// 构建上下文窗口
	sysMsgs := []llm_schema.BaseMessage{llm_schema.NewSystemMessage("你是一个助手")}
	window, err := mc.GetContextWindow(s.ctx, sysMsgs, nil, 0, 0)
	s.NoError(err)
	s.NotNil(window)
	// 系统消息 + 上下文消息
	s.Equal(1, len(window.SystemMessages))
	s.Equal(4, len(window.ContextMessages))
}

// TestSessionModelContext_GetContextWindow_窗口截断 测试窗口大小截断
func (s *ContextEngineE2ESuite) TestSessionModelContext_GetContextWindow_窗口截断() {
	config := ceschema.ContextEngineConfig{
		DefaultWindowMessageNum: 2,
		ModelContextWindowTokens: map[string]int{},
	}
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	// 添加 6 条消息
	for i := 0; i < 3; i++ {
		mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(fmt.Sprintf("问题%d", i)),
			llm_schema.NewAssistantMessage(fmt.Sprintf("回答%d", i)),
		})
	}

	// 直接用 SessionModelContext 测试窗口截断
	window, err := mc.GetContextWindow(s.ctx, nil, nil, 2, 0)
	s.NoError(err)
	// windowSize=2，contextMessages 最多保留 2 条
	s.LessOrEqual(len(window.ContextMessages), 2, "窗口应截断到 2 条消息")
}

// TestSessionModelContext_Statistic 测试统计信息
func (s *ContextEngineE2ESuite) TestSessionModelContext_Statistic() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("助手回答"),
	})

	stat := mc.Statistic()
	s.NotNil(stat)
	s.Equal(2, stat.TotalMessages)
	s.Equal(1, stat.UserMessages)
	s.Equal(1, stat.AssistantMessages)
}

// TestSessionModelContext_ClearMessages 测试清空消息
func (s *ContextEngineE2ESuite) TestSessionModelContext_ClearMessages() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	})
	s.Equal(2, mc.Len())

	err = mc.ClearMessages(s.ctx, true)
	s.NoError(err)
	s.Equal(0, mc.Len(), "ClearMessages 后消息数应为 0")
}

// TestSessionModelContext_SaveLoadState 测试状态保存和恢复
func (s *ContextEngineE2ESuite) TestSessionModelContext_SaveLoadState() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("原始问题"),
		llm_schema.NewAssistantMessage("原始回答"),
	})

	// 保存状态
	savedState := mc.SaveState()
	s.NotNil(savedState)
	s.Contains(savedState, "messages")

	// 创建新实例并恢复
	mc2, err := ce.CreateContext(s.ctx, "ctx-2", sess, iface.WithTokenCounter(tc))
	s.NoError(err)
	mc2.LoadState(savedState)
	// 恢复后消息数应一致
	s.Equal(mc.Len(), mc2.Len(), "恢复后消息数应一致")
}

// TestSessionModelContext_PopMessages 测试弹出消息
func (s *ContextEngineE2ESuite) TestSessionModelContext_PopMessages() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),
		llm_schema.NewAssistantMessage("回答1"),
		llm_schema.NewUserMessage("问题2"),
	})
	s.Equal(3, mc.Len())

	popped := mc.PopMessages(1, true)
	s.Len(popped, 1, "应弹出 1 条消息")
	s.Equal(2, mc.Len(), "弹出后应剩 2 条")
}

// TestSessionModelContext_OffloadMessages 测试卸载消息
func (s *ContextEngineE2ESuite) TestSessionModelContext_OffloadMessages() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewToolMessage("tc-1", "很长的工具结果内容，超过了阈值大小"),
	}
	mc.OffloadMessages("handle-1", msgs)
	// OffloadMessages 不应 panic，通过 SaveState 验证卸载缓冲区非空
	savedState := mc.SaveState()
	offloadMsgs, ok := savedState["offload_messages"]
	s.True(ok, "SaveState 应包含 offload_messages")
	s.NotNil(offloadMsgs)
}

// ──────────────────────────── 处理器注册表 ────────────────────────────

// TestContextEngine_处理器注册表 测试已注册的处理器类型
func (s *ContextEngineE2ESuite) TestContextEngine_处理器注册表() {
	// 触发 init() 注册
	_ = compressor.DialogueCompressorConfig{}
	_ = offloader.MessageOffloaderConfig{}
	_ = compressor.MicroCompactProcessorConfig{}

	factories := context_engine.ListProcessorFactories()
	s.NotEmpty(factories, "应有处理器注册")

	// 验证核心处理器已注册
	factoryMap := make(map[string]bool)
	for _, f := range factories {
		factoryMap[f] = true
	}
	s.True(factoryMap["DialogueCompressor"], "DialogueCompressor 应已注册")
	s.True(factoryMap["MessageOffloader"], "MessageOffloader 应已注册")
	s.True(factoryMap["MicroCompactProcessor"], "MicroCompactProcessor 应已注册")
}

// ──────────────────────────── MicroCompactProcessor ────────────────────────────

// TestMicroCompactProcessor_创建和配置 测试创建和配置验证
func (s *ContextEngineE2ESuite) TestMicroCompactProcessor_创建和配置() {
	cfg := compressor.NewMicroCompactProcessorConfig()
	s.NoError(cfg.Validate())
	s.Equal(5, cfg.TriggerThreshold)
	s.Equal(15, cfg.KeepRecentPerTool)
	s.Equal(compressor.MicroCompactClearedMarker, cfg.ClearedMarker)

	mcp, err := compressor.NewMicroCompactProcessor(cfg)
	s.NoError(err)
	s.NotNil(mcp)
	s.Equal("MicroCompactProcessor", mcp.ProcessorType())
}

// TestMicroCompactProcessor_配置校验失败 测试无效配置
func (s *ContextEngineE2ESuite) TestMicroCompactProcessor_配置校验失败() {
	cfg := &compressor.MicroCompactProcessorConfig{
		TriggerThreshold:   0, // 无效：必须 > 0
		CompactableToolNames: []string{"grep"},
		KeepRecentPerTool:  5,
		ClearedMarker:      "[cleared]",
	}
	err := cfg.Validate()
	s.Error(err, "TriggerThreshold=0 应校验失败")
}

// TestMicroCompactProcessor_触发判断_未超过阈值 测试触发判断
func (s *ContextEngineE2ESuite) TestMicroCompactProcessor_触发判断_未超过阈值() {
	cfg := compressor.NewMicroCompactProcessorConfig()
	mcp, err := compressor.NewMicroCompactProcessor(cfg)
	s.NoError(err)

	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	// 添加少量消息，未超过触发阈值
	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	})

	triggered, err := mcp.TriggerAddMessages(s.ctx, mc, nil)
	s.NoError(err)
	s.False(triggered, "消息数未超过阈值，不应触发")
}

// TestMicroCompactProcessor_清除旧工具结果 测试清除旧工具结果
func (s *ContextEngineE2ESuite) TestMicroCompactProcessor_清除旧工具结果() {
	cfg := &compressor.MicroCompactProcessorConfig{
		TriggerThreshold:      1,
		CompactableToolNames:  []string{"read_file"},
		KeepRecentPerTool:     2,
		ClearedMarker:         compressor.MicroCompactClearedMarker,
	}
	mcp, err := compressor.NewMicroCompactProcessor(cfg)
	s.NoError(err)

	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	// 构造一个完整的 API 轮次：User → Assistant(tool_calls) → Tool
	// 需要超过 triggerThreshold + keepRecentPerTool = 1 + 2 = 3 个同工具结果
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("读文件"),
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			{ID: "tc-1", Name: "read_file", Arguments: `{"path":"a"}`},
			{ID: "tc-2", Name: "read_file", Arguments: `{"path":"b"}`},
			{ID: "tc-3", Name: "read_file", Arguments: `{"path":"c"}`},
			{ID: "tc-4", Name: "read_file", Arguments: `{"path":"d"}`},
		})),
		llm_schema.NewToolMessage("tc-1", "content-a"),
		llm_schema.NewToolMessage("tc-2", "content-b"),
		llm_schema.NewToolMessage("tc-3", "content-c"),
		llm_schema.NewToolMessage("tc-4", "content-d"),
		llm_schema.NewAssistantMessage("结果汇总"),
	}

	mc.AddMessages(s.ctx, msgs)

	// 触发判断
	triggered, err := mcp.TriggerAddMessages(s.ctx, mc, nil)
	s.NoError(err)
	s.True(triggered, "超过阈值应触发微压缩")
}

// TestMicroCompactProcessor_SaveLoadState 测试状态保存和恢复（无状态处理器）
func (s *ContextEngineE2ESuite) TestMicroCompactProcessor_SaveLoadState() {
	cfg := compressor.NewMicroCompactProcessorConfig()
	mcp, err := compressor.NewMicroCompactProcessor(cfg)
	s.NoError(err)

	state := mcp.SaveState()
	s.Empty(state, "MicroCompactProcessor 无状态，SaveState 应返回空 map")

	mcp.LoadState(map[string]any{"key": "value"}) // 不应 panic
}

// ──────────────────────────── MessageOffloader ────────────────────────────

// TestMessageOffloader_创建和配置 测试创建和配置验证
func (s *ContextEngineE2ESuite) TestMessageOffloader_创建和配置() {
	cfg := &offloader.MessageOffloaderConfig{
		TokensThreshold:      1000,
		LargeMessageThreshold: 500,
		TrimSize:             100,
		OffloadMessageTypes:  []string{"tool"},
		KeepLastRound:        ptrBool(true),
	}
	mo, err := offloader.NewMessageOffloader(cfg)
	s.NoError(err)
	s.NotNil(mo)
	s.Equal("MessageOffloader", mo.ProcessorType())
}

// TestMessageOffloader_配置校验失败 测试无效配置
func (s *ContextEngineE2ESuite) TestMessageOffloader_配置校验失败() {
	// TrimSize >= LargeMessageThreshold 应失败
	cfg := &offloader.MessageOffloaderConfig{
		TokensThreshold:       1000,
		LargeMessageThreshold: 100,
		TrimSize:              200, // 无效：应 < LargeMessageThreshold
	}
	_, err := offloader.NewMessageOffloader(cfg)
	s.Error(err, "TrimSize >= LargeMessageThreshold 应校验失败")
}

// TestMessageOffloader_触发判断_消息数未超 测试消息数未超阈值
func (s *ContextEngineE2ESuite) TestMessageOffloader_触发判断_消息数未超() {
	threshold := 100
	cfg := &offloader.MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		OffloadMessageTypes:   []string{"tool"},
		KeepLastRound:         ptrBool(true),
	}
	mo, err := offloader.NewMessageOffloader(cfg)
	s.NoError(err)

	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	})

	triggered, err := mo.TriggerAddMessages(s.ctx, mc, nil)
	s.NoError(err)
	s.False(triggered, "消息数远低于阈值，不应触发")
}

// TestMessageOffloader_触发判断_消息数超阈值 测试消息数超过阈值触发
func (s *ContextEngineE2ESuite) TestMessageOffloader_触发判断_消息数超阈值() {
	threshold := 5
	cfg := &offloader.MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       20000,
		LargeMessageThreshold: 10, // 低阈值便于触发
		TrimSize:              5,
		OffloadMessageTypes:   []string{"tool"},
		KeepLastRound:         ptrBool(false),
	}
	mo, err := offloader.NewMessageOffloader(cfg)
	s.NoError(err)

	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	// 添加超过阈值的消息，包含长工具结果
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			{ID: "tc-1", Name: "search", Arguments: `{}`},
		})),
		llm_schema.NewToolMessage("tc-1", genLongString(100)),
		llm_schema.NewAssistantMessage("回答1"),
		llm_schema.NewUserMessage("问题2"),
	}
	mc.AddMessages(s.ctx, msgs)

	triggered, err := mo.TriggerAddMessages(s.ctx, mc, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题3"),
	})
	s.NoError(err)
	// 消息总数 > threshold=5，且有可卸载的候选
	s.True(triggered, "消息数超过阈值且有可卸载候选，应触发")
}

// TestMessageOffloader_SaveLoadState 测试状态保存和恢复
func (s *ContextEngineE2ESuite) TestMessageOffloader_SaveLoadState() {
	cfg := &offloader.MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
	}
	mo, err := offloader.NewMessageOffloader(cfg)
	s.NoError(err)

	state := mo.SaveState()
	s.Empty(state, "MessageOffloader 无状态，SaveState 应返回空 map")

	mo.LoadState(map[string]any{"key": "value"}) // 不应 panic
}

// ──────────────────────────── KVCacheManager ────────────────────────────

// TestKVCacheManager_创建 测试 KVCacheManager 创建
func (s *ContextEngineE2ESuite) TestKVCacheManager_创建() {
	mgr := cecontext.NewKVCacheManager("sess-1")
	s.NotNil(mgr)
}

// TestKVCacheManager_Release_无Model 测试无模型时跳过释放
func (s *ContextEngineE2ESuite) TestKVCacheManager_Release_无Model() {
	mgr := cecontext.NewKVCacheManager("sess-1")
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("test")}

	err := mgr.Release(s.ctx, window)
	s.NoError(err, "无 Model 时 Release 应成功且不释放")
}

// TestKVCacheManager_Release_首次调用保存快照 测试首次调用保存快照
func (s *ContextEngineE2ESuite) TestKVCacheManager_Release_首次调用保存快照() {
	mgr := cecontext.NewKVCacheManager("sess-1")
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("test")}

	// 首次调用（无 model 也不报错）
	err := mgr.Release(s.ctx, window)
	s.NoError(err)
}

// TestKVCacheManager_Release_连续相同窗口不释放 测试连续相同窗口无需释放
func (s *ContextEngineE2ESuite) TestKVCacheManager_Release_连续相同窗口不释放() {
	mgr := cecontext.NewKVCacheManager("sess-1")
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("test")}

	// 两次 Release 无 Model，不报错
	err := mgr.Release(s.ctx, window)
	s.NoError(err)
	err = mgr.Release(s.ctx, window)
	s.NoError(err)
}

// ──────────────────────────── CompressContext ────────────────────────────

// TestContextEngine_CompressContext_无处理器 测试无处理器时主动压缩返回 noop
func (s *ContextEngineE2ESuite) TestContextEngine_CompressContext_无处理器() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess)
	s.NoError(err)

	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	})

	result, err := ce.CompressContext(s.ctx, "ctx-1", sess)
	s.NoError(err)
	s.NotNil(result)
	// 无压缩处理器时返回 noop
	s.Equal("noop", result.Result)
}

// TestContextEngine_CompressContext_上下文不存在 测试不存在的上下文返回错误
func (s *ContextEngineE2ESuite) TestContextEngine_CompressContext_上下文不存在() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")

	result, err := ce.CompressContext(s.ctx, "nonexist", sess)
	s.Error(err, "不存在的上下文应返回错误")
	s.Nil(result)
}

// ──────────────────────────── ProcessorStateRecorder ────────────────────────────

// TestProcessorStateRecorder_历史记录 测试压缩历史记录
func (s *ContextEngineE2ESuite) TestProcessorStateRecorder_历史记录() {
	config := ceschema.NewContextEngineConfig()
	ce := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-1")
	tc := newMockTokenCounter()

	mc, err := ce.CreateContext(s.ctx, "ctx-1", sess, iface.WithTokenCounter(tc))
	s.NoError(err)

	// 添加消息
	mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	})

	// SaveState 应包含 compression_history 键
	savedState := mc.SaveState()
	s.Contains(savedState, "compression_history", "SaveState 应包含 compression_history")
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// ptrBool 返回 bool 值的指针
func ptrBool(v bool) *bool { return &v }

// genLongString 生成长字符串用于触发阈值
func genLongString(length int) string {
	result := make([]byte, length)
	for i := range result {
		result[i] = 'a' + byte(i%26)
	}
	return string(result)
}
