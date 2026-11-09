package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAgentManagerHasActiveGoal_不借不建agent 探针必须只读。
// Python: agent_manager.py:1423-1431
func TestAgentManagerHasActiveGoal_不借不建agent(t *testing.T) {
	am := NewAgentManager()
	require.False(t, am.HasActiveGoal("web", "sess-x"))
	require.Empty(t, am.agents, "探针不得创建任何 agent")
}

// TestAgentManagerHasActiveGoal_无adapter的缓存agent为假 agent 存在但 adapter 未建。
func TestAgentManagerHasActiveGoal_无adapter的缓存agent为假(t *testing.T) {
	am := NewAgentManager()
	channelKey := normalizeChannelID("web")
	am.agents[channelKey] = map[string]*agentEntry{
		"k1": {agent: NewUapClaw(), cacheKey: "k1"},
	}
	require.False(t, am.HasActiveGoal("web", "sess-y"))
}
