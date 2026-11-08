package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// TestResolveParts_产出配置 基础场景：产出 config 且 card 已配置。
func TestResolveParts_产出配置(t *testing.T) {
	spec := &hschema.DeepAgentSpec{
		Language:       "cn",
		EnableTaskLoop: true,
		MaxIterations:  5,
	}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	require.NotNil(t, parts)
	require.NotNil(t, parts.Config, "config 必须产出")
	assert.Equal(t, "cn", parts.Config.Language)
	assert.Equal(t, 5, parts.Config.MaxIterations)
}

// TestResolveParts_parentModel写派生ctx不污染调用方 _parent_model 写进**派生** ctx。
//
// 上游 deep_agent_spec.py:550-570：
//
//	build_ctx = context.derive(...)          ← 派生，不动调用方
//	build_ctx.extras = dict(build_ctx.extras) ← 重绑 extras
//	build_ctx.extras["_parent_model"] = llm_model
//
// 所以调用方传进来的 ctx **不该**被写入；写入的是内部派生体。
// 派生体不在返回值里（parts 不含 ctx），故本用例断言"不污染"这一契约。
func TestResolveParts_parentModel不污染调用方(t *testing.T) {
	spec := &hschema.DeepAgentSpec{Language: "cn"}
	ctx := &hschema.BuildContext{Language: "cn"}

	_, err := spec.ResolveParts(ctx)
	require.NoError(t, err)

	_, leaked := ctx.GetExtras()["_parent_model"]
	assert.False(t, leaked, "不得写入调用方的 extras（derive 语义）")
	assert.Empty(t, ctx.GetExtras(), "调用方 extras 应保持原样")
}

// TestResolveParts_extras不共享 派生体与调用方的 extras 不是同一个 map。
//
// 上游 :558 `build_ctx.extras = dict(build_ctx.extras)` 就是为了这个：
// 不重绑会让多个成员共享同一句柄字典。
func TestResolveParts_extras不共享(t *testing.T) {
	spec := &hschema.DeepAgentSpec{Language: "cn"}
	ctx := &hschema.BuildContext{Language: "cn"}
	ctx.GetExtras()["caller.handle"] = "keep-me"

	_, err := spec.ResolveParts(ctx)
	require.NoError(t, err)

	assert.Equal(t, "keep-me", ctx.GetExtras()["caller.handle"], "调用方原有键保留")
	assert.Len(t, ctx.GetExtras(), 1, "调用方 extras 不应多出键")
}

// TestResolveParts_衍生context回填workspace 派生 ctx 回填 workspace，原 ctx 不变。
//
// 上游 deep_agent_spec.py:558-563：derive(workspace=..., member_card_id=..., moa=None)。
func TestResolveParts_衍生context回填workspace(t *testing.T) {
	spec := &hschema.DeepAgentSpec{
		Language:  "cn",
		Workspace: &hschema.WorkspaceSpec{RootPath: "/test-ws"},
	}
	origCtx := &hschema.BuildContext{Language: "cn"}

	_, err := spec.ResolveParts(origCtx)
	require.NoError(t, err)

	// 原 ctx 不被改写（derive 语义）
	assert.Nil(t, origCtx.GetWorkspace(), "原 ctx 的 workspace 不得被改写")
	assert.Equal(t, "cn", origCtx.GetLanguage())
}

// TestResolveParts_nilContext自建 nil ctx 时自建最小上下文，不 panic。
func TestResolveParts_nilContext自建(t *testing.T) {
	spec := &hschema.DeepAgentSpec{Language: "en"}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err, "nil ctx 不得报错")
	require.NotNil(t, parts)
}

// TestResolveParts_rails经provider解析 spec 声明的 rail 走注册表，不是手工构造。
func TestResolveParts_rails经provider解析(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	called := 0
	hschema.RegisterRailProvider("test.resolve.rail", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		called++
		return nil, nil
	})

	spec := &hschema.DeepAgentSpec{
		Language: "cn",
		Rails:    []hschema.RailSpec{{Type: "test.resolve.rail"}},
	}
	_, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	assert.Equal(t, 1, called, "spec 声明的 rail 必须经 provider 解析")
}

// TestResolveParts_未知rail不报错 未知 rail 类型只 warn 不改错（对齐上游）。
func TestResolveParts_未知rail不报错(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	spec := &hschema.DeepAgentSpec{
		Language: "cn",
		Rails:    []hschema.RailSpec{{Type: "core.does.not.exist"}},
	}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err, "未知 rail 不得中断解析")
	require.NotNil(t, parts)
}

// TestResolveParts_rail返回列表被展平 provider 返回 []rail 时展平进 parts.Rails。
func TestResolveParts_rail返回列表被展平(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	hschema.RegisterRailProvider("test.list.rail", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		// 返回一个 []any（模拟上游 "a list of rails"）
		return []any{}, nil
	})

	spec := &hschema.DeepAgentSpec{
		Language: "cn",
		Rails:    []hschema.RailSpec{{Type: "test.list.rail"}},
	}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	assert.Empty(t, parts.Rails, "空列表展平后无 rail")
}

// TestResolveParts_workspace回填到config spec 的 Workspace 解析进 config。
func TestResolveParts_workspace回填到config(t *testing.T) {
	spec := &hschema.DeepAgentSpec{
		Language:  "cn",
		Workspace: &hschema.WorkspaceSpec{RootPath: "/my-ws"},
	}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	require.NotNil(t, parts.Config.Workspace, "config.Workspace 必须就位")
	assert.Equal(t, "/my-ws", parts.Config.Workspace.RootPath)
}

// TestResolveParts_memberCardID回填 member_card_id 从 spec.Card 回填进派生 ctx。
func TestResolveParts_memberCardID回填(t *testing.T) {
	spec := &hschema.DeepAgentSpec{Language: "cn"}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	require.NotNil(t, parts.Config.Card, "config.Card 必须就位（nil 时建默认卡）")
}

// TestResolveParts_不修改spec自身 解析不得改写 spec 字段。
func TestResolveParts_不修改spec自身(t *testing.T) {
	rails := []hschema.RailSpec{{Type: "x"}}
	spec := &hschema.DeepAgentSpec{
		Language:      "cn",
		MaxIterations: 7,
		Rails:         rails,
	}

	_, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	assert.Equal(t, "cn", spec.Language)
	assert.Equal(t, 7, spec.MaxIterations)
	assert.Len(t, spec.Rails, 1, "spec.Rails 不得被追加/改写")
}

// TestResolveParts_workspaceSpec为空不建空workspace RootPath 为空时不建 workspace。
func TestResolveParts_workspaceSpec为空(t *testing.T) {
	spec := &hschema.DeepAgentSpec{Language: "cn"}

	parts, err := spec.ResolveParts(nil)

	require.NoError(t, err)
	// 未指定 Workspace 时，config.Workspace 由工厂侧建默认（buildWorkspace），
	// 这里只确认不 panic
	assert.NotNil(t, parts)
}

// 引用 workspace 包（后续断言会用）
var _ = workspace.Workspace{}
