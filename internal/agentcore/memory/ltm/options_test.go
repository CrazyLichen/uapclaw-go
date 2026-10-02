package ltm

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
)

// TestNewAddMessagesParams_默认值 测试默认参数值对齐 Python。
func TestNewAddMessagesParams_默认值(t *testing.T) {
	p := newAddMessagesParams(nil, nil)
	if p.UserID != DefaultValue {
		t.Errorf("UserID = %q, want %q", p.UserID, DefaultValue)
	}
	if p.ScopeID != DefaultValue {
		t.Errorf("ScopeID = %q, want %q", p.ScopeID, DefaultValue)
	}
	if !p.GenMem {
		t.Errorf("GenMem = false, want true")
	}
	if p.GenMemWithHistoryMsgNum != 2 {
		t.Errorf("GenMemWithHistoryMsgNum = %d, want 2", p.GenMemWithHistoryMsgNum)
	}
}

// TestNewAddMessagesParams_自定义选项 测试选项覆盖默认值。
func TestNewAddMessagesParams_自定义选项(t *testing.T) {
	agentCfg := config.DefaultAgentMemoryConfig()
	p := newAddMessagesParams(nil, agentCfg,
		WithUserID("u1"),
		WithScopeID("s1"),
		WithGenMem(false),
	)
	if p.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", p.UserID, "u1")
	}
	if p.GenMem {
		t.Errorf("GenMem = true, want false")
	}
	if p.AgentConfig != agentCfg {
		t.Errorf("AgentConfig 未正确设置")
	}
}

// TestNewSearchParams_默认值 测试搜索参数默认值。
func TestNewSearchParams_默认值(t *testing.T) {
	p := newSearchParams("query", 5)
	if p.Threshold != 0.3 {
		t.Errorf("Threshold = %f, want 0.3", p.Threshold)
	}
	if p.UserID != DefaultValue {
		t.Errorf("UserID = %q, want %q", p.UserID, DefaultValue)
	}
}

// TestNewSearchParams_自定义选项 测试搜索选项覆盖。
func TestNewSearchParams_自定义选项(t *testing.T) {
	p := newSearchParams("q", 10,
		SearchWithUserID("u2"),
		SearchWithThreshold(0.5),
	)
	if p.UserID != "u2" {
		t.Errorf("UserID = %q, want %q", p.UserID, "u2")
	}
	if p.Threshold != 0.5 {
		t.Errorf("Threshold = %f, want 0.5", p.Threshold)
	}
}
