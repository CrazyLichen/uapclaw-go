package recommendation

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// capturingLLM 记录重放收到的消息，并按脚本返回回答。
type capturingLLM struct {
	// mu 保护以下字段（重放在 goroutine 里执行）
	mu sync.Mutex
	// captured 每次 Invoke 收到的消息列表
	captured [][]llmschema.BaseMessage
	// replyText 返回的回答正文
	replyText string
	// blockUntilCtxCancel 为真时 Invoke 阻塞至 ctx 取消（测 cancelled 事件路径）
	blockUntilCtxCancel bool
}

// Invoke 记录入参并返回一条回答。
func (c *capturingLLM) Invoke(
	ctx context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	c.mu.Lock()
	c.captured = append(c.captured, messages.Messages())
	c.mu.Unlock()
	if c.blockUntilCtxCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return llmschema.NewAssistantMessage(c.replyText), nil
}

// Stream 不参与本测例。
func (c *capturingLLM) Stream(
	context.Context, model_clients.MessagesParam, ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}

// GenerateImage 不参与本测例。
func (c *capturingLLM) GenerateImage(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}

// GenerateSpeech 不参与本测例。
func (c *capturingLLM) GenerateSpeech(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

// GenerateVideo 不参与本测例。
func (c *capturingLLM) GenerateVideo(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

// TranscribeAudio 不参与本测例。
func (c *capturingLLM) TranscribeAudio(
	context.Context, string, ...llmschema.TranscribeAudioOption,
) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}

// Release 不参与本测例。
func (c *capturingLLM) Release(context.Context, ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持释放。
func (c *capturingLLM) SupportsKVCacheRelease() bool { return false }

// SupportsKVCacheAffinity 不支持亲和。
func (c *capturingLLM) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 返回空参数。
func (c *capturingLLM) BuildKVCacheAffinityInvokeKwargs(
	any, string, string, int,
) map[string]any {
	return map[string]any{}
}

// capturedCount 返回已捕获的调用次数。
func (c *capturingLLM) capturedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.captured)
}

// capturedAt 返回第 index 次调用的消息列表。
func (c *capturingLLM) capturedAt(index int) []llmschema.BaseMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= len(c.captured) {
		return nil
	}
	return c.captured[index]
}

// capturingPusher 记录推帧事件。
type capturingPusher struct {
	// events 收到的事件列表
	events []SuggestionReadyEvent
}

// PushReady 记录一次推送。
func (p *capturingPusher) PushReady(_ context.Context, event SuggestionReadyEvent) {
	p.events = append(p.events, event)
}

var _ model_clients.BaseModelClient = (*capturingLLM)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// minimalCall 构造一个最小可重放的主调用。
func minimalCall(client model_clients.BaseModelClient) *MainCall {
	return &MainCall{
		LLM:           client,
		Model:         "m",
		Messages:      []llmschema.BaseMessage{llmschema.NewUserMessage("hi")},
		Answer:        &llmschema.AssistantMessage{},
		MaxIterations: 1,
		Parameters:    map[string]any{},
	}
}

// TestRunFork_前缀逐字一致 重放 = 原 messages + 原 answer + 一句 SUGGESTION_PROMPT。
// Python: prompt_suggestion.py:459 messages = [*call.messages, call.answer, UserMessage(...)]
func TestRunFork_前缀逐字一致(t *testing.T) {
	resetWorkspaceForTest(t)

	client := &capturingLLM{replyText: "继续下一步的实现"}
	call := minimalCall(client)
	f := newEnabledForks()
	f.runFork(context.Background(), "s1", "req-1", call, ConsentEpoch(f.config), nil)

	require.Equal(t, 1, client.capturedCount())
	sent := client.capturedAt(0)
	require.Len(t, sent, 3, "原 messages(1) + 原 answer(1) + SUGGESTION_PROMPT(1)")
	assert.Equal(t, "hi", sent[0].GetContent().Text(), "父消息必须原样在前")
	assert.True(t, strings.Contains(sent[2].GetContent().Text(), "app-generated request"),
		"追加的必须是上游原文，禁止本地化")
}

// TestRunFork_未注入pusher时candidate只落库不推 推送通道未接线则不推，账本仍可用。
func TestRunFork_未注入pusher时candidate只落库不推(t *testing.T) {
	resetWorkspaceForTest(t)
	client := &capturingLLM{replyText: "继续下一步的实现"}
	f := newEnabledForks()
	assert.NotPanics(t, func() {
		f.runFork(context.Background(), "s1", "req-1", minimalCall(client), ConsentEpoch(f.config), nil)
	})
	assert.Equal(t, 1, client.capturedCount(), "重放仍然发生")
	// candidate 已落账本
	store, err := NewTopicStarterStore(filepath.Join(workspaceDirForTest(), ConsentDBName))
	require.NoError(t, err)
	defer store.Close()
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_generation", Limit: 5})
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "candidate", payload["outcome"])
}

// TestRunFork_推荐关闭即退出 对齐 :466 if consent_epoch() != epoch: return
func TestRunFork_推荐关闭即退出(t *testing.T) {
	resetWorkspaceForTest(t)

	client := &capturingLLM{replyText: "继续下一步的实现"}
	f := newEnabledForks()
	// 传一个与当前 epoch 不同的值，模拟「跑起来后同意被撤销」
	f.runFork(context.Background(), "s1", "req-1", minimalCall(client), ConsentEpochValue{Value: 12345, Active: true}, nil)
	assert.Equal(t, 0, client.capturedCount(), "epoch 不符不得发起重放")
}

// TestRunFork_模型返回null不落库 对齐 :498 select_suggestions 返回空即记账 suppressed/filtered。
func TestRunFork_模型返回null不落库(t *testing.T) {
	resetWorkspaceForTest(t)

	client := &capturingLLM{replyText: "null"}
	f := newEnabledForks()
	f.runFork(context.Background(), "s1", "req-1", minimalCall(client), ConsentEpoch(f.config), nil)
	store, err := NewTopicStarterStore(filepath.Join(workspaceDirForTest(), ConsentDBName))
	require.NoError(t, err)
	defer store.Close()
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_generation", Limit: 5})
	require.Len(t, rows, 1, "无建议也记账（outcome != candidate）")
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.NotEqual(t, "candidate", payload["outcome"])
}

// waitForInFlightZero 轮询等待在飞任务清零。
func waitForInFlightZero(t *testing.T, f *PromptSuggestionForks, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for f.InFlightCount() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("在飞任务超时未清零")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ledgerRows 打开账本库并返回 generation 事件行。
func ledgerRows(t *testing.T) []map[string]any {
	t.Helper()
	store, err := NewTopicStarterStore(filepath.Join(workspaceDirForTest(), ConsentDBName))
	require.NoError(t, err)
	defer store.Close()
	rows, err := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_generation", Limit: 5})
	require.NoError(t, err)
	return rows
}

// TestTurnEnded_抑制路径落库 status=cancelled → suppressed(aborted) 落库。
func TestTurnEnded_抑制路径落库(t *testing.T) {
	f := newLedgerTestForks(t)
	f.RequestStarted("s-sup", true)
	f.SawMainCall("s-sup", minimalCall(&capturingLLM{replyText: "x"}))
	f.TurnEnded("s-sup", "r1", TurnEndOptions{Status: "cancelled"})

	rows := ledgerRows(t)
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "suppressed", payload["outcome"])
	assert.Equal(t, "aborted", payload["reason"])
}

// TestRunFork_candidate推送成功 成功轮 → candidate 落库 + 等 completion + 推送。
func TestRunFork_candidate推送成功(t *testing.T) {
	f := newLedgerTestForks(t)
	pusher := &capturingPusher{}
	f.SetSuggestionReadyPusher(pusher)
	f.RequestStarted("s-ok", true)
	f.SawMainCall("s-ok", minimalCall(&capturingLLM{replyText: "试试 git push"}))
	completionRow(t, "s-ok", "r1") // 真实完成行落盘（StageDelivery 保鲜闸要读它）
	f.TurnEnded("s-ok", "r1", TurnEndOptions{Status: "success"})

	waitForInFlightZero(t, f, 5*time.Second)
	rows := ledgerRows(t)
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "candidate", payload["outcome"])
	assert.Equal(t, "试试 git push", payload["text"])
	require.Len(t, pusher.events, 1)
	assert.Equal(t, "试试 git push", pusher.events[0].Text)
}

// TestRunFork_等completion后才推 完成行未落盘 → 不推；落盘后推。
func TestRunFork_等completion后才推(t *testing.T) {
	f := newLedgerTestForks(t)
	pusher := &capturingPusher{}
	f.SetSuggestionReadyPusher(pusher)
	f.RequestStarted("s-wait", true)
	f.SawMainCall("s-wait", minimalCall(&capturingLLM{replyText: "ok"}))
	completionRow(t, "s-wait", "r1")
	// 回执延迟 close：fork 必须等到 close 才推（对齐 asyncio.shield 等完成行落盘）
	receipt := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		close(receipt)
	}()
	f.TurnEnded("s-wait", "r1", TurnEndOptions{Status: "success", Completion: receipt})
	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, pusher.events, "回执未关闭不得推送")
	waitForInFlightZero(t, f, 5*time.Second)
	require.Len(t, pusher.events, 1)
}

// TestRunFork_cancelled落库 ctx 取消 → cancelled 事件落库。
func TestRunFork_cancelled落库(t *testing.T) {
	f := newLedgerTestForks(t)
	f.RequestStarted("s-cancel", true)
	f.SawMainCall("s-cancel", minimalCall(&capturingLLM{
		replyText:           "x",
		blockUntilCtxCancel: true,
	}))
	f.TurnEnded("s-cancel", "r1", TurnEndOptions{Status: "success"})
	f.CancelSuggestion("s-cancel")
	// Cancel 先删任务表，InFlightCount 提前归零；改为轮询账本行（cancelled 记账在 goroutine 内）
	var rows []map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows = ledgerRows(t)
		if len(rows) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "cancelled", payload["outcome"])
}

// TestRunFork_失败落库 LLM 报错 → failed 事件落库（reason 为错误类型名）。
func TestRunFork_失败落库(t *testing.T) {
	f := newLedgerTestForks(t)
	f.RequestStarted("s-fail", true)
	f.SawMainCall("s-fail", minimalCall(&failingLLM{}))
	f.TurnEnded("s-fail", "r1", TurnEndOptions{Status: "success"})
	waitForInFlightZero(t, f, 5*time.Second)
	rows := ledgerRows(t)
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "failed", payload["outcome"])
	assert.Equal(t, "*recommendation.failingLLMError", payload["reason"])
}

// failingLLM 每次调用返回固定错误的替身。
type failingLLM struct{}

func (c *failingLLM) Invoke(
	context.Context, model_clients.MessagesParam, ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	return nil, &failingLLMError{}
}

func (c *failingLLM) Stream(
	context.Context, model_clients.MessagesParam, ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}

func (c *failingLLM) GenerateImage(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}

func (c *failingLLM) GenerateSpeech(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

func (c *failingLLM) GenerateVideo(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

func (c *failingLLM) TranscribeAudio(
	context.Context, string, ...llmschema.TranscribeAudioOption,
) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}

func (c *failingLLM) Release(context.Context, ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

func (c *failingLLM) SupportsKVCacheRelease() bool                        { return false }
func (c *failingLLM) SupportsKVCacheAffinity() bool                       { return false }
func (c *failingLLM) BuildKVCacheAffinityInvokeKwargs(any, string, string, int) map[string]any {
	return map[string]any{}
}

// failingLLMError 固定错误类型（reason 断言用其类型名）。
type failingLLMError struct{}

func (e *failingLLMError) Error() string { return "boom" }
