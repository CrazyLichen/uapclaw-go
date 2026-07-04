package multi_agent

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubTeam 用于编译时检查 BaseTeam 接口满足的桩实现。
type stubTeam struct {
	card   schema.TeamCardInterface
	config *schema.TeamConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时检查 stubTeam 满足 BaseTeam 接口。
var _ schema.BaseTeam = (*stubTeam)(nil)

func (t *stubTeam) Invoke(_ context.Context, _ map[string]any, _ ...schema.TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Stream(_ context.Context, _ map[string]any, _ ...schema.TeamOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (t *stubTeam) AddAgent(_ context.Context, _ *agentschema.AgentCard, _ schema.TeamAgentProvider, _ ...schema.TeamOption) error {
	return nil
}

func (t *stubTeam) RemoveAgent(_ context.Context, _ string) error {
	return nil
}

func (t *stubTeam) Send(_ context.Context, _ map[string]any, _ string, _ string, _ ...schema.TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Publish(_ context.Context, _ map[string]any, _ string, _ string, _ ...schema.TeamOption) error {
	return nil
}

func (t *stubTeam) Subscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Unsubscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Configure(_ context.Context, _ schema.TeamConfig) error {
	return nil
}

func (t *stubTeam) GetAgentCard(_ string) (*agentschema.AgentCard, error) {
	return nil, nil
}

func (t *stubTeam) GetAgentCount() int {
	return 0
}

func (t *stubTeam) ListAgents() []string {
	return nil
}

func (t *stubTeam) Card() schema.TeamCardInterface {
	return t.card
}

func (t *stubTeam) Config() *schema.TeamConfig {
	return t.config
}

// TestBaseTeam_编译时接口检查 验证 stubTeam 满足 BaseTeam 接口。
func TestBaseTeam_编译时接口检查(t *testing.T) {
	card := schema.NewTeamCard(schema.WithTeamCardName("test-team"))
	team := &stubTeam{card: card, config: schema.NewTeamConfig()}

	// 基本调用验证
	_ = team.Card()
	_ = team.Config()
	_ = team.GetAgentCount()
	_ = team.ListAgents()
}
