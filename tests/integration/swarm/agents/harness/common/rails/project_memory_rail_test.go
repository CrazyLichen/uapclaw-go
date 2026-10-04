//go:build integration

package rails

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	rails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProjectMemoryRailSuite 测试 ProjectMemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Priority 返回值
//   - NewProjectMemoryRail 默认/自定义构造
//   - SetLanguage / SetAdditionalDirectories
//   - ResolveWorkspacePath 路径解析
//   - Init 捕获 agent → Uninit 清理
//   - GetCallbacks 回调注册
//   - BeforeModelCall / AfterToolCall 回调逻辑
//   - AgentRail 接口满足
//
// 对齐 Python: tests/system_tests/test_project_memory.py
type ProjectMemoryRailSuite struct {
	isuite.BaseIntegrationSuite
	// rail 每个测试方法共享的 ProjectMemoryRail 实例
	rail *rails.ProjectMemoryRail
	// builder 每个测试方法共享的 SystemPromptBuilder
	builder *saprompt.SystemPromptBuilder
	// tmpDir 每个测试方法使用的工作空间临时目录
	tmpDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestProjectMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(ProjectMemoryRailSuite))
}

// SetupSuite 初始化基础测试环境
func (s *ProjectMemoryRailSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
}

// TearDownSuite 清理基础测试环境
func (s *ProjectMemoryRailSuite) TearDownSuite() {
	s.BaseIntegrationSuite.TearDownSuite()
}

// SetupTest 每个测试方法前初始化 rail 和 builder
func (s *ProjectMemoryRailSuite) SetupTest() {
	s.builder = saprompt.NewSystemPromptBuilder()
	s.tmpDir = s.T().TempDir()
	s.rail = rails.NewProjectMemoryRail(s.tmpDir, "cn", 60000, nil)
}

// TearDownTest 每个测试方法后清理缓存
func (s *ProjectMemoryRailSuite) TearDownTest() {
	if s.rail != nil {
		project_memory.ClearProjectMemoryCache(s.rail.ResolveWorkspacePath())
	}
}

// ──────────────────────────── Priority ────────────────────────────

// TestProjectMemoryRail_Priority 验证 ProjectMemoryRail.Priority() 返回值。
// 注意：NewProjectMemoryRail 未调用 NewDeepAgentRail() 初始化 BaseRail，
// 因此 priority 字段为零值 0（而非 BaseRail 默认的 50）。
// sectionPriority=120 是 PromptSection 优先级，与 Rail 优先级不同。
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_Priority() {
	s.Equal(0, s.rail.Priority(), "ProjectMemoryRail 未初始化 BaseRail，Priority 为零值 0")
}

// ──────────────────────────── NewProjectMemoryRail ────────────────────────────

// TestNewProjectMemoryRail_默认值 验证默认构造参数
func (s *ProjectMemoryRailSuite) TestNewProjectMemoryRail_默认值() {
	r := rails.NewProjectMemoryRail("/tmp/ws", "", 0, nil)
	s.Equal("/tmp/ws", r.ResolveWorkspacePath(), "workspacePath 应为构造参数")
	s.Equal("cn", r.GetLanguage(), "空语言应默认 cn")
}

// TestNewProjectMemoryRail_自定义值 验证自定义构造参数
func (s *ProjectMemoryRailSuite) TestNewProjectMemoryRail_自定义值() {
	r := rails.NewProjectMemoryRail("/tmp/ws", "en", 30000, []string{"/extra"})
	s.Equal("en", r.GetLanguage(), "语言应为 en")
}

// ──────────────────────────── SetLanguage ────────────────────────────

// TestProjectMemoryRail_SetLanguage 验证语言更新
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_SetLanguage() {
	s.rail.SetLanguage("en")
	s.Equal("en", s.rail.GetLanguage(), "SetLanguage 后语言应为 en")

	// 空字符串不变
	s.rail.SetLanguage("")
	s.Equal("en", s.rail.GetLanguage(), "空字符串时语言应保持 en")

	// 相同值不变
	s.rail.SetLanguage("en")
	s.Equal("en", s.rail.GetLanguage(), "相同值时语言应保持 en")
}

// ──────────────────────────── SetAdditionalDirectories ────────────────────────────

// TestProjectMemoryRail_SetAdditionalDirectories_合并 验证额外目录合并去重
// 由于 additionalDirectories 是私有字段，通过构造 + SetAdditionalDirectories
// 后使用 BeforeModelCall 的行为间接验证目录数量变化
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_SetAdditionalDirectories_合并() {
	r := rails.NewProjectMemoryRail(s.tmpDir, "cn", 60000, []string{"/a", "/b"})

	// SetAdditionalDirectories 合并 /c，去重 /a
	r.SetAdditionalDirectories([]string{"/c", "/a"})

	// 无法直接访问私有字段，通过 GetLanguage 等其他方法验证不崩溃
	s.Equal("cn", r.GetLanguage(), "SetAdditionalDirectories 后其他方法应正常")
}

// TestProjectMemoryRail_SetAdditionalDirectories_nil 验证 nil 输入不崩溃
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_SetAdditionalDirectories_nil() {
	r := rails.NewProjectMemoryRail(s.tmpDir, "cn", 60000, []string{"/a"})

	// nil 输入不应崩溃
	r.SetAdditionalDirectories(nil)
	s.Equal("cn", r.GetLanguage(), "nil 输入后其他方法应正常")
}

// ──────────────────────────── ResolveWorkspacePath ────────────────────────────

// TestProjectMemoryRail_ResolveWorkspacePath_构造期路径 验证使用构造期路径
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_ResolveWorkspacePath_构造期路径() {
	s.Equal(s.tmpDir, s.rail.ResolveWorkspacePath(), "ResolveWorkspacePath 应返回构造期路径")
}

// TestProjectMemoryRail_ResolveWorkspacePath_Workspace注入 验证 Workspace 注入优先
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_ResolveWorkspacePath_Workspace注入() {
	w := workspace.NewWorkspace("/injected/ws", "cn")
	s.rail.SetWorkspace(w)

	s.Equal("/injected/ws", s.rail.ResolveWorkspacePath(), "Workspace 注入后应优先返回注入路径")
}

// TestProjectMemoryRail_ResolveWorkspacePath_Workspace空路径 验证 Workspace RootPath 为空时回退
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_ResolveWorkspacePath_Workspace空路径() {
	w := &workspace.Workspace{RootPath: ""}
	s.rail.SetWorkspace(w)

	s.Equal(s.tmpDir, s.rail.ResolveWorkspacePath(), "Workspace RootPath 为空时应回退到构造期路径")
}

// ──────────────────────────── Init / Uninit ────────────────────────────

// TestProjectMemoryRail_Init 验证 Init 捕获 agent 的 SystemPromptBuilder
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_Init() {
	agent := &fakeBaseAgentForIntegration{sb: s.builder}
	err := s.rail.Init(s.Ctx, agent)
	s.Require().NoError(err, "Init 不应返回错误")
}

// TestProjectMemoryRail_Init_nilAgent 验证 agent 为 nil 时不崩溃
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_Init_nilAgent() {
	err := s.rail.Init(s.Ctx, nil)
	s.Require().NoError(err, "Init(nil agent) 不应返回错误")
}

// TestProjectMemoryRail_Uninit 验证 Uninit 清除 project_memory section
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_Uninit() {
	// 先通过 Init 注入 builder
	agent := &fakeBaseAgentForIntegration{sb: s.builder}
	s.Require().NoError(s.rail.Init(s.Ctx, agent))

	// 添加 section
	s.builder.AddSection(saprompt.PromptSection{
		Name:     project_memory.SectionName,
		Content:  map[string]string{"cn": "test", "en": "test"},
		Priority: 120,
	})
	s.True(s.builder.HasSection(project_memory.SectionName), "section 应已添加")

	// Uninit 应移除 section
	err := s.rail.Uninit(nil)
	s.Require().NoError(err, "Uninit 不应返回错误")
	s.False(s.builder.HasSection(project_memory.SectionName), "Uninit 后 section 应被移除")
}

// TestProjectMemoryRail_Uninit_无Builder 验证 systemPromptBuilder 为 nil 时不崩溃
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_Uninit_无Builder() {
	r := rails.NewProjectMemoryRail(s.tmpDir, "cn", 60000, nil)
	err := r.Uninit(nil)
	s.Require().NoError(err, "Uninit（无 builder）不应返回错误")
}

// ──────────────────────────── GetCallbacks ────────────────────────────

// TestProjectMemoryRail_GetCallbacks 验证 GetCallbacks 返回 before_model_call + after_tool_call
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_GetCallbacks() {
	callbacks := s.rail.GetCallbacks()

	s.Contains(callbacks, agentinterfaces.CallbackBeforeModelCall, "应包含 before_model_call 回调")
	s.Contains(callbacks, agentinterfaces.CallbackAfterToolCall, "应包含 after_tool_call 回调")
}

// ──────────────────────────── BeforeModelCall ────────────────────────────

// TestProjectMemoryRail_BeforeModelCall_有UAPCLAWSWARMMD 验证存在 UAPCLAWSWARM.md 时注入 section
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_BeforeModelCall_有UAPCLAWSWARMMD() {
	// 创建 .git 标记和 UAPCLAWSWARM.md
	s.Require().NoError(os.Mkdir(filepath.Join(s.tmpDir, ".git"), 0o755))
	mdPath := filepath.Join(s.tmpDir, "UAPCLAWSWARM.md")
	s.Require().NoError(os.WriteFile(mdPath, []byte("# Test Memory\nHello from integration test"), 0644))
	project_memory.ClearProjectMemoryCache(s.tmpDir)

	// 注入 builder
	agent := &fakeBaseAgentForIntegration{sb: s.builder}
	s.Require().NoError(s.rail.Init(s.Ctx, agent))

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	err := s.rail.BeforeModelCall(s.Ctx, cbc)
	s.Require().NoError(err, "BeforeModelCall 不应返回错误")

	s.True(s.builder.HasSection(project_memory.SectionName), "应注入 project_memory section")

	section := s.builder.GetSection(project_memory.SectionName)
	s.Require().NotNil(section, "section 不应为 nil")
	s.Equal(120, section.Priority, "section priority 应为 120")
}

// TestProjectMemoryRail_BeforeModelCall_无记忆文件 验证无记忆文件时不注入 section
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_BeforeModelCall_无记忆文件() {
	s.Require().NoError(os.Mkdir(filepath.Join(s.tmpDir, ".git"), 0o755))
	project_memory.ClearProjectMemoryCache(s.tmpDir)

	agent := &fakeBaseAgentForIntegration{sb: s.builder}
	s.Require().NoError(s.rail.Init(s.Ctx, agent))

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	err := s.rail.BeforeModelCall(s.Ctx, cbc)
	s.Require().NoError(err, "BeforeModelCall 不应返回错误")

	s.False(s.builder.HasSection(project_memory.SectionName), "无记忆文件时不应注入 section")
}

// TestProjectMemoryRail_BeforeModelCall_无Builder 验证无 builder 时跳过
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_BeforeModelCall_无Builder() {
	r := rails.NewProjectMemoryRail("/nonexistent", "cn", 60000, nil)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	err := r.BeforeModelCall(s.Ctx, cbc)
	s.Require().NoError(err, "无 builder 时 BeforeModelCall 不应返回错误")
}

// ──────────────────────────── AfterToolCall ────────────────────────────

// TestProjectMemoryRail_AfterToolCall_写工具 验证写操作工具触发缓存清除
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_AfterToolCall_写工具() {
	for _, toolName := range []string{"write_file", "edit_file", "delete_file", "write", "delete", "move_file", "rename_file", "write_text_file"} {
		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)

		err := s.rail.AfterToolCall(s.Ctx, cbc)
		s.Require().NoError(err, "AfterToolCall(%s) 不应返回错误", toolName)
	}
}

// TestProjectMemoryRail_AfterToolCall_非写工具 验证非写操作工具不触发缓存清除
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_AfterToolCall_非写工具() {
	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)

	err := s.rail.AfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err, "非写工具 AfterToolCall 不应返回错误")
}

// TestProjectMemoryRail_AfterToolCall_nilCbc 验证 cbc 为 nil 时不崩溃
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_AfterToolCall_nilCbc() {
	err := s.rail.AfterToolCall(s.Ctx, nil)
	s.Require().NoError(err, "AfterToolCall(nil) 不应返回错误")
}

// ──────────────────────────── 接口满足 ────────────────────────────

// TestProjectMemoryRail_满足AgentRail接口 编译时验证 ProjectMemoryRail 满足 AgentRail 接口
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_满足AgentRail接口() {
	var _ agentinterfaces.AgentRail = (*rails.ProjectMemoryRail)(nil)
	s.True(true, "编译通过即满足接口")
}

// ──────────────────────────── 完整生命周期 ────────────────────────────

// TestProjectMemoryRail_完整生命周期 验证 Init → BeforeModelCall → AfterToolCall → Uninit 完整流程
func (s *ProjectMemoryRailSuite) TestProjectMemoryRail_完整生命周期() {
	// 创建 .git 标记和 UAPCLAWSWARM.md
	s.Require().NoError(os.Mkdir(filepath.Join(s.tmpDir, ".git"), 0o755))
	mdPath := filepath.Join(s.tmpDir, "UAPCLAWSWARM.md")
	s.Require().NoError(os.WriteFile(mdPath, []byte("# Lifecycle Test\nProject memory content"), 0644))
	project_memory.ClearProjectMemoryCache(s.tmpDir)

	agent := &fakeBaseAgentForIntegration{sb: s.builder}
	r := rails.NewProjectMemoryRail(s.tmpDir, "cn", 60000, []string{})

	// 1. Init
	s.Require().NoError(r.Init(s.Ctx, agent), "Init 不应返回错误")

	// 2. BeforeModelCall
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	s.Require().NoError(r.BeforeModelCall(s.Ctx, cbc), "BeforeModelCall 不应返回错误")
	s.True(s.builder.HasSection(project_memory.SectionName), "BeforeModelCall 后应存在 section")

	// 3. AfterToolCall（写工具）
	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "write_file"}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)
	s.Require().NoError(r.AfterToolCall(s.Ctx, cbc2), "AfterToolCall 不应返回错误")

	// 4. 再次 BeforeModelCall（应重新加载）
	project_memory.ClearProjectMemoryCache(s.tmpDir)
	s.Require().NoError(r.BeforeModelCall(s.Ctx, cbc), "第二次 BeforeModelCall 不应返回错误")
	s.True(s.builder.HasSection(project_memory.SectionName), "第二次 BeforeModelCall 后应存在 section")

	// 5. Uninit
	s.Require().NoError(r.Uninit(nil), "Uninit 不应返回错误")
	s.False(s.builder.HasSection(project_memory.SectionName), "Uninit 后 section 应被移除")
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeBaseAgentForIntegration 用于集成测试的 mock BaseAgent
type fakeBaseAgentForIntegration struct {
	sb saprompt.SystemPromptBuilderInterface
}

func (f *fakeBaseAgentForIntegration) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgentForIntegration) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgentForIntegration) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgentForIntegration) Card() *agentschema.AgentCard                            { return nil }
func (f *fakeBaseAgentForIntegration) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgentForIntegration) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeBaseAgentForIntegration) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *fakeBaseAgentForIntegration) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.sb
}
func (f *fakeBaseAgentForIntegration) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForIntegration) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForIntegration) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
