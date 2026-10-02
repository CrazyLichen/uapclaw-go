package migrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorMigrator 向量集合迁移执行器。
// 按 entity_key 发现目标集合，调用 VectorStore.UpdateSchema 执行迁移。
//
// Python: openjiuwen/core/memory/migration/migrator/vector_migrator.py (VectorMigrator)
type VectorMigrator struct {
	// vectorStore 向量存储实例
	vectorStore vector.BaseVectorStore
	// supportedTypes 支持的记忆类型列表（如 ["user_profile", "summary"]）
	supportedTypes []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorMigrator 创建 VectorMigrator 实例。
// supportedTypes 为支持的记忆类型列表（如 []string{"user_profile", "summary"}）。
//
// Python: VectorMigrator(vector_store)
func NewVectorMigrator(vectorStore vector.BaseVectorStore, supportedTypes []string) *VectorMigrator {
	return &VectorMigrator{vectorStore: vectorStore, supportedTypes: supportedTypes}
}

// TryMigrate 尝试执行向量集合迁移。
// 发现目标集合→过滤待执行操作→调用 UpdateSchema→更新 metadata。
//
// Python: VectorMigrator.try_migrate(entity_key, operations)
func (m *VectorMigrator) TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error {
	collectionNames, err := m.findCollections(ctx, entityKey)
	if err != nil {
		return err
	}

	return m.migrateCollections(ctx, collectionNames, operations)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// migrateCollections 对集合列表应用版本过滤后的 schema 迁移。
//
// Python: VectorMigrator._migrate_collections
func (m *VectorMigrator) migrateCollections(ctx context.Context, collectionNames []string, operations []operation.Operation) error {
	for _, collectionName := range collectionNames {
		// 获取集合元数据，包含 schema_version
		metadata, err := m.vectorStore.GetCollectionMetadata(ctx, collectionName)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Msg("获取集合元数据失败")
			return err
		}

		currentVersion := 0
		if v, ok := metadata["schema_version"]; ok {
			if vi, ok := v.(int); ok {
				currentVersion = vi
			} else if vf, ok := v.(float64); ok {
				currentVersion = int(vf)
			}
		}

		// 收集版本号大于当前版本的操作
		var opsToApply []operation.Operation
		for _, op := range operations {
			if op.SchemaVersion() > currentVersion {
				opsToApply = append(opsToApply, op)
			}
		}

		if len(opsToApply) == 0 {
			continue
		}

		// 一次性应用所有操作
		if err := m.vectorStore.UpdateSchema(ctx, collectionName, opsToApply); err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Int("pending_count", len(opsToApply)).Msg("集合 schema 更新失败")
			return err
		}

		// 更新到已应用操作中的最大版本号
		maxVersion := 0
		for _, op := range opsToApply {
			if op.SchemaVersion() > maxVersion {
				maxVersion = op.SchemaVersion()
			}
		}
		if err := m.vectorStore.UpdateCollectionMetadata(ctx, collectionName,
			map[string]any{"schema_version": maxVersion}); err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Int("new_version", maxVersion).Msg("更新集合 schema_version 失败")
			return err
		}

		logger.Info(logComponent).Str("collection_name", collectionName).
			Int("old_version", currentVersion).Int("new_version", maxVersion).
			Msg("集合 schema 迁移完成")
	}
	return nil
}

// findCollections 发现所有匹配 entity_key 的向量集合。
// 去掉 'vector_' 前缀→校验 SupportMemoryType→ListCollectionNames 过滤后缀。
//
// Python: VectorMigrator._find_collections
func (m *VectorMigrator) findCollections(ctx context.Context, memTypeStr string) ([]string, error) {
	// 去掉 'vector_' 前缀
	memTypeStr = strings.TrimPrefix(memTypeStr, "vector_")

	// 校验记忆类型是否支持
	isSupported := false
	for _, st := range m.supportedTypes {
		if st == memTypeStr {
			isSupported = true
			break
		}
	}

	if !isSupported {
		logger.Error(logComponent).Str("mem_type", memTypeStr).
			Strs("supported_types", m.supportedTypes).
			Msg("不支持的 memory type")
		return nil, exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("mem_type", memTypeStr),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的 memory type: '%s', 支持的类型: %v", memTypeStr, m.supportedTypes)),
		)
	}

	allCollections, err := m.vectorStore.ListCollectionNames(ctx)
	if err != nil {
		return nil, err
	}

	suffix := "_" + memTypeStr
	var matched []string
	for _, name := range allCollections {
		if strings.HasSuffix(name, suffix) {
			matched = append(matched, name)
		}
	}

	return matched, nil
}
