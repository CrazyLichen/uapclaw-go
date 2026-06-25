package compressor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeModelContext 测试用 ModelContext 模拟
type fakeModelContext struct {
	messages     []llm_schema.BaseMessage
	tokenCounter token.TokenCounter
}

func (f *fakeModelContext) Len() int { return len(f.messages) }
func (f *fakeModelContext) GetMessages(_ int, _ bool) []llm_schema.BaseMessage {
	return f.messages
}
func (f *fakeModelContext) SetMessages(messages []llm_schema.BaseMessage, _ bool) {
	f.messages = messages
}
func (f *fakeModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage { return nil }
func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...iface.Option) error {
	return nil
}
func (f *fakeModelContext) AddMessages(_ context.Context, _ llm_schema.BaseMessage, _ ...iface.Option) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (f *fakeModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage,
	_ []*schema.ToolInfo, _ int, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (f *fakeModelContext) Statistic() *iface.ContextStats                       { return nil }
func (f *fakeModelContext) SessionID() string                                    { return "test-session" }
func (f *fakeModelContext) ContextID() string                                    { return "test-context" }
func (f *fakeModelContext) TokenCounter() token.TokenCounter                     { return f.tokenCounter }
func (f *fakeModelContext) ReloaderTool() tool.Tool                              { return nil }
func (f *fakeModelContext) WorkspaceDir() string                                 { return "" }
func (f *fakeModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade)                     {}
func (f *fakeModelContext) GetSessionRef() sessioninterfaces.SessionFacade                      { return nil }
func (f *fakeModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (f *fakeModelContext) SaveState() map[string]any                            { return nil }
func (f *fakeModelContext) LoadState(_ map[string]any)                           {}
func (f *fakeModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return "", nil
}

// fakeTokenCounter 测试用 TokenCounter 模拟
type fakeTokenCounter struct {
	count int
	err   error
}

func (f *fakeTokenCounter) Count(_ string, _ string) (int, error) { return f.count, f.err }
func (f *fakeTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	return f.count, f.err
}
func (f *fakeTokenCounter) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return f.count, f.err
}

// dynamicFakeTokenCounter 动态返回不同计数的 TokenCounter
type dynamicFakeTokenCounter struct {
	counts []int
	index  int
	err    error
}

func (d *dynamicFakeTokenCounter) Count(_ string, _ string) (int, error) { return 0, d.err }
func (d *dynamicFakeTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	if d.index < len(d.counts) {
		count := d.counts[d.index]
		d.index++
		return count, nil
	}
	return 0, nil
}
func (d *dynamicFakeTokenCounter) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return 0, d.err
}

// testConfig 测试用 ProcessorConfig 实现（从 processor 包重新定义，因原 testConfig 不可跨包访问）
type testConfig struct {
	Name string
}

func (c *testConfig) ProcessorType() string { return c.Name }
func (c *testConfig) Validate() error       { return nil }

// validDialogueCompressorConfig 创建合法的 DialogueCompressorConfig
func validDialogueCompressorConfig() *DialogueCompressorConfig {
	return &DialogueCompressorConfig{
		TokensThreshold:         1000,
		MessagesThreshold:       10,
		MessagesToKeep:          2,
		KeepLastRound:           true,
		CompressionTargetTokens: 500,
		CustomCompressionPrompt: "",
		Model:                   &llm_schema.ModelRequestConfig{ModelName: "test-model"},
		ModelClient:             &llm_schema.ModelClientConfig{},
	}
}

// newTestDialogueCompressor 创建测试用 DialogueCompressor（跳过模型创建）
func newTestDialogueCompressor(cfg *DialogueCompressorConfig) *DialogueCompressor {
	if cfg == nil {
		cfg = validDialogueCompressorConfig()
	}
	bp := processor.NewBaseProcessor(cfg)
	compressedPrompt := cfg.CustomCompressionPrompt
	if compressedPrompt == "" {
		compressedPrompt = defaultCompressionPrompt
	}
	return &DialogueCompressor{
		BaseProcessor:           bp,
		model:                   nil, // 测试不需要实际模型
		tokenThreshold:          cfg.TokensThreshold,
		messageNumThreshold:     cfg.MessagesThreshold,
		messagesToKeep:          cfg.MessagesToKeep,
		keepLastRound:           cfg.KeepLastRound,
		compressionTargetTokens: cfg.CompressionTargetTokens,
		compressedPrompt:        compressedPrompt,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDialogueCompressorConfig_Validate_合法配置 验证合法配置通过校验
func TestDialogueCompressorConfig_Validate_合法配置(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("合法配置不应返回错误，实际: %v", err)
	}
}

// TestDialogueCompressorConfig_Validate_TokensThreshold非法 验证 TokensThreshold <= 0
func TestDialogueCompressorConfig_Validate_TokensThreshold非法(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.TokensThreshold = 0
	if err := cfg.Validate(); err == nil {
		t.Error("TokensThreshold = 0 应返回错误")
	}

	cfg.TokensThreshold = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TokensThreshold = -1 应返回错误")
	}
}

// TestDialogueCompressorConfig_Validate_MessagesThreshold非法 验证 MessagesThreshold < 0
func TestDialogueCompressorConfig_Validate_MessagesThreshold非法(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesThreshold = -1
	if err := cfg.Validate(); err == nil {
		t.Error("MessagesThreshold = -1 应返回错误")
	}
}

// TestDialogueCompressorConfig_Validate_MessagesToKeep非法 验证 MessagesToKeep < 0
func TestDialogueCompressorConfig_Validate_MessagesToKeep非法(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesToKeep = -1
	if err := cfg.Validate(); err == nil {
		t.Error("MessagesToKeep = -1 应返回错误")
	}
}

// TestDialogueCompressorConfig_Validate_CompressionTargetTokens非法 验证 CompressionTargetTokens <= 0
func TestDialogueCompressorConfig_Validate_CompressionTargetTokens非法(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.CompressionTargetTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("CompressionTargetTokens = 0 应返回错误")
	}

	cfg.CompressionTargetTokens = -100
	if err := cfg.Validate(); err == nil {
		t.Error("CompressionTargetTokens = -100 应返回错误")
	}
}

// TestDialogueCompressorConfig_Validate_MessagesThreshold零 验证 MessagesThreshold = 0 合法（不启用）
func TestDialogueCompressorConfig_Validate_MessagesThreshold零(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesThreshold = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("MessagesThreshold = 0 应合法，实际: %v", err)
	}
}

// TestGetCompressPairs_纯对话 验证不含工具调用的配对
func TestGetCompressPairs_纯对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	pairs := GetCompressPairs(messages)
	if len(pairs) != 1 {
		t.Fatalf("期望 1 个配对，实际 %d", len(pairs))
	}
	if pairs[0] != [2]int{0, 1} {
		t.Errorf("配对 = %v, want [0, 1]", pairs[0])
	}
}

// TestGetCompressPairs_含工具调用 验证含工具调用的轮次配对
func TestGetCompressPairs_含工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}
	pairs := GetCompressPairs(messages)
	if len(pairs) != 1 {
		t.Fatalf("期望 1 个配对，实际 %d", len(pairs))
	}
	if pairs[0] != [2]int{0, 3} {
		t.Errorf("配对 = %v, want [0, 3]", pairs[0])
	}
}

// TestGetCompressPairs_多轮对话 验证多轮配对
func TestGetCompressPairs_多轮对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}
	pairs := GetCompressPairs(messages)
	if len(pairs) != 2 {
		t.Fatalf("期望 2 个配对，实际 %d", len(pairs))
	}
	if pairs[0] != [2]int{0, 1} {
		t.Errorf("配对1 = %v, want [0, 1]", pairs[0])
	}
	if pairs[1] != [2]int{2, 3} {
		t.Errorf("配对2 = %v, want [2, 3]", pairs[1])
	}
}

// TestGetCompressPairs_未完成轮次 验证未完成的轮次不计入
func TestGetCompressPairs_未完成轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
	}
	pairs := GetCompressPairs(messages)
	if len(pairs) != 0 {
		t.Errorf("未完成轮次应返回 0 个配对，实际 %d", len(pairs))
	}
}

// TestGetCompressPairs_空消息 验证空消息列表
func TestGetCompressPairs_空消息(t *testing.T) {
	pairs := GetCompressPairs(nil)
	if len(pairs) != 0 {
		t.Errorf("空消息应返回 0 个配对，实际 %d", len(pairs))
	}
}

// TestGetCompressIdx_无保留 验证不保留时返回消息长度
func TestGetCompressIdx_无保留(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesToKeep = 0
	cfg.KeepLastRound = false
	dc := newTestDialogueCompressor(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
	}
	idx := dc.GetCompressIdx(messages)
	if idx != 3 {
		t.Errorf("不保留时应返回消息长度 3，实际: %d", idx)
	}
}

// TestGetCompressIdx_保留最近N条 验证保留最近 N 条
func TestGetCompressIdx_保留最近N条(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesToKeep = 2
	cfg.KeepLastRound = false
	dc := newTestDialogueCompressor(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}
	idx := dc.GetCompressIdx(messages)
	if idx != 2 {
		t.Errorf("保留 2 条时应返回索引 2，实际: %d", idx)
	}
}

// TestGetCompressIdx_保留最后一轮 验证 KeepLastRound 限制
func TestGetCompressIdx_保留最后一轮(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesToKeep = 0
	cfg.KeepLastRound = true
	dc := newTestDialogueCompressor(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}
	idx := dc.GetCompressIdx(messages)
	if idx != 5 {
		t.Errorf("KeepLastRound 时应返回索引 5，实际: %d", idx)
	}
}

// TestWrapMemoryBlock 验证记忆块包装格式
func TestWrapMemoryBlock(t *testing.T) {
	summary := "这是压缩摘要内容"
	result := WrapMemoryBlock(summary)

	if !strings.HasPrefix(result, "[DIALOGUE_MEMORY_BLOCK]") {
		t.Error("记忆块应以 [DIALOGUE_MEMORY_BLOCK] 开头")
	}
	if !strings.Contains(result, "processor: DialogueCompressor") {
		t.Error("记忆块应包含 processor: DialogueCompressor")
	}
	if !strings.Contains(result, "type: historical_memory_block") {
		t.Error("记忆块应包含 type: historical_memory_block")
	}
	if !strings.Contains(result, summary) {
		t.Error("记忆块应包含原始摘要内容")
	}
	if !strings.Contains(result, "Summary:") {
		t.Error("记忆块应包含 Summary: 标记")
	}
}

// TestIsValidBlocksPayload_有效载荷 验证有效 blocks JSON
func TestIsValidBlocksPayload_有效载荷(t *testing.T) {
	payload := map[string]any{
		"blocks": []any{
			map[string]any{"block_id": "react_1", "summary": "摘要1"},
			map[string]any{"block_id": "react_2", "summary": "摘要2"},
		},
	}
	if !IsValidBlocksPayload(payload) {
		t.Error("有效载荷应返回 true")
	}
}

// TestIsValidBlocksPayload_缺少blocks键 验证缺少 blocks 键
func TestIsValidBlocksPayload_缺少blocks键(t *testing.T) {
	payload := map[string]any{"foo": "bar"}
	if IsValidBlocksPayload(payload) {
		t.Error("缺少 blocks 键应返回 false")
	}
}

// TestIsValidBlocksPayload_blocks非数组 验证 blocks 不是数组
func TestIsValidBlocksPayload_blocks非数组(t *testing.T) {
	payload := map[string]any{"blocks": "not_array"}
	if IsValidBlocksPayload(payload) {
		t.Error("blocks 不是数组应返回 false")
	}
}

// TestIsValidBlocksPayload_非map类型 验证非 map 类型
func TestIsValidBlocksPayload_非map类型(t *testing.T) {
	if IsValidBlocksPayload("string") {
		t.Error("字符串应返回 false")
	}
	if IsValidBlocksPayload(42) {
		t.Error("整数应返回 false")
	}
	if IsValidBlocksPayload(nil) {
		t.Error("nil 应返回 false")
	}
}

// TestSerializeMessage_UserMessage 验证 UserMessage 序列化
func TestSerializeMessage_UserMessage(t *testing.T) {
	msg := llm_schema.NewUserMessage("你好世界")
	result := SerializeMessage(0, msg)

	if !strings.Contains(result, "[0]") {
		t.Error("应包含索引 [0]")
	}
	if !strings.Contains(result, "role=user") {
		t.Error("应包含 role=user")
	}
	if !strings.Contains(result, "content=你好世界") {
		t.Error("应包含 content=你好世界")
	}
}

// TestSerializeMessage_AssistantMessageWithToolCalls 验证含 tool_calls 的 AssistantMessage 序列化
func TestSerializeMessage_AssistantMessageWithToolCalls(t *testing.T) {
	msg := llm_schema.NewAssistantMessage("查询中",
		llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			{ID: "call_2", Name: "get_time", Arguments: `{}`},
		}),
	)
	result := SerializeMessage(5, msg)

	if !strings.Contains(result, "[5]") {
		t.Error("应包含索引 [5]")
	}
	if !strings.Contains(result, "role=assistant") {
		t.Error("应包含 role=assistant")
	}
	if !strings.Contains(result, "tool_calls=get_weather, get_time") {
		t.Error("应包含 tool_calls 名称列表，实际: " + result)
	}
}

// TestSerializeMessage_ToolMessage 验证 ToolMessage 序列化
func TestSerializeMessage_ToolMessage(t *testing.T) {
	msg := llm_schema.NewToolMessage("call_1", "晴天 25°C")
	result := SerializeMessage(3, msg)

	if !strings.Contains(result, "tool_call_id=call_1") {
		t.Error("应包含 tool_call_id=call_1")
	}
}

// TestEstimateContentTokens_字符串 验证字符串内容 Token 估算
func TestEstimateContentTokens_字符串(t *testing.T) {
	content := "123456789" // 9 字符 → 9/3 = 3
	tokens := processor.EstimateContentTokens(content)
	if tokens != 3 {
		t.Errorf("9 字符应估算为 3 tokens，实际: %d", tokens)
	}
}

// TestEstimateContentTokens_非字符串 验证非字符串内容 Token 估算
func TestEstimateContentTokens_非字符串(t *testing.T) {
	content := map[string]string{"key": "value"}
	tokens := processor.EstimateContentTokens(content)
	if tokens <= 0 {
		t.Errorf("非字符串内容应返回正数，实际: %d", tokens)
	}
}

// TestEstimateContentTokens_空字符串 验证空字符串返回 0
func TestEstimateContentTokens_空字符串(t *testing.T) {
	tokens := processor.EstimateContentTokens("")
	if tokens != 0 {
		t.Errorf("空字符串应返回 0 tokens，实际: %d", tokens)
	}
}

// TestHasCompressionBenefit_有收益 验证压缩有收益
func TestHasCompressionBenefit_有收益(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// TokenCounter 返回 error，fallback 到字符估算
	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 0, err: fmt.Errorf("编码器不可用")}}

	// 字符估算：原始: "hello" (5/3=1) + "world" (5/3=1) = 2
	// 替换: "sum" (3/3=1) = 1
	original := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewUserMessage("world"),
	}
	replacement := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("sum"),
	}

	result := dc.HasCompressionBenefit(mc, original, replacement)
	if !result {
		t.Error("压缩后 Token 少于原始时应返回 true")
	}
}

// TestHasCompressionBenefit_无收益 验证压缩无收益
func TestHasCompressionBenefit_无收益(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// TokenCounter 返回 error，fallback 到字符估算
	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 0, err: fmt.Errorf("编码器不可用")}}

	// 字符估算：原始: "ab" (2/3=0) → 压缩后也接近 0，无收益
	original := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("ab"),
	}
	replacement := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("xy"),
	}

	result := dc.HasCompressionBenefit(mc, original, replacement)
	if result {
		t.Error("原始 Token 为 0 时应返回 false")
	}
}

// TestHasCompressionBenefit_用TokenCounter 验证使用 TokenCounter 计算
func TestHasCompressionBenefit_用TokenCounter(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// TokenCounter 返回：原始 100 → 压缩 30
	dynamicCounter := &dynamicFakeTokenCounter{counts: []int{100, 30}}
	mc := &fakeModelContext{tokenCounter: dynamicCounter}

	original := []llm_schema.BaseMessage{llm_schema.NewUserMessage("long message")}
	replacement := []llm_schema.BaseMessage{llm_schema.NewUserMessage("short")}

	result := dc.HasCompressionBenefit(mc, original, replacement)
	if !result {
		t.Error("TokenCounter 显示压缩有收益时应返回 true")
	}
}

// TestBuildCompressTargets_无可压缩轮次 验证无可压缩轮次时返回 nil
func TestBuildCompressTargets_无可压缩轮次(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// 只有 2 条消息的轮次（blockMessageCount = 2），不满足 > 2 的条件
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	targets := dc.BuildCompressTargets(messages)
	if targets != nil {
		t.Errorf("无可压缩轮次应返回 nil，实际: %v", targets)
	}
}

// TestBuildCompressTargets_有可压缩轮次 验证有可压缩轮次时返回目标
func TestBuildCompressTargets_有可压缩轮次(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// 含工具调用的轮次：User(0) → Assistant(tool_calls)(1) → Tool(2) → Assistant(3)
	// blockMessageCount = 4 > 2，可压缩
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}
	targets := dc.BuildCompressTargets(messages)
	if len(targets) != 1 {
		t.Fatalf("期望 1 个压缩目标，实际 %d", len(targets))
	}
	if targets[0].blockID != "react_1" {
		t.Errorf("blockID 应为 react_1，实际: %s", targets[0].blockID)
	}
	if targets[0].startIDx != 1 {
		t.Errorf("startIDx 应为 1，实际: %d", targets[0].startIDx)
	}
	if targets[0].endIDx != 3 {
		t.Errorf("endIDx 应为 3，实际: %d", targets[0].endIDx)
	}
}

// TestBuildCompressTargets_多轮 验证多轮压缩目标
func TestBuildCompressTargets_多轮(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
		llm_schema.NewUserMessage("上海呢"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_2", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_2", "多云"),
		llm_schema.NewAssistantMessage("上海多云"),
	}
	targets := dc.BuildCompressTargets(messages)
	if len(targets) != 2 {
		t.Fatalf("期望 2 个压缩目标，实际 %d", len(targets))
	}
	if targets[0].blockID != "react_1" {
		t.Errorf("第1个 blockID 应为 react_1，实际: %s", targets[0].blockID)
	}
	if targets[1].blockID != "react_2" {
		t.Errorf("第2个 blockID 应为 react_2，实际: %s", targets[1].blockID)
	}
}

// TestBuildSplitContextPayload 验证上下文载荷构建
func TestBuildSplitContextPayload(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	contextMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("系统消息"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
		llm_schema.NewUserMessage("谢谢"),
	}

	targets := []compressTarget{
		{
			blockID:  "react_1",
			userIDx:  1,
			startIDx: 2,
			endIDx:   4,
			messages: contextMessages[2:5],
		},
	}

	payload := dc.BuildSplitContextPayload(contextMessages, targets)

	if !strings.Contains(payload, "[Context Before Targets]") {
		t.Error("载荷应包含 [Context Before Targets]")
	}
	if !strings.Contains(payload, "[Compression Targets]") {
		t.Error("载荷应包含 [Compression Targets]")
	}
	if !strings.Contains(payload, "[Context After Targets]") {
		t.Error("载荷应包含 [Context After Targets]")
	}
	if !strings.Contains(payload, "[Block: react_1]") {
		t.Error("载荷应包含 [Block: react_1]")
	}
}

// TestBuildTargetsPayload 验证目标映射载荷构建
func TestBuildTargetsPayload(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	targets := []compressTarget{
		{blockID: "react_1", userIDx: 0, startIDx: 1, endIDx: 3},
		{blockID: "react_2", userIDx: 4, startIDx: 5, endIDx: 7},
	}

	payload := dc.BuildTargetsPayload(targets)

	if !strings.Contains(payload, "[Target Mapping]") {
		t.Error("载荷应包含 [Target Mapping]")
	}
	if !strings.Contains(payload, "[Block: react_1]") {
		t.Error("载荷应包含 [Block: react_1]")
	}
	if !strings.Contains(payload, "anchor_user_index: 0") {
		t.Error("载荷应包含 anchor_user_index: 0")
	}
	if !strings.Contains(payload, "replace_range: [1, 3]") {
		t.Error("载荷应包含 replace_range: [1, 3]")
	}
	if !strings.Contains(payload, "[Block: react_2]") {
		t.Error("载荷应包含 [Block: react_2]")
	}
	if !strings.Contains(payload, "[Output Requirements]") {
		t.Error("载荷应包含 [Output Requirements]")
	}
}

// TestBuildJSONReplacements_有效载荷 验证从有效 JSON 构建替换
func TestBuildJSONReplacements_有效载荷(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// 使用 dynamicFakeTokenCounter：原始 1000 → 压缩 50，确保有收益
	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{1000, 50}}}

	targets := []compressTarget{
		{
			blockID:  "react_1",
			userIDx:  0,
			startIDx: 1,
			endIDx:   3,
			messages: []llm_schema.BaseMessage{
				llm_schema.NewAssistantMessage("查询中",
					llm_schema.WithToolCalls([]*llm_schema.ToolCall{
						{ID: "call_1", Name: "get_weather", Arguments: `{}`},
					}),
				),
				llm_schema.NewToolMessage("call_1", "晴天"),
				llm_schema.NewAssistantMessage("今天晴天"),
			},
		},
	}

	parserContent := map[string]any{
		"blocks": []any{
			map[string]any{
				"block_id": "react_1",
				"summary":  "用户询问天气，查询得知今天晴天",
			},
		},
	}

	replacements, modifiedIndices := dc.BuildJSONReplacements(context.Background(), mc, targets, parserContent)

	if len(replacements) == 0 {
		t.Error("有效载荷应产生替换")
	}
	if len(modifiedIndices) == 0 {
		t.Error("应有被修改的索引")
	}
}

// TestBuildJSONReplacements_无效载荷 验证从无效 JSON 不构建替换
func TestBuildJSONReplacements_无效载荷(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 0}}

	targets := []compressTarget{
		{blockID: "react_1", userIDx: 0, startIDx: 1, endIDx: 3},
	}

	replacements, modifiedIndices := dc.BuildJSONReplacements(context.Background(), mc, targets, "invalid")
	if len(replacements) != 0 || len(modifiedIndices) != 0 {
		t.Error("无效载荷不应产生替换")
	}
}

// TestBuildJSONReplacements_缺少summary 验证空 summary 被跳过
func TestBuildJSONReplacements_缺少summary(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 0}}

	targets := []compressTarget{
		{blockID: "react_1", userIDx: 0, startIDx: 1, endIDx: 3},
	}

	parserContent := map[string]any{
		"blocks": []any{
			map[string]any{
				"block_id": "react_1",
				"summary":  "",
			},
		},
	}

	replacements, _ := dc.BuildJSONReplacements(context.Background(), mc, targets, parserContent)
	if len(replacements) != 0 {
		t.Error("空 summary 不应产生替换")
	}
}

// TestBuildFallbackReplacement 验证降级替换构建
func TestBuildFallbackReplacement(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// 使用 dynamicFakeTokenCounter：原始 1000 → 压缩 50
	mc := &fakeModelContext{tokenCounter: &dynamicFakeTokenCounter{counts: []int{1000, 50}}}

	targets := []compressTarget{
		{
			blockID:  "react_1",
			userIDx:  0,
			startIDx: 1,
			endIDx:   3,
			messages: []llm_schema.BaseMessage{
				llm_schema.NewAssistantMessage("查询中",
					llm_schema.WithToolCalls([]*llm_schema.ToolCall{
						{ID: "call_1", Name: "get_weather", Arguments: `{}`},
					}),
				),
				llm_schema.NewToolMessage("call_1", "晴天"),
				llm_schema.NewAssistantMessage("今天晴天"),
			},
		},
		{
			blockID:  "react_2",
			userIDx:  4,
			startIDx: 5,
			endIDx:   7,
			messages: []llm_schema.BaseMessage{
				llm_schema.NewAssistantMessage("查询中2",
					llm_schema.WithToolCalls([]*llm_schema.ToolCall{
						{ID: "call_2", Name: "get_weather", Arguments: `{}`},
					}),
				),
				llm_schema.NewToolMessage("call_2", "多云"),
				llm_schema.NewAssistantMessage("上海多云"),
			},
		},
	}

	summary := "这是降级摘要"
	fallback := dc.BuildFallbackReplacement(context.Background(), mc, targets, summary)
	if fallback == nil {
		t.Fatal("降级替换不应为 nil")
	}
	if fallback.StartIdx != 1 {
		t.Errorf("StartIdx 应为 1，实际: %d", fallback.StartIdx)
	}
	if fallback.EndIdx != 7 {
		t.Errorf("EndIdx 应为 7，实际: %d", fallback.EndIdx)
	}
	if len(fallback.Messages) != 1 {
		t.Fatalf("降级替换应有 1 条消息，实际: %d", len(fallback.Messages))
	}
	content := fallback.Messages[0].GetContent().Text()
	if !strings.Contains(content, "[DIALOGUE_MEMORY_BLOCK]") {
		t.Error("降级替换消息应包含记忆块标记")
	}
	if !strings.Contains(content, summary) {
		t.Error("降级替换消息应包含摘要内容")
	}
}

// TestBuildFallbackReplacement_空摘要 验证空摘要返回 nil
func TestBuildFallbackReplacement_空摘要(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 0}}
	targets := []compressTarget{
		{blockID: "react_1", userIDx: 0, startIDx: 1, endIDx: 3},
	}

	fallback := dc.BuildFallbackReplacement(context.Background(), mc, targets, "  ")
	if fallback != nil {
		t.Error("空摘要应返回 nil")
	}
}

// TestExtractCompactSummaryFromReplacements 验证从替换列表提取摘要
func TestExtractCompactSummaryFromReplacements(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	replacements := []processor.Replacement{
		{
			StartIdx: 1,
			EndIdx:   3,
			Messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage(WrapMemoryBlock("摘要1")),
			},
		},
		{
			StartIdx: 5,
			EndIdx:   7,
			Messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage(WrapMemoryBlock("摘要2")),
			},
		},
	}

	summary := dc.ExtractCompactSummaryFromReplacements(replacements)
	if !strings.Contains(summary, "[DIALOGUE_MEMORY_BLOCK]") {
		t.Error("摘要应包含记忆块标记")
	}
	if !strings.Contains(summary, "摘要1") {
		t.Error("摘要应包含第一个摘要内容")
	}
	if !strings.Contains(summary, "摘要2") {
		t.Error("摘要应包含第二个摘要内容")
	}
}

// TestExtractCompactSummaryFromReplacements_无记忆块 验证不含记忆块标记的消息被跳过
func TestExtractCompactSummaryFromReplacements_无记忆块(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	replacements := []processor.Replacement{
		{
			StartIdx: 1,
			EndIdx:   3,
			Messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("普通消息"),
			},
		},
	}

	summary := dc.ExtractCompactSummaryFromReplacements(replacements)
	if summary != "" {
		t.Errorf("不含记忆块标记的替换应返回空字符串，实际: %s", summary)
	}
}

// TestDialogueCompressor_ProcessorType 验证处理器类型标识
func TestDialogueCompressor_ProcessorType(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)
	if dc.ProcessorType() != "DialogueCompressor" {
		t.Errorf("ProcessorType 应为 DialogueCompressor，实际: %s", dc.ProcessorType())
	}
}

// TestDialogueCompressor_SaveLoadState 验证状态保存/加载（空操作）
func TestDialogueCompressor_SaveLoadState(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	state := dc.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState 应返回空 map，实际: %v", state)
	}

	dc.LoadState(map[string]any{"key": "value"})
}

// TestDialogueCompressor_自定义提示词 验证自定义提示词
func TestDialogueCompressor_自定义提示词(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.CustomCompressionPrompt = "自定义压缩提示词"
	dc := newTestDialogueCompressor(cfg)
	if dc.compressedPrompt != "自定义压缩提示词" {
		t.Errorf("应使用自定义提示词，实际: %s", dc.compressedPrompt)
	}
}

// TestDialogueCompressor_默认提示词 验证默认提示词
func TestDialogueCompressor_默认提示词(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.CustomCompressionPrompt = ""
	dc := newTestDialogueCompressor(cfg)
	if dc.compressedPrompt != defaultCompressionPrompt {
		t.Error("空字符串时应使用默认提示词")
	}
}

// TestTriggerAddMessages_消息数超阈值 验证消息数超过阈值触发
func TestTriggerAddMessages_消息数超阈值(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesThreshold = 5
	cfg.TokensThreshold = 10000
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("1"), llm_schema.NewUserMessage("2")},
		tokenCounter: &fakeTokenCounter{count: 100},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("3"),
		llm_schema.NewUserMessage("4"),
		llm_schema.NewUserMessage("5"),
		llm_schema.NewUserMessage("6"),
	}

	triggered, err := dc.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if !triggered {
		t.Error("消息数 2+4=6 > 5 应触发")
	}
}

// TestTriggerAddMessages_Token数超阈值 验证 Token 数超过阈值触发
func TestTriggerAddMessages_Token数超阈值(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesThreshold = 0
	cfg.MessagesToKeep = 0
	cfg.TokensThreshold = 500
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		tokenCounter: &fakeTokenCounter{count: 600},
	}

	triggered, err := dc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if !triggered {
		t.Error("Token 数 600 > 500 应触发")
	}
}

// TestTriggerAddMessages_未达阈值 验证未达阈值不触发
func TestTriggerAddMessages_未达阈值(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesThreshold = 0
	cfg.TokensThreshold = 10000
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		tokenCounter: &fakeTokenCounter{count: 100},
	}

	triggered, err := dc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("Token 数 100 < 10000 不应触发")
	}
}

// TestTriggerAddMessages_MessagesToKeep保护 验证 MessagesToKeep 保护
func TestTriggerAddMessages_MessagesToKeep保护(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesThreshold = 100
	cfg.TokensThreshold = 10000
	cfg.MessagesToKeep = 10
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
		tokenCounter: &fakeTokenCounter{count: 600},
	}

	triggered, err := dc.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("总消息数 < MessagesToKeep 时不应触发")
	}
}

// TestInitRegistration 验证 init() 自动注册
func TestInitRegistration(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("DialogueCompressor")
	if !ok {
		t.Fatal("DialogueCompressor 应已通过 init() 注册")
	}

	// 传入其他 ProcessorConfig 实现应返回 error（类型不匹配）
	otherConfig := &testConfig{Name: "other"}
	result, err := factory(otherConfig)
	if err == nil {
		t.Error("错误类型配置应返回 error")
	}
	if result != nil {
		t.Error("错误类型配置应返回 nil 结果")
	}

	// 传入非法 DialogueCompressorConfig 应返回 error（校验失败）
	result, err = factory(&DialogueCompressorConfig{TokensThreshold: 0})
	if err == nil {
		t.Error("非法配置应返回 error")
	}
	if result != nil {
		t.Error("非法配置应返回 nil 结果")
	}
}

// TestInitRegistration_已注册 验证 DialogueCompressor 已在注册表中
func TestInitRegistration_已注册(t *testing.T) {
	factories := context_engine.ListProcessorFactories()
	found := false
	for _, name := range factories {
		if name == "DialogueCompressor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DialogueCompressor 应在已注册工厂列表中")
	}
}

// TestIsModelCallFailedError 验证 MODEL_CALL_FAILED 错误判断
func TestIsModelCallFailedError(t *testing.T) {
	// 非 BaseError
	if isModelCallFailedError(fmt.Errorf("普通错误")) {
		t.Error("普通错误不应被识别为 MODEL_CALL_FAILED")
	}

	// MODEL_CALL_FAILED 错误
	modelErr := exception.NewBaseError(
		exception.NewStatusCode("MODEL_CALL_FAILED", 181001, ""),
		exception.WithMsg("模型调用失败"),
	)
	if !isModelCallFailedError(modelErr) {
		t.Error("MODEL_CALL_FAILED 错误应被识别")
	}

	// 非 MODEL_CALL_FAILED 的 BaseError
	otherErr := exception.NewBaseError(
		exception.NewStatusCode("OTHER_ERROR", 181002, ""),
		exception.WithMsg("其他错误"),
	)
	if isModelCallFailedError(otherErr) {
		t.Error("其他 BaseError 不应被识别为 MODEL_CALL_FAILED")
	}
}

// TestBuildRangeIndices 验证索引范围构建
func TestBuildRangeIndices(t *testing.T) {
	indices := buildRangeIndices(2, 5)
	expected := []int{2, 3, 4, 5}
	if len(indices) != len(expected) {
		t.Fatalf("期望 %d 个索引，实际 %d", len(expected), len(indices))
	}
	for i, idx := range indices {
		if idx != expected[i] {
			t.Errorf("索引 %d: 期望 %d，实际 %d", i, expected[i], idx)
		}
	}
}

// TestDialogueCompressor_LoadState 验证 LoadState 不 panic
func TestDialogueCompressor_LoadState(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)
	// LoadState 是空操作，只需确保不 panic
	dc.LoadState(map[string]any{"key": "value"})
}

// TestWithCompressorModel 验证 WithCompressorModel 选项
func TestWithCompressorModel(t *testing.T) {
	_ = validDialogueCompressorConfig()
	opt := WithCompressorModel(nil) // nil model
	dc := &DialogueCompressor{}
	opt(dc)
	if dc.model != nil {
		t.Error("WithCompressorModel(nil) 应将 model 设为 nil")
	}
}

// TestSerializeMessagesRange 边界验证
func TestSerializeMessagesRange_边界(t *testing.T) {
	// start >= end
	result := serializeMessagesRange(nil, 5, 3)
	if result != "" {
		t.Errorf("start >= end 应返回空字符串，实际: %s", result)
	}

	// 空列表
	result = serializeMessagesRange([]llm_schema.BaseMessage{}, 0, 1)
	if result != "" {
		t.Errorf("空列表应返回空字符串，实际: %s", result)
	}
}

// TestSerializeMessagesRangeWithOffset_空列表 验证空列表
func TestSerializeMessagesRangeWithOffset_空列表(t *testing.T) {
	result := serializeMessagesRangeWithOffset(nil, 0)
	if result != "" {
		t.Errorf("空列表应返回空字符串，实际: %s", result)
	}
}

// TestCountMessagesTokens_TokenCounterError 验证 TokenCounter 返回 error 时的降级
func TestCountMessagesTokens_TokenCounterError(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	// TokenCounter 返回 error → 降级到字符估算
	mc := &fakeModelContext{tokenCounter: &fakeTokenCounter{count: 0, err: fmt.Errorf("编码器不可用")}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello world"), // 11/3 = 3
	}
	tokens := dc.countMessagesTokens(mc, messages)
	if tokens <= 0 {
		t.Errorf("TokenCounter 返回 error 应降级到字符估算，实际: %d", tokens)
	}
}

// TestCountMessagesTokens_无TokenCounter 验证无 TokenCounter 时的字符估算
func TestCountMessagesTokens_无TokenCounter(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	dc := newTestDialogueCompressor(cfg)

	mc := &fakeModelContext{tokenCounter: nil}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("abc"), // 3/3 = 1
	}
	tokens := dc.countMessagesTokens(mc, messages)
	if tokens != 1 {
		t.Errorf("无 TokenCounter 应使用字符估算，期望 1，实际: %d", tokens)
	}
}

// TestGetCompressIdx_KeepLastRoundNoFinalAssistant 验证 KeepLastRound 但无最终 Assistant 时回退
func TestGetCompressIdx_KeepLastRoundNoFinalAssistant(t *testing.T) {
	cfg := validDialogueCompressorConfig()
	cfg.MessagesToKeep = 0
	cfg.KeepLastRound = true
	dc := newTestDialogueCompressor(cfg)

	// 只有 UserMessage，没有最终 AssistantMessage → FindLastFinalAssistantIdx 返回 -1 → 回退到 keepIndex
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewUserMessage("查询天气"),
	}
	idx := dc.GetCompressIdx(messages)
	if idx != 2 {
		t.Errorf("无最终 Assistant 时应回退到 keepIndex=2，实际: %d", idx)
	}
}

// TestGetCompressPairs_连续UserMessage 验证连续 UserMessage 时第一个 User 作为锚点
func TestGetCompressPairs_连续UserMessage(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewUserMessage("再见"),
		llm_schema.NewAssistantMessage("拜拜"),
	}
	pairs := GetCompressPairs(messages)
	if len(pairs) != 1 {
		t.Fatalf("期望 1 个配对，实际 %d", len(pairs))
	}
	// 第一个 User(0) 为锚点，后续 User 不会重置（因为 currentUser != -1）
	// 最终 Assistant(2, 无 tool_calls) 完成配对
	if pairs[0] != [2]int{0, 2} {
		t.Errorf("配对 = %v, want [0, 2]", pairs[0])
	}
}

// TestEstimateContentTokens_NilContent 验证 nil 内容 Token 估算
func TestEstimateContentTokens_NilContent(t *testing.T) {
	tokens := processor.EstimateContentTokens(nil)
	if tokens < 0 {
		t.Errorf("nil 内容应返回非负数，实际: %d", tokens)
	}
}
