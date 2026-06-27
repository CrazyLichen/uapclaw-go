package e2a

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── IdentityOrigin 枚举测试 ────────────────────────────

// TestIdentityOrigin_枚举值 验证 4 个枚举值
func TestIdentityOrigin_枚举值(t *testing.T) {
	all := AllIdentityOrigins()
	if len(all) != 4 {
		t.Fatalf("期望 4 个 IdentityOrigin，实际 %d", len(all))
	}
	expected := map[IdentityOrigin]bool{
		IdentityOriginSystem:  true,
		IdentityOriginUser:    true,
		IdentityOriginAgent:   true,
		IdentityOriginService: true,
	}
	for _, o := range all {
		if !expected[o] {
			t.Errorf("意外的 IdentityOrigin 值: %q", o)
		}
	}
}

// TestParseIdentityOrigin_合法 验证合法字符串解析
func TestParseIdentityOrigin_合法(t *testing.T) {
	tests := []struct {
		input string
		want  IdentityOrigin
	}{
		{"system", IdentityOriginSystem},
		{"user", IdentityOriginUser},
		{"agent", IdentityOriginAgent},
		{"service", IdentityOriginService},
	}
	for _, tt := range tests {
		got, err := ParseIdentityOrigin(tt.input)
		if err != nil {
			t.Errorf("ParseIdentityOrigin(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseIdentityOrigin(%q) = %q, 期望 %q", tt.input, got, tt.want)
		}
	}
}

// TestParseIdentityOrigin_非法 验证非法字符串返回错误
func TestParseIdentityOrigin_非法(t *testing.T) {
	_, err := ParseIdentityOrigin("invalid")
	if err == nil {
		t.Error("ParseIdentityOrigin(\"invalid\") 期望返回错误，实际返回 nil")
	}
	_, err = ParseIdentityOrigin("")
	if err == nil {
		t.Error("ParseIdentityOrigin(\"\") 期望返回错误，实际返回 nil")
	}
}

// TestIsValidIdentityOrigin 验证 IsValidIdentityOrigin 判断
func TestIsValidIdentityOrigin(t *testing.T) {
	for _, s := range []string{"system", "user", "agent", "service"} {
		if !IsValidIdentityOrigin(s) {
			t.Errorf("IsValidIdentityOrigin(%q) 期望 true", s)
		}
	}
	if IsValidIdentityOrigin("invalid") {
		t.Error("IsValidIdentityOrigin(\"invalid\") 期望 false")
	}
}

// TestIdentityOrigin_String 验证 String 方法
func TestIdentityOrigin_String(t *testing.T) {
	if IdentityOriginUser.String() != "user" {
		t.Errorf("IdentityOriginUser.String() = %q, 期望 %q", IdentityOriginUser.String(), "user")
	}
}

// TestIdentityOrigin_GoString 验证 GoString 方法
func TestIdentityOrigin_GoString(t *testing.T) {
	got := IdentityOriginUser.GoString()
	want := `e2a.IdentityOrigin("user")`
	if got != want {
		t.Errorf("IdentityOriginUser.GoString() = %q, 期望 %q", got, want)
	}
}

// ──────────────────────────── E2AProvenance 测试 ────────────────────────────

// TestNewE2AProvenance_默认值 验证工厂函数默认值
func TestNewE2AProvenance_默认值(t *testing.T) {
	p := NewE2AProvenance()
	if p.SourceProtocol != E2ASourceProtocolE2A {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", E2ASourceProtocolE2A, p.SourceProtocol)
	}
	if p.Converter != "" {
		t.Errorf("Converter 期望空串，实际 %q", p.Converter)
	}
	if p.ConvertedAt != "" {
		t.Errorf("ConvertedAt 期望空串，实际 %q", p.ConvertedAt)
	}
}

// TestE2AProvenance_JSON序列化往返 验证 JSON 序列化往返一致性
func TestE2AProvenance_JSON序列化往返(t *testing.T) {
	original := &E2AProvenance{
		SourceProtocol: E2ASourceProtocolACP,
		Converter:      "acp_adapter",
		ConvertedAt:    "2026-01-01T00:00:00Z",
		Details:        map[string]any{"key": "value"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var decoded E2AProvenance
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.SourceProtocol != original.SourceProtocol {
		t.Errorf("SourceProtocol 期望 %q，实际 %q", original.SourceProtocol, decoded.SourceProtocol)
	}
	if decoded.Converter != original.Converter {
		t.Errorf("Converter 期望 %q，实际 %q", original.Converter, decoded.Converter)
	}
	if decoded.ConvertedAt != original.ConvertedAt {
		t.Errorf("ConvertedAt 期望 %q，实际 %q", original.ConvertedAt, decoded.ConvertedAt)
	}
}

// ──────────────────────────── E2AFileRef 测试 ────────────────────────────

// TestNewE2AFileRef_默认值 验证工厂函数默认值
func TestNewE2AFileRef_默认值(t *testing.T) {
	f := NewE2AFileRef("file:///tmp/test.txt")
	if f.URI != "file:///tmp/test.txt" {
		t.Errorf("URI 期望 %q，实际 %q", "file:///tmp/test.txt", f.URI)
	}
	if f.Name != "" {
		t.Errorf("Name 期望空串，实际 %q", f.Name)
	}
}

// TestE2AFileRef_JSON序列化往返 验证 JSON 序列化往返一致性（含 _meta tag）
func TestE2AFileRef_JSON序列化往返(t *testing.T) {
	original := &E2AFileRef{
		URI:      "file:///tmp/test.txt",
		Name:     "test.txt",
		MimeType: "text/plain",
		Size:     1024,
		Meta:     map[string]any{"source": "upload"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	// 验证 _meta JSON tag
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map 失败: %v", err)
	}
	if _, ok := raw["_meta"]; !ok {
		t.Error("JSON 输出中缺少 _meta 键")
	}
	var decoded E2AFileRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.URI != original.URI {
		t.Errorf("URI 期望 %q，实际 %q", original.URI, decoded.URI)
	}
	if decoded.Size != original.Size {
		t.Errorf("Size 期望 %d，实际 %d", original.Size, decoded.Size)
	}
}

// ──────────────────────────── E2AAuth 测试 ────────────────────────────

// TestNewE2AAuth_默认值 验证工厂函数默认值
func TestNewE2AAuth_默认值(t *testing.T) {
	a := NewE2AAuth()
	if a.MethodID != "" {
		t.Errorf("MethodID 期望空串，实际 %q", a.MethodID)
	}
	if a.BearerToken != "" {
		t.Errorf("BearerToken 期望空串，实际 %q", a.BearerToken)
	}
}

// TestE2AAuth_JSON序列化往返 验证 JSON 序列化往返一致性（含 _meta tag）
func TestE2AAuth_JSON序列化往返(t *testing.T) {
	original := &E2AAuth{
		MethodID:      "oauth2",
		BearerToken:   "token123",
		APIKeyRef:     "key_ref",
		CredentialRef: "cred_ref",
		ExtraHeaders:  map[string]string{"X-Custom": "value"},
		Meta:          map[string]any{"scope": "read"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	// 验证 _meta JSON tag
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map 失败: %v", err)
	}
	if _, ok := raw["_meta"]; !ok {
		t.Error("JSON 输出中缺少 _meta 键")
	}
	var decoded E2AAuth
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.MethodID != original.MethodID {
		t.Errorf("MethodID 期望 %q，实际 %q", original.MethodID, decoded.MethodID)
	}
	if decoded.BearerToken != original.BearerToken {
		t.Errorf("BearerToken 期望 %q，实际 %q", original.BearerToken, decoded.BearerToken)
	}
}
