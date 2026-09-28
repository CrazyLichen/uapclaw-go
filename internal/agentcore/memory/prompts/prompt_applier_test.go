package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewPromptApplier 测试构造函数
func TestNewPromptApplier(t *testing.T) {
	dir := t.TempDir()
	applier := NewPromptApplier(dir)
	if applier == nil {
		t.Fatal("NewPromptApplier 返回 nil")
	}
	if applier.promptDir != dir {
		t.Errorf("promptDir = %q, want %q", applier.promptDir, dir)
	}
}

// TestPromptApplier_Apply_缓存命中 测试缓存命中时不再读文件
func TestPromptApplier_Apply_缓存命中(t *testing.T) {
	dir := t.TempDir()
	templateContent := "Hello {{name}}, welcome!"
	err := os.WriteFile(filepath.Join(dir, "greeting.md"), []byte(templateContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	applier := NewPromptApplier(dir)

	// 第一次调用 — 加载并缓存
	result1, err := applier.Apply("greeting", map[string]any{"name": "World"})
	if err != nil {
		t.Fatal(err)
	}
	if result1 != "Hello World, welcome!" {
		t.Errorf("result1 = %q, want %q", result1, "Hello World, welcome!")
	}

	// 删除文件后再次调用 — 应从缓存返回
	os.Remove(filepath.Join(dir, "greeting.md"))
	result2, err := applier.Apply("greeting", map[string]any{"name": "Go"})
	if err != nil {
		t.Fatal(err)
	}
	if result2 != "Hello Go, welcome!" {
		t.Errorf("result2 = %q, want %q", result2, "Hello Go, welcome!")
	}
}

// TestPromptApplier_Apply_文件不存在 测试文件不存在时返回错误
func TestPromptApplier_Apply_文件不存在(t *testing.T) {
	dir := t.TempDir()
	applier := NewPromptApplier(dir)

	_, err := applier.Apply("nonexistent", nil)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

// TestPromptApplier_ClearCache_全部清除 测试清除所有缓存
func TestPromptApplier_ClearCache_全部清除(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("{{x}}"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("{{y}}"), 0644)

	applier := NewPromptApplier(dir)
	applier.Apply("a", map[string]any{"x": "1"})
	applier.Apply("b", map[string]any{"y": "2"})

	applier.ClearCache()

	// 清除缓存后删除文件，应报错（证明缓存已清除）
	os.Remove(filepath.Join(dir, "a.md"))
	_, err := applier.Apply("a", nil)
	if err == nil {
		t.Error("清除缓存后应重新加载文件，文件已删应报错")
	}
}

// TestPromptApplier_ClearCache_指定前缀 测试清除指定前缀的缓存
func TestPromptApplier_ClearCache_指定前缀(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("{{x}}"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("{{y}}"), 0644)

	applier := NewPromptApplier(dir)
	applier.Apply("a", map[string]any{"x": "1"})
	applier.Apply("b", map[string]any{"y": "2"})

	applier.ClearCache("a")

	// a 的缓存被清除，删除文件后应报错
	os.Remove(filepath.Join(dir, "a.md"))
	_, err := applier.Apply("a", nil)
	if err == nil {
		t.Error("指定前缀清除后应重新加载文件，文件已删应报错")
	}

	// b 的缓存仍在，删除文件后应正常
	os.Remove(filepath.Join(dir, "b.md"))
	result, err := applier.Apply("b", map[string]any{"y": "3"})
	if err != nil {
		t.Errorf("b 缓存未被清除，应返回成功: %v", err)
	}
	if result != "3" {
		t.Errorf("result = %q, want %q", result, "3")
	}
}

// TestPromptApplier_GetTemplate 测试获取模板
func TestPromptApplier_GetTemplate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("{{var}}"), 0644)

	applier := NewPromptApplier(dir)
	tmpl, err := applier.GetTemplate("test")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "test" {
		t.Errorf("tmpl.Name = %q, want %q", tmpl.Name, "test")
	}
}

// TestDefaultApplier_单例 测试全局单例
func TestDefaultApplier_单例(t *testing.T) {
	a1 := DefaultApplier()
	a2 := DefaultApplier()
	if a1 != a2 {
		t.Error("DefaultApplier 应返回同一实例")
	}
}

// TestPromptApplier_Apply_实际模板 测试实际 .md 模板文件加载
func TestPromptApplier_Apply_实际模板(t *testing.T) {
	// 使用 DefaultApplier（指向实际 prompts 目录）
	applier := DefaultApplier()
	applier.ClearCache()

	result, err := applier.Apply("memory_update_check", map[string]any{
		"old_information": "1: 用户喜欢阅读",
		"new_information": "2: 用户喜欢编程",
	})
	if err != nil {
		t.Fatalf("加载 memory_update_check.md 失败: %v", err)
	}
	if result == "" {
		t.Error("应用模板后结果不应为空")
	}
}

// TestPromptApplier_Apply_所有实际模板 测试全部 4 个 .md 模板都能正确加载
func TestPromptApplier_Apply_所有实际模板(t *testing.T) {
	applier := DefaultApplier()
	applier.ClearCache()

	cases := []struct {
		name       string
		filePrefix string
		vars       map[string]any
	}{
		{
			name:       "fragment_memory_prompt",
			filePrefix: "fragment_memory_prompt",
			vars: map[string]any{
				"reference_messages":         "参考消息内容",
				"conversation_time":          "2026-01-01",
				"current_week":               "2026.01.01~2026.01.07",
				"input_messages":             "目标消息内容",
				"user_profile_definition":    "用户画像定义",
				"semantic_memory_definition": "语义记忆定义",
				"episodic_memory_definition": "情景记忆定义",
			},
		},
		{
			name:       "memory_analysis_prompt",
			filePrefix: "memory_analysis_prompt",
			vars: map[string]any{
				"history":                    "历史消息",
				"conversation":               "当前消息",
				"has_variable":               true,
				"variables_define_template":  "[]",
				"variables_output_template":  "[]",
				"forbidden_variables":        "None",
				"max_message_token":          100,
				"user_profile_definition":    "用户画像定义",
				"semantic_memory_definition": "语义记忆定义",
				"episodic_memory_definition": "情景记忆定义",
			},
		},
		{
			name:       "memory_update_check",
			filePrefix: "memory_update_check",
			vars: map[string]any{
				"old_information": "1: 旧记忆",
				"new_information": "2: 新记忆",
			},
		},
		{
			name:       "semantic_validation",
			filePrefix: "semantic_validation",
			vars: map[string]any{
				"obtained_mem": "待判断信息",
				"old_mem":      "参考信息",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applier.ClearCache()
			result, err := applier.Apply(tc.filePrefix, tc.vars)
			if err != nil {
				t.Fatalf("加载 %s.md 失败: %v", tc.filePrefix, err)
			}
			if result == "" {
				t.Errorf("应用模板 %s 后结果不应为空", tc.filePrefix)
			}
		})
	}
}

// TestPromptApplier_Apply_缓存中存入消息列表模板 测试 Content 为 []BaseMessage 时 Apply 返回错误
func TestPromptApplier_Apply_缓存中存入消息列表模板(t *testing.T) {
	applier := NewPromptApplier(t.TempDir())

	// 手动向缓存中注入一个 Content 为 []BaseMessage 的模板
	msgTemplate := prompt.NewPromptTemplate("msgtype", []schema.BaseMessage{
		schema.NewUserMessage("hello"),
	})
	applier.cache.Store("msgtype", msgTemplate)

	_, err := applier.Apply("msgtype", map[string]any{})
	if err == nil {
		t.Error("Content 为消息列表时应返回错误，实际返回 nil")
	}
}

// TestPromptApplier_Apply_缓存中存入消息列表含nil 测试 Content 为含 nil 消息的列表时 Format 错误传播
func TestPromptApplier_Apply_缓存中存入消息列表含nil(t *testing.T) {
	applier := NewPromptApplier(t.TempDir())

	// 手动向缓存中注入一个含 nil 元素的 []BaseMessage 模板
	// deepCopyMessages 中 nil 消息会导致 Format 失败
	msgs := []schema.BaseMessage{nil, schema.NewUserMessage("hello")}
	msgTemplate := prompt.NewPromptTemplate("nilmsg", msgs)
	applier.cache.Store("nilmsg", msgTemplate)

	_, err := applier.Apply("nilmsg", map[string]any{"x": "v"})
	if err == nil {
		t.Error("含 nil 消息的模板 Format 应返回错误，实际返回 nil")
	}
}

// TestPromptApplier_GetTemplate_文件不存在 测试 GetTemplate 文件不存在
func TestPromptApplier_GetTemplate_文件不存在(t *testing.T) {
	dir := t.TempDir()
	applier := NewPromptApplier(dir)

	_, err := applier.GetTemplate("nonexistent")
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

// TestPromptApplier_Apply_无变量 测试不带变量时 Apply 返回原始模板
func TestPromptApplier_Apply_无变量(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "plain.md"), []byte("没有占位符的内容"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	applier := NewPromptApplier(dir)
	result, err := applier.Apply("plain", nil)
	if err != nil {
		t.Fatalf("Apply 返回错误: %v", err)
	}
	if result != "没有占位符的内容" {
		t.Errorf("result = %q, want %q", result, "没有占位符的内容")
	}
}

// TestPromptApplier_Apply_部分变量 测试部分变量替换
func TestPromptApplier_Apply_部分变量(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "partial.md"), []byte("你好 {{name}}，今天是{{day}}"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	applier := NewPromptApplier(dir)
	result, err := applier.Apply("partial", map[string]any{"name": "世界"})
	if err != nil {
		t.Fatalf("Apply 返回错误: %v", err)
	}
	// name 被替换，day 保留原始占位符
	if result != "你好 世界，今天是{{day}}" {
		t.Errorf("result = %q, want %q", result, "你好 世界，今天是{{day}}")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
