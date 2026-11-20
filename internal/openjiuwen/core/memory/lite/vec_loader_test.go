package lite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mattn/go-sqlite3"
)

// TestResolveVec0Path_路径格式 测试 vec0.so 路径解析
func TestResolveVec0Path_路径格式(t *testing.T) {
	path := ResolveVec0Path()
	expectedExt := ".so"
	if runtime.GOOS == "darwin" {
		expectedExt = ".dylib"
	}
	expected := filepath.Join("third_party", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0"+expectedExt)
	if path != expected {
		t.Errorf("ResolveVec0Path() = %q, want %q", path, expected)
	}
}

// TestIsVec0Available 测试 vec0.so 可用性检查
func TestIsVec0Available(t *testing.T) {
	path := ResolveVec0Path()
	_, err := os.Stat(path)
	// 在 CI 环境中可能不存在，仅验证函数不 panic
	available := IsVec0Available()
	if err == nil && !available {
		t.Error("vec0.so 存在但 IsVec0Available 返回 false")
	}
	if err != nil && available {
		t.Error("vec0.so 不存在但 IsVec0Available 返回 true")
	}
}

// TestResolveVec0Path_可执行文件同目录命中 测试 CWD 未命中时回落到可执行文件目录
func TestResolveVec0Path_可执行文件同目录命中(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Skipf("无法获取测试二进制路径: %v", err)
	}
	rel := ResolveVec0Path()
	// 测试进程 CWD 为包目录，仓库根的 third_party 对 CWD 不可见，
	// 因此在测试二进制旁造一份同名布局，验证第二候选路径命中。
	target := filepath.Join(filepath.Dir(execPath), rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Skipf("无法创建可执行文件目录下的布局: %v", err)
	}
	if err := os.WriteFile(target, []byte("stub"), 0o644); err != nil {
		t.Skipf("无法写入 stub: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(filepath.Dir(execPath), "third_party")) })

	got := ResolveVec0Path()
	if got != target {
		t.Errorf("ResolveVec0Path() = %q, 期望可执行文件同目录 %q", got, target)
	}
	if !IsVec0Available() {
		t.Error("stub 存在时 IsVec0Available 应为 true")
	}
}

// TestResolveVec0Path_当前目录命中 测试 CWD 下存在 third_party 布局时优先返回相对路径
func TestResolveVec0Path_当前目录命中(t *testing.T) {
	dir := t.TempDir()
	ext := ".so"
	switch runtime.GOOS {
	case "darwin":
		ext = ".dylib"
	case "windows":
		ext = ".dll"
	}
	rel := filepath.Join("third_party", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0"+ext)
	target := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("创建布局失败: %v", err)
	}
	if err := os.WriteFile(target, []byte("stub"), 0o644); err != nil {
		t.Fatalf("写入 stub 失败: %v", err)
	}
	t.Chdir(dir)

	if got := ResolveVec0Path(); got != rel {
		t.Errorf("ResolveVec0Path() = %q, 期望 CWD 相对路径 %q", got, rel)
	}
	if !IsVec0Available() {
		t.Error("CWD 下 stub 存在时 IsVec0Available 应为 true")
	}
}

// TestLoadVec0Extension_扩展文件不存在 测试真实连接加载失败时返回错误（不静默成功）
func TestLoadVec0Extension_扩展文件不存在(t *testing.T) {
	driver := &sqlite3.SQLiteDriver{}
	raw, err := driver.Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	defer func() { _ = raw.Close() }()
	conn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		t.Fatalf("驱动返回类型异常: %T", raw)
	}

	missing := filepath.Join(t.TempDir(), "vec0-missing.so")
	if err := LoadVec0Extension(conn, missing); err == nil {
		t.Error("扩展文件不存在时应返回错误")
	}
}

// TestLoadVec0Extension_nilConn 测试 nil 连接返回错误
func TestLoadVec0Extension_nilConn(t *testing.T) {
	err := LoadVec0Extension(nil, "vec0.so")
	if err == nil {
		t.Error("nil conn 应返回错误")
	}
}
