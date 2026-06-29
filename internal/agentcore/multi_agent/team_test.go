package multi_agent

import (
	"context"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubTeam 用于编译时检查 BaseTeam 接口满足的桩实现。
type stubTeam struct {
	card   *TeamCard
	config TeamConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时检查 stubTeam 满足 BaseTeam 接口。
var _ BaseTeam = (*stubTeam)(nil)

func (t *stubTeam) Invoke(_ context.Context, _ map[string]any, _ ...TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Stream(_ context.Context, _ map[string]any, _ ...TeamOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (t *stubTeam) AddAgent(_ context.Context, _ *agentschema.AgentCard, _ resources_manager.AgentProvider) error {
	return nil
}

func (t *stubTeam) RemoveAgent(_ context.Context, _ string) error {
	return nil
}

func (t *stubTeam) Send(_ context.Context, _ map[string]any, _ string, _ string, _ ...TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Publish(_ context.Context, _ map[string]any, _ string, _ string, _ ...TeamOption) error {
	return nil
}

func (t *stubTeam) Subscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Unsubscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Configure(_ context.Context, _ TeamConfig) error {
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

func (t *stubTeam) Card() *TeamCard {
	return t.card
}

func (t *stubTeam) Config() TeamConfig {
	return t.config
}

// TestBaseTeam_编译时接口检查 验证 stubTeam 满足 BaseTeam 接口。
func TestBaseTeam_编译时接口检查(t *testing.T) {
	card := &TeamCard{BaseCard: schema.BaseCard{ID: "test-team", Name: "test"}}
	team := &stubTeam{card: card, config: TeamConfig{}}

	// 基本调用验证
	_ = team.Card()
	_ = team.Config()
	_ = team.GetAgentCount()
	_ = team.ListAgents()
}
