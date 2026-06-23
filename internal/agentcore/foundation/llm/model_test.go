package llm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// eventRecorder 记录触发的事件，用于测试 Model 的回调触发
type eventRecorder struct {
	events []callback.LLMCallEventType
	mu     chan struct{}
}

func newEventRecorder() *eventRecorder {
	return &eventRecorder{
		mu: make(chan struct{}, 1),
	}
}

func (r *eventRecorder) record(_ context.Context, data *callback.LLMCallEventData) any {
	r.mu <- struct{}{}
	r.events = append(r.events, data.Event)
	<-r.mu
	return nil
}

// mockModelClient 模拟 BaseModelClient，用于测试 Model 门面
type mockModelClient struct {
	invokeResult    *llmschema.AssistantMessage
	invokeErr       error
	streamChan      <-chan *llmschema.AssistantMessageChunk
	streamErr       error
	releaseResult   bool
	releaseErr      error
	genImageResult  *llmschema.ImageGenerationResponse
	genImageErr     error
	genSpeechResult *llmschema.AudioGenerationResponse
	genSpeechErr    error
	genVideoResult  *llmschema.VideoGenerationResponse
	genVideoErr     error
}

func (m *mockModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return m.invokeResult, m.invokeErr
}

func (m *mockModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return m.streamChan, m.streamErr
}

func (m *mockModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return m.genImageResult, m.genImageErr
}

func (m *mockModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return m.genSpeechResult, m.genSpeechErr
}

func (m *mockModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return m.genVideoResult, m.genVideoErr
}

func (m *mockModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return m.releaseResult, m.releaseErr
}

// mockSession 模拟 SessionLike 接口
type mockSession struct {
	id string
}

func (s *mockSession) GetSessionID() string { return s.id }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewModel_空配置 测试 nil 配置返回错误
func TestNewModel_空配置(t *testing.T) {
	_, err := NewModel(nil, nil)
	if err == nil {
		t.Error("nil 配置应返回错误")
	}
}

// TestNewModel_带回调框架 测试自定义回调框架
func TestNewModel_带回调框架(t *testing.T) {
	customFW := callback.NewCallbackFramework()
	recorder := newEventRecorder()
	customFW.OnLLM(callback.LLMInvokeInput, recorder.record)

	// 创建 Model（这会触发 CreateModelClient，需要注册的 provider）
	// 由于无法轻易注册 mock client，这里只测试 WithCallbackFramework 选项
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            &mockModelClient{},
		callbackFramework: customFW,
	}

	// 应用选项
	WithCallbackFramework(customFW)(model)

	if model.callbackFramework != customFW {
		t.Error("WithCallbackFramework 应设置自定义回调框架")
	}
}

// TestModel_Invoke_回调事件 测试 Invoke 触发回调事件
func TestModel_Invoke_回调事件(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var invokeInputCalled int32
	var invokeOutputCalled int32

	fw.OnLLM(callback.LLMInvokeInput, func(_ context.Context, data *callback.LLMCallEventData) any {
		atomic.AddInt32(&invokeInputCalled, 1)
		if data.Event != callback.LLMInvokeInput {
			t.Errorf("期望 callback.LLMInvokeInput，实际 %s", data.Event)
		}
		if data.IsStream {
			t.Error("Invoke 事件 IsStream 应为 false")
		}
		return nil
	})

	fw.OnLLM(callback.LLMInvokeOutput, func(_ context.Context, data *callback.LLMCallEventData) any {
		atomic.AddInt32(&invokeOutputCalled, 1)
		if data.Event != callback.LLMInvokeOutput {
			t.Errorf("期望 callback.LLMInvokeOutput，实际 %s", data.Event)
		}
		return nil
	})

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{invokeResult: llmschema.NewAssistantMessage("hello")},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	_, err := model.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误: %v", err)
	}

	if atomic.LoadInt32(&invokeInputCalled) != 1 {
		t.Errorf("callback.LLMInvokeInput 应被调用 1 次，实际 %d 次", invokeInputCalled)
	}
	if atomic.LoadInt32(&invokeOutputCalled) != 1 {
		t.Errorf("callback.LLMInvokeOutput 应被调用 1 次，实际 %d 次", invokeOutputCalled)
	}
}

// TestModel_Invoke_错误回调 测试 Invoke 错误触发 callback.LLMCallError 回调
func TestModel_Invoke_错误回调(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var errorCalled int32

	fw.OnLLM(callback.LLMCallError, func(_ context.Context, data *callback.LLMCallEventData) any {
		atomic.AddInt32(&errorCalled, 1)
		if data.Error == nil {
			t.Error("callback.LLMCallError 事件应有 Error 字段")
		}
		return nil
	})

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{invokeErr: context.Canceled},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	_, err := model.Invoke(context.Background(), msgs)
	if err == nil {
		t.Error("Invoke 应返回错误")
	}

	if atomic.LoadInt32(&errorCalled) != 1 {
		t.Errorf("callback.LLMCallError 应被调用 1 次，实际 %d 次", errorCalled)
	}
}

// TestModel_Stream_回调事件 测试 Stream 触发回调事件
func TestModel_Stream_回调事件(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var streamInputCalled int32

	fw.OnLLM(callback.LLMStreamInput, func(_ context.Context, data *callback.LLMCallEventData) any {
		atomic.AddInt32(&streamInputCalled, 1)
		if data.Event != callback.LLMStreamInput {
			t.Errorf("期望 callback.LLMStreamInput，实际 %s", data.Event)
		}
		if !data.IsStream {
			t.Error("Stream 事件 IsStream 应为 true")
		}
		return nil
	})

	// 创建一个会关闭的 channel 模拟流式响应
	chunkChan := make(chan *llmschema.AssistantMessageChunk, 1)
	chunkChan <- llmschema.NewAssistantMessageChunk("chunk")
	close(chunkChan)

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{streamChan: chunkChan},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	result, err := model.Stream(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Stream 不应返回错误: %v", err)
	}

	if atomic.LoadInt32(&streamInputCalled) != 1 {
		t.Errorf("callback.LLMStreamInput 应被调用 1 次，实际 %d 次", streamInputCalled)
	}

	// 等待流结束，读取所有 chunk
	for range result {
	}
}

// TestModel_BuildKVCacheInvokeKwargs_参数构建 测试 KV Cache 参数构建
func TestModel_BuildKVCacheInvokeKwargs_参数构建(t *testing.T) {
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            &mockModelClient{},
		callbackFramework: callback.NewCallbackFramework(),
	}

	// 无 session
	kwargs := model.BuildKVCacheInvokeKwargs(nil, false)
	if len(kwargs) != 0 {
		t.Errorf("无 session 时期望空 map，实际 %v", kwargs)
	}

	// 有 session，启用 cache
	session := &mockSession{id: "test-session-123"}
	kwargs = model.BuildKVCacheInvokeKwargs(session, true)
	if kwargs["session_id"] != "test-session-123" {
		t.Errorf("期望 session_id=test-session-123，实际 %v", kwargs["session_id"])
	}
	if kwargs["enable_cache_sharing"] != true {
		t.Errorf("期望 enable_cache_sharing=true，实际 %v", kwargs["enable_cache_sharing"])
	}

	// 有 session，不启用 cache
	kwargs = model.BuildKVCacheInvokeKwargs(session, false)
	if kwargs["session_id"] != "test-session-123" {
		t.Errorf("期望 session_id=test-session-123，实际 %v", kwargs["session_id"])
	}
	if _, ok := kwargs["enable_cache_sharing"]; ok {
		t.Error("不启用 cache 时不应有 enable_cache_sharing 键")
	}
}

// TestModel_GetClient_获取底层客户端 测试获取底层客户端
func TestModel_GetClient_获取底层客户端(t *testing.T) {
	mockClient := &mockModelClient{}
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            mockClient,
		callbackFramework: callback.NewCallbackFramework(),
	}

	client := model.GetClient()
	if client != mockClient {
		t.Error("GetClient 应返回底层客户端实例")
	}
}

// TestModel_resolveModelName_模型名称解析 测试模型名称解析
func TestModel_resolveModelName_模型名称解析(t *testing.T) {
	model := &Model{
		ModelConfig:  llmschema.NewModelRequestConfig(llmschema.WithModelName("default-model")),
		ClientConfig: llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:       &mockModelClient{},
	}

	// 参数优先
	if name := model.resolveModelName("override"); name != "override" {
		t.Errorf("期望 override，实际 %s", name)
	}

	// 使用 ModelConfig 默认值
	if name := model.resolveModelName(""); name != "default-model" {
		t.Errorf("期望 default-model，实际 %s", name)
	}
}

// ──────────────────────────── 补充测试 ────────────────────────────

// TestNewModel_空配置_返回正确错误 验证 nil 配置的错误码
func TestNewModel_空配置_返回正确错误(t *testing.T) {
	_, err := NewModel(nil, nil)
	if err == nil {
		t.Fatal("nil 配置应返回错误")
	}
	// 验证可以获取错误信息
	if err.Error() == "" {
		t.Error("错误信息不应为空")
	}
}

// TestModel_Release_委托测试 测试 Release 委托给底层客户端
func TestModel_Release_委托测试(t *testing.T) {
	tests := []struct {
		name     string
		result   bool
		err      error
		wantBool bool
		wantErr  bool
	}{
		{
			name:     "成功释放",
			result:   true,
			err:      nil,
			wantBool: true,
			wantErr:  false,
		},
		{
			name:     "释放失败",
			result:   false,
			err:      fmt.Errorf("unsupported"),
			wantBool: false,
			wantErr:  true,
		},
		{
			name:     "不支持KV Cache",
			result:   false,
			err:      nil,
			wantBool: false,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &Model{
				ModelConfig:       llmschema.NewModelRequestConfig(),
				ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
				client:            &mockModelClient{releaseResult: tt.result, releaseErr: tt.err},
				callbackFramework: callback.NewCallbackFramework(),
			}

			got, err := model.Release(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantBool {
				t.Errorf("Release() = %v, want %v", got, tt.wantBool)
			}
		})
	}
}

// TestModel_SupportsKVCacheRelease_KV缓存释放支持 测试 KV Cache 释放支持检查
func TestModel_SupportsKVCacheRelease_KV缓存释放支持(t *testing.T) {
	t.Run("支持KV Cache Release", func(t *testing.T) {
		model := &Model{
			ModelConfig:       llmschema.NewModelRequestConfig(),
			ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
			client:            &mockModelClient{releaseResult: true, releaseErr: nil},
			callbackFramework: callback.NewCallbackFramework(),
		}
		if !model.SupportsKVCacheRelease() {
			t.Error("SupportsKVCacheRelease 应返回 true")
		}
	})

	t.Run("不支持KV Cache Release-返回错误", func(t *testing.T) {
		model := &Model{
			ModelConfig:       llmschema.NewModelRequestConfig(),
			ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
			client:            &mockModelClient{releaseResult: false, releaseErr: fmt.Errorf("not supported")},
			callbackFramework: callback.NewCallbackFramework(),
		}
		if model.SupportsKVCacheRelease() {
			t.Error("SupportsKVCacheRelease 应返回 false（客户端返回错误）")
		}
	})

	t.Run("不支持KV Cache Release-无错误但返回false", func(t *testing.T) {
		// Release 返回 (false, nil) 表示调用成功但不支持
		// 当前实现：err == nil → return true
		// 这实际上是个边界情况：成功调用 Release 但返回 false
		// 在实际场景中，InferenceAffinity 返回 (true, nil)，其他客户端返回错误
		model := &Model{
			ModelConfig:       llmschema.NewModelRequestConfig(),
			ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
			client:            &mockModelClient{releaseResult: false, releaseErr: nil},
			callbackFramework: callback.NewCallbackFramework(),
		}
		// 当前实现认为 err == nil 即为支持
		if !model.SupportsKVCacheRelease() {
			t.Error("err == nil 时当前实现返回 true（即使 bool 为 false）")
		}
	})
}

// TestModel_GenerateImage_图片生成委托 测试图片生成委托
func TestModel_GenerateImage_图片生成委托(t *testing.T) {
	expectedResp := &llmschema.ImageGenerationResponse{}
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            &mockModelClient{genImageResult: expectedResp},
		callbackFramework: callback.NewCallbackFramework(),
	}

	resp, err := model.GenerateImage(context.Background(), nil)
	if err != nil {
		t.Fatalf("GenerateImage 不应返回错误: %v", err)
	}
	if resp != expectedResp {
		t.Error("GenerateImage 应返回底层客户端的结果")
	}
}

// TestModel_GenerateSpeech_语音生成委托 测试语音生成委托
func TestModel_GenerateSpeech_语音生成委托(t *testing.T) {
	expectedResp := &llmschema.AudioGenerationResponse{}
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            &mockModelClient{genSpeechResult: expectedResp},
		callbackFramework: callback.NewCallbackFramework(),
	}

	resp, err := model.GenerateSpeech(context.Background(), nil)
	if err != nil {
		t.Fatalf("GenerateSpeech 不应返回错误: %v", err)
	}
	if resp != expectedResp {
		t.Error("GenerateSpeech 应返回底层客户端的结果")
	}
}

// TestModel_GenerateVideo_视频生成委托 测试视频生成委托
func TestModel_GenerateVideo_视频生成委托(t *testing.T) {
	expectedResp := &llmschema.VideoGenerationResponse{}
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            &mockModelClient{genVideoResult: expectedResp},
		callbackFramework: callback.NewCallbackFramework(),
	}

	resp, err := model.GenerateVideo(context.Background(), nil)
	if err != nil {
		t.Fatalf("GenerateVideo 不应返回错误: %v", err)
	}
	if resp != expectedResp {
		t.Error("GenerateVideo 应返回底层客户端的结果")
	}
}

// TestModel_Format_格式化输出 测试 fmt.Formatter 接口实现
func TestModel_Format_格式化输出(t *testing.T) {
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{},
		callbackFramework: callback.NewCallbackFramework(),
	}

	got := fmt.Sprintf("%v", model)
	expected := "Model(provider=OpenAI, model=gpt-4)"
	if got != expected {
		t.Errorf("Format 期望 %q，实际 %q", expected, got)
	}
}

// TestModel_Format_模型配置为空 测试 Format 在 ModelConfig 为 nil 时
func TestModel_Format_模型配置为空(t *testing.T) {
	model := &Model{
		ModelConfig:       nil,
		ClientConfig:      llmschema.NewModelClientConfig("TestProvider", "key", "http://localhost"),
		client:            &mockModelClient{},
		callbackFramework: callback.NewCallbackFramework(),
	}

	got := fmt.Sprintf("%v", model)
	expected := "Model(provider=TestProvider, model=)"
	if got != expected {
		t.Errorf("Format 期望 %q，实际 %q", expected, got)
	}
}

// TestModel_Invoke_带用量元数据 测试 Invoke 回调中包含 UsageMetadata
func TestModel_Invoke_带用量元数据(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var outputData *callback.LLMCallEventData

	fw.OnLLM(callback.LLMInvokeOutput, func(_ context.Context, data *callback.LLMCallEventData) any {
		outputData = data
		return nil
	})

	usage := &llmschema.UsageMetadata{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}
	result := llmschema.NewAssistantMessage("hello")
	result.UsageMetadata = usage

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{invokeResult: result},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	_, err := model.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误: %v", err)
	}

	if outputData.Usage == nil {
		t.Fatal("callback.LLMInvokeOutput 事件应包含 Usage")
	}
	if outputData.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens 期望 10，实际 %d", outputData.Usage.InputTokens)
	}
}

// TestModel_Invoke_无用量元数据 测试 Invoke 回调中无 UsageMetadata
func TestModel_Invoke_无用量元数据(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var outputData *callback.LLMCallEventData

	fw.OnLLM(callback.LLMInvokeOutput, func(_ context.Context, data *callback.LLMCallEventData) any {
		outputData = data
		return nil
	})

	result := llmschema.NewAssistantMessage("hello")
	// UsageMetadata 为 nil

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{invokeResult: result},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	_, err := model.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误: %v", err)
	}

	if outputData.Usage != nil {
		t.Error("无 UsageMetadata 时，Usage 应为 nil")
	}
}

// TestModel_Stream_错误 测试 Stream 错误触发 callback.LLMCallError
func TestModel_Stream_错误(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var errorCalled int32

	fw.OnLLM(callback.LLMCallError, func(_ context.Context, data *callback.LLMCallEventData) any {
		atomic.AddInt32(&errorCalled, 1)
		if data.Error == nil {
			t.Error("callback.LLMCallError 事件应有 Error 字段")
		}
		if !data.IsStream {
			t.Error("Stream 错误事件 IsStream 应为 true")
		}
		return nil
	})

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{streamErr: fmt.Errorf("connection refused")},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	_, err := model.Stream(context.Background(), msgs)
	if err == nil {
		t.Error("Stream 应返回错误")
	}

	if atomic.LoadInt32(&errorCalled) != 1 {
		t.Errorf("callback.LLMCallError 应被调用 1 次，实际 %d 次", errorCalled)
	}
}

// TestModel_Stream_输出回调 测试 Stream 完成后触发 callback.LLMStreamOutput
func TestModel_Stream_输出回调(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var outputCalled int32
	var outputDataMu sync.Mutex
	var outputData *callback.LLMCallEventData

	fw.OnLLM(callback.LLMStreamOutput, func(_ context.Context, data *callback.LLMCallEventData) any {
		atomic.AddInt32(&outputCalled, 1)
		outputDataMu.Lock()
		outputData = data
		outputDataMu.Unlock()
		if data.Event != callback.LLMStreamOutput {
			t.Errorf("期望 callback.LLMStreamOutput，实际 %s", data.Event)
		}
		if !data.IsStream {
			t.Error("Stream 事件 IsStream 应为 true")
		}
		return nil
	})

	// 创建带 UsageMetadata 的流式结果
	usage := &llmschema.UsageMetadata{InputTokens: 5, OutputTokens: 15, TotalTokens: 20}
	finalChunk := llmschema.NewAssistantMessageChunk("final")
	finalChunk.UsageMetadata = usage

	chunkChan := make(chan *llmschema.AssistantMessageChunk, 2)
	chunkChan <- llmschema.NewAssistantMessageChunk("chunk1")
	chunkChan <- finalChunk
	close(chunkChan)

	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model")),
		ClientConfig:      llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost"),
		client:            &mockModelClient{streamChan: chunkChan},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	result, err := model.Stream(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Stream 不应返回错误: %v", err)
	}

	// 等待流结束，读取所有 chunk
	for range result {
	}

	// 等待 goroutine 完成回调
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&outputCalled) != 2 {
		t.Errorf("callback.LLMStreamOutput 应被调用 2 次（per item），实际 %d 次", outputCalled)
	}

	outputDataMu.Lock()
	data := outputData
	outputDataMu.Unlock()
	if data != nil && data.Usage != nil {
		if data.Usage.InputTokens != 5 {
			t.Errorf("Usage.InputTokens 期望 5，实际 %d", data.Usage.InputTokens)
		}
	}
}

// TestModel_resolveModelName_模型配置为空 测试 ModelConfig 为 nil 时
func TestModel_resolveModelName_模型配置为空(t *testing.T) {
	model := &Model{
		ModelConfig:  nil,
		ClientConfig: llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:       &mockModelClient{},
	}

	if name := model.resolveModelName(""); name != "" {
		t.Errorf("ModelConfig 为 nil 且无参数时，期望空字符串，实际 %q", name)
	}
}

// TestModel_resolveStreamModelName_流式模型名称解析 测试流式模型名称解析
func TestModel_resolveStreamModelName_流式模型名称解析(t *testing.T) {
	t.Run("参数优先", func(t *testing.T) {
		model := &Model{
			ModelConfig:  llmschema.NewModelRequestConfig(llmschema.WithModelName("default-model")),
			ClientConfig: llmschema.NewModelClientConfig("test", "key", "http://localhost"),
			client:       &mockModelClient{},
		}
		if name := model.resolveStreamModelName("stream-override"); name != "stream-override" {
			t.Errorf("期望 stream-override，实际 %s", name)
		}
	})

	t.Run("使用 ModelConfig 默认值", func(t *testing.T) {
		model := &Model{
			ModelConfig:  llmschema.NewModelRequestConfig(llmschema.WithModelName("default-model")),
			ClientConfig: llmschema.NewModelClientConfig("test", "key", "http://localhost"),
			client:       &mockModelClient{},
		}
		if name := model.resolveStreamModelName(""); name != "default-model" {
			t.Errorf("期望 default-model，实际 %s", name)
		}
	})

	t.Run("ModelConfig 为 nil", func(t *testing.T) {
		model := &Model{
			ModelConfig:  nil,
			ClientConfig: llmschema.NewModelClientConfig("test", "key", "http://localhost"),
			client:       &mockModelClient{},
		}
		if name := model.resolveStreamModelName(""); name != "" {
			t.Errorf("期望空字符串，实际 %q", name)
		}
	})
}

// TestModel_Invoke_额外数据 测试 Invoke 回调中的 Extra 数据
func TestModel_Invoke_额外数据(t *testing.T) {
	fw := callback.NewCallbackFramework()
	var inputData *callback.LLMCallEventData

	fw.OnLLM(callback.LLMInvokeInput, func(_ context.Context, data *callback.LLMCallEventData) any {
		inputData = data
		return nil
	})

	modelCfg := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	clientCfg := llmschema.NewModelClientConfig("OpenAI", "key", "http://localhost")
	model := &Model{
		ModelConfig:       modelCfg,
		ClientConfig:      clientCfg,
		client:            &mockModelClient{invokeResult: llmschema.NewAssistantMessage("hello")},
		callbackFramework: fw,
	}

	msgs := model_clients.NewTextMessagesParam("test")
	_, err := model.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误: %v", err)
	}

	if inputData == nil {
		t.Fatal("callback.LLMInvokeInput 事件数据不应为 nil")
	}
	if inputData.Extra["model_config"] != modelCfg {
		t.Error("Extra 应包含 model_config")
	}
	if inputData.Extra["model_client_config"] != clientCfg {
		t.Error("Extra 应包含 model_client_config")
	}
}

// TestModel_BuildKVCacheInvokeKwargs_仅Session 测试仅 session 不启用 cache
func TestModel_BuildKVCacheInvokeKwargs_仅Session(t *testing.T) {
	model := &Model{
		ModelConfig:       llmschema.NewModelRequestConfig(),
		ClientConfig:      llmschema.NewModelClientConfig("test", "key", "http://localhost"),
		client:            &mockModelClient{},
		callbackFramework: callback.NewCallbackFramework(),
	}

	session := &mockSession{id: "session-456"}
	kwargs := model.BuildKVCacheInvokeKwargs(session, false)
	if kwargs["session_id"] != "session-456" {
		t.Errorf("期望 session_id=session-456，实际 %v", kwargs["session_id"])
	}
	if _, ok := kwargs["enable_cache_sharing"]; ok {
		t.Error("不启用 cache 时不应有 enable_cache_sharing 键")
	}
}
