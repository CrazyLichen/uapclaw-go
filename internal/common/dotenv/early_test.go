package dotenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestParseEarly_BothEmpty 测试两个参数都为空时不做任何操作。
func TestParseEarly_BothEmpty(t *testing.T) {
	result, err := ParseEarly("", "")
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	if result != "" {
		t.Errorf("期望空路径，实际: %s", result)
	}
}

// TestParseEarly_DotenvPath 测试 --dotenv 参数。
func TestParseEarly_DotenvPath(t *testing.T) {
	// 创建临时 .env 文件
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	content := "UAPCLAW_DATA_DIR=/tmp/test_ws\nUAPCLAW_INSTANCE=early_test\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 .env 文件失败: %v", err)
	}

	// 保存原有环境变量
	origDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	origInstance := os.Getenv("UAPCLAW_INSTANCE")
	t.Cleanup(func() {
		_ = os.Setenv("UAPCLAW_DATA_DIR", origDataDir)
		_ = os.Setenv("UAPCLAW_INSTANCE", origInstance)
	})

	// 执行
	result, err := ParseEarly(envPath, "")
	if err != nil {
		t.Fatalf("ParseEarly 失败: %v", err)
	}
	if result != envPath {
		t.Errorf("期望路径 %s，实际: %s", envPath, result)
	}

	// 验证环境变量
	if v := os.Getenv("UAPCLAW_DATA_DIR"); v != "/tmp/test_ws" {
		t.Errorf("UAPCLAW_DATA_DIR 期望 '/tmp/test_ws'，实际 '%s'", v)
	}
	if v := os.Getenv("UAPCLAW_INSTANCE"); v != "early_test" {
		t.Errorf("UAPCLAW_INSTANCE 期望 'early_test'，实际 '%s'", v)
	}
}

// TestParseEarly_DotenvPriority 测试 --dotenv 优先于 --name。
func TestParseEarly_DotenvPriority(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	content := "PRIORITY_TEST=dotenv_wins\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 .env 文件失败: %v", err)
	}

	origVal := os.Getenv("PRIORITY_TEST")
	t.Cleanup(func() {
		_ = os.Setenv("PRIORITY_TEST", origVal)
	})

	// 同时传入两个参数，--dotenv 应优先
	result, err := ParseEarly(envPath, "some_instance")
	if err != nil {
		t.Fatalf("ParseEarly 失败: %v", err)
	}
	if result != envPath {
		t.Errorf("期望 --dotenv 优先，路径应为 %s，实际: %s", envPath, result)
	}
	if v := os.Getenv("PRIORITY_TEST"); v != "dotenv_wins" {
		t.Errorf("期望 'dotenv_wins'，实际 '%s'", v)
	}
}

// TestParseEarly_DotenvFileNotFound 测试 --dotenv 文件不存在。
func TestParseEarly_DotenvFileNotFound(t *testing.T) {
	_, err := ParseEarly("/nonexistent/.env", "")
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

// TestParseEarly_InvalidInstanceName 测试 --name 名称无效。
func TestParseEarly_InvalidInstanceName(t *testing.T) {
	_, err := ParseEarly("", "default") // "default" 是保留名称
	if err == nil {
		t.Fatal("期望返回错误（保留名称），实际返回 nil")
	}
}

// TestParseEarly_InstanceNotFound 测试 --name 实例不存在。
func TestParseEarly_InstanceNotFound(t *testing.T) {
	// 确保工作区指向临时目录，避免影响真实环境
	origDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	dir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", dir)
	t.Cleanup(func() {
		_ = os.Setenv("UAPCLAW_DATA_DIR", origDataDir)
	})

	_, err := ParseEarly("", "nonexistent_instance_xyz")
	if err == nil {
		t.Fatal("期望返回错误（实例不存在），实际返回 nil")
	}
}

// TestParseEarly_InstanceBootstrap 测试 --name 完整流程。
func TestParseEarly_InstanceBootstrap(t *testing.T) {
	// 准备：设置临时工作区
	origDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	origInstance := os.Getenv("UAPCLAW_INSTANCE")
	homeDir := t.TempDir()
	instanceDir := filepath.Join(homeDir, ".uapclaw-instances", "mytest")
	_ = os.Setenv("UAPCLAW_DATA_DIR", filepath.Join(homeDir, ".uapclaw"))
	t.Cleanup(func() {
		_ = os.Setenv("UAPCLAW_DATA_DIR", origDataDir)
		_ = os.Setenv("UAPCLAW_INSTANCE", origInstance)
	})

	// 创建工作区目录
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatalf("创建实例目录失败: %v", err)
	}

	// 在 instances.yaml 中注册实例
	if err := workspace.UpdateInstancesYAML("mytest", instanceDir, nil); err != nil {
		t.Fatalf("注册实例失败: %v", err)
	}

	// 执行 ParseEarly
	result, err := ParseEarly("", "mytest")
	if err != nil {
		t.Fatalf("ParseEarly 失败: %v", err)
	}

	// 应该创建了 bootstrap .env 并加载
	envPath := filepath.Join(instanceDir, ".env")
	if result != envPath {
		t.Errorf("期望路径 %s，实际: %s", envPath, result)
	}

	// 验证 .env 文件存在
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		t.Errorf("bootstrap .env 文件不存在: %s", envPath)
	}

	// 验证环境变量已设入
	if v := os.Getenv("UAPCLAW_INSTANCE"); v != "mytest" {
		t.Errorf("UAPCLAW_INSTANCE 期望 'mytest'，实际 '%s'", v)
	}
}

// TestParsedDotenv 测试 ParsedDotenv 返回值。
func TestParsedDotenv(t *testing.T) {
	// 重置全局状态
	parsedDotenv = ""

	if v := ParsedDotenv(); v != "" {
		t.Errorf("初始状态期望空字符串，实际 '%s'", v)
	}

	// 加载一个 .env 后检查
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "TEST_PARSED_DOTENV=1\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 .env 文件失败: %v", err)
	}

	origVal := os.Getenv("TEST_PARSED_DOTENV")
	t.Cleanup(func() {
		_ = os.Setenv("TEST_PARSED_DOTENV", origVal)
	})

	if _, err := ParseEarly(envPath, ""); err != nil {
		t.Fatalf("ParseEarly 失败: %v", err)
	}

	if v := ParsedDotenv(); v != envPath {
		t.Errorf("期望 '%s'，实际 '%s'", envPath, v)
	}
}

// TestExpandHome 测试路径展开。
func TestExpandHome(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHome bool // 期望结果以 home 目录开头
	}{
		{"空字符串", "", false},
		{"相对路径", "foo/bar", false},
		{"绝对路径", "/tmp/test", false},
		{"~ 展开", "~/test", true},
		{"仅 ~", "~", true},
		{"~user 不展开", "~otheruser/test", false},
	}

	home, _ := os.UserHomeDir()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandHome(tt.input)
			if tt.wantHome {
				if len(result) < len(home) || result[:len(home)] != home {
					t.Errorf("期望路径以 %s 开头，实际: %s", home, result)
				}
			}
		})
	}
}

// TestParseEarly_InstanceWorkspaceNotExist 测试 --name 实例工作区目录不存在。
func TestParseEarly_InstanceWorkspaceNotExist(t *testing.T) {
	// 准备：设置临时工作区
	origDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	homeDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", filepath.Join(homeDir, ".uapclaw"))
	t.Cleanup(func() {
		_ = os.Setenv("UAPCLAW_DATA_DIR", origDataDir)
	})

	// 注册实例但指向不存在的目录
	if err := workspace.UpdateInstancesYAML("ghost", "/nonexistent/workspace/ghost", nil); err != nil {
		t.Fatalf("注册实例失败: %v", err)
	}

	_, err := ParseEarly("", "ghost")
	if err == nil {
		t.Fatal("期望返回错误（工作区不存在），实际返回 nil")
	}
}

// TestParseEarly_DotenvWithTilde 测试 --dotenv 使用 ~ 路径。
func TestParseEarly_DotenvWithTilde(t *testing.T) {
	// 在 HOME 下创建临时 .env 文件
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法获取用户主目录")
	}

	envPath := filepath.Join(home, ".uapclaw_test_dotenv_tmp")
	content := "TEST_DOTENV_TILDE=expanded\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 .env 文件失败: %v", err)
	}
	defer func() { _ = os.Remove(envPath) }()

	origVal := os.Getenv("TEST_DOTENV_TILDE")
	t.Cleanup(func() {
		_ = os.Setenv("TEST_DOTENV_TILDE", origVal)
	})

	// 使用 ~/ 前缀路径
	result, err := ParseEarly("~/.uapclaw_test_dotenv_tmp", "")
	if err != nil {
		t.Fatalf("ParseEarly 失败: %v", err)
	}
	if result != envPath {
		t.Errorf("期望路径 %s，实际: %s", envPath, result)
	}
	if v := os.Getenv("TEST_DOTENV_TILDE"); v != "expanded" {
		t.Errorf("期望 'expanded'，实际 '%s'", v)
	}
}
