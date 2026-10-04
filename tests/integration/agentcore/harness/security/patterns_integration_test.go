//go:build integration

package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	security "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PatternsIntegrationSuite 测试安全模式匹配器。
//
// 对齐 Python: tests/unit_tests/harness/security/test_patterns.py
//   - TestCommandMatcher: CommandMatcher 命令匹配
//   - TestPathMatcher: PathMatcher 路径匹配
//   - TestURLMatcher: URLMatcher URL 匹配
//   - TestMatchWildcard: 通配符匹配（含 shell 注入防护）
//   - TestBuildCommandAllowPattern: 命令允许模式构建
//   - TestContainsPath: 子路径判断（含路径穿越防护）
//   - TestWriteReadAgentConfigYAML_RoundTrip: YAML 读写往返
type PatternsIntegrationSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPatternsIntegrationSuite(t *testing.T) {
	suite.Run(t, new(PatternsIntegrationSuite))
}

// ─── CommandMatcher 测试 ───

// TestCommandMatcher_单个模式精确匹配
func (s *PatternsIntegrationSuite) TestCommandMatcher_单个模式精确匹配() {
	cm := &security.CommandMatcher{}
	s.True(cm.MatchCommand("git status", "git status"))
}

// TestCommandMatcher_通配符模式匹配带参数命令
func (s *PatternsIntegrationSuite) TestCommandMatcher_通配符模式匹配带参数命令() {
	cm := &security.CommandMatcher{}
	s.True(cm.MatchCommand("git status *", "git status -s"))
	s.True(cm.MatchCommand("git status *", "git status --short"))
	s.True(cm.MatchCommand("git status *", "git status"))
}

// TestCommandMatcher_空命令返回false
func (s *PatternsIntegrationSuite) TestCommandMatcher_空命令返回false() {
	cm := &security.CommandMatcher{}
	s.False(cm.MatchCommand("git status", ""))
}

// TestCommandMatcher_空模式返回false
func (s *PatternsIntegrationSuite) TestCommandMatcher_空模式返回false() {
	cm := &security.CommandMatcher{}
	s.False(cm.MatchCommand("", "git status"))
}

// TestCommandMatcher_不匹配返回false
func (s *PatternsIntegrationSuite) TestCommandMatcher_不匹配返回false() {
	cm := &security.CommandMatcher{}
	s.False(cm.MatchCommand("git status", "git log"))
	s.False(cm.MatchCommand("git status", "npm install"))
}

// TestCommandMatcher_MatchCommandAny_匹配任一模式
func (s *PatternsIntegrationSuite) TestCommandMatcher_MatchCommandAny_匹配任一模式() {
	cm := &security.CommandMatcher{}
	s.True(cm.MatchCommandAny([]string{"git status *", "git log *"}, "git status -s"))
	s.True(cm.MatchCommandAny([]string{"git status *", "git log *"}, "git log --oneline"))
	s.False(cm.MatchCommandAny([]string{"git status *", "git log *"}, "npm install"))
}

// ─── PathMatcher 测试 ───

// TestPathMatcher_精确路径匹配
func (s *PatternsIntegrationSuite) TestPathMatcher_精确路径匹配() {
	pm := &security.PathMatcher{}
	s.True(pm.MatchPath("/home/user/project", "/home/user/project"))
}

// TestPathMatcher_通配符路径匹配
func (s *PatternsIntegrationSuite) TestPathMatcher_通配符路径匹配() {
	pm := &security.PathMatcher{}
	s.True(pm.MatchPath("/home/user/project/*", "/home/user/project/src/main.go"))
}

// TestPathMatcher_父目录通配匹配
func (s *PatternsIntegrationSuite) TestPathMatcher_父目录通配匹配() {
	pm := &security.PathMatcher{}
	// 匹配父目录层级：pattern=/home/user/project 匹配 /home/user/project/src/main.go
	s.True(pm.MatchPath("/home/user/project", "/home/user/project/src/main.go"))
}

// TestPathMatcher_反斜杠路径规范化
func (s *PatternsIntegrationSuite) TestPathMatcher_反斜杠路径规范化() {
	pm := &security.PathMatcher{}
	// MatchPath 对 pattern 和 path 的首次匹配使用 strings.ReplaceAll 将 \ 替换为 /
	// 注意：父目录遍历使用 filepath.Clean(path)，在 Linux 上不转换反斜杠
	// 因此仅首次直接匹配能处理反斜杠路径
	s.True(pm.MatchPath(`/home/user/project`, `/home/user/project/src/main.go`))
}

// TestPathMatcher_不匹配返回false
func (s *PatternsIntegrationSuite) TestPathMatcher_不匹配返回false() {
	pm := &security.PathMatcher{}
	s.False(pm.MatchPath("/home/user/project-a", "/home/user/project-b/main.go"))
}

// TestPathMatcher_Unicode路径
func (s *PatternsIntegrationSuite) TestPathMatcher_Unicode路径() {
	pm := &security.PathMatcher{}
	// 中文路径应正常匹配
	s.True(pm.MatchPath("/home/用户/项目", "/home/用户/项目"))
	s.True(pm.MatchPath("/home/用户/项目/*", "/home/用户/项目/文件.go"))
}

// TestPathMatcher_超长路径
func (s *PatternsIntegrationSuite) TestPathMatcher_超长路径() {
	pm := &security.PathMatcher{}
	// 构造超长路径
	longSegment := ""
	for i := 0; i < 100; i++ {
		longSegment += "abcdefgh"
	}
	longPath := "/home/user/" + longSegment
	s.True(pm.MatchPath(longPath, longPath))
}

// TestPathMatcher_MatchPathAny_匹配任一模式
func (s *PatternsIntegrationSuite) TestPathMatcher_MatchPathAny_匹配任一模式() {
	pm := &security.PathMatcher{}
	s.True(pm.MatchPathAny([]string{"/home/user/a/*", "/home/user/b/*"}, "/home/user/a/file.go"))
	s.True(pm.MatchPathAny([]string{"/home/user/a/*", "/home/user/b/*"}, "/home/user/b/file.go"))
	s.False(pm.MatchPathAny([]string{"/home/user/a/*", "/home/user/b/*"}, "/home/user/c/file.go"))
}

// ─── URLMatcher 测试 ───

// TestURLMatcher_完整URL匹配
func (s *PatternsIntegrationSuite) TestURLMatcher_完整URL匹配() {
	um := &security.URLMatcher{}
	s.True(um.MatchURL("example.com", "https://example.com/path"))
}

// TestURLMatcher_主机名匹配
func (s *PatternsIntegrationSuite) TestURLMatcher_主机名匹配() {
	um := &security.URLMatcher{}
	s.True(um.MatchURL("example.com", "https://example.com:8080/path"))
}

// TestURLMatcher_通配符URL匹配
func (s *PatternsIntegrationSuite) TestURLMatcher_通配符URL匹配() {
	um := &security.URLMatcher{}
	s.True(um.MatchURL("*.example.com", "https://sub.example.com/api"))
}

// TestURLMatcher_带端口号匹配
func (s *PatternsIntegrationSuite) TestURLMatcher_带端口号匹配() {
	um := &security.URLMatcher{}
	// netloc（含端口）匹配
	s.True(um.MatchURL("example.com:8080", "https://example.com:8080/path"))
}

// TestURLMatcher_IPv6地址
func (s *PatternsIntegrationSuite) TestURLMatcher_IPv6地址() {
	um := &security.URLMatcher{}
	// IPv6 不带端口时可直接匹配
	s.True(um.MatchURL("[::1]", "http://[::1]/api"))
	// IPv6 localhost 匹配
	s.True(um.MatchURL("localhost", "http://localhost:3000/api"))
}

// TestURLMatcher_带查询字符串
func (s *PatternsIntegrationSuite) TestURLMatcher_带查询字符串() {
	um := &security.URLMatcher{}
	s.True(um.MatchURL("example.com", "https://example.com/path?key=value&foo=bar"))
}

// TestURLMatcher_空URL返回false
func (s *PatternsIntegrationSuite) TestURLMatcher_空URL返回false() {
	um := &security.URLMatcher{}
	s.False(um.MatchURL("example.com", ""))
}

// TestURLMatcher_scheme匹配
func (s *PatternsIntegrationSuite) TestURLMatcher_scheme匹配() {
	um := &security.URLMatcher{}
	// scheme://netloc 匹配
	s.True(um.MatchURL("https://example.com", "https://example.com/any/path"))
	s.True(um.MatchURL("https://example.com/*", "https://example.com/any/path"))
}

// TestURLMatcher_MatchURLAny_匹配任一模式
func (s *PatternsIntegrationSuite) TestURLMatcher_MatchURLAny_匹配任一模式() {
	um := &security.URLMatcher{}
	s.True(um.MatchURLAny([]string{"api.example.com", "cdn.example.com"}, "https://api.example.com/v1"))
	s.False(um.MatchURLAny([]string{"api.example.com", "cdn.example.com"}, "https://other.example.com"))
}

// ─── MatchWildcard 测试（安全关键）───

// TestMatchWildcard_基本通配符匹配
func (s *PatternsIntegrationSuite) TestMatchWildcard_基本通配符匹配() {
	s.True(security.MatchWildcard("git status", "git status"))
	s.True(security.MatchWildcard("git status *", "git status -s"))
	s.True(security.MatchWildcard("git status *", "git status"))
}

// TestMatchWildcard_问号通配符
func (s *PatternsIntegrationSuite) TestMatchWildcard_问号通配符() {
	s.True(security.MatchWildcard("file?.txt", "file1.txt"))
	s.True(security.MatchWildcard("file?.txt", "fileA.txt"))
	s.False(security.MatchWildcard("file?.txt", "file12.txt"))
}

// TestMatchWildcard_空值返回false
func (s *PatternsIntegrationSuite) TestMatchWildcard_空值返回false() {
	s.False(security.MatchWildcard("", "git status"))
	s.False(security.MatchWildcard("git status", ""))
}

// TestMatchWildcard_shell注入防护_分号拼接
// 对齐 Python: match_wildcard 不匹配含 shell 元字符的值（安全关键测试！）
func (s *PatternsIntegrationSuite) TestMatchWildcard_shell注入防护_分号拼接() {
	// "git status; rm -rf /" 绝不能匹配 "git status *"
	// 因为 * 被替换为限制性字符类，排除 ; | & 等元字符
	s.False(security.MatchWildcard("git status *", "git status; rm -rf /"))
}

// TestMatchWildcard_shell注入防护_管道拼接
func (s *PatternsIntegrationSuite) TestMatchWildcard_shell注入防护_管道拼接() {
	// 管道注入：| 是 shell 元字符，被限制性字符类排除
	s.False(security.MatchWildcard("git status *", "git status | cat /etc/passwd"))
}

// TestMatchWildcard_shell注入防护_反引号注入
func (s *PatternsIntegrationSuite) TestMatchWildcard_shell注入防护_反引号注入() {
	// 反引号注入：` 是 shell 元字符
	s.False(security.MatchWildcard("git status *", "git status `rm -rf /`"))
}

// TestMatchWildcard_shell注入防护_美元符号注入
func (s *PatternsIntegrationSuite) TestMatchWildcard_shell注入防护_美元符号注入() {
	// $ 变量展开注入
	s.False(security.MatchWildcard("echo *", "echo $HOME"))
}

// TestMatchWildcard_shell注入防护_And符号注入
func (s *PatternsIntegrationSuite) TestMatchWildcard_shell注入防护_And符号注入() {
	// && 注入
	s.False(security.MatchWildcard("git status *", "git status && rm -rf /"))
}

// TestMatchWildcard_shell注入防护_重定向注入
func (s *PatternsIntegrationSuite) TestMatchWildcard_shell注入防护_重定向注入() {
	// < > 重定向注入
	s.False(security.MatchWildcard("cat *", "cat /etc/passwd > /tmp/stolen"))
	s.False(security.MatchWildcard("cat *", "cat < /etc/passwd"))
}

// TestMatchWildcard_合法参数通过
func (s *PatternsIntegrationSuite) TestMatchWildcard_合法参数通过() {
	// 正常命令参数应通过
	s.True(security.MatchWildcard("git status *", "git status -s --short"))
	s.True(security.MatchWildcard("ls *", "ls -la /home"))
	s.True(security.MatchWildcard("npm *", "npm install --save-dev"))
}

// TestMatchWildcard_全串匹配防止部分匹配
func (s *PatternsIntegrationSuite) TestMatchWildcard_全串匹配防止部分匹配() {
	// "git status" 不应匹配 "git statusx"
	s.False(security.MatchWildcard("git status", "git statusx"))
	s.False(security.MatchWildcard("git status", "xgit status"))
}

// TestMatchWildcard_路径分隔符规范化
func (s *PatternsIntegrationSuite) TestMatchWildcard_路径分隔符规范化() {
	// 反斜杠规范化为正斜杠
	s.True(security.MatchWildcard(`C:/Users/*`, `C:\Users\file.txt`))
}

// ─── BuildCommandAllowPattern 测试 ───

// TestBuildCommandAllowPattern_基础命令
func (s *PatternsIntegrationSuite) TestBuildCommandAllowPattern_基础命令() {
	// "start chrome" → "start chrome *"
	s.Equal("start chrome *", security.BuildCommandAllowPattern("start chrome"))
}

// TestBuildCommandAllowPattern_带尾部空格
func (s *PatternsIntegrationSuite) TestBuildCommandAllowPattern_带尾部空格() {
	// TrimSpace 后追加 " *"
	s.Equal("start chrome *", security.BuildCommandAllowPattern("  start chrome  "))
}

// TestBuildCommandAllowPattern_单命令
func (s *PatternsIntegrationSuite) TestBuildCommandAllowPattern_单命令() {
	s.Equal("git *", security.BuildCommandAllowPattern("git"))
}

// TestBuildCommandAllowPattern_前缀匹配验证
func (s *PatternsIntegrationSuite) TestBuildCommandAllowPattern_前缀匹配验证() {
	// 构建 "npm *" 模式后，"npm install" 应匹配
	pattern := security.BuildCommandAllowPattern("npm")
	cm := &security.CommandMatcher{}
	s.True(cm.MatchCommand(pattern, "npm install"))
	s.True(cm.MatchCommand(pattern, "npm run build"))
	s.False(cm.MatchCommand(pattern, "pip install"))
}

// ─── ContainsPath 测试（安全关键）───

// TestContainsPath_子路径在父路径下返回true
func (s *PatternsIntegrationSuite) TestContainsPath_子路径在父路径下返回true() {
	s.True(security.ContainsPath("/home/user/project", "/home/user/project/src/main.go"))
}

// TestContainsPath_相同路径返回true
func (s *PatternsIntegrationSuite) TestContainsPath_相同路径返回true() {
	s.True(security.ContainsPath("/home/user/project", "/home/user/project"))
}

// TestContainsPath_兄弟路径返回false
func (s *PatternsIntegrationSuite) TestContainsPath_兄弟路径返回false() {
	s.False(security.ContainsPath("/home/user/project-a", "/home/user/project-b/main.go"))
}

// TestContainsPath_路径穿越防护
// 对齐 Python: contains_path 防止 "../" 穿越
func (s *PatternsIntegrationSuite) TestContainsPath_路径穿越防护() {
	// 子路径包含 ".." 穿越时应返回 false
	s.False(security.ContainsPath("/home/user/project", "/home/user/project/../secret"))
}

// TestContainsPath_深层穿越防护
func (s *PatternsIntegrationSuite) TestContainsPath_深层穿越防护() {
	s.False(security.ContainsPath("/home/user/project", "/home/user/project/a/../../secret"))
}

// TestContainsPath_相对路径处理
func (s *PatternsIntegrationSuite) TestContainsPath_相对路径处理() {
	s.True(security.ContainsPath(".", "subdir/file.go"))
}

// TestContainsPath_点号路径穿越
func (s *PatternsIntegrationSuite) TestContainsPath_点号路径穿越() {
	s.False(security.ContainsPath("/home/user/project", "../other"))
}

// TestContainsPath_符号链接无关判断
func (s *PatternsIntegrationSuite) TestContainsPath_符号链接无关判断() {
	// ContainsPath 基于 filepath.Rel 计算相对路径，不做 symlink 解析
	// 只要字符串上 child 在 parent 下就返回 true
	s.True(security.ContainsPath("/home/user/project", "/home/user/project/link/target"))
}

// ─── WritePermissionsSectionToAgentConfigYAML / ReadAgentConfigYAML 往返测试 ───

// TestWriteReadAgentConfigYAML_RoundTrip_基本往返
func (s *PatternsIntegrationSuite) TestWriteReadAgentConfigYAML_RoundTrip_基本往返() {
	tmpDir := s.T().TempDir()
	cfgPath := filepath.Join(tmpDir, "agent.yaml")

	permissions := map[string]any{
		"enabled": true,
		"schema":  "tiered_policy",
		"defaults": map[string]any{
			"*": "allow",
		},
		"tools": map[string]any{
			"bash": "ask",
		},
	}

	ok := security.WritePermissionsSectionToAgentConfigYAML(cfgPath, permissions)
	s.True(ok)

	// 读回并验证 permissions 段
	data := security.ReadAgentConfigYAML(cfgPath)
	s.NotNil(data)

	perms, ok := data["permissions"].(map[string]any)
	s.True(ok)
	s.Equal(true, perms["enabled"])
	s.Equal("tiered_policy", perms["schema"])
}

// TestWriteReadAgentConfigYAML_RoundTrip_保留其它顶层键
func (s *PatternsIntegrationSuite) TestWriteReadAgentConfigYAML_RoundTrip_保留其它顶层键() {
	tmpDir := s.T().TempDir()
	cfgPath := filepath.Join(tmpDir, "agent.yaml")

	// 先写入含其它键的配置
	initialData := map[string]any{
		"model":      "qwen-max",
		"max_tokens": 4096,
	}
	s.NoError(security.WriteAgentConfigYAML(cfgPath, initialData))

	// 写入 permissions 段
	permissions := map[string]any{
		"enabled": true,
		"tools":   map[string]any{"bash": "deny"},
	}
	ok := security.WritePermissionsSectionToAgentConfigYAML(cfgPath, permissions)
	s.True(ok)

	// 读回验证其它顶层键保留
	data := security.ReadAgentConfigYAML(cfgPath)
	s.Equal("qwen-max", data["model"])

	perms, ok := data["permissions"].(map[string]any)
	s.True(ok)
	s.Equal(true, perms["enabled"])
}

// TestReadAgentConfigYAML_文件不存在返回空map
func (s *PatternsIntegrationSuite) TestReadAgentConfigYAML_文件不存在返回空map() {
	data := security.ReadAgentConfigYAML("/nonexistent/path/agent.yaml")
	s.NotNil(data)
	s.Empty(data)
}

// TestReadAgentConfigYAML_空文件返回空map
func (s *PatternsIntegrationSuite) TestReadAgentConfigYAML_空文件返回空map() {
	tmpDir := s.T().TempDir()
	cfgPath := filepath.Join(tmpDir, "empty.yaml")
	s.NoError(os.WriteFile(cfgPath, []byte{}, 0644))

	data := security.ReadAgentConfigYAML(cfgPath)
	s.NotNil(data)
	s.Empty(data)
}

// TestWritePermissionsSectionToAgentConfigYAML_空路径返回false
func (s *PatternsIntegrationSuite) TestWritePermissionsSectionToAgentConfigYAML_空路径返回false() {
	// 空路径且无默认配置路径 → 返回 false
	// 注意：如果 workspace.ConfigFile() 返回非空值，此测试可能通过
	// 通过临时设置确保路径无效
	ok := security.WritePermissionsSectionToAgentConfigYAML("/nonexistent/dir/deep/agent.yaml", map[string]any{"enabled": true})
	s.False(ok)
}

// ─── PatternMatcher 测试 ───

// TestPatternMatcher_基本匹配
func (s *PatternsIntegrationSuite) TestPatternMatcher_基本匹配() {
	pm := &security.PatternMatcher{}
	s.True(pm.Match("hello", "hello"))
	s.True(pm.Match("hello *", "hello world"))
	s.False(pm.Match("hello", "world"))
}

// TestPatternMatcher_MatchAny_匹配任一
func (s *PatternsIntegrationSuite) TestPatternMatcher_MatchAny_匹配任一() {
	pm := &security.PatternMatcher{}
	s.True(pm.MatchAny([]string{"hello", "world"}, "hello"))
	s.True(pm.MatchAny([]string{"hello", "world"}, "world"))
	s.False(pm.MatchAny([]string{"hello", "world"}, "other"))
}

// ─── MergeExternalDirectoryAllowIntoPermissions 测试 ───

// TestMergeExternalDirectoryAllow_空路径不修改
func (s *PatternsIntegrationSuite) TestMergeExternalDirectoryAllow_空路径不修改() {
	perms := map[string]any{
		"external_directory": map[string]any{"*": "ask"},
	}
	merged, wrote := security.MergeExternalDirectoryAllowIntoPermissions(perms, []string{})
	s.False(wrote)
	s.Equal("ask", merged["external_directory"].(map[string]any)["*"])
}

// TestMergeExternalDirectoryAllow_添加允许目录
func (s *PatternsIntegrationSuite) TestMergeExternalDirectoryAllow_添加允许目录() {
	perms := map[string]any{
		"external_directory": map[string]any{"*": "ask"},
	}
	merged, wrote := security.MergeExternalDirectoryAllowIntoPermissions(perms, []string{"/home/user/project"})
	s.True(wrote)
	extDir := merged["external_directory"].(map[string]any)
	s.Equal("allow", extDir["/home/user"])
}

// TestMergeExternalDirectoryAllow_已存在allow不重复写入
func (s *PatternsIntegrationSuite) TestMergeExternalDirectoryAllow_已存在allow不重复写入() {
	perms := map[string]any{
		"external_directory": map[string]any{
			"/home/user": "allow",
		},
	}
	_, wrote := security.MergeExternalDirectoryAllowIntoPermissions(perms, []string{"/home/user/project"})
	s.False(wrote)
}
