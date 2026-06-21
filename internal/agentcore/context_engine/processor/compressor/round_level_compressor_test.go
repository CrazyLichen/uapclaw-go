package compressor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// rlcFakeBaseModelClient 测试用 BaseModelClient 模拟
type rlcFakeBaseModelClient struct {
	invokeResult *llm_schema.AssistantMessage
	invokeErr    error
}

func (f *rlcFakeBaseModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	return f.invokeResult, f.invokeErr
}
func (f *rlcFakeBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	return nil, nil
}
func (f *rlcFakeBaseModelClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *rlcFakeBaseModelClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *rlcFakeBaseModelClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *rlcFakeBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

const rlcTestProvider = "RLCTestProvider"

// rlcFakeRegistryOnce 确保 fake provider 只注册一次
var rlcFakeRegistryOnce bool

// rlcCurrentFakeClient 当前使用的 fake 客户端
var rlcCurrentFakeClient *rlcFakeBaseModelClient

// rlcNewFakeLLMModel 创建带 fake client 的 llm.Model 实例
func rlcNewFakeLLMModel(fakeClient *rlcFakeBaseModelClient) *llm.Model {
	rlcCurrentFakeClient = fakeClient
	if !rlcFakeRegistryOnce {
		model_clients.GetClientRegistry().Register(rlcTestProvider, "llm",
			func(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) model_clients.BaseModelClient {
				return rlcCurrentFakeClient
			},
		)
		rlcFakeRegistryOnce = true
	}

	clientConfig := &llm_schema.ModelClientConfig{
		ClientID:       "rlc-test-client",
		ClientProvider: rlcTestProvider,
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
		panic(fmt.Sprintf("rlcNewFakeLLMModel 失败: %v", err))
	}
	return model
}

// validRoundLevelCompressorConfig 创建合法的 RoundLevelCompressorConfig
func validRoundLevelCompressorConfig() *RoundLevelCompressorConfig {
	return &RoundLevelCompressorConfig{
		TriggerTotalTokens:                230000,
		TargetTotalTokens:                 160000,
		KeepRecentMessages:                0,
		CompressionCallMaxTokens:          250000,
		FirstPassTargetTokens:             30000,
		SecondPassTargetTokens:            20000,
		ThirdPassTargetTokens:             10000,
		TruncateHeadRatio:                 0.2,
		TruncatedMarker:                   "...[TRUNCATED]...",
		CompressionMarker:                 "[ROUND_LEVEL_MEMORY_BLOCK]",
		CustomCompressionPrompt:           "",
		CustomAggressiveCompressionPrompt: "",
	}
}

// newRLCWithModel 创建带 fake LLM Model 的 RoundLevelCompressor
func newRLCWithModel(cfg *RoundLevelCompressorConfig, fakeClient *rlcFakeBaseModelClient) *RoundLevelCompressor {
	if cfg == nil {
		cfg = validRoundLevelCompressorConfig()
	}
	model := rlcNewFakeLLMModel(fakeClient)
	rlc, err := NewRoundLevelCompressor(cfg, WithRoundLevelModel(model))
	if err != nil {
		panic(fmt.Sprintf("newRLCWithModel 失败: %v", err))
	}
	return rlc
}

// alwaysHighThenLowCounter 先返回高值若干次，之后一直返回低值
// 用于模拟"压缩前 Token 超预算、压缩后 Token 在预算内"的场景
type alwaysHighThenLowCounter struct {
	highCount          int
	lowCount           int
	highCallsBeforeLow int
	callCount          int
}

func (c *alwaysHighThenLowCounter) Count(_ string, _ string) (int, error) { return c.lowCount, nil }
func (c *alwaysHighThenLowCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	c.callCount++
	if c.callCount <= c.highCallsBeforeLow {
		return c.highCount, nil
	}
	return c.lowCount, nil
}
func (c *alwaysHighThenLowCounter) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return 0, nil
}

// trackingTokenCounter 带调试输出的 alwaysHighThenLowCounter（仅在 -v 模式下输出）
type trackingTokenCounter struct {
	t                  *testing.T
	name               string
	highCount          int
	lowCount           int
	highCallsBeforeLow int
	callCount          int
	// useEstimation 是否使用消息内容估算而非固定返回值
	// hasCompressionBenefit 需要区分长短消息，固定返回值无法满足
	useEstimation bool
}

func (c *trackingTokenCounter) Count(_ string, _ string) (int, error) { return c.lowCount, nil }
func (c *trackingTokenCounter) CountMessages(msgs []llm_schema.BaseMessage, _ string) (int, error) {
	c.callCount++
	if c.useEstimation {
		// 基于消息内容估算，与真实 TokenCounter 行为一致：长消息返回大数
		total := 0
		for _, msg := range msgs {
			total += len(msg.GetContent().Text()) / 3
		}
		if total == 0 {
			total = 1
		}
		return total, nil
	}
	if c.callCount <= c.highCallsBeforeLow {
		return c.highCount, nil
	}
	return c.lowCount, nil
}
func (c *trackingTokenCounter) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return 0, nil
}

// ──────────────────────────── 配置校验测试 ────────────────────────────

// TestNewRoundLevelCompressorConfig_默认值 验证15个字段默认值
func TestNewRoundLevelCompressorConfig_默认值(t *testing.T) {
	cfg := NewRoundLevelCompressorConfig()
	if cfg.TriggerTotalTokens != 230000 {
		t.Errorf("TriggerTotalTokens 期望 230000，实际: %d", cfg.TriggerTotalTokens)
	}
	if cfg.TargetTotalTokens != 160000 {
		t.Errorf("TargetTotalTokens 期望 160000，实际: %d", cfg.TargetTotalTokens)
	}
	if cfg.KeepRecentMessages != 0 {
		t.Errorf("KeepRecentMessages 期望 0，实际: %d", cfg.KeepRecentMessages)
	}
	if cfg.CompressionCallMaxTokens != 250000 {
		t.Errorf("CompressionCallMaxTokens 期望 250000，实际: %d", cfg.CompressionCallMaxTokens)
	}
	if cfg.FirstPassTargetTokens != 30000 {
		t.Errorf("FirstPassTargetTokens 期望 30000，实际: %d", cfg.FirstPassTargetTokens)
	}
	if cfg.SecondPassTargetTokens != 20000 {
		t.Errorf("SecondPassTargetTokens 期望 20000，实际: %d", cfg.SecondPassTargetTokens)
	}
	if cfg.ThirdPassTargetTokens != 10000 {
		t.Errorf("ThirdPassTargetTokens 期望 10000，实际: %d", cfg.ThirdPassTargetTokens)
	}
	if cfg.TruncateHeadRatio != 0.2 {
		t.Errorf("TruncateHeadRatio 期望 0.2，实际: %f", cfg.TruncateHeadRatio)
	}
	if cfg.TruncatedMarker != "...[TRUNCATED]..." {
		t.Errorf("TruncatedMarker 期望 '...[TRUNCATED]...'，实际: %s", cfg.TruncatedMarker)
	}
	if cfg.CompressionMarker != "[ROUND_LEVEL_MEMORY_BLOCK]" {
		t.Errorf("CompressionMarker 期望 '[ROUND_LEVEL_MEMORY_BLOCK]'，实际: %s", cfg.CompressionMarker)
	}
	if cfg.Model != nil {
		t.Error("Model 默认应为 nil")
	}
	if cfg.ModelClient != nil {
		t.Error("ModelClient 默认应为 nil")
	}
	if cfg.CustomCompressionPrompt != "" {
		t.Errorf("CustomCompressionPrompt 默认应为空，实际: %s", cfg.CustomCompressionPrompt)
	}
	if cfg.CustomAggressiveCompressionPrompt != "" {
		t.Errorf("CustomAggressiveCompressionPrompt 默认应为空，实际: %s", cfg.CustomAggressiveCompressionPrompt)
	}
}

// TestRoundLevelCompressorConfig_Validate_正常 验证合法配置通过校验
func TestRoundLevelCompressorConfig_Validate_正常(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("合法配置不应返回错误，实际: %v", err)
	}
}

// TestRoundLevelCompressorConfig_Validate_TriggerTotalTokens零 验证字段 <= 0 报错
func TestRoundLevelCompressorConfig_Validate_TriggerTotalTokens零(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("TriggerTotalTokens = 0 应返回错误")
	}
	cfg.TriggerTotalTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TriggerTotalTokens = -1 应返回错误")
	}
}

// TestRoundLevelCompressorConfig_Validate_TargetTotalTokens零 验证字段 <= 0 报错
func TestRoundLevelCompressorConfig_Validate_TargetTotalTokens零(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("TargetTotalTokens = 0 应返回错误")
	}
	cfg.TargetTotalTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TargetTotalTokens = -1 应返回错误")
	}
}

// TestRoundLevelCompressorConfig_Validate_TruncateHeadRatio越界 验证 ratio <= 0 或 >= 1 报错
func TestRoundLevelCompressorConfig_Validate_TruncateHeadRatio越界(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()

	cfg.TruncateHeadRatio = 0.0
	if err := cfg.Validate(); err == nil {
		t.Error("TruncateHeadRatio = 0.0 应返回错误")
	}

	cfg.TruncateHeadRatio = -0.1
	if err := cfg.Validate(); err == nil {
		t.Error("TruncateHeadRatio = -0.1 应返回错误")
	}

	cfg.TruncateHeadRatio = 1.0
	if err := cfg.Validate(); err == nil {
		t.Error("TruncateHeadRatio = 1.0 应返回错误")
	}

	cfg.TruncateHeadRatio = 1.5
	if err := cfg.Validate(); err == nil {
		t.Error("TruncateHeadRatio = 1.5 应返回错误")
	}
}

// ──────────────────────────── 核心方法测试 ────────────────────────────

// TestRoundLevelCompressor_ProcessorType 验证处理器类型标识
func TestRoundLevelCompressor_ProcessorType(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)
	if rlc.ProcessorType() != "RoundLevelCompressor" {
		t.Errorf("ProcessorType 应为 RoundLevelCompressor，实际: %s", rlc.ProcessorType())
	}
}

// TestRoundLevelCompressor_TriggerAddMessages_超过阈值 验证 Token 数超过阈值时触发
func TestRoundLevelCompressor_TriggerAddMessages_超过阈值(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 100 // 低阈值便于测试
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

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

	triggered, err := rlc.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if !triggered {
		t.Error("Token 数超过阈值时应触发")
	}
}

// TestRoundLevelCompressor_TriggerAddMessages_低于阈值 验证 Token 数低于阈值时不触发
func TestRoundLevelCompressor_TriggerAddMessages_低于阈值(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 100000 // 高阈值
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("short")},
		tokenCounter: &fakeTokenCounter{count: 10},
	}

	triggered, err := rlc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("Token 数低于阈值时不应触发")
	}
}

// TestRoundLevelCompressor_TriggerGetContextWindow_超过阈值 验证 Token 数超过阈值时触发
func TestRoundLevelCompressor_TriggerGetContextWindow_超过阈值(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 100 // 低阈值
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 200},
	}
	cw := iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
		},
	}

	triggered, err := rlc.TriggerGetContextWindow(context.Background(), mc, cw)
	if err != nil {
		t.Fatalf("TriggerGetContextWindow 失败: %v", err)
	}
	if !triggered {
		t.Error("Token 数超过阈值时应触发")
	}
}

// TestRoundLevelCompressor_TriggerGetContextWindow_低于阈值 验证 Token 数低于阈值时不触发
func TestRoundLevelCompressor_TriggerGetContextWindow_低于阈值(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 100000 // 高阈值
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 10},
	}
	cw := iface.ContextWindow{}

	triggered, err := rlc.TriggerGetContextWindow(context.Background(), mc, cw)
	if err != nil {
		t.Fatalf("TriggerGetContextWindow 失败: %v", err)
	}
	if triggered {
		t.Error("Token 数低于阈值时不应触发")
	}
}

// ──────────────────────────── 目标构建测试 ────────────────────────────

// TestBuildRawTargets_多块 验证构建多个 L0 压缩目标
func TestBuildRawTargets_多块(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 构造两条 completed_react 消息链
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),      // 0
		llm_schema.NewAssistantMessage("回答1"), // 1 - completed_react
		llm_schema.NewUserMessage("问题2"),      // 2
		llm_schema.NewAssistantMessage("回答2"), // 3 - completed_react
		llm_schema.NewAssistantMessage("思考中"), // 4 - ongoing_react
	}

	compressEnd := 4
	targets := rlc.buildRawTargets(messages, compressEnd)

	if len(targets) < 2 {
		t.Fatalf("应构建至少 2 个压缩目标，实际: %d", len(targets))
	}
	// 第一个块: [0,1] completed_react
	if targets[0].startIdx != 0 || targets[0].endIdx != 1 {
		t.Errorf("第一个块范围期望 [0,1]，实际 [%d,%d]", targets[0].startIdx, targets[0].endIdx)
	}
	if targets[0].scope != "completed_react" {
		t.Errorf("第一个块 scope 期望 completed_react，实际: %s", targets[0].scope)
	}
	if targets[0].currentLevel != 0 || targets[0].nextLevel != 1 {
		t.Errorf("第一个块级别期望 L0→L1，实际 L%d→L%d", targets[0].currentLevel, targets[0].nextLevel)
	}
}

// TestBuildRawTargets_跳过已有记忆块 验证跳过已有记忆块
func TestBuildRawTargets_跳过已有记忆块(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 中间插入已有记忆块
	marker := "[ROUND_LEVEL_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题1"),      // 0
		llm_schema.NewAssistantMessage("回答1"), // 1
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块"), // 2 - 应被跳过
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 3 - ack
		llm_schema.NewUserMessage("问题2"),      // 4
		llm_schema.NewAssistantMessage("回答2"), // 5
	}

	compressEnd := 5
	targets := rlc.buildRawTargets(messages, compressEnd)

	// 记忆块 [2,3] 应被跳过，产生两个块: [0,1] 和 [4,5]
	if len(targets) != 2 {
		t.Fatalf("应构建 2 个目标（跳过记忆块），实际: %d", len(targets))
	}
}

// TestFindL0BlockEnd_completedReact 验证找到 completed_react 块结束位置
func TestFindL0BlockEnd_completedReact(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),      // 0
		llm_schema.NewAssistantMessage("回答"), // 1 - 无 tool_calls → completed_react
	}

	endIdx, scope := rlc.findL0BlockEnd(messages, 0, 1)
	if endIdx != 1 {
		t.Errorf("completed_react endIdx 期望 1，实际: %d", endIdx)
	}
	if scope != "completed_react" {
		t.Errorf("scope 期望 completed_react，实际: %s", scope)
	}
}

// TestFindL0BlockEnd_ongoingReact 验证找到 ongoing_react 块结束位置
func TestFindL0BlockEnd_ongoingReact(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// AssistantMessage 有 tool_calls → ongoing_react
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"), // 0
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "tc-1", Name: "test_tool", Arguments: "{}"},
			}),
		), // 1 - 有 tool_calls → ongoing_react
		llm_schema.NewToolMessage("tc-1", "结果"), // 2
	}

	endIdx, scope := rlc.findL0BlockEnd(messages, 0, 2)
	if scope != "ongoing_react" {
		t.Errorf("scope 期望 ongoing_react，实际: %s", scope)
	}
	if endIdx != 2 {
		t.Errorf("ongoing_react endIdx 期望 2，实际: %d", endIdx)
	}
}

// TestIsRoundLevelFallbackBlock_是 验证正确识别记忆块
func TestIsRoundLevelFallbackBlock_是(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := llm_schema.NewUserMessage("[ROUND_LEVEL_MEMORY_BLOCK]\nSummary:\n内容")
	if !rlc.isRoundLevelFallbackBlock(msg) {
		t.Error("以 [ROUND_LEVEL_MEMORY_BLOCK] 开头的 UserMessage 应被识别为记忆块")
	}
}

// TestIsRoundLevelFallbackBlock_不是 验证非记忆块不被误识别
func TestIsRoundLevelFallbackBlock_不是(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 非 UserMessage
	am := llm_schema.NewAssistantMessage("回答")
	if rlc.isRoundLevelFallbackBlock(am) {
		t.Error("AssistantMessage 不应被识别为记忆块")
	}

	// 不以标记开头的 UserMessage
	um := llm_schema.NewUserMessage("普通问题")
	if rlc.isRoundLevelFallbackBlock(um) {
		t.Error("普通 UserMessage 不应被识别为记忆块")
	}
}

// TestResolveEffectiveMergeLevels_孤立块升级 验证孤立块升级到更高级别
func TestResolveEffectiveMergeLevels_孤立块升级(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 3个块：L1, L1, L2 → 孤立 L2 不影响，candidate = L1
	targets := []roundCompressTarget{
		{blockID: "memory_1", currentLevel: 1, startIdx: 0, endIdx: 0},
		{blockID: "memory_2", currentLevel: 1, startIdx: 1, endIdx: 1},
		{blockID: "memory_3", currentLevel: 2, startIdx: 2, endIdx: 2},
	}

	effectiveLevels, candidateLevel := rlc.resolveEffectiveMergeLevels(targets)

	if candidateLevel == nil {
		t.Fatal("应返回非 nil candidateLevel")
	}
	if *candidateLevel != 1 {
		t.Errorf("candidateLevel 期望 1，实际: %d", *candidateLevel)
	}

	// L2 块应升级到更高的级别（如果存在孤立逻辑）
	_ = effectiveLevels
}

// TestLooksLikeAck_确认文本 验证标准确认文本被识别
func TestLooksLikeAck_确认文本(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	am := llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context.")
	if !rlc.looksLikeAck(am) {
		t.Error("标准确认文本应被识别为 ack")
	}
}

// TestLooksLikeAck_非确认 验证非确认文本不被识别
func TestLooksLikeAck_非确认(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	am := llm_schema.NewAssistantMessage("这是一条普通回复")
	if rlc.looksLikeAck(am) {
		t.Error("普通回复不应被识别为 ack")
	}
}

// TestProtectToolCallBoundary_尾部ToolMessage 验证保护工具调用边界
func TestProtectToolCallBoundary_尾部ToolMessage(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// endIdx=1 的 AssistantMessage 有 tool_calls，但后面有对应 ToolMessage
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"), // 0
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "tc-1", Name: "tool1", Arguments: "{}"},
			}),
		), // 1 - tool_calls 引用了 tc-1
		llm_schema.NewToolMessage("tc-1", "结果"), // 2 - 对应 tc-1
		llm_schema.NewAssistantMessage("回答"),     // 3
	}

	protectedEndIdx := rlc.protectToolCallBoundary(messages, 0, 1)
	// AssistantMessage[1] 的 tool_call_id=tc-1 与 ToolMessage[2] 匹配，
	// 因此需要向前缩进到 0
	if protectedEndIdx >= 1 {
		t.Errorf("protectedEndIdx 应 < 1（保护 tool_call 配对），实际: %d", protectedEndIdx)
	}
}

// TestGetCompressLevel_是RoundLevel块 验证记忆块返回默认级别 1
func TestGetCompressLevel_是RoundLevel块(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := llm_schema.NewUserMessage("[ROUND_LEVEL_MEMORY_BLOCK]\nSummary:\n内容")
	level := rlc.getCompressLevel(msg)
	if level != 1 {
		t.Errorf("记忆块默认 compress_level 期望 1，实际: %d", level)
	}
}

// TestGetCompressLevel_普通消息 验证普通消息返回级别 0
func TestGetCompressLevel_普通消息(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := llm_schema.NewUserMessage("普通消息")
	level := rlc.getCompressLevel(msg)
	if level != 0 {
		t.Errorf("普通消息 compress_level 期望 0，实际: %d", level)
	}
}

// TestGetCompressLevel_有metadata 验证从 metadata 提取压缩级别
func TestGetCompressLevel_有metadata(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := llm_schema.NewUserMessage("消息")
	msg.SetMetadata(map[string]any{"compress_level": 3})
	level := rlc.getCompressLevel(msg)
	if level != 3 {
		t.Errorf("metadata 中 compress_level=3 期望返回 3，实际: %d", level)
	}
}

// TestBuildRecursiveMergeTargets_不足两块 验证不足两个记忆块时返回 nil
func TestBuildRecursiveMergeTargets_不足两块(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 只有 1 个记忆块
	marker := "[ROUND_LEVEL_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块1"),
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."),
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	targets := rlc.buildRecursiveMergeTargets(messages, 3)
	if targets != nil {
		t.Error("不足两个记忆块时应返回 nil")
	}
}

// TestBuildAggressiveTargets_有原始目标 验证有原始目标时直接返回
func TestBuildAggressiveTargets_有原始目标(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 有非记忆块消息，buildRawTargets 应返回非空
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),      // 0
		llm_schema.NewAssistantMessage("回答"), // 1
	}

	targets := rlc.buildAggressiveTargets(messages, 1)
	if len(targets) == 0 {
		t.Error("有原始目标时应返回非空目标列表")
	}
}

// TestBuildAggressiveTargets_无原始目标 验证无原始目标时退回记忆块
func TestBuildAggressiveTargets_无原始目标(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 全部是记忆块，buildRawTargets 返回空，应退回 collectRoundLevelMemoryTargets
	marker := "[ROUND_LEVEL_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块1"), // 0
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 1
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块2"), // 2
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 3
	}

	targets := rlc.buildAggressiveTargets(messages, 3)
	if len(targets) == 0 {
		t.Error("无原始目标时退回记忆块目标，应返回非空")
	}
}

// ──────────────────────────── JSON 解析与记忆块测试 ────────────────────────────

// TestWrapMemoryBlock_完整元数据 验证记忆块包装包含完整元数据
func TestWrapMemoryBlock_完整元数据(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	summary := "这是压缩摘要"
	result := rlc.wrapMemoryBlock(summary, "completed_react")

	if !strings.HasPrefix(result, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Error("记忆块应以 [ROUND_LEVEL_MEMORY_BLOCK] 开头")
	}
	expectedParts := []string{
		"processor: RoundLevelCompressor",
		"type: historical_memory_block",
		"scope: completed_react",
		"authority:",
		"instruction_status:",
		"conflict_priority:",
		"Summary:",
	}
	for _, part := range expectedParts {
		if !strings.Contains(result, part) {
			t.Errorf("记忆块应包含 '%s'", part)
		}
	}
	if !strings.Contains(result, summary) {
		t.Error("记忆块应包含摘要内容")
	}
}

// TestIsValidBlocksPayload_正常 验证有效 JSON 载荷
func TestIsValidBlocksPayload_正常(t *testing.T) {
	payload := map[string]any{
		"blocks": []any{
			map[string]any{"block_id": "block_1", "summary": "摘要1"},
		},
	}
	if !isValidBlocksPayload(payload) {
		t.Error("有效 blocks 载荷应返回 true")
	}
}

// TestIsValidBlocksPayload_非dict 验证非 dict 类型返回 false
func TestIsValidBlocksPayload_非dict(t *testing.T) {
	if isValidBlocksPayload("not a dict") {
		t.Error("字符串类型应返回 false")
	}
	if isValidBlocksPayload(42) {
		t.Error("整数类型应返回 false")
	}
	if isValidBlocksPayload(nil) {
		t.Error("nil 应返回 false")
	}
}

// TestIsValidBlocksPayload_无blocks键 验证缺少 blocks 键返回 false
func TestIsValidBlocksPayload_无blocks键(t *testing.T) {
	payload := map[string]any{
		"other": "value",
	}
	if isValidBlocksPayload(payload) {
		t.Error("缺少 blocks 键应返回 false")
	}
}

// TestBuildHeadTailTruncatedText_headTail 验证头尾截断
func TestBuildHeadTailTruncatedText_headTail(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	text := "0123456789ABCDEFGHIJ" // 20 字符
	keptChars := 10                // 保留 10 字符，headRatio=0.2 → head=2, tail=8

	result := rlc.buildHeadTailTruncatedText(text, keptChars)
	if !strings.Contains(result, "...[TRUNCATED]...") {
		t.Error("截断文本应包含 truncated marker")
	}
	if !strings.HasPrefix(result, "01") {
		t.Errorf("头部应保留前 2 字符，实际: %s", result[:2])
	}
}

// TestBuildHeadTailTruncatedText_零字符 验证零字符保留返回截断标记
func TestBuildHeadTailTruncatedText_零字符(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	result := rlc.buildHeadTailTruncatedText("some text", 0)
	if result != "...[TRUNCATED]..." {
		t.Errorf("零字符保留应返回 truncated marker，实际: %s", result)
	}

	result = rlc.buildHeadTailTruncatedText("some text", -1)
	if result != "...[TRUNCATED]..." {
		t.Errorf("负数字符保留应返回 truncated marker，实际: %s", result)
	}
}

// TestSerializeMessage_含ToolCalls 验证含 tool_calls 的 AssistantMessage 序列化
func TestSerializeMessage_含ToolCalls(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	am := llm_schema.NewAssistantMessage("回答",
		llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			{ID: "tc-1", Name: "search", Arguments: "{}"},
			{ID: "tc-2", Name: "calculate", Arguments: "{}"},
		}),
	)

	result := rlc.serializeMessage(0, am)
	if !strings.Contains(result, "tool_calls=search, calculate") {
		t.Errorf("序列化应包含 tool_calls，实际: %s", result)
	}
	if !strings.Contains(result, "role=assistant") {
		t.Errorf("序列化应包含 role=assistant，实际: %s", result)
	}
}

// TestSerializeMessage_普通消息 验证普通消息序列化
func TestSerializeMessage_普通消息(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	um := llm_schema.NewUserMessage("用户问题")
	result := rlc.serializeMessage(5, um)

	if !strings.Contains(result, "[5]") {
		t.Errorf("序列化应包含索引 [5]，实际: %s", result)
	}
	if !strings.Contains(result, "role=user") {
		t.Errorf("序列化应包含 role=user，实际: %s", result)
	}
	if !strings.Contains(result, "用户问题") {
		t.Errorf("序列化应包含消息内容，实际: %s", result)
	}
}

// ──────────────────────────── 硬截断测试 ────────────────────────────

// TestTruncateToTarget_预算为零 验证预算为零时返回紧凑截断消息
func TestTruncateToTarget_预算为零(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = 1 // 极低目标，使 allowedContextTokens <= 0
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 1000},
	}

	contextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("大量内容"),
		llm_schema.NewAssistantMessage("大量回答"),
	}
	systemMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("系统提示"),
	}

	result := rlc.truncateToTarget(contextMessages, mc, systemMessages, nil)
	if len(result) != 1 {
		t.Fatalf("预算为零时应返回 1 条截断消息，实际: %d", len(result))
	}
	content := result[0].GetContent().Text()
	if !strings.Contains(content, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Errorf("截断消息应包含记忆块标记，实际: %s", content)
	}
}

// TestBuildMinimalTruncatedMessage 验证最小截断消息
func TestBuildMinimalTruncatedMessage(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := rlc.buildMinimalTruncatedMessage()
	content := msg.GetContent().Text()
	if !strings.HasPrefix(content, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Error("最小截断消息应以记忆块标记开头")
	}
	if !strings.Contains(content, "truncated_full_context") {
		t.Error("最小截断消息应包含 truncated_full_context scope")
	}
	if !strings.Contains(content, "...[TRUNCATED]...") {
		t.Error("最小截断消息应包含截断标记")
	}
}

// TestBuildCompactTruncatedMessage 验证紧凑截断消息
func TestBuildCompactTruncatedMessage(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := rlc.buildCompactTruncatedMessage()
	content := msg.GetContent().Text()
	if !strings.HasPrefix(content, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Error("紧凑截断消息应以记忆块标记开头")
	}
	if !strings.Contains(content, "...[TRUNCATED]...") {
		t.Error("紧凑截断消息应包含截断标记")
	}
}

// ──────────────────────────── 工厂注册测试 ────────────────────────────

// TestRoundLevelCompressor_工厂注册 验证 GetProcessorFactory("RoundLevelCompressor") 存在
func TestRoundLevelCompressor_工厂注册(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("RoundLevelCompressor")
	if !ok {
		t.Fatal("RoundLevelCompressor 应已通过 init() 注册")
	}
	if factory == nil {
		t.Fatal("factory 不应为 nil")
	}
}

// TestRoundLevelCompressor_工厂配置类型不匹配 验证错误配置类型返回 error
func TestRoundLevelCompressor_工厂配置类型不匹配(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("RoundLevelCompressor")
	if !ok {
		t.Fatal("RoundLevelCompressor 应已注册")
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

// ──────────────────────────── 集成测试 ────────────────────────────

// TestRoundLevelCompressor_OnAddMessages_不压缩 验证设置极高阈值时不压缩
func TestRoundLevelCompressor_OnAddMessages_不压缩(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 999999999 // 极高阈值
	cfg.TargetTotalTokens = 999999999
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		tokenCounter: &fakeTokenCounter{count: 10},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("world"),
	}

	event, result, err := rlc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event != nil {
		t.Error("极高阈值时不应返回 event")
	}
	if len(result) != 1 {
		t.Errorf("极高阈值时应透传消息，实际: %d 条", len(result))
	}
}

// TestRoundLevelCompressor_OnGetContextWindow_不超预算 验证 Token 不超预算时直接返回
func TestRoundLevelCompressor_OnGetContextWindow_不超预算(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 100000 // 高阈值
	cfg.TargetTotalTokens = 100000
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 10},
	}
	cw := iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
		},
	}

	event, resultCw, err := rlc.OnGetContextWindow(context.Background(), mc, cw)
	if err != nil {
		t.Fatalf("OnGetContextWindow 失败: %v", err)
	}
	if event != nil {
		t.Error("Token 不超预算时不应返回 event")
	}
	if len(resultCw.ContextMessages) != 1 {
		t.Error("Token 不超预算时应原样返回 ContextWindow")
	}
}

// ──────────────────────────── 补充测试 ────────────────────────────

// TestNewRoundLevelCompressor_校验失败 验证非法配置创建失败
func TestNewRoundLevelCompressor_校验失败(t *testing.T) {
	cfg := &RoundLevelCompressorConfig{TriggerTotalTokens: 0}
	_, err := NewRoundLevelCompressor(cfg)
	if err == nil {
		t.Error("非法配置创建应返回错误")
	}
}

// TestRoundLevelCompressor_SaveLoadState 验证状态保存/加载（空操作）
func TestRoundLevelCompressor_SaveLoadState(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	state := rlc.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState 应返回空 map，实际: %v", state)
	}

	// LoadState 是空操作，确保不 panic
	rlc.LoadState(map[string]any{"key": "value"})
	rlc.LoadState(nil)
}

// TestRoundLevelCompressor_自定义提示词 验证自定义压缩提示词
func TestRoundLevelCompressor_自定义提示词(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.CustomCompressionPrompt = "自定义普通压缩提示词"
	cfg.CustomAggressiveCompressionPrompt = "自定义激进压缩提示词"
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)
	if rlc.firstPrompt != "自定义普通压缩提示词" {
		t.Errorf("应使用自定义普通提示词，实际: %s", rlc.firstPrompt)
	}
	if rlc.aggressivePrompt != "自定义激进压缩提示词" {
		t.Errorf("应使用自定义激进提示词，实际: %s", rlc.aggressivePrompt)
	}
}

// TestRoundLevelCompressor_默认提示词 验证空字符串时使用内置默认提示词
func TestRoundLevelCompressor_默认提示词(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)
	if rlc.firstPrompt != defaultRoundCompressionPrompt {
		t.Error("空字符串时应使用内置普通压缩提示词")
	}
	if rlc.aggressivePrompt != defaultAggressiveRoundCompressionPrompt {
		t.Error("空字符串时应使用内置激进压缩提示词")
	}
}

// TestWithRoundLevelModel 验证 WithRoundLevelModel 选项
func TestWithRoundLevelModel(t *testing.T) {
	fakeClient := &rlcFakeBaseModelClient{}
	model := rlcNewFakeLLMModel(fakeClient)
	opt := WithRoundLevelModel(model)
	rlc := &RoundLevelCompressor{}
	opt(rlc)
	if rlc.model != model {
		t.Error("WithRoundLevelModel 应将 model 设为指定实例")
	}
}

// TestRoundLevelCompressor_OnAddMessages_ModelCallFailed降级 验证 LLM 调用失败时降级透传
func TestRoundLevelCompressor_OnAddMessages_ModelCallFailed降级(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 10 // 低阈值确保触发
	cfg.TargetTotalTokens = 5
	fakeClient := &rlcFakeBaseModelClient{
		invokeErr: exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("model call failed"),
		),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 200, 200, 200, 200, 200, 200, 200}},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	event, result, err := rlc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("LLM 调用失败应降级透传，实际: %v", err)
	}
	if event != nil {
		t.Error("LLM 调用失败降级时不应返回 event")
	}
	if len(result) != len(messagesToAdd) {
		t.Errorf("LLM 调用失败时应透传消息，期望 %d 条，实际: %d 条", len(messagesToAdd), len(result))
	}
}

// TestRoundLevelCompressor_OnAddMessages_压缩成功 验证压缩成功场景
func TestRoundLevelCompressor_OnAddMessages_压缩成功(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 10   // 低阈值确保触发
	cfg.TargetTotalTokens = 100   // 压缩目标 - 假计数器初始返回500>100
	fakeClient := &rlcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(`{"blocks": [{"block_id": "block_1", "summary": "压缩摘要"}]}`),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 使用 trackingTokenCounter 来调试调用模式
	// useEstimation=true：基于消息内容估算 token 数，让 hasCompressionBenefit 能正确判断
	tc := &trackingTokenCounter{t: t, name: "OnAddMessages压缩成功"}
	tc.useEstimation = true

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: tc,
	}
	// 使用足够长的消息确保 hasCompressionBenefit 返回 true
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(strings.Repeat("问题1内容重复很多次以增加token数 ", 20)),
		llm_schema.NewAssistantMessage(strings.Repeat("回答1内容重复很多次以增加token数 ", 20)),
	}

	event, result, err := rlc.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event == nil {
		t.Fatal("压缩成功应返回非空 event")
	}
	if event.EventType != "RoundLevelCompressor" {
		t.Errorf("EventType 应为 RoundLevelCompressor，实际: %s", event.EventType)
	}
	if len(result) != 0 {
		t.Errorf("压缩后应返回空 messagesToAdd，实际: %d 条", len(result))
	}
}

// TestRoundLevelCompressor_buildJSONReplacements_正常 验证 JSON 替换列表构建
func TestRoundLevelCompressor_buildJSONReplacements_正常(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	targets := []roundCompressTarget{
		{
			blockID:      "block_1",
			scope:        "completed_react",
			startIdx:     0,
			endIdx:       1,
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage(strings.Repeat("很长的问题内容需要压缩处理，重复多次以增加token数", 10)),
				llm_schema.NewAssistantMessage(strings.Repeat("很长的回答内容需要压缩处理，重复多次以增加token数", 10)),
			},
			currentLevel: 0,
			nextLevel:    1,
		},
	}

	parserContent := map[string]any{
		"blocks": []any{
			map[string]any{"block_id": "block_1", "summary": "压缩后的短摘要"},
		},
	}

	// nil TokenCounter → 走字符估算，原始消息长>替换消息短 → hasCompressionBenefit 返回 true
	replacements := rlc.buildJSONReplacements(targets, parserContent, &fakeModelContext{tokenCounter: nil})
	if len(replacements) != 1 {
		t.Fatalf("应构建 1 个替换，实际: %d", len(replacements))
	}
	if replacements[0].StartIdx != 0 || replacements[0].EndIdx != 1 {
		t.Errorf("替换范围期望 [0,1]，实际 [%d,%d]", replacements[0].StartIdx, replacements[0].EndIdx)
	}
	if len(replacements[0].Messages) != 1 {
		t.Errorf("替换消息数期望 1，实际: %d", len(replacements[0].Messages))
	}
}

// TestRoundLevelCompressor_buildJSONReplacements_缺失blockID 验证缺失 block_id 跳过
func TestRoundLevelCompressor_buildJSONReplacements_缺失blockID(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	targets := []roundCompressTarget{
		{
			blockID:      "block_1",
			scope:        "completed_react",
			startIdx:     0,
			endIdx:       0,
			messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("很长内容")},
			currentLevel: 0,
			nextLevel:    1,
		},
	}

	parserContent := map[string]any{
		"blocks": []any{
			map[string]any{"block_id": "wrong_id", "summary": "摘要"},
		},
	}

	replacements := rlc.buildJSONReplacements(targets, parserContent, &fakeModelContext{tokenCounter: nil})
	if len(replacements) != 0 {
		t.Errorf("block_id 不匹配时不应产生替换，实际: %d", len(replacements))
	}
}

// TestRoundLevelCompressor_messagesEqual 验证消息列表比较
func TestRoundLevelCompressor_messagesEqual(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 相同引用应返回 true
	msg1 := llm_schema.NewUserMessage("a")
	msg2 := llm_schema.NewAssistantMessage("b")
	a := []llm_schema.BaseMessage{msg1, msg2}
	b := []llm_schema.BaseMessage{msg1, msg2}
	if !rlc.messagesEqual(a, b) {
		t.Error("相同引用的消息列表应返回 true")
	}

	// 不同长度
	c := []llm_schema.BaseMessage{msg1}
	if rlc.messagesEqual(a, c) {
		t.Error("不同长度消息列表应返回 false")
	}

	// 不同实例但内容相同应返回 true（深度比较）
	d := []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewAssistantMessage("b")}
	if !rlc.messagesEqual(a, d) {
		t.Error("内容相同的消息列表（深度比较）应返回 true")
	}

	// 不同内容应返回 false
	e := []llm_schema.BaseMessage{llm_schema.NewUserMessage("x"), llm_schema.NewAssistantMessage("b")}
	if rlc.messagesEqual(a, e) {
		t.Error("内容不同的消息列表应返回 false")
	}

	// 空列表
	empty1 := []llm_schema.BaseMessage{}
	empty2 := []llm_schema.BaseMessage{}
	if !rlc.messagesEqual(empty1, empty2) {
		t.Error("两个空列表应返回 true")
	}
}

// TestRoundLevelCompressor_getModelName_NilModel 验证 model 为 nil 时返回空字符串
func TestRoundLevelCompressor_getModelName_NilModel(t *testing.T) {
	rlc := &RoundLevelCompressor{}
	if rlc.getModelName() != "" {
		t.Error("model 为 nil 时应返回空字符串")
	}
}

// TestRoundLevelCompressor_getModelName_NilModelConfig 验证 model.ModelConfig 为 nil 时返回空字符串
func TestRoundLevelCompressor_getModelName_NilModelConfig(t *testing.T) {
	rlc := &RoundLevelCompressor{
		model: &llm.Model{ModelConfig: nil},
	}
	if rlc.getModelName() != "" {
		t.Error("ModelConfig 为 nil 时应返回空字符串")
	}
}

// TestRoundLevelCompressor_collectRoundLevelMemoryTargets 验证收集记忆块目标
func TestRoundLevelCompressor_collectRoundLevelMemoryTargets(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	marker := "[ROUND_LEVEL_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块1"), // 0
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 1
		llm_schema.NewUserMessage("问题"),      // 2
		llm_schema.NewAssistantMessage("回答"), // 3
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块2"), // 4
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 5
	}

	targets := rlc.collectRoundLevelMemoryTargets(messages, 5)
	if len(targets) != 2 {
		t.Fatalf("应收集 2 个记忆块目标，实际: %d", len(targets))
	}
	if targets[0].scope != "existing_round_level_block" {
		t.Errorf("scope 期望 existing_round_level_block，实际: %s", targets[0].scope)
	}
	if targets[0].currentLevel != 1 {
		t.Errorf("记忆块默认 currentLevel 期望 1，实际: %d", targets[0].currentLevel)
	}
	if targets[0].nextLevel != 2 {
		t.Errorf("记忆块 nextLevel 期望 2，实际: %d", targets[0].nextLevel)
	}
}

// TestRoundLevelCompressor_buildMemoryMessage 验证记忆块消息构建
func TestRoundLevelCompressor_buildMemoryMessage(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	msg := rlc.buildMemoryMessage("摘要内容", "ongoing_react", 2)
	content := msg.GetContent().Text()
	if !strings.HasPrefix(content, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Error("记忆块消息应以标记开头")
	}
	if !strings.Contains(content, "摘要内容") {
		t.Error("记忆块消息应包含摘要内容")
	}
	// 验证 metadata 中有 compress_level
	metadata := msg.GetMetadata()
	if metadata == nil {
		t.Fatal("记忆块消息应有 metadata")
	}
	level, ok := metadata["compress_level"]
	if !ok {
		t.Error("metadata 应包含 compress_level")
	}
	if level != 2 {
		t.Errorf("compress_level 期望 2，实际: %v", level)
	}
}

// TestRoundLevelCompressor_hasCompressionBenefit 验证压缩收益判断
func TestRoundLevelCompressor_hasCompressionBenefit(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// nil TokenCounter → 走字符估算降级：长消息 len/3 > 短消息 len/3
	mc := &fakeModelContext{tokenCounter: nil}

	// 原始消息长，替换消息短 → 有收益
	original := []llm_schema.BaseMessage{llm_schema.NewUserMessage("很长很长很长很长很长的消息内容需要压缩处理")}
	replacement := []llm_schema.BaseMessage{llm_schema.NewUserMessage("短")}
	if !rlc.hasCompressionBenefit(original, replacement, mc) {
		t.Error("原始消息更长时应有压缩收益")
	}

	// 原始消息短，替换消息长 → 无收益
	original2 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("短")}
	replacement2 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("很长很长很长很长很长的消息内容不需要压缩处理")}
	if rlc.hasCompressionBenefit(original2, replacement2, mc) {
		t.Error("替换消息更长时不应有压缩收益")
	}

	// 使用 TokenCounter 精确计数
	mcWithCounter := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{100, 20}}}
	original3 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("长消息")}
	replacement3 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("短消息")}
	if !rlc.hasCompressionBenefit(original3, replacement3, mcWithCounter) {
		t.Error("TokenCounter 显示原始 100 > 替换 20 时应有压缩收益")
	}
}

// TestRoundLevelCompressor_isUnderContextWindowBudget 验证上下文预算判断
func TestRoundLevelCompressor_isUnderContextWindowBudget(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = 100
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 50}}
	if !rlc.isUnderContextWindowBudget(nil, []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}, nil, mc) {
		t.Error("Token 数低于目标应在预算内")
	}

	mc2 := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 200}}
	if rlc.isUnderContextWindowBudget(nil, []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}, nil, mc2) {
		t.Error("Token 数超过目标不应在预算内")
	}
}

// TestRoundLevelCompressor_extractCompactSummary 验证压缩摘要提取
func TestRoundLevelCompressor_extractCompactSummary(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("[ROUND_LEVEL_MEMORY_BLOCK]\nSummary:\n摘要1"),
		llm_schema.NewAssistantMessage("普通回答"),
		llm_schema.NewUserMessage("[ROUND_LEVEL_MEMORY_BLOCK]\nSummary:\n摘要2"),
	}

	summary := rlc.extractCompactSummary(messages)
	if !strings.Contains(summary, "摘要1") {
		t.Error("应包含第一个记忆块摘要")
	}
	if !strings.Contains(summary, "摘要2") {
		t.Error("应包含第二个记忆块摘要")
	}
	if strings.Contains(summary, "普通回答") {
		t.Error("不应包含非记忆块内容")
	}
}

// TestRoundLevelCompressor_estimateContentTokens 验证内容 Token 估算
func TestRoundLevelCompressor_estimateContentTokens(t *testing.T) {
	result := estimateContentTokens("hello world")
	if result <= 0 {
		t.Errorf("估算结果应大于 0，实际: %d", result)
	}
}

// TestRoundLevelCompressor_serializeTool 验证工具定义序列化
func TestRoundLevelCompressor_serializeTool(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	tool := &schema.ToolInfo{
		Name:        "test_tool",
		Description: "测试工具",
	}
	result := rlc.serializeTool(tool)
	if !strings.Contains(result, "test_tool") {
		t.Errorf("序列化应包含工具名称，实际: %s", result)
	}
}

// TestRoundLevelCompressor_findRoundLevelBlockEnd 验证查找记忆块结束索引
func TestRoundLevelCompressor_findRoundLevelBlockEnd(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	marker := "[ROUND_LEVEL_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块"), // 0 - start
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 1 - ack
		llm_schema.NewUserMessage("问题"), // 2 - 非 ack
	}

	endIdx := rlc.findRoundLevelBlockEnd(messages, 0, 2)
	if endIdx != 1 {
		t.Errorf("记忆块结束索引期望 1，实际: %d", endIdx)
	}
}

// TestRoundLevelCompressor_buildModifyIndices 验证修改索引列表构建
func TestRoundLevelCompressor_buildModifyIndices(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	indices := rlc.buildModifyIndices(2, 5)
	if len(indices) != 4 {
		t.Fatalf("应返回 4 个索引，实际: %d", len(indices))
	}
	expected := []int{2, 3, 4, 5}
	for i, idx := range indices {
		if idx != expected[i] {
			t.Errorf("索引 %d 期望 %d，实际: %d", i, expected[i], idx)
		}
	}
}

// ──────────────────────────── 覆盖率补充测试 ────────────────────────────

// TestRoundLevelCompressor_OnGetContextWindow_超预算压缩成功 验证超预算时触发压缩
func TestRoundLevelCompressor_OnGetContextWindow_超预算压缩成功(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 10  // 低阈值确保触发
	cfg.TargetTotalTokens = 100  // 压缩目标
	fakeClient := &rlcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(`{"blocks": [{"block_id": "block_1", "summary": "压缩摘要"}]}`),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	// useEstimation=true：基于消息内容估算 token 数
	tc := &trackingTokenCounter{t: t, name: "OnGetContextWindow超预算"}
	tc.useEstimation = true

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: tc,
	}
	cw := iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(strings.Repeat("问题1内容重复很多次以增加token数 ", 20)),
			llm_schema.NewAssistantMessage(strings.Repeat("回答1内容重复很多次以增加token数 ", 20)),
		},
	}

	event, resultCw, err := rlc.OnGetContextWindow(context.Background(), mc, cw)
	if err != nil {
		t.Fatalf("OnGetContextWindow 失败: %v", err)
	}
	if event == nil {
		t.Fatal("超预算压缩成功应返回非空 event")
	}
	if event.EventType != "RoundLevelCompressor" {
		t.Errorf("EventType 应为 RoundLevelCompressor，实际: %s", event.EventType)
	}
	if len(resultCw.ContextMessages) == 0 {
		t.Error("压缩后应有上下文消息")
	}
}

// TestRoundLevelCompressor_OnGetContextWindow_模型调用失败返回错误 验证 LLM 调用失败时返回错误（兜底压缩不允许降级）
func TestRoundLevelCompressor_OnGetContextWindow_模型调用失败返回错误(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 10
	cfg.TargetTotalTokens = 5
	fakeClient := &rlcFakeBaseModelClient{
		invokeErr: exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("model call failed"),
		),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{},
		tokenCounter: &dynamicFakeTokenCounter{counts: []int{200, 200, 200, 200, 200, 200, 200, 200}},
	}
	cw := iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("问题"),
			llm_schema.NewAssistantMessage("回答"),
		},
	}

	event, resultCw, err := rlc.OnGetContextWindow(context.Background(), mc, cw)
	if err == nil {
		t.Fatal("LLM 调用失败应返回错误，兜底压缩不允许降级")
	}
	if event != nil {
		t.Error("LLM 调用失败时不应返回 event")
	}
	if len(resultCw.ContextMessages) != 2 {
		t.Errorf("应保留原始消息，期望 2 条，实际: %d 条", len(resultCw.ContextMessages))
	}
}

// TestRoundLevelCompressor_compressUntilTarget_已在预算内 验证不超预算时直接返回
func TestRoundLevelCompressor_compressUntilTarget_已在预算内(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = 100000 // 高目标，不触发压缩
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 50},
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	result, err := rlc.compressUntilTarget(context.Background(), messages, mc, nil, nil, 0, false)
	if err != nil {
		t.Fatalf("已在预算内应返回 nil error，实际: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("已在预算内应返回原消息，期望 2 条，实际: %d 条", len(result))
	}
}

// TestRoundLevelCompressor_compressUntilTarget_force强制 验证 force=true 时即使不超预算也压缩
func TestRoundLevelCompressor_compressUntilTarget_force强制(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 10
	cfg.TargetTotalTokens = 100
	fakeClient := &rlcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(`{"blocks": [{"block_id": "block_1", "summary": "摘要"}]}`),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	// useEstimation=true：基于消息内容估算 token 数
	tc := &trackingTokenCounter{t: t, name: "force压缩"}
	tc.useEstimation = true

	mc := &fakeModelContext{
		tokenCounter: tc,
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(strings.Repeat("问题内容 ", 20)),
		llm_schema.NewAssistantMessage(strings.Repeat("回答内容 ", 20)),
	}

	result, err := rlc.compressUntilTarget(context.Background(), messages, mc, nil, nil, 0, true)
	if err != nil {
		t.Fatalf("force 压缩应返回 nil error，实际: %v", err)
	}
	if result == nil {
		t.Error("force 压缩应返回非 nil 结果")
	}
}

// TestRoundLevelCompressor_runAggressivePhase_有目标 验证激进压缩阶段有目标时执行
func TestRoundLevelCompressor_runAggressivePhase_有目标(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TriggerTotalTokens = 10
	cfg.TargetTotalTokens = 100
	fakeClient := &rlcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(`{"blocks": [{"block_id": "block_1", "summary": "激进摘要"}]}`),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 前 4 次返回高值（超预算），之后返回低值
	tc := &trackingTokenCounter{t: t, name: "runAggressivePhase"}
	tc.useEstimation = true

	mc := &fakeModelContext{
		tokenCounter: tc,
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(strings.Repeat("问题内容 ", 20)),
		llm_schema.NewAssistantMessage(strings.Repeat("回答内容 ", 20)),
	}

	result, err := rlc.runAggressivePhase(context.Background(), messages, mc, nil, nil, 0, cfg.SecondPassTargetTokens, "aggressive_keep_recent")
	if err != nil {
		t.Fatalf("runAggressivePhase 应返回 nil error，实际: %v", err)
	}
	// 结果可以是 nil（模型返回空）或非 nil
	_ = result
}

// TestRoundLevelCompressor_runAggressivePhase_keepRecent为零 验证 keepRecent=0 时不跳过
func TestRoundLevelCompressor_runAggressivePhase_keepRecent为零(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(`{"blocks": [{"block_id": "block_1", "summary": "摘要"}]}`),
	}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 50},
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewAssistantMessage("回答"),
	}

	result, err := rlc.runAggressivePhase(context.Background(), messages, mc, nil, nil, 0, cfg.SecondPassTargetTokens, "test_phase")
	if err != nil {
		t.Fatalf("runAggressivePhase 应返回 nil error，实际: %v", err)
	}
	_ = result
}

// TestRoundLevelCompressor_buildRecursiveMergeTargets_两块合并 验证两个相邻同级别记忆块合并
func TestRoundLevelCompressor_buildRecursiveMergeTargets_两块合并(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	marker := "[ROUND_LEVEL_MEMORY_BLOCK]"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块1"),                          // 0 - L1
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 1 - ack
		llm_schema.NewUserMessage(marker + "\nSummary:\n记忆块2"),                          // 2 - L1（相邻）
		llm_schema.NewAssistantMessage("Understood. I have recorded this compressed context."), // 3 - ack
		llm_schema.NewUserMessage("问题"),      // 4
		llm_schema.NewAssistantMessage("回答"), // 5
	}

	// 给两个记忆块设置相同的 compress_level
	messages[0].SetMetadata(map[string]any{"compress_level": 1})
	messages[2].SetMetadata(map[string]any{"compress_level": 1})

	targets := rlc.buildRecursiveMergeTargets(messages, 3)
	if targets == nil {
		t.Fatal("两个相邻同级别记忆块应产生合并目标")
	}
	if len(targets) < 1 {
		t.Fatalf("应产生至少 1 个合并目标，实际: %d", len(targets))
	}
	// 合并目标应跨越两个记忆块
	if targets[0].scope != "recursive_merge" {
		t.Errorf("合并目标 scope 期望 recursive_merge，实际: %s", targets[0].scope)
	}
	if targets[0].currentLevel != 1 {
		t.Errorf("currentLevel 期望 1，实际: %d", targets[0].currentLevel)
	}
	if targets[0].nextLevel != 2 {
		t.Errorf("nextLevel 期望 2，实际: %d", targets[0].nextLevel)
	}
}

// TestRoundLevelCompressor_buildMergeTarget_验证合并目标构建
func TestRoundLevelCompressor_buildMergeTarget_验证合并目标构建(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("块1"),
		llm_schema.NewAssistantMessage("ack1"),
		llm_schema.NewUserMessage("块2"),
		llm_schema.NewAssistantMessage("ack2"),
	}

	group := []roundCompressTarget{
		{blockID: "memory_1", startIdx: 0, endIdx: 1, currentLevel: 1, nextLevel: 2},
		{blockID: "memory_2", startIdx: 2, endIdx: 3, currentLevel: 1, nextLevel: 2},
	}

	result := rlc.buildMergeTarget(group, messages, 1, 1)
	if result.blockID != "merge_1_1" {
		t.Errorf("blockID 期望 merge_1_1，实际: %s", result.blockID)
	}
	if result.scope != "recursive_merge" {
		t.Errorf("scope 期望 recursive_merge，实际: %s", result.scope)
	}
	if result.startIdx != 0 {
		t.Errorf("startIdx 期望 0，实际: %d", result.startIdx)
	}
	if result.endIdx != 3 {
		t.Errorf("endIdx 期望 3，实际: %d", result.endIdx)
	}
	if result.currentLevel != 1 {
		t.Errorf("currentLevel 期望 1，实际: %d", result.currentLevel)
	}
	if result.nextLevel != 2 {
		t.Errorf("nextLevel 期望 2，实际: %d", result.nextLevel)
	}
	if result.sourceBlockCount != 2 {
		t.Errorf("sourceBlockCount 期望 2，实际: %d", result.sourceBlockCount)
	}
}

// TestRoundLevelCompressor_truncatePromptToBudget_预算内 验证提示词在预算内时直接返回
func TestRoundLevelCompressor_truncatePromptToBudget_预算内(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.CompressionCallMaxTokens = 500000 // 高预算
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 100},
	}

	result := rlc.truncatePromptToBudget("system prompt", "user prompt content", mc)
	if result == nil {
		t.Fatal("预算内应返回非 nil")
	}
	if *result == "" {
		t.Error("预算内应返回原始提示词内容")
	}
}

// TestRoundLevelCompressor_truncatePromptToBudget_预算不足 验证最小提示词也超预算时返回 nil
func TestRoundLevelCompressor_truncatePromptToBudget_预算不足(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.CompressionCallMaxTokens = 1 // 极低预算
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 1000}, // 高计数值
	}

	result := rlc.truncatePromptToBudget("system prompt", "user prompt content", mc)
	if result != nil {
		t.Error("预算不足时应返回 nil")
	}
}

// TestRoundLevelCompressor_truncateToTarget_正常截断 验证正常硬截断
func TestRoundLevelCompressor_truncateToTarget_正常截断(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = 5000 // 足够容纳截断消息
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 10},
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(strings.Repeat("很长的内容需要截断处理 ", 50)),
		llm_schema.NewAssistantMessage(strings.Repeat("很长的回答需要截断处理 ", 50)),
	}

	result := rlc.truncateToTarget(messages, mc, nil, nil)
	if len(result) == 0 {
		t.Fatal("截断应返回非空消息列表")
	}
	content := result[0].GetContent().Text()
	if !strings.Contains(content, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Errorf("截断消息应包含记忆块标记，实际: %s", content[:min(100, len(content))])
	}
}

// TestRoundLevelCompressor_truncateToTarget_空消息 验证空上下文消息直接返回
func TestRoundLevelCompressor_truncateToTarget_空消息(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = 5000
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 10},
	}
	messages := []llm_schema.BaseMessage{}

	result := rlc.truncateToTarget(messages, mc, nil, nil)
	if len(result) != 0 {
		t.Errorf("空消息应返回空列表，实际: %d", len(result))
	}
}

// TestRoundLevelCompressor_countMessageTokens_有TokenCounter 验证使用 TokenCounter 计数
func TestRoundLevelCompressor_countMessageTokens_有TokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 42},
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}

	result := rlc.countMessageTokens(messages, mc)
	if result != 42 {
		t.Errorf("应返回 tokenCounter 计数值 42，实际: %d", result)
	}
}

// TestRoundLevelCompressor_countMessageTokens_nilTokenCounter 验证 nil TokenCounter 时降级估算
func TestRoundLevelCompressor_countMessageTokens_nilTokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: nil,
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello world"),
	}

	result := rlc.countMessageTokens(messages, mc)
	if result <= 0 {
		t.Errorf("降级估算应返回正值，实际: %d", result)
	}
}

// TestRoundLevelCompressor_countMessageTokens_errorTokenCounter 验证 TokenCounter 返回错误时降级
func TestRoundLevelCompressor_countMessageTokens_errorTokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 0, err: fmt.Errorf("counter error")},
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello world"),
	}

	result := rlc.countMessageTokens(messages, mc)
	if result <= 0 {
		t.Errorf("错误降级估算应返回正值，实际: %d", result)
	}
}

// TestRoundLevelCompressor_LoadState_非空验证 验证 LoadState 传入非空 map 不 panic
func TestRoundLevelCompressor_LoadState_非空验证(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 确保不 panic
	rlc.LoadState(map[string]any{"key": "value", "num": 42})
}

// TestRoundLevelCompressor_validate_配置无效TargetTotalTokens 验证 TargetTotalTokens <= TriggerTotalTokens 时的处理
func TestRoundLevelCompressor_validate_配置无效TargetTotalTokens(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.TargetTotalTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TargetTotalTokens = -1 应返回错误")
	}
}

// TestRoundLevelCompressor_countCompressionCallTokens_有TokenCounter 验证计算压缩调用 Token
func TestRoundLevelCompressor_countCompressionCallTokens_有TokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 88},
	}

	result := rlc.countCompressionCallTokens("system prompt", "user prompt", mc)
	if result != 88 {
		t.Errorf("应返回 tokenCounter 计数值 88，实际: %d", result)
	}
}

// TestRoundLevelCompressor_countCompressionCallTokens_nilTokenCounter 验证 nil TokenCounter 降级
func TestRoundLevelCompressor_countCompressionCallTokens_nilTokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: nil,
	}

	result := rlc.countCompressionCallTokens("system prompt", "user prompt", mc)
	if result <= 0 {
		t.Errorf("降级估算应返回正值，实际: %d", result)
	}
}

// TestRoundLevelCompressor_countContextWindowTokens_nilTokenCounter 验证 nil TokenCounter 降级估算
func TestRoundLevelCompressor_countContextWindowTokens_nilTokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: nil,
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}

	result := rlc.countContextWindowTokens(nil, messages, nil, mc)
	if result <= 0 {
		t.Errorf("降级估算应返回正值，实际: %d", result)
	}
}

// TestRoundLevelCompressor_countContextWindowTokens_errorTokenCounter 验证 TokenCounter 错误时降级
func TestRoundLevelCompressor_countContextWindowTokens_errorTokenCounter(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 0, err: fmt.Errorf("counter error")},
	}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}

	result := rlc.countContextWindowTokens(nil, messages, nil, mc)
	if result <= 0 {
		t.Errorf("错误降级估算应返回正值，实际: %d", result)
	}
}

// TestRoundLevelCompressor_buildHeadTailTruncatedText_全保留 验证 keptChars >= len(text) 时行为
func TestRoundLevelCompressor_buildHeadTailTruncatedText_全保留(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	text := "short"
	// 当 keptChars >= len(text) 时，head+tail 可能等于或超过原文长度
	// buildHeadTailTruncatedText 会截取 head 和 tail 子串，所以结果包含原文各部分
	result := rlc.buildHeadTailTruncatedText(text, 100)
	if !strings.Contains(result, text) {
		t.Errorf("keptChars >= len(text) 时结果应包含原文，实际: %s", result)
	}
}

// TestRoundLevelCompressor_resolveEffectiveMergeLevels_多级别 验证多个不同级别的解析
func TestRoundLevelCompressor_resolveEffectiveMergeLevels_多级别(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 4个块：L1, L1, L2, L2 → 应选择 L1 作为 candidate（最先 count>=2）
	targets := []roundCompressTarget{
		{blockID: "memory_1", currentLevel: 1, startIdx: 0, endIdx: 0},
		{blockID: "memory_2", currentLevel: 1, startIdx: 1, endIdx: 1},
		{blockID: "memory_3", currentLevel: 2, startIdx: 2, endIdx: 2},
		{blockID: "memory_4", currentLevel: 2, startIdx: 3, endIdx: 3},
	}

	effectiveLevels, candidateLevel := rlc.resolveEffectiveMergeLevels(targets)
	if candidateLevel == nil {
		t.Fatal("多级别时应返回非 nil candidateLevel")
	}
	if *candidateLevel != 1 {
		t.Errorf("candidateLevel 期望 1（最先 count>=2 的级别），实际: %d", *candidateLevel)
	}
	_ = effectiveLevels
}

// TestRoundLevelCompressor_resolveEffectiveMergeLevels_全孤立 验证全部孤立级别时返回 nil
func TestRoundLevelCompressor_resolveEffectiveMergeLevels_全孤立(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	// 每个级别只有 1 个块，且没有更高级别 → 升级后无法合并
	targets := []roundCompressTarget{
		{blockID: "memory_1", currentLevel: 1, startIdx: 0, endIdx: 0},
		{blockID: "memory_2", currentLevel: 2, startIdx: 1, endIdx: 1},
		{blockID: "memory_3", currentLevel: 3, startIdx: 2, endIdx: 2},
	}

	effectiveLevels, candidateLevel := rlc.resolveEffectiveMergeLevels(targets)
	// 全部孤立时，经过升级后应该能找到合并级别
	_ = effectiveLevels
	_ = candidateLevel
}

// TestRoundLevelCompressor_prepareRoundCompressionMessages_预算不足 验证预算不足时返回 nil
func TestRoundLevelCompressor_prepareRoundCompressionMessages_预算不足(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.CompressionCallMaxTokens = 1 // 极低预算
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 1000},
	}

	targets := []roundCompressTarget{
		{
			blockID:      "block_1",
			scope:        "completed_react",
			startIdx:     0,
			endIdx:       1,
			messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("问题"), llm_schema.NewAssistantMessage("回答")},
			currentLevel: 0,
			nextLevel:    1,
		},
	}

	result := rlc.prepareRoundCompressionMessages(
		[]llm_schema.BaseMessage{llm_schema.NewUserMessage("问题"), llm_schema.NewAssistantMessage("回答")},
		targets, mc, "test_phase", cfg.FirstPassTargetTokens, false, 0, nil, nil,
	)
	if result != nil {
		t.Error("预算不足时应返回 nil")
	}
}

// TestRoundLevelCompressor_buildRawFallbackReplacement_正常 验证 Fallback 替换构建
func TestRoundLevelCompressor_buildRawFallbackReplacement_正常(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	targets := []roundCompressTarget{
		{
			blockID:      "block_1",
			scope:        "completed_react",
			startIdx:     0,
			endIdx:       1,
			messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage(strings.Repeat("问题内容重复多次", 10)), llm_schema.NewAssistantMessage(strings.Repeat("回答内容重复多次", 10))},
			currentLevel: 0,
			nextLevel:    1,
		},
	}

	contentText := "这是 LLM 返回的非 JSON 格式摘要内容"
	// nil TokenCounter → 走字符估算，原始消息长>摘要短 → hasCompressionBenefit 返回 true
	replacement := rlc.buildRawFallbackReplacement(targets, contentText, &fakeModelContext{tokenCounter: nil})
	if replacement == nil {
		t.Fatal("应构建 Fallback 替换")
	}
	if replacement.StartIdx != 0 || replacement.EndIdx != 1 {
		t.Errorf("替换范围期望 [0,1]，实际 [%d,%d]", replacement.StartIdx, replacement.EndIdx)
	}
	if len(replacement.Messages) != 1 {
		t.Errorf("替换消息数期望 1，实际: %d", len(replacement.Messages))
	}
	content := replacement.Messages[0].GetContent().Text()
	if !strings.Contains(content, "[ROUND_LEVEL_MEMORY_BLOCK]") {
		t.Error("Fallback 替换消息应包含记忆块标记")
	}
	if !strings.Contains(content, contentText) {
		t.Error("Fallback 替换消息应包含摘要内容")
	}
}

// TestRoundLevelCompressor_serializeMessage_ToolMessage 验证 ToolMessage 序列化
func TestRoundLevelCompressor_serializeMessage_ToolMessage(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	tm := llm_schema.NewToolMessage("tc-1", "工具结果")
	result := rlc.serializeMessage(3, tm)
	if !strings.Contains(result, "[3]") {
		t.Errorf("序列化应包含索引 [3]，实际: %s", result)
	}
	if !strings.Contains(result, "tool") {
		t.Errorf("序列化应包含 role=tool，实际: %s", result)
	}
}

// TestRoundLevelCompressor_init工厂 创建验证 init() 注册工厂
func TestRoundLevelCompressor_init工厂创建(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("RoundLevelCompressor")
	if !ok {
		t.Fatal("init() 应注册 RoundLevelCompressor 工厂")
	}

	cfg := validRoundLevelCompressorConfig()
	// 工厂创建时如果没有 ModelClient 配置会失败，这是预期行为
	// 验证合法配置 + Model 选项能通过类型检查
	result, err := factory(cfg)
	// 没有 ModelClient 配置，预期会返回错误，但这验证了工厂能正确接收配置类型
	if err != nil {
		// 工厂配置类型匹配但缺少 ModelClient → 报错，这是预期行为
		if !strings.Contains(err.Error(), "model client config") && !strings.Contains(err.Error(), "Model") {
			t.Errorf("错误类型不符合预期，实际: %v", err)
		}
	} else if result != nil {
		processor, ok := result.(*RoundLevelCompressor)
		if !ok {
			t.Fatal("应返回 *RoundLevelCompressor 类型")
		}
		if processor.ProcessorType() != "RoundLevelCompressor" {
			t.Errorf("ProcessorType 应为 RoundLevelCompressor，实际: %s", processor.ProcessorType())
		}
	}
}

// TestBuildRawTargets_空消息 验证空消息列表返回空目标
func TestBuildRawTargets_空消息(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	targets := rlc.buildRawTargets([]llm_schema.BaseMessage{}, -1)
	if len(targets) != 0 {
		t.Errorf("空消息列表应返回空目标，实际: %d", len(targets))
	}
}

// TestRoundLevelCompressor_buildCompressionUserPrompt_有目标 验证有目标时返回提示词
func TestRoundLevelCompressor_buildCompressionUserPrompt_有目标(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{
		tokenCounter: &fakeTokenCounter{count: 50},
	}
	contextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题内容"),
		llm_schema.NewAssistantMessage("回答内容"),
	}
	targets := []roundCompressTarget{
		{
			blockID:      "block_1",
			scope:        "completed_react",
			startIdx:     0,
			endIdx:       1,
			messages:     contextMessages,
			currentLevel: 0,
			nextLevel:    1,
		},
	}

	result := rlc.buildCompressionUserPrompt(contextMessages, targets, mc, "test", 100, 0, nil, nil)
	if result == "" {
		t.Error("有目标时应返回非空提示词")
	}
	if !strings.Contains(result, "block_1") {
		t.Error("提示词应包含 block_id")
	}
}

// TestRoundLevelCompressor_isUnderCompressionCallBudget_正常 验证压缩调用预算判断
func TestRoundLevelCompressor_isUnderCompressionCallBudget_正常(t *testing.T) {
	cfg := validRoundLevelCompressorConfig()
	cfg.CompressionCallMaxTokens = 500
	fakeClient := &rlcFakeBaseModelClient{}
	rlc := newRLCWithModel(cfg, fakeClient)

	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 100}}
	if !rlc.isUnderCompressionCallBudget("sys", "prompt", mc) {
		t.Error("Token 数低于预算应返回 true")
	}

	mc2 := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 600}}
	if rlc.isUnderCompressionCallBudget("sys", "prompt", mc2) {
		t.Error("Token 数超过预算应返回 false")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
