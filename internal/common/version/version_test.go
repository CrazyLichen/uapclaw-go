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

// TestBuildInfo 验证 BuildInfo 输出格式
func TestBuildInfo(t *testing.T) {
	info := BuildInfo()

	// 应包含项目名称
	if !strings.Contains(info, ProjectName) {
		t.Errorf("BuildInfo() 应包含项目名称 %q, 实际: %q", ProjectName, info)
	}
	// 应包含版本号
	if !strings.Contains(info, Version) {
		t.Errorf("BuildInfo() 应包含版本号 %q, 实际: %q", Version, info)
	}
	// 应包含 commit 信息
	if !strings.Contains(info, "commit:") {
		t.Errorf("BuildInfo() 应包含 'commit:', 实际: %q", info)
	}
	// 应包含构建时间
	if !strings.Contains(info, "built:") {
		t.Errorf("BuildInfo() 应包含 'built:', 实际: %q", info)
	}
	// 应包含 Go 版本
	if !strings.Contains(info, "go") {
		t.Errorf("BuildInfo() 应包含 'go', 实际: %q", info)
	}
	// 应包含平台信息
	if !strings.Contains(info, "/") {
		t.Errorf("BuildInfo() 应包含平台信息（如 linux/amd64）, 实际: %q", info)
	}
}

// TestBuildInfo_WithLdflags 模拟 ldflags 注入后 BuildInfo 的输出
func TestBuildInfo_WithLdflags(t *testing.T) {
	// 保存原始值
	origVersion := Version
	origGitCommit := GitCommit
	origBuildDate := BuildDate

	// 模拟 ldflags 注入
	Version = "0.2.0"
	GitCommit = "abc1234"
	BuildDate = "2025-07-12T10:30:00Z"

	// 恢复原始值
	defer func() {
		Version = origVersion
		GitCommit = origGitCommit
		BuildDate = origBuildDate
	}()

	info := BuildInfo()

	// 验证注入的值出现在输出中
	if !strings.Contains(info, "0.2.0") {
		t.Errorf("BuildInfo() 应包含注入的版本号 '0.2.0', 实际: %q", info)
	}
	if !strings.Contains(info, "abc1234") {
		t.Errorf("BuildInfo() 应包含注入的 commit 'abc1234', 实际: %q", info)
	}
	if !strings.Contains(info, "2025-07-12") {
		t.Errorf("BuildInfo() 应包含注入的日期 '2025-07-12', 实际: %q", info)
	}
	// 验证日期只保留了日期部分（不含时间）
	if strings.Contains(info, "T10:30:00Z") {
		t.Errorf("BuildInfo() 不应包含时间部分, 实际: %q", info)
	}
}

// TestBuildInfo_NoLdflags 验证未注入 ldflags 时的默认值
func TestBuildInfo_NoLdflags(t *testing.T) {
	// 保存原始值
	origGitCommit := GitCommit
	origBuildDate := BuildDate

	// 模拟未注入 ldflags 的状态
	GitCommit = ""
	BuildDate = ""

	// 恢复原始值
	defer func() {
		GitCommit = origGitCommit
		BuildDate = origBuildDate
	}()

	info := BuildInfo()

	if !strings.Contains(info, "unknown") {
		t.Errorf("未注入 ldflags 时，BuildInfo() 应显示 'unknown', 实际: %q", info)
	}
}
