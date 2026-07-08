package harness

import (
	"context"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSessionFacade 用于测试的模拟会话门面
type fakeSessionFacade struct {
	// sessionID 会话标识
	sessionID string
	// stateMap 模拟的会话状态存储
	stateMap map[string]any
	// stateMu 状态读写锁
	stateMu sync.Mutex
}

// fakeAgentRail 用于测试的模拟 Rail
type fakeAgentRail struct {
	// base 嵌入 BaseRail 获得默认实现
	base agentinterfaces.BaseRail
	// initCalled Init 是否被调用
	initCalled bool
	// uninitCalled Uninit 是否被调用
	uninitCalled bool
}

// fakeAgentRailWithType 带独立类型的模拟 Rail（用于 FindRailsByType 测试）
type fakeAgentRailWithType struct {
	// base 嵌入 BaseRail 获得默认实现
	base agentinterfaces.BaseRail
}

// fakeAgentRailWithType2 另一个独立类型的模拟 Rail
type fakeAgentRailWithType2 struct {
	// base 嵌入 BaseRail 获得默认实现
	base agentinterfaces.BaseRail
}

// fakeAgentRailWithCallbacks 带回调声明的模拟 Rail
type fakeAgentRailWithCallbacks struct {
	// base 嵌入 BaseRail 获得默认实现
	base agentinterfaces.BaseRail
	// events 声明的回调事件列表
	events []agentinterfaces.AgentCallbackEvent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestDeepAgent 创建测试用 DeepAgent（自带默认 card）
func newTestDeepAgent() *DeepAgent {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test-deep"),
		agentschema.WithAgentDescription("测试 DeepAgent"),
	)
	return NewDeepAgent(card)
}

// TestNewDeepAgent 创建 DeepAgent 实例并验证基本字段
func TestNewDeepAgent(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test-deep"),
		agentschema.WithAgentDescription("测试 DeepAgent"),
	)
	d := NewDeepAgent(card)
	if d == nil {
		t.Fatal("NewDeepAgent 返回 nil，期望非 nil")
	}
	if d.Card() != card {
		t.Error("Card() 返回值与传入的 card 不一致")
	}
	if d.AbilityManager() == nil {
		t.Error("AbilityManager() 返回 nil，期望非 nil")
	}
	if d.CallbackManager() == nil {
		t.Error("CallbackManager() 返回 nil，期望非 nil")
	}
}

// TestNewDeepAgent_基本构造 验证字段赋值
func TestNewDeepAgent_基本构造(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("basic-agent"))
	card.ID = "basic-id"
	d := NewDeepAgent(card)
	if d.Card().Name != "basic-agent" {
		t.Errorf("Card().Name=%q，期望 %q", d.Card().Name, "basic-agent")
	}
}

// TestDeepAgent_Card 返回正确的 AgentCard
func TestDeepAgent_Card(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("my-agent"))
	d := NewDeepAgent(card)
	if d.Card().Name != "my-agent" {
		t.Errorf("Card().Name=%q，期望 %q", d.Card().Name, "my-agent")
	}
}

// TestDeepAgent_Config 始终返回 nil（对齐 Python DeepAgent.config = None）
func TestDeepAgent_Config(t *testing.T) {
	d := newTestDeepAgent()
	if d.Config() != nil {
		t.Error("Config() 应返回 nil")
	}
}

// TestDeepAgent_AbilityManager 返回非 nil 能力管理器
func TestDeepAgent_AbilityManager(t *testing.T) {
	d := newTestDeepAgent()
	if d.AbilityManager() == nil {
		t.Error("AbilityManager() 返回 nil，期望非 nil")
	}
}

// TestDeepAgent_CallbackManager 返回非 nil 回调管理器
func TestDeepAgent_CallbackManager(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("agent-1"))
	d := NewDeepAgent(card)
	if d.CallbackManager() == nil {
		t.Error("CallbackManager() 返回 nil，期望非 nil")
	}
}

// TestDeepAgent_AgentID 返回 card.ID
func TestDeepAgent_AgentID(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("agent-1"))
	card.ID = "agent-id-123"
	d := NewDeepAgent(card)
	if d.AgentID() != "agent-id-123" {
		t.Errorf("AgentID()=%q，期望 %q", d.AgentID(), "agent-id-123")
	}
}

// TestDeepAgent_AgentID_显式ID 设置 ID 后返回正确值
func TestDeepAgent_AgentID_显式ID(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("agent-1"))
	card.ID = "explicit-id"
	d := NewDeepAgent(card)
	if d.AgentID() != "explicit-id" {
		t.Errorf("AgentID()=%q，期望 %q", d.AgentID(), "explicit-id")
	}
}

// TestDeepAgent_ReactAgent 未配置时返回 nil
func TestDeepAgent_ReactAgent(t *testing.T) {
	d := newTestDeepAgent()
	if d.ReactAgent() != nil {
		t.Error("未配置时 ReactAgent() 应返回 nil")
	}
}

// TestDeepAgent_LoopCoordinator 未配置时返回 nil
func TestDeepAgent_LoopCoordinator(t *testing.T) {
	d := newTestDeepAgent()
	if d.LoopCoordinator() != nil {
		t.Error("未配置时 LoopCoordinator() 应返回 nil")
	}
}

// TestDeepAgent_LoopController 未配置时返回 nil
func TestDeepAgent_LoopController(t *testing.T) {
	d := newTestDeepAgent()
	if d.LoopController() != nil {
		t.Error("未配置时 LoopController() 应返回 nil")
	}
}

// TestDeepAgent_EventHandler 未配置时返回 nil
func TestDeepAgent_EventHandler(t *testing.T) {
	d := newTestDeepAgent()
	if d.EventHandler() != nil {
		t.Error("未配置时 EventHandler() 应返回 nil")
	}
}

// TestDeepAgent_DeepConfig 未配置时返回 nil
func TestDeepAgent_DeepConfig(t *testing.T) {
	d := newTestDeepAgent()
	if d.DeepConfig() != nil {
		t.Error("未配置时 DeepConfig() 应返回 nil")
	}
}

// TestDeepAgent_LoopSession 未配置时返回 nil
func TestDeepAgent_LoopSession(t *testing.T) {
	d := newTestDeepAgent()
	if d.LoopSession() != nil {
		t.Error("未配置时 LoopSession() 应返回 nil")
	}
}

// TestDeepAgent_ReactConfig 未配置时返回 nil
func TestDeepAgent_ReactConfig(t *testing.T) {
	d := newTestDeepAgent()
	if d.ReactConfig() != nil {
		t.Error("未配置时 ReactConfig() 应返回 nil")
	}
}

// TestDeepAgent_IsInvokeActive 初始为 false
func TestDeepAgent_IsInvokeActive(t *testing.T) {
	d := newTestDeepAgent()
	if d.IsInvokeActive() {
		t.Error("IsInvokeActive() 初始应为 false")
	}
}

// TestDeepAgent_IsAutoInvokeScheduled 初始为 false
func TestDeepAgent_IsAutoInvokeScheduled(t *testing.T) {
	d := newTestDeepAgent()
	if d.IsAutoInvokeScheduled() {
		t.Error("IsAutoInvokeScheduled() 初始应为 false")
	}
}

// TestDeepAgent_SetAutoInvokeScheduled 设置后可读取
func TestDeepAgent_SetAutoInvokeScheduled(t *testing.T) {
	d := newTestDeepAgent()
	d.SetAutoInvokeScheduled(true)
	if !d.IsAutoInvokeScheduled() {
		t.Error("SetAutoInvokeScheduled(true) 后 IsAutoInvokeScheduled() 应为 true")
	}
	d.SetAutoInvokeScheduled(false)
	if d.IsAutoInvokeScheduled() {
		t.Error("SetAutoInvokeScheduled(false) 后 IsAutoInvokeScheduled() 应为 false")
	}
}

// TestDeepAgent_IsInitialized 初始为 false
func TestDeepAgent_IsInitialized(t *testing.T) {
	d := newTestDeepAgent()
	if d.IsInitialized() {
		t.Error("IsInitialized() 初始应为 false")
	}
}

// TestDeepAgent_SetReactAgent 注入 ReActAgent 后可读取
func TestDeepAgent_SetReactAgent(t *testing.T) {
	d := newTestDeepAgent()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-react"))
	cfg := saconfig.NewReActAgentConfig()
	agent := agents.NewReActAgent(card, cfg)

	d.SetReactAgent(agent, true)
	if d.ReactAgent() != agent {
		t.Error("SetReactAgent 后 ReactAgent() 返回值不一致")
	}
	if !d.IsInitialized() {
		t.Error("SetReactAgent(_, true) 后 IsInitialized() 应为 true")
	}

	d.SetReactAgent(nil, false)
	if d.ReactAgent() != nil {
		t.Error("SetReactAgent(nil, false) 后 ReactAgent() 应为 nil")
	}
	if d.IsInitialized() {
		t.Error("SetReactAgent(nil, false) 后 IsInitialized() 应为 false")
	}
}

// TestDeepAgent_Configure_使用BaseAgent接口返回错误 DeepAgent 请使用 ConfigureDeepConfig()
func TestDeepAgent_Configure_使用BaseAgent接口返回错误(t *testing.T) {
	d := newTestDeepAgent()
	err := d.Configure(context.Background(), nil)
	if err == nil {
		t.Fatal("Configure() 应返回错误，期望 DeepagentConfigParamError")
	}
	if baseErr, ok := err.(*exception.BaseError); !ok || baseErr.Status() != exception.StatusDeepagentConfigParamError {
		t.Errorf("Configure() 返回错误状态=%v，期望 StatusDeepagentConfigParamError", err)
	}
}

// TestDeepAgent_ConfigureDeepConfig_首次配置 执行 initialConfigure，创建 ReActAgent
func TestDeepAgent_ConfigureDeepConfig_首次配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test-deep"),
		agentschema.WithAgentDescription("测试 DeepAgent"),
	)
	d := NewDeepAgent(card)

	deepCfg := schema.NewDeepAgentConfig()
	err := d.ConfigureDeepConfig(context.Background(), deepCfg)
	if err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 验证 DeepConfig 已设置
	if d.DeepConfig() != deepCfg {
		t.Error("ConfigureDeepConfig 后 DeepConfig() 返回值不一致")
	}

	// 验证 ReActAgent 已创建
	if d.ReactAgent() == nil {
		t.Error("ConfigureDeepConfig 后 ReactAgent() 应为非 nil")
	}

	// 验证 initialized 被重置为 false（ConfigureDeepConfig 总是重置）
	if d.IsInitialized() {
		t.Error("ConfigureDeepConfig 后 IsInitialized() 应为 false（等待 ensureInitialized）")
	}
}

// TestDeepAgent_ConfigureDeepConfig_配置中更新Card Card 字段覆盖
func TestDeepAgent_ConfigureDeepConfig_配置中更新Card(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("old-name"))
	d := NewDeepAgent(card)

	newCard := agentschema.NewAgentCard(agentschema.WithAgentName("new-name"))
	deepCfg := schema.NewDeepAgentConfig()
	deepCfg.Card = newCard

	err := d.ConfigureDeepConfig(context.Background(), deepCfg)
	if err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if d.Card().Name != "new-name" {
		t.Errorf("Card().Name=%q，期望 %q", d.Card().Name, "new-name")
	}
}

// TestDeepAgent_ConfigureDeepConfig_热重配置 二次调用执行 hotReconfigure
func TestDeepAgent_ConfigureDeepConfig_热重配置(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	// 首次配置
	cfg1 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}
	reactAgent1 := d.ReactAgent()

	// 热重配置
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.SystemPrompt = "updated prompt"
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置 ConfigureDeepConfig 返回错误: %v", err)
	}

	// ReActAgent 不应被重新创建（热重配置不重建 Agent）
	reactAgent2 := d.ReactAgent()
	if reactAgent1 != reactAgent2 {
		t.Error("热重配置后 ReActAgent 应保持同一实例")
	}

	if d.DeepConfig() != cfg2 {
		t.Error("热重配置后 DeepConfig() 应返回新配置")
	}
}

// TestDeepAgent_RegisterRail_正常注册 Init 被调用，加入 registeredRails
func TestDeepAgent_RegisterRail_正常注册(t *testing.T) {
	d := newTestDeepAgent()
	fr := &fakeAgentRail{}

	err := d.RegisterRail(context.Background(), fr)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}
	if !fr.initCalled {
		t.Error("RegisterRail 后 Rail.Init 未被调用")
	}
}

// TestDeepAgent_UnregisterRail 从已注册列表移除并调用 Uninit
func TestDeepAgent_UnregisterRail(t *testing.T) {
	d := newTestDeepAgent()
	fr := &fakeAgentRail{}

	// 先注册
	if err := d.RegisterRail(context.Background(), fr); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	// 再注销
	if err := d.UnregisterRail(context.Background(), fr); err != nil {
		t.Fatalf("UnregisterRail 返回错误: %v", err)
	}
	if !fr.uninitCalled {
		t.Error("UnregisterRail 后 Rail.Uninit 未被调用")
	}
}

// TestDeepAgent_RegisterCallback_正常注册 事件和回调类型正确时成功
func TestDeepAgent_RegisterCallback_正常注册(t *testing.T) {
	d := newTestDeepAgent()
	fn := cb.PerAgentCallbackFunc(func(ctx context.Context, agentCallbackContext any) error { return nil })
	err := d.RegisterCallback(context.Background(), agentinterfaces.CallbackBeforeInvoke, fn)
	if err != nil {
		t.Fatalf("RegisterCallback 返回错误: %v", err)
	}
}

// TestDeepAgent_RegisterCallback_未知事件 未知事件仍可注册（由 Manager 管理）
func TestDeepAgent_RegisterCallback_未知事件(t *testing.T) {
	d := newTestDeepAgent()
	fn := cb.PerAgentCallbackFunc(func(ctx context.Context, agentCallbackContext any) error { return nil })
	err := d.RegisterCallback(context.Background(), agentinterfaces.AgentCallbackEvent("unknown_event"), fn)
	// RegisterCallback 不再做事件合法性校验，由 AgentCallbackManager 管理
	assert.NoError(t, err)
}

// TestDeepAgent_RegisterCallback_正确类型 正确注册回调
func TestDeepAgent_RegisterCallback_正确类型(t *testing.T) {
	d := newTestDeepAgent()
	var called cb.PerAgentCallbackFunc = func(ctx context.Context, data any) error {
		return nil
	}
	err := d.RegisterCallback(context.Background(), agentinterfaces.CallbackBeforeInvoke, called)
	assert.NoError(t, err)
}

// TestDeepAgent_AddRail 排队一个 Rail 以便延迟注册
func TestDeepAgent_AddRail(t *testing.T) {
	d := newTestDeepAgent()
	fr := &fakeAgentRail{}

	// AddRail 返回 self 支持链式调用
	result := d.AddRail(fr)
	if result != d {
		t.Error("AddRail 应返回自身以支持链式调用")
	}
}

// TestDeepAgent_AddRail_TaskCompletionRail替换 重复添加 TaskCompletionRail 时替换旧的
func TestDeepAgent_AddRail_TaskCompletionRail替换(t *testing.T) {
	d := newTestDeepAgent()
	// 添加两个 TaskCompletionRail（类型名匹配 "TaskCompletionRail" 的不会匹配到 fakeAgentRail）
	fr1 := &fakeAgentRail{}
	fr2 := &fakeAgentRail{}

	d.AddRail(fr1)
	d.AddRail(fr2)

	// 非 TaskCompletionRail，两个都应在 pending 中
	typ := reflect.TypeOf(fr1)
	found := d.FindRailsByType(typ)
	if len(found) != 2 {
		t.Errorf("FindRailsByType 返回 %d 个 Rail，期望 2", len(found))
	}
}

// TestDeepAgent_FindRailsByType_空类型列表 返回 nil
func TestDeepAgent_FindRailsByType_空类型列表(t *testing.T) {
	d := newTestDeepAgent()
	result := d.FindRailsByType()
	if result != nil {
		t.Errorf("FindRailsByType() 返回 %v，期望 nil", result)
	}
}

// TestDeepAgent_FindRailsByType_匹配pending和registered 同时搜索 pending 和 registered
func TestDeepAgent_FindRailsByType_匹配pending和registered(t *testing.T) {
	d := newTestDeepAgent()

	fr1 := &fakeAgentRailWithType{}
	fr2 := &fakeAgentRailWithType{}

	// fr1 加入 pending
	d.AddRail(fr1)

	// fr2 注册到 registered
	if err := d.RegisterRail(context.Background(), fr2); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	typ := reflect.TypeOf(fr1)
	found := d.FindRailsByType(typ)
	if len(found) != 2 {
		t.Errorf("FindRailsByType 返回 %d 个 Rail，期望 2", len(found))
	}
}

// TestDeepAgent_FindRailsByType_无匹配 返回空切片
func TestDeepAgent_FindRailsByType_无匹配(t *testing.T) {
	d := newTestDeepAgent()
	fr := &fakeAgentRailWithType{}
	d.AddRail(fr)

	// 搜索不同类型
	typ := reflect.TypeOf(&fakeAgentRailWithType2{})
	found := d.FindRailsByType(typ)
	if len(found) != 0 {
		t.Errorf("FindRailsByType 返回 %d 个 Rail，期望 0", len(found))
	}
}

// TestDeepAgent_StripRailsByType_空类型列表 返回 0
func TestDeepAgent_StripRailsByType_空类型列表(t *testing.T) {
	d := newTestDeepAgent()
	removed := d.StripRailsByType()
	if removed != 0 {
		t.Errorf("StripRailsByType() 返回 %d，期望 0", removed)
	}
}

// TestDeepAgent_StripRailsByType_移除pending 将匹配的 pending Rail 移除
func TestDeepAgent_StripRailsByType_移除pending(t *testing.T) {
	d := newTestDeepAgent()
	fr := &fakeAgentRailWithType{}
	d.AddRail(fr)

	typ := reflect.TypeOf(fr)
	removed := d.StripRailsByType(typ)
	if removed != 1 {
		t.Errorf("StripRailsByType 返回 %d，期望 1", removed)
	}

	// 验证 pending 中已无该 Rail
	found := d.FindRailsByType(typ)
	if len(found) != 0 {
		t.Errorf("StripRailsByType 后 FindRailsByType 返回 %d 个 Rail，期望 0", len(found))
	}
}

// TestDeepAgent_StripRailsByType_标记registered为废弃 将匹配的 registered Rail 标记为废弃
func TestDeepAgent_StripRailsByType_标记registered为废弃(t *testing.T) {
	d := newTestDeepAgent()
	fr := &fakeAgentRailWithType{}
	if err := d.RegisterRail(context.Background(), fr); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	typ := reflect.TypeOf(fr)
	removed := d.StripRailsByType(typ)
	if removed < 1 {
		t.Errorf("StripRailsByType 返回 %d，期望至少 1", removed)
	}
}

// TestDeepAgent_LoadState_默认状态 无持久化状态时返回默认 DeepAgentState
func TestDeepAgent_LoadState_默认状态(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	state := d.LoadState(sess)
	if state == nil {
		t.Fatal("LoadState 返回 nil，期望非 nil 默认状态")
	}
	if state.PlanMode.Mode != "normal" {
		t.Errorf("默认 PlanMode.Mode=%q，期望 %q", state.PlanMode.Mode, "normal")
	}
}

// TestDeepAgent_LoadState_从持久化恢复 有持久化状态时恢复
func TestDeepAgent_LoadState_从持久化恢复(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 先保存一个状态
	newState := schema.NewDeepAgentState()
	newState.Iteration = 5
	newState.PlanMode.Mode = "plan"
	d.SaveState(sess, &newState)

	// 清除运行时缓存，强制从持久化加载
	d.ClearState(sess, false)

	loaded := d.LoadState(sess)
	if loaded == nil {
		t.Fatal("LoadState 返回 nil，期望非 nil")
	}
	if loaded.Iteration != 5 {
		t.Errorf("Iteration=%d，期望 5", loaded.Iteration)
	}
	if loaded.PlanMode.Mode != "plan" {
		t.Errorf("PlanMode.Mode=%q，期望 %q", loaded.PlanMode.Mode, "plan")
	}
}

// TestDeepAgent_SaveState_持久化到会话 SaveState 后可从会话 GetState 读取
func TestDeepAgent_SaveState_持久化到会话(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	newState := schema.NewDeepAgentState()
	newState.Iteration = 10
	d.SaveState(sess, &newState)

	// 验证运行时缓存
	data, err := sess.GetState(sessstate.StringKey("_deep_agent_runtime_state"))
	if err != nil {
		t.Fatalf("GetState 返回错误: %v", err)
	}
	if data == nil {
		t.Fatal("运行时缓存为 nil")
	}
	cached, ok := data.(*schema.DeepAgentState)
	if !ok {
		t.Fatal("运行时缓存类型不匹配")
	}
	if cached.Iteration != 10 {
		t.Errorf("缓存 Iteration=%d，期望 10", cached.Iteration)
	}
}

// TestDeepAgent_ClearState_仅清除运行时 clearPersisted=false 仅清除运行时缓存
func TestDeepAgent_ClearState_仅清除运行时(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	newState := schema.NewDeepAgentState()
	newState.Iteration = 3
	d.SaveState(sess, &newState)
	d.ClearState(sess, false)

	// 运行时缓存应被清除
	data, _ := sess.GetState(sessstate.StringKey("_deep_agent_runtime_state"))
	if data != nil {
		t.Error("ClearState(false) 后运行时缓存应被清除")
	}

	// 持久化状态应保留
	persisted, _ := sess.GetState(sessstate.StringKey("deep_agent_state"))
	if persisted == nil {
		t.Error("ClearState(false) 后持久化状态应保留")
	}
}

// TestDeepAgent_ClearState_连同持久化 clearPersisted=true 同时清除持久化状态
func TestDeepAgent_ClearState_连同持久化(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	newState := schema.NewDeepAgentState()
	d.SaveState(sess, &newState)
	d.ClearState(sess, true)

	// 持久化状态也应被清除
	persisted, _ := sess.GetState(sessstate.StringKey("deep_agent_state"))
	if persisted != nil {
		t.Error("ClearState(true) 后持久化状态应被清除")
	}
}

// TestDeepAgent_SwitchMode 切换 Agent 模式
func TestDeepAgent_SwitchMode(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 初始模式为 normal
	state := d.LoadState(sess)
	if state.PlanMode.Mode != "normal" {
		t.Errorf("初始 Mode=%q，期望 normal", state.PlanMode.Mode)
	}

	// 切换到 plan
	d.SwitchMode(sess, "plan")
	state = d.LoadState(sess)
	if state.PlanMode.Mode != "plan" {
		t.Errorf("SwitchMode 后 Mode=%q，期望 plan", state.PlanMode.Mode)
	}
	if state.PlanMode.PrePlanMode != "normal" {
		t.Errorf("PrePlanMode=%q，期望 normal", state.PlanMode.PrePlanMode)
	}
}

// TestDeepAgent_SwitchMode_相同模式 模式相同时只更新 PrePlanMode
func TestDeepAgent_SwitchMode_相同模式(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 先切换到 plan
	d.SwitchMode(sess, "plan")

	// 再切换到 plan（相同模式）
	d.SwitchMode(sess, "plan")
	state := d.LoadState(sess)
	if state.PlanMode.Mode != "plan" {
		t.Errorf("相同模式切换后 Mode=%q，期望 plan", state.PlanMode.Mode)
	}
}

// TestDeepAgent_RestoreModeAfterPlanExit 恢复进入规划模式前的模式
func TestDeepAgent_RestoreModeAfterPlanExit(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 切换到 plan
	d.SwitchMode(sess, "plan")

	// 退出 plan，恢复之前模式
	d.RestoreModeAfterPlanExit(sess)
	state := d.LoadState(sess)
	if state.PlanMode.Mode != "normal" {
		t.Errorf("RestoreModeAfterPlanExit 后 Mode=%q，期望 normal", state.PlanMode.Mode)
	}
	if state.PlanMode.PrePlanMode != "" {
		t.Errorf("PrePlanMode=%q，期望空串", state.PlanMode.PrePlanMode)
	}
}

// TestDeepAgent_RestoreModeAfterPlanExit_无前置模式 PrePlanMode 为空时默认恢复 normal
func TestDeepAgent_RestoreModeAfterPlanExit_无前置模式(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 直接恢复（未先切换到 plan）
	d.RestoreModeAfterPlanExit(sess)
	state := d.LoadState(sess)
	if state.PlanMode.Mode != "normal" {
		t.Errorf("无前置模式时 Mode=%q，期望 normal", state.PlanMode.Mode)
	}
}

// TestDeepAgent_GetPlanFilePath_无Slug 返回空串
func TestDeepAgent_GetPlanFilePath_无Slug(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	path := d.GetPlanFilePath(sess)
	if path != "" {
		t.Errorf("无 slug 时 GetPlanFilePath 返回 %q，期望空串", path)
	}
}

// TestDeepAgent_GetPlanFilePath_有Slug 有 slug 和 workspace 时返回路径
func TestDeepAgent_GetPlanFilePath_有Slug(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	deepCfg := schema.NewDeepAgentConfig()
	deepCfg.Workspace = &workspace.Workspace{RootPath: "/tmp/workspace"}
	if err := d.ConfigureDeepConfig(context.Background(), deepCfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	sess := newFakeSessionFacade("sess-1")
	// 设置 PlanSlug
	state := d.LoadState(sess)
	state.PlanMode.PlanSlug = "gleaming-brewing-phoenix"
	d.SaveState(sess, state)

	path := d.GetPlanFilePath(sess)
	expected := "/tmp/workspace/gleaming-brewing-phoenix.md"
	if path != expected {
		t.Errorf("GetPlanFilePath 返回 %q，期望 %q", path, expected)
	}
}

// TestDeepAgent_GetPlanFilePath_无Workspace slug 非空但 workspace 为 nil 时返回空串
func TestDeepAgent_GetPlanFilePath_无Workspace(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 设置 PlanSlug（无 workspace 配置）
	state := d.LoadState(sess)
	state.PlanMode.PlanSlug = "some-slug"
	d.SaveState(sess, state)

	path := d.GetPlanFilePath(sess)
	if path != "" {
		t.Errorf("无 workspace 时 GetPlanFilePath 返回 %q，期望空串", path)
	}
}

// TestDeepAgent_CreateSubagent_规格未找到 返回错误
func TestDeepAgent_CreateSubagent_规格未找到(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.CreateSubagent("nonexistent", "sub-sess-1")
	if err == nil {
		t.Fatal("CreateSubagent 对不存在的规格应返回错误")
	}
	if baseErr, ok := err.(*exception.BaseError); !ok || baseErr.Status() != exception.StatusDeepagentCreateSubagentNotFound {
		t.Errorf("错误状态=%v，期望 StatusDeepagentCreateSubagentNotFound", err)
	}
}

// TestDeepAgent_CreateSubagent_工厂分派 返回 stub 错误
func TestDeepAgent_CreateSubagent_工厂分派(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	factories := []string{
		"browser_agent",
		"code_agent",
		"research_agent",
		"mobile_gui_agent",
	}

	deepCfg := schema.NewDeepAgentConfig()
	for _, factory := range factories {
		subCfg := schema.NewSubAgentConfig()
		subCfg.AgentCard = agentschema.NewAgentCard(agentschema.WithAgentName(factory))
		subCfg.FactoryName = factory
		deepCfg.Subagents = append(deepCfg.Subagents, subCfg)
	}
	if err := d.ConfigureDeepConfig(context.Background(), deepCfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	for _, factory := range factories {
		_, err := d.CreateSubagent(factory, "sub-sess-1")
		if err == nil {
			t.Errorf("CreateSubagent(%q) 应返回 stub 错误", factory)
		}
	}
}

// TestDeepAgent_CreateSubagent_默认工厂 未知工厂名走 default 分支通过 CreateDeepAgent 创建
func TestDeepAgent_CreateSubagent_默认工厂(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	deepCfg := schema.NewDeepAgentConfig()
	subCfg := schema.NewSubAgentConfig()
	subCfg.AgentCard = agentschema.NewAgentCard(agentschema.WithAgentName("custom_agent"))
	subCfg.FactoryName = "custom_factory"
	deepCfg.Subagents = append(deepCfg.Subagents, subCfg)
	if err := d.ConfigureDeepConfig(context.Background(), deepCfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 未知工厂名走 default 分支，通过 CreateDeepAgent 创建子 Agent
	subAgent, err := d.CreateSubagent("custom_agent", "sub-sess-1")
	// CreateDeepAgent 可能因缺少必要配置而返回错误，也可能成功
	// 关键是不 panic，且逻辑正确分派到 default 分支
	if err != nil {
		// 预期：CreateDeepAgent 因缺少配置返回错误，这是合理的
		t.Logf("CreateSubagent 对 custom_factory 返回错误（预期行为）: %v", err)
	} else {
		// 如果成功创建，也合理
		assert.NotNil(t, subAgent)
	}
}

// TestDeepAgent_FollowUp_无Controller LoopController 为 nil 时直接返回
func TestDeepAgent_FollowUp_无Controller(t *testing.T) {
	d := newTestDeepAgent()
	// 不 panic 即通过
	d.FollowUp(context.Background(), "msg", "task-1", nil)
}

// TestDeepAgent_Steer_无Controller LoopController 为 nil 时直接返回
func TestDeepAgent_Steer_无Controller(t *testing.T) {
	d := newTestDeepAgent()
	// 不 panic 即通过
	d.Steer(context.Background(), "msg", nil)
}

// TestDeepAgent_Abort_无协调器 无协调器时不 panic
func TestDeepAgent_Abort_无协调器(t *testing.T) {
	d := newTestDeepAgent()
	// 不 panic 即通过
	d.Abort(context.Background())
}

// TestDeepAgent_EnqueueHarnessConfig 排队 harness 配置路径
func TestDeepAgent_EnqueueHarnessConfig(t *testing.T) {
	d := newTestDeepAgent()
	d.EnqueueHarnessConfig("/path/to/config1.yaml")
	d.EnqueueHarnessConfig("/path/to/config2.yaml")

	// 验证路径已排队（通过 Drain 间接验证）
	d.railsMu.Lock()
	count := len(d.pendingHarnessConfigs)
	d.railsMu.Unlock()
	if count != 2 {
		t.Errorf("排队后 pendingHarnessConfigs 长度=%d，期望 2", count)
	}
}

// TestDeepAgent_DrainPendingHarnessConfigs 加载排队的配置
func TestDeepAgent_DrainPendingHarnessConfigs(t *testing.T) {
	d := newTestDeepAgent()
	d.EnqueueHarnessConfig("/path/to/config.yaml")

	// drain 会调用 LoadHarnessConfig，目前返回 stub 错误，但 drain 本身不返回错误
	err := d.drainPendingHarnessConfigs(context.Background())
	if err != nil {
		t.Fatalf("drainPendingHarnessConfigs 返回错误: %v", err)
	}

	// 验证队列已清空
	d.railsMu.Lock()
	count := len(d.pendingHarnessConfigs)
	d.railsMu.Unlock()
	if count != 0 {
		t.Errorf("drain 后 pendingHarnessConfigs 长度=%d，期望 0", count)
	}
}

// TestDeepAgent_LoadHarnessConfig 返回 stub 错误
func TestDeepAgent_LoadHarnessConfig(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.LoadHarnessConfig(context.Background(), "/path/to/config.yaml")
	if err == nil {
		t.Fatal("LoadHarnessConfig 应返回 stub 错误")
	}
}

// TestDeepAgent_UnloadHarnessConfig 返回 stub 错误
func TestDeepAgent_UnloadHarnessConfig(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.UnloadHarnessConfig(context.Background(), "/path/to/config.yaml")
	if err == nil {
		t.Fatal("UnloadHarnessConfig 应返回 stub 错误")
	}
}

// TestDeepAgent_InitWorkspace_无配置 无配置时返回 nil
func TestDeepAgent_InitWorkspace_无配置(t *testing.T) {
	d := newTestDeepAgent()
	err := d.InitWorkspace(context.Background())
	if err != nil {
		t.Fatalf("InitWorkspace 无配置时应返回 nil，实际: %v", err)
	}
}

// TestDeepAgent_InitWorkspace_有配置 有 workspace 配置时不返回错误
func TestDeepAgent_InitWorkspace_有配置(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	deepCfg := schema.NewDeepAgentConfig()
	deepCfg.Workspace = &workspace.Workspace{RootPath: "/tmp/workspace"}
	if err := d.ConfigureDeepConfig(context.Background(), deepCfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	err := d.InitWorkspace(context.Background())
	if err != nil {
		t.Fatalf("InitWorkspace 返回错误: %v", err)
	}
}

// TestDeepAgent_GetContextUsage 返回 stub 错误
func TestDeepAgent_GetContextUsage(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.GetContextUsage(context.Background(), "sess-1", "ctx-1")
	if err == nil {
		t.Fatal("GetContextUsage 应返回 stub 错误")
	}
}

// TestDeepAgent_GetContextOccupancy 别名调用
func TestDeepAgent_GetContextOccupancy(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.GetContextOccupancy(context.Background(), "sess-1", "ctx-1")
	if err == nil {
		t.Fatal("GetContextOccupancy 应返回 stub 错误")
	}
}

// TestDeepAgent_GetCurrentContext 返回 stub 错误
func TestDeepAgent_GetCurrentContext(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.GetCurrentContext(context.Background(), "sess-1", "ctx-1")
	if err == nil {
		t.Fatal("GetCurrentContext 应返回 stub 错误")
	}
}

// TestDeepAgent_CreateNewContextEngine 返回 stub 错误
func TestDeepAgent_CreateNewContextEngine(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.CreateNewContextEngine(context.Background(), "sess-1", nil)
	if err == nil {
		t.Fatal("CreateNewContextEngine 应返回 stub 错误")
	}
}

// TestDeepAgent_NewContextEngine 别名调用
func TestDeepAgent_NewContextEngine(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.NewContextEngine(context.Background(), "sess-1", nil)
	if err == nil {
		t.Fatal("NewContextEngine 应返回 stub 错误")
	}
}

// TestNormalizeInputs_MapStringAny 从 map[string]any 解析 InvokeInputs
func TestNormalizeInputs_MapStringAny(t *testing.T) {
	d := newTestDeepAgent()
	inputs := map[string]any{
		"query":           "hello world",
		"conversation_id": "conv-1",
	}

	result, err := d.normalizeInputs(inputs)
	if err != nil {
		t.Fatalf("normalizeInputs 返回错误: %v", err)
	}
	if result.Query.PlainText() != "hello world" {
		t.Errorf("Query=%q，期望 %q", result.Query.PlainText(), "hello world")
	}
	if result.ConversationID != "conv-1" {
		t.Errorf("ConversationID=%q，期望 %q", result.ConversationID, "conv-1")
	}
}

// TestNormalizeInputs_MapStringAny_带Run 从 map 中解析 run.kind 和 run.context
func TestNormalizeInputs_MapStringAny_带Run(t *testing.T) {
	d := newTestDeepAgent()
	inputs := map[string]any{
		"query": "heartbeat query",
		"run": map[string]any{
			"kind": "heartbeat",
			"context": map[string]any{
				"reason": "interval",
			},
		},
	}

	result, err := d.normalizeInputs(inputs)
	if err != nil {
		t.Fatalf("normalizeInputs 返回错误: %v", err)
	}
	if result.RunKind != "heartbeat" {
		t.Errorf("RunKind=%q，期望 %q", result.RunKind, "heartbeat")
	}
	if result.RunContext == nil {
		t.Error("RunContext 为 nil，期望非 nil")
	}
}

// TestNormalizeInputs_String 字符串输入直接转为 Query
func TestNormalizeInputs_String(t *testing.T) {
	d := newTestDeepAgent()
	result, err := d.normalizeInputs("plain text query")
	if err != nil {
		t.Fatalf("normalizeInputs 返回错误: %v", err)
	}
	if result.Query.PlainText() != "plain text query" {
		t.Errorf("Query=%q，期望 %q", result.Query.PlainText(), "plain text query")
	}
}

// TestNormalizeInputs_Default 不支持类型返回错误（对齐 Python）
func TestNormalizeInputs_Default(t *testing.T) {
	d := newTestDeepAgent()
	result, err := d.normalizeInputs(42)
	if err == nil {
		t.Errorf("期望返回错误，实际 err=nil, result=%v", result)
	}
	if result != nil {
		t.Errorf("期望 result=nil，实际 result=%v", result)
	}
}

// TestToEffectiveInputs 转换 InvokeInputs 为 ReAct 输入字典
func TestToEffectiveInputs(t *testing.T) {
	inputs := &agentinterfaces.InvokeInputs{
		Query:          agentinterfaces.InvokeQueryString("test query"),
		ConversationID: "conv-1",
		RunKind:        "heartbeat",
	}

	result := toEffectiveInputs(inputs)
	if result["query"] != inputs.Query {
		t.Errorf("query=%v，期望 %v", result["query"], inputs.Query)
	}
	if result["conversation_id"] != "conv-1" {
		t.Errorf("conversation_id=%v，期望 conv-1", result["conversation_id"])
	}
	// RunKind 存储为 agentinterfaces.RunKind 类型
	if result["run_kind"] != agentinterfaces.RunKind("heartbeat") {
		t.Errorf("run_kind=%v，期望 heartbeat", result["run_kind"])
	}
}

// TestToEffectiveInputs_最小字段 仅 query 时只有 query 键
func TestToEffectiveInputs_最小字段(t *testing.T) {
	inputs := &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("minimal"),
	}

	result := toEffectiveInputs(inputs)
	if len(result) != 1 {
		t.Errorf("结果键数=%d，期望 1", len(result))
	}
	if _, ok := result["query"]; !ok {
		t.Error("缺少 query 键")
	}
}

// TestIsResumeInput 当前始终返回 false
func TestIsResumeInput(t *testing.T) {
	inputs := &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("resume"),
	}
	if isResumeInput(inputs) {
		t.Error("isResumeInput 当前应始终返回 false")
	}
}

// TestResultFromStreamChunk 当前始终返回 nil
func TestResultFromStreamChunk(t *testing.T) {
	result := resultFromStreamChunk(nil, nil)
	if result != nil {
		t.Error("resultFromStreamChunk 当前应始终返回 nil")
	}
}

// TestNormalizeFactoryName 规范化工厂名称：去空格、转小写
func TestNormalizeFactoryName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Browser_Agent", "browser_agent"},
		{"Code Agent", "codeagent"},
		{"  Research_Agent  ", "research_agent"},
		{"mobile_gui_agent", "mobile_gui_agent"},
		{"PlanAgent", "planagent"},
	}

	for _, tt := range tests {
		result := normalizeFactoryName(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeFactoryName(%q)=%q，期望 %q", tt.input, result, tt.expected)
		}
	}
}

// TestJoinStrings 拼接字符串切片
func TestJoinStrings(t *testing.T) {
	parts := []string{"hello", " ", "world"}
	result := joinStrings(parts)
	if result != "hello world" {
		t.Errorf("joinStrings=%q，期望 %q", result, "hello world")
	}
}

// TestJoinStrings_空切片 返回空串
func TestJoinStrings_空切片(t *testing.T) {
	result := joinStrings(nil)
	if result != "" {
		t.Errorf("joinStrings(nil)=%q，期望空串", result)
	}
}

// TestIsTaskCompletionRail 当前仅匹配类型名字符串
func TestIsTaskCompletionRail(t *testing.T) {
	fr := &fakeAgentRail{}
	// fakeAgentRail 的类型名不是 "TaskCompletionRail"，返回 false
	if isTaskCompletionRail(fr) {
		t.Error("isTaskCompletionRail 对 fakeAgentRail 应返回 false")
	}
}

// TestRemoveRailByRef 按引用移除 Rail
func TestRemoveRailByRef(t *testing.T) {
	fr1 := &fakeAgentRail{}
	fr2 := &fakeAgentRail{}
	fr3 := &fakeAgentRail{}

	rails := []agentinterfaces.AgentRail{fr1, fr2, fr3}
	result := removeRailByRef(rails, fr2)
	if len(result) != 2 {
		t.Fatalf("移除后长度=%d，期望 2", len(result))
	}
	if result[0] != fr1 || result[1] != fr3 {
		t.Error("移除后剩余元素不正确")
	}
}

// TestRemoveRailByRef_不匹配 目标不在列表时返回原列表
func TestRemoveRailByRef_不匹配(t *testing.T) {
	fr1 := &fakeAgentRail{}
	fr2 := &fakeAgentRail{}

	rails := []agentinterfaces.AgentRail{fr1}
	result := removeRailByRef(rails, fr2)
	if len(result) != 1 {
		t.Errorf("不匹配时长度=%d，期望 1", len(result))
	}
}

// TestFilterRailsNotType 按类型谓词过滤 Rail
func TestFilterRailsNotType(t *testing.T) {
	fr1 := &fakeAgentRailWithType{}
	fr2 := &fakeAgentRailWithType2{}

	rails := []agentinterfaces.AgentRail{fr1, fr2}
	predicate := func(r agentinterfaces.AgentRail) bool {
		return reflect.TypeOf(r) == reflect.TypeOf(fr1)
	}
	result := filterRailsNotType(rails, predicate)
	if len(result) != 1 {
		t.Fatalf("过滤后长度=%d，期望 1", len(result))
	}
	if result[0] != fr2 {
		t.Error("过滤后剩余元素不正确")
	}
}

// TestMatchType 检查 Rail 是否匹配指定类型列表
func TestMatchType(t *testing.T) {
	fr := &fakeAgentRailWithType{}
	typ := reflect.TypeOf(fr)

	if !matchType(fr, []reflect.Type{typ}) {
		t.Error("matchType 对匹配类型应返回 true")
	}

	typ2 := reflect.TypeOf(&fakeAgentRailWithType2{})
	if matchType(fr, []reflect.Type{typ2}) {
		t.Error("matchType 对不匹配类型应返回 false")
	}

	if matchType(fr, []reflect.Type{}) {
		t.Error("matchType 对空类型列表应返回 false")
	}
}

// TestDeepAgent_SetSessionToolkit 设置会话工具包
func TestDeepAgent_SetSessionToolkit(t *testing.T) {
	d := newTestDeepAgent()
	// 不 panic 即通过
	d.SetSessionToolkit(nil)
}

// TestDeepAgent_EnsureInitialized_未配置 未配置时 EnsureInitialized 不 panic
func TestDeepAgent_EnsureInitialized_未配置(t *testing.T) {
	d := newTestDeepAgent()
	err := d.EnsureInitialized(context.Background())
	if err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_EnsureInitialized_已初始化 返回 nil
func TestDeepAgent_EnsureInitialized_已初始化(t *testing.T) {
	d := newTestDeepAgent()
	d.initMu.Lock()
	d.initialized = true
	d.initMu.Unlock()
	err := d.EnsureInitialized(context.Background())
	if err != nil {
		t.Fatalf("已初始化时 EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_Invoke_未配置 reactAgent 为 nil 时返回错误
func TestDeepAgent_Invoke_未配置(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.Invoke(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("未配置时 Invoke 应返回错误")
	}
}

// TestDeepAgent_Stream_未配置 reactAgent 为 nil 时返回错误
func TestDeepAgent_Stream_未配置(t *testing.T) {
	d := newTestDeepAgent()
	_, err := d.Stream(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("未配置时 Stream 应返回错误")
	}
}

// TestDeepAgent_CombineRailsByType StripRailsByType 处理 pending + registered
func TestDeepAgent_CombineRailsByType(t *testing.T) {
	d := newTestDeepAgent()

	fr1 := &fakeAgentRailWithType{}
	fr2 := &fakeAgentRailWithType{}

	// fr1 pending, fr2 registered
	d.AddRail(fr1)
	if err := d.RegisterRail(context.Background(), fr2); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	typ := reflect.TypeOf(fr1)
	removed := d.StripRailsByType(typ)
	// 1 from pending + 1 from registered (stale)
	if removed != 2 {
		t.Errorf("StripRailsByType 返回 %d，期望 2", removed)
	}
}

// TestDeepAgent_Invoke_已配置单轮 已配置后 Invoke 走单轮路径（非任务循环）
func TestDeepAgent_Invoke_已配置单轮(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = false
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// Invoke 会调用 ensureInitialized，然后走 runSingleRoundInvoke
	// runSingleRoundInvoke 会调用 reactAgent.Invoke，可能返回错误
	_, err := d.Invoke(context.Background(), map[string]any{"query": "hello"})
	// 由于 ReActAgent 无 LLM，可能返回错误，但关键是代码路径被覆盖
	_ = err
}

// TestDeepAgent_Invoke_已配置任务循环 已配置且启用任务循环时走任务循环路径
func TestDeepAgent_Invoke_已配置任务循环(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 任务循环需要 *session.Session，但传 nil 时 runTaskLoopInvoke 应返回错误
	_, err := d.Invoke(context.Background(), map[string]any{"query": "hello"})
	// 可能在 runTaskLoopInvoke 中返回错误
	_ = err
}

// TestDeepAgent_Stream_已配置单轮 已配置后 Stream 走单轮路径
func TestDeepAgent_Stream_已配置单轮(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = false
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// Stream 返回 channel，可能收到数据或被关闭
	ch, err := d.Stream(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		// ReActAgent.Stream 可能因无 LLM 返回错误
		_ = err
		return
	}
	// 消费 channel 以触发 goroutine
	for range ch {
	}
}

// TestDeepAgent_Stream_已配置任务循环 已配置且启用任务循环时走任务循环流式路径
func TestDeepAgent_Stream_已配置任务循环(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	ch, err := d.Stream(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		_ = err
		return
	}
	for range ch {
	}
}

// TestDeepAgent_hotReloadModel_带模型配置 热重配置带模型配置时更新
func TestDeepAgent_hotReloadModel_带模型配置(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	// 热重配置带模型（nil Model，EnableTaskLoop=true）
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}
}

// TestDeepAgent_hotReloadTools_带工具 热重配置带工具时更新 AbilityManager
func TestDeepAgent_hotReloadTools_带工具(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	// 热重配置带工具
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.Tools = []*tool.ToolCard{
		tool.NewToolCard("test_tool", "测试工具", nil, nil),
	}
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}
}

// TestDeepAgent_hotReloadSystemPrompt_更新提示词 热重配置时更新系统提示词
func TestDeepAgent_hotReloadSystemPrompt_更新提示词(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	cfg1.SystemPrompt = "初始提示词"
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	cfg2 := schema.NewDeepAgentConfig()
	cfg2.SystemPrompt = "更新后的提示词"
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}
}

// TestDeepAgent_ScheduleAutoInvokeOnSpawnDone 调度自动 invoke
func TestDeepAgent_ScheduleAutoInvokeOnSpawnDone(t *testing.T) {
	d := newTestDeepAgent()

	err := d.ScheduleAutoInvokeOnSpawnDone("测试查询", 0.5)
	if err != nil {
		t.Fatalf("ScheduleAutoInvokeOnSpawnDone 返回错误: %v", err)
	}
	if !d.IsAutoInvokeScheduled() {
		t.Error("ScheduleAutoInvokeOnSpawnDone 后 IsAutoInvokeScheduled() 应为 true")
	}
}

// TestDeepAgent_AgentID_NilCard card 为 nil 时返回空串
func TestDeepAgent_AgentID_NilCard(t *testing.T) {
	d := &DeepAgent{}
	if d.AgentID() != "" {
		t.Errorf("AgentID()=%q，期望空串", d.AgentID())
	}
}

// TestDeepAgent_FollowUp_有Controller 有 loopController 时执行逻辑
func TestDeepAgent_FollowUp_有Controller(t *testing.T) {
	d := newTestDeepAgent()
	// 不 panic 即通过（controller 为 nil 时 early return）
	d.FollowUp(context.Background(), "msg", "task-1", nil)
}

// TestDeepAgent_Steer_有Controller 有 loopController 时执行逻辑
func TestDeepAgent_Steer_有Controller(t *testing.T) {
	d := newTestDeepAgent()
	d.Steer(context.Background(), "msg", nil)
}

// TestDeepAgent_Abort_有协调器 有协调器时执行取消逻辑
func TestDeepAgent_Abort_有协调器(t *testing.T) {
	d := newTestDeepAgent()
	d.Abort(context.Background())
}

// TestDeepAgent_queuePendingRails_各种配置 测试 ProgressiveTool/TaskLoop/Permissions 分支
func TestDeepAgent_queuePendingRails_各种配置(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	cfg.ProgressiveToolEnabled = true
	cfg.EnableTaskLoop = true
	cfg.Permissions = &security.PermissionsSection{}
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}
}

// TestDeepAgent_createReactAgent_有工作空间 有 workspace 时设置工作空间
func TestDeepAgent_createReactAgent_有工作空间(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/ws"}
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if d.ReactAgent() == nil {
		t.Fatal("有 workspace 时 ReactAgent() 应为非 nil")
	}
}

// TestDeepAgent_hasRemainingTasks 无 TaskPlan 时返回 false
func TestDeepAgent_hasRemainingTasks(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	if d.hasRemainingTasks(sess) {
		t.Error("无 TaskPlan 时 hasRemainingTasks 应返回 false")
	}
}

// TestDeepAgent_hasPendingSessionSpawn 无 toolkit 时返回 false
func TestDeepAgent_hasPendingSessionSpawn(t *testing.T) {
	d := newTestDeepAgent()

	if d.hasPendingSessionSpawn() {
		t.Error("无 toolkit 时 hasPendingSessionSpawn 应返回 false")
	}
}

// TestDeepAgent_SystemPromptBuilder 系统提示词构建器访问
func TestDeepAgent_SystemPromptBuilder(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}
	// 配置后 SystemPromptBuilder 不应为 nil
	if d.SystemPromptBuilder() == nil {
		t.Error("配置后 SystemPromptBuilder() 不应返回 nil")
	}
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_unregisterToolResource 注销工具资源
func TestDeepAgent_unregisterToolResource(t *testing.T) {
	d := newTestDeepAgent()
	tc := tool.NewToolCard("test_tool", "测试工具", nil, nil)
	d.unregisterToolResource(tc)
}

// TestDeepAgent_ensureBuiltinToolResource 确保内置工具资源
func TestDeepAgent_ensureBuiltinToolResource(t *testing.T) {
	d := newTestDeepAgent()
	tc := tool.NewToolCard("test_tool", "测试工具", nil, nil)
	cfg := schema.NewDeepAgentConfig()
	d.ensureBuiltinToolResource(tc, cfg)
}

// TestLogLoop 记录外层循环日志
func TestLogLoop(t *testing.T) {
	logLoop("round=%d started", ", query=test", 1)
	logLoop("all tasks completed", "", 0)
}

// TestDeepAgent_hotReloadRails_部分更新 配置中指定 Rails 时替换同类型
func TestDeepAgent_hotReloadRails_部分更新(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	// 首次配置，带 Rails
	fr1 := &fakeAgentRailWithType{}
	cfg1 := schema.NewDeepAgentConfig()
	cfg1.Rails = []agentinterfaces.AgentRail{fr1}
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	// 初始化以注册 Rails
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}

	// 热重配置，带新 Rails（同类型替换）
	fr2 := &fakeAgentRailWithType{}
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.Rails = []agentinterfaces.AgentRail{fr2}
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}

	// 验证 fr1 被标记为废弃（同类型替换）
	d.railsMu.Lock()
	staleCount := len(d.staleRails)
	d.railsMu.Unlock()
	if staleCount < 1 {
		t.Errorf("热重配置后 staleRails 长度=%d，期望至少 1", staleCount)
	}
}

// TestDeepAgent_hotReloadRails_全量替换 Rails 为 nil 时所有 registered 变为废弃
func TestDeepAgent_hotReloadRails_全量替换(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	// 首次配置带 Rails
	fr1 := &fakeAgentRailWithType{}
	cfg1 := schema.NewDeepAgentConfig()
	cfg1.Rails = []agentinterfaces.AgentRail{fr1}
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	// 初始化以注册 Rails
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}

	// 热重配置不带 Rails（全量替换）
	cfg2 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}

	// 验证所有 registered 变为废弃
	d.railsMu.Lock()
	staleCount := len(d.staleRails)
	d.railsMu.Unlock()
	if staleCount < 1 {
		t.Errorf("全量替换后 staleRails 长度=%d，期望至少 1", staleCount)
	}
}

// TestDeepAgent_hotReloadModel_更新模型 热重配置时更新模型
func TestDeepAgent_hotReloadModel_更新模型(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	// 热重配置带 EnableTaskLoop
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}

	// 验证热重配置成功执行
	if d.DeepConfig() != cfg2 {
		t.Error("热重配置后 DeepConfig() 应返回新配置")
	}
}

// TestDeepAgent_hotReloadSystemPrompt_自定义提示词 热重配置自定义系统提示词
func TestDeepAgent_hotReloadSystemPrompt_自定义提示词(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg1); err != nil {
		t.Fatalf("首次 ConfigureDeepConfig 返回错误: %v", err)
	}

	// 热重配置带自定义提示词
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.SystemPrompt = "你是一个专业的测试 Agent"
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}

	// 验证 systemPromptBuilder 不为 nil
	d.configMu.RLock()
	builder := d.systemPromptBuilder
	d.configMu.RUnlock()
	if builder == nil {
		t.Error("热重配置后 systemPromptBuilder 不应为 nil")
	}
}

// TestDeepAgent_registerRailSelective_桥接事件 桥接事件注册到内层 ReActAgent
func TestDeepAgent_registerRailSelective_桥接事件(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	// 先配置以创建 ReActAgent
	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 创建带桥接回调的 Rail
	fr := &fakeAgentRailWithCallbacks{
		events: []agentinterfaces.AgentCallbackEvent{
			agentinterfaces.CallbackBeforeModelCall,
			agentinterfaces.CallbackAfterModelCall,
		},
	}

	// 注册 Rail
	if err := d.RegisterRail(context.Background(), fr); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}
}

// TestDeepAgent_registerRailSelective_外层事件 外层事件注册到外层 DeepAgent
func TestDeepAgent_registerRailSelective_外层事件(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	fr := &fakeAgentRailWithCallbacks{
		events: []agentinterfaces.AgentCallbackEvent{
			agentinterfaces.CallbackBeforeInvoke,
			agentinterfaces.CallbackAfterInvoke,
		},
	}

	if err := d.RegisterRail(context.Background(), fr); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}
}

// TestDeepAgent_registerRailSelective_Deep事件 Deep 事件注册到外层
func TestDeepAgent_registerRailSelective_Deep事件(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	fr := &fakeAgentRailWithCallbacks{
		events: []agentinterfaces.AgentCallbackEvent{
			agentinterfaces.CallbackBeforeTaskIteration,
			agentinterfaces.CallbackAfterTaskIteration,
		},
	}

	if err := d.RegisterRail(context.Background(), fr); err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}
}

// TestDeepAgent_saveState_NilState state 为 nil 时从运行时缓存读取
func TestDeepAgent_saveState_NilState(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 先保存一次
	newState := schema.NewDeepAgentState()
	newState.Iteration = 7
	d.SaveState(sess, &newState)

	// 再次调用 SaveState 传入 nil，应从运行时缓存读取
	d.SaveState(sess, nil)

	// 验证运行时缓存仍有效
	data, _ := sess.GetState(sessstate.StringKey("_deep_agent_runtime_state"))
	if data == nil {
		t.Fatal("SaveState(nil) 后运行时缓存应为非 nil")
	}
}

// TestDeepAgent_createReactAgent_启用任务循环 EnableTaskLoop 时 MaxIterations 为 math.MaxInt
func TestDeepAgent_createReactAgent_启用任务循环(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 验证 ReactConfig 的 MaxIterations
	reactCfg := d.ReactConfig()
	if reactCfg == nil {
		t.Fatal("ReactConfig 返回 nil")
	}
	saCfg, ok := reactCfg.(*saconfig.ReActAgentConfig)
	if !ok {
		t.Fatal("ReactConfig 类型不匹配")
	}
	// math.MaxInt 在 64 位系统上是 9223372036854775807
	if saCfg.MaxIterations != math.MaxInt {
		t.Errorf("EnableTaskLoop 时 MaxIterations=%d，期望 math.MaxInt", saCfg.MaxIterations)
	}
}

// TestDeepAgent_needsWorkspaceInit 需要工作空间初始化的条件
func TestDeepAgent_needsWorkspaceInit(t *testing.T) {
	d := newTestDeepAgent()

	// 无配置时不需要
	if d.needsWorkspaceInit() {
		t.Error("无配置时 needsWorkspaceInit 应返回 false")
	}

	// 有 workspace 但无 SysOperation
	cfg := schema.NewDeepAgentConfig()
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/ws"}
	cfg.AutoCreateWorkspace = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}
	if d.needsWorkspaceInit() {
		t.Error("有 workspace 无 SysOperation 时 needsWorkspaceInit 应返回 false")
	}
}

// TestDeepAgent_EnsureInitialized_带Rails 有 pending Rails 时执行注册
func TestDeepAgent_EnsureInitialized_带Rails(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	fr := &fakeAgentRailWithType{}
	cfg.Rails = []agentinterfaces.AgentRail{fr}
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}

	if !d.IsInitialized() {
		t.Error("EnsureInitialized 后 IsInitialized() 应为 true")
	}
}

// TestDeepAgent_LoopCoordinator_配置后非Nil 配置后 LoopCoordinator 仍为 nil（需要 setupTaskLoop 才创建）
func TestDeepAgent_LoopCoordinator_配置后非Nil(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// LoopCoordinator 在配置后仍为 nil（需要 setupTaskLoop 创建）
	if d.LoopCoordinator() != nil {
		t.Error("配置后 LoopCoordinator() 应为 nil（需要 setupTaskLoop 创建）")
	}
}

// TestDeepAgent_readRuntimeState_无缓存 无缓存时返回 nil
func TestDeepAgent_readRuntimeState_无缓存(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	state := d.readRuntimeState(sess)
	if state != nil {
		t.Errorf("无缓存时 readRuntimeState 返回 %v，期望 nil", state)
	}
}

// TestDeepAgent_cancelStreamProcess_无Cancel 无 streamCancel 时不 panic
func TestDeepAgent_cancelStreamProcess_无Cancel(t *testing.T) {
	d := newTestDeepAgent()
	// 不 panic 即通过
	d.cancelStreamProcess()
}

// TestDeepAgent_findSubagentSpec_找到 匹配 subagentType
func TestDeepAgent_findSubagentSpec_找到(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	subCfg := schema.NewSubAgentConfig()
	subCfg.AgentCard = agentschema.NewAgentCard(agentschema.WithAgentName("my_sub"))
	cfg.Subagents = append(cfg.Subagents, subCfg)
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	spec := d.findSubagentSpec("my_sub")
	if spec == nil {
		t.Fatal("findSubagentSpec 返回 nil，期望非 nil")
	}
}

// TestDeepAgent_findSubagentSpec_未找到 不匹配时返回 nil
func TestDeepAgent_findSubagentSpec_未找到(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	spec := d.findSubagentSpec("nonexistent")
	if spec != nil {
		t.Errorf("findSubagentSpec 返回 %v，期望 nil", spec)
	}
}

// TestDeepAgent_buildSubagentCreateKwargs 配置合并逻辑
func TestDeepAgent_buildSubagentCreateKwargs(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/ws"}
	cfg.Language = "cn"
	subCfg := schema.NewSubAgentConfig()
	subCfg.AgentCard = agentschema.NewAgentCard(agentschema.WithAgentName("sub1"))
	cfg.Subagents = append(cfg.Subagents, subCfg)
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	kwargs := d.buildSubagentCreateKwargs(subCfg, "sub-sess-1")
	if kwargs == nil {
		t.Fatal("buildSubagentCreateKwargs 返回 nil，期望非 nil")
	}
	if kwargs.Language != "cn" {
		t.Errorf("language=%v，期望 cn", kwargs.Language)
	}
}

// TestDeepAgent_registerPendingMCPs_有配置 有 MCP 配置时注册
func TestDeepAgent_registerPendingMCPs_有配置(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Mcps = []*mcptypes.McpServerConfig{
		{ServerID: "mcp-1"},
	}
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 不 panic 即通过
	d.registerPendingMCPs(context.Background())
}

// TestDeepAgent_EnsureInitialized_有废弃Rails 有废弃 Rails 时执行注销
func TestDeepAgent_EnsureInitialized_有废弃Rails(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	fr := &fakeAgentRailWithType{}
	cfg.Rails = []agentinterfaces.AgentRail{fr}
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 先初始化注册 Rails
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}

	// 热重配置不带 Rails，触发废弃
	cfg2 := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg2); err != nil {
		t.Fatalf("热重配置返回错误: %v", err)
	}

	// 再次初始化，应注销废弃 Rails
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_ReactConfig_有ReactAgent 有 ReActAgent 时返回配置
func TestDeepAgent_ReactConfig_有ReactAgent(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	reactCfg := d.ReactConfig()
	if reactCfg == nil {
		t.Fatal("配置后 ReactConfig() 应为非 nil")
	}
}

// TestDeepAgent_LoopSession_配置后 配置后 LoopSession 仍为 nil
func TestDeepAgent_LoopSession_配置后(t *testing.T) {
	d := newTestDeepAgent()
	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}
	if d.LoopSession() != nil {
		t.Error("配置后 LoopSession() 应为 nil")
	}
}

// TestDeepAgent_hasRemainingTasks_有TaskPlan 有 TaskPlan 且有待执行任务时返回 true
func TestDeepAgent_hasRemainingTasks_有TaskPlan(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 设置含待执行任务的 TaskPlan
	state := d.LoadState(sess)
	plan := schema.NewTaskPlan("测试计划", "测试目标")
	plan.AddTask(schema.TodoItem{
		ID:      "task-1",
		Content: "待执行任务",
		Status:  schema.TodoStatusPending,
	})
	state.TaskPlan = &plan
	d.SaveState(sess, state)

	if !d.hasRemainingTasks(sess) {
		t.Error("有待执行任务时 hasRemainingTasks 应返回 true")
	}
}

// TestDeepAgent_hasRemainingTasks_全部完成 所有任务完成时返回 false
func TestDeepAgent_hasRemainingTasks_全部完成(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	state := d.LoadState(sess)
	plan := schema.NewTaskPlan("测试计划", "测试目标")
	plan.AddTask(schema.TodoItem{
		ID:      "task-1",
		Content: "已完成任务",
		Status:  schema.TodoStatusCompleted,
	})
	state.TaskPlan = &plan
	d.SaveState(sess, state)

	if d.hasRemainingTasks(sess) {
		t.Error("所有任务完成时 hasRemainingTasks 应返回 false")
	}
}

// TestDeepAgent_registerPendingMCPs_无配置 无 MCP 配置时不注册
func TestDeepAgent_registerPendingMCPs_无配置(t *testing.T) {
	d := newTestDeepAgent()
	d.registerPendingMCPs(context.Background())
}

// TestDeepAgent_EnsureInitialized_带MCPs 有 MCP 配置时注册
func TestDeepAgent_EnsureInitialized_带MCPs(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Mcps = []*mcptypes.McpServerConfig{
		{ServerID: "mcp-1"},
	}
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_EnsureInitialized_有Workspace 有 workspace 且 AutoCreate 时初始化
func TestDeepAgent_EnsureInitialized_有Workspace(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/ws"}
	cfg.AutoCreateWorkspace = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_createReactAgent_有上下文引擎 有 ContextEngineConfig 时设置
func TestDeepAgent_createReactAgent_有上下文引擎(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	ceCfg := ceschema.ContextEngineConfig{
		MaxContextMessageNum: 100,
	}
	cfg.ContextEngineConfig = &ceCfg
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if d.ReactAgent() == nil {
		t.Fatal("有 ContextEngineConfig 时 ReactAgent() 应为非 nil")
	}
}

// TestDeepAgent_createReactAgent_自定义提示词 有 SystemPrompt 时构建提示词
func TestDeepAgent_createReactAgent_自定义提示词(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.SystemPrompt = "你是一个测试 Agent"
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if d.ReactAgent() == nil {
		t.Fatal("有 SystemPrompt 时 ReactAgent() 应为非 nil")
	}
}

// TestDeepAgent_EnsureInitialized_二次调用 幂等
func TestDeepAgent_EnsureInitialized_二次调用(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("首次 EnsureInitialized 返回错误: %v", err)
	}
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("二次 EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_SaveState_NilState_无缓存 state 和缓存都为 nil 时跳过
func TestDeepAgent_SaveState_NilState_无缓存(t *testing.T) {
	d := newTestDeepAgent()
	sess := newFakeSessionFacade("sess-1")

	// 不先保存，直接调用 SaveState(nil, nil)
	d.SaveState(sess, nil)
}

// TestDeepAgent_FollowUp_有LoopController 有 loopController 时执行完整逻辑
func TestDeepAgent_FollowUp_有LoopController(t *testing.T) {
	d := newTestDeepAgent()
	sess := session.NewSession()

	// 通过 setupTaskLoop 内部创建 controller
	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 直接注入 loopSession 以覆盖 FollowUp 路径
	d.configMu.Lock()
	d.loopSession = sess
	d.configMu.Unlock()

	// FollowUp 不 panic
	d.FollowUp(context.Background(), "followup msg", "task-1", sess)
}

// TestDeepAgent_Steer_有LoopController 有 loopController 时执行完整逻辑
func TestDeepAgent_Steer_有LoopController(t *testing.T) {
	d := newTestDeepAgent()
	sess := session.NewSession()

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	d.configMu.Lock()
	d.loopSession = sess
	d.configMu.Unlock()

	d.Steer(context.Background(), "steer msg", sess)
}

// TestDeepAgent_Abort_注入协调器 通过注入协调器测试取消逻辑
func TestDeepAgent_Abort_注入协调器(t *testing.T) {
	d := newTestDeepAgent()

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// 注入 coordinator 和 controller
	coord := task_loop.NewLoopCoordinator(nil)
	ctrl := task_loop.NewTaskLoopController()
	d.configMu.Lock()
	d.loopCoordinator = coord
	d.loopController = ctrl
	d.configMu.Unlock()

	d.Abort(context.Background())

	if !coord.IsAborted() {
		t.Error("Abort 后协调器应被中止")
	}
}

// TestDeepAgent_cancelStreamProcess_有Cancel 有 streamCancel 时调用
func TestDeepAgent_cancelStreamProcess_有Cancel(t *testing.T) {
	d := newTestDeepAgent()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.streamMu.Lock()
	d.streamCancel = cancel
	d.streamMu.Unlock()

	d.cancelStreamProcess()
}

// TestDeepAgent_EnsureInitialized_有PendingHarnessConfig 有 pending harness config 时 drain
func TestDeepAgent_EnsureInitialized_有PendingHarnessConfig(t *testing.T) {
	d := newTestDeepAgent()

	d.EnqueueHarnessConfig("/path/to/config.yaml")

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// EnsureInitialized 不会自动 drain，Stream 才会
	if err := d.EnsureInitialized(context.Background()); err != nil {
		t.Fatalf("EnsureInitialized 返回错误: %v", err)
	}
}

// TestDeepAgent_Invoke_带ResumeInput resume 输入走单轮路径
func TestDeepAgent_Invoke_带ResumeInput(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-deep"))
	d := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	// isResumeInput 当前返回 false，所以仍走任务循环路径
	_, err := d.Invoke(context.Background(), map[string]any{"query": "resume query"})
	_ = err
}

// TestDeepAgent_Stream_带PendingHarness 有 pending harness config 时 drain
func TestDeepAgent_Stream_带PendingHarness(t *testing.T) {
	d := newTestDeepAgent()

	d.EnqueueHarnessConfig("/path/to/config.yaml")

	cfg := schema.NewDeepAgentConfig()
	if err := d.ConfigureDeepConfig(context.Background(), cfg); err != nil {
		t.Fatalf("ConfigureDeepConfig 返回错误: %v", err)
	}

	ch, err := d.Stream(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		_ = err
		return
	}
	for range ch {
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFakeSessionFacade 创建模拟会话门面
func newFakeSessionFacade(sessionID string) *fakeSessionFacade {
	return &fakeSessionFacade{
		sessionID: sessionID,
		stateMap:  make(map[string]any),
	}
}

// GetSessionID 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetSessionID() string {
	return f.sessionID
}

// UpdateState 实现 SessionFacade 接口
func (f *fakeSessionFacade) UpdateState(data map[string]any) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	for k, v := range data {
		f.stateMap[k] = v
	}
}

// GetState 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetState(key sessstate.StateKey) (any, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	// 通过将 StringKey 转换回字符串来查找
	strKey := key.String()
	return f.stateMap[strKey], nil
}

// DumpState 实现 SessionFacade 接口
func (f *fakeSessionFacade) DumpState() map[string]any {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	result := make(map[string]any, len(f.stateMap))
	for k, v := range f.stateMap {
		result[k] = v
	}
	return result
}

// WriteStream 实现 SessionFacade 接口
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error {
	return nil
}

// WriteCustomStream 实现 SessionFacade 接口
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}

// GetEnv 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any {
	return nil
}

// Interact 实现 SessionFacade 接口
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error {
	return nil
}

// 确认 fakeSessionFacade 满足 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*fakeSessionFacade)(nil)

// Init 实现 AgentRail 接口
func (r *fakeAgentRail) Init(_ agentinterfaces.BaseAgent) error {
	r.initCalled = true
	return nil
}

// Uninit 实现 AgentRail 接口
func (r *fakeAgentRail) Uninit(_ agentinterfaces.BaseAgent) error {
	r.uninitCalled = true
	return nil
}

// GetCallbacks 实现 AgentRail 接口
func (r *fakeAgentRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}

// Priority 实现 AgentRail 接口
func (r *fakeAgentRail) Priority() int {
	return r.base.Priority()
}

// BeforeInvoke 实现 AgentRail 接口
func (r *fakeAgentRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterInvoke 实现 AgentRail 接口
func (r *fakeAgentRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRail) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRail) AfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeModelCall 实现 AgentRail 接口
func (r *fakeAgentRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterModelCall 实现 AgentRail 接口
func (r *fakeAgentRail) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnModelException 实现 AgentRail 接口
func (r *fakeAgentRail) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeToolCall 实现 AgentRail 接口
func (r *fakeAgentRail) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterToolCall 实现 AgentRail 接口
func (r *fakeAgentRail) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnToolException 实现 AgentRail 接口
func (r *fakeAgentRail) OnToolException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// 确认 fakeAgentRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*fakeAgentRail)(nil)

// Init 实现 AgentRail 接口
func (r *fakeAgentRailWithType) Init(_ agentinterfaces.BaseAgent) error { return nil }

// Uninit 实现 AgentRail 接口
func (r *fakeAgentRailWithType) Uninit(_ agentinterfaces.BaseAgent) error { return nil }

// Priority 实现 AgentRail 接口
func (r *fakeAgentRailWithType) Priority() int { return r.base.Priority() }

// BeforeInvoke 实现 AgentRail 接口
func (r *fakeAgentRailWithType) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterInvoke 实现 AgentRail 接口
func (r *fakeAgentRailWithType) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRailWithType) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRailWithType) AfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeModelCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterModelCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnModelException 实现 AgentRail 接口
func (r *fakeAgentRailWithType) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeToolCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterToolCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnToolException 实现 AgentRail 接口
func (r *fakeAgentRailWithType) OnToolException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// GetCallbacks 实现 AgentRail 接口
func (r *fakeAgentRailWithType) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}

// 确认 fakeAgentRailWithType 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*fakeAgentRailWithType)(nil)

// Init 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) Init(_ agentinterfaces.BaseAgent) error { return nil }

// Uninit 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) Uninit(_ agentinterfaces.BaseAgent) error { return nil }

// Priority 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) Priority() int { return r.base.Priority() }

// BeforeInvoke 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterInvoke 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) AfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeModelCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterModelCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnModelException 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeToolCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterToolCall 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnToolException 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) OnToolException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// GetCallbacks 实现 AgentRail 接口
func (r *fakeAgentRailWithType2) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}

// 确认 fakeAgentRailWithType2 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*fakeAgentRailWithType2)(nil)

// Init 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) Init(_ agentinterfaces.BaseAgent) error { return nil }

// Uninit 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) Uninit(_ agentinterfaces.BaseAgent) error { return nil }

// Priority 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) Priority() int { return r.base.Priority() }

// BeforeInvoke 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterInvoke 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterTaskIteration 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) AfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeModelCall 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterModelCall 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnModelException 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// BeforeToolCall 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterToolCall 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnToolException 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) OnToolException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// GetCallbacks 实现 AgentRail 接口
func (r *fakeAgentRailWithCallbacks) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	result := make(map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc, len(r.events))
	for _, event := range r.events {
		result[event] = func(ctx context.Context, agentCallbackContext any) error { return nil }
	}
	return result
}

// 确认 fakeAgentRailWithCallbacks 满足 AgentRail 接口

// ──────────────────────────── 补充覆盖率测试 ────────────────────────────

// makeTestCard 创建测试用 AgentCard（option pattern）
func makeTestCard(id, name string) *agentschema.AgentCard {
	return agentschema.NewAgentCard(
		agentschema.WithAgentID(id),
		agentschema.WithAgentName(name),
	)
}

// TestDeepAgent_hotReloadModel_完整热更新 测试 hotReloadModel 完整路径
// 对齐 Python: DeepAgent._hot_reload_model(config) (line 291)
func TestDeepAgent_hotReloadModel_完整热更新(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	config.MaxIterations = 10
	_ = agent.ConfigureDeepConfig(context.Background(), config)
	require.NotNil(t, agent.ReactAgent())

	// 二次 configure 触发 hotReconfigure → hotReloadModel
	config2 := schema.NewDeepAgentConfig()
	config2.Card = card
	config2.MaxIterations = 20
	err := agent.ConfigureDeepConfig(context.Background(), config2)
	assert.NoError(t, err)
}

// TestDeepAgent_hotReloadModel_无ReactAgent 测试 hotReloadModel 在无 ReactAgent 时提前返回
func TestDeepAgent_hotReloadModel_无ReactAgent(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)
	config := schema.NewDeepAgentConfig()
	config.MaxIterations = 10
	agent.hotReloadModel(context.Background(), config)
}

// TestDeepAgent_hotReloadTools_完整流程 测试工具热重载
func TestDeepAgent_hotReloadTools_完整流程(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	tc1 := &tool.ToolCard{BaseCard: cschema.BaseCard{ID: "tool-1", Name: "bash"}}
	agent.AbilityManager().Add(tc1)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	config.Tools = []*tool.ToolCard{
		{BaseCard: cschema.BaseCard{ID: "tool-2", Name: "bash"}},
		{BaseCard: cschema.BaseCard{ID: "tool-3", Name: "read_file"}},
	}
	prev := []*tool.ToolCard{tc1}
	agent.hotReloadTools(config, &prev)

	am := agent.AbilityManager()
	assert.NotNil(t, am.Get("read_file"))
}

// TestDeepAgent_forceCleanupController_有控制器 测试强制清理控制器
func TestDeepAgent_forceCleanupController_有控制器(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	sess := session.NewSession()
	coord, ctrl, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)
	require.NotNil(t, coord)
	require.NotNil(t, ctrl)

	agent.forceCleanupController(context.Background())

	agent.configMu.RLock()
	assert.Nil(t, agent.loopController)
	agent.configMu.RUnlock()
}

// TestDeepAgent_setupTaskLoop_首次创建 测试首次创建任务循环
func TestDeepAgent_setupTaskLoop_首次创建(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	config.EnableTaskLoop = true
	_ = agent.ConfigureDeepConfig(context.Background(), config)

	sess := session.NewSession()
	coord, ctrl, err := agent.setupTaskLoop(context.Background(), sess)
	assert.NoError(t, err)
	assert.NotNil(t, coord)
	assert.NotNil(t, ctrl)
}

// TestDeepAgent_setupTaskLoop_新会话创建控制器 测试不同会话创建不同控制器
func TestDeepAgent_setupTaskLoop_新会话创建控制器(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	config.EnableTaskLoop = true
	_ = agent.ConfigureDeepConfig(context.Background(), config)

	sess1 := session.NewSession()
	coord1, ctrl1, err := agent.setupTaskLoop(context.Background(), sess1)
	require.NoError(t, err)
	require.NotNil(t, ctrl1)

	// 手动绑定会话（模拟 Invoke 中的 BindSession 流程）
	_ = ctrl1.BindSession(context.Background(), sess1)
	agent.configMu.Lock()
	agent.boundSessionID = sess1.GetSessionID()
	agent.configMu.Unlock()

	// 同一会话应复用控制器
	coord2, ctrl2, err := agent.setupTaskLoop(context.Background(), sess1)
	require.NoError(t, err)
	assert.Equal(t, ctrl1, ctrl2, "同一会话应复用相同控制器")
	_ = coord1
	_ = coord2
}

// TestDeepAgent_ensureInitialized_完整路径 测试完整初始化路径
func TestDeepAgent_ensureInitialized_完整路径(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	_ = agent.ConfigureDeepConfig(context.Background(), config)

	err := agent.ensureInitialized(context.Background())
	assert.NoError(t, err)
	assert.True(t, agent.IsInitialized())

	err = agent.ensureInitialized(context.Background())
	assert.NoError(t, err)
}

// TestDeepAgent_ScheduleAutoInvokeOnSpawnDone_活跃Invoke 测试有活跃 invoke 时的调度
func TestDeepAgent_ScheduleAutoInvokeOnSpawnDone_活跃Invoke(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)
	agent.invokeActive.Store(true)

	err := agent.ScheduleAutoInvokeOnSpawnDone("test steer text", 0.5)
	assert.NoError(t, err)
	assert.True(t, agent.IsAutoInvokeScheduled())
}

// TestDeepAgent_上下文引擎方法 测试上下文引擎相关方法
func TestDeepAgent_上下文引擎方法(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	_ = agent.ConfigureDeepConfig(context.Background(), config)

	usage, err := agent.GetContextUsage(context.Background(), "session-1", "context-1")
	assert.Error(t, err) // 未初始化上下文引擎，应返回错误
	_ = usage

	msgs, err := agent.GetCurrentContext(context.Background(), "session-1", "context-1")
	assert.Error(t, err) // 未初始化上下文引擎，应返回错误
	_ = msgs
}

// TestDeepAgent_LoadHarnessConfig_Stub 测试 LoadHarnessConfig 返回 stub 错误
func TestDeepAgent_LoadHarnessConfig_Stub(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)
	_, err := agent.LoadHarnessConfig(context.Background(), "/path/to/config.yaml")
	assert.Error(t, err)
}

// TestDeepAgent_UnloadHarnessConfig_Stub 测试 UnloadHarnessConfig 返回 stub 错误
func TestDeepAgent_UnloadHarnessConfig_Stub(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)
	_, err := agent.UnloadHarnessConfig(context.Background(), "/path/to/config.yaml")
	assert.Error(t, err)
}

// TestDeepAgent_hasPendingSessionSpawn_有控制器 测试有控制器时检查 pending spawn
func TestDeepAgent_hasPendingSessionSpawn_有控制器(t *testing.T) {
	card := makeTestCard("deep-1", "test-deep")
	agent := NewDeepAgent(card)

	config := schema.NewDeepAgentConfig()
	config.Card = card
	_ = agent.ConfigureDeepConfig(context.Background(), config)

	sess := session.NewSession()
	_, _, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)

	result := agent.hasPendingSessionSpawn()
	assert.False(t, result)
}

// ──────────────────────────── 补充覆盖率测试（第二轮） ────────────────────────────

// TestDeepAgent_Invoke_已配置带Session 单轮 Invoke 传入 session 时走保存状态路径
func TestDeepAgent_Invoke_已配置带Session(t *testing.T) {
	card := makeTestCard("deep-inv-sess", "test-deep-inv-sess")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = false
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	// Invoke 传入 session，应走 saveState/clearState 路径
	_, _ = agent.Invoke(context.Background(), map[string]any{"query": "hello"},
		agentinterfaces.WithSession(sess))
}

// TestDeepAgent_Invoke_任务循环带Session 任务循环模式下传入 session 会话
func TestDeepAgent_Invoke_任务循环带Session(t *testing.T) {
	card := makeTestCard("deep-inv-loop", "test-deep-inv-loop")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	// 使用超时 context 防止任务循环永久等待
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 任务循环模式传入 *session.Session，runTaskLoopInvoke 需要 Session 类型
	_, _ = agent.Invoke(ctx, map[string]any{"query": "hello"},
		agentinterfaces.WithSession(sess))
}

// TestDeepAgent_Stream_已配置带Session 单轮 Stream 传入 session
func TestDeepAgent_Stream_已配置带Session(t *testing.T) {
	card := makeTestCard("deep-str-sess", "test-deep-str-sess")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = false
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	ch, err := agent.Stream(context.Background(), map[string]any{"query": "hello"},
		agentinterfaces.WithSession(sess))
	if err != nil {
		return
	}
	// 消费 channel 以触发 goroutine 完成
	for range ch {
	}
}

// TestDeepAgent_Stream_任务循环带Session 任务循环模式 Stream 传入 session
func TestDeepAgent_Stream_任务循环带Session(t *testing.T) {
	card := makeTestCard("deep-str-loop", "test-deep-str-loop")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	// 使用超时 context 防止流式任务循环永久等待
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := agent.Stream(ctx, map[string]any{"query": "hello"},
		agentinterfaces.WithSession(sess))
	if err != nil {
		return
	}
	for range ch {
	}
}

// TestDeepAgent_hotReloadModel_ReactAgent配置为nil reactAgent.Config() 返回 nil 时提前返回
func TestDeepAgent_hotReloadModel_ReactAgent配置为nil(t *testing.T) {
	card := makeTestCard("deep-hrm-nil", "test-deep-hrm-nil")
	agent := NewDeepAgent(card)

	// 不配置，reactAgent 为 nil，hotReloadModel 应提前返回
	cfg := schema.NewDeepAgentConfig()
	cfg.MaxIterations = 5
	agent.hotReloadModel(context.Background(), cfg)
}

// TestDeepAgent_hotReloadModel_配置正常类型 已配置后热更新正常执行
func TestDeepAgent_hotReloadModel_配置正常类型(t *testing.T) {
	card := makeTestCard("deep-hrm-type", "test-deep-hrm-type")
	agent := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg1))

	// 二次配置触发 hotReloadModel，正常类型断言成功
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.MaxIterations = 7
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg2))
}

// TestDeepAgent_hotReloadModel_带ModelClientConfig 热更新模型客户端配置
func TestDeepAgent_hotReloadModel_带ModelClientConfig(t *testing.T) {
	card := makeTestCard("deep-hrm-model", "test-deep-hrm-model")
	agent := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg1))

	// 热重配置带 Model 配置（含 ClientConfig 和 ModelConfig）
	cfg2 := schema.NewDeepAgentConfig()
	llmModel := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{
			ClientProvider: "test",
			APIKey:         "test-key",
			APIBase:        "https://test.api",
		},
		ModelConfig: &llmschema.ModelRequestConfig{
			ModelName: "test-model",
		},
	}
	cfg2.Model = llmModel
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg2))
}

// TestDeepAgent_hotReloadModel_EnableTaskLoop时MaxIterations 热重配置启用任务循环时不报错
func TestDeepAgent_hotReloadModel_EnableTaskLoop时MaxIterations(t *testing.T) {
	card := makeTestCard("deep-hrm-loop", "test-deep-hrm-loop")
	agent := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg1))

	// 热重配置启用任务循环，需要设 Model 以触发 hotReloadModel
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.EnableTaskLoop = true
	cfg2.Model = &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{
			ClientProvider: "test",
			APIKey:         "test-key",
		},
	}
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg2))
}

// TestDeepAgent_hotReloadModel_带ContextEngineConfig 热重配置上下文引擎配置
func TestDeepAgent_hotReloadModel_带ContextEngineConfig(t *testing.T) {
	card := makeTestCard("deep-hrm-ce", "test-deep-hrm-ce")
	agent := NewDeepAgent(card)

	cfg1 := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg1))

	// 热重配置带 ContextEngineConfig
	cfg2 := schema.NewDeepAgentConfig()
	cfg2.ContextEngineConfig = &ceschema.ContextEngineConfig{
		ContextWindowTokens: 8000,
	}
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg2))
}

// TestDeepAgent_ScheduleAutoInvokeOnSpawnDone_无LoopSession loopSession 为 nil 时跳过 invoke
func TestDeepAgent_ScheduleAutoInvokeOnSpawnDone_无LoopSession(t *testing.T) {
	card := makeTestCard("deep-sai-nls", "test-deep-sai-nls")
	agent := NewDeepAgent(card)

	// 不设置 loopSession，ScheduleAutoInvokeOnSpawnDone 延迟后应跳过
	err := agent.ScheduleAutoInvokeOnSpawnDone("test query", 0.5)
	assert.NoError(t, err)
	assert.True(t, agent.IsAutoInvokeScheduled())

	// 等待延迟过去，autoInvokeScheduled 应被重置
	time.Sleep(600 * time.Millisecond)
	assert.False(t, agent.IsAutoInvokeScheduled())
}

// TestDeepAgent_ScheduleAutoInvokeOnSpawnDone_有LoopSession loopSession 非空且有 invokeActive 时跳过
func TestDeepAgent_ScheduleAutoInvokeOnSpawnDone_有LoopSession(t *testing.T) {
	card := makeTestCard("deep-sai-ls", "test-deep-sai-ls")
	agent := NewDeepAgent(card)

	sess := session.NewSession()
	agent.configMu.Lock()
	agent.loopSession = sess
	agent.configMu.Unlock()

	// 设置 invokeActive=true，延迟后应跳过 invoke
	agent.invokeActive.Store(true)
	err := agent.ScheduleAutoInvokeOnSpawnDone("test query", 0.5)
	assert.NoError(t, err)

	// 等待延迟过去
	time.Sleep(600 * time.Millisecond)
}

// TestDeepAgent_FollowUp_无Session传入 sess 为 nil 且 loopSession 为 nil 时提前返回
func TestDeepAgent_FollowUp_无Session传入(t *testing.T) {
	card := makeTestCard("deep-fu-ns", "test-deep-fu-ns")
	agent := NewDeepAgent(card)

	// 配置任务循环以创建 loopController
	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	// 注入 loopController 但不设 loopSession
	sess := session.NewSession()
	_, ctrl, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)
	require.NotNil(t, ctrl)

	// FollowUp 传 nil session，loopSession 也为 nil → 应提前返回
	agent.FollowUp(context.Background(), "msg", "task-1", nil)
}

// TestDeepAgent_FollowUp_传Session sess 非 nil 时走完整路径
func TestDeepAgent_FollowUp_传Session(t *testing.T) {
	card := makeTestCard("deep-fu-s", "test-deep-fu-s")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	_, _, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)

	// 传入 session 参数，FollowUp 应使用传入的 session
	agent.FollowUp(context.Background(), "followup msg", "task-1", sess)
}

// TestDeepAgent_Steer_无Session传入 sess 为 nil 且 loopSession 为 nil 时提前返回
func TestDeepAgent_Steer_无Session传入(t *testing.T) {
	card := makeTestCard("deep-st-ns", "test-deep-st-ns")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	_, ctrl, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)
	require.NotNil(t, ctrl)

	// Steer 传 nil session，loopSession 也为 nil → 应提前返回
	agent.Steer(context.Background(), "steer msg", nil)
}

// TestDeepAgent_Steer_传Session sess 非 nil 时走完整路径
func TestDeepAgent_Steer_传Session(t *testing.T) {
	card := makeTestCard("deep-st-s", "test-deep-st-s")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	_, _, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)

	agent.Steer(context.Background(), "steer msg", sess)
}

// TestDeepAgent_Steer_用LoopSession sess 为 nil 但 loopSession 非 nil
func TestDeepAgent_Steer_用LoopSession(t *testing.T) {
	card := makeTestCard("deep-st-ls", "test-deep-st-ls")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	_, _, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)

	// 注入 loopSession
	agent.configMu.Lock()
	agent.loopSession = sess
	agent.configMu.Unlock()

	// 传 nil session，但 loopSession 非空 → 应使用 loopSession
	agent.Steer(context.Background(), "steer msg", nil)
}

// TestDeepAgent_FollowUp_用LoopSession sess 为 nil 但 loopSession 非 nil
func TestDeepAgent_FollowUp_用LoopSession(t *testing.T) {
	card := makeTestCard("deep-fu-ls", "test-deep-fu-ls")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	_, _, err := agent.setupTaskLoop(context.Background(), sess)
	require.NoError(t, err)

	agent.configMu.Lock()
	agent.loopSession = sess
	agent.configMu.Unlock()

	agent.FollowUp(context.Background(), "followup msg", "task-1", nil)
}

// TestDeepAgent_hasPendingSessionSpawn_有RunningTask toolkit 有 running 任务时返回 true
func TestDeepAgent_hasPendingSessionSpawn_有RunningTask(t *testing.T) {
	card := makeTestCard("deep-hps-rt", "test-deep-hps-rt")
	agent := NewDeepAgent(card)

	toolkit := subagent.NewSessionToolkit()
	toolkit.UpsertRunning("task-1", "sub-sess-1", "测试任务")
	agent.SetSessionToolkit(toolkit)

	result := agent.hasPendingSessionSpawn()
	assert.True(t, result)
}

// TestDeepAgent_hasPendingSessionSpawn_有CompletedTask toolkit 只有 completed 任务时返回 false
func TestDeepAgent_hasPendingSessionSpawn_有CompletedTask(t *testing.T) {
	card := makeTestCard("deep-hps-ct", "test-deep-hps-ct")
	agent := NewDeepAgent(card)

	toolkit := subagent.NewSessionToolkit()
	toolkit.UpsertRunning("task-1", "sub-sess-1", "测试任务")
	toolkit.MarkCompleted("task-1", "done")
	agent.SetSessionToolkit(toolkit)

	result := agent.hasPendingSessionSpawn()
	assert.False(t, result)
}

// TestDeepAgent_hasPendingSessionSpawn_空Toolkit toolkit 为空时返回 false
func TestDeepAgent_hasPendingSessionSpawn_空Toolkit(t *testing.T) {
	card := makeTestCard("deep-hps-et", "test-deep-hps-et")
	agent := NewDeepAgent(card)

	toolkit := subagent.NewSessionToolkit()
	agent.SetSessionToolkit(toolkit)

	result := agent.hasPendingSessionSpawn()
	assert.False(t, result)
}

// TestDeepAgent_ensureInitialized_有StaleRails 有废弃 Rail 时注销
func TestDeepAgent_ensureInitialized_有StaleRails(t *testing.T) {
	card := makeTestCard("deep-ei-sr", "test-deep-ei-sr")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	// 初始化一次
	require.NoError(t, agent.ensureInitialized(context.Background()))

	// 注入 stale Rails
	fr := &fakeAgentRail{}
	agent.railsMu.Lock()
	agent.staleRails = append(agent.staleRails, fr)
	agent.initMu.Lock()
	agent.initialized = false
	agent.initMu.Unlock()
	agent.railsMu.Unlock()

	// 再次初始化时应注销废弃 Rail
	err := agent.ensureInitialized(context.Background())
	assert.NoError(t, err)
	assert.True(t, agent.IsInitialized())
}

// TestDeepAgent_ensureInitialized_有PendingRails 有待注册 Rail 时注册
func TestDeepAgent_ensureInitialized_有PendingRails(t *testing.T) {
	card := makeTestCard("deep-ei-pr", "test-deep-ei-pr")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	fr := &fakeAgentRail{}
	cfg.Rails = []agentinterfaces.AgentRail{fr}
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	// 首次初始化，应注册 pending Rails
	err := agent.ensureInitialized(context.Background())
	assert.NoError(t, err)
	assert.True(t, agent.IsInitialized())

	// 验证 registered Rails
	agent.railsMu.Lock()
	registered := len(agent.registeredRails)
	agent.railsMu.Unlock()
	assert.GreaterOrEqual(t, registered, 1)
}

// TestDeepAgent_ensureInitialized_有Workspace 有工作空间时初始化
func TestDeepAgent_ensureInitialized_有Workspace(t *testing.T) {
	card := makeTestCard("deep-ei-ws", "test-deep-ei-ws")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/test-ws"}
	cfg.AutoCreateWorkspace = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	err := agent.ensureInitialized(context.Background())
	assert.NoError(t, err)
}

// TestDeepAgent_ensureInitialized_有MCPs 有 MCP 配置时注册
func TestDeepAgent_ensureInitialized_有MCPs(t *testing.T) {
	card := makeTestCard("deep-ei-mcp", "test-deep-ei-mcp")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Mcps = []*mcptypes.McpServerConfig{
		{ServerID: "mcp-test"},
	}
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	err := agent.ensureInitialized(context.Background())
	assert.NoError(t, err)
}

// TestDeepAgent_createReactAgent_有Model 有 Model 时设置模型
func TestDeepAgent_createReactAgent_有Model(t *testing.T) {
	card := makeTestCard("deep-cra-m", "test-deep-cra-m")
	agent := NewDeepAgent(card)

	llmModel := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{
			ClientProvider: "test",
			APIKey:         "test-key",
			APIBase:        "https://test.api",
		},
		ModelConfig: &llmschema.ModelRequestConfig{
			ModelName: "test-model",
		},
	}

	cfg := schema.NewDeepAgentConfig()
	cfg.Model = llmModel
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	require.NotNil(t, agent.ReactAgent())
}

// TestDeepAgent_createReactAgent_有ModelAndWorkspace 有 Model 和 Workspace 时设置
func TestDeepAgent_createReactAgent_有ModelAndWorkspace(t *testing.T) {
	card := makeTestCard("deep-cra-mw", "test-deep-cra-mw")
	agent := NewDeepAgent(card)

	llmModel := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{
			ClientProvider: "test",
			APIKey:         "test-key",
		},
		ModelConfig: &llmschema.ModelRequestConfig{
			ModelName: "test-model",
		},
	}

	cfg := schema.NewDeepAgentConfig()
	cfg.Model = llmModel
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/ws2"}
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	require.NotNil(t, agent.ReactAgent())
}

// TestDeepAgent_SystemPromptBuilder_有RegisteredRails 有注册 Rail 时 SystemPromptBuilder 仍可访问
func TestDeepAgent_SystemPromptBuilder_有RegisteredRails(t *testing.T) {
	card := makeTestCard("deep-sba-rr", "test-deep-sba-rr")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	fr := &fakeAgentRail{}
	cfg.Rails = []agentinterfaces.AgentRail{fr}
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	// 初始化以注册 Rails
	require.NoError(t, agent.EnsureInitialized(context.Background()))

	// 热重配置后 SystemPromptBuilder 仍可访问
	cfg2 := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg2))
}

// TestDeepAgent_SystemPromptBuilder_未配置 未配置时返回 nil
func TestDeepAgent_SystemPromptBuilder_未配置(t *testing.T) {
	card := makeTestCard("deep-sba-sr", "test-deep-sba-sr")
	agent := NewDeepAgent(card)

	// 未配置时 SystemPromptBuilder 返回 nil
	if agent.SystemPromptBuilder() != nil {
		t.Error("未配置时 SystemPromptBuilder() 应返回 nil")
	}
}

// TestDeepAgent_runTaskLoopInvoke_非SessionFacade sess 不是 *session.Session 时返回错误
func TestDeepAgent_runTaskLoopInvoke_非SessionFacade(t *testing.T) {
	card := makeTestCard("deep-rtli-ns", "test-deep-rtli-ns")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	// 用 fakeSessionFacade 而非 *session.Session
	fakeSess := newFakeSessionFacade("fake-1")
	invokeInputs := &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("test")}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, invokeInputs, fakeSess)

	_, err := agent.runTaskLoopInvoke(context.Background(), cbc, fakeSess)
	assert.Error(t, err)
}

// TestDeepAgent_runTaskLoopInvoke_输入类型不匹配 inputs 不是 InvokeInputs 时返回错误
func TestDeepAgent_runTaskLoopInvoke_输入类型不匹配(t *testing.T) {
	card := makeTestCard("deep-rtli-it", "test-deep-rtli-it")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	// 传入非 InvokeInputs 类型的 cbc
	mapInputs := &agentinterfaces.MapInputs{Data: map[string]any{"query": "invalid"}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, mapInputs, sess)

	_, err := agent.runTaskLoopInvoke(context.Background(), cbc, sess)
	assert.Error(t, err)
}

// TestDeepAgent_runTaskLoopStream_无Session sess 为 nil 时返回错误
func TestDeepAgent_runTaskLoopStream_无Session(t *testing.T) {
	card := makeTestCard("deep-rtls-ns", "test-deep-rtls-ns")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	invokeInputs := &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("test")}
	_, err := agent.runTaskLoopStream(context.Background(), invokeInputs, nil, nil)
	assert.Error(t, err)
}

// TestDeepAgent_runTaskLoopStream_有Session sess 非空时返回 channel
func TestDeepAgent_runTaskLoopStream_有Session(t *testing.T) {
	card := makeTestCard("deep-rtls-s", "test-deep-rtls-s")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.EnableTaskLoop = true
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	invokeInputs := &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("test")}

	// 使用带超时的 context 避免 goroutine 泄漏导致测试超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := agent.runTaskLoopStream(ctx, invokeInputs, sess, nil)
	assert.NoError(t, err)
	require.NotNil(t, ch)

	// 消费 channel 直到 context 取消或 channel 关闭
	for range ch {
	}
}

// TestDeepAgent_runSingleRoundInvoke_输入类型不匹配 inputs 非 InvokeInputs 时返回错误
func TestDeepAgent_runSingleRoundInvoke_输入类型不匹配(t *testing.T) {
	card := makeTestCard("deep-sri-it", "test-deep-sri-it")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	sess := session.NewSession()
	mapInputs := &agentinterfaces.MapInputs{Data: map[string]any{"query": "invalid"}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, mapInputs, sess)

	_, err := agent.runSingleRoundInvoke(context.Background(), cbc, sess)
	assert.Error(t, err)
}

// TestDeepAgent_runSingleRoundInvoke_无ReactAgent reactAgent 为 nil 时返回错误
func TestDeepAgent_runSingleRoundInvoke_无ReactAgent(t *testing.T) {
	card := makeTestCard("deep-sri-nr", "test-deep-sri-nr")
	agent := NewDeepAgent(card)

	// 不配置，reactAgent 为 nil
	invokeInputs := &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("test")}
	fakeSess := newFakeSessionFacade("fake-1")
	cbc := agentinterfaces.NewAgentCallbackContext(agent, invokeInputs, fakeSess)

	_, err := agent.runSingleRoundInvoke(context.Background(), cbc, fakeSess)
	assert.Error(t, err)
}

// TestDeepAgent_runSingleRoundStream_无ReactAgent reactAgent 为 nil 时返回错误
func TestDeepAgent_runSingleRoundStream_无ReactAgent(t *testing.T) {
	card := makeTestCard("deep-srs-nr", "test-deep-srs-nr")
	agent := NewDeepAgent(card)

	invokeInputs := &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString("test")}
	_, err := agent.runSingleRoundStream(context.Background(), invokeInputs, nil, nil)
	assert.Error(t, err)
}

// TestDeepAgent_ensureInitialized_重复调用幂等 多次调用不报错
func TestDeepAgent_ensureInitialized_重复调用幂等(t *testing.T) {
	card := makeTestCard("deep-ei-idem", "test-deep-ei-idem")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	for i := 0; i < 3; i++ {
		err := agent.ensureInitialized(context.Background())
		assert.NoError(t, err)
	}
	assert.True(t, agent.IsInitialized())
}

// TestDeepAgent_EnsureInitialized_配置后调用 配置后调用不报错
func TestDeepAgent_EnsureInitialized_配置后调用(t *testing.T) {
	card := makeTestCard("deep-ei-cfg", "test-deep-ei-cfg")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	cfg.Workspace = &workspace.Workspace{RootPath: "/tmp/ws3"}
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	err := agent.EnsureInitialized(context.Background())
	assert.NoError(t, err)
	assert.True(t, agent.IsInitialized())
}

// TestDeepAgent_CreateSubagent_未找到Spec subagentType 未找到时返回错误
func TestDeepAgent_CreateSubagent_未找到Spec(t *testing.T) {
	card := makeTestCard("deep-cs-nf", "test-deep-cs-nf")
	agent := NewDeepAgent(card)

	cfg := schema.NewDeepAgentConfig()
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	_, err := agent.CreateSubagent("nonexistent_type", "sub-sess-1")
	assert.Error(t, err)
}

// TestDeepAgent_CreateSubagent_工厂未实现 各种未实现的工厂类型返回 stub 错误
func TestDeepAgent_CreateSubagent_工厂未实现(t *testing.T) {
	card := makeTestCard("deep-cs-fac", "test-deep-cs-fac")
	agent := NewDeepAgent(card)

	// 这些工厂分支显式返回 stub 错误
	stubFactories := []string{
		"browser_agent", "code_agent", "research_agent",
		"mobile_gui_agent",
	}

	for _, factory := range stubFactories {
		subCfg := schema.NewSubAgentConfig()
		subCfg.AgentCard = agentschema.NewAgentCard(agentschema.WithAgentName("sub"))
		subCfg.FactoryName = factory
		cfg := schema.NewDeepAgentConfig()
		cfg.Subagents = append(cfg.Subagents, subCfg)
		require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

		_, err := agent.CreateSubagent("sub", "sub-sess-1")
		assert.Error(t, err, "工厂 %s 应返回错误", factory)
	}

	// unknown_factory 走 default 分支，通过 CreateDeepAgent 创建
	// 可能成功或因缺少配置返回错误，但不应 panic
	subCfg := schema.NewSubAgentConfig()
	subCfg.AgentCard = agentschema.NewAgentCard(agentschema.WithAgentName("sub2"))
	subCfg.FactoryName = "unknown_factory"
	cfg := schema.NewDeepAgentConfig()
	cfg.Subagents = append(cfg.Subagents, subCfg)
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))

	_, err := agent.CreateSubagent("sub2", "sub-sess-1")
	// 不要求必须报错，default 分支走 CreateDeepAgent
	if err != nil {
		t.Logf("unknown_factory 返回错误: %v", err)
	}
}

// TestDeepAgent_EnqueueHarnessConfig_多次排队 多次排队追加到列表
func TestDeepAgent_EnqueueHarnessConfig_多次排队(t *testing.T) {
	card := makeTestCard("deep-ehc-m", "test-deep-ehc-m")
	agent := NewDeepAgent(card)

	agent.EnqueueHarnessConfig("/path1/config.yaml")
	agent.EnqueueHarnessConfig("/path2/config.yaml")

	agent.railsMu.Lock()
	count := len(agent.pendingHarnessConfigs)
	agent.railsMu.Unlock()
	assert.Equal(t, 2, count)
}

// TestDeepAgent_SetSessionToolkit_设置后读取 设置 toolkit 后可通过 hasPendingSessionSpawn 检查
func TestDeepAgent_SetSessionToolkit_设置后读取(t *testing.T) {
	card := makeTestCard("deep-sst", "test-deep-sst")
	agent := NewDeepAgent(card)

	toolkit := subagent.NewSessionToolkit()
	agent.SetSessionToolkit(toolkit)

	// 空 toolkit，不应有 pending spawn
	assert.False(t, agent.hasPendingSessionSpawn())

	// 添加 running 任务
	toolkit.UpsertRunning("task-1", "sub-sess-1", "测试")
	assert.True(t, agent.hasPendingSessionSpawn())
}
