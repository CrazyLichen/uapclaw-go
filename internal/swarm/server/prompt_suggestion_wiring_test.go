package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// TestNewSessionDeleteProvisioner_PromptForks是真实现 桩必须被删除（规则 8）。
// 10.3.28c 的 dispose 第①步依赖 PromptSuggestionForks.session_deleted，
// 原先注入 noopPromptSuggestionForks，10.3.30 改为全局真实现。
func TestNewSessionDeleteProvisioner_PromptForks是真实现(t *testing.T) {
	var forks swruntime.PromptSuggestionForks = recommendation.Forks
	assert.NotNil(t, forks)
	assert.Same(t, recommendation.Forks, forks,
		"必须注入全局 Forks，而不是另造实例——删会话与关服务要作用于同一份在飞任务表")
}

// TestPromptSuggestionForks_SessionDeleted经接口可用 验证接口满足性。
func TestPromptSuggestionForks_SessionDeleted经接口可用(t *testing.T) {
	var forks swruntime.PromptSuggestionForks = recommendation.Forks
	assert.NotPanics(t, func() {
		forks.SessionDeleted("nonexistent-session")
	})
}
