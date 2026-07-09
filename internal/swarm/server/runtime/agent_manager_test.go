package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewAgentManager(t *testing.T) {
	am := NewAgentManager()
	require.NotNil(t, am)
	require.NotNil(t, am.stubAgent)
}

func TestAgentManager_GetAgent(t *testing.T) {
	am := NewAgentManager()
	agent, err := am.GetAgent("web", "agent", "", "")
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

func TestAgentManager_GetAgentNoWait(t *testing.T) {
	am := NewAgentManager()
	agent := am.GetAgentNoWait("web", "agent", "", "")
	assert.NotNil(t, agent)
}

func TestAgentManager_ReloadAgentsConfig(t *testing.T) {
	am := NewAgentManager()
	err := am.ReloadAgentsConfig(nil, nil)
	assert.NoError(t, err)
}

func TestAgentManager_Initialize(t *testing.T) {
	am := NewAgentManager()
	caps, err := am.Initialize("web", nil)
	require.NoError(t, err)
	assert.NotNil(t, caps)
	_, ok := caps["capabilities"]
	assert.True(t, ok)
}

func TestAgentManager_CancelAllInflightWork(t *testing.T) {
	am := NewAgentManager()
	err := am.CancelAllInflightWork(context.Background())
	assert.NoError(t, err)
}

func TestAgentManager_Cleanup(t *testing.T) {
	am := NewAgentManager()
	err := am.Cleanup()
	assert.NoError(t, err)
}
