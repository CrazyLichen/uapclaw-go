package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// TestRailSpec_Build_未知type返回nil不报错 未知 rail 类型只告警不中断构建。
//
// 上游 deep_agent_spec.py:274-290 的明确语义：
// 「老版本持久化的 spec 引用已删除的 provider 时记 warning 并返回 None，
// 让其余 rail 仍能构建」。**不返回 error**。
func TestRailSpec_Build_未知type返回nil不报错(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	spec := hschema.RailSpec{Type: "core.removed.long.ago", Params: map[string]any{}}
	out, err := spec.Build("cn", nil, &hschema.BuildContext{Language: "cn"})

	require.NoError(t, err, "未知 type 不得返回 error（上游返回 None）")
	assert.Nil(t, out, "未知 type 返回 nil")
}

// TestRailSpec_Build_已注册provider被调用 已注册时调 provider 并透传 params。
func TestRailSpec_Build_已注册provider被调用(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	var seenParams map[string]any
	hschema.RegisterRailProvider("test.rail", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		seenParams = params
		return "rail-instance", nil
	})

	spec := hschema.RailSpec{Type: "test.rail", Params: map[string]any{"key": "value"}}
	out, err := spec.Build("cn", &workspace.Workspace{RootPath: "/ws"}, &hschema.BuildContext{Language: "cn"})

	require.NoError(t, err)
	assert.Equal(t, "rail-instance", out)
	assert.Equal(t, "value", seenParams["key"], "params 应透传")
}

// TestRailSpec_Build_provider返回err上抛 provider 失败时 error 上抛。
func TestRailSpec_Build_provider返回err上抛(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	hschema.RegisterRailProvider("fail.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return nil, assert.AnError
	})

	spec := hschema.RailSpec{Type: "fail.rail"}
	_, err := spec.Build("cn", nil, &hschema.BuildContext{})

	require.Error(t, err, "provider 自身失败必须上抛")
	assert.ErrorIs(t, err, assert.AnError)
}

// TestRailSpec_Build_nilContext自建最小上下文 ctx 为 nil 时自建含 language/workspace 的上下文。
//
// 上游 deep_agent_spec.py:291-292：
// `if context is None: context = BuildContext(language=language, workspace=workspace)`
func TestRailSpec_Build_nilContext自建最小上下文(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	var seenLang string
	var seenWS *workspace.Workspace
	hschema.RegisterRailProvider("ctx.rail", func(
		_ map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		seenLang = ctx.GetLanguage()
		seenWS = ctx.GetWorkspace()
		return nil, nil
	})

	ws := &workspace.Workspace{RootPath: "/ws"}
	spec := hschema.RailSpec{Type: "ctx.rail"}
	_, err := spec.Build("en", ws, nil)

	require.NoError(t, err)
	assert.Equal(t, "en", seenLang, "自建上下文应带上 language")
	assert.Equal(t, ws, seenWS, "自建上下文应带上 workspace")
}

// TestRailSpec_Build_不覆盖传入的ctx 已有 ctx 时原样传给 provider（不自建）。
func TestRailSpec_Build_不覆盖传入的ctx(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	var seenCtx hschema.BuildContextInterface
	hschema.RegisterRailProvider("ctx.rail", func(
		_ map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		seenCtx = ctx
		return nil, nil
	})

	orig := &hschema.BuildContext{Language: "cn", MemberName: "alice"}
	spec := hschema.RailSpec{Type: "ctx.rail"}
	_, err := spec.Build("en", nil, orig)

	require.NoError(t, err)
	assert.Same(t, orig, seenCtx, "传入的 ctx 应原样透传，不被替换")
	assert.Equal(t, "alice", seenCtx.GetMemberName(), "ctx 原有字段保留")
}

// TestRailSpec_Build_不修改spec自身 Build 不得改写 spec 的 Type/Params。
func TestRailSpec_Build_不修改spec自身(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	hschema.RegisterRailProvider("mutable.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return nil, nil
	})

	params := map[string]any{"k": "v"}
	spec := hschema.RailSpec{Type: "mutable.rail", Params: params}
	_, err := spec.Build("cn", nil, &hschema.BuildContext{})

	require.NoError(t, err)
	assert.Equal(t, "mutable.rail", spec.Type)
	assert.Equal(t, "v", spec.Params["k"])
}

// TestRailSpec_Build_nilParams安全 Params 为 nil 时 provider 收到 nil 不 panic。
func TestRailSpec_Build_nilParams安全(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	var gotNil bool
	hschema.RegisterRailProvider("nilparams.rail", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		gotNil = params == nil
		return nil, nil
	})

	spec := hschema.RailSpec{Type: "nilparams.rail"}
	_, err := spec.Build("cn", nil, &hschema.BuildContext{})

	require.NoError(t, err)
	assert.True(t, gotNil, "nil Params 原样传 nil（对齐上游 dict(self.params)）")
}
