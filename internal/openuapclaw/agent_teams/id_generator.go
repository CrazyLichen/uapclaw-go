package agent_teams

import (
	"crypto/rand"
	"math/big"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// idAlphabet base36 字符集（小写，url-safe）
	idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	// defaultIDPrefix 未知 kind 的默认前缀
	defaultIDPrefix = "t"
	// defaultIDBodyLength 默认随机体长度
	defaultIDBodyLength = 8
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// taskPrefixes kind → 单字符前缀
	taskPrefixes = map[string]string{
		"async_tool":    "x",
		"swarmflow":     "w",
		"session_spawn": "s",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateID 生成带前缀的随机 id。
// Python: generate_id(kind, *, length=8)（agent_teams/id_generator.py）
func GenerateID(kind string, length ...int) string {
	n := defaultIDBodyLength
	if len(length) > 0 && length[0] > 0 {
		n = length[0]
	}
	prefix := taskPrefixes[kind]
	if prefix == "" {
		prefix = defaultIDPrefix
	}
	body := make([]byte, n)
	max := big.NewInt(int64(len(idAlphabet)))
	for i := range body {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			body[i] = idAlphabet[0]
			continue
		}
		body[i] = idAlphabet[idx.Int64()]
	}
	return prefix + string(body)
}
