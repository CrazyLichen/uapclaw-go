// T10 七压缩器 + router 分派 + B-11 单测。
package rulecompression

import (
	"os"
	"strings"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// llmBaseMessage 别名（测试辅助）
type llmBaseMessage = llm_schema.BaseMessage
type llmToolMessage = llm_schema.ToolMessage

// itoa 测试辅助
func itoa(i int) string { return strings.TrimSpace(strings.Repeat("", 0) + intToStr(i)) }

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

// osReadDir 测试辅助
func osReadDir(dir string) ([]os.DirEntry, error) { return os.ReadDir(dir) }

// newPipelineToolMessage 构造 ToolMessage
func newPipelineToolMessage(content string) *llm_schema.ToolMessage {
	return llm_schema.NewToolMessage("call_1", content)
}

// newPipelineAssistant 构造带 tool_call 的 AssistantMessage
func newPipelineAssistant(name, argsJSON string) *llm_schema.AssistantMessage {
	am := llm_schema.NewAssistantMessage("")
	am.ToolCalls = []*llm_schema.ToolCall{llm_schema.NewToolCall("call_1", name, argsJSON)}
	return am
}

// newPipelineUser 构造 UserMessage
func newPipelineUser(content string) *llm_schema.UserMessage {
	return llm_schema.NewUserMessage(content)
}

// pipelineFakeContext 管线窄面上下文
type pipelineFakeContext struct {
	workspaceDir string
	messages     []llm_schema.BaseMessage
}

func (f *pipelineFakeContext) WorkspaceDir() string { return f.workspaceDir }
func (f *pipelineFakeContext) SessionID() string    { return "sess" }
func (f *pipelineFakeContext) ContextID() string    { return "ctx" }
func (f *pipelineFakeContext) GetMessages(int, bool) ([]llm_schema.BaseMessage, error) {
	return f.messages, nil
}

var _ PipelineContext = (*pipelineFakeContext)(nil)

// TestPlainTextCompressor 去重 + 空行收敛。
func TestPlainTextCompressor(t *testing.T) {
	content := "alpha\nalpha\n\n\nbeta\nalpha\n"
	got := PlainTextCompressor{}.Compress(content, NewRuleContext(1000))
	if !got.Modified || got.ContentType != ContentTypePlainText {
		t.Fatalf("应修改: %+v", got)
	}
	if strings.Contains(got.Content, "beta\n\nbeta") {
		t.Errorf("应去重: %q", got.Content)
	}
	// 无收益不改
	same := PlainTextCompressor{}.Compress("a\nb\nc", NewRuleContext(1000))
	if same.Modified {
		t.Error("无重复不应修改")
	}
}

// TestSourceFileCompressor_B11 按路径读的源文件拒绝规则压缩（B-11 双闸：router 判型 + 压缩器拒绝）。
func TestSourceFileCompressor_B11(t *testing.T) {
	router := NewRuleContentRouter()
	ctx := NewRuleContext(1000)
	ctx.FilePath = "/repo/main.go"
	content := strings.Repeat("func main() {\n\tfmt.Println(\"x\")\n}\n", 500)
	if ct := router.Detect(content, ctx); ct != ContentTypeSourceFile {
		t.Fatalf("源文件路径应判型 SOURCE_FILE, got %s", ct)
	}
	got := router.Compress(content, ctx)
	if got.Modified || got.ContentType != ContentTypeSourceFile {
		t.Errorf("源文件应原样返回: modified=%v type=%s", got.Modified, got.ContentType)
	}
	if got.Content != content {
		t.Error("B-11：源文件内容必须逐字保留")
	}
}

// TestRouterDetect 分派矩阵。
func TestRouterDetect(t *testing.T) {
	router := NewRuleContentRouter()
	cases := []struct {
		name    string
		content string
		want    ContentType
	}{
		{"json", `[{"a":1},{"a":2}]`, ContentTypeJSONArray},
		{"diff", "diff --git a/x b/x\n@@ -1,2 +1,2 @@\n-old\n+new", ContentTypeGitDiff},
		{"log", "2024-01-01 00:00:00 ERROR something\n2024-01-01 00:00:01 ERROR another", ContentTypeLog},
		{"plain", "just some words here", ContentTypePlainText},
		{"empty", "   ", ContentTypePlainText},
	}
	for _, tc := range cases {
		if got := router.Detect(tc.content, NewRuleContext(1000)); got != tc.want {
			t.Errorf("%s: want %s got %s", tc.name, tc.want, got)
		}
	}
}

// TestJsonArrayCompress 对象数组 CSV/JSONL 与有损抽样。
func TestJsonArrayCompress(t *testing.T) {
	var rows []string
	for i := 0; i < 200; i++ {
		rows = append(rows, `{"id":`+itoa(i)+`,"name":"item","status":"ok"}`)
	}
	content := "[" + strings.Join(rows, ",") + "]"
	// 有损抽样候选 ~186 tokens，预算给到容纳值
	ctx := NewRuleContext(200)
	got := JsonArrayCompressor{}.Compress(content, ctx)
	if !got.Modified {
		t.Fatal("大数组应压缩")
	}
	// CSV/JSONL 路径带标记头；有损抽样路径输出 JSON（含 _omitted 标记）——两者皆是合法压缩产物
	if !strings.Contains(got.Content, "_omitted") && !strings.HasPrefix(got.Content, "[JSON_ARRAY compressed") {
		t.Errorf("应含压缩产物形态: %q", got.Content[:80])
	}
	// 小数组不压
	small := `[{"a":1}]`
	smallGot := JsonArrayCompressor{}.Compress(small, NewRuleContext(1000))
	if smallGot.Modified {
		t.Error("预算内不应修改")
	}
}

// TestSearchResultsCompress 搜索结果分组压缩。
func TestSearchResultsCompress(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "/src/file"+itoa(i%3)+".py:"+itoa(i)+": some error content here "+itoa(i))
	}
	content := strings.Join(lines, "\n")
	ctx := NewRuleContext(100)
	ctx.SearchMaxFiles = 2
	ctx.SearchMaxMatchesPerFile = 3
	ctx.SearchMaxTotalMatches = 6
	got := SearchResultsCompressor{}.Compress(content, ctx)
	if !got.Modified || !strings.HasPrefix(got.Content, "[SEARCH_RESULTS compressed]") {
		t.Fatalf("应压缩: %q", got.Content[:60])
	}
	if got.Details["files_retained"].(int) != 2 {
		t.Errorf("文件数上限应生效: %v", got.Details["files_retained"])
	}
}

// TestLogCompress 日志压缩。
func TestLogCompress(t *testing.T) {
	var lines []string
	for i := 0; i < 80; i++ {
		if i%20 == 0 {
			lines = append(lines, "2024-01-01 00:00:00 ERROR failed to connect")
		} else {
			lines = append(lines, "2024-01-01 00:00:00 INFO heartbeat ok "+itoa(i))
		}
	}
	content := strings.Join(lines, "\n")
	ctx := NewRuleContext(100)
	got := LogCompressor{}.Compress(content, ctx)
	if !got.Modified || got.ContentType != ContentTypeLog {
		t.Fatalf("应压缩: modified=%v", got.Modified)
	}
	if !strings.Contains(got.Content, "[LOG compressed") || !strings.Contains(got.Content, "ERROR failed to connect") {
		t.Error("应保留标记头与错误行")
	}
	// 少于 log_min_lines 不压
	smallLog := LogCompressor{}.Compress("a\nb", NewRuleContext(100))
	if smallLog.Modified {
		t.Error("行数不足不应修改")
	}
}

// TestDiffCompress diff 压缩（大 diff 才压）。
func TestDiffCompress(t *testing.T) {
	var lines []string
	lines = append(lines, "diff --git a/f.py b/f.py", "@@ -1,4 +1,4 @@")
	for i := 0; i < 60; i++ {
		lines = append(lines, " old line "+itoa(i))
	}
	for i := 0; i < 30; i++ {
		lines = append(lines, "+error: new line "+itoa(i))
	}
	content := strings.Join(lines, "\n")
	ctx := NewRuleContext(100)
	got := DiffCompressor{}.Compress(content, ctx)
	if !got.Modified || got.ContentType != ContentTypeGitDiff {
		t.Fatalf("应压缩: modified=%v", got.Modified)
	}
	if !strings.Contains(got.Content, "files changed") {
		t.Error("应含汇总标记")
	}
	// 小 diff 不压
	smallDiff := DiffCompressor{}.Compress("diff --git a/b c\n@@ -1 +1 @@\n-x\n+y", NewRuleContext(100))
	if smallDiff.Modified {
		t.Error("小于 diff_min_lines 不应修改")
	}
}

// TestHTMLCompressFallback HTML fallback 压缩。
func TestHTMLCompressFallback(t *testing.T) {
	content := "<html><head><style>.x{color:red}</style><title>Hi</title></head>" +
		"<body><nav>menu</nav><article><h1>Title</h1><p>" +
		strings.Repeat("meaningful paragraph content. ", 30) +
		"</p><script>var a=1;</script></article></body></html>"
	ctx := NewRuleContext(100)
	ctx.HTMLMinContentChars = 50
	got := HtmlCompressor{}.Compress(content, ctx)
	if !got.Modified || got.ContentType != ContentTypeHTML {
		t.Fatalf("应压缩: modified=%v type=%s", got.Modified, got.ContentType)
	}
	if strings.Contains(got.Content, "var a=1") {
		t.Error("script 噪声应移除")
	}
	if !strings.Contains(got.Content, "meaningful paragraph") {
		t.Error("主内容应保留")
	}
}

// TestPipelineCompress 管线：ToolMessage 超预算触发 + metadata 更新（压缩消息形态）。
func TestPipelineCompress(t *testing.T) {
	dumpDir := t.TempDir()
	pipeline := NewRuleCompressionPipeline(nil, func() float64 { return 1.0 }, true, dumpDir)
	var rows []string
	for i := 0; i < 200; i++ {
		rows = append(rows, `{"key":"value`+itoa(i)+`","status":"ok"}`)
	}
	content := "[" + strings.Join(rows, ",") + "]"
	toolMsg := newPipelineToolMessage(content)
	assistant := newPipelineAssistant("read_file", `{"file_path":"/a/b.json"}`)
	user := newPipelineUser("read the config file")
	mc := &pipelineFakeContext{workspaceDir: dumpDir, messages: []llmBaseMessage{user, assistant, toolMsg}}
	got := pipeline.Compress(toolMsg, mc, "test-pass", 500, false, mc.messages)
	replaced, ok := got.(*llmToolMessage)
	if !ok {
		t.Fatalf("应返回 ToolMessage, got %T", got)
	}
	if len(replaced.Content.Text()) >= len(content) {
		t.Error("压缩后应变短")
	}
	// dump 落盘
	entries, err := osReadDir(dumpDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("dump 目录应有产物: %v", err)
	}
}

// TestExtractFilePathPipeline 管线从工具参数取 file_path 判型 SOURCE_FILE。
func TestExtractFilePathPipeline(t *testing.T) {
	if got := ExtractFilePathArgument(`{"file_path":"/x/main.py"}`); got != "/x/main.py" {
		t.Errorf("want /x/main.py got %q", got)
	}
}
