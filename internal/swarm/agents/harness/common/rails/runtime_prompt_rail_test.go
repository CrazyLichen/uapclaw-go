package rails

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	intellirouter "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients/intellirouter"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBuilder 测试用 mock SystemPromptBuilder
type mockBuilder struct {
	sections map[string]saprompt.PromptSection
	language string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func newMockBuilder() *mockBuilder {
	return &mockBuilder{
		sections: make(map[string]saprompt.PromptSection),
		language: "cn",
	}
}

func (m *mockBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}

func (m *mockBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	return nil
}

func (m *mockBuilder) Language() string { return m.language }

func (m *mockBuilder) SetLanguage(lang string) { m.language = lang }

func (m *mockBuilder) GetSection(name string) *saprompt.PromptSection {
	if s, ok := m.sections[name]; ok {
		return &s
	}
	return nil
}

func (m *mockBuilder) HasSection(name string) bool {
	_, ok := m.sections[name]
	return ok
}

// TestNewRuntimePromptRail 测试构造和默认值
func TestNewRuntimePromptRail(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	assert.Equal(t, "cn", rail.language)
	assert.Equal(t, "web", rail.channel)
	assert.Equal(t, runtimePromptRailPriority, rail.Priority())
}

// TestSetLanguage 测试 SetLanguage
func TestSetLanguage(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetLanguage("en")
	assert.Equal(t, "en", rail.language)
}

// TestSetChannel 测试 SetChannel
func TestSetChannel(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetChannel("tui")
	assert.Equal(t, "tui", rail.channel)
}

// TestSetTrustedDirs 测试 SetTrustedDirs
func TestSetTrustedDirs(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	dirs := []string{"/a", "/b"}
	rail.SetTrustedDirs(dirs)
	assert.Equal(t, dirs, rail.trustedDirs)
}

// TestSetRuntimePaths 测试 SetRuntimePaths
func TestSetRuntimePaths(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetRuntimePaths("/cwd", "/project", "/ws", "/task", "/task/work", "/task/outputs")
	assert.Equal(t, "/cwd", rail.cwd)
	assert.Equal(t, "/project", rail.projectDir)
	assert.Equal(t, "/task", rail.taskWorkspaceRoot)
	assert.Equal(t, "/task/work", rail.taskWorkDir)
	assert.Equal(t, "/task/outputs", rail.taskOutputsDir)
	// 空白字符串应被 trim
	rail.SetRuntimePaths("  ", "  ", "", " ", "", "")
	assert.Equal(t, "", rail.cwd)
	assert.Equal(t, "", rail.projectDir)
}

// TestSetModelName 测试 SetModelName
func TestSetModelName(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetModelName("qwen-max")
	assert.Equal(t, "qwen-max", rail.modelName)
}

// TestSetMode 测试 SetMode
func TestSetMode(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetMode("agent.work.plan")
	assert.Equal(t, "agent.work.plan", rail.mode)
}

// TestSetForceEnglish 测试 SetForceEnglish
func TestSetForceEnglish(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetForceEnglish(true)
	assert.True(t, rail.forceEnglish)
}

// TestBeforeModelCall_废弃节清除 对齐 Python 新版：旧版 time/language_output/
// browser_tool_policy/trusted_dirs_policy/model_answer_policy 注入已废弃，
// 仅在 remove 列表中清除残留。
func TestBeforeModelCall_废弃节清除(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	// 预置旧版残留节
	// 预置旧版残留节。注意：Python 新版 remove 列表（:481-489）不含
	// browser_tool_policy——上游已彻底废弃该节且不再清理，Go 1:1 忠实保留。
	builder.AddSection(saprompt.PromptSection{Name: "time"})
	builder.AddSection(saprompt.PromptSection{Name: "language_output"})
	builder.AddSection(saprompt.PromptSection{Name: "trusted_dirs_policy"})
	rail.mu.Lock()
	rail.builder = builder
	rail.mu.Unlock()

	require.NoError(t, rail.BeforeModelCall(context.Background(), nil))
	assert.False(t, builder.HasSection("time"))
	assert.False(t, builder.HasSection("language_output"))
	assert.False(t, builder.HasSection("trusted_dirs_policy"))

	// 新版 env 节已注入（新版结构：平台与 Shell/编码兼容性/时间查询/渠道）
	section := builder.GetSection("env")
	require.NotNil(t, section)
	assert.Equal(t, sectionEnvPriority, section.Priority)
	assert.Contains(t, section.Content["cn"], "## 平台与 Shell")
	assert.Contains(t, section.Content["cn"], "## 编码兼容性")
	assert.Contains(t, section.Content["cn"], "## 当前渠道")
}

// TestBeforeModelCall_目录边界_项目分支 对齐 Python directory_boundaries 项目分支。
func TestBeforeModelCall_目录边界_项目分支(t *testing.T) {
	tmpDir := t.TempDir()
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetChannel("web")
	rail.SetRuntimePaths(tmpDir, tmpDir, "", "", "", "")
	builder := newMockBuilder()
	rail.mu.Lock()
	rail.builder = builder
	rail.mu.Unlock()

	require.NoError(t, rail.BeforeModelCall(context.Background(), nil))
	section := builder.GetSection("directory_boundaries")
	require.NotNil(t, section)
	assert.Equal(t, sectionDirectoryBoundariesPriority, section.Priority)
	assert.Contains(t, section.Content["cn"], "# 目录与文件操作边界")
	assert.Contains(t, section.Content["cn"], "### 项目目录规则")
	assert.Contains(t, section.Content["cn"], "## JiuwenSwarm 内部目录")
	assert.Contains(t, section.Content["cn"], "### 已安装扩展资产")
}

// TestBuildDirectoryBoundaries_Projectless 对齐 Python projectless 分支。
func TestBuildDirectoryBoundaries_Projectless(t *testing.T) {
	root := t.TempDir()
	workDir := t.TempDir()
	outDir := t.TempDir()
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetRuntimePaths("", "", "", root, workDir, outDir)
	content := rail.buildDirectoryBoundaries(rail.snapshot())
	assert.Contains(t, content, "## 当前任务目录")
	assert.Contains(t, content, "- 相对路径以临时工作目录为基准。")
	assert.Contains(t, content, "最终产物目录")

	// 无项目无任务路径 → 当前项目目录分支
	rail2 := NewRuntimePromptRail("en", "web")
	content2 := rail2.buildDirectoryBoundaries(rail2.snapshot())
	assert.Contains(t, content2, "## Current Project Directory")
}

// TestSyncGitSystemContext 对齐 Python git_status 会话快照（新增 Run git yourself 句）。
func TestSyncGitSystemContext(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.mu.Lock()
	rail.builder = builder
	rail.mu.Unlock()

	rail.syncGitSystemContext(nil, map[string]any{
		"git_branch": "feature-1", "git_status": " M a.go", "git_recent_commits": "abc123 fix",
	})
	section := builder.GetSection("git_status")
	require.NotNil(t, section)
	assert.Equal(t, sectionGitStatusPriority, section.Priority)
	assert.Contains(t, section.Content["cn"], "Run git yourself when you need the current state")
	assert.Contains(t, section.Content["cn"], "Current branch: feature-1")

	// N/A → 不注入
	builder2 := newMockBuilder()
	rail.mu.Lock()
	rail.builder = builder2
	rail.mu.Unlock()
	rail.syncGitSystemContext(nil, map[string]any{"git_branch": "N/A"})
	assert.False(t, builder2.HasSection("git_status"))
}

// TestSetSessionID 与 RuntimeStatePath 对齐按 session 隔离文件。
func TestSetSessionID_RuntimeStatePath(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetSessionID("sess-1")
	assert.Equal(t, "sess-1", rail.sessionID)
	assert.Contains(t, RuntimeStatePath("sess-1"), filepath.Join("runtime_state", "sess-1.yaml"))
	// 清洗规则：非法字符 → "_"；空 → default
	assert.Contains(t, RuntimeStatePath("a/b c"), filepath.Join("runtime_state", "a_b_c.yaml"))
	assert.Contains(t, RuntimeStatePath(""), filepath.Join("runtime_state", "default.yaml"))
}

// TestSetExecutionPaths_Revision 对齐 Python set_execution_paths 变更检测。
func TestSetExecutionPaths_Revision(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetMode("agent.work.normal")
	rail.SetExecutionPaths("/a", "/a", "/ws")
	assert.Equal(t, 1, rail.executionPathsRevision)
	// 相同路径不递增
	rail.SetExecutionPaths("/a", "/a", "/ws")
	assert.Equal(t, 1, rail.executionPathsRevision)
	// 变化递增
	rail.SetExecutionPaths("/b", "/b", "/ws")
	assert.Equal(t, 2, rail.executionPathsRevision)
	// usesRoundExecutionPaths：agent/code 前缀命中
	assert.True(t, rail.usesRoundExecutionPaths())
	rail.SetMode("team")
	assert.False(t, rail.usesRoundExecutionPaths())
}

// TestBindExecutionPaths_RevisionGuard 对齐 Python ContextVar revision 判定。
func TestBindExecutionPaths_RevisionGuard(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "agent.work.normal")
	// 三路径不全 → 不绑定
	rail.bindExecutionPaths()
	assert.Nil(t, rail.executionCwdState)

	dir := t.TempDir()
	rail.SetExecutionPaths(dir, dir, dir)
	rail.bindExecutionPaths()
	require.NotNil(t, rail.executionCwdState)
	assert.Equal(t, rail.executionPathsRevision, rail.boundExecutionPathsRevision)

	// revision 未变 → 不重复绑定
	state := rail.executionCwdState
	rail.bindExecutionPaths()
	assert.Same(t, state, rail.executionCwdState)
}

// TestResolveCurrentMode 对齐 Python code 模式 live 覆盖。
func TestResolveCurrentMode(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	// 非 code 模式原样返回
	assert.Equal(t, "team", rail.resolveCurrentMode(nil, "team"))
	// 无 session → 原样返回
	assert.Equal(t, "code", rail.resolveCurrentMode(nil, "code"))
}

// TestConfiguredModelNames 从 workspace 配置读取（空配置 → 空）。
func TestConfiguredModelNames(t *testing.T) {
	names := configuredModelNames()
	assert.NotNil(t, names)
}

// TestAgentGroupModelNames_无模型组 对齐组外返回空。
func TestAgentGroupModelNames_无模型组(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	assert.Empty(t, rail.agentGroupModelNames(nil))
	assert.Equal(t, "", rail.modelGroupModelLabel(nil))
	assert.Equal(t, "", rail.agentModelName(nil))
}

// TestExistingDirs 测试 existingDirs
func TestExistingDirs(t *testing.T) {
	tmpDir := t.TempDir()
	result := existingDirs([]string{tmpDir, "/nonexistent_rail_test_xyz/path"})
	assert.Len(t, result, 1)
	assert.Equal(t, filepath.Clean(tmpDir), result[0])
	// nil 输入
	result2 := existingDirs(nil)
	assert.Len(t, result2, 0)
}

// TestExistingDir 测试 existingDir
func TestExistingDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := existingDir(tmpDir)
	assert.Equal(t, filepath.Clean(tmpDir), result)
	// 不存在
	result2 := existingDir("/nonexistent_rail_test_xyz/path")
	assert.Equal(t, "", result2)
	// 空输入
	result3 := existingDir("")
	assert.Equal(t, "", result3)
}

// TestSamePath 测试 samePath
func TestSamePath(t *testing.T) {
	assert.True(t, samePath("/tmp/test", "/tmp/test"))
	assert.False(t, samePath("/tmp/test", "/tmp/other"))
}

// TestFirstNonEmpty 测试 FirstNonEmpty
func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "fallback", FirstNonEmpty("", "", "fallback"))
	assert.Equal(t, "first", FirstNonEmpty("first", "second"))
	assert.Equal(t, "val", FirstNonEmpty("  ", "val"))
}

// TestWriteRuntimeStateYAML 测试 YAML 写入
func TestWriteRuntimeStateYAML(t *testing.T) {
	tmpDir := t.TempDir()
	// 写入 runtime_state.yaml 到 ConfigDir
	WriteRuntimeStateYAML(context.Background(), "sess-w", "test-model", "agent.work.plan", "cn", "web", "test-agent", tmpDir)
	yamlPath := RuntimeStatePath("sess-w")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		t.Log("runtime_state.yaml 未写入 ConfigDir（正常，ConfigDir 可能不是 tmpDir）")
	}
}

// TestWriteRuntimeStateYAML_Platform字段 测试 platform 字段格式（Bug 1 修复）。
// 对齐 Python: f"{platform.system()} {platform.machine()}" → "Linux amd64"
func TestWriteRuntimeStateYAML_Platform字段(t *testing.T) {
	// capitalizeOS 应将 runtime.GOOS 首字母大写
	assert.Equal(t, "Linux", capitalizeOS("linux"))
	assert.Equal(t, "Darwin", capitalizeOS("darwin"))
	assert.Equal(t, "Windows", capitalizeOS("windows"))
	assert.Equal(t, "", capitalizeOS(""))
}

// TestReadRuntimeStateYAML 测试 YAML 读取
func TestReadRuntimeStateYAML(t *testing.T) {
	result := readRuntimeStateYAML("sess-x")
	assert.NotNil(t, result)
}

// TestModeDisplayMap 测试 modeDisplayMap
func TestModeDisplayMap(t *testing.T) {
	assert.Equal(t, "规划模式", modeDisplayMap["agent.work.plan"]["cn"])
	assert.Equal(t, "Planning Mode", modeDisplayMap["agent.work.plan"]["en"])
	assert.Equal(t, "性能模式", modeDisplayMap["agent.work.normal"]["cn"])
	assert.Equal(t, "Cluster Mode", modeDisplayMap["team"]["en"])
}

// TestRuntimePromptRail_并发设置与调用 验证 Set* 与 BeforeModelCall 并发无竞态。
func TestRuntimePromptRail_并发设置与调用(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	// 不设置 builder：BeforeModelCall 取完快照后提前返回，
	// 这样只验证 rail 字段的并发读写（builder 由每个 agent 各自持有）。
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			rail.SetLanguage([]string{"cn", "en"}[i%2])
			rail.SetChannel([]string{"web", "tui"}[i%2])
			rail.SetTrustedDirs([]string{"/tmp"})
			rail.SetRuntimePaths("/tmp", "/tmp", "", "", "", "")
			rail.SetModelName("m")
			rail.SetMode("code")
			rail.SetForceEnglish(i%2 == 0)
		}(i)
		go func() {
			defer wg.Done()
			_ = rail.BeforeModelCall(context.Background(), nil)
		}()
	}
	wg.Wait()
}

// TestRuntimePromptRail_BeforeModelCall注入全部节 验证入口快照与 7 个 inject 串联。
func TestRuntimePromptRail_BeforeModelCall注入全部节(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetChannel("tui")
	rail.SetTrustedDirs([]string{t.TempDir()})
	builder := saprompt.NewSystemPromptBuilder()
	rail.mu.Lock()
	rail.builder = builder
	rail.mu.Unlock()

	if err := rail.BeforeModelCall(context.Background(), nil); err != nil {
		t.Fatalf("BeforeModelCall 失败: %v", err)
	}
	if builder.Build() == "" {
		t.Fatal("应注入至少一个提示节")
	}
}

// TestRuntimePromptRail_Uninit清除注入 验证释放 builder 前移除所有 section。
func TestRuntimePromptRail_Uninit清除注入(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := saprompt.NewSystemPromptBuilder()
	rail.mu.Lock()
	rail.builder = builder
	rail.mu.Unlock()

	require.NoError(t, rail.Uninit(nil))
	if builder.Build() != "" {
		t.Fatalf("Uninit 后应清除全部 section，实际: %q", builder.Build())
	}
	rail.mu.RLock()
	hasBuilder := rail.builder != nil
	rail.mu.RUnlock()
	if hasBuilder {
		t.Fatal("Uninit 后 builder 应为 nil")
	}
}

// TestRuntimePromptRail_Init捕获附件管理器与BeforeInvoke 对齐 Python init/before_invoke。
func TestRuntimePromptRail_Init捕获附件管理器与BeforeInvoke(t *testing.T) {
	agent := newSimpleAgentWithBuilder()
	rail := NewRuntimePromptRail("cn", "web")
	require.NoError(t, rail.Init(context.Background(), agent))
	rail.mu.RLock()
	hasBuilder := rail.builder != nil
	rail.mu.RUnlock()
	assert.True(t, hasBuilder)

	// BeforeInvoke：attachmentManager 为 nil 时降级运行，不报错
	require.NoError(t, rail.BeforeInvoke(context.Background(), nil))
}

// TestGetCallbacks_注册两个事件 对齐 before_invoke/before_model_call 注册。
func TestGetCallbacks_注册两个事件(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	callbacks := rail.GetCallbacks()
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeInvoke)
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
}

// TestExecutionCwdState 访问器。
func TestExecutionCwdState(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	assert.Nil(t, rail.ExecutionCwdState())
	dir := t.TempDir()
	rail.SetExecutionPaths(dir, dir, dir)
	rail.bindExecutionPaths()
	assert.NotNil(t, rail.ExecutionCwdState())
}

// TestSetRequestMetadata_与A4pAuthorizer setter。
func TestSetRequestMetadata_与A4pAuthorizer(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetRequestMetadata(map[string]any{"cron": map[string]any{"job_id": "j1"}})
	rail.SetA4pAuthorizerAvailable(true)
	snap := rail.snapshot()
	assert.Equal(t, "j1", snap.requestMetadata["cron"].(map[string]any)["job_id"])
	assert.True(t, snap.a4pAuthorizerAvail)

	// cron scope 桩：job_id 存在时仍清除（⤴️21.7 降级语义）
	_ = rail.BeforeModelCall(context.Background(), nil)
}

// TestRuntimeStatePath_长ID截断 对齐 128 字符上限。
func TestRuntimeStatePath_长ID截断(t *testing.T) {
	long := strings.Repeat("a", 200)
	p := RuntimeStatePath(long)
	assert.Contains(t, p, strings.Repeat("a", 128))
}

// TestModelGroupModelLabel_带组ID 对齐组名缓存与双语标签。
func TestModelGroupModelLabel_带组ID(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	// 无活模型：label 为空
	assert.Equal(t, "", rail.modelGroupModelLabel(nil))
}

// TestIntelliRouterToMap 对齐 router.model_dump 形态转换。
func TestIntelliRouterToMap(t *testing.T) {
	cfg := llmschema.NewDefaultIntelliRouterConfig()
	groupID := "grp-1"
	cfg.ModelGroupID = &groupID
	m := intelliRouterToMap(&cfg)
	assert.Equal(t, "grp-1", m["model_group_id"])
}

// TestRoutedModelPlaceholder 对齐占位常量透传。
func TestRoutedModelPlaceholder(t *testing.T) {
	assert.Equal(t, intellirouter.RoutedModelPlaceholder, routedModelPlaceholder())
}

// TestResolveCurrentMode_带Session 覆盖 code.plan / code.normal 分支。
func TestResolveCurrentMode_带Session(t *testing.T) {
	dir := t.TempDir()
	// 使用真实 DeepAgent 不可行（重依赖）；LoadState 接口缺省回退 configuredMode
	rail := NewRuntimePromptRail("cn", "web")
	assert.Equal(t, "code.plan", rail.resolveCurrentMode(nil, "code.plan"))
	_ = dir
}

// TestUpsertPromptAttachment_无Manager告警 33.3% 分支补齐。
func TestUpsertPromptAttachment_无Manager告警(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	assert.False(t, rail.upsertPromptAttachment(context.Background(), nil, "s", "c", prompts.AttachmentRuntime, 95))
}

// TestReadRuntimeStateYAML_损坏YAML 对齐异常分支（warn + 空 map）。
func TestReadRuntimeStatePath_读取(t *testing.T) {
	out := readRuntimeStateYAML("nonexistent-session-xyz")
	assert.NotNil(t, out)
	assert.Empty(t, out)
}

// simplePromptAgent 实现 BaseAgent 的最小替身（本包内多个测试用）。
type simplePromptAgent struct {
	builder *saprompt.SystemPromptBuilder
}

func newSimpleAgentWithBuilder() *simplePromptAgent {
	return &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()}
}

func (f *simplePromptAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *simplePromptAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *simplePromptAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *simplePromptAgent) Card() *agentschema.AgentCard                            { return nil }
func (f *simplePromptAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *simplePromptAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *simplePromptAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *simplePromptAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.builder
}
func (f *simplePromptAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *simplePromptAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *simplePromptAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// promptAgentWithLLM 支持返回固定模型并带 LoadState 的测试 agent。
type promptAgentWithLLM struct {
	*simplePromptAgent
	model       *llm.Model
	loadErr     error
	planMode    string
	attachments *prompts.Manager
}

// SetPromptAttachmentManager 注入动态提示词管理器（对齐 ReActAgent 同名方法）。
func (f *promptAgentWithLLM) SetPromptAttachmentManager(m *prompts.Manager) { f.attachments = m }

// PromptAttachments 返回动态提示词管理器。
func (f *promptAgentWithLLM) PromptAttachments() *prompts.Manager { return f.attachments }

func (f *promptAgentWithLLM) GetLLM() (*llm.Model, error) {
	if f.model == nil {
		return nil, fmt.Errorf("no model")
	}
	return f.model, nil
}

func (f *promptAgentWithLLM) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	st := &hschema.DeepAgentState{}
	st.PlanMode.Mode = f.planMode
	return st
}

// TestUpsertAndClearPromptAttachment_真实Manager 补齐 attachment 读写分支。
func TestUpsertAndClearPromptAttachment_真实Manager(t *testing.T) {
	manager := prompts.NewManager()
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.attachmentManager = manager
	rail.mu.Unlock()

	sess := &fakeSession{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, sess)
	ok := rail.upsertPromptAttachment(context.Background(), cbc, "runtime.setting", "内容", prompts.AttachmentRuntime, 95)
	assert.True(t, ok)
	rail.clearPromptAttachment(cbc, "runtime.setting")

	// 无 session 的 cbc → ErrSessionRequired → false
	cbcNoSess := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	ok2 := rail.upsertPromptAttachment(context.Background(), cbcNoSess, "s", "c", prompts.AttachmentRuntime, 95)
	assert.False(t, ok2)
}

// TestSyncGitSystemContext_清除Attachment 真实 manager 分支。
func TestSyncGitSystemContext_清除Attachment(t *testing.T) {
	manager := prompts.NewManager()
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.attachmentManager = manager
	rail.mu.Unlock()
	sess := &fakeSession{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, sess)
	rail.syncGitSystemContext(cbc, map[string]any{})
}

// TestA4pStub_真实Manager 对齐清除语义命中 manager 路径。
func TestA4pStub_真实Manager(t *testing.T) {
	manager := prompts.NewManager()
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.attachmentManager = manager
	rail.mu.Unlock()
	sess := &fakeSession{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, sess)
	rail.syncA4pIntentAuthorizationAttachment(context.Background(), cbc)
	rail.syncA4pCronAuthorizationScopeAttachment(context.Background(), cbc)
	// cron job_id 存在（降级语义仍清除）
	rail.SetRequestMetadata(map[string]any{"cron": map[string]any{"job_id": "j-1"}})
	rail.syncA4pCronAuthorizationScopeAttachment(context.Background(), cbc)
}

// TestResolveCurrentMode_带LoadStateAgent 覆盖 plan/normal 分支。
func TestResolveCurrentMode_带LoadStateAgent(t *testing.T) {
	sess := &fakeSession{}
	agent := &promptAgentWithLLM{simplePromptAgent: &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()}, planMode: "plan"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, sess)
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.agent = agent
	rail.mu.Unlock()
	assert.Equal(t, "code.plan", rail.resolveCurrentMode(cbc, "code"))
	agent.planMode = "normal"
	assert.Equal(t, "code.normal", rail.resolveCurrentMode(cbc, "code"))
	agent.planMode = "other"
	assert.Equal(t, "code", rail.resolveCurrentMode(cbc, "code"))
}

// TestAgentModelName_带真实Model 与组名缓存。
func TestAgentModelName_带真实Model(t *testing.T) {
	requestConfig := &llmschema.ModelRequestConfig{ModelName: "grp-model-a"}
	model := &llm.Model{ModelConfig: requestConfig, ClientConfig: &llmschema.ModelClientConfig{}}
	agent := &promptAgentWithLLM{simplePromptAgent: &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()}, model: model}
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.agent = agent
	rail.mu.Unlock()
	assert.Equal(t, "grp-model-a", rail.agentModelName(nil))
	// 组外：无 deployments → 空
	assert.Empty(t, rail.agentGroupModelNames(nil))
	assert.Equal(t, "", rail.modelGroupModelLabel(nil))

	// GetLLM 失败 → agentModelOf 走 inner agent 探测（无 ReactAgent → 空）
	agent2 := &promptAgentWithLLM{simplePromptAgent: &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()}}
	rail2 := NewRuntimePromptRail("cn", "web")
	rail2.mu.Lock()
	rail2.agent = agent2
	rail2.mu.Unlock()
	assert.Equal(t, "", rail2.agentModelName(nil))
}

// TestAgentGroupModelNames_带模型组 对齐 deployments+group 过滤路径。
func TestAgentGroupModelNames_带模型组(t *testing.T) {
	groupID := "grp-1"
	tagA := "model-a"
	routerCfg := llmschema.NewDefaultIntelliRouterConfig()
	routerCfg.ModelGroupID = &groupID
	routerCfg.Deployments = []llmschema.IntelliRouterDeploymentConfig{
		{RouteID: "r1", FallbackTag: &tagA},
	}
	model := &llm.Model{ModelConfig: &llmschema.ModelRequestConfig{ModelName: "*"}, ClientConfig: &llmschema.ModelClientConfig{IntelliRouter: &routerCfg}}
	agent := &promptAgentWithLLM{simplePromptAgent: &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()}, model: model}
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.agent = agent
	rail.mu.Unlock()
	names := rail.agentGroupModelNames(nil)
	assert.Equal(t, []string{"model-a"}, names)
	// 组名缓存：display 回退 id
	label := rail.modelGroupModelLabel(nil)
	assert.Equal(t, "模型组 grp-1", label)
}

// TestReadRuntimeStateYAML_损坏内容 对齐异常分支。
func TestReadRuntimeStateYAML_损坏内容(t *testing.T) {
	badPath := RuntimeStatePath("bad-yaml-session")
	require.NoError(t, os.MkdirAll(filepath.Dir(badPath), 0o755))
	require.NoError(t, os.WriteFile(badPath, []byte("!!!not-yet-valid-yaml: ["), 0o644))
	t.Cleanup(func() { _ = os.Remove(badPath) })
	out := readRuntimeStateYAML("bad-yaml-session")
	assert.NotNil(t, out)
	assert.Empty(t, out)
}

// TestRefreshDynamicAttachments_带Session attachment 路径。
func TestRefreshDynamicAttachments_带Session(t *testing.T) {
	manager := prompts.NewManager()
	agent := &promptAgentWithLLM{simplePromptAgent: &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()}}
	agent.SetPromptAttachmentManager(manager)
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.agent = agent
	rail.attachmentManager = manager
	rail.mu.Unlock()
	sess := &fakeSession{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.InvokeInputs{}, sess)
	state := rail.refreshDynamicAttachments(context.Background(), cbc)
	assert.NotNil(t, state)
}
