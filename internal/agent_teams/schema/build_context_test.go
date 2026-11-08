package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestBuildContext转发_与core同类型 团队侧别名必须指向 core 同一类型（非副本）。
//
// 依据上游 openjiuwen/agent_teams/schema/build_context.py 的注释：
// 「保留为薄转发，让既有团队 import 路径继续工作并指向同一对象（is-identical）」。
func TestBuildContext转发_与core同类型(t *testing.T) {
	var teamCtx *atschema.BuildContext = &hschema.BuildContext{Language: "cn"}
	var coreCtx *hschema.BuildContext = teamCtx

	require.NotNil(t, coreCtx)
	assert.Equal(t, "cn", coreCtx.GetLanguage(), "别名指向同一类型，字段互通")
}

// TestBuildContextInterface转发_与core同类型 接口别名同样指向 core。
func TestBuildContextInterface转发_与core同类型(t *testing.T) {
	var teamIface atschema.BuildContextInterface = &atschema.BuildContext{MemberName: "alice"}
	var coreIface hschema.BuildContextInterface = teamIface

	require.NotNil(t, coreIface)
	assert.Equal(t, "alice", coreIface.GetMemberName())
}

// TestBuildContextFromSeed转发_透传到core 验证函数转发生效。
func TestBuildContextFromSeed转发_透传到core(t *testing.T) {
	hschema.RegisterBuildContextFactory(func(seed map[string]any) (hschema.BuildContextInterface, error) {
		lang, _ := seed["language"].(string)
		return &hschema.BuildContext{Language: lang}, nil
	})
	t.Cleanup(hschema.ResetBuildContextFactoryForTest)

	ctx, err := atschema.BuildContextFromSeed(map[string]any{"language": "en"})

	require.NoError(t, err)
	require.NotNil(t, ctx)
	assert.Equal(t, "en", ctx.GetLanguage(), "团队侧转发应复用 core 的工厂")
}

// TestParentSysOperation转发_透传到core 验证父 SysOperation 读取转发。
func TestParentSysOperation转发_透传到core(t *testing.T) {
	ctx := &hschema.BuildContext{}
	ctx.GetExtras()[hschema.ParentSysOperationExtrasKey] = "handle"

	assert.Equal(t, "handle", atschema.ParentSysOperation(ctx))
}
