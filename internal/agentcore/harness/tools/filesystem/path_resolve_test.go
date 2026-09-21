package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// TestResolveToolFilePath_绝对路径 测试绝对路径直接返回
func TestResolveToolFilePath_绝对路径(t *testing.T) {
	ctx := context.Background()
	absPath := "/tmp/test/file.txt"
	if testing.Short() {
		t.Skip("需要文件系统")
	}
	got := ResolveToolFilePath(ctx, absPath)
	if got != absPath {
		t.Errorf("绝对路径应直接返回，got %q", got)
	}
}

// TestResolveToolFilePath_UNC路径 测试 UNC 路径直接返回
func TestResolveToolFilePath_UNC路径(t *testing.T) {
	ctx := context.Background()
	uncPath := `\\server\share\file.txt`
	got := ResolveToolFilePath(ctx, uncPath)
	if got != uncPath {
		t.Errorf("UNC 路径应直接返回，got %q", got)
	}
}

// TestResolveToolFilePath_相对路径 测试相对路径基于 cwd 解析
func TestResolveToolFilePath_相对路径(t *testing.T) {
	dir := t.TempDir()
	state := cwd.InitCwd(dir)
	ctx := cwd.WithCwdState(context.Background(), state)
	got := ResolveToolFilePath(ctx, "sub/file.txt")
	expected := filepath.Join(dir, "sub/file.txt")
	if got != expected {
		t.Errorf("相对路径应基于 cwd 解析，got %q, want %q", got, expected)
	}
}

// TestResolveToolFilePath_波浪线展开 测试 ~ 展开为用户目录
func TestResolveToolFilePath_波浪线展开(t *testing.T) {
	ctx := context.Background()
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法获取用户主目录")
	}
	got := ResolveToolFilePath(ctx, "~/test.txt")
	expected := filepath.Join(homeDir, "test.txt")
	if got != expected {
		t.Errorf("~ 应展开为用户目录，got %q, want %q", got, expected)
	}
}

// TestIsUNCPath_各种格式 测试 UNC 路径判断
func TestIsUNCPath_各种格式(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{`\\server\share`, true},
		{"//server/share", true},
		{"/local/path", false},
		{`C:\path`, false},
		{"relative/path", false},
	}
	for _, tt := range tests {
		got := isUNCPath(tt.path)
		if got != tt.want {
			t.Errorf("isUNCPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
