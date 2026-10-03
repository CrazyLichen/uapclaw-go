//go:build integration

package forbidden_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	swarmmemory "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForbiddenSuite ForbiddenMemory 集成测试套件
type ForbiddenSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 全局变量 ────────────────────────────

// originalDataDir 保存原始 UAPCLAW_DATA_DIR 环境变量
var originalDataDir string

// ──────────────────────────── 导出函数 ────────────────────────────

// TestForbiddenSuite 运行 ForbiddenMemory 集成测试套件
func TestForbiddenSuite(t *testing.T) {
	suite.Run(t, new(ForbiddenSuite))
}

// TestGetForbiddenMemoryPrompt_中文输出_integration 验证从真实配置文件读取并生成中文提示词
// 运行方式: go test -tags=integration ./tests/integration/swarm/memory/...
func (s *ForbiddenSuite) TestGetForbiddenMemoryPrompt_中文输出_integration() {
	setupTestConfig(s.T(), "forbidden_enabled")
	defer restoreConfig()

	prompt, err := swarmmemory.GetForbiddenMemoryPrompt("cn")
	s.Require().NoError(err)
	s.NotEmpty(prompt, "配置 enabled=true 时应返回非空中文提示词")
	s.True(strings.Contains(prompt, "记忆限制规则"), "中文提示词应包含 '记忆限制规则'")
	s.True(strings.Contains(prompt, "密码"), "中文提示词应包含 pattern '密码'")
	s.True(strings.Contains(prompt, "API密钥"), "中文提示词应包含 pattern 'API密钥'")
	s.True(strings.Contains(prompt, "experience_learn"), "中文提示词应包含执行要求中的 experience_learn")
}

// TestGetForbiddenMemoryPrompt_英文输出_integration 验证从真实配置文件读取并生成英文提示词
func (s *ForbiddenSuite) TestGetForbiddenMemoryPrompt_英文输出_integration() {
	setupTestConfig(s.T(), "forbidden_enabled")
	defer restoreConfig()

	prompt, err := swarmmemory.GetForbiddenMemoryPrompt("en")
	s.Require().NoError(err)
	s.NotEmpty(prompt, "配置 enabled=true 时应返回非空英文提示词")
	s.True(strings.Contains(prompt, "Memory Restriction Rules"), "英文提示词应包含 'Memory Restriction Rules'")
	s.True(strings.Contains(prompt, "passwords"), "英文提示词应包含 pattern 'passwords'")
}

// TestGetForbiddenMemoryPrompt_无Patterns_integration 验证无 patterns 时不输出列表
func (s *ForbiddenSuite) TestGetForbiddenMemoryPrompt_无Patterns_integration() {
	setupTestConfig(s.T(), "forbidden_no_patterns")
	defer restoreConfig()

	prompt, err := swarmmemory.GetForbiddenMemoryPrompt("cn")
	s.Require().NoError(err)
	s.NotEmpty(prompt, "配置 enabled=true 时应返回非空提示词")
	s.False(strings.Contains(prompt, "禁止记忆的敏感信息类型包括"), "无 patterns 时不应输出列表标题")
	s.True(strings.Contains(prompt, "执行要求"), "无 patterns 时仍应包含执行要求")
}

// TestForbiddenDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *ForbiddenSuite) TestForbiddenDoc_包引用验证() {
	// 验证 pathutil 和 runner 包可正常导入
	s.NotNil(pathutil.ResetCache)
	s.NotNil(runner.GetResourceMgr)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// testdataBaseDir 指向原始 testdata 目录的绝对路径
// 原位置：internal/swarm/agents/harness/common/memory/testdata/
const testdataBaseDir = "../../../../internal/swarm/agents/harness/common/memory/testdata"

// setupTestConfig 设置 UAPCLAW_DATA_DIR 指向 testdata 下的指定子目录
func setupTestConfig(t *testing.T, scenario string) {
	t.Helper()
	originalDataDir = os.Getenv("UAPCLAW_DATA_DIR")

	testdataDir := filepath.Join(testdataBaseDir, scenario)
	absPath, err := filepath.Abs(testdataDir)
	if err != nil {
		t.Fatalf("获取测试目录绝对路径失败: %v", err)
	}

	configFile := filepath.Join(absPath, "config", "config.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Fatalf("测试配置文件不存在: %s", configFile)
	}

	os.Setenv("UAPCLAW_DATA_DIR", absPath)
	pathutil.ResetCache()
}

// restoreConfig 恢复原始环境变量和 pathutil 缓存
func restoreConfig() {
	if originalDataDir == "" {
		os.Unsetenv("UAPCLAW_DATA_DIR")
	} else {
		os.Setenv("UAPCLAW_DATA_DIR", originalDataDir)
	}
	pathutil.ResetCache()
}
