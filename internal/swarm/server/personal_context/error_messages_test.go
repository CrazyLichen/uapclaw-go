package personalcontext

import (
	"errors"
	"strings"
	"testing"
)

// LocalizeError reason 直传（Python localize_error reason 参数路径，error_messages.py:149）
func TestLocalizeError_reason直传(t *testing.T) {
	got := LocalizeError(nil, "cn", "duplicate_service")
	if want := messages["duplicate_service"][0]; got != want {
		t.Errorf("LocalizeError cn = %q, want %q", got, want)
	}
	got = LocalizeError(nil, "en", "duplicate_service")
	if want := messages["duplicate_service"][1]; got != want {
		t.Errorf("LocalizeError en = %q, want %q", got, want)
	}
}

// LocalizeError HostError 走 code/status/pcs_reason 三级解析
// （Python error_messages.py:141-144 getattr 分支）
func TestLocalizeError_HostError三级解析(t *testing.T) {
	err := &HostError{StatusName: "CONTEXT_PROACTIVE_STATE_INVALID", Code: "154001", Msg: "x", Reason: ""}
	if got := LocalizeError(err, "cn", ""); got != messages["state_invalid"][0] {
		t.Errorf("按 status 解析失败: %q", got)
	}
	err2 := &HostError{Code: "999999", Reason: "model_invalid"}
	if got := LocalizeError(err2, "cn", ""); got != messages["model_invalid"][0] {
		t.Errorf("按 pcs_reason 解析失败: %q", got)
	}
	if got := LocalizeError(&HostError{Code: "999999"}, "cn", ""); got != messages["unknown"][0] {
		t.Errorf("未知 code 应回落 unknown: %q", got)
	}
}

// LocalizeError 字符串走 BaseError 线格式前缀解析（error_messages.py:145-148）
func TestLocalizeError_线格式字符串(t *testing.T) {
	if got := LocalizeError("[154003] fetch failed", "en", ""); got != messages["fetch_error"][1] {
		t.Errorf("线格式解析失败: %q", got)
	}
	if got := LocalizeError(errors.New("no prefix"), "cn", ""); got != messages["unknown"][0] {
		t.Errorf("无前缀错误应回落 unknown: %q", got)
	}
}

// LocalizeError mapping 分支（error_messages.py:138-140）
func TestLocalizeError_mapping(t *testing.T) {
	value := map[string]any{"code": "BAD_REQUEST"}
	if got := LocalizeError(value, "cn", ""); got != messages["parameter_invalid"][0] {
		t.Errorf("BAD_REQUEST 映射失败: %q", got)
	}
}

// LocalizePayload 只翻译错误键，其余原样拷贝（error_messages.py:155-178）
func TestLocalizePayload_错误键翻译(t *testing.T) {
	payload := map[string]any{
		"service_id": "svc-1",
		"last_error": map[string]any{"code": "154002", "message": "raw", "item_ref": "a.md"},
		"fetch_service_errors": map[string]any{
			"svc-1": map[string]any{"code": "154003", "message": "boom"},
		},
		"nested":                map[string]any{"error": "[154006] timeout", "plain": 42},
		"plain_message_no_code": map[string]any{"message": "keep"},
	}
	out := LocalizePayload(payload, "cn").(map[string]any)
	if out["service_id"] != "svc-1" {
		t.Errorf("普通键应原样拷贝")
	}
	lastError := out["last_error"].(map[string]any)
	if lastError["message"] != messages["file_error"][0] || lastError["item_ref"] != "a.md" {
		t.Errorf("last_error 应保留原键并翻译 message: %v", lastError)
	}
	fetchErrs := out["fetch_service_errors"].(map[string]any)
	if fetchErrs["svc-1"] != messages["fetch_error"][0] {
		t.Errorf("fetch_service_errors 应逐项翻译: %v", fetchErrs)
	}
	nested := out["nested"].(map[string]any)
	if nested["error"] != messages["timeout"][0] || nested["plain"] != 42 {
		t.Errorf("嵌套递归失败: %v", nested)
	}
	if out["plain_message_no_code"] == messages["unknown"][0] {
		t.Errorf("无 code 键的 message 不应走整体翻译")
	}
}

// LocalizePayload 非 map/list 原样返回（error_messages.py:159-160）
func TestLocalizePayload_原样返回(t *testing.T) {
	if got := LocalizePayload(7, "cn"); got != 7 {
		t.Errorf("标量应原样: %v", got)
	}
	list := LocalizePayload([]any{"a", map[string]any{"code": "154000", "message": "x"}}, "en").([]any)
	if list[0] != "a" {
		t.Errorf("列表元素应保留")
	}
	// Python：key == "message" 且 value 含 "code" 时才整体翻译（error_messages.py:174-175）
	if m, ok := list[1].(map[string]any); !ok || m["message"] != messages["config_invalid"][1] {
		t.Errorf("列表内 map 应递归翻译: %v", list[1])
	}
	// 无 message 键时 map 原样保留（Python 无命中分支）
	plain := LocalizePayload(map[string]any{"code": "154000"}, "en").(map[string]any)
	if plain["code"] != "154000" || len(plain) != 1 {
		t.Errorf("无 message 键的 map 应原样: %v", plain)
	}
}

// LogError 脱敏：authorization/cookie/token/bearer/URL（error_messages.py:181-201）
func TestLogError_脱敏(t *testing.T) {
	err := errors.New("failed GET https://api.github.com/user with Authorization: Bearer abc123; token: xyz; Cookie: sid=9")
	LogError(err) // 只验证不 panic + 内部脱敏函数行为
	red := credentialPattern1.ReplaceAllString(err.Error(), "[REDACTED]")
	if strings.Contains(red, "Bearer abc123") {
		t.Errorf("authorization 行未脱敏: %s", red)
	}
	red = bearerPattern.ReplaceAllString(red, "[REDACTED]")
	red = credentialPattern2.ReplaceAllString(red, "[REDACTED]")
	red = urlPattern.ReplaceAllString(red, "[URL REDACTED]")
	if strings.Contains(red, "api.github.com") || strings.Contains(red, "xyz") || strings.Contains(red, "sid=9") {
		t.Errorf("凭据/URL 未脱敏干净: %s", red)
	}
	if !strings.Contains(red, "[URL REDACTED]") {
		t.Errorf("URL 应替换为 [URL REDACTED]: %s", red)
	}
}

// truncate 按字节截断（对应 Python message[:512]）
func TestTruncate(t *testing.T) {
	if got := truncate(strings.Repeat("a", 600), 512); len(got) != 512 {
		t.Errorf("truncate 长度 = %d, want 512", len(got))
	}
	if got := truncate("short", 512); got != "short" {
		t.Errorf("短串应原样")
	}
}

// HostError.Error 线格式前缀（BaseError.__str__ 契约）
func TestHostError_线格式(t *testing.T) {
	err := &HostError{Code: "154000", Msg: "bad config"}
	if got := err.Error(); !strings.HasPrefix(got, "[154000] bad config") {
		t.Errorf("Error() = %q, want [154000] 前缀", got)
	}
	err = &HostError{Msg: "no code", Cause: errors.New("inner")}
	if got := err.Error(); !strings.Contains(got, "inner") {
		t.Errorf("cause 应包含: %q", got)
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("Unwrap 应返回 cause")
	}
}
