package etcd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EtcdKv 一条 etcd 键值记录。
// Python: client.py:60-66 EtcdKv
type EtcdKv struct {
	// Key 键（原始字节）
	Key []byte
	// Value 值（原始字节）
	Value []byte
	// ModRevision 修改版本号
	ModRevision int64
	// CreateRevision 创建版本号
	CreateRevision int64
	// Version 版本号
	Version int64
}

// EtcdRangeResult 一次区间读取的结果。
// Python: client.py:69-72 EtcdRangeResult
type EtcdRangeResult struct {
	// Kvs 命中的键值对
	Kvs []EtcdKv
	// Revision 本次读取时的集群版本
	Revision int64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PrefixRangeEnd 计算前缀扫描的 range_end（递增最后一个非 0xFF 字节）。
// Python: client.py:38-45 prefix_range_end
//
// 全为 0xFF 时返回 "\x00"（etcd 约定：range_end 为 0 表示到末尾）。
func PrefixRangeEnd(prefix []byte) []byte {
	buf := make([]byte, len(prefix))
	copy(buf, prefix)
	for index := len(buf) - 1; index >= 0; index-- {
		if buf[index] < 0xFF {
			buf[index]++
			return buf[:index+1]
		}
	}
	return []byte{0x00}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// b64 编码为 base64 字符串。
// Python: client.py:28-29 _b64
func b64(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

// unb64 base64 解码，空串返回空字节。
// Python: client.py:32-35 _unb64
func unb64(text string) []byte {
	if text == "" {
		return []byte{}
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return []byte{}
	}
	return decoded
}

// parseKv 把 etct 响应里的单条 kv 解析为 EtcdKv。
// Python: client.py:75-87 _parse_kv
//
// 字段类型不合法（非对象、数字不可解析）时返回 ok=false，跳过该条。
func parseKv(item any) (EtcdKv, bool) {
	mapping, ok := item.(map[string]any)
	if !ok {
		return EtcdKv{}, false
	}
	key, _ := mapping["key"].(string)
	value, _ := mapping["value"].(string)
	modRevision, okMod := parseInt64Field(mapping["mod_revision"])
	createRevision, okCreate := parseInt64Field(mapping["create_revision"])
	version, okVersion := parseInt64Field(mapping["version"])
	if !okMod || !okCreate || !okVersion {
		return EtcdKv{}, false
	}
	return EtcdKv{
		Key:            unb64(key),
		Value:          unb64(value),
		ModRevision:    modRevision,
		CreateRevision: createRevision,
		Version:        version,
	}, true
}

// parseInt64Field 解析版本号字段：支持 JSON 数字、字符串数字与缺省（视为 0）。
// Python: int(item.get("mod_revision") or 0)
func parseInt64Field(value any) (int64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		if typed == "" {
			return 0, true
		}
		var parsed int64
		if _, err := fmt.Sscan(typed, &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// headerRevision 从响应体取集群版本。
// Python: client.py:153-161 _header_revision
func headerRevision(payload map[string]any) int64 {
	header, ok := payload["header"].(map[string]any)
	if !ok {
		return 0
	}
	revision, ok := parseInt64Field(header["revision"])
	if !ok {
		return 0
	}
	return revision
}
