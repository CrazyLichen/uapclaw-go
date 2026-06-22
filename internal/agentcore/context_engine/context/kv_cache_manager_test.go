package context

import (
	"context"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newContextWindowWithMessages 创建包含指定消息和工具的 ContextWindow。
func newContextWindowWithMessages(
	systemMsgs []llm_schema.BaseMessage,
	contextMsgs []llm_schema.BaseMessage,
	tools []*commonschema.ToolInfo,
) *iface.ContextWindow {
	if systemMsgs == nil {
		systemMsgs = make([]llm_schema.BaseMessage, 0)
	}
	if contextMsgs == nil {
		contextMsgs = make([]llm_schema.BaseMessage, 0)
	}
	if tools == nil {
		tools = make([]*commonschema.ToolInfo, 0)
	}
	return &iface.ContextWindow{
		SystemMessages:  systemMsgs,
		ContextMessages: contextMsgs,
		Tools:           tools,
		Statistic:       iface.ContextStats{},
	}
}

// newUserMessage 创建用户消息。
func newUserMessage(text string) *llm_schema.UserMessage {
	return llm_schema.NewUserMessage(text)
}

// newAssistantMessage 创建助手消息。
func newAssistantMessage(text string) *llm_schema.AssistantMessage {
	return llm_schema.NewAssistantMessage(text)
}

// newSystemMessage 创建系统消息。
func newSystemMessage(text string) *llm_schema.SystemMessage {
	return llm_schema.NewSystemMessage(text)
}

// ──────────────────────────── 测试 ────────────────────────────

// TestNewKVCacheManager 测试创建 KVCacheManager 实例
func TestNewKVCacheManager(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")
	if mgr == nil {
		t.Fatal("NewKVCacheManager 返回 nil")
	}
	if mgr.sessionID != "session-1" {
		t.Errorf("sessionID = %q, want %q", mgr.sessionID, "session-1")
	}
	if mgr.lastContextWindow != nil {
		t.Error("lastContextWindow 应为 nil")
	}
}

// TestRelease_model为nil 测试 model 为 nil 时直接返回
func TestRelease_model为nil(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")
	ctx := context.Background()
	cw := newContextWindowWithMessages(nil, nil, nil)

	err := mgr.Release(ctx, cw, nil)
	if err != nil {
		t.Errorf("model 为 nil 时应返回 nil 错误，实际: %v", err)
	}
}

// TestRelease_首次调用model为nil时不更新快照 测试 model 为 nil 时首次调用不保存快照
func TestRelease_首次调用model为nil时不更新快照(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")
	ctx := context.Background()

	msgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	cw := newContextWindowWithMessages(msgs, nil, nil)

	_ = mgr.Release(ctx, cw, nil)

	// model 为 nil 时 Release 直接返回，不保存快照
	if mgr.lastContextWindow != nil {
		t.Error("model 为 nil 时 lastContextWindow 不应更新")
	}
}

// TestRelease_model为nil时不更新快照 测试 model 为 nil 时 Release 不会更新 lastContextWindow
func TestRelease_model为nil时不更新快照(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")
	ctx := context.Background()

	msgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	cw1 := newContextWindowWithMessages(msgs, nil, nil)
	cw2 := newContextWindowWithMessages(msgs, nil, nil)

	// 首次调用 model 为 nil
	_ = mgr.Release(ctx, cw1, nil)

	// model 为 nil 时不会更新 lastContextWindow
	if mgr.lastContextWindow != nil {
		t.Error("model 为 nil 时 lastContextWindow 不应被更新")
	}

	// 再次调用仍为 nil
	_ = mgr.Release(ctx, cw2, nil)
	if mgr.lastContextWindow != nil {
		t.Error("model 为 nil 时 lastContextWindow 始终不应被更新")
	}
}

// TestRelease_相同上下文窗口不释放 测试前后 ContextWindow 相同时不释放
func TestRelease_相同上下文窗口不释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	msgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	tools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc1", nil)}

	cw1 := newContextWindowWithMessages(msgs, nil, tools)
	cw2 := newContextWindowWithMessages(msgs, nil, tools)

	// 直接设置 lastContextWindow 模拟首次调用后的状态
	mgr.lastContextWindow = cw1

	// 第二次调用：相同的上下文窗口，checkReleaseNeeded 应返回 false
	shouldRelease, _, _ := mgr.checkReleaseNeeded(cw2)
	if shouldRelease {
		t.Error("相同上下文窗口不应需要释放")
	}
}

// TestCheckReleaseNeeded_消息不同时需要释放 测试消息内容不同时检测到需要释放
func TestCheckReleaseNeeded_消息不同时需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	// 设置 lastContextWindow
	lastMsgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	lastCW := newContextWindowWithMessages(lastMsgs, nil, nil)
	mgr.lastContextWindow = lastCW

	// 当前 ContextWindow 的消息不同
	curMsgs := []llm_schema.BaseMessage{newUserMessage("world")}
	curCW := newContextWindowWithMessages(curMsgs, nil, nil)

	shouldRelease, msgIdx, toolIdx := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("消息不同时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 0 {
		t.Errorf("msgIdx 应为 0，实际: %v", msgIdx)
	}
	if toolIdx != nil {
		t.Errorf("toolIdx 应为 nil，实际: %v", toolIdx)
	}
}

// TestCheckReleaseNeeded_消息角色不同时需要释放 测试消息角色不同时检测到需要释放
func TestCheckReleaseNeeded_消息角色不同时需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastMsgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	lastCW := newContextWindowWithMessages(lastMsgs, nil, nil)
	mgr.lastContextWindow = lastCW

	// 角色不同（user vs assistant），内容相同
	curMsgs := []llm_schema.BaseMessage{newAssistantMessage("hello")}
	curCW := newContextWindowWithMessages(curMsgs, nil, nil)

	shouldRelease, msgIdx, _ := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("消息角色不同时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 0 {
		t.Errorf("msgIdx 应为 0，实际: %v", msgIdx)
	}
}

// TestCheckReleaseNeeded_工具不同时需要释放 测试工具名称不同时检测到需要释放
func TestCheckReleaseNeeded_工具不同时需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc1", nil)}
	lastCW := newContextWindowWithMessages(nil, nil, lastTools)
	mgr.lastContextWindow = lastCW

	curTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool2", "desc1", nil)}
	curCW := newContextWindowWithMessages(nil, nil, curTools)

	shouldRelease, msgIdx, toolIdx := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("工具不同时应需要释放")
	}
	if msgIdx != nil {
		t.Errorf("msgIdx 应为 nil，实际: %v", msgIdx)
	}
	if toolIdx == nil || *toolIdx != 0 {
		t.Errorf("toolIdx 应为 0，实际: %v", toolIdx)
	}
}

// TestCheckReleaseNeeded_完全相同不需要释放 测试消息和工具完全相同时不需要释放
func TestCheckReleaseNeeded_完全相同不需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	msgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	tools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc1", nil)}

	lastCW := newContextWindowWithMessages(msgs, nil, tools)
	mgr.lastContextWindow = lastCW

	curCW := newContextWindowWithMessages(msgs, nil, tools)

	shouldRelease, msgIdx, toolIdx := mgr.checkReleaseNeeded(curCW)
	if shouldRelease {
		t.Error("完全相同时不应需要释放")
	}
	if msgIdx != nil {
		t.Errorf("msgIdx 应为 nil，实际: %v", msgIdx)
	}
	if toolIdx != nil {
		t.Errorf("toolIdx 应为 nil，实际: %v", toolIdx)
	}
}

// TestCheckReleaseNeeded_消息长度不同时需要释放 测试消息长度不同时检测到需要释放
func TestCheckReleaseNeeded_消息长度不同时需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastMsgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	lastCW := newContextWindowWithMessages(lastMsgs, nil, nil)
	mgr.lastContextWindow = lastCW

	// 消息更多（前缀一致）
	curMsgs := []llm_schema.BaseMessage{newUserMessage("hello"), newAssistantMessage("hi")}
	curCW := newContextWindowWithMessages(curMsgs, nil, nil)

	shouldRelease, msgIdx, _ := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("消息长度不同时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 1 {
		t.Errorf("msgIdx 应为 1（短列表末尾），实际: %v", msgIdx)
	}
}

// TestCheckReleaseNeeded_消息长度缩短时需要释放 测试消息变少时检测到需要释放
func TestCheckReleaseNeeded_消息长度缩短时需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastMsgs := []llm_schema.BaseMessage{newUserMessage("hello"), newAssistantMessage("hi")}
	lastCW := newContextWindowWithMessages(lastMsgs, nil, nil)
	mgr.lastContextWindow = lastCW

	// 消息更少（前缀一致）
	curMsgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	curCW := newContextWindowWithMessages(curMsgs, nil, nil)

	shouldRelease, msgIdx, _ := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("消息变少时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 1 {
		t.Errorf("msgIdx 应为 1（短列表末尾），实际: %v", msgIdx)
	}
}

// TestCheckReleaseNeeded_工具长度不同时需要释放 测试工具数量变化时检测到需要释放
func TestCheckReleaseNeeded_工具长度不同时需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc1", nil)}
	lastCW := newContextWindowWithMessages(nil, nil, lastTools)
	mgr.lastContextWindow = lastCW

	// 工具更多（前缀一致）
	curTools := []*commonschema.ToolInfo{
		commonschema.NewToolInfo("tool1", "desc1", nil),
		commonschema.NewToolInfo("tool2", "desc2", nil),
	}
	curCW := newContextWindowWithMessages(nil, nil, curTools)

	shouldRelease, _, toolIdx := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("工具数量变化时应需要释放")
	}
	if toolIdx == nil || *toolIdx != 1 {
		t.Errorf("toolIdx 应为 1（短列表末尾），实际: %v", toolIdx)
	}
}

// TestCheckReleaseNeeded_中间消息不同 测试前缀中某条消息发生变化
func TestCheckReleaseNeeded_中间消息不同(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastMsgs := []llm_schema.BaseMessage{
		newUserMessage("hello"),
		newAssistantMessage("hi"),
		newUserMessage("how are you"),
	}
	lastCW := newContextWindowWithMessages(lastMsgs, nil, nil)
	mgr.lastContextWindow = lastCW

	// 第二条消息内容不同
	curMsgs := []llm_schema.BaseMessage{
		newUserMessage("hello"),
		newAssistantMessage("hey"),
		newUserMessage("how are you"),
	}
	curCW := newContextWindowWithMessages(curMsgs, nil, nil)

	shouldRelease, msgIdx, _ := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("中间消息不同时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 1 {
		t.Errorf("msgIdx 应为 1，实际: %v", msgIdx)
	}
}

// TestCheckReleaseNeeded_系统消息和上下文消息合并比较 测试 GetMessages 合并 SystemMessages + ContextMessages
func TestCheckReleaseNeeded_系统消息和上下文消息合并比较(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	// lastContextWindow：1 条系统消息 + 1 条上下文消息
	lastCW := newContextWindowWithMessages(
		[]llm_schema.BaseMessage{newSystemMessage("system")},
		[]llm_schema.BaseMessage{newUserMessage("hello")},
		nil,
	)
	mgr.lastContextWindow = lastCW

	// 当前 ContextWindow：系统消息变了
	curCW := newContextWindowWithMessages(
		[]llm_schema.BaseMessage{newSystemMessage("system-v2")},
		[]llm_schema.BaseMessage{newUserMessage("hello")},
		nil,
	)

	shouldRelease, msgIdx, _ := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("系统消息变化时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 0 {
		t.Errorf("msgIdx 应为 0（系统消息位置），实际: %v", msgIdx)
	}
}

// TestCheckReleaseNeeded_工具描述不同但名称相同不需要释放 测试仅工具描述变化（名称相同）时不需要释放
func TestCheckReleaseNeeded_工具描述不同但名称相同不需要释放(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc1", nil)}
	lastCW := newContextWindowWithMessages(nil, nil, lastTools)
	mgr.lastContextWindow = lastCW

	// 描述不同但名称相同
	curTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc2", nil)}
	curCW := newContextWindowWithMessages(nil, nil, curTools)

	shouldRelease, _, toolIdx := mgr.checkReleaseNeeded(curCW)
	if shouldRelease {
		t.Error("仅工具描述变化（名称相同）时不应需要释放")
	}
	if toolIdx != nil {
		t.Errorf("toolIdx 应为 nil，实际: %v", toolIdx)
	}
}

// TestCheckReleaseNeeded_空消息和空工具 测试两边都为空时不需要释放
func TestCheckReleaseNeeded_空消息和空工具(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastCW := newContextWindowWithMessages(nil, nil, nil)
	mgr.lastContextWindow = lastCW

	curCW := newContextWindowWithMessages(nil, nil, nil)

	shouldRelease, msgIdx, toolIdx := mgr.checkReleaseNeeded(curCW)
	if shouldRelease {
		t.Error("空消息和空工具不需要释放")
	}
	if msgIdx != nil {
		t.Errorf("msgIdx 应为 nil，实际: %v", msgIdx)
	}
	if toolIdx != nil {
		t.Errorf("toolIdx 应为 nil，实际: %v", toolIdx)
	}
}

// TestCheckReleaseNeeded_消息不同且工具也不同 测试消息和工具同时变化时两者索引均返回
func TestCheckReleaseNeeded_消息不同且工具也不同(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")

	lastMsgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	lastTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool1", "desc1", nil)}
	lastCW := newContextWindowWithMessages(lastMsgs, nil, lastTools)
	mgr.lastContextWindow = lastCW

	curMsgs := []llm_schema.BaseMessage{newUserMessage("world")}
	curTools := []*commonschema.ToolInfo{commonschema.NewToolInfo("tool2", "desc1", nil)}
	curCW := newContextWindowWithMessages(curMsgs, nil, curTools)

	shouldRelease, msgIdx, toolIdx := mgr.checkReleaseNeeded(curCW)
	if !shouldRelease {
		t.Error("消息和工具都不同时应需要释放")
	}
	if msgIdx == nil || *msgIdx != 0 {
		t.Errorf("msgIdx 应为 0，实际: %v", msgIdx)
	}
	if toolIdx == nil || *toolIdx != 0 {
		t.Errorf("toolIdx 应为 0，实际: %v", toolIdx)
	}
}

// TestMessagesEqual 测试消息比较逻辑
func TestMessagesEqual(t *testing.T) {
	t.Helper()

	tests := []struct {
		name  string
		a     llm_schema.BaseMessage
		b     llm_schema.BaseMessage
		equal bool
	}{
		{
			name:  "相同角色和内容",
			a:     newUserMessage("hello"),
			b:     newUserMessage("hello"),
			equal: true,
		},
		{
			name:  "不同角色",
			a:     newUserMessage("hello"),
			b:     newAssistantMessage("hello"),
			equal: false,
		},
		{
			name:  "不同内容",
			a:     newUserMessage("hello"),
			b:     newUserMessage("world"),
			equal: false,
		},
		{
			name:  "相同系统消息",
			a:     newSystemMessage("prompt"),
			b:     newSystemMessage("prompt"),
			equal: true,
		},
		{
			name:  "系统消息内容不同",
			a:     newSystemMessage("prompt"),
			b:     newSystemMessage("other"),
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := messagesEqual(tt.a, tt.b)
			if got != tt.equal {
				t.Errorf("messagesEqual() = %v, want %v", got, tt.equal)
			}
		})
	}
}

// ──────────────────────────── Release 流程测试 ────────────────────────────

// TestRelease_model为nil不更新快照_多次调用 测试多次调用 Release(nil) 不更新快照
func TestRelease_model为nil不更新快照_多次调用(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")
	ctx := context.Background()

	msgs1 := []llm_schema.BaseMessage{newUserMessage("hello")}
	msgs2 := []llm_schema.BaseMessage{newUserMessage("world")}

	cw1 := newContextWindowWithMessages(msgs1, nil, nil)
	cw2 := newContextWindowWithMessages(msgs2, nil, nil)

	// model 为 nil 时，无论 ContextWindow 如何变化，都不处理
	_ = mgr.Release(ctx, cw1, nil)
	_ = mgr.Release(ctx, cw2, nil)

	// lastContextWindow 始终为 nil（因为 nil model 直接返回）
	if mgr.lastContextWindow != nil {
		t.Error("model 为 nil 时 lastContextWindow 不应被更新")
	}
}

// TestRelease_手动设置快照后model为nil 测试已有快照但 model 为 nil 时直接返回
func TestRelease_手动设置快照后model为nil(t *testing.T) {
	t.Helper()
	mgr := NewKVCacheManager("session-1")
	ctx := context.Background()

	msgs := []llm_schema.BaseMessage{newUserMessage("hello")}
	cw := newContextWindowWithMessages(msgs, nil, nil)

	// 手动设置快照（模拟首次调用后的状态）
	mgr.lastContextWindow = cw

	// model 为 nil 应直接返回，不检查差异
	msgs2 := []llm_schema.BaseMessage{newUserMessage("world")}
	cw2 := newContextWindowWithMessages(msgs2, nil, nil)
	err := mgr.Release(ctx, cw2, nil)
	if err != nil {
		t.Errorf("model 为 nil 时应返回 nil，实际: %v", err)
	}
}
