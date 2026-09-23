package rb

import (
	"testing"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// TestMemoryItemParser_Parse_正常解析 验证标准 Markdown 输入可正确解析
func TestMemoryItemParser_Parse_正常解析(t *testing.T) {
	input := `# Memory Item 1
## Title Search Strategy
## Description Use keyword-based search for better results.
## Content When searching for information, breaking down the query into keywords works better than full sentences.
`

	parser := NewMemoryItemParser()
	label := true
	memories := parser.Parse(input, "test query", &label)

	if len(memories) != 1 {
		t.Fatalf("期望 1 个记忆，实际 %d", len(memories))
	}

	m := memories[0]
	if m.Query != "test query" {
		t.Errorf("Query 期望 'test query'，实际 %q", m.Query)
	}
	if m.Label == nil || *m.Label != true {
		t.Error("Label 期望 true")
	}
	if len(m.Memory) != 1 {
		t.Fatalf("期望 1 个 MemoryItem，实际 %d", len(m.Memory))
	}
	item := m.Memory[0]
	if item.Title != "Search Strategy" {
		t.Errorf("Title 期望 'Search Strategy'，实际 %q", item.Title)
	}
	if item.Description != "Use keyword-based search for better results." {
		t.Errorf("Description 不符合预期，实际 %q", item.Description)
	}
	if item.Content != "When searching for information, breaking down the query into keywords works better than full sentences." {
		t.Errorf("Content 不符合预期，实际 %q", item.Content)
	}
}

// TestMemoryItemParser_Parse_代码围栏 验证被 ``` 包裹的输入可正确解析
func TestMemoryItemParser_Parse_代码围栏(t *testing.T) {
	input := "```markdown\n# Memory Item 1\n## Title T1\n## Description D1\n## Content C1\n```"

	parser := NewMemoryItemParser()
	memories := parser.Parse(input, "q", nil)

	if len(memories) != 1 {
		t.Fatalf("期望 1 个记忆，实际 %d", len(memories))
	}
	item := memories[0].Memory[0]
	if item.Title != "T1" {
		t.Errorf("Title 期望 'T1'，实际 %q", item.Title)
	}
}

// TestMemoryItemParser_Parse_多段落 验证多个 # Memory Item 段落可分别解析
func TestMemoryItemParser_Parse_多段落(t *testing.T) {
	input := `# Memory Item 1
## Title Title One
## Description Desc One
## Content Content One

# Memory Item 2
## Title Title Two
## Description Desc Two
## Content Content Two
`

	parser := NewMemoryItemParser()
	memories := parser.Parse(input, "multi query", nil)

	if len(memories) != 2 {
		t.Fatalf("期望 2 个记忆，实际 %d", len(memories))
	}
	if memories[0].Memory[0].Title != "Title One" {
		t.Errorf("第 1 项 Title 期望 'Title One'，实际 %q", memories[0].Memory[0].Title)
	}
	if memories[1].Memory[0].Title != "Title Two" {
		t.Errorf("第 2 项 Title 期望 'Title Two'，实际 %q", memories[1].Memory[0].Title)
	}
}

// TestMemoryItemParser_Parse_缺少字段 验证缺少 Title/Description/Content 的段落返回 nil
func TestMemoryItemParser_Parse_缺少字段(t *testing.T) {
	input := `# Memory Item 1
## Title Only Title
## Description Only Desc

# Memory Item 2
## Title Complete
## Description Has all fields
## Content With content
`

	parser := NewMemoryItemParser()
	memories := parser.Parse(input, "q", nil)

	// 只有第 2 项应该被解析
	if len(memories) != 1 {
		t.Fatalf("期望 1 个记忆（缺少字段的段落应被跳过），实际 %d", len(memories))
	}
	if memories[0].Memory[0].Title != "Complete" {
		t.Errorf("Title 期望 'Complete'，实际 %q", memories[0].Memory[0].Title)
	}
}

// TestMemoryItemParser_Parse_空响应 验证空输入返回空列表
func TestMemoryItemParser_Parse_空响应(t *testing.T) {
	parser := NewMemoryItemParser()
	memories := parser.Parse("", "q", nil)

	if len(memories) != 0 {
		t.Errorf("期望 0 个记忆，实际 %d", len(memories))
	}
}

// TestMemoryItemParser_CleanResponse 验证 ``` 围栏去除
func TestMemoryItemParser_CleanResponse(t *testing.T) {
	parser := NewMemoryItemParser()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "前后围栏",
			input:    "```\ncontent\n```",
			expected: "content",
		},
		{
			name:     "开头带语言标记",
			input:    "```markdown\ncontent\n```",
			expected: "content",
		},
		{
			name:     "无围栏",
			input:    "plain content",
			expected: "plain content",
		},
		{
			name:     "仅开头围栏",
			input:    "```\ncontent",
			expected: "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.cleanResponse(tt.input)
			if got != tt.expected {
				t.Errorf("期望 %q，实际 %q", tt.expected, got)
			}
		})
	}
}

// TestMemoryItemParser_SplitIntoSections 验证段落分割
func TestMemoryItemParser_SplitIntoSections(t *testing.T) {
	parser := NewMemoryItemParser()

	input := `# Memory Item 1
## Title T1

# Memory Item 2
## Title T2

# Memory Item 3
## Title T3`

	sections := parser.splitIntoSections(input)
	if len(sections) != 3 {
		t.Fatalf("期望 3 个段落，实际 %d", len(sections))
	}
}

// TestMemoryItemParser_ExtractMemoryItem 验证单个记忆项提取
func TestMemoryItemParser_ExtractMemoryItem(t *testing.T) {
	parser := NewMemoryItemParser()

	section := `## Title My Title
## Description My Description
## Content My Content`

	item := parser.extractMemoryItem(section)
	if item == nil {
		t.Fatal("期望非 nil，实际 nil")
	}
	if item.Title != "My Title" {
		t.Errorf("Title 期望 'My Title'，实际 %q", item.Title)
	}
	if item.Description != "My Description" {
		t.Errorf("Description 期望 'My Description'，实际 %q", item.Description)
	}
	if item.Content != "My Content" {
		t.Errorf("Content 期望 'My Content'，实际 %q", item.Content)
	}
}

// TestMemoryItemParser_ExtractMemoryItem_缺少字段 验证缺少字段时返回 nil
func TestMemoryItemParser_ExtractMemoryItem_缺少字段(t *testing.T) {
	parser := NewMemoryItemParser()

	// 缺少 Content
	section := `## Title My Title
## Description My Description`

	item := parser.extractMemoryItem(section)
	if item != nil {
		t.Error("缺少 Content 字段应返回 nil")
	}
}

// TestMemoryItemParser_ExtractField 验证字段提取
func TestMemoryItemParser_ExtractField(t *testing.T) {
	parser := NewMemoryItemParser()

	tests := []struct {
		name         string
		lines        []string
		startIdx     int
		fieldPattern string
		expectedVal  string
	}{
		{
			name:         "单行字段",
			lines:        []string{"## Title My Title", "## Description next"},
			startIdx:     0,
			fieldPattern: `##\s*Title\s+`,
			expectedVal:  "My Title",
		},
		{
			name:         "多行字段",
			lines:        []string{"## Content Line 1", "Line 2", "Line 3", "## Title next"},
			startIdx:     0,
			fieldPattern: `##\s*Content\s+`,
			expectedVal:  "Line 1 Line 2 Line 3",
		},
		{
			name:         "含空行跳过",
			lines:        []string{"## Content Start", "", "Middle", "## Title stop"},
			startIdx:     0,
			fieldPattern: `##\s*Content\s+`,
			expectedVal:  "Start Middle",
		},
		{
			name:         "含围栏行跳过",
			lines:        []string{"## Content Start", "```", "Middle", "```", "## Title stop"},
			startIdx:     0,
			fieldPattern: `##\s*Content\s+`,
			expectedVal:  "Start Middle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := parser.extractField(tt.lines, tt.startIdx, tt.fieldPattern)
			if got != tt.expectedVal {
				t.Errorf("期望 %q，实际 %q", tt.expectedVal, got)
			}
		})
	}
}

// TestMemoryItemParser_Parse_Content多行 验证 Content 字段跨越多行时正确拼接
func TestMemoryItemParser_Parse_Content多行(t *testing.T) {
	input := `# Memory Item 1
## Title Multi Line
## Description A multi-line content test.
## Content This is the first line.
This is the second line.
This is the third line.
`

	parser := NewMemoryItemParser()
	memories := parser.Parse(input, "q", nil)

	if len(memories) != 1 {
		t.Fatalf("期望 1 个记忆，实际 %d", len(memories))
	}
	item := memories[0].Memory[0]
	expected := "This is the first line. This is the second line. This is the third line."
	if item.Content != expected {
		t.Errorf("Content 期望 %q，实际 %q", expected, item.Content)
	}
}

// TestMemoryItemParser_Parse_Label为nil 验证 label 传 nil 时不崩溃
func TestMemoryItemParser_Parse_Label为nil(t *testing.T) {
	input := `# Memory Item 1
## Title T
## Description D
## Content C
`

	parser := NewMemoryItemParser()
	memories := parser.Parse(input, "q", nil)

	if len(memories) != 1 {
		t.Fatalf("期望 1 个记忆，实际 %d", len(memories))
	}
	if memories[0].Label != nil {
		t.Error("Label 期望 nil")
	}
}

// TestMemoryItemParser_Parse_ReasoningBankMemory结构 验证返回的 ReasoningBankMemory 结构完整
func TestMemoryItemParser_Parse_ReasoningBankMemory结构(t *testing.T) {
	input := `# Memory Item 1
## Title T
## Description D
## Content C
`

	parser := NewMemoryItemParser()
	label := false
	memories := parser.Parse(input, "my query", &label)

	if len(memories) != 1 {
		t.Fatalf("期望 1 个记忆，实际 %d", len(memories))
	}

	// 验证 ReasoningBankMemory 是 ceschema.ReasoningBankMemory 类型
	var _ *ceschema.ReasoningBankMemory = memories[0]
}
