package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeDist 在 dir 下写一个最小 dist（index.html + assets/app.js）。
func writeDist(t *testing.T, dir, indexContent string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("创建 dist 目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("写 index.html 失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatalf("写 assets/app.js 失败：%v", err)
	}
	return dir
}

// readIndex 读取来源里的 index.html 内容。
func readIndex(t *testing.T, src DistSource) string {
	t.Helper()
	data, err := fs.ReadFile(src.FS, "index.html")
	if err != nil {
		t.Fatalf("读 index.html 失败：%v", err)
	}
	return string(data)
}

// TestResolveDist_显式目录优先 显式目录存在时直接用它（对齐 Python --dist）。
// Python: app_web.py L985-991
func TestResolveDist_显式目录优先(t *testing.T) {
	root := t.TempDir()
	explicit := writeDist(t, filepath.Join(t.TempDir(), "mydist"), "<html>explicit</html>")

	src, err := resolveDist(root, explicit)
	if err != nil {
		t.Fatalf("resolveDist 返回错误：%v", err)
	}
	if src.Embedded {
		t.Fatal("显式目录不应标记为内嵌")
	}
	if src.Path != explicit {
		t.Errorf("Path = %q，期望 %q", src.Path, explicit)
	}
	if got := readIndex(t, src); !strings.Contains(got, "explicit") {
		t.Errorf("index.html = %q，期望含 explicit", got)
	}
}

// TestResolveDist_显式目录不存在报错 显式指定错目录必须失败（对齐 Python SystemExit）。
func TestResolveDist_显式目录不存在报错(t *testing.T) {
	_, err := resolveDist(t.TempDir(), filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("目录不存在时应返回错误")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Errorf("错误文案 = %q，期望含「不存在」", err.Error())
	}
}

// TestResolveDist_显式路径不是目录报错 显式指定文件路径必须失败。
func TestResolveDist_显式路径不是目录报错(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备文件失败：%v", err)
	}
	_, err := resolveDist(t.TempDir(), f)
	if err == nil {
		t.Fatal("路径不是目录时应返回错误")
	}
	if !strings.Contains(err.Error(), "不是目录") {
		t.Errorf("错误文案 = %q，期望含「不是目录」", err.Error())
	}
}

// TestResolveDist_用户工作区优先于内嵌 数据根下的 dist 存在时用它（Python Priority 1）。
// Python: app_web.py L56-60
func TestResolveDist_用户工作区优先于内嵌(t *testing.T) {
	root := t.TempDir()
	userDist := writeDist(t, filepath.Join(root, "channels", "web", "frontend", "dist"), "<html>user</html>")

	src, err := resolveDist(root, "")
	if err != nil {
		t.Fatalf("resolveDist 返回错误：%v", err)
	}
	if src.Embedded {
		t.Fatal("用户工作区存在 dist 时不应回落内嵌")
	}
	if src.Path != userDist {
		t.Errorf("Path = %q，期望 %q", src.Path, userDist)
	}
	if got := readIndex(t, src); !strings.Contains(got, "user") {
		t.Errorf("index.html = %q，期望含 user", got)
	}
}

// TestResolveDist_用户目录不存在回落内嵌 数据根下无 dist 时用编译期内嵌资源。
// Python: app_web.py L61-66（包内 dist 兜底）；Go 为 go:embed
func TestResolveDist_用户目录不存在回落内嵌(t *testing.T) {
	src, err := resolveDist(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolveDist 返回错误：%v", err)
	}
	if !src.Embedded {
		t.Fatal("数据根无 dist 时应回落内嵌资源")
	}
	if src.Path != "" {
		t.Errorf("内嵌来源 Path = %q，期望空", src.Path)
	}
	if _, err := fs.ReadFile(src.FS, "index.html"); err != nil {
		t.Errorf("内嵌资源应含 index.html：%v", err)
	}
}

// TestIsPlaceholderDist 占位判定的三种输入。
func TestIsPlaceholderDist(t *testing.T) {
	placeholder := writeDist(t, t.TempDir(), "<p>"+placeholderMarker+"</p>")
	if !isPlaceholderDist(os.DirFS(placeholder)) {
		t.Error("占位页应判定为 true")
	}

	real := writeDist(t, t.TempDir(), "<html><script src=\"/assets/app.js\"></script></html>")
	if isPlaceholderDist(os.DirFS(real)) {
		t.Error("真实构建产物应判定为 false")
	}

	empty := t.TempDir()
	if isPlaceholderDist(os.DirFS(empty)) {
		t.Error("无 index.html 时应判定为 false")
	}
}

// TestDistSource_SPAHandler命中静态文件 命中真实文件时原样返回。
func TestDistSource_SPAHandler命中静态文件(t *testing.T) {
	dir := writeDist(t, t.TempDir(), "<html>idx</html>")
	h := DistSource{FS: os.DirFS(dir), Path: dir}.SPAHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("响应体 = %q，期望含 console.log", rec.Body.String())
	}
}

// TestDistSource_SPAHandler未知路径回退index SPA 路由回退到 index.html。
func TestDistSource_SPAHandler未知路径回退index(t *testing.T) {
	dir := writeDist(t, t.TempDir(), "<html>idx</html>")
	h := DistSource{FS: os.DirFS(dir), Path: dir}.SPAHandler()

	for _, path := range []string{"/", "/chat/room/1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s 状态码 = %d，期望 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "idx") {
			t.Errorf("%s 响应体 = %q，期望回退 index.html", path, rec.Body.String())
		}
		if req.URL.Path != path {
			t.Errorf("%s 请求对象被改写为 %s（不应改动调用方请求）", path, req.URL.Path)
		}
	}
}

// TestDistSource_SPAHandler assets缺失不回退 assets 前缀未命中时保持 404，不回退 index。
func TestDistSource_SPAHandler_assets缺失不回退(t *testing.T) {
	dir := writeDist(t, t.TempDir(), "<html>idx</html>")
	h := DistSource{FS: os.DirFS(dir), Path: dir}.SPAHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d，期望 404（assets 前缀不回退）", rec.Code)
	}
}

// TestSPAHandler_环境变量无效时回落内嵌 显式目录无效不 panic，回落内嵌资源。
func TestSPAHandler_环境变量无效时回落内嵌(t *testing.T) {
	t.Setenv(DistDirEnv, filepath.Join(t.TempDir(), "missing"))
	h := SPAHandler() // 不应 panic

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200（回落内嵌）", rec.Code)
	}
}

// TestResolveDist_用户目录为占位页时仍使用 占位 dist 存在时照用（只打 Warn，不拦截）。
// 对齐 Python：_default_dist_dir 只判存在，不校验内容
func TestResolveDist_用户目录为占位页时仍使用(t *testing.T) {
	root := t.TempDir()
	userDist := writeDist(t, filepath.Join(root, "channels", "web", "frontend", "dist"), "<p>"+placeholderMarker+"</p>")

	src, err := resolveDist(root, "")
	if err != nil {
		t.Fatalf("resolveDist 返回错误：%v", err)
	}
	if src.Embedded || src.Path != userDist {
		t.Fatalf("应使用用户目录（只告警不拦截）：embedded=%v path=%q", src.Embedded, src.Path)
	}
}
