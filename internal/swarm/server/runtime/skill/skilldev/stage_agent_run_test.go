package skilldev

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// ──────────────────────────── 结构体 ────────────────────────────

// runMockClient 阶段执行测试用模型客户端。
type runMockClient struct {
	// reply 固定输出
	reply string
}

// Invoke 返回固定输出。
func (m *runMockClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(m.reply), nil
}

// Stream 未实现。
func (m *runMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateImage 未实现。
func (m *runMockClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateSpeech 未实现。
func (m *runMockClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateVideo 未实现。
func (m *runMockClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// TranscribeAudio 未实现。
func (m *runMockClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Release 无需释放。
func (m *runMockClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持。
func (m *runMockClient) SupportsKVCacheRelease() bool { return false }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRunStageAgent_成功调用 阶段 Agent 能完成一次对话并返回文本。
func TestRunStageAgent_成功调用(t *testing.T) {
	ctx := context.Background()
	sctx := newRunTestContext(t, "任务输出内容")

	text, err := sctx.RunStageAgent(ctx, "plan", "你是规划器", nil, 2, "帮我规划")
	if err != nil {
		t.Fatalf("RunStageAgent 失败: %v", err)
	}
	if text == "" || text == "{}" {
		t.Fatalf("应返回模型文本，实际 %q", text)
	}
}

// TestRunStageAgent_配置错误 deps 缺模型配置时返回错误。
func TestRunStageAgent_配置错误(t *testing.T) {
	sctx := newRunTestContext(t, "x")
	sctx.Deps.ModelClientConfig = nil

	if _, err := sctx.RunStageAgent(context.Background(), "plan", "p", nil, 2, "q"); err == nil {
		t.Fatal("期望配置错误，实际 nil")
	}
}

// TestInvokeStageAgent_复用Agent 同一 Agent 可连续执行两次对话。
func TestInvokeStageAgent_复用Agent(t *testing.T) {
	ctx := context.Background()
	sctx := newRunTestContext(t, "reply")

	agent, err := sctx.CreateStageAgent("generate", "你是生成器", nil, 2)
	if err != nil {
		t.Fatalf("CreateStageAgent 失败: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, invokeErr := sctx.InvokeStageAgent(ctx, "generate", agent, "写文件"); invokeErr != nil {
			t.Fatalf("第 %d 次调用失败: %v", i+1, invokeErr)
		}
	}
}

// TestExtractStageOutput 输出提取：已知键 / 空已知键 / 未知键回退格式化。
func TestExtractStageOutput(t *testing.T) {
	if got := extractStageOutput(map[string]any{"output": "文本"}); got != "文本" {
		t.Errorf("应取 output，实际 %q", got)
	}
	if got := extractStageOutput(map[string]any{"response": "响应"}); got != "响应" {
		t.Errorf("应取 response，实际 %q", got)
	}
	if got := extractStageOutput(map[string]any{"output": ""}); got != "" {
		t.Errorf("空 output 应返回空串，实际 %q", got)
	}
	if got := extractStageOutput(map[string]any{"output": 123}); got != "" {
		t.Errorf("非字符串 output 应返回空串，实际 %q", got)
	}
	if got := extractStageOutput(map[string]any{"other": "值"}); got != "map[other:值]" {
		t.Errorf("未知键应回退格式化，实际 %q", got)
	}
}

// TestStripCodeFences 围栏剥离各分支。
func TestStripCodeFences(t *testing.T) {
	cases := map[string]string{
		"plain":              "plain",
		"```\nbody\n```":     "body",
		"```go\nx := 1\n```": "x := 1",
		"```":                "```",
	}
	for in, want := range cases {
		if got := StripCodeFences(in); got != want {
			t.Errorf("StripCodeFences(%q) = %q, want %q", in, got, want)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRunTestContext 构造带 mock 模型的阶段上下文。
func newRunTestContext(t *testing.T, reply string) *SkillDevContext {
	t.Helper()

	provider := fmt.Sprintf("run_stage_unit_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return &runMockClient{reply: reply}, nil
		})
	t.Cleanup(func() { _ = model_clients.GetClientRegistry().Unregister(provider, "llm") })

	deps := &SkillDevDeps{
		ModelName: "mock-model",
		ModelClientConfig: map[string]any{
			"client_provider": provider,
			"client_id":       provider + "_id",
			"api_key":         "k",
			"api_base":        "http://127.0.0.1:1",
		},
		SysOpConfig: sysop.NewSysOperationCard(
			sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
		),
	}
	return NewSkillDevContext("run-task", deps, NewSkillDevState("run-task"), t.TempDir(), make(chan SkillDevEvent, 8))
}

// TestInvokeStageAgent_调用失败 模型不可用时返回错误。
func TestInvokeStageAgent_调用失败(t *testing.T) {
	sctx := newRunTestContext(t, "x")
	agent, err := sctx.CreateStageAgent("plan", "p", nil, 2)
	if err != nil {
		t.Fatalf("CreateStageAgent 失败: %v", err)
	}
	agent.SetLLM(nil)

	if _, err := sctx.InvokeStageAgent(context.Background(), "plan", agent, "q"); err == nil {
		t.Fatal("期望调用失败错误，实际 nil")
	}
}

// TestStageResultText 阶段返回 result_type=error 时必须上抛（不能把错误文案当内容）
func TestStageResultText(t *testing.T) {
	t.Run("error 且带文案", func(t *testing.T) {
		text, err := stageResultText(map[string]any{
			"output":      "模型未返回有效内容（空响应），请重试或检查上下文。",
			"result_type": "error",
		}, "generate")
		if err == nil {
			t.Fatal("result_type=error 应返回错误")
		}
		if text != "" {
			t.Fatalf("出错时不应返回文本，实际 %q", text)
		}
		if !strings.Contains(err.Error(), "generate") {
			t.Fatalf("错误应包含阶段名，实际 %v", err)
		}
	})

	t.Run("error 且无文案", func(t *testing.T) {
		if _, err := stageResultText(map[string]any{"result_type": "error"}, "evaluate"); err == nil {
			t.Fatal("无文案的 error 结果也应返回错误")
		}
	})

	t.Run("answer 正常取文本", func(t *testing.T) {
		text, err := stageResultText(map[string]any{"output": "内容", "result_type": "answer"}, "generate")
		if err != nil {
			t.Fatalf("answer 不应报错: %v", err)
		}
		if text != "内容" {
			t.Fatalf("应取 output，实际 %q", text)
		}
	})

	t.Run("无 result_type 兼容旧行为", func(t *testing.T) {
		text, err := stageResultText(map[string]any{"output": "内容"}, "generate")
		if err != nil || text != "内容" {
			t.Fatalf("无 result_type 时应原样返回，实际 (%q, %v)", text, err)
		}
	})
}

// SupportsKVCacheAffinity 测试替身：不支持 KV 亲和。
func (m *runMockClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 测试替身：返回空亲和参数。
func (m *runMockClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}
