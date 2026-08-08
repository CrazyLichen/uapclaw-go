package lite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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

// TestLoadVec0Extension_nilConn 测试 nil 连接返回错误
func TestLoadVec0Extension_nilConn(t *testing.T) {
	err := LoadVec0Extension(nil, "vec0.so")
	if err == nil {
		t.Error("nil conn 应返回错误")
	}
}
