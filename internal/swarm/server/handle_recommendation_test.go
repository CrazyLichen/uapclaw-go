package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newRecommendationTestServer 临时工作区 + 指定 yaml 的 AgentServer。
func newRecommendationTestServer(t *testing.T, yaml string) *AgentServer {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()
	t.Cleanup(path.ResetCache)
	cfgPath := ""
	if yaml != "" {
		cfgDir := filepath.Join(tmpDir, "config")
		require.NoError(t, os.MkdirAll(cfgDir, 0o755))
		cfgPath = filepath.Join(cfgDir, "config.yaml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o644))
	}
	cfg, err := config.New(cfgPath)
	require.NoError(t, err)
	return NewAgentServer(cfg, transport.NewChannelTransport())
}

// marshalParams map → json.RawMessage。
func marshalParams(t *testing.T, value map[string]any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

// strPtr 字符串指针。
func strPtr(value string) *string { return &value }

// recommendationStoreForTest 打开工作区内的账本库。
func recommendationStoreForTest(t *testing.T) *recommendation.TopicStarterStore {
	t.Helper()
	store, err := recommendation.NewTopicStarterStore(
		filepath.Join(workspace.AgentWorkspaceDir(), recommendation.ConsentDBName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// seedRPCGeneration 写一条 candidate generation 事件。
func seedRPCGeneration(t *testing.T, store *recommendation.TopicStarterStore, sessionID, requestID, suggestionID, text string) {
	t.Helper()
	ok, err := store.RecordEvent(map[string]any{
		"session_id": sessionID, "slate_id": "prompt-suggestion:" + requestID,
		"event_type": "prompt_suggestion_generation", "topic_id": "prompt-suggestion:" + suggestionID,
		"policy_id": recommendation.PolicyID, "selection_mode": "prompt_suggestion",
		"propensity_reason": "not_estimated", "created_at": float64(time.Now().UnixNano()) / 1e9,
		"payload": map[string]any{"suggestion_id": suggestionID, "session_id": sessionID,
			"request_id": requestID, "outcome": "candidate", "text": text},
	})
	require.True(t, ok)
	require.NoError(t, err)
}

// rpcCompletionRow 写一条完成行并等落盘。
func rpcCompletionRow(t *testing.T, sid, rid string) {
	t.Helper()
	receipt, err := session.EnqueueHistoryRequestCompletion(sid, rid, "success")
	require.NoError(t, err)
	require.NotNil(t, receipt)
	select {
	case <-receipt:
	case <-time.After(5 * time.Second):
		t.Fatal("完成行 5s 未落盘")
	}
}

// TestHandleRecommendation_Get空态 无建议 → enabled=true 空 suggestions。
func TestHandleRecommendation_Get空态(t *testing.T) {
	s := newRecommendationTestServer(t, "")
	recommendation.Forks.SetConfig(map[string]any{"recommendation": map[string]any{"prompt_enabled": true}})
	t.Cleanup(func() { recommendation.Forks.SetConfig(nil) })
	req := &schema.AgentRequest{RequestID: "rid", ChannelID: "web",
		ReqMethod: schema.ReqMethodRecommendation, SessionID: strPtr("s-get"),
		Params: marshalParams(t, map[string]any{"action": "prompt_suggestion_get"})}
	resp, err := s.handleRecommendation(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, true, resp.Payload["enabled"])
	assert.Empty(t, resp.Payload["suggestions"])
}

// TestHandleRecommendation_Get空session 空会话 → BAD_REQUEST。
func TestHandleRecommendation_Get空session(t *testing.T) {
	s := newRecommendationTestServer(t, "")
	req := &schema.AgentRequest{RequestID: "rid", ChannelID: "web",
		ReqMethod: schema.ReqMethodRecommendation,
		Params:    marshalParams(t, map[string]any{"action": "prompt_suggestion_get"})}
	resp, err := s.handleRecommendation(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, resp.OK)
	assert.Equal(t, "BAD_REQUEST", resp.Payload["code"])
}

// TestHandleRecommendation_Event回执与非法 topic 开关开：合法回执 recorded=true；非法 BAD_REQUEST。
func TestHandleRecommendation_Event回执与非法(t *testing.T) {
	s := newRecommendationTestServer(t, "recommendation:\n  topic_enabled: true\n")
	store := recommendationStoreForTest(t)
	seedRPCGeneration(t, store, "s-rpc", "rr", "sg", "hi")
	rpcCompletionRow(t, "s-rpc", "rr")

	okResp, err := s.handleRecommendation(context.Background(), &schema.AgentRequest{
		RequestID: "rid", ChannelID: "web", ReqMethod: schema.ReqMethodRecommendation,
		SessionID: strPtr("s-rpc"),
		Params:    marshalParams(t, map[string]any{"action": "prompt_suggestion_event", "session_id": "s-rpc", "event_type": "shown", "suggestion_id": "sg"}),
	})
	require.NoError(t, err)
	assert.True(t, okResp.OK)
	assert.Equal(t, true, okResp.Payload["recorded"])

	badResp, err := s.handleRecommendation(context.Background(), &schema.AgentRequest{
		RequestID: "rid", ChannelID: "web", ReqMethod: schema.ReqMethodRecommendation,
		SessionID: strPtr("s-rpc"),
		Params:    marshalParams(t, map[string]any{"action": "prompt_suggestion_event", "session_id": "s-rpc", "event_type": "shown", "suggestion_id": "nope"}),
	})
	require.NoError(t, err)
	assert.False(t, badResp.OK)
	assert.Equal(t, "BAD_REQUEST", badResp.Payload["code"])
}

// TestHandleRecommendation_Event开关关 topic 开关关 → recorded:false/disabled。
func TestHandleRecommendation_Event开关关(t *testing.T) {
	s := newRecommendationTestServer(t, "")
	resp, err := s.handleRecommendation(context.Background(), &schema.AgentRequest{
		RequestID: "rid", ChannelID: "web", ReqMethod: schema.ReqMethodRecommendation,
		SessionID: strPtr("s-rpc"),
		Params:    marshalParams(t, map[string]any{"action": "prompt_suggestion_event", "session_id": "s-rpc", "event_type": "shown", "suggestion_id": "sg"}),
	})
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, false, resp.Payload["recorded"])
	assert.Equal(t, "disabled", resp.Payload["reason"])
}

// TestHandleRecommendation_未知action unknown → BAD_REQUEST。
func TestHandleRecommendation_未知action(t *testing.T) {
	s := newRecommendationTestServer(t, "")
	resp, err := s.handleRecommendation(context.Background(), &schema.AgentRequest{
		RequestID: "rid", ChannelID: "web", ReqMethod: schema.ReqMethodRecommendation,
		Params: marshalParams(t, map[string]any{"action": "status"}),
	})
	require.NoError(t, err)
	assert.False(t, resp.OK)
}

// TestPromptSuggestionCancel_无会话 空会话 → BAD_REQUEST。
func TestPromptSuggestionCancel_无会话(t *testing.T) {
	s := newRecommendationTestServer(t, "")
	resp, err := s.handlePromptSuggestionCancel(context.Background(), &schema.AgentRequest{
		RequestID: "rid", ChannelID: "web", ReqMethod: schema.ReqMethodPromptSuggestionCancel,
	})
	require.NoError(t, err)
	assert.False(t, resp.OK)
}

// TestPromptSuggestionCancel_正常 有会话 → cancelled（无在飞为 false）。
func TestPromptSuggestionCancel_正常(t *testing.T) {
	s := newRecommendationTestServer(t, "")
	resp, err := s.handlePromptSuggestionCancel(context.Background(), &schema.AgentRequest{
		RequestID: "rid", ChannelID: "web", ReqMethod: schema.ReqMethodPromptSuggestionCancel,
		SessionID: strPtr("s-cancel"),
	})
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, false, resp.Payload["cancelled"])
}
