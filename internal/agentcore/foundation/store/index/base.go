package index

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryDoc 记忆文档，表示一条存储的记忆条目。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (MemoryDoc)
type MemoryDoc struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Text 文本内容
	Text string `json:"text"`
	// Type 类型/分类
	Type string `json:"type"`
	// Timestamp 时间戳（零值 time.Time{} 表示未设置，由实现层填充当前时间；
	// Python 创建时自动填充当前时间，Go 的 time.Time 零值为 0001-01-01）
	Timestamp time.Time `json:"timestamp"`
	// Fields 扩展字段（移除 omitempty：Python 默认 {} 空字典，Go 零值为 nil，
	// omitempty 时 nil 不输出字段而 {} 输出 "fields":{}，行为不一致。
	// 移除 omitempty 后 nil 输出 "fields":null，与 Python 行为更接近）
	Fields map[string]any `json:"fields"`
}

// MemorySearchResult 记忆搜索结果，包含匹配文档和相关度分数。
//
// 对应 Python: search 方法返回的 tuple[MemoryDoc, float]
type MemorySearchResult struct {
	// Doc 匹配的记忆文档
	Doc *MemoryDoc
	// Score 相关度分数，范围 [0, 1]，越高越相关
	Score float64
}

// UserScope 用户-作用域对，用于 ListUserScopes 返回值。
//
// 对应 Python: list_user_scopes 返回的 tuple[str, str]
type UserScope struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
}

// StorageCodec 存储编解码器接口，用于对记忆文本进行加解密。
//
// 实现示例：AES 编解码器，对记忆文本进行加密存储和解密读取。
// 方法永远返回 string，不返回 error（对齐 Python Protocol 签名：encode/decode → str）。
// 加解密失败时由实现方自行记录日志并返回原文（容错模式）。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (StorageCodec)
type StorageCodec interface {
	// Encode 对文本进行编码（如加密），失败时返回原文
	Encode(text string) string
	// Decode 对数据进行解码（如解密），失败时返回原文
	Decode(data string) string
}

// BaseMemoryIndex 记忆索引抽象接口，定义记忆文档的存储和检索操作。
//
// 所有记忆索引实现必须实现此接口。记忆文档以 user_id 和 scope_id 隔离，
// 支持多租户和多场景的记忆管理。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (BaseMemoryIndex)
type BaseMemoryIndex interface {
	// SetStorageCodec 设置存储编解码器。
	SetStorageCodec(codec StorageCodec)

	// AddMemories 添加新的记忆文档。
	AddMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error

	// UpdateMemories 更新记忆文档。
	UpdateMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error

	// DeleteMemories 按 ID 删除记忆文档。
	DeleteMemories(ctx context.Context, userID string, scopeID string, ids []string) error

	// DeleteByUser 删除指定用户的所有记忆（跨所有 scope）。
	DeleteByUser(ctx context.Context, userID string) error

	// DeleteByScope 删除指定 scope 的所有记忆（跨所有 user）。
	DeleteByScope(ctx context.Context, scopeID string) error

	// DeleteByUserAndScope 删除指定用户和 scope 组合的所有记忆。
	DeleteByUserAndScope(ctx context.Context, userID string, scopeID string) error

	// Search 语义搜索记忆文档，返回最相关的结果及相关度分数。
	// memTypes 为 nil 或空切片时搜索所有类型；topK 为 0 时使用默认值 10。
	Search(ctx context.Context, userID string, scopeID string, query string, memTypes []string, topK int) ([]*MemorySearchResult, error)

	// GetByID 按 ID 获取单条记忆文档，不存在时返回 nil, nil。
	GetByID(ctx context.Context, userID string, scopeID string, memID string) (*MemoryDoc, error)

	// ListMemories 分页获取记忆文档列表。
	// memTypes 为 nil 或空切片时返回所有类型；多个 memType 时按 memType 顺序排列。
	ListMemories(ctx context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*MemoryDoc, error)

	// GetSchemaVersion 获取当前 schema 版本号，未设置时返回 0。
	GetSchemaVersion() int

	// UpdateSchemaVersion 更新 schema 版本号。
	UpdateSchemaVersion(version int)

	// CreateBackup 创建当前数据的备份，返回备份标识。
	CreateBackup(ctx context.Context) (string, error)

	// RestoreBackup 从备份恢复数据。
	RestoreBackup(ctx context.Context, backupID string) error

	// CleanupBackup 清理备份。
	CleanupBackup(ctx context.Context, backupID string) error

	// ListUserScopes 列出索引中所有 (userID, scopeID) 对。
	ListUserScopes(ctx context.Context) ([]UserScope, error)
}

// backupData 备份数据
type backupData struct {
	// SchemaVersion 备份时的 schema 版本
	SchemaVersion int
}

// MemoryIndexBase 记忆索引的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。
// 默认提供 ListMemories / GetSchemaVersion / UpdateSchemaVersion /
// CreateBackup / RestoreBackup / CleanupBackup / ListUserScopes 的通用行为。
// backups map 和 schemaVersion 字段通过 sync.RWMutex 保护并发安全。
//
// 对应 Python: BaseMemoryIndex 中的非抽象方法默认实现
type MemoryIndexBase struct {
	// mu 保护 backups 和 schemaVersion 的并发访问
	mu sync.RWMutex
	// schemaVersion schema 版本号
	schemaVersion int
	// backups 备份数据（内存中的简单实现）
	backups map[string]*backupData
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTopK Search 默认返回结果数量（被 simple.go 等实现类引用）
	defaultTopK = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件常量，store 属于基础设施层（被 simple.go 等实现类引用）
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryIndexBase 创建记忆索引基类实例。
func NewMemoryIndexBase() *MemoryIndexBase {
	return &MemoryIndexBase{
		backups: make(map[string]*backupData),
	}
}

// ListMemories 分页获取记忆文档列表（默认实现：返回空结果）。
// 具体实现类应覆盖此方法以提供真正的数据列举。
func (b *MemoryIndexBase) ListMemories(_ context.Context, _ string, _ string, _ int, _ int, _ []string) ([]*MemoryDoc, error) {
	return nil, nil
}

// GetSchemaVersion 获取当前 schema 版本号，未设置时返回 0。
func (b *MemoryIndexBase) GetSchemaVersion() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.schemaVersion
}

// UpdateSchemaVersion 更新 schema 版本号。
func (b *MemoryIndexBase) UpdateSchemaVersion(version int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.schemaVersion = version
}

// CreateBackup 创建当前数据的备份，返回备份标识。
func (b *MemoryIndexBase) CreateBackup(_ context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bid := uuid.New().String()
	b.backups[bid] = &backupData{SchemaVersion: b.schemaVersion}
	return bid, nil
}

// RestoreBackup 从备份恢复数据。备份不存在时返回 StatusMemoryBackupNotFound 错误。
func (b *MemoryIndexBase) RestoreBackup(_ context.Context, backupID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.backups[backupID]
	if !ok {
		logger.Error(logComponent).
			Str("backup_id", backupID).
			Str("event_type", "MEMORY_BACKUP_ERROR").
			Msg("备份不存在，恢复失败")
		return exception.BuildError(exception.StatusMemoryBackupNotFound,
			exception.WithParam("backup_id", backupID),
		)
	}
	b.schemaVersion = data.SchemaVersion
	return nil
}

// CleanupBackup 清理备份。
// Python 中此方法为 @abstractmethod，子类必须实现。Go 提供了默认实现（从内存 map 中删除），
// 子类应覆盖此方法以确保外部存储中的备份数据也被正确清理。
func (b *MemoryIndexBase) CleanupBackup(_ context.Context, backupID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.backups, backupID)
	return nil
}

// ListUserScopes 列出索引中所有 (userID, scopeID) 对（默认实现：返回空结果）。
// Python 中此方法为 @abstractmethod，子类必须实现。Go 提供了默认空实现，
// 子类应覆盖此方法以提供真正的数据扫描。
func (b *MemoryIndexBase) ListUserScopes(_ context.Context) ([]UserScope, error) {
	return nil, nil
}
