package providererror

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// pythonHTML404 / pythonHTML504 与 Python test_provider_error.py:20-27 逐字一致。
var (
	pythonHTML404 = "<!DOCTYPE html><html lang='en'><head><title>Not Found | opencode</title></head>" +
		"<body><h1>404 - Page Not Found</h1></body></html>"
	pythonHTML502 = "<html><head><title>502 Bad Gateway</title></head>" +
		"<body><h1>502 Bad Gateway</h1></body></html>"
)

// typeName 对齐 Python type(exc).__name__（Go 用 %T 短名近似）。
func typeName(err error) string {
	t := fmt.Sprintf("%T", err)
	if i := strings.LastIndex(t, "."); i >= 0 {
		return t[i+1:]
	}
	return t
}

// TestFormatProviderException_空消息回退类型名 对齐 test_empty_timeout_keeps_exception_type。
func TestFormatProviderException_空消息回退类型名(t *testing.T) {
	err := errors.New("")
	if got := FormatProviderException(err, typeName(err), 0, true); got != typeName(err) {
		t.Fatalf("got %q, want %q", got, typeName(err))
	}
}

// TestFormatProviderException_带消息超时保持原文 对齐 test_timeout_with_message_is_unchanged。
func TestFormatProviderException_带消息超时保持原文(t *testing.T) {
	err := errors.New("stream frame timeout: stage=idle_chunk, timeout=10s")
	want := typeName(err) + ": stream frame timeout: stage=idle_chunk, timeout=10s"
	if got := FormatProviderException(err, typeName(err), 0, true); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := FormatProviderException(err, typeName(err), 0, false); got != err.Error() {
		t.Fatalf("include_exc_type=false: got %q", got)
	}
}

// TestFormatProviderException_JSONAPI错误保持原文 对齐 test_json_api_error_is_unchanged。
func TestFormatProviderException_JSONAPI错误保持原文(t *testing.T) {
	message := "Error code: 400 - {'error': {'message': 'invalid api key'}}"
	err := errors.New(message)
	if got := FormatProviderException(err, "_SdkError", 400, false); got != message {
		t.Fatalf("got %q, want 原文", got)
	}
}

// TestFormatProviderException_HTML404折叠 对齐 test_html_404_is_summarized_with_status_and_title。
func TestFormatProviderException_HTML404折叠(t *testing.T) {
	err := errors.New(pythonHTML404)
	text := FormatProviderException(err, "_SdkError", 404, true)
	for _, want := range []string{"HTTP 404", "Not Found | opencode", "HTML error page"} {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少 %q: %q", want, text)
		}
	}
	for _, ban := range []string{"<!DOCTYPE", "<html"} {
		if strings.Contains(text, ban) {
			t.Fatalf("不应含 %q: %q", ban, text)
		}
	}
	// str(len(_HTML_404)) in text
	if !strings.Contains(text, fmt.Sprintf("%d chars", len(pythonHTML404))) {
		t.Fatalf("应含长度 %d: %q", len(pythonHTML404), text)
	}
}

// TestFormatProviderException_HTML502标题推断状态码 对齐 test_html_502_infers_status_from_title_for_retry。
func TestFormatProviderException_HTML502标题推断状态码(t *testing.T) {
	err := errors.New(pythonHTML502)
	text := FormatProviderException(err, "_SdkError", 0, false)
	if strings.Contains(text, "<title>") {
		t.Fatalf("不应含 <title>: %q", text)
	}
	if !strings.Contains(text, "HTTP 502") || !strings.Contains(text, "502 Bad Gateway") {
		t.Fatalf("应推断 502: %q", text)
	}
}

// TestFormatProviderException_错误码前缀加HTML 对齐 test_error_code_prefix_plus_html_keeps_http_status。
func TestFormatProviderException_错误码前缀加HTML(t *testing.T) {
	err := errors.New("Error code: 404 - " + pythonHTML404)
	text := FormatProviderException(err, "_SdkError", 0, true)
	if !strings.Contains(text, "HTTP 404") || strings.Contains(text, "<!DOCTYPE") {
		t.Fatalf("文本推断 404 失效: %q", text)
	}
}

// TestFormatProviderException_短HTML片段不当错误页 对齐 test_short_html_snippet_in_json_is_not_treated_as_error_page。
func TestFormatProviderException_短HTML片段不当错误页(t *testing.T) {
	message := `{"error": "expected <html> tag"}`
	err := errors.New(message)
	if got := FormatProviderException(err, "_SdkError", 0, false); got != message {
		t.Fatalf("got %q, want 原文", got)
	}
}

// TestSummarizeProviderErrorText_超长截断 对齐 test_oversized_plain_text_is_truncated。
func TestSummarizeProviderErrorText_超长截断(t *testing.T) {
	payload := strings.Repeat("x", MaxProviderErrorChars+40)
	text := SummarizeProviderErrorText(payload, 0)
	if !strings.HasPrefix(text, "xxxxxxxx") {
		t.Fatal("应以原文开头")
	}
	if !strings.Contains(text, "[truncated 40 chars]") || len(text) >= len(payload) {
		t.Fatalf("截断不符: %q", text)
	}
}

// TestMarkupPayload_起点超160不判错误页 对齐 _markup_payload start>160 分支。
func TestMarkupPayload_起点超160不判错误页(t *testing.T) {
	text := strings.Repeat("x", 200) + "<html><body></body></html>"
	if _, ok := markupPayload(text); ok {
		t.Fatal("起点 200 > 160 应不判错误页")
	}
}

// TestIsErrorPage_XML长文档 对齐 _is_error_page 的 <?xml 分支。
func TestIsErrorPage_XML长文档(t *testing.T) {
	if !isErrorPage("<?xml version='1.0'?><fault>detail</fault>") {
		t.Fatal("<?xml + <fault> 应判错误页")
	}
	if isErrorPage("<?xml version='1.0'?>short") {
		t.Fatal("短 XML 无标签应不判")
	}
}

// TestPlainText_反转义与引号替换 对齐 _plain_text。
func TestPlainText_反转义与引号替换(t *testing.T) {
	got := plainText(`<h1>502 &quot;Bad&quot;  Gateway</h1>`)
	if got != `502 'Bad' Gateway` {
		t.Fatalf("got %q", got)
	}
}
