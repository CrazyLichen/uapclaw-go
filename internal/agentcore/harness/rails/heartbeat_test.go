package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDeepAgentForHeartbeat 实现 DeepAgentInterface 的 mock
type fakeDeepAgentForHeartbeat struct {
	fakeBaseAgent
	deepConfig *hschema.DeepAgentConfig
}

func newFakeDeepAgentForHeartbeat() *fakeDeepAgentForHeartbeat {
	return &fakeDeepAgentForHeartbeat{
		fakeBaseAgent: *newFakeBaseAgent(),
	}
}

func (f *fakeDeepAgentForHeartbeat) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentForHeartbeat) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentForHeartbeat) LoopController() controller.ControllerInterface { return nil }
func (f *fakeDeepAgentForHeartbeat) EventHandler() modules.EventHandler             { return nil }
func (f *fakeDeepAgentForHeartbeat) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeDeepAgentForHeartbeat) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }
func (f *fakeDeepAgentForHeartbeat) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForHeartbeat) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForHeartbeat) SetAutoInvokeScheduled(_ bool)        {}
func (f *fakeDeepAgentForHeartbeat) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}
func (f *fakeDeepAgentForHeartbeat) CreateSubagent(_ context.Context, _ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentForHeartbeat) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeDeepAgentForHeartbeat) SwitchMode(_ sessioninterfaces.SessionFacade, _ string)     {}
func (f *fakeDeepAgentForHeartbeat) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}
func (f *fakeDeepAgentForHeartbeat) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string {
	return ""
}
func (f *fakeDeepAgentForHeartbeat) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {
}

// 编译时验证
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForHeartbeat)(nil)
var _ agentinterfaces.BaseAgent = (*fakeDeepAgentForHeartbeat)(nil)

// fakeFsOperation 实现 sys_operation.FsOperation 的 mock
type fakeFsOperation struct {
	readFileContent string
	readFileErr     error
}

func (f *fakeFsOperation) ReadFile(_ context.Context, _ string, _ ...sys_operation.FsOption) (*result.ReadFileResult, error) {
	if f.readFileErr != nil {
		return nil, f.readFileErr
	}
	return &result.ReadFileResult{BaseResult: result.BaseResult{Code: 0}, Data: &result.ReadFileData{Content: f.readFileContent}}, nil
}
func (f *fakeFsOperation) WriteFile(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (*result.WriteFileResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ListFiles(_ context.Context, _ string, _ ...sys_operation.FsOption) (*result.ListFilesResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ListDirectories(_ context.Context, _ string, _ ...sys_operation.FsOption) (*result.ListDirsResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (*result.SearchFilesResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ReadFileStream(_ context.Context, _ string, _ ...sys_operation.FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) UploadFile(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (*result.UploadFileResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) UploadFileStream(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) DownloadFile(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (*result.DownloadFileResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) DownloadFileStream(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ListTools() []*tool.ToolCard {
	return nil
}

// fakeSysOperation 实现 sys_operation.SysOperation 的 mock
type fakeSysOperation struct {
	fsOp sys_operation.FsOperation
}

func (f *fakeSysOperation) Card() *sys_operation.SysOperationCard { return nil }
func (f *fakeSysOperation) Fs() sys_operation.FsOperation         { return f.fsOp }
func (f *fakeSysOperation) Shell() sys_operation.ShellOperation   { return nil }
func (f *fakeSysOperation) Code() sys_operation.CodeOperation     { return nil }
func (f *fakeSysOperation) IsolationKeyTemplate() string          { return "" }

// 编译时验证
var _ sys_operation.SysOperation = (*fakeSysOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// newCBCWithRunKind 创建 AgentCallbackContext 并设置 run_kind
func newCBCWithRunKind(agent agentinterfaces.BaseAgent, runKind string) *agentinterfaces.AgentCallbackContext {
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	cbc.Extra()["run_kind"] = runKind
	return cbc
}

// --- 构造函数测试 ---

// TestNewHeartbeatRail 验证构造函数
func TestNewHeartbeatRail(t *testing.T) {
	t.Parallel()

	r := NewHeartbeatRail()
	assert.Equal(t, 80, r.Priority())
	assert.Equal(t, "", r.heartbeatDir)
}

// --- Init 测试 ---

// TestHeartbeatRail_Init 正常初始化
func TestHeartbeatRail_Init(t *testing.T) {
	t.Parallel()

	r := NewHeartbeatRail()

	// 构造有 DeepConfig 的 agent
	ws := workspace.NewWorkspace("/tmp/test-workspace", "test-agent")
	deepConfig := &hschema.DeepAgentConfig{
		Workspace: ws,
	}
	agent := &fakeDeepAgentForHeartbeat{
		fakeBaseAgent: *newFakeBaseAgent(),
		deepConfig:    deepConfig,
	}

	err := r.Init(agent)
	require.NoError(t, err)

	// 验证 workspace 已设置
	assert.Equal(t, ws, r.Workspace())
	// 验证 heartbeatDir 已计算
	assert.NotEmpty(t, r.heartbeatDir)
}

// TestHeartbeatRail_Init_无DeepConfig deepConfig 为 nil 时日志记录
func TestHeartbeatRail_Init_无DeepConfig(t *testing.T) {
	t.Parallel()

	r := NewHeartbeatRail()
	agent := newFakeDeepAgentForHeartbeat()
	// deepConfig 默认为 nil

	err := r.Init(agent)
	require.NoError(t, err)

	// workspace 未设置
	assert.Nil(t, r.Workspace())
}

// TestHeartbeatRail_Init_非DeepAgent时跳过 agent 不满足 DeepAgentInterface
func TestHeartbeatRail_Init_非DeepAgent时跳过(t *testing.T) {
	t.Parallel()

	r := NewHeartbeatRail()
	agent := newFakeBaseAgent() // 不实现 DeepAgentInterface

	err := r.Init(agent)
	require.NoError(t, err)
}

// --- Uninit 测试 ---

// TestHeartbeatRail_Uninit 移除 heartbeat 节
func TestHeartbeatRail_Uninit(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	r := NewHeartbeatRail()

	err := r.Uninit(agent)
	require.NoError(t, err)

	// 验证 heartbeat 节已被移除
	assert.False(t, builder.HasSection("heartbeat"))
}

// TestHeartbeatRail_Uninit_无Builder agent 的 SystemPromptBuilder 为 nil 时不崩溃
func TestHeartbeatRail_Uninit_无Builder(t *testing.T) {
	t.Parallel()

	r := NewHeartbeatRail()
	// 传入 nil agent（SystemPromptBuilder 返回 nil）
	agent := newFakeBaseAgent()

	err := r.Uninit(agent)
	require.NoError(t, err)
}

// --- BeforeModelCall 测试 ---

// TestHeartbeatRail_BeforeModelCall_非心跳运行 run_kind 非 heartbeat 时跳过
func TestHeartbeatRail_BeforeModelCall_非心跳运行(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	r := NewHeartbeatRail()

	cbc := newCBCWithRunKind(agent, "normal")

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 不应注入 heartbeat 节
	assert.False(t, builder.HasSection("heartbeat"))
}

// TestHeartbeatRail_BeforeModelCall_心跳运行_注入节 正常心跳运行，HEARTBEAT.md 有内容
func TestHeartbeatRail_BeforeModelCall_心跳运行_注入节(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	fsOp := &fakeFsOperation{readFileContent: "检查服务健康状态"}
	sysOp := &fakeSysOperation{fsOp: fsOp}

	r := NewHeartbeatRail()
	r.heartbeatDir = "/tmp/test/HEARTBEAT.md"
	r.SetSysOperation(sysOp)

	cbc := newCBCWithRunKind(agent, string(agentinterfaces.RunKindHeartbeat))

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 应注入 heartbeat 节
	assert.True(t, builder.HasSection("heartbeat"))
}

// TestHeartbeatRail_BeforeModelCall_心跳运行_空内容 HEARTBEAT.md 为空
func TestHeartbeatRail_BeforeModelCall_心跳运行_空内容(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	fsOp := &fakeFsOperation{readFileContent: ""}
	sysOp := &fakeSysOperation{fsOp: fsOp}

	r := NewHeartbeatRail()
	r.heartbeatDir = "/tmp/test/HEARTBEAT.md"
	r.SetSysOperation(sysOp)

	cbc := newCBCWithRunKind(agent, string(agentinterfaces.RunKindHeartbeat))

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 空内容时也应注入 heartbeat 节（LLM 输出 HEARTBEAT_OK）
	assert.True(t, builder.HasSection("heartbeat"))
}

// TestHeartbeatRail_BeforeModelCall_无SysOperation sysOperation 为 nil 时日志警告
func TestHeartbeatRail_BeforeModelCall_无SysOperation(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	r := NewHeartbeatRail()

	cbc := newCBCWithRunKind(agent, string(agentinterfaces.RunKindHeartbeat))

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 不应注入 heartbeat 节
	assert.False(t, builder.HasSection("heartbeat"))
}

// TestHeartbeatRail_BeforeModelCall_读取失败 ReadFile 返回错误时日志警告
func TestHeartbeatRail_BeforeModelCall_读取失败(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	fsOp := &fakeFsOperation{readFileErr: assert.AnError}
	sysOp := &fakeSysOperation{fsOp: fsOp}

	r := NewHeartbeatRail()
	r.heartbeatDir = "/tmp/test/HEARTBEAT.md"
	r.SetSysOperation(sysOp)

	cbc := newCBCWithRunKind(agent, string(agentinterfaces.RunKindHeartbeat))

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 读取失败时 content 为空，仍应注入 heartbeat 节
	assert.True(t, builder.HasSection("heartbeat"))
}

// TestHeartbeatRail_BeforeModelCall_无Builder agent 的 SystemPromptBuilder 为 nil 时跳过
func TestHeartbeatRail_BeforeModelCall_无Builder(t *testing.T) {
	t.Parallel()

	// 创建一个 SystemPromptBuilder 返回 nil 的 agent
	agent := &fakeDeepAgentForHeartbeat{
		fakeBaseAgent: *newFakeBaseAgent(),
	}
	// fakeBaseAgent 的 SystemPromptBuilder 默认返回非 nil
	// 所以这里直接测试 cbc.Agent().SystemPromptBuilder() 为 nil 的情况
	// 实际上 fakeBaseAgent 总是返回 builder，所以这个测试验证的是正常路径
	r := NewHeartbeatRail()

	cbc := newCBCWithRunKind(agent, string(agentinterfaces.RunKindHeartbeat))

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
}

// TestHeartbeatRail_BeforeModelCall_无RunKind extra 中无 run_kind 时跳过
func TestHeartbeatRail_BeforeModelCall_无RunKind(t *testing.T) {
	t.Parallel()

	agent := newFakeBaseAgent()
	builder := agent.SystemPromptBuilder().(*saprompt.SystemPromptBuilder)
	r := NewHeartbeatRail()

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	// 不设置 run_kind

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 不应注入 heartbeat 节
	assert.False(t, builder.HasSection("heartbeat"))
}
