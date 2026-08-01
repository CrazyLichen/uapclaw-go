package database

import "context"

// ──────────────────────────── 导出函数 ────────────────────────────

// InitializeEngine 初始化数据库引擎。⤵️ 回填: 9.65a
func InitializeEngine(ctx context.Context, cfg DBConfigProvider) (any, error) { return nil, nil }

// CreateCurSessionTablesFromEngine 从引擎创建当前会话表。⤵️ 回填: 9.65a
func CreateCurSessionTablesFromEngine(ctx context.Context, engine any) error { return nil }

// DropCurSessionTablesFromEngine 从引擎删除当前会话表。⤵️ 回填: 9.65a
func DropCurSessionTablesFromEngine(ctx context.Context, engine any) error { return nil }

// CleanupAllRuntimeStateFromEngine 从引擎清理所有运行时状态。⤵️ 回填: 9.65a
func CleanupAllRuntimeStateFromEngine(ctx context.Context, engine any) ([]string, []string, error) { return nil, nil, nil }

// DropSessionTablesByIDFromEngine 按 ID 删除动态表。⤵️ 回填: 9.65a
func DropSessionTablesByIDFromEngine(ctx context.Context, engine any, sessionID string) ([]string, error) { return nil, nil }
