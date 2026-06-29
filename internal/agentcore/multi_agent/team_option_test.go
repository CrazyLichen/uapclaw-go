package multi_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

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

// TestWithTeamSession_设置会话 测试 WithTeamSession 选项。
func TestWithTeamSession_设置会话(t *testing.T) {
	sess := "test-session"
	opts := NewTeamOptions(WithTeamSession(sess))
	if opts.Session != sess {
		t.Errorf("Session 期望 %v, 实际 %v", sess, opts.Session)
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

// TestNewTeamOptions_多选项组合 测试多个选项组合。
func TestNewTeamOptions_多选项组合(t *testing.T) {
	opts := NewTeamOptions(
		WithTeamSessionID("sess-456"),
		WithTeamTimeout(60.0),
	)
	if opts.SessionID != "sess-456" {
		t.Errorf("SessionID 期望 sess-456, 实际 %s", opts.SessionID)
	}
	if opts.Timeout != 60.0 {
		t.Errorf("Timeout 期望 60.0, 实际 %f", opts.Timeout)
	}
}
