package migrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KVMigrator KV 存储迁移执行器。
// 版本跟踪通过 KV_STORE 自身的 key MEMORY_MIGRATION_KV_SCHEMA_VERSION。
// 迁移前创建备份，失败时从备份恢复。
//
// Python: openjiuwen/core/memory/migration/migrator/kv_migrator.py (KVMigrator)
type KVMigrator struct {
	// kvStore KV 存储实例
	kvStore kv.BaseKVStore
	// kvRegistry KV 操作注册表（用于获取初始版本号）
	kvRegistry *operation.OperationRegistry
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// KVSchemaVersionKey KV schema 版本存储键
	// Python: KV_SCHEMA_VERSION = "MEMORY_MIGRATION_KV_SCHEMA_VERSION"
	KVSchemaVersionKey = "MEMORY_MIGRATION_KV_SCHEMA_VERSION"
	// KVEntityKey KV 迁移实体键
	// Python: KV_ENTITY_KEY = "kv_global"
	KVEntityKey = "kv_global"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent migrator 包日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKVMigrator 创建 KVMigrator 实例。
//
// Python: KVMigrator(kv_store)
func NewKVMigrator(kvStore kv.BaseKVStore, kvRegistry *operation.OperationRegistry) *KVMigrator {
	return &KVMigrator{kvStore: kvStore, kvRegistry: kvRegistry}
}

// TryMigrate 尝试执行 KV 迁移。
// 校验 entityKey、校验操作顺序、比较版本、创建备份后逐个执行操作，失败时从备份恢复。
//
// Python: KVMigrator.try_migrate(entity_key, operations)
func (m *KVMigrator) TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error {
	if entityKey != KVEntityKey {
		logger.Error(logComponent).Str("entity_key", entityKey).Str("expected_key", KVEntityKey).
			Msg("不支持的 entity_key")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("entity_key", entityKey),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的 entity_key: '%s', 期望: '%s'", entityKey, KVEntityKey)),
		)
	}

	if len(operations) == 0 {
		return nil
	}

	if !validateOperationsOrder(operations) {
		logger.Error(logComponent).Msg("操作的 schema_version 不是升序排列")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("error_msg", "操作的 schema_version 不是升序排列"),
		)
	}

	currentVersion, err := m.getCurrentVersion(ctx)
	if err != nil {
		return err
	}

	lastOpVersion := operations[len(operations)-1].SchemaVersion()
	if currentVersion != nil && *currentVersion >= lastOpVersion {
		logger.Info(logComponent).Int("current_version", *currentVersion).Int("last_op_version", lastOpVersion).
			Msg("当前版本已 >= 最新操作版本，无需迁移")
		return nil
	}

	pendingOps := filterPendingOperations(operations, currentVersion)
	if len(pendingOps) == 0 {
		return nil
	}

	logger.Info(logComponent).Int("pending_count", len(pendingOps)).Msg("发现待执行操作")

	backupKey, err := m.createBackup(ctx, currentVersion)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("创建 KV 备份失败")
		return err
	}

	lastVersion := currentVersion
	for idx, op := range pendingOps {
		logger.Info(logComponent).Int("idx", idx+1).Int("total", len(pendingOps)).
			Str("op_type", op.TypeName()).Int("schema_version", op.SchemaVersion()).
			Msg("执行操作")

		if err := m.executeOperation(ctx, op); err != nil {
			logger.Error(logComponent).Err(err).Str("op_type", op.TypeName()).
				Int("schema_version", op.SchemaVersion()).Msg("操作执行失败")

			if restoreErr := m.restoreFromBackup(ctx, backupKey); restoreErr != nil {
				logger.Error(logComponent).Err(restoreErr).Str("backup_key", backupKey).
					Msg("从备份恢复失败")
			}
			return err
		}

		v := op.SchemaVersion()
		lastVersion = &v
		logger.Info(logComponent).Str("op_type", op.TypeName()).Int("schema_version", op.SchemaVersion()).
			Msg("操作执行成功")
	}

	if lastVersion != nil && (currentVersion == nil || *lastVersion != *currentVersion) {
		if err := m.updateVersion(ctx, *lastVersion); err != nil {
			return err
		}
		logger.Info(logComponent).
			Int("new_version", *lastVersion).
			Msg("KV schema 版本已更新")
	}

	m.cleanupBackup(ctx, backupKey)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getCurrentVersion 获取当前 KV schema 版本。
//
// Python: KVMigrator._get_current_version
func (m *KVMigrator) getCurrentVersion(ctx context.Context) (*int, error) {
	versionBytes, err := m.kvStore.Get(ctx, KVSchemaVersionKey)
	if err != nil {
		return nil, err
	}

	if versionBytes == nil {
		hasData, err := m.hasMemoryModuleData(ctx)
		if err != nil {
			return nil, err
		}

		if !hasData {
			// KV store 新初始化（无记忆模块数据），设置初始版本
			initialVersion := m.kvRegistry.GetCurrentVersion(KVEntityKey)
			if initialVersion > 0 {
				if err := m.updateVersion(ctx, initialVersion); err != nil {
					return nil, err
				}
				return &initialVersion, nil
			}
			return nil, nil
		}

		// 有记忆模块数据但缺少版本键，说明是未迁移的旧数据，返回 nil 触发迁移
		return nil, nil
	}

	// 尝试从 []byte 解析版本号
	versionStr := string(versionBytes)
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		logger.Error(logComponent).Str("version_value", versionStr).
			Msg("KV_SCHEMA_VERSION 格式无效，期望数字字符串")
		return nil, exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("无效的 SCHEMA_VERSION 格式: '%s'", versionStr)),
		)
	}

	return &version, nil
}

// hasMemoryModuleData 检查 KV store 是否包含记忆模块数据。
//
// Python: KVMigrator._has_memory_module_data
func (m *KVMigrator) hasMemoryModuleData(ctx context.Context) (bool, error) {
	prefixes := common.KVPrefixRegistry.GetAllPrefixes()
	for _, prefix := range prefixes {
		data, err := m.kvStore.GetByPrefix(ctx, prefix)
		if err != nil {
			return false, err
		}
		if len(data) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// updateVersion 更新 KV schema 版本。
//
// Python: KVMigrator._update_version
func (m *KVMigrator) updateVersion(ctx context.Context, version int) error {
	if err := m.kvStore.Set(ctx, KVSchemaVersionKey, []byte(strconv.Itoa(version))); err != nil {
		logger.Error(logComponent).Err(err).Int("version", version).Msg("更新 KV 版本失败")
		return err
	}
	return nil
}

// executeOperation 执行单个迁移操作。
//
// Python: KVMigrator._execute_operation
func (m *KVMigrator) executeOperation(ctx context.Context, op operation.Operation) error {
	switch o := op.(type) {
	case *operation.UpdateKVOperation:
		return o.UpdateFunc(ctx, m.kvStore)
	default:
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("op_type", op.TypeName()),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的操作类型: %s", op.TypeName())),
		)
	}
}

// createBackup 创建 KV 数据备份。
//
// Python: KVMigrator._create_backup
func (m *KVMigrator) createBackup(ctx context.Context, currentVersion *int) (string, error) {
	backupKey := fmt.Sprintf("%s_BACKUP_%d", KVSchemaVersionKey, time.Now().UnixMilli())
	backupData := make(map[string]string)
	prefixes := common.KVPrefixRegistry.GetAllPrefixes()

	for _, prefix := range prefixes {
		data, err := m.kvStore.GetByPrefix(ctx, prefix)
		if err != nil {
			return "", err
		}
		for k, v := range data {
			backupData[k] = string(v)
		}
	}

	if currentVersion != nil {
		versionBytes, err := m.kvStore.Get(ctx, KVSchemaVersionKey)
		if err != nil {
			return "", err
		}
		if versionBytes != nil {
			backupData[KVSchemaVersionKey] = string(versionBytes)
		}
	}

	if len(backupData) > 0 {
		backupJSON, err := json.Marshal(backupData)
		if err != nil {
			return "", err
		}
		if err := m.kvStore.Set(ctx, backupKey, backupJSON); err != nil {
			return "", err
		}
	}

	logger.Info(logComponent).Str("backup_key", backupKey).Msg("已创建 KV 备份")
	return backupKey, nil
}

// restoreFromBackup 从备份恢复 KV 数据。
//
// Python: KVMigrator._restore_from_backup
func (m *KVMigrator) restoreFromBackup(ctx context.Context, backupKey string) error {
	backupJSON, err := m.kvStore.Get(ctx, backupKey)
	if err != nil {
		return err
	}
	if backupJSON == nil {
		logger.Error(logComponent).Str("backup_key", backupKey).Msg("备份不存在")
		return nil
	}

	var backupData map[string]string
	if err := json.Unmarshal(backupJSON, &backupData); err != nil {
		logger.Error(logComponent).Err(err).Msg("反序列化备份数据失败")
		return err
	}

	prefixes := common.KVPrefixRegistry.GetAllPrefixes()
	for _, prefix := range prefixes {
		if err := m.kvStore.DeleteByPrefix(ctx, prefix, 0); err != nil {
			return err
		}
	}

	for key, value := range backupData {
		if err := m.kvStore.Set(ctx, key, []byte(value)); err != nil {
			return err
		}
	}

	logger.Info(logComponent).Int("key_count", len(backupData)).Msg("已从备份恢复 KV 数据")
	return nil
}

// cleanupBackup 清理备份数据。
//
// Python: KVMigrator._cleanup_backup
func (m *KVMigrator) cleanupBackup(ctx context.Context, backupKey string) {
	if err := m.kvStore.Delete(ctx, backupKey); err != nil {
		logger.Warn(logComponent).Err(err).Str("backup_key", backupKey).Msg("清理 KV 备份失败")
	} else {
		logger.Info(logComponent).Str("backup_key", backupKey).Msg("已清理 KV 备份")
	}
}

// validateOperationsOrder 校验操作列表的 schema_version 严格升序。
//
// Python: KVMigrator._validate_operations_order
func validateOperationsOrder(operations []operation.Operation) bool {
	for i := 0; i < len(operations)-1; i++ {
		if operations[i].SchemaVersion() >= operations[i+1].SchemaVersion() {
			return false
		}
	}
	return true
}

// filterPendingOperations 过滤出 schema_version > currentVersion 的操作。
//
// 对齐 Python: [op for op in operations if current_version is None or op.schema_version > current_version]
func filterPendingOperations(operations []operation.Operation, currentVersion *int) []operation.Operation {
	if currentVersion == nil {
		return operations
	}
	var pending []operation.Operation
	for _, op := range operations {
		if op.SchemaVersion() > *currentVersion {
			pending = append(pending, op)
		}
	}
	return pending
}
