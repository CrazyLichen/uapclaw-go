package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
)

// TestStreamController_PauseAgent_资源为nil时安全 验证未装配资源时暂停/恢复是 no-op。
func TestStreamController_PauseAgent_资源为nil时安全(t *testing.T) {
	sc := &StreamController{}
	assert.NoError(t, sc.PauseAgent(context.Background()))
	assert.NoError(t, sc.ResumeAgent(context.Background(), nil))
}

// TestStreamController_PauseAgent_未启动Harness安全 验证 harness 未启动时暂停/恢复是 no-op。
func TestStreamController_PauseAgent_未启动Harness安全(t *testing.T) {
	sc := &StreamController{
		resources: &PrivateAgentResources{Harness: &teamharness.TeamHarness{}},
	}
	assert.NoError(t, sc.PauseAgent(context.Background()))
	empty := ""
	assert.NoError(t, sc.ResumeAgent(context.Background(), &empty))
}
