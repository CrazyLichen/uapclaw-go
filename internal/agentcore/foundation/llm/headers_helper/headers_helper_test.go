package headers_helper

import (
	"testing"
)

// ──────────────────────────── SanitizeHeaders 测试 ────────────────────────────

func TestSanitizeHeaders_Nil输入(t *testing.T) {
	// nil 输入应返回空 map（不是 nil）
	result := SanitizeHeaders(nil)
	if result == nil {
		t.Error("SanitizeHeaders(nil) 不应返回 nil，应返回空 map")
	}
	if len(result) != 0 {
		t.Errorf("SanitizeHeaders(nil) = %v, 期望空 map", result)
	}
}

func TestSanitizeHeaders_空输入(t *testing.T) {
	// 空字典应返回空 map
	result := SanitizeHeaders(map[string]any{})
	if result == nil {
		t.Error("SanitizeHeaders(empty) 不应返回 nil，应返回空 map")
	}
	if len(result) != 0 {
		t.Errorf("SanitizeHeaders(empty) = %v, 期望空 map", result)
	}
}

func TestSanitizeHeaders_受保护头部被过滤(t *testing.T) {
	// 受保护头部应被过滤（与 Python 对齐的 5 项列表）
	input := map[string]any{
		"Authorization":     "Bearer token",
		"Host":              "api.openai.com",
		"Content-Length":    "100",
		"Transfer-Encoding": "chunked",
		"Connection":        "keep-alive",
		"X-Custom":          "value",
	}
	result := SanitizeHeaders(input)

	// 全部 5 个受保护头部都应被过滤
	protectedKeys := []string{"Authorization", "Host", "Content-Length", "Transfer-Encoding", "Connection"}
	for _, key := range protectedKeys {
		if _, ok := result[key]; ok {
			t.Errorf("%s 不应出现在结果中（属于受保护头部）", key)
		}
	}

	// 自定义头部应保留
	if result["X-Custom"] != "value" {
		t.Errorf("X-Custom = %q, 期望 %q", result["X-Custom"], "value")
	}
}

func TestSanitizeHeaders_ContentType不受保护(t *testing.T) {
	// content-type 不属于受保护头部（与 Python 对齐）
	input := map[string]any{
		"Content-Type": "application/json",
	}
	result := SanitizeHeaders(input)
	if result["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q, 期望 %q", result["Content-Type"], "application/json")
	}
}

func TestSanitizeHeaders_正常头部(t *testing.T) {
	// 正常头部应被保留，值转为字符串
	input := map[string]any{
		"X-Request-ID":  "abc-123",
		"X-Retry-Count": 3,
	}
	result := SanitizeHeaders(input)
	if len(result) != 2 {
		t.Errorf("len(result) = %d, 期望 2", len(result))
	}
	if result["X-Request-ID"] != "abc-123" {
		t.Errorf("X-Request-ID = %q, 期望 %q", result["X-Request-ID"], "abc-123")
	}
	if result["X-Retry-Count"] != "3" {
		t.Errorf("X-Retry-Count = %q, 期望 %q", result["X-Retry-Count"], "3")
	}
}

func TestSanitizeHeaders_空值被过滤(t *testing.T) {
	// 空值头部应被过滤
	input := map[string]any{
		"X-Empty": "",
		"X-Valid": "ok",
	}
	result := SanitizeHeaders(input)
	if _, ok := result["X-Empty"]; ok {
		t.Error("空值头部应被过滤")
	}
	if result["X-Valid"] != "ok" {
		t.Errorf("X-Valid = %q, 期望 %q", result["X-Valid"], "ok")
	}
}

func TestSanitizeHeaders_Nil值被过滤(t *testing.T) {
	// nil 值头部应被过滤
	input := map[string]any{
		"X-None":  nil,
		"X-Valid": "ok",
	}
	result := SanitizeHeaders(input)
	if _, ok := result["X-None"]; ok {
		t.Error("nil 值头部应被过滤")
	}
	if result["X-Valid"] != "ok" {
		t.Errorf("X-Valid = %q, 期望 %q", result["X-Valid"], "ok")
	}
}

func TestSanitizeHeaders_空白键被过滤(t *testing.T) {
	// 空白键头部应被过滤
	input := map[string]any{
		"":        "empty-key",
		"  ":      "whitespace-key",
		"X-Valid": "ok",
	}
	result := SanitizeHeaders(input)
	if len(result) != 1 {
		t.Errorf("len(result) = %d, 期望 1", len(result))
	}
	if result["X-Valid"] != "ok" {
		t.Errorf("X-Valid = %q, 期望 %q", result["X-Valid"], "ok")
	}
}

func TestSanitizeHeaders_纯空白值被过滤(t *testing.T) {
	// 仅含空白的值应被过滤
	input := map[string]any{
		"X-Space": "  ",
		"X-Valid": "ok",
	}
	result := SanitizeHeaders(input)
	if _, ok := result["X-Space"]; ok {
		t.Error("纯空白值应被过滤")
	}
	if result["X-Valid"] != "ok" {
		t.Errorf("X-Valid = %q, 期望 %q", result["X-Valid"], "ok")
	}
}

func TestSanitizeHeaders_全部被过滤返回空map(t *testing.T) {
	// 所有头部都被过滤时应返回空 map（不是 nil）
	input := map[string]any{
		"Authorization": "Bearer token",
		"Host":          "api.openai.com",
	}
	result := SanitizeHeaders(input)
	if result == nil {
		t.Error("全部被过滤时不应返回 nil，应返回空 map")
	}
	if len(result) != 0 {
		t.Errorf("SanitizeHeaders(all protected) = %v, 期望空 map", result)
	}
}

func TestSanitizeHeaders_键首尾空白被去除(t *testing.T) {
	// 键首尾空白应被去除
	input := map[string]any{
		"  X-Trimmed  ": "value",
	}
	result := SanitizeHeaders(input)
	if result["X-Trimmed"] != "value" {
		t.Errorf("X-Trimmed = %q, 期望 %q", result["X-Trimmed"], "value")
	}
}

// ──────────────────────────── BuildBaseHeaders 测试 ────────────────────────────

func TestBuildBaseHeaders_Nil输入(t *testing.T) {
	// nil 输入应返回空 map
	result := BuildBaseHeaders(nil)
	if result == nil {
		t.Error("BuildBaseHeaders(nil) 不应返回 nil，应返回空 map")
	}
	if len(result) != 0 {
		t.Errorf("BuildBaseHeaders(nil) = %v, 期望空 map", result)
	}
}

func TestBuildBaseHeaders_正常输入(t *testing.T) {
	// 正常输入应与 SanitizeHeaders 结果一致
	input := map[string]any{
		"X-Token": "abc",
		"X-Count": 42,
		"Host":    "should-be-filtered",
	}
	result := BuildBaseHeaders(input)
	if len(result) != 2 {
		t.Errorf("len(result) = %d, 期望 2", len(result))
	}
	if result["X-Token"] != "abc" {
		t.Errorf("X-Token = %q, 期望 %q", result["X-Token"], "abc")
	}
	if result["X-Count"] != "42" {
		t.Errorf("X-Count = %q, 期望 %q", result["X-Count"], "42")
	}
}

// ──────────────────────────── MergeHeadersCaseInsensitive 测试 ────────────────────────────

func TestMergeHeadersCaseInsensitive_仅base(t *testing.T) {
	// 只有 base headers，new 为空
	base := map[string]string{
		"X-Base": "base-value",
	}
	MergeHeadersCaseInsensitive(base, nil)
	if base["X-Base"] != "base-value" {
		t.Errorf("X-Base = %q, 期望 %q", base["X-Base"], "base-value")
	}
}

func TestMergeHeadersCaseInsensitive_请求级覆盖配置级(t *testing.T) {
	// 请求级覆盖配置级
	base := map[string]string{
		"X-Header": "base-value",
	}
	new := map[string]string{
		"X-Header": "request-value",
	}
	MergeHeadersCaseInsensitive(base, new)
	if base["X-Header"] != "request-value" {
		t.Errorf("X-Header = %q, 期望 %q", base["X-Header"], "request-value")
	}
}

func TestMergeHeadersCaseInsensitive_大小写不敏感匹配(t *testing.T) {
	// 大小写不敏感合并，保留 base 的 key 大小写
	base := map[string]string{
		"X-Custom-Header": "base",
	}
	new := map[string]string{
		"x-custom-header": "request",
	}
	MergeHeadersCaseInsensitive(base, new)
	// 保留 base 的 key 大小写，但值被 new 覆盖
	if base["X-Custom-Header"] != "request" {
		t.Errorf("X-Custom-Header = %q, 期望 %q", base["X-Custom-Header"], "request")
	}
	// 不应有小写版本的 key
	if _, ok := base["x-custom-header"]; ok {
		t.Error("不应存在小写版本的 key")
	}
}

func TestMergeHeadersCaseInsensitive_两者均为空(t *testing.T) {
	// 两者均为空
	base := map[string]string{}
	MergeHeadersCaseInsensitive(base, nil)
	if len(base) != 0 {
		t.Errorf("MergeHeadersCaseInsensitive(empty, nil) = %v, 期望空 map", base)
	}
}

func TestMergeHeadersCaseInsensitive_新增key(t *testing.T) {
	// new 中新增 key
	base := map[string]string{
		"X-Base": "base",
	}
	new := map[string]string{
		"X-New": "new-value",
	}
	MergeHeadersCaseInsensitive(base, new)
	if base["X-Base"] != "base" {
		t.Errorf("X-Base = %q, 期望 %q", base["X-Base"], "base")
	}
	if base["X-New"] != "new-value" {
		t.Errorf("X-New = %q, 期望 %q", base["X-New"], "new-value")
	}
}

func TestMergeHeadersCaseInsensitive_原地修改(t *testing.T) {
	// 验证原地修改 base（与 Python 行为一致）
	base := map[string]string{
		"X-Base": "base",
	}
	new := map[string]string{
		"X-Base": "updated",
	}
	MergeHeadersCaseInsensitive(base, new)
	if base["X-Base"] != "updated" {
		t.Errorf("X-Base = %q, 期望 %q", base["X-Base"], "updated")
	}
}

// ──────────────────────────── MergeRequestHeaders 测试 ────────────────────────────

func TestMergeRequestHeaders_配置级和请求级(t *testing.T) {
	// base + request 合并
	base := map[string]string{
		"X-Base": "base-val",
	}
	result := MergeRequestHeaders(base, map[string]any{"X-Request": "req-val"})
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

func TestMergeRequestHeaders_请求级覆盖配置级(t *testing.T) {
	// 请求级优先覆盖配置级
	base := map[string]string{
		"X-Token": "base-token",
	}
	result := MergeRequestHeaders(base, map[string]any{"X-Token": "req-token"})
	if result["X-Token"] != "req-token" {
		t.Errorf("X-Token = %q, 期望 %q", result["X-Token"], "req-token")
	}
}

func TestMergeRequestHeaders_大小写不敏感覆盖(t *testing.T) {
	// 大小写不敏感覆盖，保留 base 的 key 大小写
	base := map[string]string{
		"X-Tenant": "tenant-cfg",
		"UserID":   "user-cfg",
	}
	result := MergeRequestHeaders(base, map[string]any{
		"x-tenant":      "tenant-req",
		"userid":        "user-req",
		"Connection":    "blocked",
		"Authorization": "Bearer blocked",
	})
	// 保留 base 的 key 大小写，值被覆盖
	if result["X-Tenant"] != "tenant-req" {
		t.Errorf("X-Tenant = %q, 期望 %q", result["X-Tenant"], "tenant-req")
	}
	if result["UserID"] != "user-req" {
		t.Errorf("UserID = %q, 期望 %q", result["UserID"], "user-req")
	}
	// 受保护头部应被过滤
	if _, ok := result["Connection"]; ok {
		t.Error("Connection 不应出现在结果中（受保护头部）")
	}
	if _, ok := result["Authorization"]; ok {
		t.Error("Authorization 不应出现在结果中（受保护头部）")
	}
}

func TestMergeRequestHeaders_Nil请求头(t *testing.T) {
	// nil 请求头，只返回 base 拷贝
	base := map[string]string{
		"X-Base": "base-val",
	}
	result := MergeRequestHeaders(base, nil)
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if len(result) != 1 {
		t.Errorf("len(result) = %d, 期望 1", len(result))
	}
}

func TestMergeRequestHeaders_空配置头(t *testing.T) {
	// 空 base，只有请求头
	result := MergeRequestHeaders(nil, map[string]any{"X-Request": "req-val"})
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

func TestMergeRequestHeaders_不修改原始base(t *testing.T) {
	// MergeRequestHeaders 应拷贝 base，不修改原始 base
	base := map[string]string{
		"X-Base": "base-val",
	}
	_ = MergeRequestHeaders(base, map[string]any{"X-New": "new-val"})
	if _, ok := base["X-New"]; ok {
		t.Error("MergeRequestHeaders 不应修改原始 base")
	}
	if base["X-Base"] != "base-val" {
		t.Errorf("原始 base 的 X-Base = %q, 期望 %q", base["X-Base"], "base-val")
	}
}

func TestMergeRequestHeaders_两者均为空(t *testing.T) {
	// 两者均为空
	result := MergeRequestHeaders(nil, nil)
	if result == nil {
		t.Error("MergeRequestHeaders(nil, nil) 不应返回 nil，应返回空 map")
	}
	if len(result) != 0 {
		t.Errorf("MergeRequestHeaders(nil, nil) = %v, 期望空 map", result)
	}
}

// ──────────────────────────── ProtectedHeaders 测试 ────────────────────────────

func TestProtectedHeaders_包含全部5项(t *testing.T) {
	// 与 Python 对齐的 5 项受保护头部
	expected := []string{"host", "content-length", "transfer-encoding", "connection", "authorization"}
	for _, h := range expected {
		if !ProtectedHeaders[h] {
			t.Errorf("ProtectedHeaders[%q] 应为 true", h)
		}
	}
	if len(ProtectedHeaders) != 5 {
		t.Errorf("len(ProtectedHeaders) = %d, 期望 5", len(ProtectedHeaders))
	}
}

func TestProtectedHeaders_不含ContentType(t *testing.T) {
	// content-type 不属于受保护头部（与 Python 对齐）
	if ProtectedHeaders["content-type"] {
		t.Error("content-type 不应是受保护头部")
	}
}

func TestProtectedHeaders_键均为小写(t *testing.T) {
	// ProtectedHeaders 的键应为小写
	if ProtectedHeaders["Host"] {
		t.Error("ProtectedHeaders 键应为小写，'Host' 不应存在")
	}
}

// ──────────────────────────── IsProtectedHeader 测试 ────────────────────────────

func TestIsProtectedHeader_受保护头部(t *testing.T) {
	// 受保护头部应返回 true（大小写不敏感）
	if !IsProtectedHeader("authorization") {
		t.Error("IsProtectedHeader(\"authorization\") 应返回 true")
	}
	if !IsProtectedHeader("Authorization") {
		t.Error("IsProtectedHeader(\"Authorization\") 应返回 true")
	}
	if !IsProtectedHeader("HOST") {
		t.Error("IsProtectedHeader(\"HOST\") 应返回 true")
	}
}

func TestIsProtectedHeader_非受保护头部(t *testing.T) {
	// 非受保护头部应返回 false
	if IsProtectedHeader("content-type") {
		t.Error("IsProtectedHeader(\"content-type\") 应返回 false")
	}
	if IsProtectedHeader("x-custom") {
		t.Error("IsProtectedHeader(\"x-custom\") 应返回 false")
	}
}
