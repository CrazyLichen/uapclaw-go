package schema

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubTeam 用于编译时检查 BaseTeam 接口满足的桩实现。
type stubTeam struct {
	card   TeamCardInterface
	config *TeamConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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

func (t *stubTeam) AddAgent(_ context.Context, _ *agentschema.AgentCard, _ TeamAgentProvider, _ ...TeamOption) error {
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

func (t *stubTeam) Card() TeamCardInterface {
	return t.card
}

func (t *stubTeam) Config() *TeamConfig {
	return t.config
}

// TestBaseTeam_编译时接口检查 验证 stubTeam 满足 BaseTeam 接口。
func TestBaseTeam_编译时接口检查(t *testing.T) {
	card := NewTeamCard(WithTeamCardName("test-team"))
	team := &stubTeam{card: card, config: NewTeamConfig()}

	// 基本调用验证
	_ = team.Card()
	_ = team.Config()
	_ = team.GetAgentCount()
	_ = team.ListAgents()
}

// TestNewTeamConfig_默认值 验证默认值 MaxAgents=10, MaxConcurrentMessages=100, MessageTimeout=30.0。
func TestNewTeamConfig_默认值(t *testing.T) {
	cfg := NewTeamConfig()
	if cfg.MaxAgents != 10 {
		t.Errorf("期望 MaxAgents=10，实际 %d", cfg.MaxAgents)
	}
	if cfg.MaxConcurrentMessages != 100 {
		t.Errorf("期望 MaxConcurrentMessages=100，实际 %d", cfg.MaxConcurrentMessages)
	}
	if cfg.MessageTimeout != 30.0 {
		t.Errorf("期望 MessageTimeout=30.0，实际 %f", cfg.MessageTimeout)
	}
}

// TestTeamConfig_链式配置 验证 ConfigureMaxAgents/ConfigureTimeout/ConfigureConcurrency 链式调用。
func TestTeamConfig_链式配置(t *testing.T) {
	cfg := NewTeamConfig().
		ConfigureMaxAgents(5).
		ConfigureTimeout(60.0).
		ConfigureConcurrency(200)

	if cfg.MaxAgents != 5 {
		t.Errorf("期望 MaxAgents=5，实际 %d", cfg.MaxAgents)
	}
	if cfg.MessageTimeout != 60.0 {
		t.Errorf("期望 MessageTimeout=60.0，实际 %f", cfg.MessageTimeout)
	}
	if cfg.MaxConcurrentMessages != 200 {
		t.Errorf("期望 MaxConcurrentMessages=200，实际 %d", cfg.MaxConcurrentMessages)
	}
}

// TestTeamConfig_Extra 验证 SetExtra/GetExtra 读写。
func TestTeamConfig_Extra(t *testing.T) {
	cfg := NewTeamConfig()

	val, ok := cfg.GetExtra("not_exist")
	if ok {
		t.Error("不存在的 key 不应返回 ok=true")
	}
	if val != nil {
		t.Errorf("不存在的 key 应返回 nil，实际 %v", val)
	}

	cfg.SetExtra("custom_key", "custom_value")
	val, ok = cfg.GetExtra("custom_key")
	if !ok {
		t.Error("已设置的 key 应返回 ok=true")
	}
	if val != "custom_value" {
		t.Errorf("期望 'custom_value'，实际 %v", val)
	}
}

// TestNewTeamOptions_空选项 测试无选项时默认值。
func TestNewTeamOptions_空选项(t *testing.T) {
	opts := NewTeamOptions()
	if opts.Session != nil {
		t.Error("Session 应为 nil")
	}
	if opts.SessionID != "" {
		t.Error("SessionID 应为空")
	}
	if opts.Timeout != 0 {
		t.Error("Timeout 应为 0")
	}
	if opts.StreamModes != nil {
		t.Error("StreamModes 应为 nil")
	}
}

// TestWithTeamSessionID_设置会话标识 测试 WithTeamSessionID 选项。
func TestWithTeamSessionID_设置会话标识(t *testing.T) {
	opts := NewTeamOptions(WithTeamSessionID("sess-123"))
	if opts.SessionID != "sess-123" {
		t.Errorf("SessionID 期望 sess-123, 实际 %s", opts.SessionID)
	}
}

// TestWithTeamTimeout_设置超时 测试 WithTeamTimeout 选项。
func TestWithTeamTimeout_设置超时(t *testing.T) {
	opts := NewTeamOptions(WithTeamTimeout(30.0))
	if opts.Timeout != 30.0 {
		t.Errorf("Timeout 期望 30.0, 实际 %f", opts.Timeout)
	}
}

// TestWithTeamStreamModes_设置流模式 测试 WithTeamStreamModes 选项。
func TestWithTeamStreamModes_设置流模式(t *testing.T) {
	modes := []stream.StreamMode{stream.StreamModeOutput}
	opts := NewTeamOptions(WithTeamStreamModes(modes))
	if len(opts.StreamModes) != 1 || opts.StreamModes[0].Mode() != stream.StreamModeOutput.Mode() {
		t.Errorf("StreamModes 期望 [StreamModeOutput], 实际 %v", opts.StreamModes)
	}
}

// TestWithTeamSession_设置会话 测试 WithTeamSession 选项。
func TestWithTeamSession_设置会话(t *testing.T) {
	sess := &session.AgentTeamSession{}
	opts := NewTeamOptions(WithTeamSession(sess))
	if opts.Session != sess {
		t.Error("Session 期望为设置的会话实例")
	}
}

// TestWithParentAgentID_设置父AgentID 测试 WithParentAgentID 选项。
func TestWithParentAgentID_设置父AgentID(t *testing.T) {
	opts := NewTeamOptions(WithParentAgentID("parent_123"))
	if opts.ParentAgentID != "parent_123" {
		t.Errorf("ParentAgentID 期望 parent_123, 实际 %s", opts.ParentAgentID)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
