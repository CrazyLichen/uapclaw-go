// utils 包提供通用工具函数。
//
// hash.go 实现哈希键生成工具，用于连接池配置的唯一标识。
// 对应 Python：openjiuwen/core/common/utils/hash_util.py

package utils

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateKey 根据密钥、地址和提供商生成 SHA256 哈希键。
//
// 对应 Python: generate_key(api_key, api_base, model_provider)
// 将参数排序后拼接，计算 SHA256 摘要，返回十六进制字符串。
// 用于连接池等场景的配置唯一标识——相同参数组合生成相同 key。
func GenerateKey(apiKey, apiBase, modelProvider string) string {
	parts := []string{apiKey, apiBase, modelProvider}
	sort.Strings(parts)
	combined := strings.Join(parts, "")
	hash := sha256.Sum256([]byte(combined))
	return fmt.Sprintf("%x", hash)
}
