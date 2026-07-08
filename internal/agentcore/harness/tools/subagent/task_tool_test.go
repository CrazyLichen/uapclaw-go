package subagent

import (
	"strings"
	"testing"
)

func TestBuildSubSessionID(t *testing.T) {
	tests := []struct {
		name            string
		parentSessionID string
		subagentType    string
		wantPrefix      string
	}{
		{
			name:            "browser_agent 确定性 ID",
			parentSessionID: "sess-123",
			subagentType:    "browser_agent",
			wantPrefix:      "sess-123_sub_browser_agent",
		},
		{
			name:            "verification_agent 确定性 ID",
			parentSessionID: "sess-456",
			subagentType:    "verification_agent",
			wantPrefix:      "sess-456_sub_verification_agent",
		},
		{
			name:            "其他类型带随机后缀",
			parentSessionID: "sess-789",
			subagentType:    "explore",
			wantPrefix:      "sess-789_sub_explore_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSubSessionID(tt.parentSessionID, tt.subagentType)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("期望前缀 %s，实际 %s", tt.wantPrefix, got)
			}
		})
	}
}

func TestBuildSubSessionID_随机后缀不重复(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		id := buildSubSessionID("sess", "explore")
		seen[id] = struct{}{}
	}
	if len(seen) < 10 {
		t.Errorf("50 次生成仅有 %d 个不同值", len(seen))
	}
}
