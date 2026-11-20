package evaluationresultanalyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
)

// TestBoundedToolCalls_投影 对照 Python _bounded_tool_calls 全分支。
// Python: case_reader.py:346-362
func TestBoundedToolCalls_投影(t *testing.T) {
	if got := boundedToolCalls("not-a-list"); got != nil {
		t.Fatalf("非清单应 nil, got %v", got)
	}
	value := []any{}
	for i := 0; i < 10; i++ {
		value = append(value, map[string]any{
			"name": "tool" + itoaOf(i), "input": strings.Repeat("i", 1500),
			"output": strings.Repeat("o", 1500), "error": strings.Repeat("e", 1500),
			"step_pointer": "s",
		})
	}
	value = append(value, "junk")
	got := boundedToolCalls(value)
	if len(got) != 8 {
		t.Fatalf("应截断 8, got %d", len(got))
	}
	if len(got[0]["input"].(string)) != 1200 {
		t.Fatalf("input 应截断 1200, got %d", len(got[0]["input"].(string)))
	}
	if got[0]["name"] != "tool0" {
		t.Fatalf("name 不符: %v", got[0]["name"])
	}
}

// TestSpecToMap_结构转映 对照 Python spec.model_dump() 键面。
func TestSpecToMap_结构转映(t *testing.T) {
	spec := hschema.PromptSectionSpec{
		Name:         "main",
		Content:      map[string]string{"cn": "正文", "en": "body"},
		Priority:     90,
		RenderParams: map[string]any{"k": "v"},
	}
	got := specToMap(spec)
	if got["name"] != "main" || got["priority"] != 90 {
		t.Fatalf("键面不符: %v", got)
	}
	if got["content"].(map[string]any)["cn"] != "正文" {
		t.Fatalf("content 不符: %v", got["content"])
	}
	if got["render_params"].(map[string]any)["k"] != "v" {
		t.Fatalf("render_params 不符: %v", got["render_params"])
	}
	// 零值字段省略（model_dump 语义近似）
	minimal := specToMap(hschema.PromptSectionSpec{Name: "x", Content: map[string]string{"cn": "c"}})
	if _, has := minimal["priority"]; has {
		t.Fatal("零 priority 不应出现")
	}
	if _, has := minimal["render_params"]; has {
		t.Fatal("nil render_params 不应出现")
	}
}

// TestValidateDiagnosisPaths_平台门卫 覆盖 GOOS 门卫真实路径。
func TestValidateDiagnosisPaths_平台门卫(t *testing.T) {
	// 真实入口（当前平台非 Windows 时直通）
	if err := ValidateDiagnosisPaths("/tmp/rt", "/tmp/att", "", ""); err != nil && !isWindowsGOOS() {
		t.Fatalf("非 Windows 不应报错: %v", err)
	}
	if isWindowsGOOS() {
		t.Skip("Windows 平台语义由注入测试覆盖")
	}
}

// TestRepositorySnapshot_Symlink跟随 对照 Python symlinks=False（跟随拷贝）。
func TestRepositorySnapshot_Symlink跟随(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	ws := filepath.Join(caseRoot, "ws")
	if err := os.MkdirAll(filepath.Join(ws, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "real", "content.txt"), []byte("via-link"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "real"), filepath.Join(ws, "link")); err != nil {
		t.Skip("symlink 不可用")
	}
	manifest, snapErr := PrepareRepositorySnapshot(ws, "", runtimeDir, "")
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if manifest["repository"] != "complete" {
		t.Fatalf("symlink 跟随拷贝应 complete: %v", manifest)
	}
	// symlinks=False：按目标内容拷成真实文件/目录
	if _, err := os.Stat(filepath.Join(runtimeDir, "repository", "link", "content.txt")); err != nil {
		t.Fatalf("链接目标内容应可经 link 路径访问: %v", err)
	}
}

// TestResolveLinkTarget_相对目标 覆盖相对链接解析分支。
func TestResolveLinkTarget_相对目标(t *testing.T) {
	// Python resolve() 归一 ".."：/tmp/ws/../other/x.txt → /tmp/other/x.txt（Go 一致）
	abs, err := resolveLinkTarget("/tmp/ws/link", "../other/x.txt")
	if err != nil || abs != "/tmp/other/x.txt" {
		t.Fatalf("相对目标解析不符: %s %v", abs, err)
	}
	abs2, err2 := resolveLinkTarget("/tmp/ws/link", "/abs/x")
	if err2 != nil || abs2 != "/abs/x" {
		t.Fatalf("绝对目标应原样: %s %v", abs2, err2)
	}
}
