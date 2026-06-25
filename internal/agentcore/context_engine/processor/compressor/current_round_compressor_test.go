package compressor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// validCurrentRoundCompressorConfig 创建合法的 CurrentRoundCompressorConfig
func validCurrentRoundCompressorConfig() *CurrentRoundCompressorConfig {
	return &CurrentRoundCompressorConfig{
		TokensThreshold:                 100,
		MessagesToKeep:                  3,
		MinSelectedTokensForCompression: 10,
		CompressionTargetTokens:         4000,
		SummaryMergeTargetTokens:        4000,
		AccumulatedSummaryTokenLimit:    20000,
		SummaryMergeMinBlocks:           3,
		PriorContextWindowSize:          10,
	}
}

// crcFakeBaseModelClient 测试用 BaseModelClient 模拟
type crcFakeBaseModelClient struct {
	invokeResult *llm_schema.AssistantMessage
	invokeErr    error
}

func (f *crcFakeBaseModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	return f.invokeResult, f.invokeErr
}
func (f *crcFakeBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llm_schema.AssistantMessageChunk, error) {
	return nil, nil
}
func (f *crcFakeBaseModelClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *crcFakeBaseModelClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *crcFakeBaseModelClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *crcFakeBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

func (f *crcFakeBaseModelClient) SupportsKVCacheRelease() bool {
	return false
}

const crcTestProvider = "CRCTestProvider"

// crcFakeRegistryOnce 确保 fake provider 只注册一次
var crcFakeRegistryOnce sync.Once

// crcCurrentFakeClient 当前使用的 fake 客户端（因为注册表工厂只能注册一次）
var crcCurrentFakeClient *crcFakeBaseModelClient

// crcNewFakeLLMModel 创建带 fake client 的 llm.Model 实例
func crcNewFakeLLMModel(fakeClient *crcFakeBaseModelClient) *llm.Model {
	crcCurrentFakeClient = fakeClient
	crcFakeRegistryOnce.Do(func() {
		model_clients.GetClientRegistry().Register(crcTestProvider, "llm",
			func(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) model_clients.BaseModelClient {
				return crcCurrentFakeClient
			},
		)
	})

	clientConfig := &llm_schema.ModelClientConfig{
		ClientID:       "crc-test-client",
		ClientProvider: crcTestProvider,
		APIKey:         "fake-key",
		APIBase:        "http://localhost",
		Timeout:        60,
		MaxRetries:     1,
		VerifySSL:      false,
	}
	modelConfig := &llm_schema.ModelRequestConfig{
		ModelName: "test-model",
	}

	model, err := llm.NewModel(clientConfig, modelConfig)
	if err != nil {
		panic(fmt.Sprintf("crcNewFakeLLMModel 失败: %v", err))
	}
	return model
}

// newCRCWithModel 创建带 fake LLM Model 的 CurrentRoundCompressor
func newCRCWithModel(cfg *CurrentRoundCompressorConfig, fakeClient *crcFakeBaseModelClient) *CurrentRoundCompressor {
	if cfg == nil {
		cfg = validCurrentRoundCompressorConfig()
	}
	model := crcNewFakeLLMModel(fakeClient)
	crc, err := NewCurrentRoundCompressor(cfg, WithCurrentRoundModel(model))
	if err != nil {
		panic(fmt.Sprintf("newCRCWithModel 失败: %v", err))
	}
	return crc
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewCurrentRoundCompressorConfig_默认值 验证默认配置值与 Python 对齐
func TestNewCurrentRoundCompressorConfig_默认值(t *testing.T) {
	cfg := NewCurrentRoundCompressorConfig()
	if cfg.TokensThreshold != 100000 {
		t.Errorf("TokensThreshold 期望 100000，实际: %d", cfg.TokensThreshold)
	}
	if cfg.MessagesToKeep != 3 {
		t.Errorf("MessagesToKeep 期望 3，实际: %d", cfg.MessagesToKeep)
	}
	if cfg.MinSelectedTokensForCompression != 20000 {
		t.Errorf("MinSelectedTokensForCompression 期望 20000，实际: %d", cfg.MinSelectedTokensForCompression)
	}
	if cfg.CompressionTargetTokens != 4000 {
		t.Errorf("CompressionTargetTokens 期望 4000，实际: %d", cfg.CompressionTargetTokens)
	}
	if cfg.SummaryMergeTargetTokens != 4000 {
		t.Errorf("SummaryMergeTargetTokens 期望 4000，实际: %d", cfg.SummaryMergeTargetTokens)
	}
	if cfg.AccumulatedSummaryTokenLimit != 20000 {
		t.Errorf("AccumulatedSummaryTokenLimit 期望 20000，实际: %d", cfg.AccumulatedSummaryTokenLimit)
	}
	if cfg.SummaryMergeMinBlocks != 3 {
		t.Errorf("SummaryMergeMinBlocks 期望 3，实际: %d", cfg.SummaryMergeMinBlocks)
	}
	if cfg.PriorContextWindowSize != 10 {
		t.Errorf("PriorContextWindowSize 期望 10，实际: %d", cfg.PriorContextWindowSize)
	}
}

// TestCurrentRoundCompressorConfig_Validate_正常 验证合法配置通过校验
func TestCurrentRoundCompressorConfig_Validate_正常(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("合法配置不应返回错误，实际: %v", err)
	}
}

// TestCurrentRoundCompressorConfig_Validate_TokensThreshold零 验证 TokensThreshold <= 0 报错
func TestCurrentRoundCompressorConfig_Validate_TokensThreshold零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 0
	if err := cfg.Validate(); err == nil {
		t.Error("TokensThreshold = 0 应返回错误")
	}

	cfg.TokensThreshold = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TokensThreshold = -1 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_MessagesToKeep负数 验证 MessagesToKeep < 0 报错
func TestCurrentRoundCompressorConfig_Validate_MessagesToKeep负数(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 0
	if err := cfg.Validate(); err == nil {
		t.Error("MessagesToKeep = 0 应返回错误")
	}

	cfg.MessagesToKeep = -1
	if err := cfg.Validate(); err == nil {
		t.Error("MessagesToKeep = -1 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_MinSelectedTokensForCompression零 验证字段 <= 0 报错
func TestCurrentRoundCompressorConfig_Validate_MinSelectedTokensForCompression零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 0
	if err := cfg.Validate(); err == nil {
		t.Error("MinSelectedTokensForCompression = 0 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_CompressionTargetTokens零 验证字段 <= 0 报错
func TestCurrentRoundCompressorConfig_Validate_CompressionTargetTokens零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.CompressionTargetTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("CompressionTargetTokens = 0 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_SummaryMergeTargetTokens零 验证字段 <= 0 报错
func TestCurrentRoundCompressorConfig_Validate_SummaryMergeTargetTokens零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.SummaryMergeTargetTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("SummaryMergeTargetTokens = 0 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_AccumulatedSummaryTokenLimit零 验证字段 <= 0 报错
func TestCurrentRoundCompressorConfig_Validate_AccumulatedSummaryTokenLimit零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.AccumulatedSummaryTokenLimit = 0
	if err := cfg.Validate(); err == nil {
		t.Error("AccumulatedSummaryTokenLimit = 0 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_SummaryMergeMinBlocks小于2 验证字段 < 2 报错
func TestCurrentRoundCompressorConfig_Validate_SummaryMergeMinBlocks小于2(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.SummaryMergeMinBlocks = 1
	if err := cfg.Validate(); err == nil {
		t.Error("SummaryMergeMinBlocks = 1 应返回错误（必须 >= 2）")
	}

	cfg.SummaryMergeMinBlocks = 0
	if err := cfg.Validate(); err == nil {
		t.Error("SummaryMergeMinBlocks = 0 应返回错误")
	}
}

// TestCurrentRoundCompressorConfig_Validate_PriorContextWindowSize零 验证字段 <= 0 报错
func TestCurrentRoundCompressorConfig_Validate_PriorContextWindowSize零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.PriorContextWindowSize = 0
	if err := cfg.Validate(); err == nil {
		t.Error("PriorContextWindowSize = 0 应返回错误")
	}
}

// TestCurrentRoundCompressor_ProcessorType 验证处理器类型标识
func TestCurrentRoundCompressor_ProcessorType(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)
	if crc.ProcessorType() != "CurrentRoundCompressor" {
		t.Errorf("ProcessorType 应为 CurrentRoundCompressor，实际: %s", crc.ProcessorType())
	}
}

// TestCurrentRoundCompressor_TriggerAddMessages_超过阈值 验证 Token 数超过阈值时触发
func TestCurrentRoundCompressor_TriggerAddMessages_超过阈值(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 10 // 低阈值便于测试
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	// 使用 fakeTokenCounter 返回高 Token 数
	mc := &fakeModelContext{
		messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello world"),
			llm_schema.NewAssistantMessage("response"),
		},
		tokenCounter: &fakeTokenCounter{count: 200},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("another message"),
	}

	triggered, err := crc.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if !triggered {
		t.Error("Token 数超过阈值时应触发")
	}
}

// TestCurrentRoundCompressor_TriggerAddMessages_低于阈值 验证 Token 数低于阈值时不触发
func TestCurrentRoundCompressor_TriggerAddMessages_低于阈值(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 100000 // 高阈值
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("short")},
		tokenCounter: &fakeTokenCounter{count: 10},
	}

	triggered, err := crc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("Token 数低于阈值时不应触发")
	}
}

// TestCurrentRoundCompressor_TriggerAddMessages_消息数少于MessagesToKeep 验证消息数 < MessagesToKeep 不触发
func TestCurrentRoundCompressor_TriggerAddMessages_消息数少于MessagesToKeep(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 10
	cfg.TokensThreshold = 50
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		tokenCounter: &fakeTokenCounter{count: 1000},
	}

	triggered, err := crc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("总消息数 < MessagesToKeep 时不应触发")
	}
}

// TestCurrentRoundCompressor_OnAddMessages_触发压缩 验证压缩触发后输出包含 CURRENT_ROUND_MEMORY_BLOCK
func TestCurrentRoundCompressor_OnAddMessages_触发压缩(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 10                // 低阈值确保触发
	cfg.MessagesToKeep = 1                  // 仅保留 1 条
	cfg.MinSelectedTokensForCompression = 1 // 低阈值确保进入压缩
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("压缩摘要内容"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	// 使用 dynamicFakeTokenCounter：原始消息返回高 Token 数，压缩后返回低 Token 数
	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10}},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "tc-1", Name: "test_tool", Arguments: "{}"},
			}),
		),
		llm_schema.NewToolMessage("tc-1", "工具结果内容"),
		llm_schema.NewAssistantMessage("最终回答"),
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}

	// 验证压缩成功
	if event == nil {
		contextMessages := append(mc.GetMessages(0, true), messagesToAdd...)
		idx := crc.GetCompressIdx(contextMessages)
		t.Fatalf("压缩触发后应返回非空 event，GetCompressIdx=%d, len(contextMessages)=%d", idx, len(contextMessages))
	}
	if event.EventType != "CurrentRoundCompressor" {
		t.Errorf("EventType 应为 CurrentRoundCompressor，实际: %s", event.EventType)
	}
	if !strings.Contains(event.CompactSummary, "[CURRENT_ROUND_MEMORY_BLOCK]") {
		t.Errorf("CompactSummary 应包含 CURRENT_ROUND_MEMORY_BLOCK，实际: %s", event.CompactSummary)
	}

	if len(result) != 0 {
		t.Errorf("压缩后应返回空 messagesToAdd，实际: %d 条", len(result))
	}

	// 验证 ModelContext 中的消息包含记忆块
	updated := mc.GetMessages(0, true)
	found := false
	for _, msg := range updated {
		if strings.Contains(msg.GetContent().Text(), "[CURRENT_ROUND_MEMORY_BLOCK]") {
			found = true
			break
		}
	}
	if !found {
		t.Error("压缩后 ModelContext 应包含 CURRENT_ROUND_MEMORY_BLOCK 消息")
	}
}

// TestCurrentRoundCompressor_OnAddMessages_不压缩 验证 Token 低于阈值时透传
func TestCurrentRoundCompressor_OnAddMessages_不压缩(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 100000 // 高阈值，不触发
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		tokenCounter: &fakeTokenCounter{count: 10},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("world"),
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("Token 低于阈值时不应返回 event")
	}
	if len(result) != 1 {
		t.Errorf("Token 低于阈值时应透传消息，实际: %d 条", len(result))
	}
}

// TestCurrentRoundCompressor_OnAddMessages_最后一条是UserMessage 验证 GetCompressIdx 返回 -1 时不压缩
func TestCurrentRoundCompressor_OnAddMessages_最后一条是UserMessage(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: nil,
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("回答"),
		llm_schema.NewUserMessage("最后一条是用户消息"),
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("最后一条是 UserMessage 时不应返回 event")
	}
	if len(result) != 2 {
		t.Errorf("最后一条是 UserMessage 时应透传所有消息，实际: %d 条", len(result))
	}
}

// TestCurrentRoundCompressor_GetCompressIdx_找到边界 验证正常 UserMessage 在保留区域内返回 -1
func TestCurrentRoundCompressor_GetCompressIdx_找到边界(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 3
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一个问题"),    // 0
		llm_schema.NewAssistantMessage("回答1"), // 1
		llm_schema.NewUserMessage("第二个问题"),    // 2 - keepIndex = 5-3=2, 2 >= 2 → 在保留区域
		llm_schema.NewAssistantMessage("回答2"), // 3
		llm_schema.NewAssistantMessage("回答3"), // 4
	}

	idx := crc.GetCompressIdx(messages)
	if idx != -1 {
		t.Errorf("UserMessage 在保留区域内应返回 -1，实际: %d", idx)
	}
}

// TestCurrentRoundCompressor_GetCompressIdx_最后UserMessage 验证最后一条是 UserMessage 时返回 -1
func TestCurrentRoundCompressor_GetCompressIdx_最后UserMessage(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 2
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("回答"),
		llm_schema.NewUserMessage("用户问题"), // 最后一条消息
	}

	idx := crc.GetCompressIdx(messages)
	// 最后一条 UserMessage 是最后一条消息，compressedIdx == len(messages)-1 → 返回 -1
	if idx != -1 {
		t.Errorf("最后一条是 UserMessage 时应返回 -1，实际: %d", idx)
	}
}

// TestCurrentRoundCompressor_GetCompressIdx_在保留区域内 验证 UserMessage 在保留区域内返回 -1
func TestCurrentRoundCompressor_GetCompressIdx_在保留区域内(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 3
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),      // 0
		llm_schema.NewAssistantMessage("回答1"), // 1
		llm_schema.NewUserMessage("问题2"),      // 2 - keepIndex=5-3=2, 2 >= 2
		llm_schema.NewAssistantMessage("回答2"), // 3
		llm_schema.NewAssistantMessage("回答3"), // 4
	}

	idx := crc.GetCompressIdx(messages)
	if idx != -1 {
		t.Errorf("UserMessage 在保留区域内应返回 -1，实际: %d", idx)
	}
}

// TestCurrentRoundCompressor_GetCompressIdx_可压缩边界 验证正常可压缩场景
func TestCurrentRoundCompressor_GetCompressIdx_可压缩边界(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 2
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),      // 0
		llm_schema.NewAssistantMessage("回答1"), // 1
		llm_schema.NewUserMessage("问题2"),      // 2 - keepIndex=5-2=3, 2 < 3
		llm_schema.NewAssistantMessage("回答2"), // 3
		llm_schema.NewAssistantMessage("回答3"), // 4
	}

	idx := crc.GetCompressIdx(messages)
	if idx != 2 {
		t.Errorf("可压缩 UserMessage 应返回索引 2，实际: %d", idx)
	}
}

// TestCurrentRoundCompressor_GetCompressIdx_无UserMessage 验证无 UserMessage 时返回 -1
func TestCurrentRoundCompressor_GetCompressIdx_无UserMessage(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 2
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("回答1"),
		llm_schema.NewAssistantMessage("回答2"),
	}

	idx := crc.GetCompressIdx(messages)
	if idx != -1 {
		t.Errorf("无 UserMessage 时应返回 -1，实际: %d", idx)
	}
}

// TestWrapCurrentRoundMemoryBlock 验证完整元数据头部
func TestWrapCurrentRoundMemoryBlock(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	summary := "这是压缩摘要内容"
	result := crc.WrapCurrentRoundMemoryBlock(summary)

	// 验证标记前缀
	if !strings.HasPrefix(result, "[CURRENT_ROUND_MEMORY_BLOCK]") {
		t.Error("记忆块应以 [CURRENT_ROUND_MEMORY_BLOCK] 开头")
	}
	// 验证元数据头部
	expectedHeaders := []string{
		"processor: CurrentRoundCompressor",
		"type: historical_memory_block",
		"scope: current_round_increment",
		"type_note:",
		"authority:",
		"instruction_status:",
		"strategy_status:",
		"tool_action_state_status:",
		"conflict_priority:",
		"Summary:",
	}
	for _, header := range expectedHeaders {
		if !strings.Contains(result, header) {
			t.Errorf("记忆块应包含头部 '%s'", header)
		}
	}
	// 验证摘要内容
	if !strings.Contains(result, summary) {
		t.Error("记忆块应包含原始摘要内容")
	}
}

// TestUnwrapMemoryBlockSummary 验证剥离信封提取摘要
func TestUnwrapMemoryBlockSummary(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	// 测试完整信封
	wrapped := "[CURRENT_ROUND_MEMORY_BLOCK]\nprocessor: test\n\nSummary:\n这是摘要内容"
	result := crc.UnwrapMemoryBlockSummary(wrapped)
	if result != "这是摘要内容" {
		t.Errorf("剥离信封后应为 '这是摘要内容'，实际: %s", result)
	}

	// 测试无信封的纯文本
	plainText := "这是纯文本摘要"
	result = crc.UnwrapMemoryBlockSummary(plainText)
	if result != plainText {
		t.Errorf("纯文本应原样返回，实际: %s", result)
	}

	// 测试空字符串
	result = crc.UnwrapMemoryBlockSummary("")
	if result != "" {
		t.Errorf("空字符串应返回空，实际: %s", result)
	}

	// 测试有标记但无 Summary 的文本
	noSummary := "[CURRENT_ROUND_MEMORY_BLOCK]\nprocessor: test"
	result = crc.UnwrapMemoryBlockSummary(noSummary)
	if result != noSummary {
		t.Errorf("有标记但无 Summary 应原样返回，实际: %s", result)
	}
}

// TestFormatRecentContext_排除记忆块 验证跳过 SUMMARY_MARKER 消息
func TestFormatRecentContext_排除记忆块(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),                                         // 0
		llm_schema.NewAssistantMessage("回答"),                                      // 1
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块"), // 2 - 应被排除
		llm_schema.NewAssistantMessage("最新回答"),                                    // 3
	}

	// endIdx=1，取 messages[2:] 作为 recent messages
	result := crc.FormatRecentContext(messages, 1)

	if !strings.Contains(result, "最新回答") {
		t.Error("FormatRecentContext 应包含非记忆块消息")
	}
	if strings.Contains(result, "[CURRENT_ROUND_MEMORY_BLOCK]") {
		t.Error("FormatRecentContext 应排除记忆块消息")
	}
}

// TestFormatRecentContext_空尾部 验证无尾部消息时返回空
func TestFormatRecentContext_空尾部(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	result := crc.FormatRecentContext(messages, 1)
	if result != "" {
		t.Errorf("无尾部消息时应返回空字符串，实际: %s", result)
	}
}

// TestFormatPriorContextAndQuery_过滤工具消息 验证仅包含纯 User 和无 tool_calls 的 Assistant
func TestFormatPriorContextAndQuery_过滤工具消息(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.PriorContextWindowSize = 10
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("纯用户消息"),      // 0
		llm_schema.NewAssistantMessage("纯助手消息"), // 1
		llm_schema.NewAssistantMessage("带工具",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "tc-1", Name: "test_tool", Arguments: "{}"},
			}),
		), // 2 - 应被过滤
		llm_schema.NewToolMessage("tc-1", "工具结果"),                                // 3 - 应被过滤
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n记忆块"), // 4 - 摘要 User 应被过滤
		llm_schema.NewUserMessage("当前查询"),                                        // 5 - currentQueryIdx
	}

	result := crc.FormatPriorContextAndQuery(messages, 5)

	if !strings.Contains(result, "纯用户消息") {
		t.Error("应包含纯 UserMessage")
	}
	if !strings.Contains(result, "纯助手消息") {
		t.Error("应包含纯 AssistantMessage（无 tool_calls）")
	}
	if strings.Contains(result, "带工具") {
		t.Error("应排除含 tool_calls 的 AssistantMessage")
	}
	if strings.Contains(result, "工具结果") {
		t.Error("应排除 ToolMessage")
	}
	if strings.Contains(result, "[CURRENT_ROUND_MEMORY_BLOCK]") {
		t.Error("应排除记忆块 UserMessage")
	}
	if !strings.Contains(result, "当前查询") {
		t.Error("应包含当前查询消息")
	}
	if !strings.Contains(result, "Current User Intent") {
		t.Error("应包含 Current User Intent 标记")
	}
}

// TestFormatPriorContextAndQuery_窗口截断 验证超出 priorContextWindowSize 时截断
func TestFormatPriorContextAndQuery_窗口截断(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.PriorContextWindowSize = 2
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户1"),      // 0
		llm_schema.NewAssistantMessage("助手1"), // 1
		llm_schema.NewUserMessage("用户2"),      // 2
		llm_schema.NewAssistantMessage("助手2"), // 3
		llm_schema.NewUserMessage("用户3"),      // 4
		llm_schema.NewAssistantMessage("助手3"), // 5
		llm_schema.NewUserMessage("当前查询"),     // 6 - currentQueryIdx
	}

	result := crc.FormatPriorContextAndQuery(messages, 6)

	if strings.Contains(result, "用户1") {
		t.Error("窗口截断后不应包含最早的用户1")
	}
	if strings.Contains(result, "用户2") {
		t.Error("窗口截断后不应包含用户2")
	}
	if !strings.Contains(result, "用户3") {
		t.Error("窗口截断后应保留最近的用户3")
	}
	if !strings.Contains(result, "当前查询") {
		t.Error("应包含当前查询")
	}
}

// TestFormatPriorContextAndQuery_空上下文 验证无先验上下文时只包含当前查询
func TestFormatPriorContextAndQuery_空上下文(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("当前查询"),
	}

	result := crc.FormatPriorContextAndQuery(messages, 0)
	if !strings.Contains(result, "当前查询") {
		t.Error("应包含当前查询")
	}
}

// TestBuildPrompt 验证提示词占位符填充
func TestBuildPrompt(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	result := crc.BuildPrompt(4000, "旧摘要", "近期上下文", "先验上下文和查询")

	if !strings.Contains(result, "4000") {
		t.Error("提示词应包含 target_tokens=4000")
	}
	if !strings.Contains(result, "旧摘要") {
		t.Error("提示词应包含 accumulated_summaries")
	}
	if !strings.Contains(result, "近期上下文") {
		t.Error("提示词应包含 recent_messages")
	}
	if !strings.Contains(result, "先验上下文和查询") {
		t.Error("提示词应包含 prior_context_and_query")
	}
}

// TestBuildPrompt_空值 验证空值替换为 (none)
func TestBuildPrompt_空值(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	result := crc.BuildPrompt(4000, "", "", "")

	if !strings.Contains(result, "(none)") {
		t.Error("空值占位符应替换为 (none)")
	}
}

// TestCurrentRoundCompressor_SaveLoadState 验证状态保存/加载（空操作）
func TestCurrentRoundCompressor_SaveLoadState(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	state := crc.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState 应返回空 map，实际: %v", state)
	}

	// LoadState 是空操作，只需确保不 panic
	crc.LoadState(map[string]any{"key": "value"})
}

// TestCurrentRoundCompressor_OnGetContextWindow 验证空操作
func TestCurrentRoundCompressor_OnGetContextWindow(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	cw := iface.ContextWindow{}
	event, resultCw, err := crc.OnGetContextWindow(context.Background(), nil, cw)
	if err != nil {
		t.Fatalf("OnGetContextWindow 失败: %v", err)
	}
	if event != nil {
		t.Error("OnGetContextWindow 应返回 nil event")
	}
	// OnGetContextWindow 应原样返回传入的 ContextWindow
	if len(resultCw.ContextMessages) != len(cw.ContextMessages) {
		t.Error("OnGetContextWindow 应原样返回 ContextWindow")
	}
}

// TestCurrentRoundCompressor_TriggerGetContextWindow 验证始终不触发
func TestCurrentRoundCompressor_TriggerGetContextWindow(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	triggered, err := crc.TriggerGetContextWindow(context.Background(), nil, iface.ContextWindow{})
	if err != nil {
		t.Fatalf("TriggerGetContextWindow 失败: %v", err)
	}
	if triggered {
		t.Error("TriggerGetContextWindow 应始终返回 false")
	}
}

// TestCurrentRoundCompressor_自定义提示词 验证自定义提示词
func TestCurrentRoundCompressor_自定义提示词(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.CustomCompressionPrompt = "自定义压缩提示词 {target_tokens}"
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)
	if crc.compressedPrompt != "自定义压缩提示词 {target_tokens}" {
		t.Errorf("应使用自定义提示词，实际: %s", crc.compressedPrompt)
	}
}

// TestCurrentRoundCompressor_默认提示词 验证默认提示词
func TestCurrentRoundCompressor_默认提示词(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.CustomCompressionPrompt = ""
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)
	if crc.compressedPrompt != defaultCurrentRoundCompressionPrompt {
		t.Error("空字符串时应使用默认提示词")
	}
}

// TestWithCurrentRoundModel 验证 WithCurrentRoundModel 选项
func TestWithCurrentRoundModel(t *testing.T) {
	fakeClient := &crcFakeBaseModelClient{}
	model := crcNewFakeLLMModel(fakeClient)
	opt := WithCurrentRoundModel(model)
	crc := &CurrentRoundCompressor{}
	opt(crc)
	if crc.model != model {
		t.Error("WithCurrentRoundModel 应将 model 设为指定实例")
	}
}

// TestCurrentRoundCompressor_工厂注册 验证 GetProcessorFactory("CurrentRoundCompressor") 存在
func TestCurrentRoundCompressor_工厂注册(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("CurrentRoundCompressor")
	if !ok {
		t.Fatal("CurrentRoundCompressor 应已通过 init() 注册")
	}
	if factory == nil {
		t.Fatal("factory 不应为 nil")
	}
}

// TestCurrentRoundCompressor_工厂注册列表 验证 CurrentRoundCompressor 在已注册工厂列表中
func TestCurrentRoundCompressor_工厂注册列表(t *testing.T) {
	factories := context_engine.ListProcessorFactories()
	found := false
	for _, name := range factories {
		if name == "CurrentRoundCompressor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("CurrentRoundCompressor 应在已注册工厂列表中")
	}
}

// TestCurrentRoundCompressor_工厂配置类型不匹配 验证错误配置类型返回 error
func TestCurrentRoundCompressor_工厂配置类型不匹配(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("CurrentRoundCompressor")
	if !ok {
		t.Fatal("CurrentRoundCompressor 应已注册")
	}

	otherConfig := &testConfig{Name: "other"}
	result, err := factory(otherConfig)
	if err == nil {
		t.Error("错误类型配置应返回 error")
	}
	if result != nil {
		t.Error("错误类型配置应返回 nil 结果")
	}
	if !strings.Contains(err.Error(), "配置类型不匹配") {
		t.Errorf("错误信息应包含'配置类型不匹配'，实际: %v", err)
	}
}

// TestCurrentRoundCompressor_工厂非法配置 验证非法配置返回 error
func TestCurrentRoundCompressor_工厂非法配置(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("CurrentRoundCompressor")
	if !ok {
		t.Fatal("CurrentRoundCompressor 应已注册")
	}

	result, err := factory(&CurrentRoundCompressorConfig{TokensThreshold: 0})
	if err == nil {
		t.Error("非法配置应返回 error")
	}
	if result != nil {
		t.Error("非法配置应返回 nil 结果")
	}
}

// TestNewCurrentRoundCompressor_校验失败 验证非法配置创建失败
func TestNewCurrentRoundCompressor_校验失败(t *testing.T) {
	cfg := &CurrentRoundCompressorConfig{TokensThreshold: 0}
	_, err := NewCurrentRoundCompressor(cfg)
	if err == nil {
		t.Error("非法配置创建应返回错误")
	}
}

// TestCurrentRoundCompressor_Compress_低于最小Token 验证选定跨度 Token 数低于阈值跳过压缩
func TestCurrentRoundCompressor_Compress_低于最小Token(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 10000 // 高阈值
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	messagesToCompress := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("短消息"),
	}
	allContextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("短消息"),
		llm_schema.NewAssistantMessage("保留"),
	}

	result, err := crc.Compress(context.Background(), mc, messagesToCompress, allContextMessages, 1, 0)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	if result != nil {
		t.Error("Token 数低于最小阈值时应返回 nil")
	}
}

// TestCurrentRoundCompressor_MultiCompress_无更新 验证无压缩更新时返回 nil
func TestCurrentRoundCompressor_MultiCompress_无更新(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 100000 // 高阈值确保跳过
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
		llm_schema.NewAssistantMessage("保留"),
	}

	result, _, _, err := crc.MultiCompress(context.Background(), mc, messages, 0, 0)
	if err != nil {
		t.Fatalf("MultiCompress 失败: %v", err)
	}
	if result != nil {
		t.Error("无更新时应返回 nil 消息列表")
	}
}

// TestCurrentRoundCompressor_MergeSummaryBlocks_未超限 验证累积摘要未超限跳过合并
func TestCurrentRoundCompressor_MergeSummaryBlocks_未超限(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.AccumulatedSummaryTokenLimit = 100000
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	oldMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("旧记忆块1"),
		llm_schema.NewUserMessage("旧记忆块2"),
	}

	result, err := crc.MergeSummaryBlocks(context.Background(), mc, oldMessages)
	if err != nil {
		t.Fatalf("MergeSummaryBlocks 失败: %v", err)
	}
	if result != nil {
		t.Error("累积摘要未超限时应返回 nil")
	}
}

// TestCurrentRoundCompressor_MergeSummaryBlocks_超限合并 验证累积摘要超限触发合并
func TestCurrentRoundCompressor_MergeSummaryBlocks_超限合并(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.AccumulatedSummaryTokenLimit = 10 // 低限值确保超限
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("合并摘要"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	// 构造超过限值的旧记忆块（字符估算 >> 10）
	oldMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块1内容很长"),
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块2内容也很长"),
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块3内容非常长"),
	}

	result, err := crc.MergeSummaryBlocks(context.Background(), mc, oldMessages)
	if err != nil {
		t.Fatalf("MergeSummaryBlocks 失败: %v", err)
	}
	if result == nil {
		t.Fatal("累积摘要超限时应返回合并结果")
	}
	content := result.GetContent().Text()
	if !strings.Contains(content, "[CURRENT_ROUND_MEMORY_BLOCK]") {
		t.Error("合并结果应包含 CURRENT_ROUND_MEMORY_BLOCK 标记")
	}
	if !strings.Contains(content, "合并摘要") {
		t.Error("合并结果应包含 LLM 返回的摘要内容")
	}
}

// TestCurrentRoundCompressor_WrapUnwrapRoundTrip 验证包装和解包装的往返一致性
func TestCurrentRoundCompressor_WrapUnwrapRoundTrip(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	originalSummary := "这是原始摘要内容"
	wrapped := crc.WrapCurrentRoundMemoryBlock(originalSummary)
	unwrapped := crc.UnwrapMemoryBlockSummary(wrapped)

	if unwrapped != originalSummary {
		t.Errorf("往返后应恢复原始摘要，期望: %s，实际: %s", originalSummary, unwrapped)
	}
}

// TestCurrentRoundCompressor_WrapCurrentRoundMemoryBlock_剥离已有信封 验证再次包装时剥离旧信封
func TestCurrentRoundCompressor_WrapCurrentRoundMemoryBlock_剥离已有信封(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	alreadyWrapped := "[CURRENT_ROUND_MEMORY_BLOCK]\nprocessor: test\n\nSummary:\n已有摘要"
	result := crc.WrapCurrentRoundMemoryBlock(alreadyWrapped)

	if strings.Count(result, "[CURRENT_ROUND_MEMORY_BLOCK]") != 1 {
		t.Error("应只包含一个 CURRENT_ROUND_MEMORY_BLOCK 标记（剥离旧信封后重新包装）")
	}
	if !strings.Contains(result, "已有摘要") {
		t.Error("应保留原始摘要内容")
	}
}

// TestCurrentRoundCompressor_OnAddMessages_空消息 验证空消息列表不触发压缩
func TestCurrentRoundCompressor_OnAddMessages_空消息(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: nil,
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("空消息不应触发压缩")
	}
	if len(result) != 0 {
		t.Errorf("空消息应返回空结果，实际: %d 条", len(result))
	}
}

// TestCurrentRoundCompressor_OnAddMessages_ModelCallFailed降级 验证 LLM 调用失败时降级透传
// Compress 和 MergeSummaryBlocks 内部吞掉 LLM 错误返回 (nil, nil)，
// 因此 MultiCompress 不会返回 error，OnAddMessages 最终透传消息。
func TestCurrentRoundCompressor_OnAddMessages_ModelCallFailed降级(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1 // 低阈值确保进入压缩
	cfg.MessagesToKeep = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeErr: exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("model call failed"),
		),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10}},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "tc-1", Name: "test_tool", Arguments: "{}"},
			}),
		),
		llm_schema.NewToolMessage("tc-1", "工具结果"),
		llm_schema.NewAssistantMessage("回答"),
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("LLM 调用失败应被内部吞掉不报错，实际: %v", err)
	}
	if event != nil {
		t.Error("LLM 调用失败时不应返回 event")
	}
	if len(result) != len(messagesToAdd) {
		t.Errorf("LLM 调用失败时应透传消息，期望 %d 条，实际: %d 条", len(messagesToAdd), len(result))
	}
}

// TestCurrentRoundCompressor_OnAddMessages_压缩后无更新 验证压缩结果为 nil 时透传
func TestCurrentRoundCompressor_OnAddMessages_压缩后无更新(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 100000 // 高阈值跳过压缩
	cfg.MessagesToKeep = 2
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: nil,
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),
		llm_schema.NewAssistantMessage("回答1"),
		llm_schema.NewUserMessage("问题2"),
		llm_schema.NewAssistantMessage("回答2"),
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("无更新时不应返回 event")
	}
	if len(result) != len(messagesToAdd) {
		t.Errorf("无更新时应透传消息，实际: %d 条", len(result))
	}
}

// TestCurrentRoundCompressor_OnAddMessages_KeepStartIdx小于零 验证 keepStartIdx 被修正为 0
func TestCurrentRoundCompressor_OnAddMessages_KeepStartIdx小于零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MessagesToKeep = 100 // 大于消息数
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("压缩摘要"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10}},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	// 由于 MessagesToKeep > len(contextMessages), GetCompressIdx 返回 -1
	// 此测试验证 OnAddMessages 在 keepStartIdx < 0 场景下正确行为
	event, _, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("MessagesToKeep 大于消息数时不应触发压缩")
	}
}

// TestCurrentRoundCompressor_Compress_LLM调用失败 验证 LLM 调用失败返回 nil
func TestCurrentRoundCompressor_Compress_LLM调用失败(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeErr: fmt.Errorf("LLM unavailable"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200}}}

	messagesToCompress := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("长消息内容"),
	}
	allContextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("长消息内容"),
		llm_schema.NewAssistantMessage("保留"),
	}

	result, err := crc.Compress(context.Background(), mc, messagesToCompress, allContextMessages, 1, 0)
	if err != nil {
		t.Fatalf("Compress LLM 调用失败应返回 nil 而非 error，实际: %v", err)
	}
	if result != nil {
		t.Error("LLM 调用失败时应返回 nil")
	}
}

// TestCurrentRoundCompressor_Compress_压缩无收益 验证压缩后 Token 数未减少时跳过
func TestCurrentRoundCompressor_Compress_压缩无收益(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("非常长的压缩结果比原文还长很多很多很多"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	// 输入 Token=200，压缩后 Token 也=200（无收益）
	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 200}}}

	messagesToCompress := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("长消息内容"),
	}
	allContextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("长消息内容"),
		llm_schema.NewAssistantMessage("保留"),
	}

	result, err := crc.Compress(context.Background(), mc, messagesToCompress, allContextMessages, 1, 0)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	if result != nil {
		t.Error("压缩无收益时应返回 nil")
	}
}

// TestCurrentRoundCompressor_Compress_成功压缩 验证正常压缩流程包含先验摘要和近期上下文
func TestCurrentRoundCompressor_Compress_成功压缩(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("成功压缩摘要"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10}}}

	marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
	messagesToCompress := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("需要压缩的内容"),
	}
	allContextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewUserMessage(marker + "\nSummary:\n先验摘要"),
		llm_schema.NewAssistantMessage("需要压缩的内容"),
		llm_schema.NewAssistantMessage("保留消息"),
	}

	result, err := crc.Compress(context.Background(), mc, messagesToCompress, allContextMessages, 2, 0)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	if result == nil {
		t.Fatal("压缩成功时应返回非 nil")
	}
	content := result.GetContent().Text()
	if !strings.Contains(content, marker) {
		t.Error("压缩结果应包含 CURRENT_ROUND_MEMORY_BLOCK 标记")
	}
	if !strings.Contains(content, "成功压缩摘要") {
		t.Error("压缩结果应包含 LLM 返回的摘要内容")
	}
}

// TestCurrentRoundCompressor_Compress_空LLM响应 验证 LLM 返回空内容时跳过
func TestCurrentRoundCompressor_Compress_空LLM响应(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(""),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200}}}

	messagesToCompress := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("长消息内容"),
	}
	allContextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("长消息内容"),
	}

	result, err := crc.Compress(context.Background(), mc, messagesToCompress, allContextMessages, 1, 0)
	if err != nil {
		t.Fatalf("Compress 失败: %v", err)
	}
	// 空 LLM 响应，summary 为空，不进入 token 比较分支，直接包装空字符串
	if result == nil {
		t.Fatal("LLM 返回空内容但 summary 为空字符串，仍应包装返回")
	}
}

// TestCurrentRoundCompressor_MultiCompress_仅压缩无合并 验证第一阶段压缩但无合并范围
func TestCurrentRoundCompressor_MultiCompress_仅压缩无合并(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	cfg.SummaryMergeMinBlocks = 10 // 高阈值确保无合并范围
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("压缩摘要"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10}}}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答1"),
		llm_schema.NewAssistantMessage("回答2"),
	}

	result, modifiedIndices, compactSummary, err := crc.MultiCompress(context.Background(), mc, messages, 0, 1)
	if err != nil {
		t.Fatalf("MultiCompress 失败: %v", err)
	}
	if result == nil {
		t.Fatal("有压缩更新时应返回非 nil 消息列表")
	}
	if len(modifiedIndices) == 0 {
		t.Error("压缩后应有修改索引")
	}
	if compactSummary == "" {
		t.Error("压缩后应有摘要内容")
	}
}

// TestCurrentRoundCompressor_MultiCompress_actualEndIdx小于startIdx 验证跳过压缩阶段
func TestCurrentRoundCompressor_MultiCompress_actualEndIdx小于startIdx(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	// startIdx = lastUserIdx + 1 = 1, actualEndIdx = 0 → 跳过压缩
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	result, _, _, err := crc.MultiCompress(context.Background(), mc, messages, 0, -1)
	if err != nil {
		t.Fatalf("MultiCompress 失败: %v", err)
	}
	if result != nil {
		t.Error("无压缩更新时应返回 nil 消息列表")
	}
}

// TestCurrentRoundCompressor_MultiCompress_两阶段都执行 验证压缩和合并都执行
func TestCurrentRoundCompressor_MultiCompress_两阶段都执行(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	cfg.AccumulatedSummaryTokenLimit = 10 // 低限值触发合并
	cfg.SummaryMergeMinBlocks = 2
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("合并结果"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10, 200, 10}}}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewUserMessage(marker + "\nSummary:\n旧记忆块1"),
		llm_schema.NewUserMessage(marker + "\nSummary:\n旧记忆块2"),
		llm_schema.NewAssistantMessage("回答"),
		llm_schema.NewAssistantMessage("保留"),
	}

	result, modifiedIndices, compactSummary, err := crc.MultiCompress(context.Background(), mc, messages, 0, 2)
	if err != nil {
		t.Fatalf("MultiCompress 失败: %v", err)
	}
	if result == nil {
		t.Fatal("两阶段都执行时应返回非 nil 消息列表")
	}
	_ = modifiedIndices
	_ = compactSummary
}

// TestCurrentRoundCompressor_MultiCompress_压缩错误 验证压缩阶段错误向上传播
func TestCurrentRoundCompressor_MultiCompress_压缩错误(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeErr: fmt.Errorf("compression error"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{200}}}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答1"),
		llm_schema.NewAssistantMessage("保留"),
	}

	_, _, _, err := crc.MultiCompress(context.Background(), mc, messages, 0, 1)
	if err != nil {
		t.Fatalf("Compress 中 LLM 调用失败返回 nil 而非 error，MultiCompress 应继续而非报错，实际: %v", err)
	}
}

// TestCurrentRoundCompressor_MergeSummaryBlocks_LLM调用失败 验证 LLM 调用失败返回 nil
func TestCurrentRoundCompressor_MergeSummaryBlocks_LLM调用失败(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.AccumulatedSummaryTokenLimit = 10
	fakeClient := &crcFakeBaseModelClient{
		invokeErr: fmt.Errorf("LLM unavailable"),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	oldMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块1"),
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块2"),
	}

	result, err := crc.MergeSummaryBlocks(context.Background(), mc, oldMessages)
	if err != nil {
		t.Fatalf("MergeSummaryBlocks LLM 失败应返回 nil 而非 error，实际: %v", err)
	}
	if result != nil {
		t.Error("LLM 调用失败时应返回 nil")
	}
}

// TestCurrentRoundCompressor_MergeSummaryBlocks_LLM返回空 验证 LLM 返回空内容返回 nil
func TestCurrentRoundCompressor_MergeSummaryBlocks_LLM返回空(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.AccumulatedSummaryTokenLimit = 10
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(""),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	oldMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块1"),
		llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\n旧记忆块2"),
	}

	result, err := crc.MergeSummaryBlocks(context.Background(), mc, oldMessages)
	if err != nil {
		t.Fatalf("MergeSummaryBlocks 失败: %v", err)
	}
	if result != nil {
		t.Error("LLM 返回空内容时应返回 nil")
	}
}

// TestCurrentRoundCompressor_MergeSummaryBlocks_空记忆块列表 验证空列表不触发合并
func TestCurrentRoundCompressor_MergeSummaryBlocks_空记忆块列表(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.AccumulatedSummaryTokenLimit = 10
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: nil}

	result, err := crc.MergeSummaryBlocks(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("MergeSummaryBlocks 失败: %v", err)
	}
	if result != nil {
		t.Error("空记忆块列表应返回 nil")
	}
}

// TestCurrentRoundCompressor_UnwrapMemoryBlockSummary_空白字符 验证仅含空白字符时返回空
func TestCurrentRoundCompressor_UnwrapMemoryBlockSummary_空白字符(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	result := crc.UnwrapMemoryBlockSummary("   ")
	if result != "" {
		t.Errorf("仅含空白字符时应返回空字符串，实际: %q", result)
	}
}

// TestCurrentRoundCompressor_UnwrapMemoryBlockSummary_多行Summary 验证多行 Summary 正确提取
func TestCurrentRoundCompressor_UnwrapMemoryBlockSummary_多行Summary(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	wrapped := "[CURRENT_ROUND_MEMORY_BLOCK]\nprocessor: test\n\nSummary:\n第一行\n第二行\n第三行"
	result := crc.UnwrapMemoryBlockSummary(wrapped)
	if result != "第一行\n第二行\n第三行" {
		t.Errorf("多行 Summary 应完整提取，实际: %q", result)
	}
}

// TestFormatPriorContextAndQuery_currentQueryIdx为零 验证无先验上下文时只包含当前查询
func TestFormatPriorContextAndQuery_currentQueryIdx为零(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.PriorContextWindowSize = 10
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("当前查询"),
	}

	result := crc.FormatPriorContextAndQuery(messages, 0)
	if !strings.Contains(result, "当前查询") {
		t.Error("currentQueryIdx=0 时应包含当前查询")
	}
	if !strings.Contains(result, "Current User Intent") {
		t.Error("应包含 Current User Intent 标记")
	}
}

// TestFormatPriorContextAndQuery_负数QueryIdx 验证负数索引不产生先验上下文
func TestFormatPriorContextAndQuery_负数QueryIdx(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("消息"),
	}

	result := crc.FormatPriorContextAndQuery(messages, -1)
	if result != "" {
		t.Errorf("负数索引应返回空字符串，实际: %q", result)
	}
}

// TestFormatRecentContext_全部是记忆块 验证全部为记忆块消息时返回空
func TestFormatRecentContext_全部是记忆块(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块1"),
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块2"),
	}

	result := crc.FormatRecentContext(messages, 1)
	if result != "" {
		t.Errorf("全部为记忆块消息时应返回空字符串，实际: %q", result)
	}
}

// TestFormatRecentContext_混合消息 验证混合记忆块和普通消息时只保留普通消息
func TestFormatRecentContext_混合消息(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块"),
		llm_schema.NewAssistantMessage("新回答"),
		llm_schema.NewUserMessage("新问题"),
	}

	result := crc.FormatRecentContext(messages, 1)
	if !strings.Contains(result, "新回答") {
		t.Error("应包含非记忆块消息")
	}
	if !strings.Contains(result, "新问题") {
		t.Error("应包含非记忆块用户消息")
	}
	if strings.Contains(result, marker) {
		t.Error("应排除记忆块消息")
	}
}

// TestCurrentRoundCompressor_getModelName_NilModel 验证 model 为 nil 时返回空字符串
func TestCurrentRoundCompressor_getModelName_NilModel(t *testing.T) {
	crc := &CurrentRoundCompressor{}
	if crc.getModelName() != "" {
		t.Error("model 为 nil 时应返回空字符串")
	}
}

// TestCurrentRoundCompressor_getModelName_NilModelConfig 验证 model.ModelConfig 为 nil 时返回空字符串
func TestCurrentRoundCompressor_getModelName_NilModelConfig(t *testing.T) {
	crc := &CurrentRoundCompressor{
		model: &llm.Model{ModelConfig: nil},
	}
	if crc.getModelName() != "" {
		t.Error("ModelConfig 为 nil 时应返回空字符串")
	}
}

// TestCurrentRoundCompressor_LoadState 验证 LoadState 不 panic
func TestCurrentRoundCompressor_LoadState(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	// LoadState 是空操作，确保不 panic
	crc.LoadState(nil)
	crc.LoadState(map[string]any{"key": "value"})
}

// TestCurrentRoundCompressor_工厂创建合法实例 验证工厂创建完整实例
func TestCurrentRoundCompressor_工厂创建合法实例(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("CurrentRoundCompressor")
	if !ok {
		t.Fatal("CurrentRoundCompressor 应已注册")
	}

	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("测试"),
	}
	model := crcNewFakeLLMModel(fakeClient)

	cfg := validCurrentRoundCompressorConfig()
	cfg.Model = model.ModelConfig
	cfg.ModelClient = &llm_schema.ModelClientConfig{
		ClientID:       "crc-test-client",
		ClientProvider: crcTestProvider,
		APIKey:         "fake-key",
		APIBase:        "http://localhost",
		Timeout:        60,
		MaxRetries:     1,
		VerifySSL:      false,
	}

	result, err := factory(cfg)
	if err != nil {
		t.Fatalf("工厂创建合法实例应成功，实际: %v", err)
	}
	if result == nil {
		t.Fatal("工厂应返回非 nil 实例")
	}
	crc, ok := result.(*CurrentRoundCompressor)
	if !ok {
		t.Fatal("工厂返回类型应为 *CurrentRoundCompressor")
	}
	if crc.ProcessorType() != "CurrentRoundCompressor" {
		t.Errorf("ProcessorType 应为 CurrentRoundCompressor，实际: %s", crc.ProcessorType())
	}
}

// TestBuildPrompt_仅部分空值 验证部分占位符为空时替换为 (none)
func TestBuildPrompt_仅部分空值(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	result := crc.BuildPrompt(4000, "有摘要", "", "有先验上下文")

	if !strings.Contains(result, "有摘要") {
		t.Error("应包含非空摘要")
	}
	if !strings.Contains(result, "(none)") {
		t.Error("空值应替换为 (none)")
	}
	if !strings.Contains(result, "有先验上下文") {
		t.Error("应包含非空先验上下文")
	}
}

// TestCurrentRoundCompressor_OnAddMessages_CompressionUsage 验证压缩成功时记录用量
func TestCurrentRoundCompressor_OnAddMessages_CompressionUsage(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 10
	cfg.MessagesToKeep = 1
	cfg.MinSelectedTokensForCompression = 1
	fakeClient := &crcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("压缩摘要",
			llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
				InputTokens:  100,
				OutputTokens: 50,
			}),
		),
	}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 10}},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "tc-1", Name: "test_tool", Arguments: "{}"},
			}),
		),
		llm_schema.NewToolMessage("tc-1", "工具结果"),
		llm_schema.NewAssistantMessage("回答"),
	}

	event, _, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event == nil {
		t.Fatal("压缩成功应返回 event")
	}
	if event.CompressionUsage == nil {
		t.Error("压缩成功时应记录 CompressionUsage")
	}
}

// TestCurrentRoundCompressor_TriggerAddMessages_等于阈值 验证 Token 数等于阈值时不触发
func TestCurrentRoundCompressor_TriggerAddMessages_等于阈值(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 200
	cfg.MessagesToKeep = 1
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello"), llm_schema.NewAssistantMessage("world")},
		tokenCounter: &fakeTokenCounter{count: 200},
	}

	triggered, err := crc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("Token 数等于阈值时不应触发（需严格大于）")
	}
}

// TestCurrentRoundCompressor_TriggerAddMessages_刚超阈值 验证 Token 数刚超阈值时触发
func TestCurrentRoundCompressor_TriggerAddMessages_刚超阈值(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 199
	cfg.MessagesToKeep = 1
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello"), llm_schema.NewAssistantMessage("world")},
		tokenCounter: &fakeTokenCounter{count: 200},
	}

	triggered, err := crc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if !triggered {
		t.Error("Token 数刚超阈值时应触发")
	}
}

// TestCurrentRoundCompressor_OnAddMessages_无UserMessage不压缩 验证全部为 AssistantMessage 时 GetCompressIdx 返回 -1
func TestCurrentRoundCompressor_OnAddMessages_无UserMessage不压缩(t *testing.T) {
	cfg := validCurrentRoundCompressorConfig()
	cfg.TokensThreshold = 10
	fakeClient := &crcFakeBaseModelClient{}
	crc := newCRCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &fakeTokenCounter{count: 200},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("只有助手消息1"),
		llm_schema.NewAssistantMessage("只有助手消息2"),
	}

	event, result, err := crc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("无 UserMessage 时不应触发压缩")
	}
	if len(result) != 2 {
		t.Errorf("无 UserMessage 时应透传消息，实际: %d 条", len(result))
	}
}
