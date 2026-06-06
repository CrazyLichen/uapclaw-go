package output_parsers

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── Parse 测试 ────────────────────────────

// TestMarkdownOutputParser_Parse_Headers 测试标题提取。
func TestMarkdownOutputParser_Parse_Headers(t *testing.T) {
	parser := NewMarkdownOutputParser()

	input := "# 大标题\n## 二级标题\n### 三级标题"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content, ok := result.(*MarkdownContent)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}

	if len(content.Headers) != 3 {
		t.Fatalf("Headers 数量 = %d, 期望 3", len(content.Headers))
	}
	if content.Headers[0]["title"] != "大标题" {
		t.Errorf("第一个标题 = %v, 期望 大标题", content.Headers[0]["title"])
	}
}

// TestMarkdownOutputParser_Parse_CodeBlocks 测试代码块提取。
func TestMarkdownOutputParser_Parse_CodeBlocks(t *testing.T) {
	parser := NewMarkdownOutputParser()

	input := "代码如下：\n```go\nfmt.Println(\"hello\")\n```\n结束"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content := result.(*MarkdownContent)

	// 1 个代码块 + 0 个行内代码 = 1 个 CodeBlock
	codeBlockCount := 0
	for _, cb := range content.CodeBlocks {
		if cb["language"] != "inline" {
			codeBlockCount++
		}
	}
	if codeBlockCount != 1 {
		t.Errorf("代码块数量 = %d, 期望 1", codeBlockCount)
	}

	// 找到 go 代码块
	var goBlock *map[string]string
	for i := range content.CodeBlocks {
		if content.CodeBlocks[i]["language"] == "go" {
			goBlock = &content.CodeBlocks[i]
			break
		}
	}
	if goBlock == nil {
		t.Fatal("未找到 go 代码块")
	}
	if !contains((*goBlock)["code"], "Println") {
		t.Errorf("go 代码块内容不包含 Println: %v", (*goBlock)["code"])
	}
}

// TestMarkdownOutputParser_Parse_LinksAndImages 测试链接和图片提取。
func TestMarkdownOutputParser_Parse_LinksAndImages(t *testing.T) {
	parser := NewMarkdownOutputParser()

	input := "![图片描述](https://example.com/img.png)\n[链接文本](https://example.com)"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content := result.(*MarkdownContent)

	if len(content.Images) != 1 {
		t.Errorf("Images 数量 = %d, 期望 1", len(content.Images))
	} else if content.Images[0]["alt"] != "图片描述" {
		t.Errorf("图片 alt = %v, 期望 图片描述", content.Images[0]["alt"])
	}

	if len(content.Links) != 1 {
		t.Errorf("Links 数量 = %d, 期望 1", len(content.Links))
	} else if content.Links[0]["text"] != "链接文本" {
		t.Errorf("链接 text = %v, 期望 链接文本", content.Links[0]["text"])
	}
}

// TestMarkdownOutputParser_Parse_Tables 测试表格提取。
func TestMarkdownOutputParser_Parse_Tables(t *testing.T) {
	parser := NewMarkdownOutputParser()

	input := "| A | B |\n|---|---|\n| 1 | 2 |"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content := result.(*MarkdownContent)

	if len(content.Tables) != 1 {
		t.Fatalf("Tables 数量 = %d, 期望 1", len(content.Tables))
	}
	if !contains(content.Tables[0], "| A |") {
		t.Errorf("表格内容不包含 '| A |': %v", content.Tables[0])
	}
}

// TestMarkdownOutputParser_Parse_Lists 测试列表提取。
func TestMarkdownOutputParser_Parse_Lists(t *testing.T) {
	parser := NewMarkdownOutputParser()

	input := "- 项目一\n- 项目二\n- 项目三"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content := result.(*MarkdownContent)

	if len(content.Lists) != 1 {
		t.Fatalf("Lists 数量 = %d, 期望 1", len(content.Lists))
	}
	if !contains(content.Lists[0], "项目一") {
		t.Errorf("列表内容不包含 '项目一': %v", content.Lists[0])
	}
}

// TestMarkdownOutputParser_Parse_EmptyInput 测试空输入。
func TestMarkdownOutputParser_Parse_EmptyInput(t *testing.T) {
	parser := NewMarkdownOutputParser()

	result, err := parser.Parse("")
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("空输入应返回 nil, 实际: %v", result)
	}
}

// TestMarkdownOutputParser_Parse_AssistantMessage 测试 AssistantMessage 输入。
func TestMarkdownOutputParser_Parse_AssistantMessage(t *testing.T) {
	parser := NewMarkdownOutputParser()

	msg := llmschema.NewAssistantMessage("# Hello\nSome text")
	result, err := parser.Parse(msg)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content := result.(*MarkdownContent)
	if len(content.Headers) != 1 {
		t.Errorf("Headers 数量 = %d, 期望 1", len(content.Headers))
	}
}

// TestMarkdownOutputParser_Parse_ComplexDocument 测试复杂文档。
func TestMarkdownOutputParser_Parse_ComplexDocument(t *testing.T) {
	parser := NewMarkdownOutputParser()

	input := `# 标题

一些文字和 ` + "`inline code`" + `

` + "```python" + `
print("hello")
` + "```" + `

- 列表项1
- 列表项2

| 列A | 列B |
|-----|-----|
| 1   | 2   |

[链接](https://example.com)

![图片](img.png)`

	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}

	content := result.(*MarkdownContent)

	if len(content.Headers) < 1 {
		t.Errorf("Headers 数量 = %d, 期望至少 1", len(content.Headers))
	}
	if len(content.CodeBlocks) < 2 { // python 块 + inline code
		t.Errorf("CodeBlocks 数量 = %d, 期望至少 2", len(content.CodeBlocks))
	}
	if len(content.Lists) < 1 {
		t.Errorf("Lists 数量 = %d, 期望至少 1", len(content.Lists))
	}
	if len(content.Tables) < 1 {
		t.Errorf("Tables 数量 = %d, 期望至少 1", len(content.Tables))
	}
	if len(content.Links) < 1 {
		t.Errorf("Links 数量 = %d, 期望至少 1", len(content.Links))
	}
	if len(content.Images) < 1 {
		t.Errorf("Images 数量 = %d, 期望至少 1", len(content.Images))
	}
}

// ──────────────────────────── StreamParse 测试 ────────────────────────────

// TestMarkdownOutputParser_StreamParse 测试流式解析。
func TestMarkdownOutputParser_StreamParse(t *testing.T) {
	parser := NewMarkdownOutputParser()

	chunks := make(chan *llmschema.AssistantMessageChunk, 3)
	chunks <- llmschema.NewAssistantMessageChunk("# 标题\n")
	chunks <- llmschema.NewAssistantMessageChunk("- 列表项\n")
	close(chunks)

	var lastContent *MarkdownContent
	for r := range parser.StreamParse(chunks) {
		if r.Error != nil {
			t.Fatalf("StreamParse 返回错误: %v", r.Error)
		}
		if md, ok := r.Content.(*MarkdownContent); ok {
			lastContent = md
		}
	}

	if lastContent == nil {
		t.Fatal("未收到解析结果")
	}
	if len(lastContent.Headers) < 1 {
		t.Errorf("Headers 数量 = %d, 期望至少 1", len(lastContent.Headers))
	}
}

// TestMarkdownOutputParser_StreamParse_EmptyStream 测试空流。
func TestMarkdownOutputParser_StreamParse_EmptyStream(t *testing.T) {
	parser := NewMarkdownOutputParser()

	chunks := make(chan *llmschema.AssistantMessageChunk)
	close(chunks)

	count := 0
	for range parser.StreamParse(chunks) {
		count++
	}

	if count != 0 {
		t.Errorf("空流结果数量 = %d, 期望 0", count)
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// contains 检查字符串是否包含子串。
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

// containsSubstr 辅助函数，检查字符串包含关系。
func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
