package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// roundModelClient 单轮固定文本的假模型客户端。
// 只实现 LLM 对话两方法，其余能力返回不支持错误（与真实非多模态客户端一致）。
// 注册进 ClientRegistry 后由 llm.NewModel 取用，使 RunOneRound 能在**单测**里
// 走完 SubmitRound → executor → WaitRoundCompletion → 写流 → 边界帧的全链路。
type roundModelClient struct{}

// Invoke 返回固定助手消息
func (c *roundModelClient) Invoke(context.Context, model_clients.MessagesParam,
	...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return &llmschema.AssistantMessage{
		DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("已完成")},
	}, nil
}

// Stream 返回单帧内容后关闭（ReAct 视为一次完整响应）
func (c *roundModelClient) Stream(context.Context, model_clients.MessagesParam,
	...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	ch <- llmschema.NewAssistantMessageChunk("已完成")
	close(ch)
	return ch, nil
}

// GenerateImage 不支持
func (c *roundModelClient) GenerateImage(context.Context, []*llmschema.UserMessage,
	...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("roundModelClient 不支持 GenerateImage")
}

// GenerateSpeech 不支持
func (c *roundModelClient) GenerateSpeech(context.Context, []*llmschema.UserMessage,
	...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("roundModelClient 不支持 GenerateSpeech")
}

// GenerateVideo 不支持
func (c *roundModelClient) GenerateVideo(context.Context, []*llmschema.UserMessage,
	...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("roundModelClient 不支持 GenerateVideo")
}

// TranscribeAudio 不支持
func (c *roundModelClient) TranscribeAudio(context.Context, string,
	...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("roundModelClient 不支持 TranscribeAudio")
}

// Release 无缓存可释放
func (c *roundModelClient) Release(context.Context, ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持
func (c *roundModelClient) SupportsKVCacheRelease() bool { return false }

// newRoundModel 注册假客户端并构造 Model。
func newRoundModel(t *testing.T) *llm.Model {
	t.Helper()
	provider := fmt.Sprintf("mock_round_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(*llmschema.ModelRequestConfig, *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return &roundModelClient{}, nil
		})
	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: provider, ClientID: provider + "_id"},
		&llmschema.ModelRequestConfig{ModelName: "mock-round-model"},
	)
	require.NoError(t, err)
	return model
}

// TestRunOneRound_端到端一轮 用假模型跑完整一轮，覆盖 RunOneRound 的真实分支。
// 与 tests/integration 的 TestRoundEndToEnd 互补：那条走 MockLLM 的集成链路，
// 这条在单测口径下把 RunOneRound / executeRound 真分支计入改动行覆盖率。
func TestRunOneRound_端到端一轮(t *testing.T) {
	ctx := context.Background()

	card := agentschema.NewAgentCard(agentschema.WithAgentName("round-e2e-agent"))
	agent, err := CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:  card,
		Model: newRoundModel(t),
	})
	require.NoError(t, err, "创建 DeepAgent 失败")

	sess := session.NewSession()
	require.NoError(t, sess.PreRun(ctx), "Session PreRun 失败")
	require.NoError(t, agent.Start(ctx, sess), "Start 失败")
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	out, attachErr := agent.AttachOutput(ctx)
	require.NoError(t, attachErr)
	require.NotNil(t, out, "应拿到唯一输出租约")

	require.NoError(t, agent.SendInput(ctx, hschema.SendInputRequest{
		RequestID: "req-round", Inputs: map[string]any{"query": "你好"},
	}))

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	frames := 0
	for {
		_, ok := agent.NextOutput(readCtx, out.Lease())
		if !ok {
			break
		}
		frames++
		if frames > 500 {
			t.Fatal("输出帧数异常：超过 500 帧仍未结束")
		}
	}

	assert.Greater(t, frames, 0, "一轮执行必须至少产出一帧")
	assert.Equal(t, hschema.InteractionPhaseIdle, agent.Phase(), "轮次结束后应回到 idle")
}

// TestRunOneRound_goal轮注入run上下文 验证 goal 轮的 inputs["run"] 组装
// Python: run_one_round 的 kind == "goal" 分支（L3773-3777）
func TestRunOneRound_goal轮注入run上下文(t *testing.T) {
	ctx := context.Background()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("round-goal-agent"))
	agent, err := CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:  card,
		Model: newRoundModel(t),
	})
	require.NoError(t, err)

	sess := session.NewSession()
	require.NoError(t, sess.PreRun(ctx))
	require.NoError(t, agent.Start(ctx, sess))
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	out, attachErr := agent.AttachOutput(ctx)
	require.NoError(t, attachErr)
	require.NotNil(t, out)

	work := hschema.NewGoalWorkItem(map[string]any{"query": "目标轮"},
		"goal-1", 2, sess.GetSessionID())
	outcome := agent.RunOneRound(ctx, work, "task-goal", sess)
	assert.Equal(t, "", outcome.ErrorCode, "goal 轮不应返回错误码")
}

// TestRunOneRound_打断恢复走单轮invoke 验证 resume 分支
// Python: run_one_round 的 is_resume_input 分支（L3788-3794）
func TestRunOneRound_打断恢复走单轮invoke(t *testing.T) {
	ctx := context.Background()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("round-resume-agent"))
	agent, err := CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:  card,
		Model: newRoundModel(t),
	})
	require.NoError(t, err)

	sess := session.NewSession()
	require.NoError(t, sess.PreRun(ctx))
	require.NoError(t, agent.Start(ctx, sess))
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	out, attachErr := agent.AttachOutput(ctx)
	require.NoError(t, attachErr)
	require.NotNil(t, out)

	interactive, err := interaction.NewInteractiveInput("继续")
	require.NoError(t, err)

	work := hschema.NewUserWorkItem("req-resume",
		map[string]any{"query": interactive}, false, false)
	outcome := agent.RunOneRound(ctx, work, "task-resume", sess)
	assert.Equal(t, "", outcome.ErrorCode, "打断恢复不应返回错误码")
}
