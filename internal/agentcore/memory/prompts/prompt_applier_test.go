package prompts

import (
	"os"
	"path/filepath"
	"testing"
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

// ──────────────────────────── 非导出函数 ────────────────────────────
