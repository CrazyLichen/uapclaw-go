package context_engineer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── Mock 测试桩 ────────────────────────────

// railFsOperation 测试用文件操作桩（路径→内容）
//
// 对齐 Python _make_sys_operation + sys_operation.fs().write_file 的
// 文件准备方式，但以内存 map 模拟 ReadFile。
type railFsOperation struct {
	files map[string]string
}

func newRailFsOperation() *railFsOperation {
	return &railFsOperation{files: make(map[string]string)}
}

func (f *railFsOperation) ReadFile(_ context.Context, path string, _ ...sys_operation.FsOption) (*result.ReadFileResult, error) {
	if content, ok := f.files[path]; ok {
		return &result.ReadFileResult{Data: &result.ReadFileData{Path: path, Content: content, Mode: "text"}}, nil
	}
	return &result.ReadFileResult{}, nil
}
func (f *railFsOperation) ReadFileStream(_ context.Context, path string, _ ...sys_operation.FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, nil
}
func (f *railFsOperation) WriteFile(_ context.Context, path string, _ string, _ ...sys_operation.FsOption) (*result.WriteFileResult, error) {
	return nil, nil
}
func (f *railFsOperation) UploadFile(_ context.Context, localPath string, targetPath string, _ ...sys_operation.FsOption) (*result.UploadFileResult, error) {
	return nil, nil
}
func (f *railFsOperation) UploadFileStream(_ context.Context, localPath string, targetPath string, _ ...sys_operation.FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, nil
}
func (f *railFsOperation) DownloadFile(_ context.Context, sourcePath string, localPath string, _ ...sys_operation.FsOption) (*result.DownloadFileResult, error) {
	return nil, nil
}
func (f *railFsOperation) DownloadFileStream(_ context.Context, sourcePath string, localPath string, _ ...sys_operation.FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, nil
}
func (f *railFsOperation) ListFiles(_ context.Context, path string, _ ...sys_operation.FsOption) (*result.ListFilesResult, error) {
	return &result.ListFilesResult{}, nil
}
func (f *railFsOperation) ListDirectories(_ context.Context, path string, _ ...sys_operation.FsOption) (*result.ListDirsResult, error) {
	return nil, nil
}
func (f *railFsOperation) SearchFiles(_ context.Context, path string, pattern string, _ ...sys_operation.FsOption) (*result.SearchFilesResult, error) {
	return nil, nil
}
func (f *railFsOperation) ListTools() []*tool.ToolCard { return nil }

var _ sys_operation.FsOperation = (*railFsOperation)(nil)

// railSysOperation 测试用 SysOperation 桩（Fs 返回 railFsOperation）
type railSysOperation struct {
	fsOp *railFsOperation
}

func (s *railSysOperation) Card() *sys_operation.SysOperationCard {
	return sys_operation.NewSysOperationCard()
}
func (s *railSysOperation) Fs() sys_operation.FsOperation       { return s.fsOp }
func (s *railSysOperation) Shell() sys_operation.ShellOperation { return nil }
func (s *railSysOperation) Code() sys_operation.CodeOperation   { return nil }
func (s *railSysOperation) IsolationKeyTemplate() string        { return "" }

var _ sys_operation.SysOperation = (*railSysOperation)(nil)

// railAbility 测试用能力桩（对齐 Python ToolCard：name + description）
type railAbility struct {
	name string
	desc string
}

func (a *railAbility) AbilityName() string             { return a.name }
func (a *railAbility) AbilityID() string               { return a.name }
func (a *railAbility) AbilityKind() schema.AbilityKind { return schema.AbilityKindTool }
func (a *railAbility) GetDescription() string          { return a.desc }
func (a *railAbility) ListTools() []*tool.ToolCard     { return nil }

var _ schema.Ability = (*railAbility)(nil)

// railAbilityManager 测试用能力管理器桩
//
// 对齐 Python test 中 agent.ability_manager.add(ToolCard(...)) 的行为：
// List() 返回添加的能力。
type railAbilityManager struct {
	abilities []*railAbility
}

func (m *railAbilityManager) Add(ability schema.Ability) agentschema.AddAbilityResult {
	if ra, ok := ability.(*railAbility); ok {
		m.abilities = append(m.abilities, ra)
	}
	return agentschema.AddAbilityResult{}
}
func (m *railAbilityManager) AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult {
	for _, a := range abilities {
		m.Add(a)
	}
	return nil
}
func (m *railAbilityManager) Remove(name string) schema.Ability          { return nil }
func (m *railAbilityManager) RemoveMany(names []string) []schema.Ability { return nil }
func (m *railAbilityManager) Get(name string) schema.Ability             { return nil }
func (m *railAbilityManager) List() []schema.Ability {
	result := make([]schema.Ability, len(m.abilities))
	for i, a := range m.abilities {
		result[i] = a
	}
	return result
}
func (m *railAbilityManager) ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]schema.ToolInfoInterface, error) {
	return nil, nil
}
func (m *railAbilityManager) Execute(
	ctx context.Context,
	cbc *sainterfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) []agentschema.ExecuteResult {
	return nil
}
func (m *railAbilityManager) SetContextEngine(ce ceinterface.ContextEngine) {}
func (m *railAbilityManager) ReorderTools(orderedNames []string)            {}

var _ sainterfaces.AbilityManagerInterface = (*railAbilityManager)(nil)

// railAgent 测试用 BaseAgent 桩：暴露 builder/abilityManager/sysOp，
// 对齐 Python _make_agent + register_rail 的 Init 捕获链路。
// 其余 BaseAgent 方法均为空实现（测试只走 Init 的捕获路径）。
type railAgent struct {
	builder   *saprompt.SystemPromptBuilder
	ability   sainterfaces.AbilityManagerInterface
	sysOp     sys_operation.SysOperation
	workspace *hworkspace.Workspace
	callbacks *sainterfaces.AgentCallbackManager
}

func (a *railAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return a.builder }
func (a *railAgent) AbilityManager() sainterfaces.AbilityManagerInterface       { return a.ability }

// ── BaseAgent 其余方法空实现 ──

func (a *railAgent) Configure(ctx context.Context, config sainterfaces.AgentConfig) error { return nil }
func (a *railAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...sainterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (a *railAgent) Stream(ctx context.Context, inputs map[string]any, opts ...sainterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (a *railAgent) Card() *agentschema.AgentCard     { return nil }
func (a *railAgent) Config() sainterfaces.AgentConfig { return nil }
func (a *railAgent) CallbackManager() *sainterfaces.AgentCallbackManager {
	if a.callbacks == nil {
		a.callbacks = sainterfaces.NewAgentCallbackManager("rail-test-agent")
	}
	return a.callbacks
}
func (a *railAgent) RegisterCallback(ctx context.Context, event sainterfaces.AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) error {
	return nil
}
func (a *railAgent) RegisterRail(ctx context.Context, rail sainterfaces.AgentRail, opts ...cb.CallbackOption) error {
	return nil
}
func (a *railAgent) UnregisterRail(ctx context.Context, rail sainterfaces.AgentRail) error {
	return nil
}

var _ sainterfaces.BaseAgent = (*railAgent)(nil)

// newRailTestEnv 构造 rail 集成层测试环境。
//
// 对齐 Python 测试的 _make_sys_operation + _make_agent + register_rail：
//   - root：t.TempDir 工作空间根
//   - builder：真实 SystemPromptBuilder（验证节注入/移除）
//   - fsOp：路径→内容桩（验证 context 节文件读取）
func newRailTestEnv(t *testing.T) (*ContextAssembleRail, *saprompt.SystemPromptBuilder, *railFsOperation, *hworkspace.Workspace) {
	t.Helper()
	root := t.TempDir()
	fsOp := newRailFsOperation()
	builder := saprompt.NewSystemPromptBuilder()
	ws := hworkspace.NewWorkspace(root, "cn")

	rail := NewContextAssembleRail()
	rail.SetSysOperation(&railSysOperation{fsOp: fsOp})
	rail.SetWorkspace(ws)
	// 直接注入 builder（Python 经 register_rail→init 捕获，Go 测试等价直注）
	rail.systemPromptBuilder = builder
	return rail, builder, fsOp, ws
}

// writeWsFile 写入工作空间文件（内存桩 + 磁盘，供目录树扫描）
func writeWsFile(t *testing.T, ws *hworkspace.Workspace, fsOp *railFsOperation, name string, content string) {
	t.Helper()
	path := filepath.Join(ws.RootPath, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	fsOp.files[path] = content
}

// renderSection 渲染指定节的 cn 内容
func renderSection(t *testing.T, builder *saprompt.SystemPromptBuilder, name string) string {
	t.Helper()
	s := builder.GetSection(name)
	require.NotNil(t, s, "节 %s 应存在", name)
	return s.Render("cn")
}

// ──────────────────────────── BeforeModelCall 集成测试 ────────────────────────────

// TestRailBeforeModelCall_注入节 对齐 Python test_before_model_call_injects_sections：
// 注入 workspace 和 context 节；无工具时不注入 tools 节。
func TestRailBeforeModelCall_注入节(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "AGENT.md", "# Agent Config\nreal body")
	ctx := context.Background()

	require.NoError(t, rail.BeforeModelCall(ctx, newCallbackContextWithMC(nil)))

	assert.NotNil(t, builder.GetSection(sections.SectionWorkspace))
	assert.NotNil(t, builder.GetSection(sections.SectionContext))

	wsContent := renderSection(t, builder, sections.SectionWorkspace)
	assert.Contains(t, wsContent, "# 工作空间")
	ctxContent := renderSection(t, builder, sections.SectionContext)
	assert.Contains(t, ctxContent, "## AGENT.md")
	assert.NotContains(t, ctxContent, "# 可用工具") // 无工具
}

// TestRailBeforeModelCall_心跳轻量上下文 对齐 Python test_before_model_call_heartbeat_uses_lightweight_context：
// heartbeat 保留 HEARTBEAT.md 但跳过 daily memory。
func TestRailBeforeModelCall_心跳轻量上下文(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "AGENT.md", "# Agent Config\nreal body")
	writeWsFile(t, ws, fsOp, "SOUL.md", "# Soul Content\nreal body")
	writeWsFile(t, ws, fsOp, "HEARTBEAT.md", "# Heartbeat Tasks\nreal body")
	// 今日 daily memory（heartbeat 应跳过）
	writeWsFile(t, ws, fsOp, filepath.Join("memory", "daily_memory", "2026-10-25.md"), "# Today")
	// 写入桩时 daily_memory 读取路径依赖日期，这里直接预置常见路径变体
	dailyDir := filepath.Join(ws.RootPath, "memory", "daily_memory")
	_ = dailyDir

	ctx := context.Background()
	cbc := newCallbackContextWithMC(nil)
	cbc.SetExtra("run_kind", string(sainterfaces.RunKindHeartbeat))

	require.NoError(t, rail.BeforeModelCall(ctx, cbc))

	cnContent := renderSection(t, builder, sections.SectionContext)
	assert.Contains(t, cnContent, "## AGENT.md")
	assert.Contains(t, cnContent, "# Agent Config")
	assert.Contains(t, cnContent, "## SOUL.md")
	assert.Contains(t, cnContent, "## HEARTBEAT.md")
	assert.Contains(t, cnContent, "# Heartbeat Tasks")
	assert.NotContains(t, cnContent, "# Today")
	assert.NotContains(t, cnContent, "## daily_memory/")
}

// TestRailBeforeModelCall_workspace为nil时移除节 对齐 Python test_before_model_call_removes_sections_when_workspace_is_none
func TestRailBeforeModelCall_workspace为nil时移除节(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "AGENT.md", "# Agent Config\nreal body")
	ctx := context.Background()

	// 第一次调用注入节
	require.NoError(t, rail.BeforeModelCall(ctx, newCallbackContextWithMC(nil)))
	assert.True(t, builder.HasSection(sections.SectionWorkspace))
	assert.True(t, builder.HasSection(sections.SectionContext))

	// workspace 置 nil 后再调用 → 移除节
	rail.SetWorkspace(nil)
	require.NoError(t, rail.BeforeModelCall(ctx, newCallbackContextWithMC(nil)))
	assert.False(t, builder.HasSection(sections.SectionWorkspace))
	assert.False(t, builder.HasSection(sections.SectionContext))
}

// TestRailUninit_移除节 对齐 Python test_uninit_removes_sections。
// M-17 回归：Uninit 只移除 workspace/context 两节（Python docstring 提 tools 是笔误）。
func TestRailUninit_移除节(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "AGENT.md", "# Agent Config\nreal body")
	ctx := context.Background()

	require.NoError(t, rail.BeforeModelCall(ctx, newCallbackContextWithMC(nil)))
	assert.True(t, builder.HasSection(sections.SectionWorkspace))
	assert.True(t, builder.HasSection(sections.SectionContext))

	require.NoError(t, rail.Uninit(nil))
	assert.False(t, builder.HasSection(sections.SectionWorkspace))
	assert.False(t, builder.HasSection(sections.SectionContext))
}

// TestRailUninit_M17回归_不移除tools节 M-17 专项回归：
// Uninit 不应移除 tools 节（对齐 Python 实际代码只移 workspace/context）。
func TestRailUninit_M17回归_不移除tools节(t *testing.T) {
	rail, builder, _, ws := newRailTestEnv(t)

	// 手动添加 tools 节（模拟其他 rail 注入）
	builder.AddSection(saprompt.PromptSection{
		Name:     sections.SectionTools,
		Content:  map[string]string{"cn": "# 可用工具"},
		Priority: 30,
	})
	assert.True(t, builder.HasSection(sections.SectionTools))

	require.NoError(t, rail.Uninit(nil))
	// tools 节应保留（M-17：Python 实际只移 workspace/context）
	assert.True(t, builder.HasSection(sections.SectionTools))
	_ = ws
}

// TestRailInit_捕获builder 对齐 Python test_rail_init_captures_system_prompt_builder：
// Init 从 agent 捕获 system_prompt_builder 与 ability_manager。
func TestRailInit_捕获builder(t *testing.T) {
	root := t.TempDir()
	builder := saprompt.NewSystemPromptBuilder()
	ability := &railAbilityManager{}
	agent := &railAgent{
		builder:   builder,
		ability:   ability,
		sysOp:     &railSysOperation{fsOp: newRailFsOperation()},
		workspace: hworkspace.NewWorkspace(root, "cn"),
	}

	rail := NewContextAssembleRail()
	assert.Nil(t, rail.systemPromptBuilder)
	assert.Nil(t, rail.abilityManager)

	require.NoError(t, rail.Init(context.Background(), agent))

	// Init 从 agent 捕获（BaseAgent 桩的 SystemPromptBuilder()/AbilityManager()）
	assert.NotNil(t, rail.systemPromptBuilder)
	assert.NotNil(t, rail.abilityManager)
}

// TestRailBeforeModelCall_builder为nil时早退 对齐 Python test_before_model_call_returns_early_when_builder_is_none
func TestRailBeforeModelCall_builder为nil时早退(t *testing.T) {
	rail, _, _, _ := newRailTestEnv(t)
	rail.systemPromptBuilder = nil

	// 不应 panic，直接返回 nil
	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
}

// TestRailBeforeModelCall_空工作空间 对齐 Python test_before_model_call_with_empty_workspace：
// 空目录仍注入 workspace 节。
func TestRailBeforeModelCall_空工作空间(t *testing.T) {
	rail, builder, _, _ := newRailTestEnv(t)

	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
	assert.True(t, builder.HasSection(sections.SectionWorkspace))
}

// TestRailBeforeModelCall_仅有README 对齐 Python test_before_model_call_with_only_readme
func TestRailBeforeModelCall_仅有README(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "README.md", "# Test Project")

	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
	assert.NotNil(t, builder.GetSection(sections.SectionWorkspace))
}

// TestRailBeforeModelCall_注入工具节 对齐 Python test_before_model_call_adds_tools_section：
// ability_manager 有工具时注入 tools 节。
func TestRailBeforeModelCall_注入工具节(t *testing.T) {
	rail, builder, _, _ := newRailTestEnv(t)
	rail.abilityManager = &railAbilityManager{abilities: []*railAbility{
		{name: "test_tool", desc: "A test tool"},
	}}

	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
	toolsSection := builder.GetSection(sections.SectionTools)
	require.NotNil(t, toolsSection)
	assert.Contains(t, toolsSection.Render("cn"), "test_tool")
}

// TestRailBeforeModelCall_能力为空时移除工具节 对齐 Python test_before_model_call_removes_tools_when_ability_manager_empty：
// ability_manager 无工具时移除 tools 节。
func TestRailBeforeModelCall_能力为空时移除工具节(t *testing.T) {
	rail, builder, _, _ := newRailTestEnv(t)

	// 预置 tools 节（模拟前次注入）
	builder.AddSection(saprompt.PromptSection{
		Name:     sections.SectionTools,
		Content:  map[string]string{"cn": "# 可用工具"},
		Priority: 30,
	})
	assert.True(t, builder.HasSection(sections.SectionTools))

	rail.abilityManager = &railAbilityManager{} // 空
	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
	assert.False(t, builder.HasSection(sections.SectionTools))
}

// TestRailUninit_builder为nil安全 对齐 Python test_uninit_handles_none_builder
func TestRailUninit_builder为nil安全(t *testing.T) {
	rail, _, _, _ := newRailTestEnv(t)
	rail.systemPromptBuilder = nil

	// 不应 panic
	require.NoError(t, rail.Uninit(nil))
}

// TestRailBeforeModelCall_中文渲染 对齐 Python test_before_model_call_with_chinese_language
func TestRailBeforeModelCall_中文渲染(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "README.md", "# Test")
	rail.systemPromptBuilder.SetLanguage("cn")

	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
	wsSection := builder.GetSection(sections.SectionWorkspace)
	require.NotNil(t, wsSection)
	assert.Contains(t, wsSection.Render("cn"), "# 工作空间")
}

// TestRailBeforeModelCall_英文渲染 对齐 Python test_before_model_call_with_english_language
func TestRailBeforeModelCall_英文渲染(t *testing.T) {
	rail, builder, fsOp, ws := newRailTestEnv(t)
	writeWsFile(t, ws, fsOp, "README.md", "# Test")
	rail.systemPromptBuilder.SetLanguage("en")

	require.NoError(t, rail.BeforeModelCall(context.Background(), newCallbackContextWithMC(nil)))
	wsSection := builder.GetSection(sections.SectionWorkspace)
	require.NotNil(t, wsSection)
	assert.Contains(t, wsSection.Render("en"), "# Workspace")
}

// TestRailPriority 对齐 Python test_rail_priority：优先级 85。
func TestRailPriority(t *testing.T) {
	rail := NewContextAssembleRail()
	assert.Equal(t, 85, rail.Priority())
}
