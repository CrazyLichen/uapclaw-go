package harness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestNewNativeHarness_从spec物化 构造器收 spec 而非已建好的 agent。
//
// 对齐上游 native_harness.py:150-192：前向构造——先 resolve_parts，
// 再用 spec 的真实 card 配置 DeepAgent（无 placeholder、无 throwaway 模板）。
func TestNewNativeHarness_从spec物化(t *testing.T) {
	spec := atschema.NewDeepAgentSpec()
	spec.Language = "cn"

	h, err := harness.NewNativeHarness(spec, &hschema.BuildContext{Language: "cn"})

	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, harness.HarnessStateIdle, h.State(), "构造后相位为 idle")
	assert.NotNil(t, h.DeepAgent(), "构造期即完成 resolve，内层 agent 已就位")
}

// TestNewNativeHarness_构造即完成resolve 构造后 DeepConfig 已可用（前向构造）。
//
// 上游：「Synchronous, so a host can read workspace / sys_operation off
// deep_config right after construction」。
func TestNewNativeHarness_构造即完成resolve(t *testing.T) {
	spec := atschema.NewDeepAgentSpec()
	spec.Language = "en"

	h, err := harness.NewNativeHarness(spec, nil)

	require.NoError(t, err)
	cfg := h.DeepAgent().DeepConfig()
	require.NotNil(t, cfg, "构造后 config 必须就位（对齐上游同步配置）")
	assert.NotNil(t, cfg.Card, "card 已配置")
}

// TestNewNativeHarness_nilContext安全 ctx 为 nil 时自建，不报错。
func TestNewNativeHarness_nilContext安全(t *testing.T) {
	spec := atschema.NewDeepAgentSpec()

	h, err := harness.NewNativeHarness(spec, nil)

	require.NoError(t, err)
	require.NotNil(t, h)
}

// TestNativeHarness_prepare_模板哨兵 AgentTemplateSpec 非 nil 时返回 ErrTemplateUnsupported。
//
// ⤵️ 15.7-15.9 换真实现（决策 C2-b）。
func TestNativeHarness_prepare_模板哨兵(t *testing.T) {
	spec := atschema.NewDeepAgentSpec()
	spec.AgentTemplateSpec = map[string]any{"some": "template"}

	h, err := harness.NewNativeHarness(spec, nil)
	require.NoError(t, err)

	// prepare 未导出，通过 Start 间接触发（Start 第一步调 prepare）
	err = h.Start(t.Context(), nil)

	require.Error(t, err, "模板非 nil 应返回哨兵错误")
	assert.ErrorIs(t, err, harness.ErrTemplateUnsupported)
}

// TestErrTemplateUnsupported_是明确哨兵 哨兵错误必须有可读消息。
func TestErrTemplateUnsupported_是明确哨兵(t *testing.T) {
	require.Error(t, harness.ErrTemplateUnsupported)
	assert.Contains(t, harness.ErrTemplateUnsupported.Error(), "template")
}

// TestNativeHarness_prepare_幂等 多次 prepare 只做一次（模板哨兵仍生效）。
//
// 上游：「Idempotent. start calls this first」。
func TestNativeHarness_prepare_幂等(t *testing.T) {
	spec := atschema.NewDeepAgentSpec()
	spec.AgentTemplateSpec = map[string]any{"t": "v"}

	h, err := harness.NewNativeHarness(spec, nil)
	require.NoError(t, err)

	err1 := h.Start(t.Context(), nil)
	err2 := h.Start(t.Context(), nil)

	assert.ErrorIs(t, err1, harness.ErrTemplateUnsupported)
	assert.ErrorIs(t, err2, harness.ErrTemplateUnsupported, "重复调用仍返回同一哨兵")
}
