//go:build test

// Package main 提供 CLI 层（runCLI/report）单元测试。
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// twoSubsystemSpanFixture 灰区目录样例：调用跨度 2（< 3 触发覆盖验证）。
const twoSubsystemSpanFixture = `//go:build integration

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

func (s *FakeSuite) TestGray() {
	r := runner.NewRunner()
	x := session.NewSession()
	s.Require().NotNil(r)
	s.NotNil(x)
}
`

// buildAuditRoot 构建含真集成/冒充两类目录的测试根目录，返回 root 与豁免文件路径。
func buildAuditRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()

	// 真集成目录：三子系统真实调用
	writeFixture(t, root, "agentcore/real/real_test.go", realIntegrationFixture)
	// 冒充目录：符号引用凑数
	writeFixture(t, root, "fake/symbol/symbol_test.go", symbolRefFakeFixture)

	exemptPath := filepath.Join(t.TempDir(), "exempt")
	require.NoError(t, os.WriteFile(exemptPath, []byte(
		"# 测试豁免\ntests/integration/fake/symbol\n"), 0o644))
	return root, exemptPath
}

// TestRunCLI_全部通过 测合规目录退出码 0
func TestRunCLI_全部通过(t *testing.T) {
	root, exemptPath := buildAuditRoot(t)

	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "3",
		"-exempt-file", exemptPath,
	})
	assert.Equal(t, 0, code)
}

// TestRunCLI_违规返回1 测冒充目录未豁免时退出码 1
func TestRunCLI_违规返回1(t *testing.T) {
	root, _ := buildAuditRoot(t)

	// 不加载豁免文件（指向不存在的路径）→ fake/symbol 违规
	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "3",
		"-exempt-file", filepath.Join(t.TempDir(), "nope"),
	})
	assert.Equal(t, 1, code)
}

// TestRunCLI_参数错误返回2 测试非法参数退出码 2
func TestRunCLI_参数错误返回2(t *testing.T) {
	code := runCLI([]string{"-unknown-flag"})
	assert.Equal(t, 2, code)
}

// TestRunCLI_JSON输出 测 JSON 报告可解析且字段完整
func TestRunCLI_JSON输出(t *testing.T) {
	root, exemptPath := buildAuditRoot(t)

	// 捕获 stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "3",
		"-exempt-file", exemptPath,
		"-json",
	})

	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.Equal(t, 0, code)
	var payload struct {
		Audit  *AuditResult  `json:"audit"`
		Verify *VerifyResult `json:"verify"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.True(t, payload.Audit.Passed)
	assert.Nil(t, payload.Verify) // 未开 -verify 时无验证层
	require.Len(t, payload.Audit.Dirs, 2)
	// fake/symbol 豁免、agentcore/real 通过
	for _, d := range payload.Audit.Dirs {
		if strings.HasSuffix(d.Dir, "fake/symbol") {
			assert.True(t, d.Exempt)
		}
		if strings.HasSuffix(d.Dir, "agentcore/real") {
			assert.False(t, d.Exempt)
			assert.Len(t, d.CallSpan, 3)
		}
	}
}

// TestRunCLI_低阈值下违规目录也通过 测阈值可配置（min-call-span=0 关闭跨度门禁）
func TestRunCLI_低阈值下违规目录也通过(t *testing.T) {
	root, _ := buildAuditRoot(t)

	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "0",
		"-min-effective-tests", "0",
		"-exempt-file", filepath.Join(t.TempDir(), "nope"),
	})
	assert.Equal(t, 0, code)
}

// TestPrintReport_文本输出含处置指引 测违规时报告含指引文案
func TestPrintReport_文本输出含处置指引(t *testing.T) {
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
	require.False(t, result.Passed)

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	printReport(result, false)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	out := buf.String()
	assert.Contains(t, out, "[违规]")
	assert.Contains(t, out, "处置指引")
	assert.Contains(t, out, "调用跨度")
}

// TestPrintReport_通过目录展示 测通过目录的展示行
func TestPrintReport_通过目录展示(t *testing.T) {
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
	require.True(t, result.Passed)

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	printReport(result, false)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	out := buf.String()
	assert.Contains(t, out, "[通过]")
	assert.Contains(t, out, "tests/integration/agentcore/real")
	assert.NotContains(t, out, "处置指引")
}

// TestRunCLI_verify模式_基线污染不通过 测 -verify 标志触发覆盖验证层，
// 冒充目录（各子系统仅基线污染行数）被判不通过
func TestRunCLI_verify模式_基线污染不通过(t *testing.T) {
	// 替换 runner 工厂为 mock（测试后恢复）
	oldNewRunner := newRunner
	newRunner = func(root string) (GoTestRunner, error) {
		return &fakeRunner{profileContent: baselinePollutionProfile}, nil
	}
	t.Cleanup(func() { newRunner = oldNewRunner })

	root, _ := buildAuditRoot(t)
	// 构造灰区目录：调用跨度 2 < 3 → PendingCoverage
	writeFixture(t, root, "agentcore/gray/gray_test.go", twoSubsystemSpanFixture)

	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "3",
		"-verify",
		"-verify-workdir", t.TempDir(),
		"-exempt-file", filepath.Join(t.TempDir(), "nope"),
	})
	assert.Equal(t, 1, code)
}

// TestRunCLI_verify模式_真实覆盖通过 测灰区目录覆盖验证通过时退出码 0
func TestRunCLI_verify模式_真实覆盖通过(t *testing.T) {
	oldNewRunner := newRunner
	newRunner = func(root string) (GoTestRunner, error) {
		return &fakeRunner{profileContent: realCrossCoverageProfile}, nil
	}
	t.Cleanup(func() { newRunner = oldNewRunner })

	root, exemptPath := buildAuditRoot(t)
	writeFixture(t, root, "agentcore/gray/gray_test.go", twoSubsystemSpanFixture)

	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "3",
		"-verify",
		"-verify-workdir", t.TempDir(),
		"-exempt-file", exemptPath,
	})
	assert.Equal(t, 0, code)
}

// TestRunCLI_verify模式_JSON输出 测 -verify -json 组合输出可解析
func TestRunCLI_verify模式_JSON输出(t *testing.T) {
	oldNewRunner := newRunner
	newRunner = func(root string) (GoTestRunner, error) {
		return &fakeRunner{profileContent: realCrossCoverageProfile}, nil
	}
	t.Cleanup(func() { newRunner = oldNewRunner })

	root, exemptPath := buildAuditRoot(t)
	writeFixture(t, root, "agentcore/gray/gray_test.go", twoSubsystemSpanFixture)

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	code := runCLI([]string{
		"-root", root,
		"-min-call-span", "3",
		"-verify",
		"-verify-workdir", t.TempDir(),
		"-exempt-file", exemptPath,
		"-json",
	})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.Equal(t, 0, code)
	var payload struct {
		Audit  *AuditResult  `json:"audit"`
		Verify *VerifyResult `json:"verify"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	require.NotNil(t, payload.Verify)
	assert.True(t, payload.Verify.Passed)
	// 灰区目录被运行
	require.Len(t, payload.Verify.Dirs, 1)
	assert.True(t, payload.Verify.Dirs[0].Passed)
	assert.Contains(t, payload.Verify.Dirs[0].Qualified, "agentcore/harness")
}

// TestPrintVerifyReport_文本输出 测验证报告文本含门禁判定与覆盖分布
func TestPrintVerifyReport_文本输出(t *testing.T) {
	vr := &VerifyResult{
		Dirs: []*DirVerify{{
			Dir:            "tests/integration/fake",
			SubsystemLines: map[string]int{"agentcore/harness": 542, "agentcore/session": 24},
			Qualified:      []string{"agentcore/harness"},
			Passed:         false,
			Reason:         "真实覆盖 1 个子系统（要求 ≥ 3，每子系统 ≥ 30 行）",
		}},
		Passed: false,
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	printVerifyReport(vr)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	out := buf.String()
	assert.Contains(t, out, "覆盖交叉验证")
	assert.Contains(t, out, "[不通过]")
	assert.Contains(t, out, "agentcore/harness:542*")
	assert.Contains(t, out, "agentcore/session:24")
	assert.Contains(t, out, "处置指引")
}

// TestPrintVerifyReport_跳过目录 测试跳过目录的展示
func TestPrintVerifyReport_跳过目录(t *testing.T) {
	vr := &VerifyResult{
		Dirs: []*DirVerify{{
			Dir:        "tests/integration/fake",
			Skipped:    true,
			SkipReason: "已有静态违规，无需覆盖验证",
		}},
		Passed: false,
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	printVerifyReport(vr)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	out := buf.String()
	assert.Contains(t, out, "[跳过]")
	assert.Contains(t, out, "已有静态违规")
	// vr.Passed=false（跳过即不通过）→ 输出处置指引
	assert.Contains(t, out, "处置指引")
}
