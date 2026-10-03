package migration

import (
	"context"
	"fmt"

	db "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/migrator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// DefaultSupportMemoryTypes 返回默认支持的记忆类型列表。
// 由调用方（如 ltm 包）在 init 中设置，以打破 migration → mem_model 的循环依赖。
var DefaultSupportMemoryTypes func() []string

// ──────────────────────────── 导出函数 ────────────────────────────

// RunVectorMigrations 执行所有已注册的向量迁移。
// supportedTypes 为支持的记忆类型列表（如 []string{"user_profile", "summary"}）。
// 当 supportedTypes 为 nil 或空时，从 DefaultSupportMemoryTypes 获取默认值。
//
// Python: run_vector_migrations(vector_store)
func RunVectorMigrations(ctx context.Context, vectorStore vector.BaseVectorStore, supportedTypes []string) error {
	// S-14: 对齐 Python，nil 时从 SupportMemoryType 枚举默认获取
	if len(supportedTypes) == 0 && DefaultSupportMemoryTypes != nil {
		supportedTypes = DefaultSupportMemoryTypes()
	}
	return runMigrationsWithRegistry(ctx, VectorRegistry,
		func() migrator.Migrator {
			return migrator.NewVectorMigrator(vectorStore, supportedTypes)
		},
		"vector store",
	)
}

// RunKVMigrations 执行所有已注册的 KV 迁移。
//
// Python: run_kv_migrations(kv_store)
func RunKVMigrations(ctx context.Context, kvStore kv.BaseKVStore) error {
	return runMigrationsWithRegistry(ctx, KVRegistry,
		func() migrator.Migrator {
			return migrator.NewKVMigrator(kvStore, KVRegistry)
		},
		"kv store",
	)
}

// RunSQLMigrations 执行所有已注册的 SQL 迁移。
//
// Python: run_sql_migrations(sql_db_store)
func RunSQLMigrations(ctx context.Context, sqlDbStore migrator.SqlDbStoreForMigrator) error {
	db := sqlDbStore.GetDB()
	metaManager := migrator.NewMemoryMetaManager(sqlDbStore)
	return runMigrationsWithRegistry(ctx, SQLRegistry,
		func() migrator.Migrator {
			return migrator.NewSQLMigrator(db, metaManager)
		},
		"db store",
	)
}

// RunMessageMigrations 执行所有已注册的消息迁移。
//
// Python: run_message_migrations(message_store)
func RunMessageMigrations(ctx context.Context, messageStore db.BaseMessageStore) error {
	return runMigrationsWithRegistry(ctx, MessageRegistry,
		func() migrator.Migrator {
			return migrator.NewMessageMigrator(messageStore)
		},
		"message",
	)
}

// RunIndexVersionMigrations 执行所有已注册的索引版本迁移。
// 注意：此函数不使用 runMigrationsWithRegistry，因为 IndexVersionMigrator.TryMigrate 签名不同。
//
// Python: run_index_version_migrations(index)
func RunIndexVersionMigrations(ctx context.Context, idx index.BaseMemoryIndex) error {
	registryMap := IndexRegistry.GetAllOperations()

	if len(registryMap) == 0 {
		logger.Info(logComponent).Str("event_type", "MEMORY_INIT").
			Str("sub_component", "run_migrations").Msg("无已注册的 index version 迁移，跳过")
		return nil
	}

	for entityKey, operations := range registryMap {
		m := migrator.NewIndexVersionMigrator()
		if err := m.TryMigrate(ctx, idx, operations); err != nil {
			logger.Error(logComponent).Str("event_type", "MEMORY_INIT").Err(err).
				Str("sub_component", "run_migrations").
				Str("store_name", "index version").Str("entity_key", entityKey).
				Msg("index version 迁移失败")
			return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
				exception.WithParam("store_name", "index version"),
				exception.WithParam("entity_key", entityKey),
				exception.WithParam("error_msg", fmt.Sprintf("index version 迁移失败: %v", err)),
			)
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runMigrationsWithRegistry 通用迁移运行器，处理日志和错误处理。
//
// Python: _run_migrations_with_registry(registry, migrator_factory, store_name)
func runMigrationsWithRegistry(
	ctx context.Context,
	registry *operation.OperationRegistry,
	migratorFactory func() migrator.Migrator,
	storeName string,
) error {
	m := migratorFactory()
	registryMap := registry.GetAllOperations()

	if len(registryMap) == 0 {
		logger.Info(logComponent).Str("event_type", "MEMORY_INIT").
			Str("sub_component", "run_migrations").Str("store_name", storeName).Msg("无已注册的迁移，跳过")
		return nil
	}

	for entityKey, operations := range registryMap {
		if err := m.TryMigrate(ctx, entityKey, operations); err != nil {
			logger.Error(logComponent).Str("event_type", "MEMORY_INIT").Err(err).
				Str("sub_component", "run_migrations").
				Str("store_name", storeName).Str("entity_key", entityKey).Msg("迁移失败")
			return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
				exception.WithParam("entity_key", entityKey),
				exception.WithParam("store_name", storeName),
				exception.WithParam("error_msg", fmt.Sprintf("%s 迁移失败 (entity: %s): %v", storeName, entityKey, err)),
			)
		}
	}

	return nil
}
