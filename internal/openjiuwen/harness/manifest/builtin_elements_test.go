//go:build !test

package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/workspace"
)

func init() { EnsureCoreElementsRegistered() }

// builtin_elements 单测：逐 provider 对齐上游构造语义
// （builtin_elements.py:84-420）。包内测试以直测非导出助手。

// ──────────────────────────── 测试 ────────────────────────────

// TestWebLanguageAgentID_从context取 web 三件的 language/agent_id 来自
// context（Python WebToolInput :192-211 context_field），member_card_id
// 命名空间化。
func TestWebLanguageAgentID_从context取(t *testing.T) {
	ctx := &hschema.BuildContext{Language: "en", MemberCardID: "card-9"}
	provider, ok := hschema.LookupToolProvider("core.web_search")
	require.True(t, ok)
	tools, err := provider(nil, ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	card := tools[0].(tool.Tool).Card()
	assert.Equal(t, "free_search", card.GetName())
	// agent_id 命名空间化：card id 应携带 member card id（非随机 uuid）
	assert.Contains(t, card.GetID(), "card-9")

	// 缺省 language=cn
	tools2, err2 := provider(nil, &hschema.BuildContext{})
	require.NoError(t, err2)
	require.Len(t, tools2, 1)
}

// TestWebPaidSearch_未配置key返回空 付费搜索 key 未配置 → 空表（:240-254）。
func TestWebPaidSearch_未配置key返回空(t *testing.T) {
	provider, ok := hschema.LookupToolProvider("core.web_paid_search")
	require.True(t, ok)
	tools, err := provider(nil, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.Empty(t, tools, "未配置付费 key 应跳过（IsPaidSearchEnabled=false）")
}

// TestVision_完整性门控 is_vision_model_config_complete：api_key/base_url/
// model 三者非空才物化（:273-296 + schema/config.py:97-108）。
func TestVision_完整性门控(t *testing.T) {
	provider, ok := hschema.LookupToolProvider("core.vision")
	require.True(t, ok)

	// 未配置 → nil
	tools, err := provider(nil, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.Empty(t, tools)

	// 不完整（缺 model）→ nil
	tools2, err2 := provider(map[string]any{
		"vision_model_config": map[string]any{"api_key": "k", "base_url": "https://x"},
	}, &hschema.BuildContext{})
	require.NoError(t, err2)
	assert.Empty(t, tools2, "缺 model 应判不完整跳过")

	// 完整 → vision 工具组
	tools3, err3 := provider(map[string]any{
		"vision_model_config": map[string]any{
			"api_key": "k", "base_url": "https://x", "model": "v-model",
		},
	}, &hschema.BuildContext{})
	require.NoError(t, err3)
	assert.NotEmpty(t, tools3, "完整配置应物化 vision 工具组")
}

// TestAudio_dedicated分支 dedicated=false → 仅 audio_metadata（:319-366）；
// dedicated+完整 config → 全组。
func TestAudio_dedicated分支(t *testing.T) {
	provider, ok := hschema.LookupToolProvider("core.audio")
	require.True(t, ok)

	// 非 dedicated → 仅 audio_metadata
	tools, err := provider(nil, &hschema.BuildContext{})
	require.NoError(t, err)
	require.Len(t, tools, 1, "无 dedicated 应仅 audio_metadata")
	assert.Equal(t, "audio_metadata", tools[0].(tool.Tool).Card().GetName())

	// dedicated + 完整 config → 全组（>1）
	tools2, err2 := provider(map[string]any{
		"dedicated":          true,
		"audio_model_config": map[string]any{"api_key": "k", "base_url": "https://x", "question_answering_model": "qa"},
	}, &hschema.BuildContext{})
	require.NoError(t, err2)
	assert.Greater(t, len(tools2), 1, "dedicated 齐全应物化音频全组")
}

// TestSkillUseDirs_workspace派生 params 缺 skills_dir 时从 workspace skills
// 节点 + 默认 CLI 目录派生（:89-103）。
func TestSkillUseDirs_workspace派生(t *testing.T) {
	ws := workspace.NewWorkspace(t.TempDir(), "")
	ctx := &hschema.BuildContext{Workspace: ws}
	dirs := skillUseDirs(nil, ctx)
	require.NotEmpty(t, dirs)
	// 首位 = workspace skills 节点
	assert.Contains(t, dirs[0], "skills")
	// 末两位 = Python 字面默认目录（展开后）
	assert.Contains(t, dirs[len(dirs)-2], ".openjiuwen")
	assert.Contains(t, dirs[len(dirs)-1], ".claude")

	// params 显式 skills_dir 优先
	dirs2 := skillUseDirs(map[string]any{"skills_dir": []any{"/custom/lib"}}, ctx)
	assert.Equal(t, []string{"/custom/lib"}, dirs2)
}

// TestWorktreeConfig_从params WorktreeConfig(**params)（:155-165）。
func TestWorktreeConfig_从params(t *testing.T) {
	provider, ok := hschema.LookupRailProvider("core.worktree")
	require.True(t, ok)
	rail, err := provider(map[string]any{"enabled": true, "base_dir": "/tmp/wt"}, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.NotNil(t, rail)
}

// TestTrackingRails_classbuilder直构 token/tool tracking class builder
// 直构造（:429/:435），Go 基座 15.5 已有。
func TestTrackingRails_classbuilder直构(t *testing.T) {
	for _, name := range []string{"core.token_tracking", "core.tool_tracking"} {
		provider, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, name)
		rail, err := provider(nil, &hschema.BuildContext{})
		require.NoError(t, err)
		assert.NotNil(t, rail, name)
	}
}

// TestTaskPlanning_classbuilder无参 class builder 无参构造（:405），
// 不带 language（漂移修复回归锚）。
func TestTaskPlanning_classbuilder无参(t *testing.T) {
	provider, ok := hschema.LookupRailProvider("core.task_planning")
	require.True(t, ok)
	rail, err := provider(map[string]any{"language": "en"}, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.NotNil(t, rail)
}

// TestLspObservability_基座缺返回nil lsp/observability 基座缺 → nil
// （对齐 Python provider 存在但返回 None 的门控语义）。
func TestLspObservability_基座缺返回nil(t *testing.T) {
	for _, name := range []string{"core.lsp", "core.observability"} {
		provider, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, name)
		rail, err := provider(nil, &hschema.BuildContext{})
		require.NoError(t, err)
		assert.Nil(t, rail, name)
	}
}

// TestEnsureCoreElementsRegistered_幂等 重复调用安全。
func TestEnsureCoreElementsRegistered_幂等(t *testing.T) {
	EnsureCoreElementsRegistered()
	EnsureCoreElementsRegistered()
	_, ok := hschema.LookupToolProvider("core.web_search")
	assert.True(t, ok)
}
