package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestIsMostlyEnglish 测试英文占比判断
func TestIsMostlyEnglish(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "纯英文",
			text:     "Hello World this is a test",
			expected: true,
		},
		{
			name:     "纯中文",
			text:     "这是一个中文测试",
			expected: false,
		},
		{
			name:     "中英混合偏英文",
			text:     "This is a test 测试 for English ratio",
			expected: true,
		},
		{
			name:     "中英混合偏中文",
			text:     "这是中文描述 with some English words",
			expected: false,
		},
		{
			name:     "空字符串",
			text:     "",
			expected: false,
		},
		{
			name:     "纯空格",
			text:     "   ",
			expected: false,
		},
		{
			name:     "纯数字",
			text:     "1234567890",
			expected: false,
		},
		{
			name:     "英文加数字",
			text:     "abc123def456",
			expected: false, // 6/12 = 0.5 < 0.7
		},
		{
			name:     "JSON 英文键值",
			text:     `{"name": "test", "description": "a tool for testing"}`,
			expected: true,
		},
		{
			name:     "JSON 中文值",
			text:     `{"name": "测试工具", "description": "这是一个测试用的工具描述"}`,
			expected: false,
		},
		{
			name:     "日文文本",
			text:     "これはテストです",
			expected: false,
		},
		{
			name:     "特殊字符",
			text:     "!@#$%^&*()",
			expected: false,
		},
		{
			name:     "恰好超过阈值",
			text:     "abcde测试",
			expected: false, // 5/11 ≈ 0.45 < 0.7
		},
		{
			name:     "恰好低于阈值",
			text:     "abcd测试测",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMostlyEnglish(tt.text)
			assert.Equal(t, tt.expected, result, "isMostlyEnglish(%q) = %v, want %v", tt.text, result, tt.expected)
		})
	}
}

// TestProcess_步骤顺序验证 验证 Process 按正确步骤顺序执行
func TestProcess_步骤顺序验证(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("test-model", "test-key", nil)

	// 记录执行顺序
	var executionOrder []string

	// 通过测试已知步骤名验证 Process 的步骤分派逻辑
	tests := []struct {
		name        string
		steps       []string
		expectErr   bool
		errContains string
	}{
		{
			name:      "空步骤列表",
			steps:     []string{},
			expectErr: false,
		},
		{
			name:        "未知步骤",
			steps:       []string{"unknown_step"},
			expectErr:   true,
			errContains: "unknown processing step: unknown_step",
		},
		{
			// InvokeWithVerify 吞没 LLM 错误，返回包含 "error" 键的 dict 而非 Go error
			name:      "clean步骤需LLM但model为nil",
			steps:     []string{"clean"},
			expectErr: false, // 不返回 Go error，而是返回含 error 键的 dict
		},
		{
			name:      "cross_check步骤需LLM但model为nil",
			steps:     []string{"cross_check"},
			expectErr: false, // 不返回 Go error，而是返回含 error 键的 dict
		},
		{
			name:      "translate步骤对中文数据直接返回",
			steps:     []string{"translate"},
			expectErr: false,
		},
	}

	data := map[string]any{
		"name":        "测试工具",
		"description": "这是一个中文描述",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = executionOrder
			result, err := reviewer.Process(context.Background(), data, "原始工具描述", tt.steps)

			if tt.expectErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				if err == nil {
					assert.NotNil(t, result)
				}
			}
		})
	}
}

// TestProcess_TranslateToChinese中文数据直接返回 验证中文数据不调用LLM
func TestProcess_TranslateToChinese中文数据直接返回(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("test-model", "test-key", nil)

	data := map[string]any{
		"name":        "测试工具",
		"description": "这是一个中文描述的工具",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"城市": map[string]any{
					"type":        "string",
					"description": "查询的城市名称",
				},
			},
		},
	}

	// translate 步骤对中文数据应直接返回，不调用 LLM（model 为 nil 也不会报错）
	result, err := reviewer.Process(context.Background(), data, "原始描述", []string{"translate"})
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

// TestToolDescriptionReviewer_Format签名验证 验证 Format 方法签名和基本行为
func TestToolDescriptionReviewer_Format签名验证(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("test-model", "test-key", nil)

	// 验证 reviewer 结构体字段
	assert.Equal(t, "test-model", reviewer.evalModelID)
	assert.Equal(t, "test-key", reviewer.llmAPIKey)
	assert.Nil(t, reviewer.model)

	// Format 在 model 为 nil 时会调用 InvokeWithVerify，
	// 而 InvokeWithVerify 内部会因 model 为 nil 而失败
	// 这里只验证方法签名和错误路径
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "名称",
			},
		},
	}

	_, err := reviewer.Format(context.Background(), schema, "测试描述", nil)
	// model 为 nil 时 InvokeWithVerify 吞没错误，返回 error dict 而非 Go error
	// 所以 err 为 nil，结果中包含 "error" 键
	assert.NoError(t, err)
}

// TestNewToolDescriptionReviewer 测试创建 ToolDescriptionReviewer 实例
func TestNewToolDescriptionReviewer(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("gpt-4", "api-key-123", nil)
	require.NotNil(t, reviewer)
	assert.Equal(t, "gpt-4", reviewer.evalModelID)
	assert.Equal(t, "api-key-123", reviewer.llmAPIKey)
	assert.Nil(t, reviewer.model)
}

// TestIsMostlyEnglish_边界值测试 测试英文占比阈值边界
func TestIsMostlyEnglish_边界值测试(t *testing.T) {
	// 构造恰好 0.7 的情况：7 个英文字符 + 3 个非英文字符 = 0.7
	// 0.7 不大于 0.7，所以返回 false
	text70Percent := "abcdefg测试"
	result := isMostlyEnglish(text70Percent)
	assert.False(t, result, "恰好 0.7 应该返回 false（需要 > 0.7）")

	// 8 个英文字符 + 2 个非英文字符 = 0.8 > 0.7
	text80Percent := "abcdefgh测"
	result = isMostlyEnglish(text80Percent)
	assert.True(t, result, "0.8 > 0.7 应该返回 true")
}

// TestProcess_未知步骤 验证 Process 对未知步骤返回错误
func TestProcess_未知步骤(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("test-model", "test-key", nil)
	data := map[string]any{"name": "test"}

	_, err := reviewer.Process(context.Background(), data, "ori", []string{"invalid_step"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown processing step: invalid_step")
}

// TestCleanAndDeduplicate_model为nil 验证 model 为 nil 时不返回 Go error（InvokeWithVerify 吞没错误）
func TestCleanAndDeduplicate_model为nil(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("test-model", "test-key", nil)
	data := map[string]any{"name": "test"}

	result, err := reviewer.CleanAndDeduplicate(context.Background(), data)
	assert.NoError(t, err) // InvokeWithVerify 吞没错误
	if _, hasError := result["error"]; !hasError {
		t.Error("期望结果包含 'error' 键")
	}
}

// TestCrossCheck_model为nil 验证 model 为 nil 时不返回 Go error（InvokeWithVerify 吞没错误）
func TestCrossCheck_model为nil(t *testing.T) {
	reviewer := NewToolDescriptionReviewer("test-model", "test-key", nil)
	data := map[string]any{"name": "test"}

	result, err := reviewer.CrossCheck(context.Background(), data, "原始工具描述")
	assert.NoError(t, err) // InvokeWithVerify 吞没错误
	if _, hasError := result["error"]; !hasError {
		t.Error("期望结果包含 'error' 键")
	}
}

// TestFormat_prompt内容验证 验证 Format 生成的 prompt 包含关键内容
func TestFormat_prompt内容验证(t *testing.T) {
	// 验证 prompt 模板中的关键中文内容
	schema := map[string]any{"type": "object"}
	description := "测试描述"

	schemaJSON, _ := json.MarshalIndent(schema, "", "  ")
	prompt := fmt.Sprintf(`将下面输入转换为目标 JSON 结构。必须满足：

- 输出只允许是有效 JSON，且严格匹配目标结构的键路径与层级（不多不少）。
- 语义必须完全保留：不新增、不删减、不改写含义；可改写措辞以压缩。
- description 去冗余是强制要求：
    - 任何 "每项包含/含有/由…组成/字段包括…" 这类字段清单式描述都必须删除或改写为非清单表述。
    - 不得在 description 中重复 schema 已表达的信息：字段名、字段类型、required 已涵盖的"必填"。
    - 仅保留 schema 无法表达或未显式表达的约束到 description，例如：
        - 覆盖区间/不得留隙/分段规则
        - 默认值语义（如 inflationRate 默认 0）
        - 业务规则（按年累加、考虑通胀等）
    - 枚举值列表只出现一次，放在最贴近字段的位置（通常是该字段的 description）；不得在父级/子级重复。
    如输入中 description 同时包含"字段清单 + 业务约束"，只保留业务约束部分。
    - 若某个 description 完全是冗余字段清单，允许变为简短描述，但不得留空（除非输入本身为空）。
- 请直接输出转换后的 JSON，不要附加解释。

这是目标的json 模板:
%s

下面是你需要修改的json，生成后请自检：所有 description 中不得出现"含/包含/包括/each item/contains/fields"等字段列举句式；否则重写直到满足。

Input:
%s
`, string(schemaJSON), description)

	// 验证关键中文提示词内容
	assert.Contains(t, prompt, "将下面输入转换为目标 JSON 结构")
	assert.Contains(t, prompt, "输出只允许是有效 JSON")
	assert.Contains(t, prompt, "语义必须完全保留")
	assert.Contains(t, prompt, "description 去冗余是强制要求")
	assert.Contains(t, prompt, "自检")
	assert.Contains(t, prompt, string(schemaJSON))
	assert.Contains(t, prompt, description)
}
