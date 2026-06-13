package graph

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetUUID 生成32位十六进制UUID（无连字符）
func GetUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GetCurrentUTCTimestamp 获取当前UTC时间戳（秒级整数）
func GetCurrentUTCTimestamp() int64 {
	return time.Now().UTC().Unix()
}
