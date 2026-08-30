package database

import (
	"encoding/hex"
	"time"

	"golang.org/x/crypto/blake2s"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

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

// ──────────────────────────── 非导出函数 ────────────────────────────
