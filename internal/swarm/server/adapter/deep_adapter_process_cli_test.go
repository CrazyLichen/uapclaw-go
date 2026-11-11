package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestConfigureProcessCliRun_根adapter存选项 验证根只记待消费。
// Python: interface_deep.py:12432-12437 根是 router，存 _process_cli_run_options
func TestConfigureProcessCliRun_根adapter存选项(t *testing.T) {
	d := &DeepAdapter{isSessionScoped: false, channelID: "process_cli"}
	maxTurns := 5
	require.NoError(t, d.configureProcessCliRun(context.Background(), &maxTurns, nil))
	require.NotNil(t, d.processCliRunOptions, "根 adapter 应暂存选项等 child 消费")
}

// TestConfigureProcessCliRun_非processCli渠道报错 验证渠道守卫。
// Python: interface_deep.py:12430-12431 if self._channel_id != "process_cli": raise ValueError
func TestConfigureProcessCliRun_非processCli渠道报错(t *testing.T) {
	d := &DeepAdapter{isSessionScoped: true, channelID: "web"}
	maxTurns := 5
	err := d.configureProcessCliRun(context.Background(), &maxTurns, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "require a Process CLI Agent")
}

// TestConfigureProcessCliRun_child未就绪报错 验证 instance 判空。
// Python: interface_deep.py:12438-12439 if self._instance is None: raise ValueError
func TestConfigureProcessCliRun_child未就绪报错(t *testing.T) {
	d := &DeepAdapter{isSessionScoped: true, channelID: "process_cli"}
	maxTurns := 5
	err := d.configureProcessCliRun(context.Background(), &maxTurns, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not ready")
}

// TestConfigureProcessCliRun_hostTools未实现报错 验证 ⤵️ 20.6 占位。
func TestConfigureProcessCliRun_hostTools未实现报错(t *testing.T) {
	d := &DeepAdapter{isSessionScoped: true, channelID: "process_cli"}
	d.instance = newStubDeepAgentForProcessCli(t)
	err := d.configureProcessCliRun(context.Background(), nil, []any{"tool"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "20.6")
}

// TestTakeProcessCliRunOptions_取一次即清空 验证 run options 不跨请求泄漏。
// Python: interface_deep.py:18989-18992 取用前先置 None
func TestTakeProcessCliRunOptions_取一次即清空(t *testing.T) {
	maxTurns := 5
	d := &DeepAdapter{processCliRunOptions: &ProcessCliRunOptions{MaxTurns: &maxTurns}}

	first := d.TakeProcessCliRunOptions()
	require.NotNil(t, first)
	require.Equal(t, 5, *first.MaxTurns)

	second := d.TakeProcessCliRunOptions()
	require.Nil(t, second, "取过一次后必须清空，避免失败请求泄漏回调")
}

// newStubDeepAgentForProcessCli 造一个最小的 DeepAgent（未装配 react agent）。
func newStubDeepAgentForProcessCli(t *testing.T) *harness.DeepAgent {
	t.Helper()
	return harness.NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("stub")))
}
