//go:build integration

package headers_helper

import (
	"testing"

	"github.com/stretchr/testify/suite"
	headers "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/headers_helper"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HeadersHelperSuite 测试 headers_helper 包核心功能。
//
// 覆盖：
//   - BuildBaseHeaders 配置级基础 headers 构建
//   - MergeHeadersCaseInsensitive 大小写不敏感合并
//   - MergeRequestHeaders 配置级 + 请求级合并
//   - SanitizeHeaders 清洗受保护头部和空值
//   - IsProtectedHeader 头部保护检查
//   - ProtectedHeaders 受保护头部集合
//   - 空/nil 输入安全处理
//   - ModelClientConfig CustomHeaders 交互验证
//
// 对齐 Python: tests/system_tests/foundation/llm/test_custom_headers_system.py
type HeadersHelperSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHeadersHelperSuite(t *testing.T) {
	suite.Run(t, new(HeadersHelperSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ---------------------------------------------------------------------------
// BuildBaseHeaders（对齐 Python: build_base_headers）
// ---------------------------------------------------------------------------

// TestBuildBaseHeaders_正常输入 测试正常自定义头部构建。
// 对齐 Python: test_model_invoke_injects_sanitized_config_headers
func (s *HeadersHelperSuite) TestBuildBaseHeaders_正常输入() {
	result := headers.BuildBaseHeaders(map[string]string{
		"Token":  "token-a",
		"UserID": "user-a",
	})
	s.Equal("token-a", result["Token"], "Token 应保留")
	s.Equal("user-a", result["UserID"], "UserID 应保留")
	s.Len(result, 2, "应保留 2 个头部")
}

// TestBuildBaseHeaders_过滤受保护头部 测试受保护头部被过滤。
// 对齐 Python: test_model_invoke_injects_sanitized_config_headers (Authorization 被过滤)
func (s *HeadersHelperSuite) TestBuildBaseHeaders_过滤受保护头部() {
	result := headers.BuildBaseHeaders(map[string]string{
		"Token":         "token-a",
		"Authorization": "blocked",
	})
	s.Equal("token-a", result["Token"], "Token 应保留")
	s.Len(result, 1, "Authorization 应被过滤，只保留 1 个头部")
}

// TestBuildBaseHeaders_过滤空值头部 测试空值头部被过滤。
func (s *HeadersHelperSuite) TestBuildBaseHeaders_过滤空值头部() {
	result := headers.BuildBaseHeaders(map[string]string{
		"Token":   "token-a",
		"X-Empty": "",
	})
	s.Equal("token-a", result["Token"], "Token 应保留")
	s.Len(result, 1, "空值头部应被过滤，只保留 1 个头部")
}

// TestBuildBaseHeaders_过滤纯空白值 测试纯空白值被过滤。
func (s *HeadersHelperSuite) TestBuildBaseHeaders_过滤纯空白值() {
	result := headers.BuildBaseHeaders(map[string]string{
		"Token":   "token-x",
		"X-Blank": " ",
	})
	s.Equal("token-x", result["Token"], "Token 应保留")
	s.Len(result, 1, "纯空白值头部应被过滤")
}

// TestBuildBaseHeaders_Nil输入 测试 nil 输入返回空 map。
func (s *HeadersHelperSuite) TestBuildBaseHeaders_Nil输入() {
	result := headers.BuildBaseHeaders(nil)
	s.NotNil(result, "nil 输入不应返回 nil，应返回空 map")
	s.Empty(result, "nil 输入应返回空 map")
}

// TestBuildBaseHeaders_空输入 测试空 map 输入返回空 map。
func (s *HeadersHelperSuite) TestBuildBaseHeaders_空输入() {
	result := headers.BuildBaseHeaders(map[string]string{})
	s.NotNil(result, "空输入不应返回 nil，应返回空 map")
	s.Empty(result, "空输入应返回空 map")
}

// ---------------------------------------------------------------------------
// MergeHeadersCaseInsensitive（对齐 Python: merge_headers_case_insensitive）
// ---------------------------------------------------------------------------

// TestMergeHeadersCaseInsensitive_请求级覆盖配置级 测试请求级头部覆盖配置级。
// 对齐 Python: test_model_invoke_request_headers_override_case_insensitive
func (s *HeadersHelperSuite) TestMergeHeadersCaseInsensitive_请求级覆盖配置级() {
	base := map[string]string{
		"X-Tenant": "tenant-cfg",
		"UserID":   "user-cfg",
	}
	new := map[string]string{
		"x-tenant": "tenant-req",
		"userid":   "user-req",
	}
	headers.MergeHeadersCaseInsensitive(base, new)

	// 保留 base 的 key 大小写，值被 new 覆盖
	s.Equal("tenant-req", base["X-Tenant"], "X-Tenant 值应为请求级")
	s.Equal("user-req", base["UserID"], "UserID 值应为请求级")
	s.Len(base, 2, "合并后应保留 2 个头部")
}

// TestMergeHeadersCaseInsensitive_新增key 测试新增 key。
func (s *HeadersHelperSuite) TestMergeHeadersCaseInsensitive_新增key() {
	base := map[string]string{
		"X-Base": "base",
	}
	new := map[string]string{
		"X-New": "new-value",
	}
	headers.MergeHeadersCaseInsensitive(base, new)

	s.Equal("base", base["X-Base"], "X-Base 应保持原值")
	s.Equal("new-value", base["X-New"], "X-New 应被添加")
}

// TestMergeHeadersCaseInsensitive_Nil新头部 测试 nil 新头部不修改 base。
func (s *HeadersHelperSuite) TestMergeHeadersCaseInsensitive_Nil新头部() {
	base := map[string]string{
		"X-Base": "base-value",
	}
	headers.MergeHeadersCaseInsensitive(base, nil)

	s.Equal("base-value", base["X-Base"], "nil 新头部不应修改 base")
	s.Len(base, 1, "base 应保留 1 个头部")
}

// TestMergeHeadersCaseInsensitive_两者均为空 测试两者均为空。
func (s *HeadersHelperSuite) TestMergeHeadersCaseInsensitive_两者均为空() {
	base := map[string]string{}
	headers.MergeHeadersCaseInsensitive(base, nil)

	s.Empty(base, "两者均为空时结果应为空 map")
}

// ---------------------------------------------------------------------------
// MergeRequestHeaders（对齐 Python: merge_request_headers）
// ---------------------------------------------------------------------------

// TestMergeRequestHeaders_配置级和请求级 测试配置级和请求级合并。
func (s *HeadersHelperSuite) TestMergeRequestHeaders_配置级和请求级() {
	base := map[string]string{
		"X-Base": "base-val",
	}
	result := headers.MergeRequestHeaders(base, map[string]string{"X-Request": "req-val"})

	s.Equal("base-val", result["X-Base"], "X-Base 应保留配置级值")
	s.Equal("req-val", result["X-Request"], "X-Request 应为请求级值")
}

// TestMergeRequestHeaders_请求级覆盖配置级 测试请求级优先覆盖。
// 对齐 Python: test_model_invoke_request_headers_override_case_insensitive
func (s *HeadersHelperSuite) TestMergeRequestHeaders_请求级覆盖配置级() {
	base := map[string]string{
		"X-Token": "base-token",
	}
	result := headers.MergeRequestHeaders(base, map[string]string{"X-Token": "req-token"})

	s.Equal("req-token", result["X-Token"], "请求级应覆盖配置级")
}

// TestMergeRequestHeaders_大小写不敏感覆盖 测试大小写不敏感覆盖。
// 对齐 Python: test_model_invoke_request_headers_override_case_insensitive
func (s *HeadersHelperSuite) TestMergeRequestHeaders_大小写不敏感覆盖() {
	base := map[string]string{
		"X-Tenant": "tenant-cfg",
		"UserID":   "user-cfg",
	}
	result := headers.MergeRequestHeaders(base, map[string]string{
		"x-tenant":      "tenant-req",
		"userid":        "user-req",
		"Connection":    "blocked",
		"Authorization": "Bearer blocked",
	})

	// 保留 base 的 key 大小写，值被覆盖
	s.Equal("tenant-req", result["X-Tenant"], "X-Tenant 值应为请求级")
	s.Equal("user-req", result["UserID"], "UserID 值应为请求级")
	// 受保护头部应被过滤
	s.NotContains(result, "Connection", "Connection 不应出现（受保护头部）")
	s.NotContains(result, "Authorization", "Authorization 不应出现（受保护头部）")
}

// TestMergeRequestHeaders_Nil请求头 测试 nil 请求头。
func (s *HeadersHelperSuite) TestMergeRequestHeaders_Nil请求头() {
	base := map[string]string{
		"X-Base": "base-val",
	}
	result := headers.MergeRequestHeaders(base, nil)

	s.Equal("base-val", result["X-Base"], "nil 请求头应返回 base 拷贝")
	s.Len(result, 1, "结果应保留 1 个头部")
}

// TestMergeRequestHeaders_空配置头 测试空 base。
func (s *HeadersHelperSuite) TestMergeRequestHeaders_空配置头() {
	result := headers.MergeRequestHeaders(nil, map[string]string{"X-Request": "req-val"})

	s.Equal("req-val", result["X-Request"], "空 base 时请求头应保留")
}

// TestMergeRequestHeaders_不修改原始base 测试不修改原始 base。
func (s *HeadersHelperSuite) TestMergeRequestHeaders_不修改原始base() {
	base := map[string]string{
		"X-Base": "base-val",
	}
	_ = headers.MergeRequestHeaders(base, map[string]string{"X-New": "new-val"})

	s.NotContains(base, "X-New", "MergeRequestHeaders 不应修改原始 base")
	s.Equal("base-val", base["X-Base"], "原始 base 的 X-Base 应保持不变")
}

// TestMergeRequestHeaders_两者均为空 测试两者均为空。
func (s *HeadersHelperSuite) TestMergeRequestHeaders_两者均为空() {
	result := headers.MergeRequestHeaders(nil, nil)

	s.NotNil(result, "两者均为空不应返回 nil，应返回空 map")
	s.Empty(result, "两者均为空应返回空 map")
}

// ---------------------------------------------------------------------------
// SanitizeHeaders（对齐 Python: sanitize_headers）
// ---------------------------------------------------------------------------

// TestSanitizeHeaders_受保护头部被过滤 测试受保护头部被过滤。
func (s *HeadersHelperSuite) TestSanitizeHeaders_受保护头部被过滤() {
	input := map[string]string{
		"Authorization":     "Bearer token",
		"Host":              "api.openai.com",
		"Content-Length":    "100",
		"Transfer-Encoding": "chunked",
		"Connection":        "keep-alive",
		"X-Custom":          "value",
	}
	result := headers.SanitizeHeaders(input)

	// 全部 5 个受保护头部都应被过滤
	protectedKeys := []string{"Authorization", "Host", "Content-Length", "Transfer-Encoding", "Connection"}
	for _, key := range protectedKeys {
		s.NotContains(result, key, "%s 不应出现在结果中（受保护头部）", key)
	}
	s.Equal("value", result["X-Custom"], "X-Custom 应保留")
}

// TestSanitizeHeaders_ContentType不受保护 测试 content-type 不受保护。
func (s *HeadersHelperSuite) TestSanitizeHeaders_ContentType不受保护() {
	input := map[string]string{
		"Content-Type": "application/json",
	}
	result := headers.SanitizeHeaders(input)

	s.Equal("application/json", result["Content-Type"], "Content-Type 应保留")
}

// TestSanitizeHeaders_空值被过滤 测试空值头部被过滤。
func (s *HeadersHelperSuite) TestSanitizeHeaders_空值被过滤() {
	input := map[string]string{
		"X-Empty": "",
		"X-Valid": "ok",
	}
	result := headers.SanitizeHeaders(input)

	s.NotContains(result, "X-Empty", "空值头部应被过滤")
	s.Equal("ok", result["X-Valid"], "X-Valid 应保留")
}

// TestSanitizeHeaders_空白键被过滤 测试空白键被过滤。
func (s *HeadersHelperSuite) TestSanitizeHeaders_空白键被过滤() {
	input := map[string]string{
		"":        "empty-key",
		"  ":      "whitespace-key",
		"X-Valid": "ok",
	}
	result := headers.SanitizeHeaders(input)

	s.Len(result, 1, "空白键应被过滤，只保留 1 个头部")
	s.Equal("ok", result["X-Valid"], "X-Valid 应保留")
}

// TestSanitizeHeaders_Nil输入 测试 nil 输入返回空 map。
func (s *HeadersHelperSuite) TestSanitizeHeaders_Nil输入() {
	result := headers.SanitizeHeaders(nil)

	s.NotNil(result, "nil 输入不应返回 nil，应返回空 map")
	s.Empty(result, "nil 输入应返回空 map")
}

// TestSanitizeHeaders_键首尾空白被去除 测试键首尾空白被去除。
func (s *HeadersHelperSuite) TestSanitizeHeaders_键首尾空白被去除() {
	input := map[string]string{
		"  X-Trimmed  ": "value",
	}
	result := headers.SanitizeHeaders(input)

	s.Equal("value", result["X-Trimmed"], "键首尾空白去除后应保留")
}

// ---------------------------------------------------------------------------
// IsProtectedHeader & ProtectedHeaders
// ---------------------------------------------------------------------------

// TestIsProtectedHeader_受保护头部 测试受保护头部（大小写不敏感）。
func (s *HeadersHelperSuite) TestIsProtectedHeader_受保护头部() {
	s.True(headers.IsProtectedHeader("authorization"), "authorization 应为受保护头部")
	s.True(headers.IsProtectedHeader("Authorization"), "Authorization 应为受保护头部")
	s.True(headers.IsProtectedHeader("HOST"), "HOST 应为受保护头部")
	s.True(headers.IsProtectedHeader("content-length"), "content-length 应为受保护头部")
	s.True(headers.IsProtectedHeader("Transfer-Encoding"), "Transfer-Encoding 应为受保护头部")
	s.True(headers.IsProtectedHeader("Connection"), "Connection 应为受保护头部")
}

// TestIsProtectedHeader_非受保护头部 测试非受保护头部。
func (s *HeadersHelperSuite) TestIsProtectedHeader_非受保护头部() {
	s.False(headers.IsProtectedHeader("content-type"), "content-type 不应是受保护头部")
	s.False(headers.IsProtectedHeader("x-custom"), "x-custom 不应是受保护头部")
	s.False(headers.IsProtectedHeader(""), "空字符串不应是受保护头部")
}

// TestProtectedHeaders_包含全部5项 测试受保护头部包含全部 5 项。
func (s *HeadersHelperSuite) TestProtectedHeaders_包含全部5项() {
	expected := []string{"host", "content-length", "transfer-encoding", "connection", "authorization"}
	for _, h := range expected {
		s.True(headers.ProtectedHeaders[h], "ProtectedHeaders[%q] 应为 true", h)
	}
	s.Len(headers.ProtectedHeaders, 5, "受保护头部应恰好 5 项")
}

// ---------------------------------------------------------------------------
// ModelClientConfig CustomHeaders 交互验证
// ---------------------------------------------------------------------------

// TestModelClientConfig_CustomHeaders交互 测试 ModelClientConfig 的 CustomHeaders
// 与 headers_helper 的清洗逻辑交互。
// 对齐 Python: test_common_async_openai_client_forwards_sanitized_default_headers
func (s *HeadersHelperSuite) TestModelClientConfig_CustomHeaders交互() {
	// 模拟 Python 测试中的场景：配置中包含受保护头部和空值
	customHeaders := map[string]string{
		"Token":         "token-x",
		"Authorization": "blocked",
		"X-Blank":       " ",
	}

	// 通过 BuildBaseHeaders 清洗，等同于 Python 的 sanitize_headers
	sanitized := headers.BuildBaseHeaders(customHeaders)

	s.Equal("token-x", sanitized["Token"], "Token 应保留")
	s.NotContains(sanitized, "Authorization", "Authorization 应被过滤")
	s.NotContains(sanitized, "X-Blank", "纯空白值应被过滤")
	s.Len(sanitized, 1, "清洗后应只保留 1 个头部")
}

// TestModelClientConfig_CustomHeaders字段引用 测试 ModelClientConfig 的 CustomHeaders 字段可被引用。
// 验证集成测试中 llmschema 包可跨包引用。
func (s *HeadersHelperSuite) TestModelClientConfig_CustomHeaders字段引用() {
	cfg := llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		APIKey:         "sk-test",
		APIBase:        "https://api.openai.com/v1",
		CustomHeaders: map[string]string{
			"Token":  "integration-test",
			"UserID": "test-user",
		},
	}

	// 验证 CustomHeaders 字段可正常读写
	s.Equal("integration-test", cfg.CustomHeaders["Token"], "CustomHeaders Token 应可读取")
	s.Equal("test-user", cfg.CustomHeaders["UserID"], "CustomHeaders UserID 应可读取")

	// 通过 headers_helper 清洗
	sanitized := headers.BuildBaseHeaders(cfg.CustomHeaders)
	s.Len(sanitized, 2, "清洗后应保留 2 个头部")
	s.Equal("integration-test", sanitized["Token"], "清洗后 Token 应保留")
}

// TestModelClientConfig_NilCustomHeaders 测试 CustomHeaders 为 nil 时安全处理。
// 对齐 Python: test_common_openai_client_without_custom_headers_omits_default_headers
func (s *HeadersHelperSuite) TestModelClientConfig_NilCustomHeaders() {
	cfg := llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		APIKey:         "sk-test",
		APIBase:        "https://api.openai.com/v1",
		CustomHeaders:  nil,
	}

	// nil CustomHeaders 通过 BuildBaseHeaders 应返回空 map
	result := headers.BuildBaseHeaders(cfg.CustomHeaders)
	s.NotNil(result, "nil CustomHeaders 不应返回 nil")
	s.Empty(result, "nil CustomHeaders 应返回空 map")
}
