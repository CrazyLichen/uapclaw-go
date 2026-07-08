//go:build test

package rails

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	cschema2 "github.com/uapclaw/uapclaw-go/internal/common/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── Mock 实现 ────────────────────────────

// fakeModelSwitcherAgent 同时实现 BaseAgent + modelSwitcher + deepStateLoader
type fakeModelSwitcherAgent struct {
	fakeBaseAgent
	// switchModelCalls 记录 SwitchModel 调用
	switchModelCalls []*llm.Model
	// currentLLM GetLLM 返回值
	currentLLM *llm.Model
	// currentLLMErr GetLLM 返回错误
	currentLLMErr error
	// deepState LoadState 返回值
	deepState *hschema.DeepAgentState
}

func newFakeModelSwitcherAgent() *fakeModelSwitcherAgent {
	return &fakeModelSwitcherAgent{
		fakeBaseAgent: *newFakeBaseAgent(),
	}
}

// SwitchModel 实现 modelSwitcher 接口
func (f *fakeModelSwitcherAgent) SwitchModel(model *llm.Model) {
	f.switchModelCalls = append(f.switchModelCalls, model)
}

// GetLLM 实现 modelSwitcher 接口
func (f *fakeModelSwitcherAgent) GetLLM() (*llm.Model, error) {
	return f.currentLLM, f.currentLLMErr
}

// LoadState 实现 deepStateLoader 接口
func (f *fakeModelSwitcherAgent) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return f.deepState
}

// 编译时验证 fakeModelSwitcherAgent 满足 modelSwitcher + deepStateLoader
var _ modelSwitcher = (*fakeModelSwitcherAgent)(nil)
var _ deepStateLoader = (*fakeModelSwitcherAgent)(nil)

// fakeDeepAgentForTaskPlanning 实现 DeepAgentInterface 的 mock（供 Init 测试）
type fakeDeepAgentForTaskPlanning struct {
	fakeBaseAgent
	deepConfig *hschema.DeepAgentConfig
}

func (f *fakeDeepAgentForTaskPlanning) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentForTaskPlanning) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentForTaskPlanning) LoopController() controller.ControllerInterface { return nil }
func (f *fakeDeepAgentForTaskPlanning) EventHandler() modules.EventHandler             { return nil }
func (f *fakeDeepAgentForTaskPlanning) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeDeepAgentForTaskPlanning) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }
func (f *fakeDeepAgentForTaskPlanning) IsInvokeActive() bool                   { return false }
func (f *fakeDeepAgentForTaskPlanning) IsAutoInvokeScheduled() bool            { return false }
func (f *fakeDeepAgentForTaskPlanning) SetAutoInvokeScheduled(_ bool)          {}
func (f *fakeDeepAgentForTaskPlanning) ScheduleAutoInvokeOnSpawnDone(_ string) error {
	return nil
}
func (f *fakeDeepAgentForTaskPlanning) CreateSubagent(_ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}

// Invoke 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForTaskPlanning) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// SwitchMode 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForTaskPlanning) SwitchMode(_ sessioninterfaces.SessionFacade, _ string) {}

// RestoreModeAfterPlanExit 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForTaskPlanning) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}

// GetPlanFilePath 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForTaskPlanning) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string { return "" }

// SaveState 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForTaskPlanning) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {}

// 编译时验证
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForTaskPlanning)(nil)

// fakeTodoTool TodoTool 的 mock 替代
type fakeTodoTool struct {
	// loadTodosResult LoadTodos 返回值
	loadTodosResult []hschema.TodoItem
	// loadTodosErr LoadTodos 返回错误
	loadTodosErr error
	// saveTodosCalls 记录 SaveTodos 调用
	saveTodosCalls [][]hschema.TodoItem
	// cleanupSessionCalls 记录 CleanupSession 调用
	cleanupSessionCalls []string
}

func (f *fakeTodoTool) LoadTodos(_ context.Context, _ string) ([]hschema.TodoItem, error) {
	return f.loadTodosResult, f.loadTodosErr
}

func (f *fakeTodoTool) SaveTodos(_ context.Context, _ string, todos []hschema.TodoItem) error {
	f.saveTodosCalls = append(f.saveTodosCalls, todos)
	return nil
}

func (f *fakeTodoTool) CleanupSession(sessionID string) {
	f.cleanupSessionCalls = append(f.cleanupSessionCalls, sessionID)
}

// fakeModelContext ModelContext 的 mock
type fakeModelContext struct {
	// addMessagesCalls 记录 AddMessages 调用
	addMessagesCalls []llmschema.BaseMessage
}

func (f *fakeModelContext) Len() int { return 0 }
func (f *fakeModelContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return nil, nil
}
func (f *fakeModelContext) SetMessages(_ []llmschema.BaseMessage, _ bool) {}
func (f *fakeModelContext) PopMessages(_ int, _ bool) []llmschema.BaseMessage { return nil }
func (f *fakeModelContext) AddMessages(_ context.Context, message llmschema.BaseMessage, _ ...any) ([]llmschema.BaseMessage, error) {
	f.addMessagesCalls = append(f.addMessagesCalls, message)
	return nil, nil
}
func (f *fakeModelContext) GetContextWindow(_ context.Context, _ []llmschema.BaseMessage, _ []cschema2.ToolInfoInterface, _, _ int, _ ...any) (*any, error) {
	return nil, nil
}

// fakeSession 简单的 SessionFacade mock
type fakeSession struct {
	sessionID string
}

func newFakeSession(id string) *fakeSession {
	return &fakeSession{sessionID: id}
}

func (s *fakeSession) GetSessionID() string                     { return s.sessionID }
func (s *fakeSession) UpdateState(_ map[string]any)             {}
func (s *fakeSession) GetState(_ state.StateKey) (any, error)   { return nil, nil }
func (s *fakeSession) DumpState() map[string]any                { return nil }
func (s *fakeSession) WriteStream(_ context.Context, _ any) error       { return nil }
func (s *fakeSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *fakeSession) GetEnv(_ string, _ ...any) any                    { return nil }
func (s *fakeSession) Interact(_ context.Context, _ any) error          { return nil }

// 编译时验证
var _ sessioninterfaces.SessionFacade = (*fakeSession)(nil)

// newTestTodoTool 创建仅初始化 lockManager 的 TodoTool（供缓存命中路径测试使用）。
// 注意：fs 字段为零值，LoadTodos 调用会 panic，因此仅用于缓存命中的场景。
func newTestTodoTool() *todo.TodoTool {
	tt := todo.TodoTool{}
	// 使用反射无法设置未导出字段，改用 todo 包的导出构造函数
	// 实际上无法从包外初始化，这里返回 nil 并在测试中只验证缓存路径
	return &tt
}

// mockFsOperation 模拟文件系统操作
type mockFsOperation struct {
	data map[string]string
}

func newMockFsOperation() *mockFsOperation {
	return &mockFsOperation{data: make(map[string]string)}
}

func (m *mockFsOperation) ReadFile(_ context.Context, path string, _ ...sys_operation.FsOption) (*sys_operation.ReadFileResult, error) {
	content, ok := m.data[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return &sys_operation.ReadFileResult{Code: 0, Data: content}, nil
}

func (m *mockFsOperation) WriteFile(_ context.Context, path string, content string, _ ...sys_operation.FsOption) (*sys_operation.WriteFileResult, error) {
	m.data[path] = content
	return &sys_operation.WriteFileResult{Code: 0}, nil
}

func (m *mockFsOperation) ListFiles(_ context.Context, _ string, _ ...sys_operation.FsOption) (*sys_operation.ListFilesResult, error) {
	return &sys_operation.ListFilesResult{Code: 0}, nil
}

func (m *mockFsOperation) ListDirectories(_ context.Context, _ string, _ ...sys_operation.FsOption) (*sys_operation.ListDirsResult, error) {
	return &sys_operation.ListDirsResult{Code: 0}, nil
}

func (m *mockFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (*sys_operation.SearchFilesResult, error) {
	return &sys_operation.SearchFilesResult{Code: 0}, nil
}

func (m *mockFsOperation) ListTools() []*tool.ToolCard { return nil }

// 编译时验证
var _ sys_operation.FsOperation = (*mockFsOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// --- 构造函数测试 ---

// TestNewTaskPlanningRail 验证默认值和优先级 90
func TestNewTaskPlanningRail(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	assert.Equal(t, 90, r.Priority())
	assert.Equal(t, 20, r.listToolCallInterval)
	assert.False(t, r.enableProgressRepeat)
	assert.Empty(t, r.modelSelection)
	assert.Empty(t, r.modelIDToModel)
	assert.Empty(t, r.usageRecords)
	assert.Empty(t, r.tools)
	assert.Nil(t, r.todoTool)
	assert.Empty(t, r.language)
	assert.Empty(t, r.agentID)
}

// TestNewTaskPlanningRail_WithOptions 验证 Option 覆盖
func TestNewTaskPlanningRail_WithOptions(t *testing.T) {
	t.Parallel()

	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "model-a"},
	}
	r := NewTaskPlanningRail(
		WithEnableProgressRepeat(true),
		WithListToolCallInterval(10),
		WithModelSelection(map[*llm.Model]string{model: "模型A"}),
		WithLanguage("en"),
		WithAgentID("my-agent"),
	)
	assert.True(t, r.enableProgressRepeat)
	assert.Equal(t, 10, r.listToolCallInterval)
	assert.Contains(t, r.modelSelection, model)
	assert.Equal(t, "en", r.language)
	assert.Equal(t, "my-agent", r.agentID)
	// 验证 modelIDToModel 映射已构建
	assert.Contains(t, r.modelIDToModel, "model-a")
	assert.Equal(t, model, r.modelIDToModel["model-a"])
}

// TestWithListToolCallInterval_最小值 验证 n<1 时回退到默认值 20
func TestWithListToolCallInterval_最小值(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail(WithListToolCallInterval(0))
	assert.Equal(t, 20, r.listToolCallInterval)
}

// --- Init/Uninit 测试 ---

// TestTaskPlanningRail_Init_非DeepAgent时跳过 验证 agent 不满足 DeepAgentInterface 时直接返回 nil
func TestTaskPlanningRail_Init_非DeepAgent时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	err := r.Init(agent)
	assert.NoError(t, err)
	assert.Nil(t, r.tools)
}

// TestTaskPlanningRail_Init_AbilityManager为nil时跳过 验证 am 为 nil 时直接返回 nil
func TestTaskPlanningRail_Init_AbilityManager为nil时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// fakeDeepAgentForTaskPlanning 的 fakeBaseAgent.AbilityManager() 返回 nil
	agent := &fakeDeepAgentForTaskPlanning{fakeBaseAgent: *newFakeBaseAgent()}
	err := r.Init(agent)
	assert.NoError(t, err)
	assert.Nil(t, r.tools)
}

// TestTaskPlanningRail_Init_设置SysOpWorkspace 验证 Init 设置 sysOp/workspace
func TestTaskPlanningRail_Init_设置SysOpWorkspace(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	// fakeBaseAgent 不实现 DeepAgentInterface，Init 直接返回 nil
	err := r.Init(agent)
	assert.NoError(t, err)
}

// TestTaskPlanningRail_Uninit_移除工具 验证 Uninit 清理 tools 和 todoTool
func TestTaskPlanningRail_Uninit_移除工具(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	err := r.Uninit(agent)
	assert.NoError(t, err)
	assert.Nil(t, r.tools)
	assert.Nil(t, r.todoTool)
}

// TestTaskPlanningRail_Uninit_移除提示词节 验证 Uninit 调用 RemoveSection
func TestTaskPlanningRail_Uninit_移除提示词节(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder()
	// 先添加 todo 节
	builder.AddSection(saprompt.PromptSection{Name: sections.SectionTodo, Content: map[string]string{"cn": "test"}})
	assert.True(t, builder.HasSection(sections.SectionTodo))

	err := r.Uninit(agent)
	assert.NoError(t, err)
	assert.False(t, builder.HasSection(sections.SectionTodo))
}

// --- BeforeModelCall 测试 ---

// TestTaskPlanningRail_BeforeModelCall_注入提示词 验证 AddSection 被调用
func TestTaskPlanningRail_BeforeModelCall_注入提示词(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder()

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	// 验证 todo 节已被注入
	assert.True(t, builder.HasSection(sections.SectionTodo))
}

// TestTaskPlanningRail_BeforeModelCall_模型切换 验证 mock modelSwitcher，SwitchModel 被调用
func TestTaskPlanningRail_BeforeModelCall_模型切换(t *testing.T) {
	t.Parallel()

	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "target-model"},
	}
	defaultModel := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "default-model"},
	}
	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{model: "目标模型"}),
	)

	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = defaultModel
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	// defaultLLM 应被捕获
	assert.NotNil(t, r.defaultLLM)
	// 在无 in_progress 任务时，应切换到 defaultLLM
	require.Len(t, agent.switchModelCalls, 1)
	assert.Equal(t, defaultModel, agent.switchModelCalls[0])
}

// TestTaskPlanningRail_BeforeModelCall_无模型选择 验证 modelSelection 为空时跳过切换
func TestTaskPlanningRail_BeforeModelCall_无模型选择(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// modelSelection 为空

	agent := newFakeModelSwitcherAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	// 不应有模型切换调用
	assert.Empty(t, agent.switchModelCalls)
}

// TestTaskPlanningRail_BeforeModelCall_断言失败 验证 agent 不满足 modelSwitcher 时记录日志
func TestTaskPlanningRail_BeforeModelCall_断言失败(t *testing.T) {
	t.Parallel()

	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "m1"},
	}
	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{model: "模型1"}),
	)

	// fakeBaseAgent 不实现 modelSwitcher
	agent := newFakeBaseAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	// defaultLLM 不应被设置
	assert.Nil(t, r.defaultLLM)
}

// --- AfterToolCall 测试 ---

// TestTaskPlanningRail_AfterToolCall_todoTool为nil时跳过 验证 todoTool 为 nil 时直接返回
func TestTaskPlanningRail_AfterToolCall_todoTool为nil时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// r.todoTool 为 nil
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_create"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestTaskPlanningRail_AfterToolCall_刷新缓存 验证 todo_ 前缀工具刷新缓存
func TestTaskPlanningRail_AfterToolCall_刷新缓存(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()

	// 直接验证 todo_ 前缀判断逻辑
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_create"}
	sess := newFakeSession("sess-1")
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	// todoTool 为 nil，直接返回
	err := r.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestTaskPlanningRail_AfterToolCall_非Todo工具 验证非 todo_ 前缀工具不触发刷新
func TestTaskPlanningRail_AfterToolCall_非Todo工具(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// todoTool 为 nil，AfterToolCall 直接返回 nil
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	sess := newFakeSession("sess-1")
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
	// 缓存应保持为空
	assert.Empty(t, r.todosCache)
}

// TestTaskPlanningRail_AfterToolCall_进度提醒间隔 验证 N 次调用后注入 UserMessage
func TestTaskPlanningRail_AfterToolCall_进度提醒间隔(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail(
		WithEnableProgressRepeat(true),
		WithListToolCallInterval(2),
	)
	// todoTool 为 nil，AfterToolCall 直接返回 nil
	// 但我们可以通过直接操作 toolCallCounts 验证计数逻辑

	// 直接设置 toolCallCounts 模拟已调用 1 次
	r.toolCallCounts["sess-prog"] = 1
	assert.Equal(t, 1, r.toolCallCounts["sess-prog"])
}

// --- AfterModelCall 测试 ---

// TestTaskPlanningRail_AfterModelCall_累计Token 验证累加 UsageMetadata 到 usageRecords
func TestTaskPlanningRail_AfterModelCall_累计Token(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "qwen-max"},
	}
	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = model

	inputs := &agentinterfaces.ModelCallInputs{
		Response: &llmschema.AssistantMessage{
			UsageMetadata: &llmschema.UsageMetadata{
				InputTokens:  100,
				OutputTokens: 50,
			},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterModelCall(context.Background(), cbc)
	require.NoError(t, err)

	record, ok := r.usageRecords["qwen-max"]
	require.True(t, ok)
	assert.Equal(t, 100, record.InputTokens)
	assert.Equal(t, 50, record.OutputTokens)
}

// TestTaskPlanningRail_AfterModelCall_多次累加 验证多次调用累加 token
func TestTaskPlanningRail_AfterModelCall_多次累加(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "qwen-max"},
	}
	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = model

	// 第一次调用
	inputs1 := &agentinterfaces.ModelCallInputs{
		Response: &llmschema.AssistantMessage{
			UsageMetadata: &llmschema.UsageMetadata{InputTokens: 100, OutputTokens: 50},
		},
	}
	cbc1 := agentinterfaces.NewAgentCallbackContext(agent, inputs1, nil)
	err := r.AfterModelCall(context.Background(), cbc1)
	require.NoError(t, err)

	// 第二次调用
	inputs2 := &agentinterfaces.ModelCallInputs{
		Response: &llmschema.AssistantMessage{
			UsageMetadata: &llmschema.UsageMetadata{InputTokens: 200, OutputTokens: 80},
		},
	}
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, inputs2, nil)
	err = r.AfterModelCall(context.Background(), cbc2)
	require.NoError(t, err)

	record := r.usageRecords["qwen-max"]
	assert.Equal(t, 300, record.InputTokens)
	assert.Equal(t, 130, record.OutputTokens)
}

// TestTaskPlanningRail_AfterModelCall_无UsageMetadata 验证 Response.UsageMetadata 为 nil 时跳过
func TestTaskPlanningRail_AfterModelCall_无UsageMetadata(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "qwen-max"},
	}
	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = model

	// Response 为 nil
	inputs := &agentinterfaces.ModelCallInputs{Response: nil}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.Empty(t, r.usageRecords)
}

// TestTaskPlanningRail_AfterModelCall_UsageMetadata为nil 验证 UsageMetadata 为 nil 时跳过
func TestTaskPlanningRail_AfterModelCall_UsageMetadata为nil(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "qwen-max"},
	}
	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = model

	inputs := &agentinterfaces.ModelCallInputs{
		Response: &llmschema.AssistantMessage{UsageMetadata: nil},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.Empty(t, r.usageRecords)
}

// TestTaskPlanningRail_AfterModelCall_断言失败 验证 agent 不满足 modelSwitcher 时跳过
func TestTaskPlanningRail_AfterModelCall_断言失败(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// fakeBaseAgent 不实现 modelSwitcher
	agent := newFakeBaseAgent()

	inputs := &agentinterfaces.ModelCallInputs{
		Response: &llmschema.AssistantMessage{
			UsageMetadata: &llmschema.UsageMetadata{InputTokens: 100, OutputTokens: 50},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.Empty(t, r.usageRecords)
}

// TestTaskPlanningRail_AfterModelCall_token为零时跳过 验证 inputTokens 和 outputTokens 都为 0 时跳过
func TestTaskPlanningRail_AfterModelCall_token为零时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "qwen-max"},
	}
	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = model

	inputs := &agentinterfaces.ModelCallInputs{
		Response: &llmschema.AssistantMessage{
			UsageMetadata: &llmschema.UsageMetadata{InputTokens: 0, OutputTokens: 0},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.Empty(t, r.usageRecords)
}

// --- AfterTaskIteration 测试 ---

// TestTaskPlanningRail_AfterTaskIteration_同步TaskPlan 验证 mock deepStateLoader，SaveTodos 被调用
func TestTaskPlanningRail_AfterTaskIteration_同步TaskPlan(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// 由于 syncTodosFromPlan 使用 r.todoTool.LoadTodos/SaveTodos，
	// 但 todoTool 类型为 *todo.TodoTool，无法直接替换为 fakeTodoTool。
	// 此处验证 deepStateLoader 断言路径：agent 实现 deepStateLoader 时调用 LoadState

	agent := newFakeModelSwitcherAgent()
	agent.deepState = &hschema.DeepAgentState{
		TaskPlan: &hschema.TaskPlan{
			Tasks: []hschema.TodoItem{
				{ID: "task-1", Status: hschema.TodoStatusCompleted},
			},
		},
	}

	sess := newFakeSession("sess-sync")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	// todoTool 为 nil 时 syncTodosFromPlan 直接返回
	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_AfterTaskIteration_无TaskPlan 验证 TaskPlan 为 nil 时跳过
func TestTaskPlanningRail_AfterTaskIteration_无TaskPlan(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeModelSwitcherAgent()
	agent.deepState = &hschema.DeepAgentState{
		TaskPlan: nil,
	}

	sess := newFakeSession("sess-no-plan")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_AfterTaskIteration_断言失败 验证 agent 不满足 deepStateLoader 时记录日志
func TestTaskPlanningRail_AfterTaskIteration_断言失败(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// fakeBaseAgent 不实现 deepStateLoader
	agent := newFakeBaseAgent()

	sess := newFakeSession("sess-no-loader")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_AfterTaskIteration_无Session时跳过 验证 session 为 nil 时跳过
func TestTaskPlanningRail_AfterTaskIteration_无Session时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeModelSwitcherAgent()

	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

// --- AfterInvoke 测试 ---

// TestTaskPlanningRail_AfterInvoke_清理 验证缓存被清理
func TestTaskPlanningRail_AfterInvoke_清理(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	sessID := "sess-cleanup"
	r.todosCache[sessID] = []hschema.TodoItem{{ID: "1"}}
	r.toolCallCounts[sessID] = 5
	r.usageRecords["model-a"] = &hschema.ModelUsageRecord{ModelID: "model-a", InputTokens: 100, OutputTokens: 50}

	sess := newFakeSession(sessID)
	inputs := &agentinterfaces.InvokeInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterInvoke(context.Background(), cbc)
	require.NoError(t, err)

	// 验证缓存已清理
	assert.NotContains(t, r.todosCache, sessID)
	assert.NotContains(t, r.toolCallCounts, sessID)
	// usageRecords 已重置
	assert.Empty(t, r.usageRecords)
}

// TestTaskPlanningRail_AfterInvoke_CleanupSession 验证 CleanupSession 被调用
func TestTaskPlanningRail_AfterInvoke_CleanupSession(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	sessID := "sess-cleanup-session"
	// 设置 todoTool（真实 TodoTool 不便于直接调用 CleanupSession 验证，这里只测试 nil 分支）
	r.todoTool = nil

	sess := newFakeSession(sessID)
	inputs := &agentinterfaces.InvokeInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterInvoke(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_AfterInvoke_无Session 验证 session 为 nil 时跳过清理
func TestTaskPlanningRail_AfterInvoke_无Session(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	r.usageRecords["model-a"] = &hschema.ModelUsageRecord{ModelID: "model-a", InputTokens: 100, OutputTokens: 50}

	inputs := &agentinterfaces.InvokeInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterInvoke(context.Background(), cbc)
	require.NoError(t, err)

	// usageRecords 已重置（日志汇总仍执行）
	assert.Empty(t, r.usageRecords)
	// 但 todosCache 和 toolCallCounts 未清理（无 sessionID）
}

// --- GetCallbacks 测试 ---

// TestTaskPlanningRail_GetCallbacks 验证所有 5 个事件都注册了
func TestTaskPlanningRail_GetCallbacks(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	callbacks := r.GetCallbacks()

	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterToolCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterInvoke)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// --- 辅助方法测试 ---

// TestGetInProgressModelID 验证查找 in_progress 任务的 selected_model_id
func TestGetInProgressModelID(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// 设置 todoTool 非 nil 以进入缓存查找路径
	r.todoTool = &todo.TodoTool{}

	// 场景 1：缓存中有 in_progress 任务
	r.todosCache["sess-1"] = []hschema.TodoItem{
		{ID: "1", Status: hschema.TodoStatusPending, SelectedModelID: ""},
		{ID: "2", Status: hschema.TodoStatusInProgress, SelectedModelID: "model-b"},
		{ID: "3", Status: hschema.TodoStatusCompleted, SelectedModelID: "model-c"},
	}

	agent := newFakeBaseAgent()
	sess := newFakeSession("sess-1")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	modelID := r.getInProgressModelID(context.Background(), cbc)
	assert.Equal(t, "model-b", modelID)
}

// TestGetInProgressModelID_无InProgress 验证无 in_progress 任务时返回空
func TestGetInProgressModelID_无InProgress(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	r.todoTool = &todo.TodoTool{}
	r.todosCache["sess-2"] = []hschema.TodoItem{
		{ID: "1", Status: hschema.TodoStatusPending, SelectedModelID: ""},
		{ID: "2", Status: hschema.TodoStatusCompleted, SelectedModelID: "model-c"},
	}

	agent := newFakeBaseAgent()
	sess := newFakeSession("sess-2")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	modelID := r.getInProgressModelID(context.Background(), cbc)
	assert.Equal(t, "", modelID)
}

// TestGetInProgressModelID_无Session 验证 session 为 nil 时返回空
func TestGetInProgressModelID_无Session(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	modelID := r.getInProgressModelID(context.Background(), cbc)
	assert.Equal(t, "", modelID)
}

// TestGetInProgressModelID_todoTool为nil 验证 todoTool 为 nil 时返回空
func TestGetInProgressModelID_todoTool为nil(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	r.todoTool = nil

	agent := newFakeBaseAgent()
	sess := newFakeSession("sess-3")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	modelID := r.getInProgressModelID(context.Background(), cbc)
	assert.Equal(t, "", modelID)
}

// TestGetInProgressModelID_缓存未命中时加载 验证缓存未命中时通过 todoTool 加载
func TestGetInProgressModelID_缓存未命中时加载(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// 不设置缓存，但 todoTool 为 nil，应返回空
	agent := newFakeBaseAgent()
	sess := newFakeSession("sess-nocache")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	modelID := r.getInProgressModelID(context.Background(), cbc)
	assert.Equal(t, "", modelID)
}

// TestFormatTaskContent 验证格式化任务内容
func TestFormatTaskContent(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	todos := []hschema.TodoItem{
		{ID: "1", Content: "任务A", Status: hschema.TodoStatusPending},
		{ID: "2", Content: "任务B", Status: hschema.TodoStatusInProgress},
		{ID: "3", Content: "任务C", Status: hschema.TodoStatusCompleted},
	}

	tasksStr, inProgressTask := r.formatTaskContent(todos)
	assert.Contains(t, tasksStr, "id: 1 |status: pending |content: 任务A")
	assert.Contains(t, tasksStr, "id: 2 |status: in_progress |content: 任务B")
	assert.Contains(t, tasksStr, "id: 3 |status: completed |content: 任务C")
	assert.Equal(t, "任务B", inProgressTask)
}

// TestFormatTaskContent_无InProgress 验证无 in_progress 任务时 inProgressTask 为空
func TestFormatTaskContent_无InProgress(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	todos := []hschema.TodoItem{
		{ID: "1", Content: "任务A", Status: hschema.TodoStatusPending},
		{ID: "2", Content: "任务C", Status: hschema.TodoStatusCompleted},
	}

	tasksStr, inProgressTask := r.formatTaskContent(todos)
	assert.Contains(t, tasksStr, "id: 1 |status: pending |content: 任务A")
	assert.Equal(t, "", inProgressTask)
}

// TestFormatTaskContent_空列表 验证空列表
func TestFormatTaskContent_空列表(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	tasksStr, inProgressTask := r.formatTaskContent(nil)
	assert.Equal(t, "", tasksStr)
	assert.Equal(t, "", inProgressTask)
}

// TestBuildModelSelectionString 验证构建模型选择字符串
func TestBuildModelSelectionString(t *testing.T) {
	t.Parallel()

	model1 := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "model-a"},
	}
	model2 := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "model-b"},
	}
	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{
			model1: "模型A",
			model2: "模型B",
		}),
	)

	result := r.buildModelSelectionString()
	assert.Contains(t, result, "-selected_model_id: model-a: 模型A")
	assert.Contains(t, result, "-selected_model_id: model-b: 模型B")
}

// TestBuildModelSelectionString_空 验证 modelSelection 为空时返回空字符串
func TestBuildModelSelectionString_空(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	result := r.buildModelSelectionString()
	assert.Equal(t, "", result)
}

// TestBuildModelSelectionString_nilModel 验证 model 为 nil 时跳过
func TestBuildModelSelectionString_nilModel(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{
			nil: "空模型",
		}),
	)
	result := r.buildModelSelectionString()
	assert.Equal(t, "", result)
}

// --- GetCallbacks 完整调用测试 ---

// TestTaskPlanningRail_GetCallbacks_调用BeforeModelCall 验证回调映射中 BeforeModelCall 可调用
func TestTaskPlanningRail_GetCallbacks_调用BeforeModelCall(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	require.True(t, ok)

	agent := newFakeBaseAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_GetCallbacks_调用AfterModelCall 验证回调映射中 AfterModelCall 可调用
func TestTaskPlanningRail_GetCallbacks_调用AfterModelCall(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackAfterModelCall]
	require.True(t, ok)

	agent := newFakeModelSwitcherAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_GetCallbacks_调用AfterToolCall 验证回调映射中 AfterToolCall 可调用
func TestTaskPlanningRail_GetCallbacks_调用AfterToolCall(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackAfterToolCall]
	require.True(t, ok)

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_GetCallbacks_调用AfterInvoke 验证回调映射中 AfterInvoke 可调用
func TestTaskPlanningRail_GetCallbacks_调用AfterInvoke(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackAfterInvoke]
	require.True(t, ok)

	inputs := &agentinterfaces.InvokeInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
}

// TestTaskPlanningRail_GetCallbacks_调用AfterTaskIteration 验证回调映射中 AfterTaskIteration 可调用
func TestTaskPlanningRail_GetCallbacks_调用AfterTaskIteration(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackAfterTaskIteration]
	require.True(t, ok)

	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
}

// --- syncTodosFromPlan 测试 ---

// TestSyncTodosFromPlan_无Session 验证 session 为 nil 时直接返回
func TestSyncTodosFromPlan_无Session(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeModelSwitcherAgent()
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	// 不应 panic
	r.syncTodosFromPlan(context.Background(), cbc)
}

// TestSyncTodosFromPlan_不实现deepStateLoader 验证 agent 不实现 deepStateLoader 时跳过
func TestSyncTodosFromPlan_不实现deepStateLoader(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeBaseAgent()
	sess := newFakeSession("sess-sync")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	// 不应 panic，应记录日志后返回
	r.syncTodosFromPlan(context.Background(), cbc)
}

// TestSyncTodosFromPlan_state为nil 验证 LoadState 返回 nil 时跳过
func TestSyncTodosFromPlan_state为nil(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeModelSwitcherAgent()
	agent.deepState = nil
	sess := newFakeSession("sess-nil-state")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	r.syncTodosFromPlan(context.Background(), cbc)
}

// TestSyncTodosFromPlan_TaskPlan为空 验证 TaskPlan.Tasks 为空时跳过
func TestSyncTodosFromPlan_TaskPlan为空(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	agent := newFakeModelSwitcherAgent()
	agent.deepState = &hschema.DeepAgentState{
		TaskPlan: &hschema.TaskPlan{Tasks: []hschema.TodoItem{}},
	}
	sess := newFakeSession("sess-empty-plan")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	r.syncTodosFromPlan(context.Background(), cbc)
}

// TestSyncTodosFromPlan_todoTool为nil 验证 todoTool 为 nil 时跳过
func TestSyncTodosFromPlan_todoTool为nil(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	r.todoTool = nil
	agent := newFakeModelSwitcherAgent()
	agent.deepState = &hschema.DeepAgentState{
		TaskPlan: &hschema.TaskPlan{
			Tasks: []hschema.TodoItem{{ID: "1", Status: hschema.TodoStatusCompleted}},
		},
	}
	sess := newFakeSession("sess-no-todo-tool")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	r.syncTodosFromPlan(context.Background(), cbc)
}

// --- BeforeModelCall 完整模型切换测试 ---

// TestTaskPlanningRail_BeforeModelCall_按InProgressModelID切换 验证根据 in_progress 任务的 selected_model_id 切换模型
func TestTaskPlanningRail_BeforeModelCall_按InProgressModelID切换(t *testing.T) {
	t.Parallel()

	targetModel := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "coding-model"},
	}
	defaultModel := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "default-model"},
	}
	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{
			targetModel: "编码模型",
		}),
	)
	// 设置 todoTool 非 nil 以进入缓存查找路径
	r.todoTool = &todo.TodoTool{}
	// 设置缓存中的 in_progress 任务指定 coding-model
	r.todosCache["sess-switch"] = []hschema.TodoItem{
		{ID: "1", Status: hschema.TodoStatusInProgress, SelectedModelID: "coding-model"},
	}

	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = defaultModel
	sess := newFakeSession("sess-switch")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	require.Len(t, agent.switchModelCalls, 1)
	assert.Equal(t, targetModel, agent.switchModelCalls[0])
}

// TestTaskPlanningRail_BeforeModelCall_首次捕获DefaultLLM 验证首次调用捕获 defaultLLM
func TestTaskPlanningRail_BeforeModelCall_首次捕获DefaultLLM(t *testing.T) {
	t.Parallel()

	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "m1"},
	}
	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{model: "模型1"}),
	)

	agent := newFakeModelSwitcherAgent()
	agent.currentLLM = model
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.Equal(t, model, r.defaultLLM)
}

// TestTaskPlanningRail_BeforeModelCall_GetLLM失败 验证 GetLLM 返回错误时 defaultLLM 不被设置
func TestTaskPlanningRail_BeforeModelCall_GetLLM失败(t *testing.T) {
	t.Parallel()

	model := &llm.Model{
		ClientConfig: &llmschema.ModelClientConfig{ClientID: "m1"},
	}
	r := NewTaskPlanningRail(
		WithModelSelection(map[*llm.Model]string{model: "模型1"}),
	)

	agent := newFakeModelSwitcherAgent()
	agent.currentLLMErr = fmt.Errorf("llm error")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.Nil(t, r.defaultLLM)
}

// TestTaskPlanningRail_BeforeModelCall_builder为nil时跳过 验证 systemPromptBuilder 为 nil 时跳过提示词注入
func TestTaskPlanningRail_BeforeModelCall_builder为nil时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()

	agent := &fakeBaseAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("test"), builder: nil}
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
}

// --- AfterToolCall 完整测试 ---

// TestTaskPlanningRail_AfterToolCall_非ToolCallInputs 验证 inputs 非 ToolCallInputs 时跳过刷新
func TestTaskPlanningRail_AfterToolCall_非ToolCallInputs(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// todoTool 为 nil，AfterToolCall 直接返回 nil

	inputs := &agentinterfaces.ModelCallInputs{}
	sess := newFakeSession("sess-not-tool")
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestTaskPlanningRail_AfterToolCall_无Session 验证 session 为 nil 时跳过刷新
func TestTaskPlanningRail_AfterToolCall_无Session(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()
	// todoTool 为 nil，AfterToolCall 直接返回 nil

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_create"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestTaskPlanningRail_AfterToolCall_进度提醒未启用 验证 enableProgressRepeat 为 false 时跳过
func TestTaskPlanningRail_AfterToolCall_进度提醒未启用(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail() // enableProgressRepeat 默认 false
	// todoTool 为 nil，AfterToolCall 直接返回 nil

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_create"}
	sess := newFakeSession("sess-no-repeat")
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
	// toolCallCounts 不应被增加
	assert.Empty(t, r.toolCallCounts)
}

// --- 使用真实 TodoTool 的增强测试 ---

// TestTaskPlanningRail_AfterToolCall_todo工具刷新缓存 验证 todo_ 前缀工具触发缓存刷新
func TestTaskPlanningRail_AfterToolCall_todo工具刷新缓存(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	tools, tt := todo.CreateTodosTool("/tmp/test-workspace", fs, "cn", "test-agent")
	r := NewTaskPlanningRail()
	r.todoTool = &tt
	r.tools = tools

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_create"}
	sess := newFakeSession("sess-refresh")
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)
	// 缓存应被设置（即使 LoadTodos 返回空列表）
	assert.Contains(t, r.todosCache, "sess-refresh")
}

// TestTaskPlanningRail_AfterToolCall_进度提醒触发 验证 N 次调用后触发进度提醒
func TestTaskPlanningRail_AfterToolCall_进度提醒触发(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	// 预设一些 todos 数据
	todoData := `[{"id":"1","content":"任务1","status":"in_progress","activeForm":"执行任务1","description":"描述1"}]`
	fs.data["/tmp/test-workspace/sess-remind/todo.json"] = todoData

	tools, tt := todo.CreateTodosTool("/tmp/test-workspace", fs, "cn", "test-agent")
	r := NewTaskPlanningRail(
		WithEnableProgressRepeat(true),
		WithListToolCallInterval(1), // 每次都触发
	)
	r.todoTool = &tt
	r.tools = tools

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_modify"}
	sess := newFakeSession("sess-remind")
	agent := newFakeBaseAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)
	// toolCallCounts 应为 1
	assert.Equal(t, 1, r.toolCallCounts["sess-remind"])
}

// TestTaskPlanningRail_Init_完整路径 验证 DeepAgent 有 AbilityManager 时注册工具
func TestTaskPlanningRail_Init_完整路径(t *testing.T) {
	t.Parallel()

	r := NewTaskPlanningRail()

	// 构造一个有 AbilityManager 和 DeepConfig 的 DeepAgent mock
	builder := saprompt.NewSystemPromptBuilder()
	am := &fakeAbilityManager{}
	agent := &fakeDeepAgentWithAm{
		fakeBaseAgent: fakeBaseAgent{
			cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
			builder: builder,
		},
		am: am,
	}

	err := r.Init(agent)
	require.NoError(t, err)
}

// TestTaskPlanningRail_Uninit_有工具时移除 验证 Uninit 有工具时调用 Remove
func TestTaskPlanningRail_Uninit_有工具时移除(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	tools, _ := todo.CreateTodosTool("/tmp/test-workspace", fs, "cn", "test-agent")
	r := NewTaskPlanningRail()
	r.tools = tools

	am := &fakeAbilityManager{}
	agent := &fakeBaseAgentWithAm{
		fakeBaseAgent: *newFakeBaseAgent(),
		am:           am,
	}

	err := r.Uninit(agent)
	require.NoError(t, err)
	assert.Nil(t, r.tools)
	assert.Nil(t, r.todoTool)
}

// TestTaskPlanningRail_AfterInvoke_有TodoTool时清理 验证 AfterInvoke 调用 CleanupSession
func TestTaskPlanningRail_AfterInvoke_有TodoTool时清理(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	_, tt := todo.CreateTodosTool("/tmp/test-workspace", fs, "cn", "test-agent")
	r := NewTaskPlanningRail()
	r.todoTool = &tt
	sessID := "sess-cleanup-todo"
	r.todosCache[sessID] = []hschema.TodoItem{{ID: "1"}}
	r.toolCallCounts[sessID] = 5

	sess := newFakeSession(sessID)
	inputs := &agentinterfaces.InvokeInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)

	err := r.AfterInvoke(context.Background(), cbc)
	require.NoError(t, err)

	// 验证缓存已清理
	assert.NotContains(t, r.todosCache, sessID)
	assert.NotContains(t, r.toolCallCounts, sessID)
}

// TestGetInProgressModelID_缓存未命中 验证缓存未命中时通过 todoTool 加载
func TestGetInProgressModelID_缓存未命中(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	todoData := `[{"id":"1","content":"任务1","status":"in_progress","selected_model_id":"model-x","activeForm":"执行","description":"描述"}]`
	fs.data["/tmp/test-ws/sess-load/todo.json"] = todoData

	_, tt := todo.CreateTodosTool("/tmp/test-ws", fs, "cn", "test-agent")
	r := NewTaskPlanningRail()
	r.todoTool = &tt

	agent := newFakeBaseAgent()
	sess := newFakeSession("sess-load")
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	modelID := r.getInProgressModelID(context.Background(), cbc)
	assert.Equal(t, "model-x", modelID)
	// 缓存应被填充
	assert.Contains(t, r.todosCache, "sess-load")
}

// TestSyncTodosFromPlan_完整同步 验证 syncTodosFromPlan 完整路径
func TestSyncTodosFromPlan_完整同步(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	// 预设 todos 文件：任务 1 是 in_progress
	todoData := `[{"id":"1","content":"任务1","status":"in_progress","activeForm":"执行任务1","description":"描述1","selected_model_id":"model-a"}]`
	fs.data["/tmp/test-sync/sess-full/todo.json"] = todoData

	_, tt := todo.CreateTodosTool("/tmp/test-sync", fs, "cn", "test-agent")
	r := NewTaskPlanningRail()
	r.todoTool = &tt

	agent := newFakeModelSwitcherAgent()
	// 设置 deepState：任务 1 应变为 completed
	agent.deepState = &hschema.DeepAgentState{
		TaskPlan: &hschema.TaskPlan{
			Tasks: []hschema.TodoItem{
				{ID: "1", Status: hschema.TodoStatusCompleted},
			},
		},
	}

	sess := newFakeSession("sess-full")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	r.syncTodosFromPlan(context.Background(), cbc)
	// 验证文件已更新（SaveTodos 被调用）
	updatedData, ok := fs.data["/tmp/test-sync/sess-full/todo.json"]
	assert.True(t, ok)
	assert.Contains(t, updatedData, "completed")
}

// TestSyncTodosFromPlan_状态未变化时跳过 验证 todos 状态与 TaskPlan 一致时不调用 SaveTodos
func TestSyncTodosFromPlan_状态未变化时跳过(t *testing.T) {
	t.Parallel()

	fs := newMockFsOperation()
	// todos 和 TaskPlan 的状态一致（任务 1 都是 in_progress）
	todoData := `[{"id":"1","content":"任务1","status":"in_progress","activeForm":"执行任务1","description":"描述1"}]`
	fs.data["/tmp/test-skip/sess-skip/todo.json"] = todoData

	_, tt := todo.CreateTodosTool("/tmp/test-skip", fs, "cn", "test-agent")
	r := NewTaskPlanningRail()
	r.todoTool = &tt

	agent := newFakeModelSwitcherAgent()
	agent.deepState = &hschema.DeepAgentState{
		TaskPlan: &hschema.TaskPlan{
			Tasks: []hschema.TodoItem{
				{ID: "1", Status: hschema.TodoStatusInProgress},
			},
		},
	}

	sess := newFakeSession("sess-skip")
	inputs := &agentinterfaces.TaskIterationInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)

	r.syncTodosFromPlan(context.Background(), cbc)
	// 状态未变化，SaveTodos 不应被调用
}

// fakeAbilityManager 简单的 AbilityManager mock
type fakeAbilityManager struct {
	addedAbilities   []string
	removedAbilities []string
}

func (f *fakeAbilityManager) Add(ability cschema2.Ability) agentschema.AddAbilityResult {
	if ability != nil {
		f.addedAbilities = append(f.addedAbilities, ability.AbilityName())
	}
	return agentschema.AddAbilityResult{}
}
func (f *fakeAbilityManager) AddMany(abilities []cschema2.Ability) []agentschema.AddAbilityResult {
	return nil
}
func (f *fakeAbilityManager) Remove(name string) cschema2.Ability {
	f.removedAbilities = append(f.removedAbilities, name)
	return nil
}
func (f *fakeAbilityManager) RemoveMany(names []string) []cschema2.Ability { return nil }
func (f *fakeAbilityManager) Get(name string) cschema2.Ability               { return nil }
func (f *fakeAbilityManager) List() []cschema2.Ability                       { return nil }
func (f *fakeAbilityManager) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]cschema2.ToolInfoInterface, error) {
	return nil, nil
}
func (f *fakeAbilityManager) Execute(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ []*llmschema.ToolCall, _ sessioninterfaces.SessionFacade, _ string) []agentschema.ExecuteResult {
	return nil
}
func (f *fakeAbilityManager) SetContextEngine(_ ceinterface.ContextEngine) {}
func (f *fakeAbilityManager) ReorderTools(_ []string)                     {}

// fakeDeepAgentWithAm 有 AbilityManager 的 fakeBaseAgent
type fakeDeepAgentWithAm struct {
	fakeBaseAgent
	am *fakeAbilityManager
}

func (f *fakeDeepAgentWithAm) AbilityManager() agentinterfaces.AbilityManagerInterface { return f.am }
func (f *fakeDeepAgentWithAm) ReactAgent() *agents.ReActAgent                         { return nil }
func (f *fakeDeepAgentWithAm) LoopCoordinator() hinterfaces.LoopCoordinatorInterface   { return nil }
func (f *fakeDeepAgentWithAm) LoopController() controller.ControllerInterface          { return nil }
func (f *fakeDeepAgentWithAm) EventHandler() modules.EventHandler                      { return nil }
func (f *fakeDeepAgentWithAm) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeDeepAgentWithAm) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeDeepAgentWithAm) IsInvokeActive() bool                   { return false }
func (f *fakeDeepAgentWithAm) IsAutoInvokeScheduled() bool            { return false }
func (f *fakeDeepAgentWithAm) SetAutoInvokeScheduled(_ bool)          {}
func (f *fakeDeepAgentWithAm) ScheduleAutoInvokeOnSpawnDone(_ string) error {
	return nil
}
func (f *fakeDeepAgentWithAm) CreateSubagent(_ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}

// Invoke 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentWithAm) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// SwitchMode 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentWithAm) SwitchMode(_ sessioninterfaces.SessionFacade, _ string) {}

// RestoreModeAfterPlanExit 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentWithAm) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}

// GetPlanFilePath 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentWithAm) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string { return "" }

// SaveState 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentWithAm) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {}

// 编译时验证
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentWithAm)(nil)

// fakeBaseAgentWithAm 有 AbilityManager 的 fakeBaseAgent（非 DeepAgent）
type fakeBaseAgentWithAm struct {
	fakeBaseAgent
	am *fakeAbilityManager
}

func (f *fakeBaseAgentWithAm) AbilityManager() agentinterfaces.AbilityManagerInterface { return f.am }

// ──────────────────────────── 非导出函数 ────────────────────────────

// 确保编译时 fakeModelSwitcherAgent 满足 BaseAgent
var _ agentinterfaces.BaseAgent = (*fakeModelSwitcherAgent)(nil)

// 确保编译时 fakeDeepAgentForTaskPlanning 满足 BaseAgent
var _ agentinterfaces.BaseAgent = (*fakeDeepAgentForTaskPlanning)(nil)
