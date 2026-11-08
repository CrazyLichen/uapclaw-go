package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// codeLikeContext 模拟上游 CodeBuildContext：子类多一个 Adapter 字段。
//
// 上游 jiuwenswarm/agents/harness/code/spec.py:63 的
// `class CodeBuildContext(BuildContext)` 就是这种形态，它靠 copy.copy
// 在 derive 后保持子类；Go 无 copy.copy，改由子类自己覆写 Derive。
type codeLikeContext struct {
	schema.BuildContext
	// Adapter 平台句柄（子类专有）
	Adapter string
}

// Derive 覆写以保留子类具体类型。
func (c *codeLikeContext) Derive(o schema.BuildContextOverrides) schema.BuildContextInterface {
	base := c.BuildContext.Derive(o).(*schema.BuildContext)
	return &codeLikeContext{BuildContext: *base, Adapter: c.Adapter}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestBuildContext_Derive_不污染原对象 验证 derive 后原 context 各字段不变。
//
// 对齐上游 BuildContext.derive 用 copy.copy 的语义
// （openjiuwen/harness/schema/build_context.py:85-95）：
// 「原 context 保持不被修改，所以一个每团队基础上下文可以扇出给多个成员」。
func TestBuildContext_Derive_不污染原对象(t *testing.T) {
	orig := &schema.BuildContext{
		Language:   "cn",
		MemberName: "alice",
		Role:       "teammate",
	}

	derived := orig.Derive(schema.BuildContextOverrides{
		MemberName: schema.StringPtr("bob"),
	})

	require.NotNil(t, derived)
	assert.Equal(t, "bob", derived.GetMemberName(), "派生体应拿到覆盖值")
	assert.Equal(t, "alice", orig.MemberName, "原对象不得被改写")
	assert.Equal(t, "cn", orig.Language, "未覆盖字段在原对象里保持原值")
	assert.Equal(t, "cn", derived.GetLanguage(), "未覆盖字段应继承")
}

// TestBuildContext_Derive_只浅拷贝Extras 验证 Derive 不深拷贝 Extras。
//
// 对齐上游结构：copy.copy 是浅拷贝，extras 的 dict(...) 复制由调用方
// 自己做（agent_configurator.py:867 的
// `member_build_context.extras = dict(member_build_context.extras)`）。
// Go 侧同理：Derive 只浅拷贝，调用方要隔离就自己复制。
func TestBuildContext_Derive_只浅拷贝Extras(t *testing.T) {
	origExtras := map[string]any{"team.messager": "shared"}
	orig := &schema.BuildContext{Extras: origExtras}

	derived := orig.Derive(schema.BuildContextOverrides{})

	// 浅拷贝：派生体的 Extras 与原体是同一个 map（对齐 copy.copy）
	derived.GetExtras()["team.team_backend"] = "leaked"
	_, exists := orig.Extras["team.team_backend"]
	assert.True(t, exists, "Derive 是浅拷贝，写派生体会影响原体（与上游一致）")

	// 调用方自行复制后即隔离（模拟 agent_configurator.py:867 的做法）
	orig2 := &schema.BuildContext{Extras: map[string]any{"k": "v"}}
	derived2 := orig2.Derive(schema.BuildContextOverrides{}).(*schema.BuildContext)
	derived2.Extras = copyExtras(derived2.Extras)
	derived2.Extras["injected"] = "x"
	_, leaked := orig2.Extras["injected"]
	assert.False(t, leaked, "调用方复制后应完全隔离")
}

// copyExtras 复刻上游 agent_configurator.py:867 的 dict(...) 一层复制。
func copyExtras(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// TestBuildContext_Derive_Overrides部分生效 验证只覆盖传入字段。
func TestBuildContext_Derive_Overrides部分生效(t *testing.T) {
	orig := &schema.BuildContext{
		Language:       "cn",
		MemberName:     "alice",
		Role:           "teammate",
		MemberCardID:   "card-1",
		ProjectDir:     "/orig",
		TeamOutputsDir: "/out",
	}

	derived := orig.Derive(schema.BuildContextOverrides{
		Role:       schema.StringPtr("leader"),
		ProjectDir: schema.StringPtr("/new"),
	})

	assert.Equal(t, "leader", derived.GetRole())
	assert.Equal(t, "/new", derived.GetProjectDir())
	assert.Equal(t, "alice", derived.GetMemberName(), "未指定字段继承")
	assert.Equal(t, "card-1", derived.GetMemberCardID(), "未指定字段继承")
	assert.Equal(t, "/out", derived.GetTeamOutputsDir(), "未指定字段继承")
}

// TestBuildContext_Derive_Workspace覆盖 验证 Workspace 用 HasWorkspace 位区分"未指定"与"置空"。
func TestBuildContext_Derive_Workspace覆盖(t *testing.T) {
	orig := &schema.BuildContext{Workspace: &workspace.Workspace{RootPath: "/a"}}

	// 未指定 HasWorkspace：继承原值
	inherited := orig.Derive(schema.BuildContextOverrides{})
	require.NotNil(t, inherited.GetWorkspace())
	assert.Equal(t, "/a", inherited.GetWorkspace().RootPath)

	// 显式 HasWorkspace 且 Workspace 为 nil：清空
	cleared := orig.Derive(schema.BuildContextOverrides{HasWorkspace: true, Workspace: nil})
	assert.Nil(t, cleared.GetWorkspace(), "显式清空应生效")
}

// TestBuildContext_子类Derive保类型 验证子类实现 Derive 后仍返回子类类型（C4 核心契约）。
func TestBuildContext_子类Derive保类型(t *testing.T) {
	sub := &codeLikeContext{
		BuildContext: schema.BuildContext{Language: "cn"},
		Adapter:      "code-adapter",
	}

	derived := sub.Derive(schema.BuildContextOverrides{MemberName: schema.StringPtr("bob")})

	casted, ok := derived.(*codeLikeContext)
	require.True(t, ok, "Derive 必须保留子类具体类型")
	assert.Equal(t, "code-adapter", casted.Adapter, "子类专有字段不丢")
	assert.Equal(t, "bob", casted.GetMemberName(), "基类字段按 overrides 生效")
}

// TestResolveMemberWorkDir_基类恒nil 验证基类无平台约定。
//
// 对齐上游 build_context.py:97-115：「基类 BuildContext 没有平台工作目录约定，
// 只有平台子类才会分配」。
func TestResolveMemberWorkDir_基类恒nil(t *testing.T) {
	ctx := &schema.BuildContext{}
	assert.Nil(t, ctx.ResolveMemberWorkDir())

	// 即使 team_outputs_dir 有值，基类仍返回 nil（上游同语义）
	ctxWithOutputs := &schema.BuildContext{TeamOutputsDir: "/out"}
	assert.Nil(t, ctxWithOutputs.ResolveMemberWorkDir())
}

// TestBuildContextFromSeed_无工厂返回nil 验证无工厂时静默返回 nil。
//
// 对齐上游 build_context.py:146-160：
// `if _BUILD_CONTEXT_FACTORY is None or not seed: return None`
// —— 静默回落，不抛异常，让调用方走旧路径。
func TestBuildContextFromSeed_无工厂返回nil(t *testing.T) {
	schema.ResetBuildContextFactoryForTest()

	ctx, err := schema.BuildContextFromSeed(map[string]any{"language": "cn"})

	assert.NoError(t, err, "无工厂不是错误（上游静默返回 None）")
	assert.Nil(t, ctx, "无工厂时返回 nil")
}

// TestBuildContextFromSeed_空seed返回nil 验证空 seed 静默返回 nil。
func TestBuildContextFromSeed_空seed返回nil(t *testing.T) {
	schema.RegisterBuildContextFactory(func(seed map[string]any) (schema.BuildContextInterface, error) {
		return &schema.BuildContext{Language: "en"}, nil
	})
	t.Cleanup(schema.ResetBuildContextFactoryForTest)

	for _, seed := range []map[string]any{nil, {}} {
		ctx, err := schema.BuildContextFromSeed(seed)

		assert.NoError(t, err, "空 seed 不是错误")
		assert.Nil(t, ctx, "空 seed 时不调用工厂，返回 nil")
	}
}

// TestBuildContextFromSeed_往返 验证 seed → context 的关键字段还原。
func TestBuildContextFromSeed_往返(t *testing.T) {
	schema.RegisterBuildContextFactory(func(seed map[string]any) (schema.BuildContextInterface, error) {
		lang, _ := seed["language"].(string)
		return &schema.BuildContext{Language: lang}, nil
	})
	t.Cleanup(schema.ResetBuildContextFactoryForTest)

	ctx, err := schema.BuildContextFromSeed(map[string]any{"language": "en"})

	require.NoError(t, err)
	require.NotNil(t, ctx)
	assert.Equal(t, "en", ctx.GetLanguage())
}

// TestParentSysOperation_未发布返回nil 验证 extras 里没有该键时返回 nil。
//
// 对齐上游 build_context.py:36-48 的 parent_sys_operation：
// 「容忍 None 与没有 extras 的上下文，让 provider 可以无条件调用」。
func TestParentSysOperation_未发布返回nil(t *testing.T) {
	assert.Nil(t, schema.ParentSysOperation(nil), "nil 上下文安全")

	ctx := &schema.BuildContext{}
	assert.Nil(t, schema.ParentSysOperation(ctx), "未发布时返回 nil")
}

// TestParentSysOperation_已发布返回句柄 验证发布后能取回。
func TestParentSysOperation_已发布返回句柄(t *testing.T) {
	ctx := &schema.BuildContext{}
	ctx.GetExtras()[schema.ParentSysOperationExtrasKey] = "sysop-handle"

	assert.Equal(t, "sysop-handle", schema.ParentSysOperation(ctx))
}

// TestParentSysOperationExtrasKey_值与上游一致 键值必须逐字对齐。
//
// 上游 build_context.py:33：PARENT_SYS_OPERATION_EXTRAS_KEY = "_parent_sys_operation"
func TestParentSysOperationExtrasKey_值与上游一致(t *testing.T) {
	assert.Equal(t, "_parent_sys_operation", schema.ParentSysOperationExtrasKey)
}
