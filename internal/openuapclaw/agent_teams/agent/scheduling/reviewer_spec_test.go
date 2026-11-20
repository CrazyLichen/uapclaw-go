// Python 源：reviewer_spec.py 用例 1:1 转 Go（收窄语义七项）

package scheduling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/manifest"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// reviewerBaseFixture 构造带全量能力面的 teammate 基座 spec。
func reviewerBaseFixture() *hschema.DeepAgentSpec {
	base := &hschema.DeepAgentSpec{
		MaxIterations:          15,
		AutoCreateWorkspace:    true,
		CompletionTimeout:      3600.0,
		AddGeneralPurposeAgent: true,
		EnableAsyncSubagent:    true,
		EnableTaskPlanning:     true,
	}
	base.Rails = []hschema.RailSpec{
		{Type: manifest.SysOperation, Params: map[string]any{"write": true}},
		{Type: manifest.Security, Params: map[string]any{}},
		{Type: manifest.SkillUse, Params: map[string]any{}},
	}
	base.BuiltinTools = []hschema.BuiltinToolSpec{
		{Type: manifest.TaskPlanning},
		{Type: manifest.Subagent},
		{Type: manifest.SysOperation},
	}
	base.ToolCards = []*tool.ToolCard{{BaseCard: schema.BaseCard{Name: "raw_card"}}}
	base.Subagents = []hschema.SubAgentSpec{{SystemPrompt: "reviewer-x"}}
	return base
}

func TestBuildReviewerAgentSpec(t *testing.T) {
	t.Run("文件工具只读化_其他rail透传", func(t *testing.T) {
		base := reviewerBaseFixture()
		reviewer := BuildReviewerAgentSpec(base, nil, nil)
		require.Len(t, reviewer.Rails, 3)
		assert.True(t, reviewer.Rails[0].Params["read_only"].(bool))
		assert.True(t, base.Rails[0].Params["write"].(bool)) // 原 rail.Params 不被改动
		_, hasReadOnly := base.Rails[0].Params["read_only"]
		assert.False(t, hasReadOnly) // base 未被污染
		assert.Equal(t, manifest.Security, reviewer.Rails[1].Type)
	})
	t.Run("subagents清空_通用代理关_异步子代理关", func(t *testing.T) {
		reviewer := BuildReviewerAgentSpec(reviewerBaseFixture(), nil, nil)
		assert.Nil(t, reviewer.Subagents)
		assert.False(t, reviewer.AddGeneralPurposeAgent)
		assert.False(t, reviewer.EnableAsyncSubagent)
	})
	t.Run("todo_planning关", func(t *testing.T) {
		reviewer := BuildReviewerAgentSpec(reviewerBaseFixture(), nil, nil)
		assert.False(t, reviewer.EnableTaskPlanning)
	})
	t.Run("完成超时钉1200秒", func(t *testing.T) {
		assert.Equal(t, 1200.0, ReviewerCompletionTimeoutSeconds)
		reviewer := BuildReviewerAgentSpec(reviewerBaseFixture(), nil, nil)
		assert.Equal(t, 1200.0, reviewer.CompletionTimeout)
	})
	t.Run("rail白名单收窄_nil保留全部", func(t *testing.T) {
		base := reviewerBaseFixture()
		full := BuildReviewerAgentSpec(base, nil, nil)
		assert.Len(t, full.Rails, 3)
		narrowed := BuildReviewerAgentSpec(base, []string{manifest.SysOperation}, nil)
		require.Len(t, narrowed.Rails, 1)
		assert.Equal(t, manifest.SysOperation, narrowed.Rails[0].Type)
	})
	t.Run("tool白名单只作用于BuiltinToolSpec_原始卡片恒保留", func(t *testing.T) {
		base := reviewerBaseFixture()
		full := BuildReviewerAgentSpec(base, nil, nil)
		assert.Len(t, full.BuiltinTools, 3)
		assert.Len(t, full.ToolCards, 1) // 原始工具卡片原样继承
		narrowed := BuildReviewerAgentSpec(base, nil, []string{"core.sys_operation"})
		require.Len(t, narrowed.BuiltinTools, 1)
		assert.Equal(t, "core.sys_operation", narrowed.BuiltinTools[0].Type)
	})
	t.Run("MCP原样继承", func(t *testing.T) {
		base := reviewerBaseFixture()
		reviewer := BuildReviewerAgentSpec(base, nil, nil)
		assert.Equal(t, base.Mcps, reviewer.Mcps)
	})
}
