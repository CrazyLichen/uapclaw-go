// headers_helper 包提供 LLM 请求头构建辅助函数。
//
// 包含 HTTP 头清洗、大小写不敏感合并、配置级/请求级 headers 组装等功能。
//
// 对应 Python:
//   - openjiuwen/core/common/utils/header_utils.py (sanitize_headers, PROTECTED_HEADERS)
//   - openjiuwen/core/foundation/llm/headers_helper.py (merge/build 函数)
package headers_helper

import (
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ProtectedHeaders 受保护的 HTTP 头名称（小写）。
//
// 这些头部由 HTTP 客户端/传输层自动管理，不应由用户手动设置。
// 对应 Python: openjiuwen/core/common/utils/header_utils.py (PROTECTED_HEADERS)
var ProtectedHeaders = map[string]bool{
	"host":              true,
	"content-length":    true,
	"transfer-encoding": true,
	"connection":        true,
	"authorization":     true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SanitizeHeaders 清洗 HTTP 头，移除受保护头部和空值。
//
// 接受 map[string]string 输入（来自 ModelClientConfig.CustomHeaders
// 和 InvokeParams.CustomHeaders），返回清洗后的 map[string]string。
//
// 处理规则（对齐 Python: sanitize_headers()）：
//  1. 去除空键；去除键首尾空白后跳过空白键
//  2. 跳过空值
//  3. 阻止受保护头部（host, content-length, transfer-encoding, connection, authorization）
//
// 空输入或全部被过滤时返回空 map（map[string]string{}），不返回 nil。
// 这与 Python 的行为一致（返回空 dict），也避免调用方 nil 检查。
//
// 对应 Python: openjiuwen/core/common/utils/header_utils.py (sanitize_headers)
func SanitizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	sanitized := make(map[string]string, len(headers))
	for key, val := range headers {
		// 跳过空键
		if key == "" {
			continue
		}

		// 去除键首尾空白
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		// 跳过受保护头部
		if ProtectedHeaders[strings.ToLower(key)] {
			continue
		}

		// 跳过空值
		if strings.TrimSpace(val) == "" {
			continue
		}

		sanitized[key] = val
	}

	return sanitized
}

// IsProtectedHeader 判断给定 header 名称是否属于受保护头部（大小写不敏感）。
//
// 对应 Python: `key.lower() in PROTECTED_HEADERS` 检查
func IsProtectedHeader(name string) bool {
	return ProtectedHeaders[strings.ToLower(name)]
}

// BuildBaseHeaders 从自定义请求头构建配置级基础 headers。
//
// 是 SanitizeHeaders 的薄包装，语义更清晰：
// 在客户端构造时调用，将用户配置的 CustomHeaders 清洗后作为 baseHeaders 存储。
//
// 对应 Python: build_base_headers()
func BuildBaseHeaders(customHeaders map[string]string) map[string]string {
	return SanitizeHeaders(customHeaders)
}

// MergeHeadersCaseInsensitive 大小写不敏感合并 headers。
//
// 将 newHeaders 合并到 baseHeaders 中，同名 key 大小写不敏感匹配，
// 保留 baseHeaders 中首次出现的 key 大小写（即 base 的 casing 优先）。
// newHeaders 的值覆盖 baseHeaders 的值。
//
// 注意：本函数会修改 baseHeaders（原地合并），与 Python 行为一致。
// 如果需要保留 baseHeaders 原始数据，调用前需自行拷贝。
//
// 对应 Python: merge_headers_case_insensitive()
func MergeHeadersCaseInsensitive(baseHeaders map[string]string, newHeaders map[string]string) {
	if len(newHeaders) == 0 {
		return
	}

	// 构建大小写不敏感索引
	normalizedToKey := make(map[string]string, len(baseHeaders))
	for key := range baseHeaders {
		normalizedToKey[strings.ToLower(key)] = key
	}

	// 合并 newHeaders
	for key, val := range newHeaders {
		normKey := strings.ToLower(key)
		if existingKey, ok := normalizedToKey[normKey]; ok {
			// 同名覆盖，保留原始 key 大小写
			baseHeaders[existingKey] = val
		} else {
			baseHeaders[key] = val
			normalizedToKey[normKey] = key
		}
	}
}

// MergeRequestHeaders 合并配置级和请求级 headers。
//
// 拷贝 baseHeaders 后，将 requestCustomHeaders 清洗并合并到拷贝上。
// 请求级优先，同名 key 大小写不敏感匹配，保留首次出现的 key 大小写。
//
// 对应 Python: merge_request_headers()
func MergeRequestHeaders(baseHeaders map[string]string, requestCustomHeaders map[string]string) map[string]string {
	// 拷贝 base
	result := make(map[string]string, len(baseHeaders))
	for k, v := range baseHeaders {
		result[k] = v
	}

	// 清洗请求级 headers
	sanitized := SanitizeHeaders(requestCustomHeaders)

	// 合并
	MergeHeadersCaseInsensitive(result, sanitized)

	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
