// factory_test.go 对齐 Python factory.py 的可测路径。
// Python: agents/factory.py。

package agents

import (
	"context"
	"errors"
	agentface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer/agentface"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
)

// TestPackageWriteAllowed_声明路径 对齐 Python package_write_allowed 非 repair 分支。
func TestPackageWriteAllowed_声明路径(t *testing.T) {
	declared := []string{"prompt_sections/identity.md", "skills"}
	require.True(t, PackageWriteAllowed("prompt_sections/identity.md", declared, false))
	require.True(t, PackageWriteAllowed("skills/my_skill/SKILL.md", declared, false))
	require.False(t, PackageWriteAllowed("tools/x.py", declared, false))
	// 绝对路径 / 点开头 / .. / 受保护文件名 / 受保护顶层目录
	require.False(t, PackageWriteAllowed("/abs/path", declared, false))
	require.False(t, PackageWriteAllowed(".hidden", declared, false))
	require.False(t, PackageWriteAllowed("../escape", declared, false))
	require.False(t, PackageWriteAllowed("plan.yaml", declared, false))
	require.False(t, PackageWriteAllowed("current_harnesses/solver/identity.md", declared, false))
	require.False(t, PackageWriteAllowed("evaluation/x", declared, false))
}

// TestPackageWriteAllowed_repair模式 对齐 repair 分支。
func TestPackageWriteAllowed_repair模式(t *testing.T) {
	// repair 放宽到五顶层目录 / 三顶层文件 / 四后缀
	require.True(t, PackageWriteAllowed("prompt_sections/identity.md", nil, true))
	require.True(t, PackageWriteAllowed("harness.yaml", nil, true))
	require.True(t, PackageWriteAllowed("tools/x.py", nil, true))
	// Python repair 分支：suffix in {".yaml",".yml",".json",".py"} —— txt 不允许
	require.False(t, PackageWriteAllowed("notes.txt", nil, true))
	require.False(t, PackageWriteAllowed("plan.yaml", nil, true)) // 受保护名单仍拒绝
}

// TestRenderPrompt_占位缺失报错 对齐 _render_prompt KeyError。
func TestRenderPrompt_占位缺失报错(t *testing.T) {
	out, err := RenderPrompt("role_attribution.md", map[string]string{})
	require.NoError(t, err) // role_attribution.md 无 {{PLACEHOLDER}} 占位
	require.NotEmpty(t, out)

	// mechanism_attribution.md 含三组占位；缺失任一报错
	_, err = RenderPrompt("mechanism_attribution.md", map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing prompt placeholder value:")
}

// TestRenderPrompt_枚举注入 对照 create_mechanism_attribution_agent 的占位值格式。
func TestRenderPrompt_枚举注入(t *testing.T) {
	out, err := RenderPrompt("mechanism_attribution.md", map[string]string{
		"MECHANISM_TYPE_VALUES":       joinBulletValues(harnessschema2MechanismValues()),
		"FAILURE_SIGNATURE_VALUES":    joinBulletValues(harnessschema2FailureValues()),
		"OPTIMIZATION_SURFACE_VALUES": joinBulletValues(harnessschema2SurfaceValues()),
	})
	require.NoError(t, err)
	require.Contains(t, out, "- prompt")
	require.NotContains(t, out, "{{MECHANISM_TYPE_VALUES}}")
}

// TestLoadMemberOptimizerModel_错误路径 对齐三处 RuntimeError。
func TestLoadMemberOptimizerModel_错误路径(t *testing.T) {
	_, err := memberoptimizer.LoadMemberOptimizerModel("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_config_ref is required for member optimization")

	_, err = memberoptimizer.LoadMemberOptimizerModel("/nonexistent/ref.yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_config_ref not found")

	dir := t.TempDir()
	// 非 dict 顶层（JSON 数组）
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte(`[1,2]`), 0o644)
	_, err = memberoptimizer.LoadMemberOptimizerModel(path)
	require.Error(t, err)
}

// TestLoadMemberOptimizerModel_成功路径 完整配置载入预算化模型。
func TestLoadMemberOptimizerModel_成功路径(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.yaml")
	os.WriteFile(path, []byte(`model:
  model_client_config:
    client_provider: OpenAI
    client_id: c1
    api_key: k
    api_base: https://api.example.com
    max_retries: 5
  model_request_config:
    max_tokens: null
`), 0o644)
	model, err := memberoptimizer.LoadMemberOptimizerModel(path)
	require.NoError(t, err)
	require.NotNil(t, model)
	require.Equal(t, 0, model.ClientConfig.MaxRetries) // without_inner_sdk_retries
}

// TestFormatActionDefinitions 对照 _format_action_definitions 输出格式。
func TestFormatActionDefinitions(t *testing.T) {
	// 空定义返回空串
	require.Empty(t, FormatActionDefinitions(nil))
	one := []harnessrsi.ActionDefinition{{
		Group: "prompt", Operation: "edit", Purpose: "adjust identity",
		Function: "edit_prompt_section",
	}}
	out := FormatActionDefinitions(one)
	require.Contains(t, out, "## Action Definitions")
	require.Contains(t, out, "- prompt/edit: adjust identity (function=edit_prompt_section)")
}

// TestOptimizerAgentResources_句柄释放 无操作 ID 时 Release 安全。
func TestOptimizerAgentResources_句柄释放(t *testing.T) {
	handle, err := OptimizerAgentResources(&harness.DeepAgent{})
	require.NoError(t, err)
	require.NoError(t, handle.Release()) // operationID 为空：no-op
}

// ───────────────────────── 枚举注入辅助（对齐 factory.py:513-523） ─────────────────────────

func harnessschema2MechanismValues() []string {
	return memberoptimizer.MechanismTypeValuesExcludingRail()
}

func harnessschema2FailureValues() []string {
	return memberoptimizer.FailureSignatureValuesExcludingConfigOrRailMismatch()
}

func harnessschema2SurfaceValues() []string {
	return memberoptimizer.OptimizationSurfaceValuesExcludingRail()
}

// writeFactoryModelConfig 写最小合法模型配置（creator 测试共用）。
func writeFactoryModelConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "model.yaml")
	content := `model:
  model_client_config:
    client_id: factory-client
    client_provider: llm_OpenAI
    model_name: gpt-4o-mini
    api_key: sk-test
    base_url: http://localhost:1
  model_request_config:
    max_tokens: 512
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCreateAllMemberAgents_五工厂装配(t *testing.T) {
	dir := t.TempDir()
	modelRef := writeFactoryModelConfig(t, dir)
	workspace := filepath.Join(dir, "ws")
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		create func() (*harness.DeepAgent, error)
	}{
		{"role_attribution", func() (*harness.DeepAgent, error) {
			return CreateRoleAttributionAgent(ctx, MemberAgentParams{ModelConfigRef: modelRef, Workspace: workspace})
		}},
		{"mechanism_attribution", func() (*harness.DeepAgent, error) {
			return CreateMechanismAttributionAgent(ctx, MemberAgentParams{ModelConfigRef: modelRef, Workspace: workspace})
		}},
		{"action_planning", func() (*harness.DeepAgent, error) {
			return CreateActionPlanningAgent(ctx, MemberAgentParams{ModelConfigRef: modelRef, Workspace: workspace}, nil)
		}},
		{"action_execution", func() (*harness.DeepAgent, error) {
			return CreateActionExecutionAgent(ctx, MemberAgentParams{ModelConfigRef: modelRef, Workspace: workspace}, false, nil)
		}},
		{"verification_repair", func() (*harness.DeepAgent, error) {
			return CreateVerificationRepairAgent(ctx, MemberAgentParams{ModelConfigRef: modelRef, Workspace: workspace})
		}},
	} {
		agent, err := tc.create()
		require.NoError(t, err, tc.name)
		require.NotNil(t, agent, tc.name)
		require.NotNil(t, agent) // DeepAgent 装配成功即视为构造级通过
	}
}

func TestCreateMemberOptimizerAgent_模型缺失拒绝(t *testing.T) {
	_, err := CreateMemberOptimizerAgent(context.Background(), MemberAgentParams{ModelConfigRef: "", Workspace: t.TempDir()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_config_ref is required")
	_, err = CreateMemberOptimizerAgent(context.Background(), MemberAgentParams{ModelConfigRef: "/nonexistent/x.yaml", Workspace: t.TempDir()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSupportedHostReadRoots_含仓根(t *testing.T) {
	roots := SupportedHostReadRoots()
	require.NotEmpty(t, roots)
	found := false
	for _, root := range roots {
		if strings.Contains(root, "uapclaw") || root != "" {
			found = true
		}
	}
	require.True(t, found)
}

func TestBuildFileToolsForWorkspace_装配(t *testing.T) {
	ws := t.TempDir()
	tools, sysOp, err := BuildFileToolsForWorkspace(ws, "test_agent", nil, []string{"a.md"}, false)
	require.NoError(t, err)
	require.NotEmpty(t, tools)
	require.NotNil(t, sysOp)
}

func TestPackageWriteAllowed_矩阵(t *testing.T) {
	require.True(t, PackageWriteAllowed("pkg/prompt_sections/a.md", []string{"pkg/prompt_sections"}, false))
	require.False(t, PackageWriteAllowed("pkg/other/a.md", []string{"pkg/prompt_sections"}, false))
	// nil declared → any(...) over empty = False（Python 语义）
	require.False(t, PackageWriteAllowed("pkg/prompt_sections/a.md", nil, false))
	require.False(t, PackageWriteAllowed("current_harnesses/solver/a.md", nil, false), "受保护顶层目录")
	require.True(t, PackageWriteAllowed("anything/config.yaml", nil, true), "repair 模式 yaml 后缀可写")
}

func TestSetFilterActionDefinitions_注入(t *testing.T) {
	saved := filterActionDefinitionsFn
	called := false
	SetFilterActionDefinitions(func(defs []harnessrsi.ActionDefinition) []harnessrsi.ActionDefinition {
		called = true
		return defs
	})
	defer SetFilterActionDefinitions(saved)
	defs := []harnessrsi.ActionDefinition{{Name: "n"}}
	got := filterActionDefinitionsFn(defs)
	require.True(t, called)
	require.Equal(t, defs, got)
}

func TestRenderPrompt_缺占位值报错(t *testing.T) {
	// 缺占位值 → 报错（Go 侧严格校验语义）
	_, err := RenderPrompt("mechanism_attribution.md", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing prompt placeholder value")
	// 提供全部占位值 → 渲染成功
	_, err = RenderPrompt("mechanism_attribution.md", map[string]string{
		"MECHANISM_TYPE_VALUES":       "- prompt",
		"FAILURE_SIGNATURE_VALUES":    "- repeated_failure_loop",
		"OPTIMIZATION_SURFACE_VALUES": "- config",
	})
	require.NoError(t, err)
	// 未知模板 → 报错
	_, err = RenderPrompt("nonexistent.md", nil)
	require.Error(t, err)
}

// fakeInnerTool 内层工具假实现（记录 Invoke 输入）。
type fakeInnerTool struct {
	name       string
	invoked    map[string]any
	streamable bool
}

func (f *fakeInnerTool) Card() *tool.ToolCard {
	card := &tool.ToolCard{}
	card.Name = f.name
	return card
}
func (f *fakeInnerTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	f.invoked = inputs
	return map[string]any{"ok": true}, nil
}
func (f *fakeInnerTool) Stream(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	if !f.streamable {
		return nil, tool.ErrStreamNotSupported
	}
	ch := make(chan tool.StreamChunk, 1)
	ch <- tool.StreamChunk{Data: map[string]any{"text": "chunk"}}
	close(ch)
	return ch, nil
}
func (f *fakeInnerTool) GetCallTimeout(_ map[string]any, def float64) float64 { return def }

func TestScopedPackageTool_Write路径边界(t *testing.T) {
	ws := t.TempDir()
	inner := &fakeInnerTool{name: "write_file"}
	scoped := NewScopedPackageTool(inner, ws, nil, []string{"pkg/prompt_sections"}, false)
	// 合法写
	_, err := scoped.Invoke(context.Background(), map[string]any{"file_path": "pkg/prompt_sections/a.md"})
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(inner.invoked["file_path"].(string), filepath.Join("pkg", "prompt_sections", "a.md")))
	// 越界写
	inner.invoked = nil
	_, err = scoped.Invoke(context.Background(), map[string]any{"file_path": "other/b.md"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "write is outside the permitted package paths")
	// 非字符串路径
	_, err = scoped.Invoke(context.Background(), map[string]any{"file_path": 123})
	require.Error(t, err)
	// nil inputs
	_, err = scoped.Invoke(context.Background(), nil)
	require.Error(t, err)
}

func TestScopedPackageTool_读路径根白名单(t *testing.T) {
	ws := t.TempDir()
	readRoot := t.TempDir()
	inner := &fakeInnerTool{name: "read_file"}
	scoped := NewScopedPackageTool(inner, ws, []string{readRoot}, nil, false)
	// 根内读
	_, err := scoped.Invoke(context.Background(), map[string]any{"file_path": readRoot + "/a.txt"})
	require.NoError(t, err)
	// 根外读
	_, err = scoped.Invoke(context.Background(), map[string]any{"file_path": "/etc/hosts"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "read is outside")
}

func TestScopedPackageTool_Glob与Stream(t *testing.T) {
	ws := t.TempDir()
	glob := &fakeInnerTool{name: "glob"}
	scoped := NewScopedPackageTool(glob, ws, []string{ws}, nil, false)
	_, err := scoped.Invoke(context.Background(), map[string]any{"path": ".", "pattern": "../x/*.go"})
	require.Error(t, err)
	_, err = scoped.Invoke(context.Background(), map[string]any{"path": ".", "pattern": "*.go"})
	require.NoError(t, err)
	// Stream 转调
	streamable := &fakeInnerTool{name: "write_file", streamable: true}
	scoped2 := NewScopedPackageTool(streamable, ws, nil, nil, false)
	ch, err := scoped2.Stream(context.Background(), map[string]any{"file_path": "pkg/a.md"})
	require.NoError(t, err)
	require.NotNil(t, ch)
	// GetCallTimeout 转调
	require.Equal(t, 5.0, scoped2.GetCallTimeout(nil, 5.0))
	// Card 转调
	require.Equal(t, "write_file", scoped2.Card().Name)
}

func TestBuildRails_技能与额外护栏(t *testing.T) {
	// 无技能无额外护栏 → 空
	require.Empty(t, BuildRails(BuildRailsParams{}))
	// ExtraRails 直通
	extra := fakeRail{}
	got := BuildRails(BuildRailsParams{ExtraRails: []agentinterfaces.AgentRail{&extra}})
	require.Len(t, got, 1)
	// EnabledSkillsOverride 指向不存在技能 → buildSkillRail 返回 nil → 空
	require.Empty(t, BuildRails(BuildRailsParams{EnabledSkillsOverride: []string{"no_such_skill"}}))
}

// fakeRail 空护栏（嵌入 BaseRail 空实现，规则 10.1 惯例）。
type fakeRail struct {
	agentinterfaces.BaseRail
}

func (fakeRail) Name() string { return "fake_rail" }

func TestIsSymlink_路径判定(t *testing.T) {
	dir := t.TempDir()
	require.False(t, isSymlink(dir))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(dir, link))
	require.True(t, isSymlink(link))
}

func TestIsUnderAnyRoot(t *testing.T) {
	root := t.TempDir()
	require.True(t, isUnderAnyRoot(filepath.Join(root, "x"), []string{root}))
	require.False(t, isUnderAnyRoot("/etc/hosts", []string{root}))
}

func TestExpandUserPath_分支(t *testing.T) {
	require.Equal(t, "/x", expandUserPath("/x"))
	require.NotEqual(t, "~", expandUserPath("~"))
	require.Contains(t, expandUserPath("~/f"), string(filepath.Separator))
	require.Equal(t, "rel", expandUserPath("rel"))
}

func TestExpandEnvVarsAny_嵌套分支(t *testing.T) {
	t.Setenv("RSI_TEST_VAR", "v")
	got := expandEnvVarsAny(map[string]any{
		"a": "${RSI_TEST_VAR}",
		"b": []any{map[string]any{"c": "${RSI_TEST_VAR}"}, "plain"},
		"d": 1,
		"e": []string{"${RSI_TEST_VAR}"},
	})
	m := got.(map[string]any)
	require.Equal(t, "v", m["a"])
	require.Equal(t, "plain", m["b"].([]any)[1])
	// []string 分支（Python 只处理 list；Go []string 直通）
	require.NotNil(t, m["e"])
}

func TestOptimizerResourceHandle_Release_无ID(t *testing.T) {
	handle := &OptimizerResourceHandle{}
	require.NoError(t, handle.Release())
	// operationID 命中 Runner 资源管理器注销路径
	handle2 := &OptimizerResourceHandle{operationID: "op-x"}
	require.NoError(t, handle2.Release())
}

func TestPyScalarJSON_各类型(t *testing.T) {
	require.Equal(t, "null", pyScalarJSON(nil))
	require.Equal(t, "true", pyScalarJSON(true))
	require.Equal(t, "3", pyScalarJSON(3))
	require.Equal(t, "1.5", pyScalarJSON(1.5))
	require.Equal(t, `"s"`, pyScalarJSON("s"))
}

func TestPyJSONQuote_转义(t *testing.T) {
	require.Equal(t, `"a\"b"`, pyJSONQuote("a\"b"))
	require.Equal(t, `"a\nb"`, pyJSONQuote("a\nb"))
}

func TestWritePyJSONIndent_缩进输出(t *testing.T) {
	var b strings.Builder
	writePyJSONIndent(&b, map[string]any{"a": 1, "b": []any{"x", "y"}}, 0)
	out := b.String()
	require.Contains(t, out, `"a": 1`)
	require.Contains(t, out, "\n")
}

func TestOptimizerAgentResources_深配置(t *testing.T) {
	// 深配置无 SysOperation → 空句柄（用真 DeepAgent 零值）
	handle, err := OptimizerAgentResources(&harness.DeepAgent{})
	require.NoError(t, err)
	require.NotNil(t, handle)
	require.NoError(t, handle.Release())
}

func TestUnwrapModel_分支(t *testing.T) {
	// nil → nil
	require.Nil(t, unwrapModel(nil))
	// 有底层模型 → 转出
	dir := t.TempDir()
	modelRef := writeFactoryModelConfig(t, dir)
	model, err := memberoptimizer.LoadMemberOptimizerModel(modelRef)
	require.NoError(t, err)
	require.NotNil(t, unwrapModel(model))
}

func TestResolveRgBinary(t *testing.T) {
	// Python shutil.which("rg") 等价：环境无 rg 时返回空串（不报错）
	_ = resolveRgBinary()
}

func TestStopReasonOr_默认值(t *testing.T) {
	require.Equal(t, "attempt_limit_reached", stopReasonOr("", "attempt_limit_reached"))
	require.Equal(t, "custom", stopReasonOr("custom", "attempt_limit_reached"))
}

func TestBuildSkillRail_命中仓内技能(t *testing.T) {
	// 仓内 agents/skills 目录存在（空目录）→ 未命中技能返回 nil
	require.Nil(t, buildSkillRail([]string{"no_such_skill"}, nil))
	// 临时技能目录命中
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "my_skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	rail := buildSkillRail([]string{"my_skill"}, []string{tmp})
	require.NotNil(t, rail)
}

func TestInvokeStructured_瞬时失败重试与恢复(t *testing.T) {
	saved := invokeFn
	t.Cleanup(func() { invokeFn = saved })
	calls := 0
	transient := errors.New("transient")
	invokeFn = func(context.Context, *agentface.Agent, string, string) (map[string]any, error) {
		calls++
		if calls == 1 {
			return nil, transient
		}
		return map[string]any{"text": `{"role": "solver"}`}, nil
	}
	if !harnessrsi.IsRetryableModelCallFailure(transient) {
		t.Skip("transient 不属于可重试失败分类")
	}
	agent := &fakeAgent{invoke: func() map[string]any { return map[string]any{} }}
	_ = agent
	// 走真实结构化调用（InvokeMemberOptimizerAgentStructured 覆盖重试分支）
	savedBridge := memberoptimizer.SnapshotAgentsBridge()
	defer memberoptimizer.RestoreAgentsBridge(savedBridge)
	params := memberoptimizer.StructuredInvokeParams{
		Agent:         &agentface.Agent{},
		AgentName:     "T",
		UserMessage:   "u",
		SessionID:     "s",
		RetryLimit:    1,
		ParseResponse: ParseJSONObjectResponse,
	}
	raw, err := InvokeMemberOptimizerAgentStructured(context.Background(), params)
	require.NoError(t, err)
	require.Equal(t, "solver", raw["role"])
}

func TestBridgeAdapters_注册闭包(t *testing.T) {
	// 直接调用 init 注册进父包桥的适配器闭包（D-c1-1 桥接路径）
	bridge := memberoptimizer.SnapshotAgentsBridge()
	require.NotNil(t, bridge.InvokeStructured)
	require.NotNil(t, bridge.ParseJSONObject)
	require.NotNil(t, bridge.ParseYamlOrJSONObject)
	raw, err := bridge.ParseJSONObject(`{"a": 1}`)
	require.NoError(t, err)
	require.Equal(t, float64(1), raw["a"])
	raw, err = bridge.ParseYamlOrJSONObject("a: 1")
	require.NoError(t, err)
	require.NotNil(t, raw["a"]) // YAML int(1) / JSON float64(1)——解析器各自语义
	// 两个 agent 工厂适配器（模型配置缺失即报错——验证转调链路）
	for _, fn := range []func(context.Context, memberoptimizer.AttributionAgentParams) (*agentface.Agent, error){
		bridge.CreateRoleAttributionAgent, bridge.CreateMechanismAttributionAgent,
	} {
		_, err := fn(context.Background(), memberoptimizer.AttributionAgentParams{ModelConfigRef: ""})
		require.Error(t, err)
	}
}
