package ltm

import (
	"context"
	"fmt"

	db "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	storeindex "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RegisterStoreOption RegisterStore 的可选参数。
type RegisterStoreOption func(*registerStoreParams)

// registerStoreParams RegisterStore 的全部参数。
type registerStoreParams struct {
	kvStore        kv.BaseKVStore
	vectorStore    vector.BaseVectorStore
	dbStore        db.BaseDbStore
	embeddingModel embedding.BaseEmbedding
	messageStore   db.BaseMessageStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func init() {
	// S-14: 设置默认支持记忆类型，打破 migration → mem_model 的循环依赖
	migration.DefaultSupportMemoryTypes = mem_model.AllSupportMemoryTypeValues
}

// RegisterStore 注册存储实例。
//
// 完整流程对齐 Python register_store：
// 1. kv_store 必填校验
// 2. 赋值各存储
// 3. vector_store + kv_store → 自动注册 SimpleMemoryIndex
// 4. db_store → create_tables
// 5. 无 message_store + 有 db_store → 自动创建 SqlMessageStore
// 6. 调用 SetConfig 初始化所有 manager
// 7. 执行 4 类 migration
//
// Python: LongTermMemory.register_store(kv_store, vector_store, db_store, embedding_model, message_store)
func (m *LongTermMemory) RegisterStore(
	ctx context.Context,
	kvStore kv.BaseKVStore,
	opts ...RegisterStoreOption,
) error {
	// Step 1: kv_store 必填校验
	if err := validateKVStore(kvStore); err != nil {
		return err
	}

	// 解析可选参数
	params := &registerStoreParams{kvStore: kvStore}
	for _, opt := range opts {
		opt(params)
	}

	// Step 2: 赋值各存储
	m.kvStore = kvStore
	m.vectorStore = params.vectorStore
	m.dbStore = params.dbStore
	m.baseEmbed = params.embeddingModel
	m.messageStore = params.messageStore

	// Step 3: vector_store + kv_store → 自动注册 SimpleMemoryIndex
	if m.vectorStore != nil && m.kvStore != nil {
		simpleIndex := storeindex.NewSimpleMemoryIndex(m.kvStore, m.vectorStore, m.baseEmbed)
		m.RegisterPlugin("simple_memory_index", simpleIndex, nil)
	}

	// Step 4: create_tables
	if m.dbStore != nil {
		if err := mem_model.CreateTables(m.dbStore.GetDB(ctx)); err != nil {
			logger.Error(logComponent).Err(err).Msg("CreateTables 失败")
			return exception.BuildError(exception.StatusMemoryRegisterStoreExecutionError,
				exception.WithParam("store_type", "db store"),
				exception.WithMsg(fmt.Sprintf("create_tables failed: %v", err)),
				exception.WithCause(err),
			)
		}
	}

	// Step 5: 无 message_store + 有 db_store → 自动创建 SqlMessageStore
	if m.messageStore == nil && m.dbStore != nil {
		// M-02: 缓存 sqlDbStore，避免重复创建
		if m.sqlDbStore == nil {
			m.sqlDbStore = mem_model.NewSqlDbStore(m.dbStore)
		}
		sqlMsgStore, err := mem_model.NewSqlMessageStore(nil, m.sqlDbStore, "")
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 SqlMessageStore 失败")
			return exception.BuildError(exception.StatusMemoryRegisterStoreExecutionError,
				exception.WithParam("store_type", "message store"),
				exception.WithMsg(fmt.Sprintf("create SqlMessageStore failed: %v", err)),
				exception.WithCause(err),
			)
		}
		m.messageStore = sqlMsgStore
	}

	// Step 6: SetConfig 初始化所有 manager
	if err := m.SetConfig(config.DefaultMemoryEngineConfig()); err != nil {
		return err
	}

	// Step 7: 执行 4 类 migration
	if err := runMigration(ctx, func(ctx context.Context) error {
		return migration.RunKVMigrations(ctx, m.kvStore)
	}, "kv store"); err != nil {
		return err
	}

	if m.vectorStore != nil {
		if err := runMigration(ctx, func(ctx context.Context) error {
			return migration.RunVectorMigrations(ctx, m.vectorStore, nil)
		}, "vector store"); err != nil {
			return err
		}
	}

	if m.dbStore != nil {
		// M-02: 复用缓存的 sqlDbStore
		if m.sqlDbStore == nil {
			m.sqlDbStore = mem_model.NewSqlDbStore(m.dbStore)
		}
		if err := runMigration(ctx, func(ctx context.Context) error {
			return migration.RunSQLMigrations(ctx, m.sqlDbStore)
		}, "db store"); err != nil {
			return err
		}
	}

	if m.messageStore != nil {
		if err := runMigration(ctx, func(ctx context.Context) error {
			return migration.RunMessageMigrations(ctx, m.messageStore)
		}, "message store"); err != nil {
			return err
		}
	}

	return nil
}

// WithVectorStore 设置向量存储。
func WithVectorStore(store vector.BaseVectorStore) RegisterStoreOption {
	return func(p *registerStoreParams) { p.vectorStore = store }
}

// WithDbStore 设置数据库存储。
func WithDbStore(store db.BaseDbStore) RegisterStoreOption {
	return func(p *registerStoreParams) { p.dbStore = store }
}

// WithEmbeddingModel 设置嵌入模型。
func WithEmbeddingModel(model embedding.BaseEmbedding) RegisterStoreOption {
	return func(p *registerStoreParams) { p.embeddingModel = model }
}

// WithMessageStore 设置消息存储。
func WithMessageStore(store db.BaseMessageStore) RegisterStoreOption {
	return func(p *registerStoreParams) { p.messageStore = store }
}

// RegisterPlugin 注册 BaseMemoryIndex 插件。
//
// Python: LongTermMemory.register_plugin(name, cls, params)
//
// Go 差异：Python 使用 (name, cls, params) 三元组做动态实例化，
// Go 使用 (name, memoryIndex, params) 模式——name 用于标识，memoryIndex 为已实例化的索引，
// params 保留用于日志和未来 index_registry 扩展。
func (m *LongTermMemory) RegisterPlugin(name string, memoryIndex storeindex.BaseMemoryIndex, params map[string]any) {
	if m.memoryIndex == nil {
		m.memoryIndex = memoryIndex
		logger.Info(logComponent).Str("plugin_name", name).
			Msg("RegisterPlugin: 已注册索引插件")
	} else {
		logger.Warn(logComponent).Str("plugin_name", name).
			Msg("RegisterPlugin: 已有索引插件，跳过注册")
	}
}

// MigrateBetweenIndices 跨索引迁移，将源索引的所有数据复制到目标索引。
//
// Python: LongTermMemory.migrate_between_indices(source_index, target_index)
func MigrateBetweenIndices(ctx context.Context, sourceIndex, targetIndex storeindex.BaseMemoryIndex) error {
	scopes, err := sourceIndex.ListUserScopes(ctx)
	if err != nil {
		return err
	}

	for _, us := range scopes {
		offset := 0
		batchSize := 100

		for {
			docs, err := sourceIndex.ListMemories(ctx, us.UserID, us.ScopeID, offset, batchSize, nil)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				break
			}

			targetDocs := make([]*storeindex.MemoryDoc, len(docs))
			for i, doc := range docs {
				targetDocs[i] = &storeindex.MemoryDoc{
					ID:        doc.ID,
					Text:      doc.Text,
					Type:      doc.Type,
					Timestamp: doc.Timestamp,
					Fields:    copyFields(doc.Fields),
				}
			}
			if err := targetIndex.AddMemories(ctx, us.UserID, us.ScopeID, targetDocs); err != nil {
				return err
			}
			offset += batchSize
		}
	}

	logger.Info(logComponent).Int("scope_count", len(scopes)).
		Msg("Cross-index migration completed")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyFields 深拷贝 fields map。
func copyFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	result := make(map[string]any, len(fields))
	for k, v := range fields {
		result[k] = v
	}
	return result
}
