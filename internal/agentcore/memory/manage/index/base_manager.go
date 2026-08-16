package index

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMemoryManager 记忆管理器抽象接口。
//
// 定义了记忆管理器的 6 个核心操作：AddMemories、Update、Search、Get、Delete、DeleteByUserID。
// 所有记忆管理器实现（FragmentMemoryManager、SummaryManager、VariableManager）必须实现此接口。
//
// 对应 Python: openjiuwen/core/memory/manage/index/base_memory_manager.py (BaseMemoryManager)
type BaseMemoryManager interface {
	// AddMemories 批量添加记忆（含冲突检查和冗余消除）。
	// memories 的 key 为 mem_type 字符串（如 "user_profile"），value 为该类型的记忆列表。
	AddMemories(ctx context.Context, userID string, scopeID string,
		memories map[string][]*mem_model.FragmentMemoryUnit) ([]*mem_model.FragmentMemoryUnit, error)
	// Update 按 ID 更新记忆内容
	Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error)
	// Search 语义搜索记忆
	Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error)
	// Get 按 ID 获取单条记忆
	Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error)
	// Delete 按 ID 删除记忆
	Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error)
	// DeleteByUserID 删除用户+scope 下所有记忆
	DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error)
}

// memoryManagerBase 记忆管理器公共基类。
//
// 嵌入此结构体后，实现类只需实现 BaseMemoryManager 接口即可。
// 提供 validateParams / wrapException / encryptMemoryIfNeeded / decryptMemoryIfNeeded 公共逻辑。
//
// 对应 Python: openjiuwen/core/memory/manage/index/base_memory_manager.py (BaseMemoryManager 非抽象方法)
type memoryManagerBase struct {
	// memoryIndex 记忆索引（KV + 向量库）
	memoryIndex index.BaseMemoryIndex
	// cryptoKey 加密密钥
	cryptoKey []byte
	// memType 类型标识（如 "fragment"）
	memType string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// FragmentMemoryTypeUserProfile 用户画像类型
	FragmentMemoryTypeUserProfile = "user_profile"
	// FragmentMemoryTypeSemanticMemory 语义记忆类型
	FragmentMemoryTypeSemanticMemory = "semantic_memory"
	// FragmentMemoryTypeEpisodicMemory 情景记忆类型
	FragmentMemoryTypeEpisodicMemory = "episodic_memory"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// FragmentMemoryTypes 碎片记忆类型列表
	FragmentMemoryTypes = []string{
		FragmentMemoryTypeUserProfile,
		FragmentMemoryTypeSemanticMemory,
		FragmentMemoryTypeEpisodicMemory,
	}

	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateParams 校验必填参数，缺少时返回 *BaseError。
// 对齐 Python: BaseMemoryManager._validate_required_params
func (b *memoryManagerBase) validateParams(userID, scopeID string, statusCode exception.StatusCode, memType string) error {
	if userID == "" {
		return exception.BuildError(statusCode,
			exception.WithParam("memory_type", memType),
			exception.WithMsg("user_id is required"),
		)
	}
	if scopeID == "" {
		return exception.BuildError(statusCode,
			exception.WithParam("memory_type", memType),
			exception.WithMsg("scope_id is required"),
		)
	}
	if b.memoryIndex == nil {
		return exception.BuildError(statusCode,
			exception.WithParam("memory_type", memType),
			exception.WithMsg("memory_index is not initialized"),
		)
	}
	return nil
}

// wrapException 包装异常为统一 *BaseError。
// 如果原始错误已经是 *BaseError，原样返回；否则包装为新的 *BaseError。
// 对齐 Python: BaseMemoryManager._wrap_exception
func (b *memoryManagerBase) wrapException(e error, statusCode exception.StatusCode, memType string) error {
	var baseErr *exception.BaseError
	if errors.As(e, &baseErr) {
		return baseErr
	}
	return exception.BuildError(statusCode,
		exception.WithParam("memory_type", memType),
		exception.WithMsg(e.Error()),
		exception.WithCause(e),
	)
}

// encryptMemoryIfNeeded 如果 key 非空且 plaintext 非空，使用 AES 加密；否则返回原文。
// 加密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为）。
// 对齐 Python: BaseMemoryManager.encrypt_memory_if_needed
func encryptMemoryIfNeeded(key []byte, plaintext string) string {
	if len(key) == 0 || plaintext == "" {
		return plaintext
	}
	c, err := codec.NewAesStorageCodec(key)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "MEMORY_PROCESS").
			Msg("创建编解码器失败，返回原文")
		return plaintext
	}
	return c.Encode(plaintext)
}

// decryptMemoryIfNeeded 如果 key 非空且 ciphertext 非空，使用 AES 解密；否则返回原文。
// 解密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为）。
// 对齐 Python: BaseMemoryManager.decrypt_memory_if_needed
func decryptMemoryIfNeeded(key []byte, ciphertext string) string {
	if len(key) == 0 || ciphertext == "" {
		return ciphertext
	}
	c, err := codec.NewAesStorageCodec(key)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "MEMORY_PROCESS").
			Msg("创建编解码器失败，返回原文")
		return ciphertext
	}
	return c.Decode(ciphertext)
}
