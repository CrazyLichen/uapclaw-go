package lite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// validFrontmatterDoc 构造一个通过校验的记忆文档。
func validFrontmatterDoc(body string) string {
	return "---\nname: test-mem\ndescription: d\ntype: project\n---\n" + body
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCodingMemoryWriteWithContext_校验分支 验证写入前的各项校验。
func TestCodingMemoryWriteWithContext_校验分支(t *testing.T) {
	ctx := context.Background()

	t.Run("toolCtx 为 nil", func(t *testing.T) {
		res := CodingMemoryWriteWithContext(ctx, nil, "a.md", validFrontmatterDoc("body"))
		if res["success"] != false || res["error"] != "Workspace 未初始化" {
			t.Fatalf("res = %v", res)
		}
	})

	t.Run("Workspace 未初始化", func(t *testing.T) {
		res := CodingMemoryWriteWithContext(ctx, &CodingMemoryToolContext{}, "a.md", validFrontmatterDoc("body"))
		if res["success"] != false {
			t.Fatalf("res = %v", res)
		}
	})

	t.Run("路径校验失败", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryWriteWithContext(ctx, toolCtx, "../escape.md", validFrontmatterDoc("body"))
		if res["success"] != false {
			t.Fatalf("目录遍历应被拒，res = %v", res)
		}
	})

	t.Run("缺少 frontmatter", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryWriteWithContext(ctx, toolCtx, "a.md", "no frontmatter")
		if res["success"] != false || !strings.Contains(errString(res["error"]), "frontmatter") {
			t.Fatalf("res = %v", res)
		}
	})

	t.Run("frontmatter 字段不全", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryWriteWithContext(ctx, toolCtx, "a.md", "---\nname: n\n---\nbody")
		if res["success"] != false {
			t.Fatalf("res = %v", res)
		}
	})

	t.Run("type 非法", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryWriteWithContext(ctx, toolCtx, "a.md",
			"---\nname: n\ndescription: d\ntype: bogus\n---\nbody")
		if res["success"] != false || !strings.Contains(errString(res["error"]), "type") {
			t.Fatalf("res = %v", res)
		}
	})

	t.Run("无内容体", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryWriteWithContext(ctx, toolCtx, "a.md",
			"---\nname: n\ndescription: d\ntype: project\n---\n")
		if res["success"] != false {
			t.Fatalf("res = %v", res)
		}
	})
}

// TestCodingMemoryWriteWithContext_写入成功 验证合法输入真的落盘。
func TestCodingMemoryWriteWithContext_写入成功(t *testing.T) {
	ctx := context.Background()
	toolCtx, memDir := newReadCtx(t)

	res := CodingMemoryWriteWithContext(ctx, toolCtx, "written.md", validFrontmatterDoc("hello body"))
	if res["success"] != true {
		t.Fatalf("应写入成功，res = %v", res)
	}

	data, err := os.ReadFile(filepath.Join(memDir, "written.md"))
	if err != nil {
		t.Fatalf("读取写入文件失败: %v", err)
	}
	if !strings.Contains(string(data), "hello body") {
		t.Fatalf("文件内容应含 body，实际 %q", string(data))
	}
}

// TestCodingMemoryEditWithContext_校验分支 验证编辑前的各项校验。
func TestCodingMemoryEditWithContext_校验分支(t *testing.T) {
	ctx := context.Background()

	t.Run("oldText 为空", func(t *testing.T) {
		res := CodingMemoryEditWithContext(ctx, nil, "a.md", "", "new")
		if res.Error != "old_text 不能为空" {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("toolCtx 为 nil", func(t *testing.T) {
		res := CodingMemoryEditWithContext(ctx, nil, "a.md", "old", "new")
		if res.Error != "Workspace 未初始化" {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("Workspace 未初始化", func(t *testing.T) {
		res := CodingMemoryEditWithContext(ctx, &CodingMemoryToolContext{}, "a.md", "old", "new")
		if res.Error != "Workspace 未初始化" {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("路径校验失败", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryEditWithContext(ctx, toolCtx, "/abs.md", "old", "new")
		if res.Error == "" {
			t.Fatal("绝对路径应被拒")
		}
	})

	t.Run("无 SysOperation", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		toolCtx.SysOperation = nil
		res := CodingMemoryEditWithContext(ctx, toolCtx, "a.md", "old", "new")
		if res.Error != "无可用 sys_operation" {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("文件不存在", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryEditWithContext(ctx, toolCtx, "missing.md", "old", "new")
		if res.Error == "" {
			t.Fatal("文件不存在应报错")
		}
	})
}

// TestCodingMemoryEditWithContext_编辑语义 验证未找到/多次出现/成功替换。
func TestCodingMemoryEditWithContext_编辑语义(t *testing.T) {
	ctx := context.Background()
	toolCtx, memDir := newReadCtx(t)
	path := filepath.Join(memDir, "edit.md")
	if err := os.WriteFile(path, []byte(validFrontmatterDoc("alpha beta")), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	t.Run("old_text 未找到", func(t *testing.T) {
		res := CodingMemoryEditWithContext(ctx, toolCtx, "edit.md", "not-present", "x")
		if res.Error == "" || !strings.Contains(res.Error, "未找到") {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("old_text 出现多次被拒", func(t *testing.T) {
		if err := os.WriteFile(path, []byte(validFrontmatterDoc("dup dup")), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		res := CodingMemoryEditWithContext(ctx, toolCtx, "edit.md", "dup", "x")
		if res.Error == "" || !strings.Contains(res.Error, "出现 2 次") {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("唯一匹配时成功替换", func(t *testing.T) {
		if err := os.WriteFile(path, []byte(validFrontmatterDoc("alpha beta")), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		res := CodingMemoryEditWithContext(ctx, toolCtx, "edit.md", "alpha", "gamma")
		if !res.Success {
			t.Fatalf("应成功，Error = %q", res.Error)
		}
		if !strings.Contains(res.NewContent, "gamma beta") {
			t.Fatalf("NewContent = %q", res.NewContent)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "gamma beta") {
			t.Fatalf("落盘内容不符: %q", string(data))
		}
	})
}

// errString 把 error 字段归一为字符串（可能是 error 或 string）。
func errString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case error:
		return val.Error()
	default:
		return ""
	}
}
