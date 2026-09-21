package filesystem

import (
	"testing"
)

// TestDesanitize_HTML实体反转 测试 HTML 实体解码
func TestDesanitize_HTML实体反转(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"html实体", "&lt;div&gt;", "<div>"},
		{"压缩标记fnr", "<fnr>hello", "<function_results>hello"},
		{"压缩标记n", "<n>test</n>", "<name>test</name>"},
		{"压缩标记o", "<o>out</o>", "<output>out</output>"},
		{"压缩标记e", "<e>err</e>", "<error>err</error>"},
		{"压缩标记s", "<s>sys</s>", "<system>sys</system>"},
		{"压缩标记r", "<r>res</r>", "<result>res</result>"},
		{"META标记", "< META_START >", "<META_START>"},
		{"H标记", "\n\nH:hello", "\n\nHuman:hello"},
		{"A标记", "\n\nA:reply", "\n\nAssistant:reply"},
		{"无替换", "plain text", "plain text"},
		{"混合", "&lt;n&gt;tag&lt;/n&gt;", "<name>tag</name>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Desanitize(tt.input)
			if got != tt.want {
				t.Errorf("Desanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNormalizeQuotes_弯引号转直引号 测试弯引号归一化
func TestNormalizeQuotes_弯引号转直引号(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"左单弯引号", "\u2018hello\u2019", "'hello'"},
		{"右单弯引号", "it\u2019s", "it's"},
		{"双弯引号", "\u201chello\u201d", `"hello"`},
		{"混合引号", "\u2018it\u2019s \u201cquoted\u201d", "'it's \"quoted\""},
		{"无引号", "plain", "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeQuotes(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestApplyCurlyDoubleQuotes_上下文感知 测试弯双引号替换
func TestApplyCurlyDoubleQuotes_上下文感知(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"句首开引号", `"hello"`, "\u201chello\u201d"},
		{"空格后开引号", `he said "hi"`, "he said \u201chi\u201d"},
		{"括号后开引号", `("test")`, "(\u201ctest\u201d)"},
		{"多个引号", `"a" and "b"`, "\u201ca\u201d and \u201cb\u201d"},
		{"无引号", "no quotes", "no quotes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyCurlyDoubleQuotes(tt.input)
			if got != tt.want {
				t.Errorf("ApplyCurlyDoubleQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestApplyCurlySingleQuotes_上下文感知 测试弯单引号替换
func TestApplyCurlySingleQuotes_上下文感知(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"撇号", "it's great", "it\u2019s great"},
		{"句首开引号", "'hello'", "\u2018hello\u2019"},
		{"空格后开引号", "he said 'hi'", "he said \u2018hi\u2019"},
		{"多个引号", "it's 'nice'", "it\u2019s \u2018nice\u2019"},
		{"无引号", "no quotes", "no quotes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyCurlySingleQuotes(tt.input)
			if got != tt.want {
				t.Errorf("ApplyCurlySingleQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestPreserveQuoteStyle_引号风格保留 测试引号风格保留逻辑
func TestPreserveQuoteStyle_引号风格保留(t *testing.T) {
	tests := []struct {
		name        string
		oldStr      string
		actualOldStr string
		newStr      string
		want        string
	}{
		{"相同无转换", "hello", "hello", "world", "world"},
		{"含弯双引号", `"old"`, "\u201cold\u201d", `"new"`, "\u201cnew\u201d"},
		{"含弯单引号", "'old'", "\u2018old\u2019", "'new'", "\u2018new\u2019"},
		{"混合引号", `"it's"`, "\u201cit\u2019s\u201d", `"new's"`, "\u201cnew\u2019s\u201d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PreserveQuoteStyle(tt.oldStr, tt.actualOldStr, tt.newStr)
			if got != tt.want {
				t.Errorf("PreserveQuoteStyle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTryQuoteVariants_引号变体匹配 测试引号变体查找
func TestTryQuoteVariants_引号变体匹配(t *testing.T) {
	tests := []struct {
		name       string
		oldStr     string
		content    string
		wantMatch  string
		wantFound  bool
	}{
		{"直接匹配", "hello", "say hello world", "hello", true},
		{"弯引号匹配", "\u2018hello\u2019", "say 'hello' world", "'hello'", true},
		{"弯双引号匹配", "\u201chello\u201d", `say "hello" world`, `"hello"`, true},
		{"未匹配", "xyz", "no match here", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatch, gotFound := TryQuoteVariants(tt.oldStr, tt.content)
			if gotFound != tt.wantFound {
				t.Errorf("TryQuoteVariants() found = %v, want %v", gotFound, tt.wantFound)
			}
			if gotFound && gotMatch != tt.wantMatch {
				t.Errorf("TryQuoteVariants() match = %q, want %q", gotMatch, tt.wantMatch)
			}
		})
	}
}
