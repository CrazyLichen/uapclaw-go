package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextMessageBuffer 消息缓冲区，管理历史消息和上下文消息，支持最大容量限制和自动扩缩容。
//
// 对应 Python: openjiuwen/core/context_engine/context/message_buffer.py (ContextMessageBuffer)
type ContextMessageBuffer struct {
	// maxBufferSize 最大缓冲区大小，0 表示无限制
	maxBufferSize int
	// contextMessages 所有消息（历史+上下文）
	contextMessages []llm_schema.BaseMessage
	// historyMessagesSize 历史消息数量标记位置
	historyMessagesSize int
}

// OffloadMessageBuffer 管理被卸载(offload)的消息，支持内存存储和文件系统存储两种方式。
//
// 对应 Python: openjiuwen/core/context_engine/context/message_buffer.py (OffloadMessageBuffer)
type OffloadMessageBuffer struct {
	// inMemoryMessages 内存中的卸载消息字典
	inMemoryMessages map[string][]llm_schema.BaseMessage
	// sysOperation 系统操作接口，用于文件系统 reload
	sysOperation any
	// workspaceDir 工作空间目录路径
	workspaceDir string
	// sessionID 会话 ID
	sessionID string
}

// ──────────────────────────── 常量 ────────────────────────────

// offloadTypeInMemory 内存存储类型标识
const offloadTypeInMemory = "in_memory"

// offloadTypeFilesystem 文件系统存储类型标识
const offloadTypeFilesystem = "filesystem"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextMessageBuffer 创建消息缓冲区实例。
//
// 对应 Python: ContextMessageBuffer.__init__
func NewContextMessageBuffer(historyMessages []llm_schema.BaseMessage, maxBufferSize int) *ContextMessageBuffer {
	buf := &ContextMessageBuffer{
		maxBufferSize: maxBufferSize,
	}
	buf.Rebuild(historyMessages)
	return buf
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Size 返回有效消息数量。
//
// maxBufferSize > 0 时返回 min(len, maxBufferSize)，否则返回实际长度。
// 对应 Python: ContextMessageBuffer.size
func (b *ContextMessageBuffer) Size() int {
	if b.maxBufferSize > 0 {
		size := len(b.contextMessages)
		if size > b.maxBufferSize {
			return b.maxBufferSize
		}
		return size
	}
	return len(b.contextMessages)
}

// AddBack 追加消息到缓冲区尾部，然后检查是否需要自动裁剪。
//
// 对应 Python: ContextMessageBuffer.add_back
func (b *ContextMessageBuffer) AddBack(messages []llm_schema.BaseMessage) {
	b.contextMessages = append(b.contextMessages, messages...)
	b.ifNeedResize()
}

// GetBack 获取缓冲区尾部消息。
//
// size ≤ 0: withHistory=true 返回全部有效消息，withHistory=false 返回历史之后的部分；
// size > 0: 根据withHistory计算实际size，返回尾部N条消息。
//
// 对应 Python: ContextMessageBuffer.get_back
func (b *ContextMessageBuffer) GetBack(size int, withHistory bool) []llm_schema.BaseMessage {
	// 先截取有效窗口
	messages := b.contextMessages
	if b.maxBufferSize > 0 && len(messages) > b.maxBufferSize {
		messages = messages[len(messages)-b.maxBufferSize:]
	}

	if size <= 0 {
		if withHistory {
			return messages
		}
		// 返回历史之后的部分
		contextStart := b.historyMessagesSize
		if contextStart >= len(messages) {
			return nil
		}
		return messages[contextStart:]
	}

	// size > 0
	requestedSize := size
	if !withHistory {
		contextStart := b.historyMessagesSize
		// 计算上下文部分可用的消息数
		contextLen := len(messages) - contextStart
		if contextLen <= 0 {
			return nil
		}
		if requestedSize > contextLen {
			requestedSize = contextLen
		}
	} else {
		if requestedSize > len(messages) {
			requestedSize = len(messages)
		}
	}

	if requestedSize <= 0 {
		return nil
	}
	return messages[len(messages)-requestedSize:]
}

// PopBack 弹出缓冲区尾部消息。
//
// withHistory=true 且弹出数量超过上下文部分时，减少 historyMessagesSize。
// 对应 Python: ContextMessageBuffer.pop_back
func (b *ContextMessageBuffer) PopBack(size int, withHistory bool) []llm_schema.BaseMessage {
	if size <= 0 {
		return nil
	}
	popped := b.GetBack(size, withHistory)
	poppedCount := len(popped)

	if poppedCount == 0 {
		return nil
	}

	if withHistory {
		// 弹出数量超过上下文部分，需要减少 historyMessagesSize
		contextLen := len(b.contextMessages) - b.historyMessagesSize
		if poppedCount > contextLen {
			overflow := poppedCount - contextLen
			b.historyMessagesSize -= overflow
			if b.historyMessagesSize < 0 {
				b.historyMessagesSize = 0
			}
		}
	}

	// 截断 contextMessages
	b.contextMessages = b.contextMessages[:len(b.contextMessages)-poppedCount]
	return popped
}

// SetMessages 替换缓冲区消息。
//
// withHistory=true: 直接替换所有消息，historyMessagesSize 置零；
// withHistory=false: 保留历史前缀，替换上下文部分。
// 对应 Python: ContextMessageBuffer.set_messages
func (b *ContextMessageBuffer) SetMessages(messages []llm_schema.BaseMessage, withHistory bool) {
	if withHistory {
		b.contextMessages = messages
		b.historyMessagesSize = 0
	} else {
		// 保留历史前缀，替换上下文部分
		historyPart := b.contextMessages
		if len(historyPart) > b.historyMessagesSize {
			historyPart = historyPart[:b.historyMessagesSize]
		}
		b.contextMessages = make([]llm_schema.BaseMessage, 0, len(historyPart)+len(messages))
		b.contextMessages = append(b.contextMessages, historyPart...)
		b.contextMessages = append(b.contextMessages, messages...)
	}
	b.ifNeedResize()
}

// Rebuild 从历史消息重建缓冲区。
//
// maxBufferSize > 0 时截取尾部 maxBufferSize 条消息作为初始内容，并设置 historyMessagesSize；
// 否则复制全部历史消息。
// 对应 Python: ContextMessageBuffer.rebulid (Python 拼写为 rebulid)
func (b *ContextMessageBuffer) Rebuild(historyMessages []llm_schema.BaseMessage) {
	if b.maxBufferSize > 0 {
		if len(historyMessages) > b.maxBufferSize {
			// 截取尾部 maxBufferSize 条
			b.contextMessages = make([]llm_schema.BaseMessage, b.maxBufferSize)
			copy(b.contextMessages, historyMessages[len(historyMessages)-b.maxBufferSize:])
			b.historyMessagesSize = b.maxBufferSize
		} else {
			b.contextMessages = make([]llm_schema.BaseMessage, len(historyMessages))
			copy(b.contextMessages, historyMessages)
			b.historyMessagesSize = len(historyMessages)
		}
	} else {
		b.contextMessages = make([]llm_schema.BaseMessage, len(historyMessages))
		copy(b.contextMessages, historyMessages)
		b.historyMessagesSize = len(historyMessages)
	}
}

// NewOffloadMessageBuffer 创建卸载消息缓冲区实例。
//
// 对应 Python: OffloadMessageBuffer.__init__
func NewOffloadMessageBuffer(initMessages map[string][]llm_schema.BaseMessage) *OffloadMessageBuffer {
	if initMessages == nil {
		initMessages = make(map[string][]llm_schema.BaseMessage)
	}
	return &OffloadMessageBuffer{
		inMemoryMessages: initMessages,
	}
}

// SetSysOperation 设置系统操作接口。
//
// 对应 Python: OffloadMessageBuffer.set_sys_operation
func (b *OffloadMessageBuffer) SetSysOperation(op any) {
	b.sysOperation = op
}

// SetWorkspaceInfo 设置工作空间信息。
//
// 对应 Python: OffloadMessageBuffer.set_workspace_info
func (b *OffloadMessageBuffer) SetWorkspaceInfo(workspaceDir, sessionID string) {
	b.workspaceDir = workspaceDir
	b.sessionID = sessionID
}

// Offload 卸载消息到指定存储。
//
// in_memory 类型存入内存 map；其他类型暂不处理。
// 对应 Python: OffloadMessageBuffer.offload
func (b *OffloadMessageBuffer) Offload(offloadHandle string, offloadType string, messages []llm_schema.BaseMessage) {
	if offloadType == offloadTypeInMemory {
		b.inMemoryMessages[offloadHandle] = messages
		logger.Info(logComponent).
			Str("offload_handle", offloadHandle).
			Str("offload_type", offloadType).
			Int("message_count", len(messages)).
			Msg("消息已卸载到内存")
	} else {
		logger.Info(logComponent).
			Str("offload_handle", offloadHandle).
			Str("offload_type", offloadType).
			Int("message_count", len(messages)).
			Msg("非内存卸载类型暂不处理")
	}
}

// Reload 从指定存储重新加载消息。
//
// in_memory 从内存 map 取出；filesystem 从文件系统读取。
// 对应 Python: OffloadMessageBuffer.reload
func (b *OffloadMessageBuffer) Reload(offloadHandle string, offloadType string) []llm_schema.BaseMessage {
	if offloadType == offloadTypeInMemory {
		messages, ok := b.inMemoryMessages[offloadHandle]
		if !ok {
			logger.Warn(logComponent).
				Str("offload_handle", offloadHandle).
				Msg("内存中未找到卸载消息")
			return nil
		}
		logger.Info(logComponent).
			Str("offload_handle", offloadHandle).
			Int("message_count", len(messages)).
			Msg("从内存重新加载消息")
		return messages
	}

	if offloadType == offloadTypeFilesystem {
		return b.reloadFromFilesystem(offloadHandle)
	}

	logger.Warn(logComponent).
		Str("offload_handle", offloadHandle).
		Str("offload_type", offloadType).
		Msg("不支持的卸载类型")
	return nil
}

// Clear 清除指定卸载消息。
//
// in_memory 类型从 map 中删除对应条目。
// 对应 Python: OffloadMessageBuffer.clear
func (b *OffloadMessageBuffer) Clear(offloadHandle string, offloadType string) {
	if offloadType == offloadTypeInMemory {
		delete(b.inMemoryMessages, offloadHandle)
		logger.Info(logComponent).
			Str("offload_handle", offloadHandle).
			Str("offload_type", offloadType).
			Msg("已清除卸载消息")
	}
}

// GetAll 返回全部内存卸载消息。
//
// 对应 Python: OffloadMessageBuffer.get_all
func (b *OffloadMessageBuffer) GetAll() map[string][]llm_schema.BaseMessage {
	return b.inMemoryMessages
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ifNeedResize 当缓冲区大小超过 2 倍 maxBufferSize 时，裁剪前 maxBufferSize 条消息。
//
// 对应 Python: ContextMessageBuffer._if_need_resize
func (b *ContextMessageBuffer) ifNeedResize() {
	if b.maxBufferSize <= 0 {
		return
	}
	if len(b.contextMessages) > 2*b.maxBufferSize {
		// 裁剪前 maxBufferSize 条
		dropCount := b.maxBufferSize
		b.contextMessages = b.contextMessages[dropCount:]
		// 历史消息标记位置相应调整
		b.historyMessagesSize -= dropCount
		if b.historyMessagesSize < 0 {
			b.historyMessagesSize = 0
		}
		logger.Info(logComponent).
			Int("max_buffer_size", b.maxBufferSize).
			Int("dropped_count", dropCount).
			Int("remaining_count", len(b.contextMessages)).
			Msg("缓冲区自动裁剪")
	}
}

// reloadFromFilesystem 从文件系统读取 JSON 文件并反序列化为消息列表。
//
// 当前使用 os.ReadFile 直接读取 JSON 文件。如果文件不存在或解析失败，返回空切片。
// 对应 Python: OffloadMessageBuffer._reload_from_filesystem
func (b *OffloadMessageBuffer) reloadFromFilesystem(offloadHandle string) []llm_schema.BaseMessage {
	candidatePaths := b.filesystemReloadPaths(offloadHandle)

	for _, path := range candidatePaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var rawMessages []json.RawMessage
		if err := json.Unmarshal(data, &rawMessages); err != nil {
			logger.Warn(logComponent).
				Str("path", path).
				Err(err).
				Msg("卸载消息文件 JSON 解析失败")
			continue
		}

		messages := make([]llm_schema.BaseMessage, 0, len(rawMessages))
		for i, raw := range rawMessages {
			msg, err := llm_schema.UnmarshalMessage(raw)
			if err != nil {
				logger.Warn(logComponent).
					Str("path", path).
					Int("index", i).
					Err(err).
					Msg("卸载消息反序列化失败")
				continue
			}
			messages = append(messages, msg)
		}

		if len(messages) > 0 {
			logger.Info(logComponent).
				Str("path", path).
				Int("message_count", len(messages)).
				Msg("从文件系统重新加载消息")
			return messages
		}
	}

	logger.Warn(logComponent).
		Str("offload_handle", offloadHandle).
		Strs("candidate_paths", candidatePaths).
		Msg("未找到可用的卸载消息文件")
	return nil
}

// filesystemReloadPaths 构建候选文件路径列表。
//
// 无 workspaceDir 时返回 [offloadHandle]；
// 有 workspaceDir 时构建精确路径 + glob 匹配路径。
// 对应 Python: OffloadMessageBuffer._filesystem_reload_paths
func (b *OffloadMessageBuffer) filesystemReloadPaths(offloadHandle string) []string {
	if b.workspaceDir == "" {
		return []string{offloadHandle}
	}

	// 精确路径：workspaceDir/sessionID/offloadHandle.json
	precisePath := filepath.Join(b.workspaceDir, b.sessionID, offloadHandle+".json")

	// glob 匹配路径：workspaceDir/sessionID/offloadHandle*.json
	globPattern := filepath.Join(b.workspaceDir, b.sessionID, offloadHandle+"*.json")
	matches, err := filepath.Glob(globPattern)
	if err != nil {
		logger.Warn(logComponent).
			Str("pattern", globPattern).
			Err(err).
			Msg("glob 匹配失败")
		return []string{precisePath}
	}

	// 去重并排序，精确路径优先
	pathSet := make(map[string]struct{})
	pathSet[precisePath] = struct{}{}
	for _, m := range matches {
		pathSet[m] = struct{}{}
	}

	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// 确保精确路径排在最前
	result := make([]string, 0, len(paths))
	result = append(result, precisePath)
	for _, p := range paths {
		if p != precisePath {
			result = append(result, p)
		}
	}

	return result
}
