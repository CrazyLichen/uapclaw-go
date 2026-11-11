//go:build integration

// 建议账本全链路集成测试：成功轮 → 完成行落盘 → fork candidate 落库 →
// ready 推送 → 回读命中 → accepted 消费 → 再回读为空；以及过期请求不推送。
// 跨子系统：recommendation（harness 建议链路）+ server/session（history 完成行）+
// agentcore/foundation/llm（mockllm 模型替身真实 Invoke）。
package prompt_suggestion_ledger

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSuggestionLedgerSuite 建议账本全链路。
type PromptSuggestionLedgerSuite struct {
	isuite.BaseIntegrationSuite
}

// pushRecorderFunc 函数式 pusher 替身。
type pushRecorderFunc func(ctx context.Context, e recommendation.SuggestionReadyEvent)

// PushReady 记录一次推送。
func (f pushRecorderFunc) PushReady(ctx context.Context, e recommendation.SuggestionReadyEvent) {
	f(ctx, e)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPromptSuggestionLedgerSuite 运行建议账本集成测试。
func TestPromptSuggestionLedgerSuite(t *testing.T) {
	suite.Run(t, new(PromptSuggestionLedgerSuite))
}

// SetupTest 每用例临时工作区 + 开启推荐 + 干净推送记录。
func (s *PromptSuggestionLedgerSuite) SetupTest() {
	tmpDir := s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()
	s.T().Cleanup(path.ResetCache)
}

// TearDownTest 清理全局单例，避免后面的用例继承在飞任务。
func (s *PromptSuggestionLedgerSuite) TearDownTest() {
	recommendation.Forks.Shutdown()
	recommendation.Forks.SetConfig(nil)
	recommendation.Forks.SetSuggestionReadyPusher(nil)
}

// TestLedger 全链路：生成→落库→推送→回读→消费→再回读。
func (s *PromptSuggestionLedgerSuite) TestLedger() {
	var pushed []recommendation.SuggestionReadyEvent
	forks := recommendation.NewPromptSuggestionForks()
	forks.SetConfig(map[string]any{"recommendation": map[string]any{"prompt_enabled": true}})
	forks.SetSuggestionReadyPusher(pushRecorderFunc(func(ctx context.Context, e recommendation.SuggestionReadyEvent) {
		pushed = append(pushed, e)
	}))

	// 1. 成功轮：RequestStarted → SawMainCall → 真实完成行落盘 → TurnEnded
	forks.RequestStarted("s-it", true)
	forks.SawMainCall("s-it", s.minimalCall("试试 git push"))
	receipt, err := session.EnqueueHistoryRequestCompletion("s-it", "r-it", "success")
	s.Require().NoError(err)
	s.Require().NotNil(receipt)
	<-receipt
	forks.TurnEnded("s-it", "r-it", recommendation.TurnEndOptions{Status: "success"})
	s.waitInFlight(forks, 5*time.Second)

	// 2. candidate 落库 + ready 推送
	store := s.openStore()
	rows, qErr := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: "prompt_suggestion_generation", Limit: 5})
	s.Require().NoError(qErr)
	s.Require().Len(rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	s.Require().Equal("candidate", payload["outcome"])
	s.Require().Equal("试试 git push", payload["text"])
	s.Require().Len(pushed, 1)
	s.Require().Equal("试试 git push", pushed[0].Text)

	// 3. 回读命中
	got := recommendation.CurrentSuggestions(forks, store, "s-it")
	s.Require().Equal(true, got["enabled"])
	suggestions, _ := got["suggestions"].([]map[string]any)
	s.Require().Len(suggestions, 1)

	// 4. 消费后再回读为空
	suggestionID, _ := suggestions[0]["suggestion_id"].(string)
	recorded := recommendation.RecordPageOutcome(store, map[string]any{
		"session_id": "s-it", "suggestion_id": suggestionID, "event_type": "accepted",
		"method": "click", "time_to_accept_ms": 42,
	})
	s.Require().NotNil(recorded)
	s.Require().True(*recorded)
	got2 := recommendation.CurrentSuggestions(forks, store, "s-it")
	suggestions2, _ := got2["suggestions"].([]map[string]any)
	s.Require().Empty(suggestions2)
}

// TestLedger_staleRequest 旧请求的建议被判 stale 不推送。
func (s *PromptSuggestionLedgerSuite) TestLedger_staleRequest() {
	var pushed []recommendation.SuggestionReadyEvent
	forks := recommendation.NewPromptSuggestionForks()
	forks.SetConfig(map[string]any{"recommendation": map[string]any{"prompt_enabled": true}})
	forks.SetSuggestionReadyPusher(pushRecorderFunc(func(ctx context.Context, e recommendation.SuggestionReadyEvent) {
		pushed = append(pushed, e)
	}))

	// 请求 r1 出了 candidate，但会话最新完成行是 r2（已过期）
	forks.RequestStarted("s-stale", true)
	forks.SawMainCall("s-stale", s.minimalCall("提交刚才的修改"))
	r1Receipt, err := session.EnqueueHistoryRequestCompletion("s-stale", "r1", "success")
	s.Require().NoError(err)
	<-r1Receipt
	r2Receipt, err := session.EnqueueHistoryRequestCompletion("s-stale", "r2", "success")
	s.Require().NoError(err)
	<-r2Receipt
	forks.TurnEnded("s-stale", "r1", recommendation.TurnEndOptions{Status: "success"})
	s.waitInFlight(forks, 5*time.Second)

	s.Require().Empty(pushed, "过期请求不得推送")
	store := s.openStore()
	rows, qErr := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: "prompt_suggestion_delivery", Limit: 5})
	s.Require().NoError(qErr)
	s.Require().Len(rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	s.Require().Equal("suppressed", payload["status"])
	s.Require().Equal("stale_request", payload["reason"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// minimalCall 用集成 mockllm 造一个最小可重放的主调用。
func (s *PromptSuggestionLedgerSuite) minimalCall(reply string) *recommendation.MainCall {
	s.T().Helper()
	return &recommendation.MainCall{
		LLM:           s.mockClient(reply),
		Model:         "mock",
		Messages:      []llmschema.BaseMessage{llmschema.NewUserMessage("hi")},
		Answer:        &llmschema.AssistantMessage{},
		MaxIterations: 1,
		Parameters:    map[string]any{},
	}
}

// mockClient 固定文本回复的模型替身——复用集成测试现成的 MockModelClient
// （tests/integration/mockllm/mock_client.go，Invoke 与 BaseModelClient 一致）。
func (s *PromptSuggestionLedgerSuite) mockClient(reply string) model_clients.BaseModelClient {
	s.T().Helper()
	client := s.MockLLM
	client.SetResponses()
	client.AddTextResponse(reply)
	return client
}

// waitInFlight 轮询等待在飞任务清零。
func (s *PromptSuggestionLedgerSuite) waitInFlight(forks *recommendation.PromptSuggestionForks, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for forks.InFlightCount() > 0 {
		s.Require().Less(time.Now(), deadline, "在飞任务超时未清零")
		time.Sleep(20 * time.Millisecond)
	}
}

// openStore 打开工作区内账本库。
func (s *PromptSuggestionLedgerSuite) openStore() *recommendation.TopicStarterStore {
	store, err := recommendation.NewTopicStarterStore(
		filepath.Join(workspace.AgentWorkspaceDir(), recommendation.ConsentDBName))
	s.Require().NoError(err)
	return store
}
