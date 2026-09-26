package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	ext "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/external"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// noopSystemPromptBuilder 空 SystemPromptBuilder，用于测试中满足非 nil 检查
type noopSystemPromptBuilder struct{}

func (n *noopSystemPromptBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	return nil
}
func (n *noopSystemPromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	return nil
}
func (n *noopSystemPromptBuilder) Language() string                               { return "cn" }
func (n *noopSystemPromptBuilder) SetLanguage(lang string)                        {}
func (n *noopSystemPromptBuilder) GetSection(name string) *saprompt.PromptSection { return nil }
func (n *noopSystemPromptBuilder) HasSection(name string) bool                    { return false }

// fakeAbilityManager 测试用 AbilityManagerInterface 实现
type fakeAbilityManager struct {
	addedAbilities map[string]bool
}

func newFakeAbilityManager() *fakeAbilityManager {
	return &fakeAbilityManager{addedAbilities: make(map[string]bool)}
}

func (f *fakeAbilityManager) Add(ability cschema.Ability) agentschema.AddAbilityResult {
	name := ability.AbilityName()
	f.addedAbilities[name] = true
	return agentschema.AddAbilityResult{Name: name, Added: true, Reason: "added_tool"}
}
func (f *fakeAbilityManager) AddMany(abilities []cschema.Ability) []agentschema.AddAbilityResult {
	var results []agentschema.AddAbilityResult
	for _, a := range abilities {
		results = append(results, f.Add(a))
	}
	return results
}
func (f *fakeAbilityManager) Remove(name string) cschema.Ability          { return nil }
func (f *fakeAbilityManager) RemoveMany(names []string) []cschema.Ability { return nil }
func (f *fakeAbilityManager) Get(name string) cschema.Ability             { return nil }
func (f *fakeAbilityManager) List() []cschema.Ability                     { return nil }
func (f *fakeAbilityManager) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]cschema.ToolInfoInterface, error) {
	return nil, nil
}
func (f *fakeAbilityManager) Execute(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ []*llmschema.ToolCall, _ sessioninterfaces.SessionFacade, _ string) []agentschema.ExecuteResult {
	return nil
}
func (f *fakeAbilityManager) SetContextEngine(_ ceinterface.ContextEngine) {}
func (f *fakeAbilityManager) ReorderTools(_ []string)                      {}

// fakeBaseAgentForMemoryRail 测试用 BaseAgent 实现，支持 SystemPromptBuilder + AbilityManager
type fakeBaseAgentForMemoryRail struct {
	builder *saprompt.SystemPromptBuilder
	am      *fakeAbilityManager
}

func newFakeBaseAgentForMemoryRail() *fakeBaseAgentForMemoryRail {
	return &fakeBaseAgentForMemoryRail{
		builder: saprompt.NewSystemPromptBuilder(),
		am:      newFakeAbilityManager(),
	}
}

func (f *fakeBaseAgentForMemoryRail) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgentForMemoryRail) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgentForMemoryRail) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgentForMemoryRail) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: cschema.BaseCard{ID: "test-agent", Name: "TestAgent"}}
}
func (f *fakeBaseAgentForMemoryRail) Config() agentinterfaces.AgentConfig { return nil }
func (f *fakeBaseAgentForMemoryRail) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.am
}
func (f *fakeBaseAgentForMemoryRail) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return agentinterfaces.NewAgentCallbackManager("test-agent")
}
func (f *fakeBaseAgentForMemoryRail) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.builder
}
func (f *fakeBaseAgentForMemoryRail) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForMemoryRail) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForMemoryRail) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeBaseAgentForMemoryRail)(nil)

// syncTurnRecord 记录一次 SyncTurn 调用
type syncTurnRecord struct {
	userMsg      string
	assistantMsg string
}

// fakeProvider 测试用 MemoryProvider 实现
type fakeProvider struct {
	ext.BaseMemoryProvider
	mu             sync.Mutex
	name           string
	available      bool
	initialized    bool
	toolSchemas    []ext.ToolSchema
	prefetchResult string
	prefetchErr    error
	prefetchDelay  time.Duration
	syncTurnCalls  []syncTurnRecord
	syncTurnErr    error
	syncTurnDelay  time.Duration
	shutdownCalled bool
}

func (f *fakeProvider) Name() string      { return f.name }
func (f *fakeProvider) IsAvailable() bool { return f.available }
func (f *fakeProvider) Initialize(_ context.Context, _ ...ext.ProviderOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initialized = true
	return nil
}
func (f *fakeProvider) GetToolSchemas() []ext.ToolSchema { return f.toolSchemas }
func (f *fakeProvider) HandleToolCall(_ context.Context, toolName string, _ map[string]any) (string, error) {
	return `{"tool": "` + toolName + `", "result": "ok"}`, nil
}
func (f *fakeProvider) Prefetch(ctx context.Context, _ string, _ ...ext.ProviderOption) (string, error) {
	if f.prefetchDelay > 0 {
		select {
		case <-time.After(f.prefetchDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.prefetchResult, f.prefetchErr
}
func (f *fakeProvider) SyncTurn(_ context.Context, userMsg, assistantMsg string, _ ...ext.ProviderOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.syncTurnDelay > 0 {
		time.Sleep(f.syncTurnDelay)
	}
	if f.syncTurnErr != nil {
		return f.syncTurnErr
	}
	f.syncTurnCalls = append(f.syncTurnCalls, syncTurnRecord{userMsg: userMsg, assistantMsg: assistantMsg})
	return nil
}
func (f *fakeProvider) IsInitialized() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initialized
}
func (f *fakeProvider) Shutdown(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCalled = true
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestBuildMemoryContextBlock(t *testing.T) {
	raw := "用户偏好：喜欢简洁的代码风格"
	result := buildMemoryContextBlock(raw)
	if !strings.Contains(result, "<memory-context>") {
		t.Error("缺少 <memory-context> 标签")
	}
	if !strings.Contains(result, "用户偏好：喜欢简洁的代码风格") {
		t.Error("缺少原始内容")
	}
	if !strings.Contains(result, "NOT new user input") {
		t.Error("缺少 System note")
	}
	if !strings.Contains(result, "</memory-context>") {
		t.Error("缺少 </memory-context> 闭合标签")
	}
}

func TestBuildMemoryContextBlock_空内容(t *testing.T) {
	result := buildMemoryContextBlock("")
	if !strings.Contains(result, "<memory-context>") {
		t.Error("空内容也应生成标签结构")
	}
}

func TestIsBackgroundRun_心跳(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindHeartbeat,
	}, nil)
	if !isBackgroundRun(cbc) {
		t.Error("心跳运行应返回 true")
	}
}

func TestIsBackgroundRun_Cron(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindCron,
	}, nil)
	if !isBackgroundRun(cbc) {
		t.Error("Cron 运行应返回 true")
	}
}

func TestIsBackgroundRun_正常(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindNormal,
	}, nil)
	if isBackgroundRun(cbc) {
		t.Error("正常运行应返回 false")
	}
}

func TestIsBackgroundRun_非InvokeInputs(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	if isBackgroundRun(cbc) {
		t.Error("非 InvokeInputs 应返回 false")
	}
}

func TestResolveUserTextForMemory_InvokeInputs查询优先(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("帮我写代码"),
	}, nil)
	text := resolveUserTextForMemory(cbc)
	if text != "帮我写代码" {
		t.Errorf("resolveUserTextForMemory = %q, want %q", text, "帮我写代码")
	}
}

func TestResolveUserTextForMemory_空查询(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	text := resolveUserTextForMemory(cbc)
	if text != "" {
		t.Errorf("resolveUserTextForMemory = %q, want empty", text)
	}
}

func TestResolveUserTextForMemory_空白查询(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("   "),
	}, nil)
	text := resolveUserTextForMemory(cbc)
	if text != "" {
		t.Errorf("resolveUserTextForMemory = %q, want empty for whitespace", text)
	}
}

func TestResolveUserTextForMemory_ModelCallInputs用户消息(t *testing.T) {
	msgs := []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户历史消息"),
		llmschema.NewUserMessage("最新用户消息"),
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{
		Messages: msgs,
	}, nil)
	text := resolveUserTextForMemory(cbc)
	if text != "最新用户消息" {
		t.Errorf("resolveUserTextForMemory = %q, want %q", text, "最新用户消息")
	}
}

func TestExtractAssistantOutput_InvokeInputsResult(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Result: map[string]any{"output": "这是助手回复"},
	}, nil)
	text := extractAssistantOutput(cbc)
	if text != "这是助手回复" {
		t.Errorf("extractAssistantOutput = %q, want %q", text, "这是助手回复")
	}
}

func TestExtractAssistantOutput_嵌套Message(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Result: map[string]any{"message": map[string]any{"content": "嵌套回复"}},
	}, nil)
	text := extractAssistantOutput(cbc)
	if text != "嵌套回复" {
		t.Errorf("extractAssistantOutput = %q, want %q", text, "嵌套回复")
	}
}

func TestExtractAssistantOutput_空Result(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	text := extractAssistantOutput(cbc)
	if text != "" {
		t.Errorf("extractAssistantOutput = %q, want empty", text)
	}
}

func TestExtractAssistantOutput_非InvokeInputs(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	text := extractAssistantOutput(cbc)
	if text != "" {
		t.Errorf("extractAssistantOutput = %q, want empty for non-InvokeInputs", text)
	}
}

func TestExternalMemoryRail_BeforeInvoke_初始化Provider(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeInvoke = %v", err)
	}
	if !provider.initialized {
		t.Error("Provider 未初始化")
	}
	if !rail.initialized {
		t.Error("Rail initialized 标记未设置")
	}
}

func TestExternalMemoryRail_BeforeInvoke_清空缓存(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	// 设置缓存
	cached := "cached result"
	rail.prefetchCache = &cached
	rail.prefetchInvokeID = 999

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	_ = rail.BeforeInvoke(context.Background(), cbc)

	if rail.prefetchCache != nil {
		t.Error("BeforeInvoke 应清空 prefetchCache")
	}
	if rail.prefetchInvokeID != 0 {
		t.Error("BeforeInvoke 应清空 prefetchInvokeID")
	}
}

func TestExternalMemoryRail_BeforeModelCall_Prefetch缓存命中(t *testing.T) {
	provider := &fakeProvider{
		name:           "test",
		available:      true,
		prefetchResult: "用户喜欢 Python",
		toolSchemas:    []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true
	// systemPromptBuilder 不为 nil 才能进入 prefetch 逻辑
	rail.systemPromptBuilder = &noopSystemPromptBuilder{}

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("你好"),
	}, nil)

	// 第一次调用 — 执行 prefetch
	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall = %v", err)
	}
	if rail.prefetchCache == nil || *rail.prefetchCache != "用户喜欢 Python" {
		t.Error("Prefetch 结果未缓存")
	}

	// 第二次调用同 invokeID — 应命中缓存，不再调 provider
	provider.prefetchResult = "新结果"
	invokeID := cbcRefID(cbc)
	rail.prefetchInvokeID = invokeID
	cachedResult := "缓存结果"
	rail.prefetchCache = &cachedResult

	err = rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 缓存命中 = %v", err)
	}
	// prefetchCache 不应被新结果替换
	if rail.prefetchCache == nil || *rail.prefetchCache != "缓存结果" {
		t.Error("缓存命中时不应重新 prefetch")
	}
}

func TestExternalMemoryRail_BeforeModelCall_未初始化(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	// 不设置 initialized

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("你好"),
	}, nil)

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 未初始化 = %v", err)
	}
	if rail.prefetchCache != nil {
		t.Error("未初始化时不应执行 prefetch")
	}
}

func TestExternalMemoryRail_AfterInvoke_跳过后台运行(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true, toolSchemas: []ext.ToolSchema{}}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query:   agentinterfaces.InvokeQueryString("测试"),
		Result:  map[string]any{"output": "回复"},
		RunKind: agentinterfaces.RunKindHeartbeat,
	}, nil)

	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterInvoke = %v", err)
	}

	// 等一下确保 goroutine 不会执行
	time.Sleep(50 * time.Millisecond)
	provider.mu.Lock()
	calls := len(provider.syncTurnCalls)
	provider.mu.Unlock()
	if calls != 0 {
		t.Error("心跳运行不应触发 SyncTurn")
	}
}

func TestExternalMemoryRail_AfterInvoke_SyncTurn执行(t *testing.T) {
	provider := &fakeProvider{
		name:        "test",
		available:   true,
		initialized: true,
		toolSchemas: []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query:  agentinterfaces.InvokeQueryString("用户问题"),
		Result: map[string]any{"output": "助手回复"},
	}, nil)

	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterInvoke = %v", err)
	}

	// 等待 goroutine 完成
	time.Sleep(100 * time.Millisecond)
	provider.mu.Lock()
	calls := provider.syncTurnCalls
	provider.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("SyncTurn 调用次数 = %d, want 1", len(calls))
	}
	if calls[0].userMsg != "用户问题" {
		t.Errorf("userMsg = %q, want %q", calls[0].userMsg, "用户问题")
	}
	if calls[0].assistantMsg != "助手回复" {
		t.Errorf("assistantMsg = %q, want %q", calls[0].assistantMsg, "助手回复")
	}
}

func TestExternalMemoryRail_AfterInvoke_熔断器(t *testing.T) {
	provider := &fakeProvider{
		name:        "test",
		available:   true,
		initialized: true,
		syncTurnErr: errors.New("网络错误"),
		toolSchemas: []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	// 连续触发 5 次失败
	for i := 0; i < 5; i++ {
		cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
			Query:  agentinterfaces.InvokeQueryString("测试"),
			Result: map[string]any{"output": "回复"},
		}, nil)
		rail.AfterInvoke(context.Background(), cbc)
		time.Sleep(50 * time.Millisecond) // 等 goroutine
	}

	// 验证熔断器开启
	rail.syncMu.Lock()
	failures := rail.syncConsecutiveFailures
	breakerUntil := rail.syncBreakerUntil
	rail.syncMu.Unlock()

	if failures < externalMemorySyncBreakerThreshold {
		t.Errorf("consecutiveFailures = %d, want >= %d", failures, externalMemorySyncBreakerThreshold)
	}
	if breakerUntil.IsZero() {
		t.Error("熔断器截止时间不应为零")
	}
}

func TestExternalMemoryRail_AfterInvoke_空查询跳过(t *testing.T) {
	provider := &fakeProvider{
		name:        "test",
		available:   true,
		initialized: true,
		toolSchemas: []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	// 空查询
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterInvoke = %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	provider.mu.Lock()
	calls := len(provider.syncTurnCalls)
	provider.mu.Unlock()
	if calls != 0 {
		t.Error("空查询不应触发 SyncTurn")
	}
}

func TestNewExternalMemoryRail_构造函数(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	if rail.provider != provider {
		t.Error("provider 未正确设置")
	}
	if rail.userID != "u1" {
		t.Errorf("userID = %q, want %q", rail.userID, "u1")
	}
	if rail.scopeID != "s1" {
		t.Errorf("scopeID = %q, want %q", rail.scopeID, "s1")
	}
	if rail.sessionID != "sess1" {
		t.Errorf("sessionID = %q, want %q", rail.sessionID, "sess1")
	}
	if rail.Priority() != externalMemoryRailPriority {
		t.Errorf("Priority = %d, want %d", rail.Priority(), externalMemoryRailPriority)
	}
	if rail.initialized != false {
		t.Error("初始状态不应为 initialized")
	}
}

func TestSchemaParamsToParamSlice_完整参数(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "搜索关键词",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "返回数量",
			},
		},
		"required": []any{"query"},
	}
	result := schemaParamsToParamSlice(params)

	if len(result) != 2 {
		t.Fatalf("参数数量 = %d, want 2", len(result))
	}

	// 查找 query 参数
	var queryParam *cschema.Param
	for _, p := range result {
		if p.Name == "query" {
			queryParam = p
			break
		}
	}
	if queryParam == nil {
		t.Fatal("未找到 query 参数")
	}
	if !queryParam.Required {
		t.Error("query 应为 required")
	}
	if queryParam.Description != "搜索关键词" {
		t.Errorf("query description = %q", queryParam.Description)
	}
}

func TestSchemaParamsToParamSlice_空参数(t *testing.T) {
	result := schemaParamsToParamSlice(nil)
	if result != nil {
		t.Error("nil 参数应返回 nil")
	}

	result = schemaParamsToParamSlice(map[string]any{})
	if result != nil {
		t.Error("空参数应返回 nil")
	}
}

func TestParamTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  cschema.ParamType
	}{
		{"string", cschema.ParamTypeString},
		{"boolean", cschema.ParamTypeBoolean},
		{"integer", cschema.ParamTypeInteger},
		{"number", cschema.ParamTypeNumber},
		{"array", cschema.ParamTypeArray},
		{"object", cschema.ParamTypeObject},
		{"unknown", cschema.ParamTypeString},
	}
	for _, tt := range tests {
		got := paramTypeFromString(tt.input)
		if got != tt.want {
			t.Errorf("paramTypeFromString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestProviderTool_Invoke_JSON解析(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	schema := ext.ToolSchema{
		Name:        "search",
		Description: "搜索",
		Parameters:  nil,
	}
	pt := newProviderTool(provider, schema)

	result, err := pt.Invoke(context.Background(), map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("Invoke = %v", err)
	}
	if result["tool"] != "search" {
		t.Errorf("result[tool] = %v, want search", result["tool"])
	}
}

func TestProviderTool_Card(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	schema := ext.ToolSchema{
		Name:        "search",
		Description: "搜索",
		Parameters:  nil,
	}
	pt := newProviderTool(provider, schema)

	card := pt.Card()
	if card.Name != "search" {
		t.Errorf("Card().Name = %q, want %q", card.Name, "search")
	}
	if card.ID != "external_memory_test_search" {
		t.Errorf("Card().ID = %q, want %q", card.ID, "external_memory_test_search")
	}
}

func TestExternalMemoryRail_GetCallbacks(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	callbacks := rail.GetCallbacks()
	if _, ok := callbacks[agentinterfaces.CallbackBeforeInvoke]; !ok {
		t.Error("缺少 BeforeInvoke 回调")
	}
	if _, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]; !ok {
		t.Error("缺少 BeforeModelCall 回调")
	}
	if _, ok := callbacks[agentinterfaces.CallbackAfterInvoke]; !ok {
		t.Error("缺少 AfterInvoke 回调")
	}
}

// ──────────────────────────── Init / Uninit 测试 ────────────────────────────

// setupTestRunner 确保全局 Runner 已初始化（含 ResourceMgr）
func setupTestRunner() {
	// 触发 initRunner() 懒初始化
	_ = runner.GetResourceMgr()
}

func TestExternalMemoryRail_Init_注册工具和注入提示词(t *testing.T) {
	setupTestRunner()

	provider := &fakeProvider{
		name:      "test",
		available: true,
		toolSchemas: []ext.ToolSchema{
			{Name: "search", Description: "搜索记忆", Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "搜索关键词"},
				},
				"required": []any{"query"},
			}},
		},
	}
	// BaseMemoryProvider 嵌入后 SystemPromptBlock 返回空串，用一个自定义 provider 测试注入
	providerWithPrompt := &fakeProviderWithPrompt{
		fakeProvider: *provider,
		promptBlock:  "记住用户偏好",
	}

	rail := NewExternalMemoryRail(providerWithPrompt, "u1", "s1", "sess1")
	agent := newFakeBaseAgentForMemoryRail()

	err := rail.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init = %v", err)
	}

	// 验证 systemPromptBuilder 被设置
	if rail.systemPromptBuilder == nil {
		t.Error("Init 后 systemPromptBuilder 不应为 nil")
	}

	// 验证工具注册到 ability_manager
	if !agent.am.addedAbilities["search"] {
		t.Error("search 工具应注册到 ability_manager")
	}

	// 验证 ownedToolNames
	if _, ok := rail.ownedToolNames["search"]; !ok {
		t.Error("search 应在 ownedToolNames 中")
	}

	// 验证 ownedToolIDs
	toolID := "external_memory_test_search"
	if _, ok := rail.ownedToolIDs[toolID]; !ok {
		t.Errorf("工具 ID %q 应在 ownedToolIDs 中", toolID)
	}
}

func TestExternalMemoryRail_Uninit_注销工具和关闭Provider(t *testing.T) {
	setupTestRunner()

	provider := &fakeProvider{
		name:      "test",
		available: true,
		toolSchemas: []ext.ToolSchema{
			{Name: "search", Description: "搜索记忆", Parameters: nil},
		},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	agent := newFakeBaseAgentForMemoryRail()

	// 先 Init
	_ = rail.Init(context.Background(), agent)
	rail.initialized = true

	// Uninit
	err := rail.Uninit(agent)
	if err != nil {
		t.Fatalf("Uninit = %v", err)
	}

	// 验证状态清理
	if rail.initialized {
		t.Error("Uninit 后 initialized 应为 false")
	}
	if len(rail.ownedToolNames) != 0 {
		t.Error("Uninit 后 ownedToolNames 应为空")
	}
	if len(rail.ownedToolIDs) != 0 {
		t.Error("Uninit 后 ownedToolIDs 应为空")
	}

	// 验证 Provider Shutdown 被调用
	provider.mu.Lock()
	shutdownCalled := provider.shutdownCalled
	provider.mu.Unlock()
	if !shutdownCalled {
		t.Error("Provider Shutdown 应被调用")
	}
}

// ──────────────────────────── providerTool 补充测试 ────────────────────────────

func TestProviderTool_Stream_不支持(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	schema := ext.ToolSchema{Name: "search", Description: "搜索"}
	pt := newProviderTool(provider, schema)

	_, err := pt.Stream(context.Background(), map[string]any{})
	if err != tool.ErrStreamNotSupported {
		t.Errorf("Stream err = %v, want ErrStreamNotSupported", err)
	}
}

func TestProviderTool_Invoke_HandleToolCall错误(t *testing.T) {
	provider := &fakeProviderErrHandle{
		name:   "test",
		errMsg: "工具调用失败",
	}
	schema := ext.ToolSchema{Name: "search", Description: "搜索"}
	pt := newProviderTool(provider, schema)

	_, err := pt.Invoke(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Error("HandleToolCall 错误时应返回 error")
	}
	if err.Error() != "工具调用失败" {
		t.Errorf("Invoke err = %v, want 工具调用失败", err)
	}
}

// ──────────────────────────── BeforeInvoke 补充测试 ────────────────────────────

func TestExternalMemoryRail_BeforeInvoke_初始化失败降级(t *testing.T) {
	provider := &fakeProviderInitErr{
		name: "test",
		err:  errors.New("网络不可达"),
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	err := rail.BeforeInvoke(context.Background(), cbc)
	// 不应返回错误，降级运行
	if err != nil {
		t.Fatalf("BeforeInvoke = %v, want nil（降级运行）", err)
	}
	if rail.initialized {
		t.Error("初始化失败时 initialized 不应为 true")
	}
}

// ──────────────────────────── AfterInvoke 补充测试 ────────────────────────────

func TestExternalMemoryRail_AfterInvoke_SyncTurn成功重置失败计数(t *testing.T) {
	provider := &fakeProvider{
		name:        "test",
		available:   true,
		initialized: true,
		toolSchemas: []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	// 先设一些失败计数
	rail.syncConsecutiveFailures = 3

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query:  agentinterfaces.InvokeQueryString("用户问题"),
		Result: map[string]any{"output": "助手回复"},
	}, nil)

	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterInvoke = %v", err)
	}

	// 等待 goroutine 完成
	time.Sleep(100 * time.Millisecond)

	rail.syncMu.Lock()
	failures := rail.syncConsecutiveFailures
	rail.syncMu.Unlock()

	if failures != 0 {
		t.Errorf("SyncTurn 成功后 consecutiveFailures = %d, want 0", failures)
	}
}

func TestExternalMemoryRail_AfterInvoke_熔断器冷却后重置(t *testing.T) {
	provider := &fakeProvider{
		name:        "test",
		available:   true,
		initialized: true,
		toolSchemas: []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	// 设置熔断器已过冷却期
	rail.syncConsecutiveFailures = externalMemorySyncBreakerThreshold
	rail.syncBreakerUntil = time.Now().Add(-1 * time.Second) // 已过冷却

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query:  agentinterfaces.InvokeQueryString("测试"),
		Result: map[string]any{"output": "回复"},
	}, nil)

	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterInvoke = %v", err)
	}

	// 冷却完毕应重置失败计数
	rail.syncMu.Lock()
	failures := rail.syncConsecutiveFailures
	rail.syncMu.Unlock()
	// 注意：goroutine 完成后 successes 重置为 0，但在 goroutine 启动前已重置为 0
	if failures >= externalMemorySyncBreakerThreshold {
		t.Error("冷却完毕后失败计数应被重置")
	}
}

// ──────────────────────────── injectMemoryContext 补充测试 ────────────────────────────

func TestInjectMemoryContext_空上下文(t *testing.T) {
	rail := NewExternalMemoryRail(&fakeProvider{name: "test"}, "u1", "s1", "sess1")
	rail.systemPromptBuilder = &noopSystemPromptBuilder{}

	// 空字符串不应注入
	rail.injectMemoryContext("")
	// noopSystemPromptBuilder 的 AddSection 不会被调用（因为 rawContext 为空）
}

func TestInjectMemoryContext_NilBuilder(t *testing.T) {
	rail := NewExternalMemoryRail(&fakeProvider{name: "test"}, "u1", "s1", "sess1")
	rail.systemPromptBuilder = nil

	// nil builder 不应 panic
	rail.injectMemoryContext("some context")
}

// ──────────────────────────── BeforeModelCall 补充测试 ────────────────────────────

func TestExternalMemoryRail_BeforeModelCall_Prefetch超时(t *testing.T) {
	provider := &fakeProvider{
		name:           "test",
		available:      true,
		prefetchResult: "",
		prefetchErr:    context.DeadlineExceeded,
		prefetchDelay:  10 * time.Second, // 超过 5s 超时
		toolSchemas:    []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true
	rail.systemPromptBuilder = &noopSystemPromptBuilder{}

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("你好"),
	}, nil)

	// 应不返回错误，降级运行
	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall prefetch 超时 = %v", err)
	}
}

// ──────────────────────────── 辅助测试类型 ────────────────────────────

// fakeProviderWithPrompt 带自定义 SystemPromptBlock 的 fakeProvider
type fakeProviderWithPrompt struct {
	fakeProvider
	promptBlock string
}

func (f *fakeProviderWithPrompt) SystemPromptBlock() string { return f.promptBlock }

// fakeProviderInitErr Initialize 返回错误的 fakeProvider
type fakeProviderInitErr struct {
	ext.BaseMemoryProvider
	name string
	err  error
}

func (f *fakeProviderInitErr) Name() string      { return f.name }
func (f *fakeProviderInitErr) IsAvailable() bool { return true }
func (f *fakeProviderInitErr) Initialize(_ context.Context, _ ...ext.ProviderOption) error {
	return f.err
}
func (f *fakeProviderInitErr) GetToolSchemas() []ext.ToolSchema { return nil }
func (f *fakeProviderInitErr) HandleToolCall(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", nil
}
func (f *fakeProviderInitErr) Prefetch(_ context.Context, _ string, _ ...ext.ProviderOption) (string, error) {
	return "", nil
}
func (f *fakeProviderInitErr) SyncTurn(_ context.Context, _, _ string, _ ...ext.ProviderOption) error {
	return nil
}

// fakeProviderErrHandle HandleToolCall 返回错误的 fakeProvider
type fakeProviderErrHandle struct {
	ext.BaseMemoryProvider
	name   string
	errMsg string
}

func (f *fakeProviderErrHandle) Name() string      { return f.name }
func (f *fakeProviderErrHandle) IsAvailable() bool { return true }
func (f *fakeProviderErrHandle) Initialize(_ context.Context, _ ...ext.ProviderOption) error {
	return nil
}
func (f *fakeProviderErrHandle) GetToolSchemas() []ext.ToolSchema { return nil }
func (f *fakeProviderErrHandle) HandleToolCall(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", errors.New(f.errMsg)
}
func (f *fakeProviderErrHandle) Prefetch(_ context.Context, _ string, _ ...ext.ProviderOption) (string, error) {
	return "", nil
}
func (f *fakeProviderErrHandle) SyncTurn(_ context.Context, _, _ string, _ ...ext.ProviderOption) error {
	return nil
}
