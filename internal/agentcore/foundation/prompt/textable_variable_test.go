package prompt

import (
	"testing"
)

// ────────────────────── 构造测试 ──────────────────────

// TestTextableVariable_SinglePlaceholder 验证单个占位符解析。
func TestTextableVariable_SinglePlaceholder(t *testing.T) {
	v, err := NewTextableVariable("{{x}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if len(v.InputKeys()) != 1 || v.InputKeys()[0] != "x" {
		t.Errorf("InputKeys() = %v, want [x]", v.InputKeys())
	}
	if len(v.Placeholders()) != 1 || v.Placeholders()[0] != "x" {
		t.Errorf("Placeholders() = %v, want [x]", v.Placeholders())
	}
	if v.Name() != "default" {
		t.Errorf("Name() = %q, want %q", v.Name(), "default")
	}
}

// TestTextableVariable_MultiplePlaceholders 验证多个占位符。
func TestTextableVariable_MultiplePlaceholders(t *testing.T) {
	v, err := NewTextableVariable("{{x}}{{y}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if len(v.InputKeys()) != 2 {
		t.Errorf("InputKeys() = %v, want len 2", v.InputKeys())
	}
	if len(v.Placeholders()) != 2 {
		t.Errorf("Placeholders() = %v, want len 2", v.Placeholders())
	}
}

// TestTextableVariable_EmptyPlaceholder 验证空占位符返回错误。
func TestTextableVariable_EmptyPlaceholder(t *testing.T) {
	_, err := NewTextableVariable("{{}}", "default")
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestTextableVariable_NestedPlaceholder 验证嵌套属性占位符。
func TestTextableVariable_NestedPlaceholder(t *testing.T) {
	v, err := NewTextableVariable("Hello, {{user.name}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if len(v.InputKeys()) != 1 || v.InputKeys()[0] != "user" {
		t.Errorf("InputKeys() = %v, want [user]", v.InputKeys())
	}
	if len(v.Placeholders()) != 1 || v.Placeholders()[0] != "user.name" {
		t.Errorf("Placeholders() = %v, want [user.name]", v.Placeholders())
	}
}

// TestTextableVariable_CustomPrefixSuffix 验证自定义前后缀。
func TestTextableVariable_CustomPrefixSuffix(t *testing.T) {
	v, err := NewTextableVariable("${domain}$", "default",
		WithPrefix("${"), WithSuffix("}$"))
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if len(v.InputKeys()) != 1 || v.InputKeys()[0] != "domain" {
		t.Errorf("InputKeys() = %v, want [domain]", v.InputKeys())
	}
}

// TestTextableVariable_EmptyName 验证空名称使用默认值。
func TestTextableVariable_EmptyName(t *testing.T) {
	v, err := NewTextableVariable("{{x}}", "")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if v.Name() != defaultVarName {
		t.Errorf("Name() = %q, want %q", v.Name(), defaultVarName)
	}
}

// ────────────────────── Update 测试 ──────────────────────

// TestTextableVariable_Update_Normal 验证正常占位符替换。
func TestTextableVariable_Update_Normal(t *testing.T) {
	v, err := NewTextableVariable("You're an expert in the domain of {{domain}}.", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	v.Update(map[string]any{"domain": "science"})
	if v.Value() != "You're an expert in the domain of science." {
		t.Errorf("Value() = %q, want替换后文本", v.Value())
	}
}

// TestTextableVariable_Update_Numeric 验证数值类型替换。
func TestTextableVariable_Update_Numeric(t *testing.T) {
	v, err := NewTextableVariable("This value is {{value}}.", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	v.Update(map[string]any{"value": 42})
	if v.Value() != "This value is 42." {
		t.Errorf("Value() = %q, want 'This value is 42.'", v.Value())
	}
}

// TestTextableVariable_Update_Nested 验证嵌套属性替换。
func TestTextableVariable_Update_Nested(t *testing.T) {
	v, err := NewTextableVariable("Hello, {{user.name}}!", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	v.Update(map[string]any{"user": map[string]any{"name": "Alice"}})
	if v.Value() != "Hello, Alice!" {
		t.Errorf("Value() = %q, want 'Hello, Alice!'", v.Value())
	}
}

// TestTextableVariable_Update_Bool 验证布尔类型替换。
func TestTextableVariable_Update_Bool(t *testing.T) {
	v, err := NewTextableVariable("Flag: {{flag}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	v.Update(map[string]any{"flag": true})
	if v.Value() != "Flag: true" {
		t.Errorf("Value() = %q, want 'Flag: true'", v.Value())
	}
}

// ────────────────────── Eval 测试 ──────────────────────

// TestTextableVariable_Eval_Normal 验证 Eval 正常求值。
func TestTextableVariable_Eval_Normal(t *testing.T) {
	v, err := NewTextableVariable("You're an expert in the domain of {{domain}}.", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"domain": "science"})
	if result != "You're an expert in the domain of science." {
		t.Errorf("Eval() = %q, want替换后文本", result)
	}
}

// TestTextableVariable_Eval_Nested 验证 Eval 嵌套属性求值。
func TestTextableVariable_Eval_Nested(t *testing.T) {
	v, err := NewTextableVariable("Hello, {{user.name}}!", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"user": map[string]any{"name": "Alice"}})
	if result != "Hello, Alice!" {
		t.Errorf("Eval() = %q, want 'Hello, Alice!'", result)
	}
}

// TestTextableVariable_Eval_Multiple 验证 Eval 多占位符求值。
func TestTextableVariable_Eval_Multiple(t *testing.T) {
	v, err := NewTextableVariable("{{greeting}}, {{user.name}}! You have {{count}} messages.", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{
		"greeting": "Hi",
		"user":     map[string]any{"name": "Bob"},
		"count":    3,
	})
	if result != "Hi, Bob! You have 3 messages." {
		t.Errorf("Eval() = %q, want 'Hi, Bob! You have 3 messages.'", result)
	}
}

// TestTextableVariable_Eval_FilterExtraKeys 验证 Eval 过滤无关参数。
func TestTextableVariable_Eval_FilterExtraKeys(t *testing.T) {
	v, err := NewTextableVariable("Hi {{name}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"name": "Bob", "age": 20})
	if result != "Hi Bob" {
		t.Errorf("Eval() = %q, want 'Hi Bob'", result)
	}
}

// ────────────────────── 自定义前后缀 Eval 测试 ──────────────────────

// TestTextableVariable_Eval_CustomDelimiters 验证自定义前后缀的 Eval。
func TestTextableVariable_Eval_CustomDelimiters(t *testing.T) {
	v, err := NewTextableVariable("你是一个精通${domain}$领域的问答助手。", "default",
		WithPrefix("${"), WithSuffix("}$"))
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"domain": "科学"})
	if result != "你是一个精通科学领域的问答助手。" {
		t.Errorf("Eval() = %q, want替换后文本", result)
	}
}

// TestTextableVariable_Eval_CurlyBraceDelimiters 验证 {} 格式占位符。
func TestTextableVariable_Eval_CurlyBraceDelimiters(t *testing.T) {
	v, err := NewTextableVariable("你是一个精通{domain}领域的问答助手。", "default",
		WithPrefix("{"), WithSuffix("}"))
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"domain": "天文"})
	if result != "你是一个精通天文领域的问答助手。" {
		t.Errorf("Eval() = %q, want替换后文本", result)
	}
}

// ────────────────────── 类型覆盖测试 ──────────────────────

// TestTextableVariable_Eval_Float64 验证 float64 类型替换。
func TestTextableVariable_Eval_Float64(t *testing.T) {
	v, err := NewTextableVariable("Price: {{price}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"price": 3.14})
	if result != "Price: 3.14" {
		t.Errorf("Eval() = %q, want 'Price: 3.14'", result)
	}
}

// TestTextableVariable_Eval_Int64 验证 int64 类型替换。
func TestTextableVariable_Eval_Int64(t *testing.T) {
	v, err := NewTextableVariable("Count: {{count}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"count": int64(100)})
	if result != "Count: 100" {
		t.Errorf("Eval() = %q, want 'Count: 100'", result)
	}
}

// TestTextableVariable_Eval_Float32 验证 float32 类型替换。
func TestTextableVariable_Eval_Float32(t *testing.T) {
	v, err := NewTextableVariable("Rate: {{rate}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"rate": float32(1.5)})
	if result != "Rate: 1.5" {
		t.Errorf("Eval() = %q, want 'Rate: 1.5'", result)
	}
}

// TestTextableVariable_Eval_NonPrimitiveType 验证非基本类型（如 slice）的 str() 转换。
func TestTextableVariable_Eval_NonPrimitiveType(t *testing.T) {
	v, err := NewTextableVariable("Items: {{items}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	result := v.Eval(map[string]any{"items": []string{"a", "b"}})
	if result != "Items: [a b]" {
		t.Errorf("Eval() = %q, want 'Items: [a b]'", result)
	}
}

// TestTextableVariable_Update_MissingNestedKey 验证 Update 中嵌套键缺失时保留占位符。
func TestTextableVariable_Update_MissingNestedKey(t *testing.T) {
	v, err := NewTextableVariable("Hello {{user.name}}!", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	// 传入不包含 name 的 user，解析失败，保留占位符
	v.Update(map[string]any{"user": map[string]any{"age": 30}})
	if v.Value() != "Hello {{user.name}}!" {
		t.Errorf("Value() = %q, want保留占位符", v.Value())
	}
}

// TestTextableVariable_TextGetter 验证 Text() getter。
func TestTextableVariable_TextGetter(t *testing.T) {
	v, err := NewTextableVariable("Hello {{name}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if v.Text() != "Hello {{name}}" {
		t.Errorf("Text() = %q, want 'Hello {{name}}'", v.Text())
	}
}

// TestTextableVariable_DuplicatePlaceholder 验证重复占位符去重。
func TestTextableVariable_DuplicatePlaceholder(t *testing.T) {
	v, err := NewTextableVariable("{{name}} and {{name}}", "default")
	if err != nil {
		t.Fatalf("NewTextableVariable 失败: %v", err)
	}
	if len(v.Placeholders()) != 1 {
		t.Errorf("Placeholders() = %v, want len 1 (去重)", v.Placeholders())
	}
	result := v.Eval(map[string]any{"name": "Alice"})
	if result != "Alice and Alice" {
		t.Errorf("Eval() = %q, want 'Alice and Alice'", result)
	}
}
