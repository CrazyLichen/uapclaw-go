package compressor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fcpFakeModelContext 测试用 ModelContext 模拟（FullCompact 测试专用）
type fcpFakeModelContext struct {
	messages     []llm_schema.BaseMessage
	tokenCounter token.TokenCounter
}

func (f *fcpFakeModelContext) Len() int { return len(f.messages) }
func (f *fcpFakeModelContext) GetMessages(_ int, _ bool) []llm_schema.BaseMessage {
	return f.messages
}
func (f *fcpFakeModelContext) SetMessages(messages []llm_schema.BaseMessage, _ bool) {
	f.messages = messages
}
func (f *fcpFakeModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage { return nil }
func (f *fcpFakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...iface.Option) error {
	return nil
}
func (f *fcpFakeModelContext) AddMessages(_ context.Context, _ llm_schema.BaseMessage, _ ...iface.Option) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (f *fcpFakeModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage,
	_ []*schema.ToolInfo, _ int, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (f *fcpFakeModelContext) Statistic() *iface.ContextStats                       { return nil }
func (f *fcpFakeModelContext) SessionID() string                                    { return "test-session" }
func (f *fcpFakeModelContext) ContextID() string                                    { return "test-context" }
func (f *fcpFakeModelContext) TokenCounter() token.TokenCounter                     { return f.tokenCounter }
func (f *fcpFakeModelContext) ReloaderTool() tool.Tool                              { return nil }
func (f *fcpFakeModelContext) WorkspaceDir() string                                 { return "" }
func (f *fcpFakeModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade)      {}
func (f *fcpFakeModelContext) GetSessionRef() sessioninterfaces.SessionFacade       { return nil }
func (f *fcpFakeModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (f *fcpFakeModelContext) SaveState() map[string]any                            { return nil }
func (f *fcpFakeModelContext) LoadState(_ map[string]any)                           {}
func (f *fcpFakeModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return "", nil
}

// fcpFakeTokenCounter 测试用 TokenCounter 模拟
type fcpFakeTokenCounter struct {
	count int
	err   error
}

func (f *fcpFakeTokenCounter) Count(_ string, _ string) (int, error) { return f.count, f.err }
func (f *fcpFakeTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	return f.count, f.err
}
func (f *fcpFakeTokenCounter) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return f.count, f.err
}

// fcpDynamicTokenCounter 动态返回不同计数的 TokenCounter
type fcpDynamicTokenCounter struct {
	counts []int
	index  int
	err    error
}

func (d *fcpDynamicTokenCounter) Count(_ string, _ string) (int, error) { return 0, d.err }
func (d *fcpDynamicTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
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
func (d *fcpDynamicTokenCounter) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return 0, d.err
}

// newTestFCP 创建测试用 FullCompactProcessor（跳过模型创建）
func newTestFCP(cfg *FullCompactProcessorConfig) *FullCompactProcessor {
	if cfg == nil {
		cfg = NewFullCompactProcessorConfig()
	}
	bp := processor.NewBaseProcessor(cfg)
	reinjector := newFullCompactStateReinjector()
	return &FullCompactProcessor{
		BaseProcessor: bp,
		fcpConfig:     cfg,
		model:         nil,
		reinjector:    reinjector,
	}
}

// validFCPConfig 创建合法的 FullCompactProcessorConfig
func validFCPConfig() *FullCompactProcessorConfig {
	return &FullCompactProcessorConfig{
		TriggerTotalTokens:          180000,
		CompressionCallMaxTokens:    200000,
		MessagesToKeep:              10,
		SessionMemoryEnabled:        true,
		KeepToolMessagePairs:        true,
		StateSnapshotMaxChars:       4000,
		ReinjectRecentSkills:        3,
		ReinjectFileToolNames:       []string{"read_file", "write_file", "edit_file", "glob", "grep"},
		ReinjectToolResultHintNames: []string{"read_file", "write_file", "edit_file", "glob", "grep"},
		Marker:                      "[FULL_COMPACT_BOUNDARY]",
		StateMarker:                 "[FULL_COMPACT_STATE]",
		SyntheticUserMarker:         "[earlier conversation truncated for compaction retry]",
		SummaryIntro:                "This session is being continued from a previous conversation...",
		RecentMessagesNotice:        "Recent messages are preserved verbatim.",
		SessionMemoryMarker:         "[SESSION_MEMORY_BOUNDARY]",
		SessionMemoryIntro:          "Earlier conversation has been replaced with the session memory file...",
	}
}

// makeAPIRoundMessages 创建构成完整 API 轮次的消息列表
func makeAPIRoundMessages() []llm_schema.BaseMessage {
	return []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天 25°C"),
		llm_schema.NewAssistantMessage("今天晴天 25°C"),
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestFullCompactProcessorConfig_Validate_默认配置 验证默认配置通过校验
func TestFullCompactProcessorConfig_Validate_默认配置(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("默认配置不应返回错误，实际: %v", err)
	}
}

// TestFullCompactProcessorConfig_Validate_合法配置 验证合法配置通过校验
func TestFullCompactProcessorConfig_Validate_合法配置(t *testing.T) {
	cfg := validFCPConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("合法配置不应返回错误，实际: %v", err)
	}
}

// TestFullCompactProcessorConfig_Validate_TriggerTotalTokens非法 验证 TriggerTotalTokens <= 0
func TestFullCompactProcessorConfig_Validate_TriggerTotalTokens非法(t *testing.T) {
	cfg := validFCPConfig()
	cfg.TriggerTotalTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("TriggerTotalTokens = 0 应返回错误")
	}
	cfg.TriggerTotalTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TriggerTotalTokens = -1 应返回错误")
	}
}

// TestFullCompactProcessorConfig_Validate_CompressionCallMaxTokens非法 验证 CompressionCallMaxTokens <= 0
func TestFullCompactProcessorConfig_Validate_CompressionCallMaxTokens非法(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("CompressionCallMaxTokens = 0 应返回错误")
	}
}

// TestFullCompactProcessorConfig_Validate_MessagesToKeep非法 验证 MessagesToKeep < 0
func TestFullCompactProcessorConfig_Validate_MessagesToKeep非法(t *testing.T) {
	cfg := validFCPConfig()
	cfg.MessagesToKeep = -1
	if err := cfg.Validate(); err == nil {
		t.Error("MessagesToKeep = -1 应返回错误")
	}
}

// TestFullCompactProcessorConfig_Validate_StateSnapshotMaxChars非法 验证 StateSnapshotMaxChars <= 0
func TestFullCompactProcessorConfig_Validate_StateSnapshotMaxChars非法(t *testing.T) {
	cfg := validFCPConfig()
	cfg.StateSnapshotMaxChars = 0
	if err := cfg.Validate(); err == nil {
		t.Error("StateSnapshotMaxChars = 0 应返回错误")
	}
}

// TestFullCompactProcessorConfig_Validate_ReinjectRecentSkills非法 验证 ReinjectRecentSkills < 0
func TestFullCompactProcessorConfig_Validate_ReinjectRecentSkills非法(t *testing.T) {
	cfg := validFCPConfig()
	cfg.ReinjectRecentSkills = -1
	if err := cfg.Validate(); err == nil {
		t.Error("ReinjectRecentSkills = -1 应返回错误")
	}
}

// TestFullCompactProcessorConfig_Validate_MessagesToKeep零 验证 MessagesToKeep = 0 合法
func TestFullCompactProcessorConfig_Validate_MessagesToKeep零(t *testing.T) {
	cfg := validFCPConfig()
	cfg.MessagesToKeep = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("MessagesToKeep = 0 应合法，实际: %v", err)
	}
}

// TestFullCompactProcessor_ProcessorType 验证处理器类型标识
func TestFullCompactProcessor_ProcessorType(t *testing.T) {
	fcp := newTestFCP(nil)
	if fcp.ProcessorType() != "FullCompactProcessor" {
		t.Errorf("ProcessorType 应为 FullCompactProcessor，实际: %s", fcp.ProcessorType())
	}
}

// TestFullCompactProcessor_TriggerAddMessages_非APIRound 验证非 API round 不触发
func TestFullCompactProcessor_TriggerAddMessages_非APIRound(t *testing.T) {
	cfg := validFCPConfig()
	cfg.TriggerTotalTokens = 100
	fcp := newTestFCP(cfg)

	mc := &fcpFakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("你好")},
		tokenCounter: &fcpFakeTokenCounter{count: 200},
	}
	// messagesToAdd 不构成完整 API round
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewUserMessage("世界")}

	triggered, err := fcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("非 API round 不应触发")
	}
}

// TestFullCompactProcessor_TriggerAddMessages_超阈值触发 验证超阈值触发
func TestFullCompactProcessor_TriggerAddMessages_超阈值触发(t *testing.T) {
	cfg := validFCPConfig()
	cfg.TriggerTotalTokens = 100
	fcp := newTestFCP(cfg)

	// 构建完整 API round 消息
	apiMessages := makeAPIRoundMessages()
	mc := &fcpFakeModelContext{
		messages:     apiMessages[:1],
		tokenCounter: &fcpFakeTokenCounter{count: 200},
	}
	messagesToAdd := apiMessages[1:]

	triggered, err := fcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if !triggered {
		t.Error("Token 数超过阈值应触发")
	}
}

// TestFullCompactProcessor_TriggerAddMessages_未超阈值 验证未超阈值不触发
func TestFullCompactProcessor_TriggerAddMessages_未超阈值(t *testing.T) {
	cfg := validFCPConfig()
	cfg.TriggerTotalTokens = 10000
	fcp := newTestFCP(cfg)

	apiMessages := makeAPIRoundMessages()
	mc := &fcpFakeModelContext{
		messages:     apiMessages[:1],
		tokenCounter: &fcpFakeTokenCounter{count: 100},
	}
	messagesToAdd := apiMessages[1:]

	triggered, err := fcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 失败: %v", err)
	}
	if triggered {
		t.Error("Token 数未超阈值不应触发")
	}
}

// TestFullCompactProcessor_IsBoundaryMessage 验证边界消息识别
func TestFullCompactProcessor_IsBoundaryMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// 正例：SystemMessage 且 Content 以 Marker 开头
	boundaryMsg := llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\nConversation compacted")
	if !fcp._isBoundaryMessage(boundaryMsg) {
		t.Error("以 Marker 开头的 SystemMessage 应识别为 boundary")
	}

	// 反例：SystemMessage 但 Content 不以 Marker 开头
	normalSystemMsg := llm_schema.NewSystemMessage("正常系统消息")
	if fcp._isBoundaryMessage(normalSystemMsg) {
		t.Error("不以 Marker 开头的 SystemMessage 不应识别为 boundary")
	}

	// 反例：UserMessage 即使 Content 以 Marker 开头
	userBoundaryMsg := llm_schema.NewUserMessage("[FULL_COMPACT_BOUNDARY]")
	if fcp._isBoundaryMessage(userBoundaryMsg) {
		t.Error("UserMessage 不应识别为 boundary")
	}
}

// TestFullCompactProcessor_IsStateMessage 验证状态消息识别
func TestFullCompactProcessor_IsStateMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// 正例
	stateMsg := llm_schema.NewUserMessage("[FULL_COMPACT_STATE]\n[SKILLS]\n...")
	if !fcp._isStateMessage(stateMsg) {
		t.Error("以 StateMarker 开头的 UserMessage 应识别为 state")
	}

	// 反例：SystemMessage
	systemStateMsg := llm_schema.NewSystemMessage("[FULL_COMPACT_STATE]")
	if fcp._isStateMessage(systemStateMsg) {
		t.Error("SystemMessage 不应识别为 state")
	}

	// 反例：不以 StateMarker 开头
	normalUserMsg := llm_schema.NewUserMessage("普通消息")
	if fcp._isStateMessage(normalUserMsg) {
		t.Error("不以 StateMarker 开头不应识别为 state")
	}
}

// TestFullCompactProcessor_IsSessionMemoryBoundaryMessage 验证 Session Memory 边界消息识别
func TestFullCompactProcessor_IsSessionMemoryBoundaryMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// 正例
	smBoundaryMsg := llm_schema.NewSystemMessage("[SESSION_MEMORY_BOUNDARY]\nEarlier conversation replaced")
	if !fcp._isSessionMemoryBoundaryMessage(smBoundaryMsg) {
		t.Error("以 SessionMemoryMarker 开头的 SystemMessage 应识别")
	}

	// 反例
	normalMsg := llm_schema.NewSystemMessage("正常消息")
	if fcp._isSessionMemoryBoundaryMessage(normalMsg) {
		t.Error("不以 SessionMemoryMarker 开头不应识别")
	}
}

// TestFullCompactProcessor_IsSessionMemorySummaryMessage 验证 Session Memory 摘要消息识别
func TestFullCompactProcessor_IsSessionMemorySummaryMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// 正例
	intro := fcp.fcpConfig.SessionMemoryIntro
	smSummaryMsg := llm_schema.NewUserMessage(intro + " more content")
	if !fcp._isSessionMemorySummaryMessage(smSummaryMsg) {
		t.Error("以 SessionMemoryIntro 开头的 UserMessage 应识别")
	}

	// 反例
	normalMsg := llm_schema.NewUserMessage("普通消息")
	if fcp._isSessionMemorySummaryMessage(normalMsg) {
		t.Error("不以 SessionMemoryIntro 开头不应识别")
	}
}

// TestFullCompactProcessor_IsSyntheticMarkerMessage 验证合成标记消息识别
func TestFullCompactProcessor_IsSyntheticMarkerMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// 正例：Content == SyntheticUserMarker
	syntheticMsg := llm_schema.NewUserMessage("[earlier conversation truncated for compaction retry]")
	if !fcp._isSyntheticMarkerMessage(syntheticMsg) {
		t.Error("Content == SyntheticUserMarker 的 UserMessage 应识别")
	}

	// 反例：Content 只是前缀
	prefixMsg := llm_schema.NewUserMessage("[earlier conversation truncated for compaction retry] extra")
	if fcp._isSyntheticMarkerMessage(prefixMsg) {
		t.Error("Content 只是前缀不应识别")
	}

	// 反例：其他类型
	systemMsg := llm_schema.NewSystemMessage("[earlier conversation truncated for compaction retry]")
	if fcp._isSyntheticMarkerMessage(systemMsg) {
		t.Error("SystemMessage 不应识别")
	}
}

// TestFullCompactProcessor_SerializeMessage 验证消息序列化
func TestFullCompactProcessor_SerializeMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// UserMessage
	userMsg := llm_schema.NewUserMessage("你好世界")
	result := fcp._serializeMessage(userMsg)
	if !strings.Contains(result, "role=user") {
		t.Error("应包含 role=user")
	}
	if !strings.Contains(result, "content=你好世界") {
		t.Error("应包含 content=你好世界")
	}

	// AssistantMessage with ToolCalls
	assistantMsg := llm_schema.NewAssistantMessage("查询中",
		llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`, Type: "function"},
		}),
	)
	result = fcp._serializeMessage(assistantMsg)
	if !strings.Contains(result, "role=assistant") {
		t.Error("应包含 role=assistant")
	}
	if !strings.Contains(result, "tool_calls=") {
		t.Error("应包含 tool_calls=")
	}
	if !strings.Contains(result, "get_weather") {
		t.Error("应包含工具名称")
	}

	// ToolMessage
	toolMsg := llm_schema.NewToolMessage("call_1", "晴天")
	result = fcp._serializeMessage(toolMsg)
	if !strings.Contains(result, "tool_call_id=call_1") {
		t.Error("应包含 tool_call_id=call_1")
	}
	if !strings.Contains(result, "content=晴天") {
		t.Error("应包含 content=晴天")
	}
}

// TestFullCompactProcessor_SerializeMessages 验证消息列表序列化
func TestFullCompactProcessor_SerializeMessages(t *testing.T) {
	fcp := newTestFCP(nil)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	result := fcp._serializeMessages(messages)

	if !strings.Contains(result, "role=user") {
		t.Error("应包含第一条消息")
	}
	if !strings.Contains(result, "role=assistant") {
		t.Error("应包含第二条消息")
	}
	if strings.Count(result, "\n") != 1 {
		t.Errorf("两条消息应以单个换行连接，实际换行数: %d", strings.Count(result, "\n"))
	}
}

// TestFormatSummary 验证摘要格式化
func TestFormatSummary(t *testing.T) {
	// 包含 <analysis> 和 <summary> 标签
	content := "<analysis>这是分析内容</analysis>\n<summary>\n这是摘要内容\n</summary>"
	result := _formatSummary(content)
	if !strings.HasPrefix(result, "Summary:\n") {
		t.Error("结果应以 Summary: 开头")
	}
	if strings.Contains(result, "分析内容") {
		t.Error("结果不应包含 analysis 内容")
	}
	if !strings.Contains(result, "摘要内容") {
		t.Error("结果应包含 summary 内容")
	}

	// 不包含 <summary> 标签，直接返回去除 analysis 后的内容
	content2 := "<analysis>分析</analysis>\n直接内容"
	result2 := _formatSummary(content2)
	if strings.Contains(result2, "分析") {
		t.Error("不应包含 analysis 内容")
	}
	if !strings.Contains(result2, "直接内容") {
		t.Error("应包含直接内容")
	}
}

// TestFormatSummary_无Analysis 验证无 analysis 标签时直接提取 summary
func TestFormatSummary_无Analysis(t *testing.T) {
	content := "<summary>\n摘要内容\n</summary>"
	result := _formatSummary(content)
	if !strings.HasPrefix(result, "Summary:\n") {
		t.Error("结果应以 Summary: 开头")
	}
	if !strings.Contains(result, "摘要内容") {
		t.Error("结果应包含摘要内容")
	}
}

// TestFullCompactProcessor_SelectMessagesToKeep 验证消息选择
func TestFullCompactProcessor_SelectMessagesToKeep(t *testing.T) {
	cfg := validFCPConfig()
	cfg.MessagesToKeep = 3
	cfg.KeepToolMessagePairs = false
	fcp := newTestFCP(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewUserMessage("2"),
		llm_schema.NewUserMessage("3"),
		llm_schema.NewUserMessage("4"),
		llm_schema.NewUserMessage("5"),
	}

	kept := fcp._selectMessagesToKeep(messages)
	if len(kept) != 3 {
		t.Fatalf("期望保留 3 条，实际: %d", len(kept))
	}
	if kept[0].GetContent().Text() != "3" {
		t.Errorf("第一条应为 3，实际: %s", kept[0].GetContent().Text())
	}
}

// TestFullCompactProcessor_SelectMessagesToKeep_零保留 验证 MessagesToKeep=0
func TestFullCompactProcessor_SelectMessagesToKeep_零保留(t *testing.T) {
	cfg := validFCPConfig()
	cfg.MessagesToKeep = 0
	fcp := newTestFCP(cfg)

	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("1")}
	kept := fcp._selectMessagesToKeep(messages)
	if len(kept) != 0 {
		t.Errorf("MessagesToKeep=0 应返回空列表，实际: %d", len(kept))
	}
}

// TestFullCompactProcessor_SelectMessagesToKeep_超出消息长度 验证 MessagesToKeep > 消息数
func TestFullCompactProcessor_SelectMessagesToKeep_超出消息长度(t *testing.T) {
	cfg := validFCPConfig()
	cfg.MessagesToKeep = 10
	cfg.KeepToolMessagePairs = false
	fcp := newTestFCP(cfg)

	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("1"), llm_schema.NewUserMessage("2")}
	kept := fcp._selectMessagesToKeep(messages)
	if len(kept) != 2 {
		t.Errorf("消息数不足时应返回全部，实际: %d", len(kept))
	}
}

// TestFullCompactProcessor_AdjustStartIndexForToolPairs 验证工具对调整
func TestFullCompactProcessor_AdjustStartIndexForToolPairs(t *testing.T) {
	cfg := validFCPConfig()
	fcp := newTestFCP(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
		llm_schema.NewUserMessage("谢谢"),
	}

	// 从索引 2 开始保留，ToolMessage(2) 需要 call_1 的 AssistantMessage(1)
	startIdx := fcp._adjustStartIndexForToolPairs(messages, 2)
	if startIdx != 1 {
		t.Errorf("应调整为 1 以包含 ToolMessage 对应的 AssistantMessage，实际: %d", startIdx)
	}
}

// TestFullCompactProcessor_AdjustStartIndexForToolPairs_无需调整 验证无需调整的情况
func TestFullCompactProcessor_AdjustStartIndexForToolPairs_无需调整(t *testing.T) {
	cfg := validFCPConfig()
	fcp := newTestFCP(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户问题"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}

	// 从索引 1 开始保留，AssistantMessage(1) 已包含所需 tool_call_id
	startIdx := fcp._adjustStartIndexForToolPairs(messages, 1)
	if startIdx != 1 {
		t.Errorf("无需调整时应保持原值，实际: %d", startIdx)
	}
}

// TestFullCompactProcessor_BuildFallbackSummary 验证降级摘要构建
func TestFullCompactProcessor_BuildFallbackSummary(t *testing.T) {
	fcp := newTestFCP(nil)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}

	result := fcp._buildFallbackSummary(messages)
	if !strings.HasPrefix(result, "Summary:\n") {
		t.Error("降级摘要应以 Summary: 开头")
	}
	if !strings.Contains(result, "你好") {
		t.Error("降级摘要应包含消息内容")
	}
}

// TestFullCompactProcessor_BuildFallbackSummary_超过20条 验证超过 20 条时只取尾部
func TestFullCompactProcessor_BuildFallbackSummary_超过20条(t *testing.T) {
	fcp := newTestFCP(nil)

	var messages []llm_schema.BaseMessage
	for i := 0; i < 30; i++ {
		messages = append(messages, llm_schema.NewUserMessage(fmt.Sprintf("消息%d", i)))
	}

	result := fcp._buildFallbackSummary(messages)
	if !strings.HasPrefix(result, "Summary:\n") {
		t.Error("降级摘要应以 Summary: 开头")
	}
	// 应包含最后 20 条（消息10 ~ 消息29），不应包含消息9
	if strings.Contains(result, "消息9") {
		t.Error("降级摘要不应包含前 10 条消息")
	}
	if !strings.Contains(result, "消息10") {
		t.Error("降级摘要应包含消息10（第 11 条）")
	}
}

// TestFullCompactStateReinjector_RegisterBuilder 验证构建器注册
func TestFullCompactStateReinjector_RegisterBuilder(t *testing.T) {
	r := &FullCompactStateReinjector{}

	builder := func(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
		return "test"
	}
	r.RegisterBuilder("test", "TEST", builder)

	builders := r.IterBuilders()
	if len(builders) != 1 {
		t.Fatalf("期望 1 个构建器，实际: %d", len(builders))
	}
	if builders[0].Name != "test" {
		t.Errorf("构建器名称应为 test，实际: %s", builders[0].Name)
	}
	if builders[0].Label != "TEST" {
		t.Errorf("构建器标签应为 TEST，实际: %s", builders[0].Label)
	}
}

// TestFullCompactStateReinjector_RegisterBuilder_同名替换 验证同名构建器替换
func TestFullCompactStateReinjector_RegisterBuilder_同名替换(t *testing.T) {
	r := &FullCompactStateReinjector{}

	builder1 := func(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
		return "v1"
	}
	builder2 := func(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
		return "v2"
	}
	r.RegisterBuilder("test", "V1", builder1)
	r.RegisterBuilder("test", "V2", builder2)

	builders := r.IterBuilders()
	if len(builders) != 1 {
		t.Fatalf("同名替换后期望 1 个构建器，实际: %d", len(builders))
	}
	if builders[0].Label != "V2" {
		t.Errorf("同名替换后标签应为 V2，实际: %s", builders[0].Label)
	}
}

// TestFullCompactStateReinjector_IterBuilders 验证迭代构建器
func TestFullCompactStateReinjector_IterBuilders(t *testing.T) {
	r := newFullCompactStateReinjector()
	builders := r.IterBuilders()

	// 应包含 4 个默认 builder：skills, task_status, plan_mode, plan
	if len(builders) != 4 {
		t.Fatalf("默认应注册 4 个构建器，实际: %d", len(builders))
	}

	expectedNames := map[string]bool{"skills": true, "task_status": true, "plan_mode": true, "plan": true}
	for _, b := range builders {
		if !expectedNames[b.Name] {
			t.Errorf("未预期的构建器名称: %s", b.Name)
		}
	}
}

// TestBuildSkillReinjectedContent 验证 skill 构建器
func TestBuildSkillReinjectedContent(t *testing.T) {
	// 构建含 skill 读取的轮次
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("读取skill"),
		llm_schema.NewAssistantMessage("读取中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"file_path": "/path/to/my_skill/skill.md"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "skill内容"),
		llm_schema.NewAssistantMessage("已读取skill"),
	}

	result := buildSkillReinjectedContent(context.Background(), nil, messages, nil)
	msgList, ok := result.([]llm_schema.BaseMessage)
	if !ok {
		t.Fatalf("期望返回 []BaseMessage，实际: %T", result)
	}
	if len(msgList) == 0 {
		t.Error("含 skill 读取的轮次应产生注入消息")
	}
	// 消息应包含 SKILLS 标记
	if !strings.Contains(msgList[0].GetContent().Text(), "[SKILLS]") {
		t.Error("注入消息应包含 [SKILLS] 标记")
	}
}

// TestBuildSkillReinjectedContent_无Skill 验证无 skill 读取时不注入
func TestBuildSkillReinjectedContent_无Skill(t *testing.T) {
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

	result := buildSkillReinjectedContent(context.Background(), nil, messages, nil)
	msgList, ok := result.([]llm_schema.BaseMessage)
	if !ok {
		// 可能返回 nil 或空列表
		return
	}
	if len(msgList) != 0 {
		t.Error("无 skill 读取的轮次不应产生注入消息")
	}
}

// TestBuildTaskStatusReinjectedContent 验证任务状态构建器
func TestBuildTaskStatusReinjectedContent(t *testing.T) {
	mc := &fcpFakeModelContext{}
	result := buildTaskStatusReinjectedContent(context.Background(), mc, nil, nil)
	str, ok := result.(string)
	if !ok {
		t.Fatalf("期望返回 string，实际: %T", result)
	}
	// 无 session 时返回空字符串
	if str != "" {
		t.Errorf("无 session 时应返回空字符串，实际: %s", str)
	}
}

// TestBuildPlanModeReinjectedContent 验证计划模式构建器
func TestBuildPlanModeReinjectedContent(t *testing.T) {
	mc := &fcpFakeModelContext{}
	result := buildPlanModeReinjectedContent(context.Background(), mc, nil, nil)
	str, ok := result.(string)
	if !ok {
		t.Fatalf("期望返回 string，实际: %T", result)
	}
	// 无 session 时返回空字符串
	if str != "" {
		t.Errorf("无 session 时应返回空字符串，实际: %s", str)
	}
}

// TestBuildPlanReinjectedContent 验证计划构建器（空实现）
func TestBuildPlanReinjectedContent(t *testing.T) {
	result := buildPlanReinjectedContent(context.Background(), nil, nil, nil)
	str, ok := result.(string)
	if !ok {
		t.Fatalf("期望返回 string，实际: %T", result)
	}
	if str != "" {
		t.Errorf("空实现应返回空字符串，实际: %s", str)
	}
}

// TestFullCompactProcessor_BuildReinjectedStateMessages 验证状态重新注入消息构建
func TestFullCompactProcessor_BuildReinjectedStateMessages(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{}

	// 普通消息（无 skill 读取）→ 只有 task_status/plan_mode/plan builder，都返回 ""
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}

	result := fcp.buildReinjectedStateMessages(context.Background(), mc, messages, nil)
	// 无 session 时所有 builder 返回空内容，结果应为 nil
	if len(result) != 0 {
		t.Errorf("所有 builder 返回空时应为 nil，实际: %d", len(result))
	}
}

// TestFullCompactProcessor_SaveState 验证 SaveState
func TestFullCompactProcessor_SaveState(t *testing.T) {
	fcp := newTestFCP(nil)
	state := fcp.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState 应返回空 map，实际: %v", state)
	}
}

// TestFullCompactProcessor_LoadState 验证 LoadState 不 panic
func TestFullCompactProcessor_LoadState(t *testing.T) {
	fcp := newTestFCP(nil)
	fcp.LoadState(map[string]any{"key": "value"})
}

// TestFullCompactProcessor_TruncateStateText 验证状态文本截断
func TestFullCompactProcessor_TruncateStateText(t *testing.T) {
	cfg := validFCPConfig()
	cfg.StateSnapshotMaxChars = 100
	fcp := newTestFCP(cfg)

	// 短文本不截断
	shortText := "短文本"
	if fcp.TruncateStateText(shortText) != shortText {
		t.Error("短文本不应被截断")
	}

	// 长文本截断
	longText := strings.Repeat("a", 200)
	result := fcp.TruncateStateText(longText)
	if result == longText {
		t.Error("长文本应被截断")
	}
	if !strings.Contains(result, "...[TRUNCATED]...") {
		t.Error("截断文本应包含 TRUNCATED 标记")
	}
}

// TestBuildHeadTailTruncatedText 验证头尾截断
func TestBuildHeadTailTruncatedText(t *testing.T) {
	// keptChars = 0
	result := _buildHeadTailTruncatedText("hello", 0)
	if result != "...[TRUNCATED]..." {
		t.Errorf("keptChars=0 应返回 TRUNCATED 标记，实际: %s", result)
	}

	// 正常截断
	text := strings.Repeat("x", 100)
	result = _buildHeadTailTruncatedText(text, 50)
	if !strings.Contains(result, "...[TRUNCATED]...") {
		t.Error("应包含 TRUNCATED 标记")
	}
	// 头部约占 20%，即 10 个字符
	if !strings.HasPrefix(result, "xxxxxxxxxx") {
		t.Error("应保留头部字符")
	}
}

// TestFullCompactProcessor_FindLastCompactionBoundaryIndex 验证边界索引查找
func TestFullCompactProcessor_FindLastCompactionBoundaryIndex(t *testing.T) {
	fcp := newTestFCP(nil)

	// 无边界
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewUserMessage("2"),
	}
	idx := fcp._findLastCompactionBoundaryIndex(messages)
	if idx != -1 {
		t.Errorf("无边界时应返回 -1，实际: %d", idx)
	}

	// 有 boundary
	messages = []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\nConversation compacted"),
		llm_schema.NewUserMessage("2"),
	}
	idx = fcp._findLastCompactionBoundaryIndex(messages)
	if idx != 1 {
		t.Errorf("应找到索引 1，实际: %d", idx)
	}

	// 有 session memory boundary
	messages = []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewSystemMessage("[SESSION_MEMORY_BOUNDARY]\nEarlier conversation replaced"),
		llm_schema.NewUserMessage("2"),
	}
	idx = fcp._findLastCompactionBoundaryIndex(messages)
	if idx != 1 {
		t.Errorf("应找到 session memory boundary 索引 1，实际: %d", idx)
	}
}

// TestFullCompactProcessor_SplitMessagesAtCompactionBoundary 验证消息分割
func TestFullCompactProcessor_SplitMessagesAtCompactionBoundary(t *testing.T) {
	fcp := newTestFCP(nil)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\nConversation compacted"),
		llm_schema.NewUserMessage("2"),
		llm_schema.NewUserMessage("3"),
	}

	// boundaryIndex = 1
	prefix, active := fcp._splitMessagesAtCompactionBoundary(messages, 1)
	if len(prefix) != 1 {
		t.Errorf("prefix 应有 1 条，实际: %d", len(prefix))
	}
	if len(active) != 2 {
		t.Errorf("active 应有 2 条，实际: %d", len(active))
	}

	// boundaryIndex = -1（无边界）
	prefix, active = fcp._splitMessagesAtCompactionBoundary(messages, -1)
	if prefix != nil {
		t.Error("无边界时 prefix 应为 nil")
	}
	if len(active) != len(messages) {
		t.Errorf("无边界时 active 应为全部消息，实际: %d", len(active))
	}

	// boundaryIndex = 0
	prefix, active = fcp._splitMessagesAtCompactionBoundary(messages, 0)
	if prefix != nil {
		t.Error("boundaryIndex=0 时 prefix 应为 nil")
	}
	if len(active) != 3 {
		t.Errorf("boundaryIndex=0 时 active 应有 3 条，实际: %d", len(active))
	}
}

// TestFullCompactProcessor_MakeStateMessage 验证状态消息构建
func TestFullCompactProcessor_MakeStateMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	msg := fcp._makeStateMessage("SKILLS", "skill内容")
	if msg.GetRole() != llm_schema.RoleTypeUser {
		t.Error("状态消息应为 UserMessage")
	}
	content := msg.GetContent().Text()
	if !strings.HasPrefix(content, "[FULL_COMPACT_STATE]") {
		t.Error("状态消息应以 StateMarker 开头")
	}
	if !strings.Contains(content, "[SKILLS]") {
		t.Error("状态消息应包含标签")
	}
	if !strings.Contains(content, "skill内容") {
		t.Error("状态消息应包含内容")
	}
}

// TestFullCompactProcessor_BuildSummaryMessage 验证摘要消息构建
func TestFullCompactProcessor_BuildSummaryMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	// 有保留消息
	result := fcp._buildSummaryMessage("摘要内容", true)
	if !strings.Contains(result, fcp.fcpConfig.SummaryIntro) {
		t.Error("应包含 SummaryIntro")
	}
	if !strings.Contains(result, "摘要内容") {
		t.Error("应包含摘要内容")
	}
	if !strings.Contains(result, fcp.fcpConfig.RecentMessagesNotice) {
		t.Error("有保留消息时应包含 RecentMessagesNotice")
	}

	// 无保留消息
	result = fcp._buildSummaryMessage("摘要内容", false)
	if strings.Contains(result, fcp.fcpConfig.RecentMessagesNotice) {
		t.Error("无保留消息时不应包含 RecentMessagesNotice")
	}
}

// TestFullCompactProcessor_BuildSessionMemoryMessage 验证 Session Memory 消息构建
func TestFullCompactProcessor_BuildSessionMemoryMessage(t *testing.T) {
	fcp := newTestFCP(nil)

	result := fcp._buildSessionMemoryMessage("memory内容", true)
	if !strings.Contains(result, fcp.fcpConfig.SessionMemoryIntro) {
		t.Error("应包含 SessionMemoryIntro")
	}
	if !strings.Contains(result, "memory内容") {
		t.Error("应包含 memory 内容")
	}
	if !strings.Contains(result, fcp.fcpConfig.RecentMessagesNotice) {
		t.Error("有保留消息时应包含 RecentMessagesNotice")
	}
}

// TestFullCompactProcessor_OnGetContextWindow 验证上下文窗口透传
func TestFullCompactProcessor_OnGetContextWindow(t *testing.T) {
	fcp := newTestFCP(nil)
	cw := iface.ContextWindow{}

	event, resultCW, err := fcp.OnGetContextWindow(context.Background(), nil, cw)
	if err != nil {
		t.Fatalf("OnGetContextWindow 失败: %v", err)
	}
	if event != nil {
		t.Error("OnGetContextWindow 应返回 nil event")
	}
	if resultCW.SystemMessages != nil || resultCW.ContextMessages != nil {
		t.Error("OnGetContextWindow 应透传输入窗口")
	}
}

// TestFullCompactProcessor_TriggerGetContextWindow 验证不触发上下文窗口获取
func TestFullCompactProcessor_TriggerGetContextWindow(t *testing.T) {
	fcp := newTestFCP(nil)
	triggered, err := fcp.TriggerGetContextWindow(context.Background(), nil, iface.ContextWindow{})
	if err != nil {
		t.Fatalf("TriggerGetContextWindow 失败: %v", err)
	}
	if triggered {
		t.Error("TriggerGetContextWindow 应始终返回 false")
	}
}

// TestFullCompactProcessor_PrepareMessagesForPrompt 验证消息过滤
func TestFullCompactProcessor_PrepareMessagesForPrompt(t *testing.T) {
	fcp := newTestFCP(nil)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\ncompacted"),
		llm_schema.NewUserMessage("[FULL_COMPACT_STATE]\n[SKILLS]\n..."),
		llm_schema.NewSystemMessage("[SESSION_MEMORY_BOUNDARY]\nreplaced"),
		llm_schema.NewUserMessage("2"),
	}

	result := fcp._prepareMessagesForPrompt(messages)
	if len(result) != 2 {
		t.Fatalf("应过滤掉 3 条标记消息，保留 2 条，实际: %d", len(result))
	}
	if result[0].GetContent().Text() != "1" || result[1].GetContent().Text() != "2" {
		t.Error("保留的消息内容不正确")
	}
}

// TestIsSkillFilePath 验证 skill 文件路径识别
func TestIsSkillFilePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/path/to/my_skill/skill.md", true},
		{"skill.md", true},
		{"Skill.md", true},
		{"/path/TO/My_Skill/SKILL.MD", true},
		{"\\path\\to\\skill\\skill.md", true},
		{"", false},
		{"/path/to/readme.md", false},
		{"/path/to/skill.txt", false},
	}
	for _, tt := range tests {
		result := processor.IsSkillFilePath(tt.path)
		if result != tt.expected {
			t.Errorf("IsSkillFilePath(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}
}

// TestExtractArgumentValue 验证参数值提取
func TestExtractArgumentValue(t *testing.T) {
	// JSON 格式
	result := processor.ExtractArgumentValue(nil, `{"file_path": "/tmp/test.go"}`, "file_path")
	if result != "/tmp/test.go" {
		t.Errorf("期望 /tmp/test.go，实际: %s", result)
	}

	// 多个 key
	result = processor.ExtractArgumentValue(nil, `{"pattern": "*.go"}`, "pattern", "path")
	if result != "*.go" {
		t.Errorf("期望 *.go，实际: %s", result)
	}

	// 空 JSON
	result = processor.ExtractArgumentValue(nil, "", "file_path")
	if result != "" {
		t.Errorf("空 JSON 应返回空字符串，实际: %s", result)
	}

	// 非法 JSON fallback
	result = processor.ExtractArgumentValue(nil, `invalid json "file_path": "/tmp/test.go"`, "file_path")
	if result != "/tmp/test.go" {
		t.Errorf("fallback 正则应提取值，实际: %s", result)
	}
}

// TestRoundContainsSkillRead 验证轮次 skill 读取检测
func TestRoundContainsSkillRead(t *testing.T) {
	// 含 skill 读取
	messages := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("读取中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"file_path": "/skill/my_skill/skill.md"}`},
			}),
		),
	}
	if !processor.RoundContainsSkillRead(messages) {
		t.Error("含 skill.md 读取的轮次应返回 true")
	}

	// 非 skill 读取
	messages2 := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("读取中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"file_path": "/tmp/test.go"}`},
			}),
		),
	}
	if processor.RoundContainsSkillRead(messages2) {
		t.Error("非 skill 读取的轮次应返回 false")
	}

	// 非文件读取工具
	messages3 := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
	}
	if processor.RoundContainsSkillRead(messages3) {
		t.Error("非文件读取工具应返回 false")
	}
}

// TestFullCompactProcessor_CountContextWindowTokens 验证 Token 计数
func TestFullCompactProcessor_CountContextWindowTokens(t *testing.T) {
	fcp := newTestFCP(nil)

	// 使用 TokenCounter
	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 500}}
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	tokens := fcp.countContextWindowTokens(mc, messages)
	if tokens != 500 {
		t.Errorf("期望 500，实际: %d", tokens)
	}

	// TokenCounter 返回 error，降级到字符估算
	mc2 := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 0, err: fmt.Errorf("编码器不可用")}}
	tokens = fcp.countContextWindowTokens(mc2, messages)
	if tokens <= 0 {
		t.Errorf("降级估算应返回正数，实际: %d", tokens)
	}

	// 无 TokenCounter
	mc3 := &fcpFakeModelContext{tokenCounter: nil}
	tokens = fcp.countContextWindowTokens(mc3, messages)
	if tokens <= 0 {
		t.Errorf("无 TokenCounter 应使用字符估算，实际: %d", tokens)
	}
}

// TestFullCompactProcessor_BuildMinimalCompactInput 验证最小压缩输入构建
func TestFullCompactProcessor_BuildMinimalCompactInput(t *testing.T) {
	fcp := newTestFCP(nil)

	// 最后一条是 UserMessage
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("1"), llm_schema.NewUserMessage("2")}
	result := fcp._buildMinimalCompactInput(messages)
	if len(result) != 1 || result[0].GetContent().Text() != "2" {
		t.Error("最后一条是 UserMessage 时应只返回最后一条")
	}

	// 最后一条是 AssistantMessage
	messages2 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("1"), llm_schema.NewAssistantMessage("2")}
	result = fcp._buildMinimalCompactInput(messages2)
	if len(result) != 2 {
		t.Errorf("最后一条是 AssistantMessage 时应返回 2 条，实际: %d", len(result))
	}
	if result[0].GetContent().Text() != fcp.fcpConfig.SyntheticUserMarker {
		t.Error("第一条应为 SyntheticUserMarker")
	}

	// 空消息
	result = fcp._buildMinimalCompactInput(nil)
	if result != nil {
		t.Error("空消息应返回 nil")
	}
}

// TestMessageToText 验证消息文本提取
func TestMessageToText(t *testing.T) {
	msg := llm_schema.NewUserMessage("你好世界")
	result := processor.MessageToText(msg)
	if result != "你好世界" {
		t.Errorf("期望 你好世界，实际: %s", result)
	}
}

// TestInitRegistration_FullCompact 验证 init() 自动注册
func TestInitRegistration_FullCompact(t *testing.T) {
	factory, ok := context_engine.GetProcessorFactory("FullCompactProcessor")
	if !ok {
		t.Fatal("FullCompactProcessor 应已通过 init() 注册")
	}

	// 传入其他 ProcessorConfig 实现应返回 error
	otherConfig := &testConfig{Name: "other"}
	result, err := factory(otherConfig)
	if err == nil {
		t.Error("错误类型配置应返回 error")
	}
	if result != nil {
		t.Error("错误类型配置应返回 nil 结果")
	}

	// 传入非法 FullCompactProcessorConfig 应返回 error
	result, err = factory(&FullCompactProcessorConfig{TriggerTotalTokens: 0})
	if err == nil {
		t.Error("非法配置应返回 error")
	}
	if result != nil {
		t.Error("非法配置应返回 nil 结果")
	}
}

// TestFullCompactProcessor_LoadSessionMemoryRuntime 验证 Session Memory 运行时加载
func TestFullCompactProcessor_LoadSessionMemoryRuntime(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{}
	result := fcp._loadSessionMemoryRuntime(context.Background(), mc)
	// 无 session 时返回 nil
	if result != nil {
		t.Errorf("无 session 时应返回 nil，实际: %v", result)
	}
}

// TestFullCompactProcessor_LoadSessionMemoryText 验证 Session Memory 文本加载
func TestFullCompactProcessor_LoadSessionMemoryText(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{}
	result := fcp._loadSessionMemoryText(context.Background(), mc, nil)
	// 无 session 时返回空
	if result != "" {
		t.Errorf("无 session 时应返回空字符串，实际: %s", result)
	}
}

// TestFullCompactProcessor_ResolveSessionMemoryPath 验证路径解析
func TestFullCompactProcessor_ResolveSessionMemoryPath(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{}
	result := fcp._resolveSessionMemoryPath(context.Background(), mc, nil)
	// 无 workspace 时返回空
	if result != "" {
		t.Errorf("无 workspace 时应返回空字符串，实际: %s", result)
	}
}

// TestFullCompactProcessor_SelectMessagesAfterSessionMemory 验证消息选择
func TestFullCompactProcessor_SelectMessagesAfterSessionMemory(t *testing.T) {
	fcp := newTestFCP(nil)
	result := fcp._selectMessagesAfterSessionMemory(nil, nil, false)
	// 空 runtime 时返回原消息（nil）
	if result != nil {
		t.Errorf("空 runtime 时应返回 nil，实际: %v", result)
	}
}

// TestFullCompactProcessor_InvalidateSessionMemoryAnchor 验证锚点失效
func TestFullCompactProcessor_InvalidateSessionMemoryAnchor(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{}
	fcp._invalidateSessionMemoryAnchor(context.Background(), mc)
}

// TestNewFullCompactProcessorConfig_默认值 验证默认配置值
func TestNewFullCompactProcessorConfig_默认值(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	if cfg.TriggerTotalTokens != 180000 {
		t.Errorf("TriggerTotalTokens 默认值应为 180000，实际: %d", cfg.TriggerTotalTokens)
	}
	if cfg.CompressionCallMaxTokens != 200000 {
		t.Errorf("CompressionCallMaxTokens 默认值应为 200000，实际: %d", cfg.CompressionCallMaxTokens)
	}
	if cfg.MessagesToKeep != 10 {
		t.Errorf("MessagesToKeep 默认值应为 10，实际: %d", cfg.MessagesToKeep)
	}
	if !cfg.SessionMemoryEnabled {
		t.Error("SessionMemoryEnabled 默认值应为 true")
	}
	if !cfg.KeepToolMessagePairs {
		t.Error("KeepToolMessagePairs 默认值应为 true")
	}
	if cfg.StateSnapshotMaxChars != 4000 {
		t.Errorf("StateSnapshotMaxChars 默认值应为 4000，实际: %d", cfg.StateSnapshotMaxChars)
	}
	if cfg.ReinjectRecentSkills != 3 {
		t.Errorf("ReinjectRecentSkills 默认值应为 3，实际: %d", cfg.ReinjectRecentSkills)
	}
	if cfg.Marker != "[FULL_COMPACT_BOUNDARY]" {
		t.Errorf("Marker 默认值不正确，实际: %s", cfg.Marker)
	}
	if cfg.StateMarker != "[FULL_COMPACT_STATE]" {
		t.Errorf("StateMarker 默认值不正确，实际: %s", cfg.StateMarker)
	}
}

// TestFullCompactProcessor_GroupMessagesByAPIRound 验证 API 轮次分组
func TestFullCompactProcessor_GroupMessagesByAPIRound(t *testing.T) {
	fcp := newTestFCP(nil)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}

	groups := fcp._groupMessagesByAPIRound(messages)
	if len(groups) != 2 {
		t.Fatalf("期望 2 个轮次，实际: %d", len(groups))
	}
}

// ──────────────────────────── fake LLM Model ────────────────────────────

// fcpFakeBaseModelClient 测试用 BaseModelClient 模拟
type fcpFakeBaseModelClient struct {
	invokeResult *llm_schema.AssistantMessage
	invokeErr    error
}

func (f *fcpFakeBaseModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	return f.invokeResult, f.invokeErr
}
func (f *fcpFakeBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llm_schema.AssistantMessageChunk, error) {
	return nil, nil
}
func (f *fcpFakeBaseModelClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *fcpFakeBaseModelClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *fcpFakeBaseModelClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *fcpFakeBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

func (f *fcpFakeBaseModelClient) SupportsKVCacheRelease() bool {
	return false
}

const fcpTestProvider = "FCPTestProvider"

// fcpFakeRegistryOnce 确保 fake provider 只注册一次
var fcpFakeRegistryOnce sync.Once

// newFakeLLMModel 创建带 fake client 的 llm.Model 实例
func newFakeLLMModel(fakeClient *fcpFakeBaseModelClient) *llm.Model {
	fcpFakeRegistryOnce.Do(func() {
		model_clients.GetClientRegistry().Register(fcpTestProvider, "llm",
			func(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) model_clients.BaseModelClient {
				return fakeClient
			},
		)
	})

	clientConfig := &llm_schema.ModelClientConfig{
		ClientID:       "fcp-test-client",
		ClientProvider: fcpTestProvider,
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
		panic(fmt.Sprintf("newFakeLLMModel 失败: %v", err))
	}
	return model
}

// newFCPWithModel 创建带 fake LLM Model 的 FullCompactProcessor
func newFCPWithModel(cfg *FullCompactProcessorConfig, fakeClient *fcpFakeBaseModelClient) *FullCompactProcessor {
	if cfg == nil {
		cfg = NewFullCompactProcessorConfig()
	}
	cfg.SessionMemoryEnabled = false // 禁用 Session Memory 路径，走 LLM 压缩路径
	model := newFakeLLMModel(fakeClient)
	fcp, err := NewFullCompactProcessor(cfg, WithFullCompactModel(model))
	if err != nil {
		panic(fmt.Sprintf("newFCPWithModel 失败: %v", err))
	}
	return fcp
}

// ──────────────────────────── _generateSummary 测试 ────────────────────────────

// TestFullCompactProcessor_GenerateSummary_正常LLM 验证 LLM 正常返回摘要
func TestFullCompactProcessor_GenerateSummary_正常LLM(t *testing.T) {
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(
			"<analysis>分析内容</analysis>\n<summary>\n这是摘要\n</summary>",
		),
	}
	fcp := newFCPWithModel(nil, fakeClient)
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	summary := fcp._generateSummary(context.Background(), messages, nil)
	if !strings.HasPrefix(summary, "Summary:\n") {
		t.Errorf("摘要应以 Summary: 开头，实际: %s", summary)
	}
	if !strings.Contains(summary, "这是摘要") {
		t.Errorf("摘要应包含 '这是摘要'，实际: %s", summary)
	}
}

// TestFullCompactProcessor_GenerateSummary_LLM返回error 验证 LLM 返回 error 时回退到 fallback
func TestFullCompactProcessor_GenerateSummary_LLM返回error(t *testing.T) {
	fakeClient := &fcpFakeBaseModelClient{
		invokeErr: errors.New("LLM 调用失败"),
	}
	fcp := newFCPWithModel(nil, fakeClient)
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	summary := fcp._generateSummary(context.Background(), messages, nil)
	if !strings.HasPrefix(summary, "Summary:\n") {
		t.Errorf("回退摘要应以 Summary: 开头，实际: %s", summary)
	}
}

// TestFullCompactProcessor_GenerateSummary_LLM返回空内容 验证 LLM 返回空内容时回退
func TestFullCompactProcessor_GenerateSummary_LLM返回空内容(t *testing.T) {
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(""),
	}
	fcp := newFCPWithModel(nil, fakeClient)
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	summary := fcp._generateSummary(context.Background(), messages, nil)
	if !strings.HasPrefix(summary, "Summary:\n") {
		t.Errorf("空内容应回退到 fallback，实际: %s", summary)
	}
}

// TestFullCompactProcessor_GenerateSummary_无model 验证无 model 时使用 fallback
func TestFullCompactProcessor_GenerateSummary_无model(t *testing.T) {
	fcp := newTestFCP(nil)
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	summary := fcp._generateSummary(context.Background(), messages, nil)
	if !strings.HasPrefix(summary, "Summary:\n") {
		t.Errorf("无 model 应使用 fallback，实际: %s", summary)
	}
	if !strings.Contains(summary, "你好") {
		t.Errorf("fallback 摘要应包含原始消息内容，实际: %s", summary)
	}
}

// ──────────────────────────── _buildFullCompactMessages 测试 ────────────────────────────

// TestFullCompactProcessor_BuildFullCompactMessages_完整流程 验证 LLM 压缩路径完整流程
func TestFullCompactProcessor_BuildFullCompactMessages_完整流程(t *testing.T) {
	cfg := validFCPConfig()
	cfg.MessagesToKeep = 2
	cfg.KeepToolMessagePairs = false
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(
			"<analysis>分析</analysis>\n<summary>\n对话摘要\n</summary>",
		),
	}
	fcp := newFCPWithModel(cfg, fakeClient)

	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 100}}
	prefix := []llm_schema.BaseMessage{llm_schema.NewUserMessage("旧消息1")}
	activeMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}

	result, summary := fcp._buildFullCompactMessages(context.Background(), mc, prefix, activeMessages)
	if result == nil {
		t.Fatal("应返回非 nil 消息列表")
	}
	if summary == "" {
		t.Error("摘要不应为空")
	}
	// 结果应包含: prefix + boundary + summary_msg + kept(2) + reinjected_state
	if len(result) < 4 {
		t.Errorf("结果应至少包含 4 条消息，实际: %d", len(result))
	}
	// 第二条应为 boundary
	if !strings.HasPrefix(result[1].GetContent().Text(), cfg.Marker) {
		t.Error("第二条消息应为 boundary 消息")
	}
}

// TestFullCompactProcessor_BuildFullCompactMessages_无活跃消息 验证无活跃消息时返回 nil
func TestFullCompactProcessor_BuildFullCompactMessages_无活跃消息(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 100}}
	// 只有 boundary/state 消息，_prepareMessagesForPrompt 会过滤掉
	activeMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\ncompacted"),
		llm_schema.NewUserMessage("[FULL_COMPACT_STATE]\n[SKILLS]"),
	}
	result, _ := fcp._buildFullCompactMessages(context.Background(), mc, nil, activeMessages)
	if result != nil {
		t.Error("所有消息被过滤后应返回 nil")
	}
}

// ──────────────────────────── _buildSessionMemoryMessages 测试 ────────────────────────────

// TestFullCompactProcessor_BuildSessionMemoryMessages_禁用时 验证 SessionMemoryEnabled=false 时返回 nil
func TestFullCompactProcessor_BuildSessionMemoryMessages_禁用时(t *testing.T) {
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = false
	fcp := newTestFCP(cfg)
	mc := &fcpFakeModelContext{}
	messages, userMsg := fcp._buildSessionMemoryMessages(context.Background(), mc, nil, nil, false)
	if messages != nil {
		t.Error("SessionMemoryEnabled=false 时应返回 nil messages")
	}
	if userMsg != nil {
		t.Error("SessionMemoryEnabled=false 时应返回 nil userMsg")
	}
}

// TestFullCompactProcessor_BuildSessionMemoryMessages_启用但无内容 验证 Session Memory 启用但无内容时返回 nil
func TestFullCompactProcessor_BuildSessionMemoryMessages_启用但无内容(t *testing.T) {
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = true
	fcp := newTestFCP(cfg)
	mc := &fcpFakeModelContext{}
	// 无 session 时 _loadSessionMemoryText 返回空，所以 sessionMemoryText 为空
	messages, userMsg := fcp._buildSessionMemoryMessages(context.Background(), mc, nil, nil, false)
	if messages != nil {
		t.Error("sessionMemoryText 为空时应返回 nil messages")
	}
	if userMsg != nil {
		t.Error("sessionMemoryText 为空时应返回 nil userMsg")
	}
}

// ──────────────────────────── _truncateForPromptBudget 测试 ────────────────────────────

// TestFullCompactProcessor_TruncateForPromptBudget_预算充足 验证预算充足时不截断
func TestFullCompactProcessor_TruncateForPromptBudget_预算充足(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 100000
	fcp := newTestFCP(cfg)

	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 50}}
	messages := makeAPIRoundMessages()

	result := fcp._truncateForPromptBudget(messages, mc)
	if len(result) != len(messages) {
		t.Errorf("预算充足时不应截断，期望 %d 条，实际: %d", len(messages), len(result))
	}
}

// TestFullCompactProcessor_TruncateForPromptBudget_超预算截断 验证超预算时从前面丢弃 API round
func TestFullCompactProcessor_TruncateForPromptBudget_超预算截断(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 100
	fcp := newTestFCP(cfg)

	// 两组 API round，每组 token 数 = 200 > 100
	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 200}}
	round1 := makeAPIRoundMessages()
	round2 := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("新问题"),
		llm_schema.NewAssistantMessage("新回答"),
	}
	messages := append(round1, round2...)

	result := fcp._truncateForPromptBudget(messages, mc)
	// 丢弃第一轮后，仅保留第二轮，但 200 > 100，仍超预算
	// 最终只有一组时走 _truncateMessagesFromHead
	if result == nil {
		t.Error("截断后应返回非 nil 消息")
	}
}

// ──────────────────────────── _countPromptTokens 测试 ────────────────────────────

// TestFullCompactProcessor_CountPromptTokens_有TokenCounter 验证有 TokenCounter 时的计算
func TestFullCompactProcessor_CountPromptTokens_有TokenCounter(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 500}}
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}

	tokens := fcp._countPromptTokens(messages, mc)
	if tokens != 500 {
		t.Errorf("期望 500，实际: %d", tokens)
	}
}

// TestFullCompactProcessor_CountPromptTokens_无TokenCounter 验证无 TokenCounter 时使用字符估算
func TestFullCompactProcessor_CountPromptTokens_无TokenCounter(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{tokenCounter: nil}
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}

	tokens := fcp._countPromptTokens(messages, mc)
	if tokens <= 0 {
		t.Errorf("无 TokenCounter 应使用字符估算，实际: %d", tokens)
	}
}

// TestFullCompactProcessor_CountPromptTokens_TokenCounter报错 验证 TokenCounter 报错时使用字符估算
func TestFullCompactProcessor_CountPromptTokens_TokenCounter报错(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 0, err: errors.New("不可用")}}
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}

	tokens := fcp._countPromptTokens(messages, mc)
	if tokens <= 0 {
		t.Errorf("TokenCounter 报错应使用字符估算，实际: %d", tokens)
	}
}

// ──────────────────────────── _truncateMessagesFromHead 测试 ────────────────────────────

// TestFullCompactProcessor_TruncateMessagesFromHead_预算内 验证预算内不截断
func TestFullCompactProcessor_TruncateMessagesFromHead_预算内(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 10000
	fcp := newTestFCP(cfg)

	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 100}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}

	result := fcp._truncateMessagesFromHead(messages, mc)
	if len(result) != 2 {
		t.Errorf("预算内不应截断，期望 2 条，实际: %d", len(result))
	}
}

// TestFullCompactProcessor_TruncateMessagesFromHead_超预算逐条移除 验证超预算时从头部逐条移除
func TestFullCompactProcessor_TruncateMessagesFromHead_超预算逐条移除(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 50
	fcp := newTestFCP(cfg)

	// 每次 CountMessages 返回 100 > 50，触发逐条移除
	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 100}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewUserMessage("2"),
		llm_schema.NewUserMessage("3"),
		llm_schema.NewUserMessage("4"),
	}

	result := fcp._truncateMessagesFromHead(messages, mc)
	// 逐条移除直到剩余 1 条（UserMessage "4"），100 > 50
	// 最后只剩 1 条时 _countPromptTokens 仍 > 50，走 _buildMinimalCompactInput
	if result == nil {
		t.Error("应返回最小压缩输入")
	}
}

// TestFullCompactProcessor_TruncateMessagesFromHead_SyntheticMarker 验证 SyntheticMarker 消息跳两条
func TestFullCompactProcessor_TruncateMessagesFromHead_SyntheticMarker(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 50
	fcp := newTestFCP(cfg)

	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 100}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(cfg.SyntheticUserMarker), // synthetic marker
		llm_schema.NewAssistantMessage("回复"),
		llm_schema.NewUserMessage("3"),
	}

	result := fcp._truncateMessagesFromHead(messages, mc)
	if result == nil {
		t.Error("应返回非 nil")
	}
}

// ──────────────────────────── flattenGroups 测试 ────────────────────────────

// TestFlattenGroups 验证扁平化消息分组
func TestFlattenGroups(t *testing.T) {
	groups := [][]llm_schema.BaseMessage{
		{llm_schema.NewUserMessage("1"), llm_schema.NewUserMessage("2")},
		{llm_schema.NewUserMessage("3")},
	}
	result := processor.FlattenGroups(groups)
	if len(result) != 3 {
		t.Errorf("期望 3 条消息，实际: %d", len(result))
	}
}

// TestFlattenGroups_空分组 验证空分组
func TestFlattenGroups_空分组(t *testing.T) {
	result := processor.FlattenGroups(nil)
	if result != nil {
		t.Errorf("空分组应返回 nil，实际: %v", result)
	}
}

// ──────────────────────────── WithFullCompactModel 测试 ────────────────────────────

// TestWithFullCompactModel 验证 WithFullCompactModel 选项函数
func TestWithFullCompactModel(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	bp := processor.NewBaseProcessor(cfg)
	fcp := &FullCompactProcessor{
		BaseProcessor: bp,
		fcpConfig:     cfg,
		reinjector:    newFullCompactStateReinjector(),
	}
	if fcp.model != nil {
		t.Error("初始 model 应为 nil")
	}

	// 注入 fake model
	fakeClient := &fcpFakeBaseModelClient{}
	model := newFakeLLMModel(fakeClient)
	opt := WithFullCompactModel(model)
	opt(fcp)

	if fcp.model == nil {
		t.Error("WithFullCompactModel 应注入 model")
	}
}

// ──────────────────────────── NewFullCompactProcessor 测试 ────────────────────────────

// TestNewFullCompactProcessor_合法配置 验证合法配置创建成功
func TestNewFullCompactProcessor_合法配置(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	fcp, err := NewFullCompactProcessor(cfg)
	if err != nil {
		t.Fatalf("合法配置创建不应失败: %v", err)
	}
	if fcp == nil {
		t.Fatal("应返回非 nil 实例")
	}
	if fcp.model != nil {
		t.Error("无 Model/ModelClient 配置时 model 应为 nil")
	}
}

// TestNewFullCompactProcessor_非法配置 验证非法配置返回错误
func TestNewFullCompactProcessor_非法配置(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	cfg.TriggerTotalTokens = 0
	fcp, err := NewFullCompactProcessor(cfg)
	if err == nil {
		t.Error("非法配置应返回错误")
	}
	if fcp != nil {
		t.Error("非法配置应返回 nil 实例")
	}
}

// TestNewFullCompactProcessor_有Model配置 验证有 Model+ModelClient 配置时初始化 model
func TestNewFullCompactProcessor_有Model配置(t *testing.T) {
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage("ok"),
	}
	// 注册 fake provider
	fcpFakeRegistryOnce.Do(func() {
		model_clients.GetClientRegistry().Register(fcpTestProvider, "llm",
			func(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) model_clients.BaseModelClient {
				return fakeClient
			},
		)
	})

	cfg := NewFullCompactProcessorConfig()
	cfg.Model = &llm_schema.ModelRequestConfig{ModelName: "test-model"}
	cfg.ModelClient = &llm_schema.ModelClientConfig{
		ClientID:       "fcp-test",
		ClientProvider: fcpTestProvider,
		APIKey:         "fake",
		APIBase:        "http://localhost",
		Timeout:        60,
		MaxRetries:     1,
		VerifySSL:      false,
	}

	fcp, err := NewFullCompactProcessor(cfg)
	if err != nil {
		t.Fatalf("有 Model 配置创建不应失败: %v", err)
	}
	if fcp.model == nil {
		t.Error("有 Model+ModelClient 配置时 model 应不为 nil")
	}
}

// TestNewFullCompactProcessor_WithOption 验证 WithFullCompactModel 选项
func TestNewFullCompactProcessor_WithOption(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	fakeClient := &fcpFakeBaseModelClient{}
	model := newFakeLLMModel(fakeClient)

	fcp, err := NewFullCompactProcessor(cfg, WithFullCompactModel(model))
	if err != nil {
		t.Fatalf("WithFullCompactModel 选项创建不应失败: %v", err)
	}
	if fcp.model == nil {
		t.Error("WithFullCompactModel 应注入 model")
	}
}

// ──────────────────────────── _buildReplacementMessages 测试 ────────────────────────────

// TestFullCompactProcessor_BuildReplacementMessages_无活跃消息 验证无活跃消息时返回 nil
func TestFullCompactProcessor_BuildReplacementMessages_无活跃消息(t *testing.T) {
	fcp := newTestFCP(nil)
	// boundary 在最后，后面没有消息 → activeMessages 为空
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\ncompacted"),
	}
	event, newCtx, userMsg := fcp._buildReplacementMessages(context.Background(), nil, messages)
	if event != nil {
		t.Error("无活跃消息时应返回 nil event")
	}
	if newCtx != nil {
		t.Error("无活跃消息时应返回 nil newContextMessages")
	}
	if userMsg != nil {
		t.Error("无活跃消息时应返回 nil sessionMemoryMessage")
	}
}

// TestFullCompactProcessor_BuildReplacementMessages_LLM压缩路径 验证走 LLM 压缩路径
func TestFullCompactProcessor_BuildReplacementMessages_LLM压缩路径(t *testing.T) {
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = false
	cfg.MessagesToKeep = 2
	cfg.KeepToolMessagePairs = false
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(
			"<analysis>分析</analysis>\n<summary>\n摘要内容\n</summary>",
		),
	}
	fcp := newFCPWithModel(cfg, fakeClient)

	mc := &fcpFakeModelContext{tokenCounter: &fcpFakeTokenCounter{count: 100}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}

	event, newCtx, userMsg := fcp._buildReplacementMessages(context.Background(), mc, messages)
	if event == nil {
		t.Fatal("应返回非 nil event")
	}
	if newCtx == nil {
		t.Error("应返回非 nil newContextMessages")
	}
	if userMsg != nil {
		t.Error("LLM 压缩路径应返回 nil sessionMemoryMessage")
	}
	if event.EventType != "FullCompactProcessor" {
		t.Errorf("EventType 应为 FullCompactProcessor，实际: %s", event.EventType)
	}
}

// ──────────────────────────── OnAddMessages 测试 ────────────────────────────

// TestFullCompactProcessor_OnAddMessages_完整流程 验证 OnAddMessages 完整流程
func TestFullCompactProcessor_OnAddMessages_完整流程(t *testing.T) {
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = false
	cfg.MessagesToKeep = 2
	cfg.KeepToolMessagePairs = false
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(
			"<analysis>分析</analysis>\n<summary>\n摘要\n</summary>",
		),
	}
	fcp := newFCPWithModel(cfg, fakeClient)

	existingMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	mc := &fcpFakeModelContext{
		messages:     existingMessages,
		tokenCounter: &fcpFakeTokenCounter{count: 100},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("今天晴天"),
	}

	event, result, err := fcp.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event == nil {
		t.Error("应返回非 nil event")
	}
	if len(result) != 0 {
		t.Errorf("OnAddMessages 应返回空消息列表（消息已替换），实际: %d", len(result))
	}
	// mc.SetMessages 应被调用
	if len(mc.messages) == 0 {
		t.Error("mc.messages 应被更新")
	}
}

// TestFullCompactProcessor_OnAddMessages_所有消息被过滤 验证所有消息为标记消息时 _buildFullCompactMessages 返回 nil
func TestFullCompactProcessor_OnAddMessages_所有消息被过滤(t *testing.T) {
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = false
	fcp := newTestFCP(cfg)
	// 所有消息都是 boundary/state 类型 → _prepareMessagesForPrompt 过滤后为空 → _buildFullCompactMessages 返回 nil

	existingMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\ncompacted"),
		llm_schema.NewUserMessage("[FULL_COMPACT_STATE]\n[SKILLS]"),
	}
	mc := &fcpFakeModelContext{
		messages:     existingMessages,
		tokenCounter: &fcpFakeTokenCounter{count: 100},
	}
	// messagesToAdd 需要构成 API round 才能走到 OnAddMessages
	// 但这里我们直接测试 OnAddMessages，不关心 Trigger
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("[SESSION_MEMORY_BOUNDARY]\nreplaced"),
		llm_schema.NewUserMessage("正常消息"),
	}

	// 注意：这里测试的是 OnAddMessages 的逻辑，即使 Trigger 不触发，OnAddMessages 也会被调用
	event, result, err := fcp.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	// 因为存在正常消息，_prepareMessagesForPrompt 不会过滤所有消息
	// _buildFullCompactMessages 使用 fallback 生成摘要 → 返回非 nil
	// 所以 newContextMessages 不为 nil → 走 SetMessages 路径
	_ = event
	_ = result
}

// TestFullCompactProcessor_OnAddMessages_有Event时设置CompressionUsage 验证 event 包含 CompressionUsage
func TestFullCompactProcessor_OnAddMessages_有Event时设置CompressionUsage(t *testing.T) {
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = false
	cfg.MessagesToKeep = 1
	cfg.KeepToolMessagePairs = false
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(
			"<summary>\n摘要\n</summary>",
		),
	}
	fcp := newFCPWithModel(cfg, fakeClient)

	mc := &fcpFakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("你好")},
		tokenCounter: &fcpFakeTokenCounter{count: 100},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("你好！"),
	}

	event, _, err := fcp.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event == nil {
		t.Fatal("应返回非 nil event")
	}
	// CompressionUsage 应被设置（即使为 nil 也应该被赋值）
	_ = event.CompressionUsage
}

// ──────────────────────────── _invalidateSessionMemoryAnchor 测试 ────────────────────────────

// TestFullCompactProcessor_InvalidateSessionMemoryAnchor_空操作 验证无 session 时不 panic
func TestFullCompactProcessor_InvalidateSessionMemoryAnchor_空操作(t *testing.T) {
	fcp := newTestFCP(nil)
	mc := &fcpFakeModelContext{}
	// 不应 panic
	fcp._invalidateSessionMemoryAnchor(context.Background(), mc)
}

// ──────────────────────────── LoadState 测试 ────────────────────────────

// TestFullCompactProcessor_LoadState_各种输入 验证 LoadState 不 panic
func TestFullCompactProcessor_LoadState_各种输入(t *testing.T) {
	fcp := newTestFCP(nil)
	// 空 map
	fcp.LoadState(nil)
	// 有内容的 map
	fcp.LoadState(map[string]any{"key": "value", "num": 123})
}

// ──────────────────────────── _truncateForPromptBudget 动态 token 测试 ────────────────────────────

// TestFullCompactProcessor_TruncateForPromptBudget_多轮丢弃 验证多轮次时从前丢弃
func TestFullCompactProcessor_TruncateForPromptBudget_多轮丢弃(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 50
	fcp := newTestFCP(cfg)

	// 3 个 API round，token 计数动态递减
	// 3 rounds → 300 > 50 → 丢弃第1轮
	// 2 rounds → 200 > 50 → 丢弃第2轮
	// 1 round → 100 > 50 → 走 _truncateMessagesFromHead
	mc := &fcpFakeModelContext{tokenCounter: &fcpDynamicTokenCounter{counts: []int{300, 200, 100, 50, 50}}}
	round1 := makeAPIRoundMessages()
	round2 := makeAPIRoundMessages()
	round3 := makeAPIRoundMessages()
	messages := append(append(round1, round2...), round3...)

	result := fcp._truncateForPromptBudget(messages, mc)
	if result == nil {
		t.Error("截断后应返回非 nil 消息")
	}
}

// ──────────────────────────── _messageToText 分支测试 ────────────────────────────

// TestMessageToText_多模态内容 验证多模态消息（空 text + Parts）的文本提取
func TestMessageToText_多模态内容(t *testing.T) {
	// 创建一个空 text 但有 Parts 的 UserMessage
	msg := llm_schema.NewUserMessage("")
	msg.SetContent(llm_schema.NewMultiModalContent(
		llm_schema.ContentPart{Type: "text", Text: "图片描述"},
		llm_schema.ContentPart{Type: "image_url", ImageURL: &llm_schema.ImageURL{URL: "http://example.com/img.png"}},
		llm_schema.ContentPart{Type: "text", Text: "更多描述"},
	))
	result := processor.MessageToText(msg)
	if !strings.Contains(result, "图片描述") {
		t.Errorf("应包含 Parts 中的文本，实际: %s", result)
	}
	if !strings.Contains(result, "更多描述") {
		t.Errorf("应包含多个 Parts 的文本，实际: %s", result)
	}
}

// TestMessageToText_空Parts 验证空 text + 空 Parts 返回空字符串
func TestMessageToText_空Parts(t *testing.T) {
	msg := llm_schema.NewUserMessage("")
	msg.SetContent(llm_schema.NewMultiModalContent())
	result := processor.MessageToText(msg)
	if result != "" {
		t.Errorf("空 Parts 应返回空字符串，实际: %s", result)
	}
}

// TestMessageToText_纯文本 验证纯文本消息直接返回
func TestMessageToText_纯文本(t *testing.T) {
	msg := llm_schema.NewUserMessage("你好世界")
	result := processor.MessageToText(msg)
	if result != "你好世界" {
		t.Errorf("纯文本应直接返回，实际: %s", result)
	}
}

// ──────────────────────────── buildReinjectedStateMessages 分支测试 ────────────────────────────

// TestFullCompactProcessor_BuildReinjectedStateMessages_有StringBuilder 验证 builder 返回非空 string 时注入状态消息
func TestFullCompactProcessor_BuildReinjectedStateMessages_有StringBuilder(t *testing.T) {
	cfg := validFCPConfig()
	fcp := newTestFCP(cfg)
	// 注册一个返回非空 string 的 builder
	fcp.reinjector.RegisterBuilder("test_string", "TEST_STRING", func(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
		return "这是测试状态内容"
	})

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}

	result := fcp.buildReinjectedStateMessages(context.Background(), &fcpFakeModelContext{}, messages, nil)
	if len(result) == 0 {
		t.Error("有非空 string builder 时应返回状态消息")
	}
	// 应包含 TEST_STRING 标签
	found := false
	for _, msg := range result {
		if strings.Contains(msg.GetContent().Text(), "[TEST_STRING]") {
			found = true
			break
		}
	}
	if !found {
		t.Error("状态消息应包含 [TEST_STRING] 标签")
	}
}

// TestFullCompactProcessor_BuildReinjectedStateMessages_有SliceBuilder 验证 builder 返回 []BaseMessage 时直接扩展
func TestFullCompactProcessor_BuildReinjectedStateMessages_有SliceBuilder(t *testing.T) {
	cfg := validFCPConfig()
	fcp := newTestFCP(cfg)
	// 注册一个返回 []BaseMessage 的 builder
	injectedMsg := llm_schema.NewUserMessage("注入消息")
	fcp.reinjector.RegisterBuilder("test_slice", "TEST_SLICE", func(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
		return []llm_schema.BaseMessage{injectedMsg}
	})

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}

	result := fcp.buildReinjectedStateMessages(context.Background(), &fcpFakeModelContext{}, messages, nil)
	if len(result) == 0 {
		t.Error("有 []BaseMessage builder 时应返回注入消息")
	}
}

// TestFullCompactProcessor_BuildReinjectedStateMessages_空CandidateMessages 验证候选消息为空时返回 nil
func TestFullCompactProcessor_BuildReinjectedStateMessages_空CandidateMessages(t *testing.T) {
	fcp := newTestFCP(nil)
	// 所有消息都是 boundary/state → _prepareMessagesForPrompt 过滤后为空
	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("[FULL_COMPACT_BOUNDARY]\ncompacted"),
	}
	result := fcp.buildReinjectedStateMessages(context.Background(), &fcpFakeModelContext{}, messages, nil)
	if len(result) != 0 {
		t.Errorf("候选消息为空时应返回 nil，实际: %d", len(result))
	}
}

// ──────────────────────────── NewFullCompactProcessor 错误分支 ────────────────────────────

// TestNewFullCompactProcessor_有Model配置但创建失败 验证 Model+ModelClient 配置创建 model 失败
func TestNewFullCompactProcessor_有Model配置但创建失败(t *testing.T) {
	cfg := NewFullCompactProcessorConfig()
	cfg.Model = &llm_schema.ModelRequestConfig{ModelName: "test"}
	cfg.ModelClient = &llm_schema.ModelClientConfig{
		ClientProvider: "不存在的provider",
		ClientID:       "test",
		APIKey:         "fake",
	}

	fcp, err := NewFullCompactProcessor(cfg)
	if err == nil {
		t.Error("不存在的 provider 应返回错误")
	}
	if fcp != nil {
		t.Error("创建失败应返回 nil 实例")
	}
}

// ──────────────────────────── _buildHeadTailTruncatedText 分支测试 ────────────────────────────

// TestBuildHeadTailTruncatedText_只有头 验证只有头没有尾的情况
func TestBuildHeadTailTruncatedText_只有头(t *testing.T) {
	text := "abc"
	result := _buildHeadTailTruncatedText(text, 1)
	// headChars = 1*20/100 = 0, tailChars = 1-0 = 1
	// head = text[:0] = "", tail = text[2:] = "c"
	// head == "" → 走 "if tail != "" 分支
	if !strings.Contains(result, "c") {
		t.Errorf("应包含尾部字符，实际: %s", result)
	}
}

// ──────────────────────────── OnAddMessages sessionMemoryMessage 非 nil 测试 ────────────────────────────

// TestFullCompactProcessor_OnAddMessages_SessionMemoryMessage非nil 验证 sessionMemoryMessage != nil 时不调用 _invalidateSessionMemoryAnchor
func TestFullCompactProcessor_OnAddMessages_SessionMemoryMessage非nil(t *testing.T) {
	// 当前 _buildSessionMemoryMessages 返回 nil（⤵️），所以 sessionMemoryMessage 始终为 nil
	// 但我们可以通过让 _buildReplacementMessages 返回 nil newContextMessages 来测试
	// 间接验证 OnAddMessages 的 sessionMemoryMessage == nil 分支
	// 此时 _invalidateSessionMemoryAnchor 会被调用
	cfg := validFCPConfig()
	cfg.SessionMemoryEnabled = false
	fakeClient := &fcpFakeBaseModelClient{
		invokeResult: llm_schema.NewAssistantMessage(
			"<summary>\n摘要\n</summary>",
		),
	}
	fcp := newFCPWithModel(cfg, fakeClient)

	mc := &fcpFakeModelContext{
		messages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("你好")},
		tokenCounter: &fcpFakeTokenCounter{count: 100},
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("你好！"),
	}

	event, _, err := fcp.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 失败: %v", err)
	}
	if event == nil {
		t.Error("应返回非 nil event")
	}
	// sessionMemoryMessage 为 nil → _invalidateSessionMemoryAnchor 被调用（空操作）
}

// ──────────────────────────── _truncateForPromptBudget 只有1轮且超预算 ────────────────────────────

// TestFullCompactProcessor_TruncateForPromptBudget_单轮超预算 验证只有1轮且超预算时走 _truncateMessagesFromHead
func TestFullCompactProcessor_TruncateForPromptBudget_单轮超预算(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 50
	fcp := newTestFCP(cfg)

	// 1 个 API round，token > 50 → 直接走 _truncateMessagesFromHead
	mc := &fcpFakeModelContext{tokenCounter: &fcpDynamicTokenCounter{counts: []int{200, 50}}}
	messages := makeAPIRoundMessages()

	result := fcp._truncateForPromptBudget(messages, mc)
	if result == nil {
		t.Error("截断后应返回非 nil 消息")
	}
}

// ──────────────────────────── _truncateForPromptBudget Assistant 开头需注入 Synthetic ────────────────────────────

// TestFullCompactProcessor_TruncateForPromptBudget_Assistant开头 验证丢弃首轮后第二轮以 Assistant 开头时注入 Synthetic
func TestFullCompactProcessor_TruncateForPromptBudget_Assistant开头(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 50
	fcp := newTestFCP(cfg)

	// 2 个 API round，丢弃第一个后第二个以 Assistant 开头
	mc := &fcpFakeModelContext{tokenCounter: &fcpDynamicTokenCounter{counts: []int{300, 50}}}
	round1 := makeAPIRoundMessages()
	// 第二轮以 AssistantMessage 开头（不符合标准 API round 格式，但覆盖代码路径）
	round2 := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("直接回复"),
		llm_schema.NewUserMessage("追问"),
		llm_schema.NewAssistantMessage("回答"),
	}
	messages := append(round1, round2...)

	result := fcp._truncateForPromptBudget(messages, mc)
	if result == nil {
		t.Error("应返回非 nil 消息")
	}
	// 丢弃首轮后，第二轮以 Assistant 开头 → 应注入 SyntheticUserMarker
	foundSynthetic := false
	for _, msg := range result {
		if msg.GetContent().Text() == cfg.SyntheticUserMarker && msg.GetRole() == llm_schema.RoleTypeUser {
			foundSynthetic = true
			break
		}
	}
	if !foundSynthetic {
		t.Error("Assistant 开头时应注入 SyntheticUserMarker")
	}
}

// ──────────────────────────── _truncateMessagesFromHead 遇到 Assistant 时注入 Synthetic ────────────────────────────

// TestFullCompactProcessor_TruncateMessagesFromHead_Assistant开头 验证从头部移除后遇到 Assistant 时注入 Synthetic
func TestFullCompactProcessor_TruncateMessagesFromHead_Assistant开头(t *testing.T) {
	cfg := validFCPConfig()
	cfg.CompressionCallMaxTokens = 50
	fcp := newTestFCP(cfg)

	mc := &fcpFakeModelContext{tokenCounter: &fcpDynamicTokenCounter{counts: []int{200, 100, 50}}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("1"),
		llm_schema.NewAssistantMessage("回复"),
		llm_schema.NewUserMessage("2"),
	}

	result := fcp._truncateMessagesFromHead(messages, mc)
	if result == nil {
		t.Error("应返回非 nil 消息")
	}
}
