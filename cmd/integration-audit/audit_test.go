//go:build test

// Package main 提供 integration-audit 审计引擎单元测试。
//
// 运行方式: go test -tags=test ./cmd/integration-audit/...
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeFixture 写入临时测试文件。
func writeFixture(t *testing.T, root, rel string, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// realIntegrationFixture 真集成测试样例：三个子系统真实调用 + 断言。
const realIntegrationFixture = `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestE2E() {
	r := runner.NewRunner()
	sess := session.NewSession()
	agent := harness.CreateDeepAgent(nil)
	result, err := r.Invoke(agent, sess)
	s.Require().NoError(err)
	s.Equal(3, len(result))
}
`

// symbolRefFakeFixture 符号引用凑数样例：只引用函数值不调用。
const symbolRefFakeFixture = `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestSymbolRef() {
	s.NotNil(runner.GetResourceMgr)
	_ = harness.DeepAgent{}
	_ = session.Session{}
	s.Require().NotNil(runner.NewRunner)
}
`

// skipShellFixture 空壳 Skip 测试样例。
const skipShellFixture = `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestPending() {
	s.T().Skip("待补充：需要重构为导出 API")
}
`

// aliasedImportFixture 别名 import 场景：agentinterfaces 形式的真实调用。
const aliasedImportFixture = `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestAliased() {
	opts := agentinterfaces.WithSession(nil)
	r := runner.NewRunner()
	h := harness.NewDeepAgent()
	s.Require().NotNil(opts)
	s.NotNil(r)
	s.NotNil(h)
}
`

// commonOnlyFixture 仅 import common 公共层的样例（不计入跨度）。
const commonOnlyFixture = `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestCommon() {
	_ = logger.ComponentAgentCore
	r := runner.NewRunner()
	s.Require().NotNil(r)
}
`

// noBuildTagFixture 缺少 build tag 的样例。
const noBuildTagFixture = `package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestX() {
	r := runner.NewRunner()
	s.Require().NotNil(r)
}
`

// TestAudit_真集成测试通过 测三个子系统真实调用的目录应通过
func TestAudit_真集成测试通过(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "agentcore/real/real_test.go", realIntegrationFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       3,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)
	assert.True(t, result.Passed)

	dir := result.Dirs[0]
	assert.Equal(t, "tests/integration/agentcore/real", dir.Dir)
	assert.Len(t, dir.CallSpan, 3)
	assert.Contains(t, dir.CallSpan, "agentcore/runner")
	assert.Contains(t, dir.CallSpan, "agentcore/session")
	assert.Contains(t, dir.CallSpan, "agentcore/harness")
	assert.Equal(t, 1, dir.EffectiveTests)
}

// TestAudit_符号引用凑数被抓 测 s.NotNil(pkg.Fn) 函数值引用不计入调用跨度
func TestAudit_符号引用凑数被抓(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "fake/symbol/symbol_test.go", symbolRefFakeFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       3,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)
	assert.False(t, result.Passed)

	dir := result.Dirs[0]
	// runner/harness/session 三包 import 但零真实调用（harness.CreateDeepAgent(nil) 无——
	// 该 fixture 中 CreateDeepAgent 出现于真实调用 fixture；此处仅符号引用）
	assert.Empty(t, dir.CallSpan)
	require.Len(t, dir.Violations, 1)
	assert.Contains(t, dir.Violations[0], "调用跨度 0 < 3")
}

// TestAudit_Skip空壳被判无效 测纯 Skip 方法不计入有效测试数
func TestAudit_Skip空壳被判无效(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "fake/shell/shell_test.go", skipShellFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       3,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)
	assert.False(t, result.Passed)

	dir := result.Dirs[0]
	assert.Equal(t, 1, dir.Files[0].TotalTests)
	assert.Equal(t, 0, dir.EffectiveTests)
}

// TestAudit_别名import真实调用 测 agentinterfaces 别名形式调用正确归组
func TestAudit_别名import真实调用(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "fake/alias/alias_test.go", aliasedImportFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       3,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)

	dir := result.Dirs[0]
	assert.Len(t, dir.CallSpan, 3)
	assert.Contains(t, dir.CallSpan, "agentcore/single_agent")
}

// TestAudit_common公共层不计跨度 测 common/* import 被过滤
func TestAudit_common公共层不计跨度(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "fake/common/common_test.go", commonOnlyFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       2,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)

	dir := result.Dirs[0]
	assert.Equal(t, []string{"agentcore/runner"}, dir.CallSpan)
}

// TestAudit_缺BuildTag违规 测无 //go:build integration 的文件被标记
func TestAudit_缺BuildTag违规(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "fake/notag/notag_test.go", noBuildTagFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       2,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)

	fa := result.Dirs[0].Files[0]
	assert.False(t, fa.HasBuildTag)
	assert.Contains(t, fa.Violations[0], "go:build integration")
}

// TestAudit_豁免目录不判罚 测登记在册的目录跳过门禁
func TestAudit_豁免目录不判罚(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "fake/exempt/exempt_test.go", symbolRefFakeFixture)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       3,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{"tests/integration/fake/exempt": "真跨进程"},
	})
	require.NoError(t, err)

	dir := result.Dirs[0]
	assert.True(t, dir.Exempt)
	assert.Equal(t, "真跨进程", dir.ExemptReason)
	assert.Empty(t, dir.Violations)
	assert.True(t, result.Passed)
}

// TestAudit_多目录聚合 测目录级并集聚合：两个文件各调不同子系统合并计数
func TestAudit_多目录聚合(t *testing.T) {
	root := t.TempDir()
	fileA := `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestA() {
	r := runner.NewRunner()
	s.Require().NotNil(r)
	s.NotNil(r)
}
`
	fileB := `//go:build integration

package fake_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
)

type FakeSuite struct {
	suite.Suite
}

func TestFakeSuite(t *testing.T) {
	suite.Run(t, new(FakeSuite))
}

func (s *FakeSuite) TestB() {
	x := session.NewSession()
	y := harness.NewDeepAgent()
	s.Require().NotNil(x)
	s.NotNil(y)
}
`
	writeFixture(t, root, "agentcore/multi/a_test.go", fileA)
	writeFixture(t, root, "agentcore/multi/b_test.go", fileB)

	result, err := Audit(&AuditConfig{
		RootDir:           root,
		MinCallSpan:       3,
		MinAssertions:     2,
		MinEffectiveTests: 1,
		ExemptDirs:        map[string]string{},
	})
	require.NoError(t, err)

	dir := result.Dirs[0]
	assert.Len(t, dir.CallSpan, 3)
	assert.Equal(t, 2, dir.EffectiveTests)
	assert.True(t, result.Passed)
}

// TestSubsystemOf 测子系统路径提取
func TestSubsystemOf(t *testing.T) {
	assert.Equal(t, "agentcore/runner", subsystemOf("github.com/uapclaw/uapclaw-go/internal/agentcore/runner"))
	assert.Equal(t, "agentcore/foundation", subsystemOf("github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"))
	assert.Equal(t, "", subsystemOf("github.com/uapclaw/uapclaw-go/internal/common/logger"))
	assert.Equal(t, "config", subsystemOf("github.com/uapclaw/uapclaw-go/internal/config"))
	assert.Equal(t, "", subsystemOf("github.com/other/project/internal/foo/bar"))
}

// TestIsSkipBody 测纯 Skip 空壳方法体识别（AST 级）
func TestIsSkipBody(t *testing.T) {
	cases := []struct {
		name string
		src  string
		skip bool
	}{
		{"纯Skip", "func (s *S) T() {\n s.T().Skip(\"a\")\n}", true},
		{"Skipf", "func (s *S) T() {\n s.T().Skipf(\"%d\", 1)\n}", true},
		{"Skip后return", "func (s *S) T() {\n s.T().Skip(\"a\")\n return\n}", true},
		{"正常断言", "func (s *S) T() {\n s.Equal(1, 1)\n}", false},
		{"Skip后有真实逻辑", "func (s *S) T() {\n s.T().Skip(\"a\")\n x := 1\n y := 2\n z := x + y\n w := z * 2\n v := w + 1\n _ = v\n}", false},
		// 空体：isSkipBody 定义上为 true（零语句循环），analyzeTests 已单独拦截空体
		{"空体", "func (s *S) T() {\n}", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "t.go", "package p\n"+c.src, 0)
			require.NoError(t, err)
			fd := file.Decls[0].(*ast.FuncDecl)
			assert.Equal(t, c.skip, isSkipBody(fd.Body))
		})
	}
}

// TestCountAssertions 测断言计数
func TestCountAssertions(t *testing.T) {
	src := `package p
func (s *S) T() {
	s.Require().NoError(nil)
	s.Equal(1, 1)
	s.NotNil(x)
	_ = doWork()
	s.T().Errorf("bad")
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "t.go", src, 0)
	require.NoError(t, err)
	fd := file.Decls[0].(*ast.FuncDecl)
	assert.Equal(t, 4, countAssertions(fd.Body))
}

// TestLoadExemptFile 测豁免清单解析
func TestLoadExemptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exempt")
	content := `# 注释行

tests/integration/external/spawn
tests/integration/fake/exempt
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	got := loadExemptFile(path)
	assert.Len(t, got, 2)
	assert.Contains(t, got, "tests/integration/external/spawn")
	assert.Contains(t, got, "tests/integration/fake/exempt")

	// 不存在的文件返回空 map
	empty := loadExemptFile(filepath.Join(t.TempDir(), "nope"))
	assert.Empty(t, empty)
}
