package session

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// titleMaxLen 自动标题截取长度，对齐 Python _TITLE_MAX_LEN = 50
	titleMaxLen = 50
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件，对齐项目规范 server 层使用 ComponentAgentServer
	logComponent = logger.ComponentAgentServer
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AutoTitle 从首条用户消息自动生成会话标题。
//
// 对齐 Python _auto_title(content): 截取前 50 字符，换行替换为空格
func AutoTitle(content string) string {
	title := strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	if len(title) > titleMaxLen {
		title = title[:titleMaxLen] + "..."
	}
	return title
}

// SerializeValue 递归序列化值，确保 JSON 可序列化。
//
// 对齐 Python _serialize_value(obj): time.Time→ISO string, map/slice 递归
func SerializeValue(obj any) any {
	switch v := obj.(type) {
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, val := range v {
			result[k] = SerializeValue(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = SerializeValue(val)
		}
		return result
	default:
		return v
	}
}

// DeepCopyMap 深拷贝 map，对齐 Python copy.deepcopy()。
func DeepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = DeepCopyMap(val)
		default:
			dst[k] = v
		}
	}
	return dst
}

// DerefStr 解引用字符串指针，nil 时返回 fallback。
func DerefStr(s *string, fallback string) string {
	if s != nil {
		return *s
	}
	return fallback
}

// PtrStr 字符串→指针辅助函数，供联动调用构造 *string 参数。
func PtrStr(s string) *string {
	return &s
}

// MakeSessionID 生成会话标识，对齐 Python _make_session_id。
//
// 格式：sess_{hex_timestamp}_{6_random_hex}
func MakeSessionID() string {
	ts := fmt.Sprintf("%x", time.Now().UnixMilli())
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("sess_%s_%x", ts, suffix)
}

// NormalizeSessionID 规范化 sessionID，空串→"default"。
func NormalizeSessionID(sessionID string) string {
	sid := sessionID
	if sid == "" {
		return "default"
	}
	return sid
}

// CurrentTimestamp 返回当前 UTC 时间戳（秒），对齐 Python _current_timestamp。
func CurrentTimestamp() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}
