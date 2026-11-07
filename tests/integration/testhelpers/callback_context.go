//go:build integration

package testhelpers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FailingHistory 固定失败的 HistoryContext，用于验证附件同步失败分支。
type FailingHistory struct {
	// sessionID 会话标识
	sessionID string
	// err 固定返回的错误
	err error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCallbackContextForTest 构造一个只挂 session 的回调上下文，供集成用例挂 extra。
//
// 覆盖链路：single_agent/interfaces（回调上下文）→ session（SessionFacade 替身）。
func NewCallbackContextForTest(t *testing.T, sessionID string) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, NewMockSessionFacade(sessionID))
	require.NotNil(t, cbc)
	return cbc
}

// NewFailingHistory 构造固定失败的历史上下文。
func NewFailingHistory(sessionID string, err error) *FailingHistory {
	return &FailingHistory{sessionID: sessionID, err: err}
}

// SessionID 返回固定会话 id
func (f *FailingHistory) SessionID() string { return f.sessionID }

// GetMessages 固定失败
func (f *FailingHistory) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return nil, f.err
}

// AddMessages 固定失败
func (f *FailingHistory) AddMessages(_ context.Context, _ []llmschema.BaseMessage, _ ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	return nil, f.err
}
