package recommendation

import (
	"context"
	"strings"
	"sync"
	"testing"

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
}

// Invoke 记录入参并返回一条回答。
func (c *capturingLLM) Invoke(
	_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.captured = append(c.captured, messages.Messages())
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

// capturingPublisher 记录落库结果。
type capturingPublisher struct {
	// results 收到的结果列表
	results []ForkResult
}

// PublishForkResult 记录一次落库。
func (p *capturingPublisher) PublishForkResult(_ context.Context, result ForkResult) {
	p.results = append(p.results, result)
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
	ResetConsentEpoch()
	defer ResetConsentEpoch()

	client := &capturingLLM{replyText: "继续下一步的实现"}
	call := minimalCall(client)
	f := newEnabledForks()
	f.runFork(context.Background(), "s1", "req-1", call, ConsentEpoch(f.config))

	require.Equal(t, 1, client.capturedCount())
	sent := client.capturedAt(0)
	require.Len(t, sent, 3, "原 messages(1) + 原 answer(1) + SUGGESTION_PROMPT(1)")
	assert.Equal(t, "hi", sent[0].GetContent().Text(), "父消息必须原样在前")
	assert.True(t, strings.Contains(sent[2].GetContent().Text(), "app-generated request"),
		"追加的必须是上游原文，禁止本地化")
}

// TestRunFork_未注入publisher时结果丢弃且无error 对齐 spec DEV-10。
func TestRunFork_未注入publisher时结果丢弃且无error(t *testing.T) {
	ResetConsentEpoch()
	defer ResetConsentEpoch()
	client := &capturingLLM{replyText: "继续下一步的实现"}
	f := newEnabledForks()
	assert.NotPanics(t, func() {
		f.runFork(context.Background(), "s1", "req-1", minimalCall(client), ConsentEpoch(f.config))
	})
	assert.Equal(t, 1, client.capturedCount(), "重放仍然发生")
}

// TestRunFork_注入publisher收到结果 为 10.7 铺路。
func TestRunFork_注入publisher收到结果(t *testing.T) {
	ResetConsentEpoch()
	defer ResetConsentEpoch()

	client := &capturingLLM{replyText: "继续下一步的实现"}
	publisher := &capturingPublisher{}
	pusher := &capturingPusher{}
	f := newEnabledForks()
	f.SetForkResultPublisher(publisher)
	f.SetSuggestionReadyPusher(pusher)

	f.runFork(context.Background(), "s1", "req-1", minimalCall(client), ConsentEpoch(f.config))

	require.Len(t, publisher.results, 1)
	assert.Equal(t, "s1", publisher.results[0].SessionID)
	assert.Equal(t, "req-1", publisher.results[0].RequestID)
	assert.Equal(t, "继续下一步的实现", publisher.results[0].Text)
	assert.NotEmpty(t, publisher.results[0].SuggestionID)
	require.Len(t, pusher.events, 1, "落库成功后推送 ready")
	assert.Equal(t, publisher.results[0].SuggestionID, pusher.events[0].SuggestionID)
}

// TestRunFork_推荐关闭即退出 对齐 :466 if consent_epoch() != epoch: return
func TestRunFork_推荐关闭即退出(t *testing.T) {
	ResetConsentEpoch()
	defer ResetConsentEpoch()

	client := &capturingLLM{replyText: "继续下一步的实现"}
	f := newEnabledForks()
	// 传一个与当前 epoch 不同的值，模拟「跑起来后同意被撤销」
	f.runFork(context.Background(), "s1", "req-1", minimalCall(client), 12345)
	assert.Equal(t, 0, client.capturedCount(), "epoch 不符不得发起重放")
}

// TestRunFork_模型返回null不落库 对齐 :498 select_suggestions 返回空即返回。
func TestRunFork_模型返回null不落库(t *testing.T) {
	ResetConsentEpoch()
	defer ResetConsentEpoch()

	client := &capturingLLM{replyText: "null"}
	publisher := &capturingPublisher{}
	f := newEnabledForks()
	f.SetForkResultPublisher(publisher)
	f.runFork(context.Background(), "s1", "req-1", minimalCall(client), ConsentEpoch(f.config))
	assert.Empty(t, publisher.results, "null 视为无建议")
}
