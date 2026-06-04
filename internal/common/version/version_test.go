package version

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestVersionNotEmpty 验证版本号不为空
func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Error("Version 不应为空字符串")
	}
}

// TestVersionFormat 验证版本号格式符合语义化版本规范
func TestVersionFormat(t *testing.T) {
	// 版本号应包含点号分隔的数字部分
	if !strings.Contains(Version, ".") {
		t.Errorf("版本号 %q 格式不正确，应包含点号分隔", Version)
	}
}

// TestVersionDevSuffix 验证开发版本包含 -dev 后缀
func TestVersionDevSuffix(t *testing.T) {
	if !strings.HasSuffix(Version, "-dev") {
		t.Errorf("开发版本号 %q 应以 '-dev' 结尾", Version)
	}
}

// TestProjectName 验证项目名称
func TestProjectName(t *testing.T) {
	if ProjectName != "uapclaw" {
		t.Errorf("ProjectName 期望 'uapclaw', 实际 '%s'", ProjectName)
	}
}
