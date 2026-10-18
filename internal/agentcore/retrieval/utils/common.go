package utils

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMilvusAlias 生成 Milvus 连接别名。
//
// 对齐 Python retrieval/utils/common.py (create_milvus_alias)：
//   - alias 非空时直接返回
//   - 否则基于 URI + 认证信息生成：token 非空用 MD5(token)，否则用 user 或 "noauth"
//   - 格式：kb-{uri}-{auth_info}，空元素跳过
func CreateMilvusAlias(alias, uri, token string, user ...string) string {
	if alias != "" {
		return alias
	}
	// 对齐 Python: auth_info = user or "noauth"
	authInfo := "noauth"
	if len(user) > 0 && user[0] != "" {
		authInfo = user[0]
	}
	// 对齐 Python: if token: md5(token) 作为 auth_info
	if token != "" {
		md5sum := md5.Sum([]byte(token)) // #nosec G401 — 对齐 Python usedforsecurity=False
		authInfo = hex.EncodeToString(md5sum[:])
	}
	// 对齐 Python: "-".join(elem for elem in ["kb", uri, auth_info] if elem)
	elems := make([]string, 0, 3)
	for _, e := range []string{"kb", uri, authInfo} {
		if e != "" {
			elems = append(elems, e)
		}
	}
	return strings.Join(elems, "-")
}

// Deduplicate 去重并保留首次出现顺序。
//
// 对齐 Python retrieval/utils/common.py (deduplicate)：
// 按 key 函数计算键，重复项只保留首次出现。
func Deduplicate[T any, K comparable](data []T, key func(T) K) []T {
	seen := make(map[K]struct{})
	result := make([]T, 0, len(data))
	for _, item := range data {
		k := key(item)
		if _, exists := seen[k]; !exists {
			seen[k] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
