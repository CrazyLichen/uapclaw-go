package context

import (
	"context"
	"fmt"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KVCacheManager 管理 KV 缓存的释放逻辑，通过比较前后两次 ContextWindow 的差异来决定是否释放缓存。
//
// 对应 Python: openjiuwen/core/context_engine/context/kv_cache_manager.py (KVCacheManager)
type KVCacheManager struct {
	// sessionID 会话标识
	sessionID string
	// lastContextWindow 上一次的上下文窗口快照
	lastContextWindow *iface.ContextWindow
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKVCacheManager 创建 KVCacheManager 实例。
//
// 对应 Python: KVCacheManager.__init__(session_id)
func NewKVCacheManager(sessionID string) *KVCacheManager {
	return &KVCacheManager{
		sessionID:         sessionID,
		lastContextWindow: nil,
	}
}

// Release 检查前后两次 ContextWindow 差异并释放 KV 缓存。
//
// 流程：
//  1. 从 Option 中提取 model，为 nil 或不支持 KV Cache Release → 直接返回
//  2. 首次调用（lastContextWindow 为 nil）→ 保存快照，不释放
//  3. 前缀对比检测差异 → 无差异则更新快照返回
//  4. 有差异 → 构建 ReleaseOption 调用 model.Release
//
// 对应 Python: KVCacheManager.release()
func (m *KVCacheManager) Release(ctx context.Context, contextWindow *iface.ContextWindow, opts ...iface.Option) error {
	// 从 Option 中提取 model
	po := iface.NewProcessorOption(opts...)
	model := po.Model

	// model 为 nil，无需释放
	if model == nil {
		return nil
	}

	// 不支持 KV Cache Release，无需释放
	if !model.SupportsKVCacheRelease() {
		return nil
	}

	// 首次调用，保存快照，不释放
	if m.lastContextWindow == nil {
		m.lastContextWindow = contextWindow
		return nil
	}

	// 前缀对比检测差异
	shouldRelease, msgIdx, toolIdx := m.checkReleaseNeeded(contextWindow)

	// 无差异，更新快照返回
	if !shouldRelease {
		m.lastContextWindow = contextWindow
		return nil
	}

	// 构建 ReleaseOption
	releaseOpts := []model_clients.ReleaseOption{
		model_clients.WithReleaseSessionID(m.sessionID),
		model_clients.WithReleaseMessages(model_clients.NewMessagesParam(contextWindow.GetMessages()...)),
	}

	// 消息释放索引
	if msgIdx >= 0 {
		releaseOpts = append(releaseOpts, model_clients.WithReleaseMessagesIndex(msgIdx))
	}

	// 工具释放
	if toolIdx >= 0 {
		tools := contextWindow.GetTools()
		releaseOpts = append(releaseOpts, model_clients.WithReleaseTools(tools...))
		releaseOpts = append(releaseOpts, model_clients.WithReleaseToolsIndex(toolIdx))
	}

	// 调用模型释放
	_, err := model.Release(ctx, releaseOpts...)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "KV_CACHE_RELEASE_ERROR").
			Str("session_id", m.sessionID).
			Err(err).
			Msg("KV 缓存释放失败")
		return fmt.Errorf("KV 缓存释放失败: %w", err)
	}

	// 更新快照
	m.lastContextWindow = contextWindow

	// 记录 Info 日志
	evt := logger.Info(logComponent).
		Str("event_type", "KV_CACHE_RELEASED").
		Str("session_id", m.sessionID)
	if msgIdx >= 0 {
		evt = evt.Int("messages_released_index", msgIdx)
	}
	if toolIdx >= 0 {
		evt = evt.Int("tools_released_index", toolIdx)
	}
	evt.Msg("KV 缓存已释放")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// checkReleaseNeeded 前缀对比，检测前后两次 ContextWindow 的消息和工具差异。
//
// 消息比较：逐位对比 lastContextWindow.GetMessages() 和 contextWindow.GetMessages()，
// 使用 GetRole() + GetContent().Text() 作为判断是否一致的依据。
// 工具比较：同理对比 GetTools()，使用 ToolInfo.Name 作为判断依据。
//
// 返回 (shouldRelease, msgIdx, toolIdx)：
//   - shouldRelease: 是否需要释放（找到差异时为 true）
//   - msgIdx: 消息首个差异位置（-1 表示消息无差异）
//   - toolIdx: 工具首个差异位置（-1 表示工具无差异）
//
// 对应 Python: KVCacheManager._check_release_needed()
func (m *KVCacheManager) checkReleaseNeeded(contextWindow *iface.ContextWindow) (bool, int, int) {
	msgIdx := -1
	toolIdx := -1

	// 消息比较
	lastMessages := m.lastContextWindow.GetMessages()
	currentMessages := contextWindow.GetMessages()

	minLen := len(lastMessages)
	if len(currentMessages) < minLen {
		minLen = len(currentMessages)
	}

	for i := 0; i < minLen; i++ {
		if !messagesEqual(lastMessages[i], currentMessages[i]) {
			msgIdx = i
			break
		}
	}

	// 长度不同但前缀一致时，短列表末尾即为差异位置
	if msgIdx < 0 && len(lastMessages) != len(currentMessages) {
		msgIdx = minLen
	}

	// 工具比较
	lastTools := m.lastContextWindow.GetTools()
	currentTools := contextWindow.GetTools()

	minToolLen := len(lastTools)
	if len(currentTools) < minToolLen {
		minToolLen = len(currentTools)
	}

	for i := 0; i < minToolLen; i++ {
		if lastTools[i].GetName() != currentTools[i].GetName() {
			toolIdx = i
			break
		}
	}

	// 长度不同但前缀一致时，短列表末尾即为差异位置
	if toolIdx < 0 && len(lastTools) != len(currentTools) {
		toolIdx = minToolLen
	}

	shouldRelease := msgIdx >= 0 || toolIdx >= 0
	return shouldRelease, msgIdx, toolIdx
}

// messagesEqual 比较两条消息是否一致，使用 Role + Content.Text 作为判断依据。
func messagesEqual(a, b llm_schema.BaseMessage) bool {
	return a.GetRole() == b.GetRole() && a.GetContent().Text() == b.GetContent().Text()
}
