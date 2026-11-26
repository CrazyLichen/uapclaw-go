// T9 地基单测：计数/阈值/路径判定/前缀剥离/词项抽取。
// Python: common.py + query_terms.py + types.py。
package rulecompression

import (
	"strings"
	"testing"
)

// TestCountTokens 自定义计数器优先与 len//3 兜底。
// Python: common.py:74。
func TestCountTokens(t *testing.T) {
	ctx := NewRuleContext(100)
	if got := CountTokens("abcdef", ctx); got != 2 {
		t.Errorf("len//3 兜底 want 2 got %d", got)
	}
	if got := CountTokens("", ctx); got != 1 {
		t.Errorf("空串下限 1, got %d", got)
	}
	ctx.CountTokens = func(string) int { return 0 }
	if got := CountTokens("x", ctx); got != 1 {
		t.Errorf("自定义计数下限 1, got %d", got)
	}
}

// TestMeetsSavingsRatio 最小节省比例判定。
// Python: common.py:79。
func TestMeetsSavingsRatio(t *testing.T) {
	ctx := NewRuleContext(100)
	long := strings.Repeat("a", 300)
	short := strings.Repeat("a", 30)
	if !MeetsSavingsRatio(long, short, ctx) {
		t.Error("90% 节省应达标")
	}
	if MeetsSavingsRatio(long, strings.Repeat("a", 280), ctx) {
		t.Error("7% 节省不应达标")
	}
}

// TestFitsBudgetAndSaves 预算 + 节省双闸。
// Python: common.py:85。
func TestFitsBudgetAndSaves(t *testing.T) {
	ctx := NewRuleContext(10)
	original := strings.Repeat("a", 300)
	if FitsBudgetAndSaves(original, strings.Repeat("b", 60), ctx) {
		t.Error("候选超预算（60>10）应拒绝")
	}
	if !FitsBudgetAndSaves(original, strings.Repeat("b", 20), ctx) {
		t.Error("预算内且省 93% 应通过")
	}
}

// TestExtractFilePathArgument 已知键取首值 + JSON 字符串入参解析。
// Python: common.py:90。
func TestExtractFilePathArgument(t *testing.T) {
	if got := ExtractFilePathArgument(map[string]any{"path": " /a/b.go ", "other": 1}); got != "/a/b.go" {
		t.Errorf("want /a/b.go got %q", got)
	}
	if got := ExtractFilePathArgument(`{"file_path":"/x.py"}`); got != "/x.py" {
		t.Errorf("JSON 字符串 want /x.py got %q", got)
	}
	if got := ExtractFilePathArgument(`not json`); got != "" {
		t.Errorf("非法 JSON 应空, got %q", got)
	}
	if got := ExtractFilePathArgument(map[string]any{"unrelated": "v"}); got != "" {
		t.Errorf("无已知键应空, got %q", got)
	}
}

// TestIsSourceFilePath 源码/无扩展名约定名/点文件/数据文件路由。
// Python: common.py:110。
func TestIsSourceFilePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/a/b/main.go", true},
		{"C:\\x\\y\\app.py", true},
		{"/a/Makefile", true},
		{"/a/makefile", true},
		{"/a/.env", true},
		{"/a/data.json", false},
		{"/a/notes.log", false},
		{"/a/readme.txt", false},
		{"/a/diff.patch", false},
	}
	for _, tc := range cases {
		if got := IsSourceFilePath(tc.path); got != tc.want {
			t.Errorf("IsSourceFilePath(%q)=%v want %v", tc.path, got, tc.want)
		}
	}
}

// TestStripDisplayLinePrefixes 30% 闸门与体剥离。
// Python: common.py:130/138。
func TestStripDisplayLinePrefixes(t *testing.T) {
	numbered := "1\talpha\n2\tbeta\n3\tgamma\n"
	got := StripDisplayLinePrefixes(numbered)
	if strings.Contains(got, "1\t") {
		t.Errorf("应剥离行号前缀, got %q", got)
	}
	// 占比 <30% 不动
	mixed := "1\talpha\nplain line\nanother plain\nanother\n"
	if got := StripDisplayLinePrefixes(mixed); got != mixed {
		t.Errorf("占比不足 30%% 应原样, got %q", got)
	}
	// 保留体空白变体
	ws := "1\t  indented\n2\t  also\n"
	got = StripDisplayLinePrefixesPreservingBodyWhitespace(ws)
	if !strings.Contains(got, "  indented") {
		t.Errorf("保留变体应保住体首空白, got %q", got)
	}
}

// TestExtractQueryTerms 词项抽取：路径键与非用户键跳过。
// Python: query_terms.py:36。
func TestExtractQueryTerms(t *testing.T) {
	terms := ExtractQueryTerms("fix the LoginHandler bug", "read_file",
		map[string]any{
			"file_path": "/src/LoginHandler.py",
			"limit":     10,
			"query":     "error handling",
		})
	if !terms["loginhandler"] || !terms["read_file"] || !terms["error"] {
		t.Errorf("应含 loginhandler/read_file/error, got %v", terms)
	}
	// file_path 值不入词项（键名路径键跳过）
	if terms["src"] || terms["loginhandler.py"] {
		t.Errorf("路径键值不应入词项, got %v", terms)
	}
	// limit 值不入词项
	if terms["10"] {
		t.Errorf("非用户键值不应入词项, got %v", terms)
	}
	// 路径分隔符前缀跳过
	terms2 := ExtractQueryTerms("see /src/auth.py and plain auth", "", nil)
	if terms2["src"] {
		t.Errorf("分隔符前缀词应跳过, got %v", terms2)
	}
	if !terms2["auth"] {
		t.Error("普通词应保留")
	}
}
