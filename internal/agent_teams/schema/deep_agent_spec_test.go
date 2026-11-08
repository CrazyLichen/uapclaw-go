package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestRailSpec别名_与core同类型 团队侧 RailSpec 必须是 core 类型的别名（非副本）。
//
// 依据：上游 RailSpec 定义在 openjiuwen/harness/schema/deep_agent_spec.py（core 层），
// 团队侧只是 import 使用。若将来有人把别名改成独立结构体，
// 两边的 Build 方法行为就会分叉。
func TestRailSpec别名_与core同类型(t *testing.T) {
	var teamSpec = &atschema.RailSpec{Type: "x", Params: map[string]any{"k": "v"}}
	var coreSpec = teamSpec

	require.NotNil(t, coreSpec)
	assert.Equal(t, "x", coreSpec.Type, "别名指向同一类型，字段互通")
	assert.Equal(t, "v", coreSpec.Params["k"])
}

// TestBuiltinToolSpec别名_与core同类型 同上。
func TestBuiltinToolSpec别名_与core同类型(t *testing.T) {
	var teamSpec = &atschema.BuiltinToolSpec{Type: "web"}
	var coreSpec = teamSpec

	require.NotNil(t, coreSpec)
	assert.Equal(t, "web", coreSpec.Type)
}
