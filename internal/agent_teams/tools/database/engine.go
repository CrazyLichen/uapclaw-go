package database

import (
	"context"
	"encoding/hex"
	"time"

	"golang.org/x/crypto/blake2s"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCurrentTime 返回当前毫秒时间戳。对齐 Python: get_current_time()
func GetCurrentTime() int64 {
	return time.Now().UnixMilli()
}

// SanitizeSessionIDForTable 将 session_id 转为 SQL-safe 的固定长度 hex 后缀。
// 对齐 Python: _sanitize_session_id_for_table(session_id)
// 使用 BLAKE2s XOF(size=8) → 16 hex chars（精确对齐 Python digest_size=8）
func SanitizeSessionIDForTable(sessionID string) string {
	xof, err := blake2s.NewXOF(8, nil)
	if err != nil {
		// 降级为简单 hex 截断
		return hex.EncodeToString([]byte(sessionID))[:16]
	}
	if _, err := xof.Write([]byte(sessionID)); err != nil {
		return hex.EncodeToString([]byte(sessionID))[:16]
	}
	buf := make([]byte, 8)
	if _, err := xof.Read(buf); err != nil {
		return hex.EncodeToString([]byte(sessionID))[:16]
	}
	return hex.EncodeToString(buf)
}

// InitializeEngine 初始化数据库引擎。⤵️ 9.65a-5 SQL 实现时回填
func InitializeEngine(_ context.Context, _ DBConfigProvider) (any, error) { return nil, nil }

// CreateCurSessionTablesFromEngine 创建当前会话动态表。⤵️ 9.65a-5
func CreateCurSessionTablesFromEngine(_ context.Context, _ any) error { return nil }

// DropCurSessionTablesFromEngine 删除当前会话动态表。⤵️ 9.65a-5
func DropCurSessionTablesFromEngine(_ context.Context, _ any) error { return nil }

// CleanupAllRuntimeStateFromEngine 清理所有运行时状态。⤵️ 9.65a-5
func CleanupAllRuntimeStateFromEngine(_ context.Context, _ any) ([]string, []string, error) {
	return nil, nil, nil
}

// DropSessionTablesByIDFromEngine 按 ID 删除动态表。⤵️ 9.65a-5
func DropSessionTablesByIDFromEngine(_ context.Context, _ any, _ string) ([]string, error) {
	return nil, nil
}
