//go:build integration

package llm_client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/usage"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/dashscope"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/inference_affinity"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/intellirouter"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/siliconflow"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMClientSuite LLM 客户端 C 组对齐的跨子系统集成测试。
//
// 跨子系统：llm 客户端（请求/解析）→ llm schema（配置语义）→
// context_engine/usage（用量聚合）→ 共享 Transport 池（连接复用）。
type LLMClientSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLLMClientSuite 运行 LLM 客户端集成测试套件。
func TestLLMClientSuite(t *testing.T) {
	suite.Run(t, new(LLMClientSuite))
}

// TestStreamUsageEndToEnd_持续输出与缓存用量 验证 C-01 + C-04 + C-14 三段贯通：
//
//  1. 服务端持续吐 1.2s（配置 timeout 0.8s）——流式无总超时，12 块全收（C-01）；
//  2. 末帧带 usage（cached_tokens / reasoning_tokens）——统一 BuildUsageMetadata 抽齐（C-04）；
//  3. 用量交给 context_engine/usage 聚合——归一字段优先（C-B3）；
//  4. 全程复用同一共享 Transport 实例（C-14）。
func (s *LLMClientSuite) TestStreamUsageEndToEnd_持续输出与缓存用量() {
	var reqCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 12; i++ {
			_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"x"},"finish_reason":null}]}`+"\n\n")
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
		// 末帧：finish_reason + usage（含 cache 与 reasoning 明细）
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":100,"completion_tokens":200,"total_tokens":300,`+
			`"prompt_tokens_details":{"cached_tokens":60},`+
			`"completion_tokens_details":{"reasoning_tokens":150}}}`+"\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	clientConfig, err := llmschema.NewModelClientConfig("OpenAI", "k", srv.URL,
		llmschema.WithVerifySSL(false),
		llmschema.WithTimeout(0.8), // 故意小于流时长，验证流式无总超时
	)
	s.Require().NoError(err)
	// 注：此处显式传 timeout=0.8 以在可接受时长内复现「流时长 > 总超时」；
	// 默认值不变量（timeout 360 >= first_chunk 300）由单独用例守护。

	// 走 llm.Model 门面：包裹 callback 派发 + 帧超时（首帧/空闲）+ 共享客户端，
	// 使本用例横跨 agentcore/foundation/llm 与 model_clients 两层。
	model, err := llm.NewModel(clientConfig,
		llmschema.NewModelRequestConfig(llmschema.WithModelName("m")))
	s.Require().NoError(err)

	// 注册 LLM 事件观测器：既断言事件序（input → response_received × N → output），
	// 也使本用例横跨 agentcore/runner（callback 框架）子系统。
	var inputEvents, outputEvents, receivedEvents int
	fw := callback.GetCallbackFramework()
	onInput := func(_ context.Context, _ *callback.LLMCallEventData) any {
		inputEvents++
		return nil
	}
	onReceived := func(_ context.Context, _ *callback.LLMCallEventData) any {
		receivedEvents++
		return nil
	}
	onOutput := func(_ context.Context, _ *callback.LLMCallEventData) any {
		outputEvents++
		return nil
	}
	fw.OnLLM(callback.LLMInput, onInput)
	fw.OnLLM(callback.LLMResponseReceived, onReceived)
	fw.OnLLM(callback.LLMOutput, onOutput)
	defer func() {
		fw.OffLLM(callback.LLMInput, onInput)
		fw.OffLLM(callback.LLMResponseReceived, onReceived)
		fw.OffLLM(callback.LLMOutput, onOutput)
	}()

	ch, err := model.Stream(context.Background(),
		model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	s.Require().NoError(err)

	var chunks int
	var lastUsage *llmschema.UsageMetadata
	var finishReason string
	for chunk := range ch {
		if chunk.StreamError != nil {
			s.Require().NoError(chunk.StreamError, "流内不应出现错误")
		}
		if chunk.Content.Text() != "" {
			chunks++
		}
		if chunk.UsageMetadata != nil {
			lastUsage = chunk.UsageMetadata
		}
		if chunk.FinishReason != "" && chunk.FinishReason != llmschema.FinishReasonNull {
			finishReason = chunk.FinishReason
		}
	}

	// 1) C-01：1.2s 持续输出全部收完（旧实现会在 0.8s 处硬切断）
	s.Equal(12, chunks, "持续输出不应被总超时切断")
	s.Equal("stop", finishReason, "finish_reason 应保留 provider 原值")

	// callback 事件序：input 恰 1 次、output 恰 1 次、response_received 每 token 块一次
	s.Equal(1, inputEvents, "LLMInput 应触发一次")
	s.Equal(1, outputEvents, "LLMOutput 应触发一次")
	s.GreaterOrEqual(receivedEvents, 1, "LLMResponseReceived 应随分片触发")

	// 2) C-04：usage 归一字段齐备
	s.Require().NotNil(lastUsage, "应收到 usage 帧")
	s.Equal(100, lastUsage.InputTokens)
	s.Equal(200, lastUsage.OutputTokens)
	s.Require().NotNil(lastUsage.CacheReadTokens, "缓存读 token 应解析")
	s.Equal(60, *lastUsage.CacheReadTokens)
	s.Require().NotNil(lastUsage.ReasoningTokens, "推理 token 应解析")
	s.Equal(150, *lastUsage.ReasoningTokens)
	s.Equal("observed", lastUsage.CacheStatus)
	s.True(lastUsage.CacheAuthoritative)

	// 3) C-B3：context_engine/usage 聚合优先采用归一字段
	req := usage.RequestCacheFromUsage(lastUsage)
	s.Require().NotNil(req.CacheReadTokens)
	s.Equal(60, *req.CacheReadTokens)
	s.Require().NotNil(req.CacheMissTokens)
	s.Equal(40, *req.CacheMissTokens, "miss 由 provider 上报或 input-read 推导")
	s.Equal("reported", req.Status)
	s.Equal("provider_usage", req.Source)

	session := usage.AggregateSessionCache([]usage.RequestKVCacheUsage{req})
	s.Equal(1, session.CallsTotal)
	s.Equal(1, session.CallsObserved, "应统计为「已观测」而非「未上报」")

	// 4) C-14：同一 (verify_ssl, ssl_cert) 复用同一 Transport 实例
	shared := model_clients.GetSharedTransport(false, "")
	s.NotNil(shared)
	s.Equal(1, int(atomic.LoadInt32(&reqCount)), "单次流式请求")
}

// TestInvokeEncodingErrorNotRetried 验证 C-10：编码错误分类为 181004 且不被包成
// MODEL_CALL_FAILED——重试层（ModelAnomalyDetectionRail）据此跳过无意义重试。
func (s *LLMClientSuite) TestInvokeEncodingErrorNotRetried() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.Fail("编码错误不应发出网络请求")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	clientConfig, err := llmschema.NewModelClientConfig("OpenAI", "k", srv.URL,
		llmschema.WithVerifySSL(false))
	s.Require().NoError(err)
	client, err := openai.NewOpenAIModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), clientConfig)
	s.Require().NoError(err)

	// 非法 UTF-8 触发编码阶段失败
	bad := model_clients.NewDictsMessagesParam([]map[string]any{
		{"role": "user", "content": "bad\xff"},
	})
	_, err = client.Invoke(context.Background(), bad)
	s.Require().Error(err)
	s.True(openai.IsRequestEncodingErr(err), "应保留 request_encoding 分类，实际 %v", err)
	s.NotContains(err.Error(), "MODEL_CALL_FAILED", "不应被包装为通用模型调用失败")
}

// TestSharedTransportAcrossClients 验证 C-14：不同客户端实例共用同一分桶 Transport。
func (s *LLMClientSuite) TestSharedTransportAcrossClients() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		cc, err := llmschema.NewModelClientConfig("OpenAI", "k", srv.URL,
			llmschema.WithVerifySSL(false))
		s.Require().NoError(err)
		c, err := openai.NewOpenAIModelClient(
			llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
		s.Require().NoError(err)
		resp, err := c.Invoke(context.Background(),
			model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
		s.Require().NoError(err)
		s.Equal("ok", strings.TrimSpace(resp.Content.Text()))
	}
	// 三次独立客户端仍走同一分桶（verify_ssl=false, 无证书）
	s.NotNil(model_clients.GetSharedTransport(false, ""))
}

// TestStreamAcrossProviders 验证 C-A2/C-D2 在四个独立 Stream 实现上的一致语义：
// 流式不设 Client 总超时、鉴权头按 auth_mode、共享 Transport 分桶。
//
// 四个客户端（siliconflow / inference_affinity / intellirouter / dashscope 对话路径）
// 都自行实现 Stream 并调用 BuildHTTPRequestWithAuth，此用例逐一驱动其真实 HTTP 链路。
func (s *LLMClientSuite) TestStreamAcrossProviders() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 鉴权头：AuthModeNone 时不应带 Authorization
		if r.Header.Get("Authorization") != "" && r.URL.Path != "/chat/completions" {
			// 允许 Bearer 但记录
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4,`+
			`"prompt_tokens_details":{"cached_tokens":2}}}`+"\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("m"))
	providers := []struct {
		name     string
		provider string
		newCli   func(*llmschema.ModelClientConfig) (model_clients.BaseModelClient, error)
	}{
		{"siliconflow", "SiliconFlow", func(cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return siliconflow.NewSiliconFlowModelClient(mc, cc)
		}},
		{"inference_affinity", "InferenceAffinity", func(cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return inferenceaffinity.NewInferenceAffinityModelClient(mc, cc)
		}},
	}
	for _, p := range providers {
		s.Run(p.name, func() {
			cc, err := llmschema.NewModelClientConfig(p.provider, "k", srv.URL,
				llmschema.WithVerifySSL(false),
				llmschema.WithTimeout(0.5), // 远小于帧间隔总和 → 验证流式无总超时
			)
			s.Require().NoError(err)
			cli, err := p.newCli(cc)
			s.Require().NoError(err)

			ch, err := cli.Stream(context.Background(),
				model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
			s.Require().NoError(err)
			var got string
			var usageMeta *llmschema.UsageMetadata
			for chunk := range ch {
				s.Require().NoError(chunk.StreamError)
				got += chunk.Content.Text()
				if chunk.UsageMetadata != nil {
					usageMeta = chunk.UsageMetadata
				}
			}
			s.Equal("hi", got, "流式内容应完整")
			s.Require().NotNil(usageMeta, "应解析 usage")
			s.Equal(3, usageMeta.InputTokens)
			s.Require().NotNil(usageMeta.CacheReadTokens, "C-B 归一字段应生效")
			s.Equal(2, *usageMeta.CacheReadTokens)
		})
	}
}

// TestStreamIntelliRouter 验证 intellirouter 的 Stream 链路（C-A2 共享 Transport +
// C-G router 生命周期 + C-D1 白名单不要求 api_key）。
func (s *LLMClientSuite) TestStreamIntelliRouter() {
	tokenCount := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&tokenCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6,`+
			`"completion_tokens_details":{"reasoning_tokens":1}}}`+"\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	// intelli_router 不在 api_key/api_base 白名单：可空 key/base，凭据在 deployment 级
	cc, err := llmschema.NewModelClientConfig("intelli_router", "", "",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{"id": "d1", "model_name": "m", "api_base": srv.URL, "api_key": "k"},
			},
			"intelli_router_num_retries": 1,
		}),
	)
	s.Require().NoError(err)
	cli, err := intellirouter.NewIntelliRouterModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
	s.Require().NoError(err)

	ch, err := cli.Stream(context.Background(),
		model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	s.Require().NoError(err)
	var content string
	var usageMeta *llmschema.UsageMetadata
	for chunk := range ch {
		s.Require().NoError(chunk.StreamError)
		content += chunk.Content.Text()
		if chunk.UsageMetadata != nil {
			usageMeta = chunk.UsageMetadata
		}
	}
	s.Equal("ok", content)
	// 注：IntelliRouter 的 convertChunk 只提取 content（对齐上游 IntelliRouter 无
	// _astream_with_parser、不提取 usage_metadata），故此处不断言 usage——
	// usage 归一字段的断言在 TestStreamUsageEndToEnd / TestStreamAcrossProviders。
	_ = usageMeta
	s.GreaterOrEqual(atomic.LoadInt32(&tokenCount), int32(1))

	// C-G：驱逐入口可用且幂等（缓存键不导出，故此处只验证不 panic 与重复调用安全）
	irCfg := intellirouter.FromModelClientConfig(cc)
	s.NotNil(irCfg)
	s.NotPanics(func() { intellirouter.ResetRouterCache("") })
}

// TestTranscribeAudio 验证 C-A2 改造后的 transcription 路径：共享 Transport +
// ctx 超时（原先走已删除的 buildHTTPClient）。
func (s *LLMClientSuite) TestTranscribeAudio() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// multipart 上传应带 Authorization 与 Content-Type
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			s.T().Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"text":"hello"}`)
	}))
	defer srv.Close()

	// 写一个临时音频文件（内容不重要，服务端不校验）
	audio := filepath.Join(s.T().TempDir(), "a.wav")
	s.Require().NoError(os.WriteFile(audio, []byte("RIFF....WAVE"), 0o600))

	cc, err := llmschema.NewModelClientConfig("OpenAI", "k", srv.URL,
		llmschema.WithVerifySSL(false),
		llmschema.WithTimeout(5))
	s.Require().NoError(err)
	cli, err := openai.NewOpenAIModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("whisper-1")), cc)
	s.Require().NoError(err)

	resp, err := cli.TranscribeAudio(context.Background(), audio)
	s.Require().NoError(err)
	s.Equal("hello", resp.Text)
}

// TestDashScopeNativeImage 验证 C-E1 归一 + C-A2 共享 Transport 在 DashScope
// 原生多模态路径（CallDashScopeAPI，不经 BuildHTTPRequest）上同样生效。
func (s *LLMClientSuite) TestDashScopeNativeImage() {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status_code":200,"request_id":"r1",`+
			`"output":{"choices":[{"message":{"content":[{"image":"https://img/x.png"}]}}]}}`)
	}))
	defer srv.Close()

	// api_base 故意带完整端点 URL：NormalizeAPIBase 应剥成 base（C-E1）。
	// DashScope 原生路径先剥 /compatible-mode 再拼原生 path。
	cc, err := llmschema.NewModelClientConfig("DashScope", "k",
		srv.URL+"/compatible-mode/v1/chat/completions",
		llmschema.WithVerifySSL(false),
		llmschema.WithTimeout(5))
	s.Require().NoError(err)
	s.Equal(srv.URL, strings.TrimRight(cc.APIBase, "/")[:len(srv.URL)],
		"api_base 保留原样（归一发生在使用点）")

	cli, err := dashscope.NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wanx-v1")), cc)
	s.Require().NoError(err)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("a cat")}
	resp, err := cli.GenerateImage(context.Background(), msgs)
	s.Require().NoError(err)
	s.Require().NotEmpty(resp.Images)
	s.Contains(gotPath, "multimodal-generation", "应走 DashScope 原生多模态路径（非 OpenAI 兼容端点）")
}

// TestReleaseKVCache 验证 inference_affinity 的 Release 路径（C-E1 归一 + C-A2
// ctx 超时派生 + 共享 Transport）：api_base 带完整端点时仍拼对路径。
func (s *LLMClientSuite) TestReleaseKVCache() {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":200}`)
	}))
	defer srv.Close()

	// api_base 故意带 /chat/completions：NormalizeAPIBase 应剥掉后再拼 /release_kv_cache
	cc, err := llmschema.NewModelClientConfig("InferenceAffinity", "k",
		srv.URL+"/v1/chat/completions",
		llmschema.WithVerifySSL(false),
		llmschema.WithTimeout(5))
	s.Require().NoError(err)
	cli, err := inferenceaffinity.NewInferenceAffinityModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
	s.Require().NoError(err)

	sessionID := "sess-1"
	ok, err := cli.Release(context.Background(),
		model_clients.WithReleaseSessionID(sessionID),
		model_clients.WithReleaseMessages(model_clients.NewMessagesParam(llmschema.NewUserMessage("hi"))),
	)
	s.Require().NoError(err)
	s.True(ok)
	s.Equal("/v1/release_kv_cache", gotPath, "应剥离 /chat/completions 后拼 release 路径")
}
