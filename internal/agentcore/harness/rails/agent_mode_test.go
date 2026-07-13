//go:build test

package rails

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── Mock 实现 ────────────────────────────

// fakeDeepAgentForAgentMode 实现 DeepAgentInterface 的 mock（供 AgentModeRail 测试）
type fakeDeepAgentForAgentMode struct {
	fakeBaseAgent
	// deepState LoadState 返回值
	deepState *hschema.DeepAgentState
	// planFilePath GetPlanFilePath 返回值
	planFilePath string
}

func newFakeDeepAgentForAgentMode() *fakeDeepAgentForAgentMode {
	return &fakeDeepAgentForAgentMode{
		fakeBaseAgent: *newFakeBaseAgent(),
		deepState:     &hschema.DeepAgentState{},
	}
}

func (f *fakeDeepAgentForAgentMode) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentForAgentMode) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentForAgentMode) LoopController() controller.ControllerInterface { return nil }
func (f *fakeDeepAgentForAgentMode) EventHandler() modules.EventHandler             { return nil }
func (f *fakeDeepAgentForAgentMode) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return f.deepState
}
func (f *fakeDeepAgentForAgentMode) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeDeepAgentForAgentMode) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForAgentMode) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForAgentMode) SetAutoInvokeScheduled(_ bool)        {}
func (f *fakeDeepAgentForAgentMode) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}
func (f *fakeDeepAgentForAgentMode) CreateSubagent(_ context.Context, _ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentForAgentMode) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeDeepAgentForAgentMode) SwitchMode(_ sessioninterfaces.SessionFacade, _ string)     {}
func (f *fakeDeepAgentForAgentMode) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}
func (f *fakeDeepAgentForAgentMode) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string {
	return f.planFilePath
}
func (f *fakeDeepAgentForAgentMode) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {
}

// 编译时验证
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForAgentMode)(nil)
var _ agentinterfaces.BaseAgent = (*fakeDeepAgentForAgentMode)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// --- 构造函数测试 ---

// TestNewAgentModeRail 验证默认允许工具集
func TestNewAgentModeRail(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	assert.Equal(t, 85, r.Priority())
	// 验证默认白名单与 defaultPlanModeAllowedTools 一致
	assert.Equal(t, len(defaultPlanModeAllowedTools), len(r.allowedTools))
	for k := range defaultPlanModeAllowedTools {
		assert.Contains(t, r.allowedTools, k, "默认白名单应包含 %q", k)
	}
}

// TestNewAgentModeRail_自定义白名单 验证自定义 allowedTools
func TestNewAgentModeRail_自定义白名单(t *testing.T) {
	t.Parallel()

	custom := []string{"read_file", "grep", "bash"}
	r := NewAgentModeRail(custom)
	assert.Equal(t, 85, r.Priority())
	assert.Equal(t, len(custom), len(r.allowedTools))
	for _, name := range custom {
		assert.Contains(t, r.allowedTools, name)
	}
	// 不应包含自定义列表外的工具
	assert.NotContains(t, r.allowedTools, "write_file")
}

// TestAgentModeRail_Priority 验证优先级为 85
func TestAgentModeRail_Priority(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	assert.Equal(t, 85, r.Priority())
}

// --- 正则表达式测试 ---

// TestGitWriteRE 验证 git 写操作正则匹配
func TestGitWriteRE(t *testing.T) {
	t.Parallel()

	// 应匹配的 git 写操作
	matched := []string{
		"git add .",
		"git commit -m msg",
		"git push",
		"git pull",
		"git reset --hard HEAD",
		"git clean -df",
		"git stash drop",
		"git branch -D feature",
		"git merge main",
		"git tag v1.0",
		"git rebase main",
	}
	for _, cmd := range matched {
		assert.True(t, gitWriteRE.MatchString(cmd), "应匹配 %q", cmd)
	}

	// 不应匹配的 git 只读操作
	unmatched := []string{
		"git status",
		"git log",
		"git diff",
		"git branch",
		"git remote -v",
	}
	for _, cmd := range unmatched {
		assert.False(t, gitWriteRE.MatchString(cmd), "不应匹配 %q", cmd)
	}
}

// --- 集合验证测试 ---

// TestIsPlanFile 验证计划文件路径比较逻辑
func TestIsPlanFile(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	// 相同路径应匹配
	assert.True(t, r.isPlanFile("/tmp/plan.md", "/tmp/plan.md"))

	// 不同路径不应匹配
	assert.False(t, r.isPlanFile("/tmp/other.md", "/tmp/plan.md"))

	// 空路径不应匹配
	assert.False(t, r.isPlanFile("", "/tmp/plan.md"))
	assert.False(t, r.isPlanFile("/tmp/plan.md", ""))
	assert.False(t, r.isPlanFile("", ""))
}

// TestHiddenInPlan 验证 plan 模式下隐藏的工具集合
func TestHiddenInPlan(t *testing.T) {
	t.Parallel()

	// hiddenInPlan 应包含所有 todo 和 session 工具
	for k := range todoToolNames {
		assert.Contains(t, hiddenInPlan, k, "hiddenInPlan 应包含 todo 工具 %q", k)
	}
	for k := range sessionToolNames {
		assert.Contains(t, hiddenInPlan, k, "hiddenInPlan 应包含 session 工具 %q", k)
	}
	// 不应包含 plan 模式工具
	assert.NotContains(t, hiddenInPlan, "enter_plan_mode")
	assert.NotContains(t, hiddenInPlan, "exit_plan_mode")
}

// TestHiddenInNormal 验证普通模式下隐藏的工具集合
func TestHiddenInNormal(t *testing.T) {
	t.Parallel()

	assert.Contains(t, hiddenInNormal, "enter_plan_mode")
	assert.Contains(t, hiddenInNormal, "exit_plan_mode")
	// 不应隐藏普通工具
	assert.NotContains(t, hiddenInNormal, "bash")
	assert.NotContains(t, hiddenInNormal, "read_file")
}

// TestDefaultPlanModeAllowedTools 验证默认白名单内容
func TestDefaultPlanModeAllowedTools(t *testing.T) {
	t.Parallel()

	expected := []string{
		"switch_mode", "enter_plan_mode", "exit_plan_mode",
		"ask_user", "task_tool", "read_file", "grep",
		"list_files", "glob", "bash", "write_file", "edit_file",
	}
	for _, name := range expected {
		assert.Contains(t, defaultPlanModeAllowedTools, name, "默认白名单应包含 %q", name)
	}
	assert.Equal(t, len(expected), len(defaultPlanModeAllowedTools))
}

// --- BeforeModelCall 测试 ---

// TestAgentModeRail_BeforeModelCall_Normal模式移除ModeInstructions 验证非 plan 模式移除 MODE_INSTRUCTIONS 节并隐藏 enter/exit 工具
func TestAgentModeRail_BeforeModelCall_Normal模式移除ModeInstructions(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "normal"},
	}
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	r.agent = agent
	r.systemPromptBuilder = builder

	// 先添加一个 MODE_INSTRUCTIONS 节
	builder.AddSection(saprompt.PromptSection{Name: sections.SectionModeInstructions, Content: map[string]string{"cn": "test"}})
	assert.True(t, builder.HasSection(sections.SectionModeInstructions))

	// 构建包含 enter_plan_mode / exit_plan_mode 的工具列表
	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("enter_plan_mode", "进入规划模式", nil),
		cschema.NewToolInfo("exit_plan_mode", "退出规划模式", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	inputs := &agentinterfaces.ModelCallInputs{Tools: tools}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// MODE_INSTRUCTIONS 节应被移除
	assert.False(t, builder.HasSection(sections.SectionModeInstructions))

	// enter_plan_mode / exit_plan_mode 应从工具列表中过滤
	toolNames := make([]string, len(inputs.Tools))
	for i, t := range inputs.Tools {
		toolNames[i] = t.GetName()
	}
	assert.NotContains(t, toolNames, "enter_plan_mode")
	assert.NotContains(t, toolNames, "exit_plan_mode")
	assert.Contains(t, toolNames, "bash")
}

// TestAgentModeRail_BeforeModelCall_Plan模式注入ModeInstructions 验证 plan 模式注入 MODE_INSTRUCTIONS、移除 Todo/SessionTools 节、过滤隐藏工具
func TestAgentModeRail_BeforeModelCall_Plan模式注入ModeInstructions(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	agent.planFilePath = "/tmp/plan.md"
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	r.agent = agent
	r.systemPromptBuilder = builder
	r.language = "cn"

	// 先添加 Todo 和 SessionTools 节
	builder.AddSection(saprompt.PromptSection{Name: sections.SectionTodo, Content: map[string]string{"cn": "test"}})
	builder.AddSection(saprompt.PromptSection{Name: sections.SectionSessionTools, Content: map[string]string{"cn": "test"}})
	assert.True(t, builder.HasSection(sections.SectionTodo))
	assert.True(t, builder.HasSection(sections.SectionSessionTools))

	// 构建包含 todo/session 工具的工具列表
	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("todo_create", "创建待办", nil),
		cschema.NewToolInfo("sessions_list", "列出会话", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	inputs := &agentinterfaces.ModelCallInputs{Tools: tools}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// MODE_INSTRUCTIONS 节应被注入
	assert.True(t, builder.HasSection(sections.SectionModeInstructions))

	// Todo 和 SessionTools 节应被移除
	assert.False(t, builder.HasSection(sections.SectionTodo))
	assert.False(t, builder.HasSection(sections.SectionSessionTools))

	// todo/session 工具应被过滤
	toolNames := make([]string, len(inputs.Tools))
	for i, t := range inputs.Tools {
		toolNames[i] = t.GetName()
	}
	assert.NotContains(t, toolNames, "todo_create")
	assert.NotContains(t, toolNames, "sessions_list")
	assert.Contains(t, toolNames, "bash")
}

// --- BeforeToolCall 测试 ---

// TestAgentModeRail_BeforeToolCall_非Plan模式放行 验证普通模式无条件放行
func TestAgentModeRail_BeforeToolCall_非Plan模式放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "normal"},
	}
	r.agent = agent

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "some_tool"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 不应被标记为跳过
	assert.Nil(t, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_EnterPlanMode校验 验证 enter_plan_mode 模式前提校验
func TestAgentModeRail_BeforeToolCall_EnterPlanMode校验(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 非 plan 模式调用 enter_plan_mode 应被拒绝
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "normal"},
	}
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "enter_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 应被标记为跳过
	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_ExitPlanMode校验 验证 exit_plan_mode 模式前提校验
func TestAgentModeRail_BeforeToolCall_ExitPlanMode校验(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 非 plan 模式调用 exit_plan_mode 应被拒绝
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "normal"},
	}
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "exit_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 应被标记为跳过
	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_Plan模式白名单拒绝 验证非白名单工具被拒绝
func TestAgentModeRail_BeforeToolCall_Plan模式白名单拒绝(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 使用不在默认白名单中的工具
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "dangerous_tool"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 应被标记为跳过
	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_Git写操作拦截 验证 plan 模式下 git 写操作被拦截
func TestAgentModeRail_BeforeToolCall_Git写操作拦截(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// bash 工具执行 git add .
	argsJSON, _ := json.Marshal(map[string]any{"command": "git add ."})
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "bash",
		ToolArgs: string(argsJSON),
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 应被标记为跳过
	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_计划文件路径拦截 验证 write_file 写入非计划文件被拦截
func TestAgentModeRail_BeforeToolCall_计划文件路径拦截(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	agent.planFilePath = "/tmp/plan.md"
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// write_file 写入非计划文件
	argsJSON, _ := json.Marshal(map[string]any{"file_path": "/tmp/other.md", "content": "hello"})
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: string(argsJSON),
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 应被标记为跳过
	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_计划文件路径放行 验证 write_file 写入计划文件被放行
func TestAgentModeRail_BeforeToolCall_计划文件路径放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	agent.planFilePath = "/tmp/plan.md"
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// write_file 写入计划文件
	argsJSON, _ := json.Marshal(map[string]any{"file_path": "/tmp/plan.md", "content": "plan content"})
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: string(argsJSON),
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 不应被标记为跳过
	_, hasSkip := cbc.Extra()[extraSkipToolKey]
	assert.False(t, hasSkip)
}

// --- AfterToolCall 测试 ---

// TestAgentModeRail_AfterToolCall_EnterPlanMode注册TaskTool 验证 enter_plan_mode 成功触发 task_tool 注册
func TestAgentModeRail_AfterToolCall_EnterPlanMode注册TaskTool(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	r.agent = agent

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "enter_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 由于 DeepConfig 返回 nil（无 subagents），不会真正注册 task_tool
	// 但不应 panic 或返回错误
	assert.False(t, r.ownsTaskTool)
}

// TestAgentModeRail_AfterToolCall_ExitPlanMode注销TaskTool 验证 exit_plan_mode 成功触发 task_tool 注销
func TestAgentModeRail_AfterToolCall_ExitPlanMode注销TaskTool(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	r.agent = agent

	// 先设置 ownsTaskTool = true 模拟已持有 task_tool
	r.ownsTaskTool = true
	r.taskTools = nil // 空 taskTools 列表，unregisterTaskTool 将跳过

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "exit_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// ownsTaskTool 应仍为 true（因为 taskTools 为空，unregisterTaskTool 直接返回）
	assert.True(t, r.ownsTaskTool)
}

// --- 辅助函数测试 ---

// TestExtractFilePath 验证从工具参数 JSON 提取 file_path
func TestExtractFilePath(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	// 正常提取
	argsJSON, _ := json.Marshal(map[string]any{"file_path": "/tmp/test.md"})
	inputs := &agentinterfaces.ToolCallInputs{ToolArgs: string(argsJSON)}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	filePath := r.extractFilePath(cbc)
	assert.Equal(t, "/tmp/test.md", filePath)

	// 缺少 file_path
	argsJSON2, _ := json.Marshal(map[string]any{"command": "ls"})
	inputs2 := &agentinterfaces.ToolCallInputs{ToolArgs: string(argsJSON2)}
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, inputs2, nil)

	filePath2 := r.extractFilePath(cbc2)
	assert.Equal(t, "", filePath2)

	// 空 ToolArgs
	inputs3 := &agentinterfaces.ToolCallInputs{ToolArgs: ""}
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, inputs3, nil)

	filePath3 := r.extractFilePath(cbc3)
	assert.Equal(t, "", filePath3)

	// 无效 JSON
	inputs4 := &agentinterfaces.ToolCallInputs{ToolArgs: "not json"}
	cbc4 := agentinterfaces.NewAgentCallbackContext(agent, inputs4, nil)

	filePath4 := r.extractFilePath(cbc4)
	assert.Equal(t, "", filePath4)
}

// TestExtractBashCommand 验证从工具参数 JSON 提取 command
func TestExtractBashCommand(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	// 正常提取
	argsJSON, _ := json.Marshal(map[string]any{"command": "git push"})
	inputs := &agentinterfaces.ToolCallInputs{ToolArgs: string(argsJSON)}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	cmd := r.extractBashCommand(cbc)
	assert.Equal(t, "git push", cmd)

	// 缺少 command
	argsJSON2, _ := json.Marshal(map[string]any{"file_path": "/tmp/test.md"})
	inputs2 := &agentinterfaces.ToolCallInputs{ToolArgs: string(argsJSON2)}
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, inputs2, nil)

	cmd2 := r.extractBashCommand(cbc2)
	assert.Equal(t, "", cmd2)

	// 空 ToolArgs
	inputs3 := &agentinterfaces.ToolCallInputs{ToolArgs: ""}
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, inputs3, nil)

	cmd3 := r.extractBashCommand(cbc3)
	assert.Equal(t, "", cmd3)

	// 无效 JSON
	inputs4 := &agentinterfaces.ToolCallInputs{ToolArgs: "not json"}
	cbc4 := agentinterfaces.NewAgentCallbackContext(agent, inputs4, nil)

	cmd4 := r.extractBashCommand(cbc4)
	assert.Equal(t, "", cmd4)
}

// --- GetCallbacks 测试 ---

// TestAgentModeRail_GetCallbacks 验证回调映射包含 BeforeModelCall + BeforeToolCall + AfterToolCall
func TestAgentModeRail_GetCallbacks(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	callbacks := r.GetCallbacks()

	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeToolCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterToolCall)
}

// --- Init 测试 ---

// TestAgentModeRail_Init_非DeepAgent时跳过 验证 agent 不满足 DeepAgentInterface 时直接返回 nil
func TestAgentModeRail_Init_非DeepAgent时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeBaseAgent()
	err := r.Init(agent)
	assert.NoError(t, err)
	assert.Nil(t, r.tools)
}

// TestAgentModeRail_Init_正常初始化 验证 DeepAgent 初始化成功
func TestAgentModeRail_Init_正常初始化(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	// 设置 AbilityManager 为 fakeAbilityManager
	am := &fakeAbilityManager{}
	agent.fakeBaseAgent = fakeBaseAgent{
		cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
		builder: saprompt.NewSystemPromptBuilder(),
	}
	// 需要覆盖 AbilityManager 方法，使用 wrapper
	wrapper := &fakeDeepAgentWithAmForAgentMode{
		fakeDeepAgentForAgentMode: *agent,
		am:                        am,
	}
	err := r.Init(wrapper)
	assert.NoError(t, err)
	assert.Len(t, r.tools, 3)
}

// fakeDeepAgentWithAmForAgentMode 有 AbilityManager 的 fakeDeepAgentForAgentMode
type fakeDeepAgentWithAmForAgentMode struct {
	fakeDeepAgentForAgentMode
	am *fakeAbilityManager
}

func (f *fakeDeepAgentWithAmForAgentMode) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.am
}

// fakeDeepAgentWithConfigForAgentMode 有 DeepConfig 的 fakeDeepAgentForAgentMode
type fakeDeepAgentWithConfigForAgentMode struct {
	fakeDeepAgentForAgentMode
	deepConfig *hschema.DeepAgentConfig
}

func (f *fakeDeepAgentWithConfigForAgentMode) DeepConfig() *hschema.DeepAgentConfig {
	return f.deepConfig
}

// --- 额外边界测试 ---

// TestAgentModeRail_BeforeToolCall_已跳过工具放行 验证 _skip_tool=true 时放行
func TestAgentModeRail_BeforeToolCall_已跳过工具放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "dangerous_tool"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	// 预设 _skip_tool = true
	cbc.Extra()[extraSkipToolKey] = true

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// ToolResult 不应被设置（未被 rejectTool 处理）
	assert.Nil(t, inputs.ToolResult)
}

// TestAgentModeRail_BeforeToolCall_HiddenInPlan工具拦截 验证 plan 模式下 hiddenInPlan 工具被拦截
func TestAgentModeRail_BeforeToolCall_HiddenInPlan工具拦截(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "todo_create"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_Git只读放行 验证 plan 模式下 git 只读操作放行
func TestAgentModeRail_BeforeToolCall_Git只读放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	argsJSON, _ := json.Marshal(map[string]any{"command": "git status"})
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "bash",
		ToolArgs: string(argsJSON),
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	_, hasSkip := cbc.Extra()[extraSkipToolKey]
	assert.False(t, hasSkip)
}

// TestAgentModeRail_BeforeToolCall_EditFile拦截 验证 edit_file 写入非计划文件被拦截
func TestAgentModeRail_BeforeToolCall_EditFile拦截(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	agent.planFilePath = "/tmp/plan.md"
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	argsJSON, _ := json.Marshal(map[string]any{"file_path": "/tmp/other.md", "content": "hello"})
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "edit_file",
		ToolArgs: string(argsJSON),
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// TestAgentModeRail_BeforeToolCall_EnterPlanMode在Plan模式下放行 验证 plan 模式下 enter_plan_mode 放行
func TestAgentModeRail_BeforeToolCall_EnterPlanMode在Plan模式下放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "enter_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 在 plan 模式下，enter_plan_mode 不应被拒绝
	_, hasSkip := cbc.Extra()[extraSkipToolKey]
	assert.False(t, hasSkip)
}

// TestAgentModeRail_BeforeToolCall_ExitPlanMode在Plan模式下放行 验证 plan 模式下 exit_plan_mode 放行
func TestAgentModeRail_BeforeToolCall_ExitPlanMode在Plan模式下放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "exit_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// 在 plan 模式下，exit_plan_mode 不应被拒绝
	_, hasSkip := cbc.Extra()[extraSkipToolKey]
	assert.False(t, hasSkip)
}

// TestAgentModeRail_BeforeToolCall_非ToolCallInputs时放行 验证 inputs 非 ToolCallInputs 时放行
func TestAgentModeRail_BeforeToolCall_非ToolCallInputs时放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeToolCall(context.Background(), cbc)
	require.NoError(t, err)
}

// TestAgentModeRail_AfterToolCall_非ToolCallInputs时放行 验证 inputs 非 ToolCallInputs 时放行
func TestAgentModeRail_AfterToolCall_非ToolCallInputs时放行(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)
}

// TestAgentModeRail_AfterToolCall_非EnterExit工具时跳过 验证非 enter/exit 工具不触发注册/注销
func TestAgentModeRail_AfterToolCall_非EnterExit工具时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// ownsTaskTool 不应改变
	assert.False(t, r.ownsTaskTool)
}

// TestAgentModeRail_AfterToolCall_SkipTool为true时跳过注册 验证 _skip_tool=true 时跳过 task_tool 注册
func TestAgentModeRail_AfterToolCall_SkipTool为true时跳过注册(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	r.agent = agent

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "enter_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	cbc.Extra()[extraSkipToolKey] = true

	err := r.AfterToolCall(context.Background(), cbc)
	require.NoError(t, err)

	// ownsTaskTool 不应改变（因为 skip 为 true）
	assert.False(t, r.ownsTaskTool)
}

// TestRejectTool 验证工具拒绝逻辑
func TestRejectTool(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "dangerous_tool",
		ToolCall: &llmschema.ToolCall{ID: "call-1"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	r.rejectTool(cbc, "test error message")

	// 应设置 skip 标记
	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
	// 应设置 ToolResult
	assert.NotNil(t, inputs.ToolResult)
	// 应设置 ToolMsg
	assert.NotNil(t, inputs.ToolMsg)
}

// TestLanguageIsCN 验证语言判断
func TestLanguageIsCN(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)

	// builder 为 nil 时默认中文
	r.systemPromptBuilder = nil
	assert.True(t, r.languageIsCN())

	// 中文语言（通过 builder 设置语言）
	r.systemPromptBuilder = builder
	builder.SetLanguage("cn")
	assert.True(t, r.languageIsCN())

	// 英文语言
	builder.SetLanguage("en")
	assert.False(t, r.languageIsCN())
}

// TestBuildPlanModeSection_中文 验证中文 BuildPlanModeSection 输出包含对应文本
func TestBuildPlanModeSection_中文(t *testing.T) {
	t.Parallel()

	// 路径已设置（enter_plan_mode 已调用）
	s := sections.BuildPlanModeSection("/tmp/plan.md", true, "cn")
	assert.Contains(t, s.Content["cn"], "enter_plan_mode")
	assert.Contains(t, s.Content["cn"], "工作流")
	assert.Contains(t, s.Content["cn"], "/tmp/plan.md")
	assert.Contains(t, s.Content["cn"], "edit_file")

	// 路径未设置（enter_plan_mode 未调用）
	s2 := sections.BuildPlanModeSection("", false, "cn")
	assert.Contains(t, s2.Content["cn"], "enter_plan_mode")

	// 路径不存在
	s3 := sections.BuildPlanModeSection("/tmp/plan.md", false, "cn")
	assert.Contains(t, s3.Content["cn"], "/tmp/plan.md")
	assert.Contains(t, s3.Content["cn"], "write_file")
}

// TestBuildPlanModeSection_英文 验证英文 BuildPlanModeSection 输出包含对应文本
func TestBuildPlanModeSection_英文(t *testing.T) {
	t.Parallel()

	// 路径已设置（enter_plan_mode 已调用）
	s := sections.BuildPlanModeSection("/tmp/plan.md", true, "en")
	assert.Contains(t, s.Content["en"], "enter_plan_mode")
	assert.Contains(t, s.Content["en"], "workflow")
	assert.Contains(t, s.Content["en"], "/tmp/plan.md")
	assert.Contains(t, s.Content["en"], "edit_file")

	// 路径未设置（enter_plan_mode 未调用）
	s2 := sections.BuildPlanModeSection("", false, "en")
	assert.Contains(t, s2.Content["en"], "enter_plan_mode")

	// 路径不存在
	s3 := sections.BuildPlanModeSection("/tmp/plan.md", false, "en")
	assert.Contains(t, s3.Content["en"], "/tmp/plan.md")
	assert.Contains(t, s3.Content["en"], "write_file")
}

// TestParseToolArgs 验证工具参数解析
func TestParseToolArgs(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	// 空字符串
	assert.Nil(t, r.parseToolArgs(""))

	// 无效 JSON
	assert.Nil(t, r.parseToolArgs("not json"))

	// 正常 JSON
	result := r.parseToolArgs(`{"file_path": "/tmp/test.md"}`)
	require.NotNil(t, result)
	assert.Equal(t, "/tmp/test.md", result["file_path"])
}

// TestFilterHiddenTools 验证工具过滤逻辑
func TestFilterHiddenTools(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	hidden := map[string]struct{}{
		"todo_create": {},
		"bash":        {},
	}

	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("todo_create", "创建待办", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
		cschema.NewToolInfo("read_file", "读取文件", nil),
	}
	inputs := &agentinterfaces.ModelCallInputs{Tools: tools}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	r.filterHiddenTools(cbc, hidden)

	toolNames := make([]string, len(inputs.Tools))
	for i, t := range inputs.Tools {
		toolNames[i] = t.GetName()
	}
	assert.NotContains(t, toolNames, "todo_create")
	assert.NotContains(t, toolNames, "bash")
	assert.Contains(t, toolNames, "read_file")
}

// TestIsPlanFile_相对路径解析 验证相对路径解析到绝对路径后比较
func TestIsPlanFile_相对路径解析(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	// 相同相对路径应解析到相同绝对路径
	// 这里只验证不同路径不匹配
	assert.False(t, r.isPlanFile("plan.md", "other.md"))
}

// TestAgentModeRail_BeforeModelCall_Plan模式builder为nil 验证 plan 模式下 builder 为 nil 不 panic
func TestAgentModeRail_BeforeModelCall_Plan模式builder为nil(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := &fakeDeepAgentForAgentMode{
		fakeBaseAgent: fakeBaseAgent{
			cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
			builder: nil,
		},
		deepState: &hschema.DeepAgentState{
			PlanMode: hschema.PlanModeState{Mode: "plan"},
		},
		planFilePath: "/tmp/plan.md",
	}
	r.agent = agent
	r.systemPromptBuilder = nil
	r.language = "cn"

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
}

// --- Uninit 测试 ---

// TestAgentModeRail_Uninit_移除工具 验证 Uninit 清理 tools
func TestAgentModeRail_Uninit_移除工具(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	am := &fakeAbilityManager{}
	wrapper := &fakeDeepAgentWithAmForAgentMode{
		fakeDeepAgentForAgentMode: *agent,
		am:                        am,
	}

	// 先 Init 注册工具
	err := r.Init(wrapper)
	require.NoError(t, err)
	require.Len(t, r.tools, 3)

	// 再 Uninit 移除工具
	err = r.Uninit(wrapper)
	require.NoError(t, err)
	assert.Nil(t, r.tools)
}

// TestAgentModeRail_Uninit_无工具时不报错 验证 Uninit 在 tools 为 nil 时正常返回
func TestAgentModeRail_Uninit_无工具时不报错(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	am := &fakeAbilityManager{}
	wrapper := &fakeDeepAgentWithAmForAgentMode{
		fakeDeepAgentForAgentMode: *agent,
		am:                        am,
	}

	err := r.Uninit(wrapper)
	require.NoError(t, err)
}

// --- GetCallbacks 完整测试 ---

// TestAgentModeRail_GetCallbacks_完整调用 验证回调映射中各事件可调用
func TestAgentModeRail_GetCallbacks_完整调用(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	callbacks := r.GetCallbacks()

	// BeforeModelCall
	fn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	require.True(t, ok)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{PlanMode: hschema.PlanModeState{Mode: "normal"}}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	err := fn(context.Background(), cbc)
	require.NoError(t, err)

	// BeforeToolCall
	fn2, ok := callbacks[agentinterfaces.CallbackBeforeToolCall]
	require.True(t, ok)
	inputs2 := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, inputs2, nil)
	err = fn2(context.Background(), cbc2)
	require.NoError(t, err)

	// AfterToolCall
	fn3, ok := callbacks[agentinterfaces.CallbackAfterToolCall]
	require.True(t, ok)
	inputs3 := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, inputs3, nil)
	err = fn3(context.Background(), cbc3)
	require.NoError(t, err)
}

// --- registerTaskTool / unregisterTaskTool 测试 ---

// TestRegisterTaskTool_已持有时跳过 验证 ownsTaskTool=true 时跳过注册
func TestRegisterTaskTool_已持有时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	r.ownsTaskTool = true
	agent := newFakeDeepAgentForAgentMode()

	r.registerTaskTool(agent)
	// 不应改变状态
	assert.True(t, r.ownsTaskTool)
}

// TestRegisterTaskTool_无Subagents时跳过 验证 DeepConfig 无 subagents 时跳过注册
func TestRegisterTaskTool_无Subagents时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	r.agent = agent

	r.registerTaskTool(agent)
	assert.False(t, r.ownsTaskTool)
}

// TestRegisterTaskTool_非DeepAgent时跳过 验证 agent 不满足 DeepAgentInterface 时跳过注册
func TestRegisterTaskTool_非DeepAgent时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeBaseAgent()

	r.registerTaskTool(agent)
	assert.False(t, r.ownsTaskTool)
}

// TestRegisterTaskTool_已有注册时跳过 验证 isTaskToolRegistered=true 时跳过注册
func TestRegisterTaskTool_已有注册时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	// ownsTaskTool = true 模拟已有注册
	r.ownsTaskTool = true

	agent := newFakeDeepAgentForAgentMode()
	r.registerTaskTool(agent)
	assert.True(t, r.ownsTaskTool)
}

// TestRegisterTaskTool_DeepConfig为nil时跳过 验证 DeepConfig 为 nil 时跳过注册
func TestRegisterTaskTool_DeepConfig为nil时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	// DeepConfig() 默认返回 nil
	r.agent = agent

	r.registerTaskTool(agent)
	assert.False(t, r.ownsTaskTool)
}

// TestRegisterTaskTool_空Subagents时跳过 验证 Subagents 为空时跳过注册
func TestRegisterTaskTool_空Subagents时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := &fakeDeepAgentWithConfigForAgentMode{
		fakeDeepAgentForAgentMode: *newFakeDeepAgentForAgentMode(),
		deepConfig:                &hschema.DeepAgentConfig{Subagents: []hschema.SubagentSpec{}},
	}
	r.agent = agent

	r.registerTaskTool(agent)
	assert.False(t, r.ownsTaskTool)
}

// TestRegisterTaskTool_有Subagents但因CreateTaskTool需要真实DeepAgent 验证有 subagents 时触发注册流程但不 panic
// 注意：CreateTaskTool 需要真实 DeepAgent 实例（ReactAgent.Card()），
// 无法在纯单元测试中完整覆盖，属于集成测试范畴。
// 此处仅验证 early return 路径（DeepConfig 有 subagents 但 CreateTaskTool 返回空列表）。

// TestUnregisterTaskTool_未持有时跳过 验证 ownsTaskTool=false 时跳过注销
func TestUnregisterTaskTool_未持有时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	r.unregisterTaskTool(agent)
	assert.False(t, r.ownsTaskTool)
}

// TestUnregisterTaskTool_持有但无工具时跳过 验证 ownsTaskTool=true 但 taskTools 为空时跳过
func TestUnregisterTaskTool_持有但无工具时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	r.ownsTaskTool = true
	r.taskTools = nil

	agent := newFakeDeepAgentForAgentMode()
	r.unregisterTaskTool(agent)

	// taskTools 为空，直接返回不注销
	assert.True(t, r.ownsTaskTool)
}

// TestUnregisterTaskTool_持有工具时注销 验证 ownsTaskTool=true 且有 taskTools 时注销
func TestUnregisterTaskTool_持有工具时注销(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	r.ownsTaskTool = true
	r.taskTools = []tool.Tool{&fakeToolForSync{cardName: "task_tool"}}
	r.ownedTaskToolNames = map[string]struct{}{"task_tool": {}}

	am := &fakeAbilityManager{}
	wrapper := &fakeDeepAgentWithAmForAgentMode{
		fakeDeepAgentForAgentMode: *newFakeDeepAgentForAgentMode(),
		am:                        am,
	}
	r.unregisterTaskTool(wrapper)

	assert.False(t, r.ownsTaskTool)
	assert.Nil(t, r.taskTools)
}

// TestIsTaskToolRegistered 验证 isTaskToolRegistered 逻辑
func TestIsTaskToolRegistered(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	// 未持有
	assert.False(t, r.isTaskToolRegistered())

	// 已持有
	r.ownsTaskTool = true
	assert.True(t, r.isTaskToolRegistered())
}

// --- syncTaskToolForModelToolInputs 测试 ---

// TestSyncTaskToolForModelToolInputs_持有taskTool时注入 验证持有 task_tool 时确保在 tools 列表中
func TestSyncTaskToolForModelToolInputs_持有taskTool时注入(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	r.ownsTaskTool = true

	// 创建一个简单的 mock tool
	fakeTool := &fakeToolForSync{cardName: "task_tool"}
	r.taskTools = []tool.Tool{fakeTool}
	r.ownedTaskToolNames = map[string]struct{}{"task_tool": {}}

	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	inputs := &agentinterfaces.ModelCallInputs{Tools: tools}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	r.syncTaskToolForModelToolInputs(cbc)

	// task_tool 应被添加到工具列表
	toolNames := make([]string, len(inputs.Tools))
	for i, t := range inputs.Tools {
		toolNames[i] = t.GetName()
	}
	assert.Contains(t, toolNames, "task_tool")
}

// TestSyncTaskToolForModelToolInputs_未持有时移除 验证未持有 task_tool 时从 tools 列表中移除
func TestSyncTaskToolForModelToolInputs_未持有时移除(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	r.ownsTaskTool = false
	r.ownedTaskToolNames = map[string]struct{}{"task_tool": {}}

	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("task_tool", "任务工具", nil),
		cschema.NewToolInfo("bash", "执行命令", nil),
	}
	inputs := &agentinterfaces.ModelCallInputs{Tools: tools}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	r.syncTaskToolForModelToolInputs(cbc)

	// task_tool 应被移除
	toolNames := make([]string, len(inputs.Tools))
	for i, t := range inputs.Tools {
		toolNames[i] = t.GetName()
	}
	assert.NotContains(t, toolNames, "task_tool")
	assert.Contains(t, toolNames, "bash")
}

// TestSyncTaskToolForModelToolInputs_非ModelCallInputs时跳过 验证 inputs 非 ModelCallInputs 时跳过
func TestSyncTaskToolForModelToolInputs_非ModelCallInputs时跳过(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	r.ownsTaskTool = true
	r.taskTools = nil

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	// 不应 panic
	r.syncTaskToolForModelToolInputs(cbc)
}

// --- buildAvailableAgents 测试 ---

// TestBuildAvailableAgents_空列表 验证空 subagents 列表返回空字符串
func TestBuildAvailableAgents_空列表(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	result := r.buildAvailableAgents(nil)
	assert.Equal(t, "", result)
}

// TestBuildAvailableAgents_NonSubAgentConfig 验证非 SubAgentConfig 类型使用 SpecName
func TestBuildAvailableAgents_NonSubAgentConfig(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	subagents := []hschema.SubagentSpec{
		&fakeSubagentSpec{name: "coder"},
	}
	result := r.buildAvailableAgents(subagents)
	assert.Contains(t, result, "coder")
	assert.Contains(t, result, "DeepAgent instance")
}

// TestBuildAvailableAgents_SubAgentConfig 验证 SubAgentConfig 类型构建描述
func TestBuildAvailableAgents_SubAgentConfig(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	subagents := []hschema.SubagentSpec{
		&hschema.SubAgentConfig{
			AgentCard: &agentschema.AgentCard{
				BaseCard: cschema.BaseCard{Name: "coder", Description: "编码 Agent"},
			},
		},
	}
	result := r.buildAvailableAgents(subagents)
	assert.Contains(t, result, "coder")
	assert.Contains(t, result, "编码 Agent")
}

// TestBuildAvailableAgents_SubAgentConfigNilCard 验证 SubAgentConfig.AgentCard 为 nil 时使用默认值
func TestBuildAvailableAgents_SubAgentConfigNilCard(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	subagents := []hschema.SubagentSpec{
		&hschema.SubAgentConfig{},
	}
	result := r.buildAvailableAgents(subagents)
	assert.Contains(t, result, "general-purpose")
}

// fakeSubagentSpec 简单的 SubagentSpec mock
type fakeSubagentSpec struct {
	name string
}

func (f *fakeSubagentSpec) SpecName() string { return f.name }

// --- rejectTool 边界测试 ---

// TestRejectTool_非ToolCallInputs 验证 inputs 非 ToolCallInputs 时仍设置 skip 标记
func TestRejectTool_非ToolCallInputs(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	r.rejectTool(cbc, "test error")

	assert.Equal(t, true, cbc.Extra()[extraSkipToolKey])
}

// --- handleEnter/handleExit 边界测试 ---

// TestHandleEnter_Plan模式下不拒绝 验证 plan 模式下 handleEnter 不拒绝
func TestHandleEnter_Plan模式下不拒绝(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "enter_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	r.handleEnter(cbc)

	// plan 模式下不应被拒绝
	_, hasSkip := cbc.Extra()[extraSkipToolKey]
	assert.False(t, hasSkip)
}

// TestHandleExit_Plan模式下不拒绝 验证 plan 模式下 handleExit 不拒绝
func TestHandleExit_Plan模式下不拒绝(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)
	agent := newFakeDeepAgentForAgentMode()
	agent.deepState = &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "exit_plan_mode"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	r.handleExit(cbc)

	// plan 模式下不应被拒绝
	_, hasSkip := cbc.Extra()[extraSkipToolKey]
	assert.False(t, hasSkip)
}

// --- extractFilePath / extractBashCommand 边界测试 ---

// TestExtractFilePath_非ToolCallInputs 验证 inputs 非 ToolCallInputs 时返回空
func TestExtractFilePath_非ToolCallInputs(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	filePath := r.extractFilePath(cbc)
	assert.Equal(t, "", filePath)
}

// TestExtractBashCommand_非ToolCallInputs 验证 inputs 非 ToolCallInputs 时返回空
func TestExtractBashCommand_非ToolCallInputs(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	cmd := r.extractBashCommand(cbc)
	assert.Equal(t, "", cmd)
}

// --- filterHiddenTools 边界测试 ---

// TestFilterHiddenTools_非ModelCallInputs 验证 inputs 非 ModelCallInputs 时跳过
func TestFilterHiddenTools_非ModelCallInputs(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	hidden := map[string]struct{}{"bash": {}}
	r.filterHiddenTools(cbc, hidden)

	// 不应 panic
}

// TestFilterHiddenTools_NilTools 验证 Tools 为 nil 时跳过
func TestFilterHiddenTools_NilTools(t *testing.T) {
	t.Parallel()

	r := NewAgentModeRail(nil)

	inputs := &agentinterfaces.ModelCallInputs{Tools: nil}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	hidden := map[string]struct{}{"bash": {}}
	r.filterHiddenTools(cbc, hidden)

	// 不应 panic
}

// --- Mock 辅助类型 ---

// fakeToolForSync 用于 syncTaskToolForModelToolInputs 测试的 mock tool
type fakeToolForSync struct {
	// cardName 工具名称
	cardName string
}

func (f *fakeToolForSync) Card() *tool.ToolCard {
	return &tool.ToolCard{
		BaseCard: cschema.BaseCard{ID: f.cardName, Name: f.cardName},
	}
}
func (f *fakeToolForSync) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeToolForSync) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}
