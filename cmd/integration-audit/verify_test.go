//go:build test

// Package main 提供覆盖交叉验证引擎单元测试。
//
// 运行方式: go test -tags=test ./cmd/integration-audit/...
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeRunner 用于测试的 go test 执行器 mock。
type fakeRunner struct {
	// runErr 模拟执行失败
	runErr error
	// profileContent 执行成功时写出的覆盖率档案内容
	profileContent string
	// runDirs 记录被请求运行的目录
	runDirs []string
}

// RunGoTest 实现 GoTestRunner：记录目录并写合成档案。
func (f *fakeRunner) RunGoTest(testDir string, profilePath string) error {
	f.runDirs = append(f.runDirs, testDir)
	if f.runErr != nil {
		return f.runErr
	}
	return os.WriteFile(profilePath, []byte(f.profileContent), 0o644)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// baselinePollutionProfile 模拟 suite 基线污染：
// 各子系统 15~24 行（与 swarm/server 实测一致），无任何子系统达 30 行。
const baselinePollutionProfile = `mode: set
github.com/uapclaw/uapclaw-go/internal/agentcore/session/session.go:100.13,102.2 2 1
github.com/uapclaw/uapclaw-go/internal/agentcore/session/state.go:50.13,52.2 22 1
github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/client.go:10.13,12.2 21 1
github.com/uapclaw/uapclaw-go/internal/agentcore/runner/runner.go:20.13,22.2 19 1
github.com/uapclaw/uapclaw-go/internal/agentcore/harness/agent.go:30.13,32.2 15 1
github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite/lite.go:40.13,42.2 13 1
github.com/uapclaw/uapclaw-go/internal/common/schema/schema.go:1.13,2.2 5 1
github.com/uapclaw/uapclaw-go/internal/swarm/server/server.go:60.13,62.2 2 1
`

// realCrossCoverageProfile 模拟真实跨子系统覆盖（heartbeat 形态）：
// harness 573 / runner 383 / context_engine 298 / foundation 274 / session 228 行。
const realCrossCoverageProfile = `mode: set
github.com/uapclaw/uapclaw-go/internal/agentcore/harness/agent.go:1.13,80.2 573 1
github.com/uapclaw/uapclaw-go/internal/agentcore/runner/runner.go:1.13,60.2 383 1
github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/engine.go:1.13,70.2 298 1
github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/client.go:1.13,50.2 274 1
github.com/uapclaw/uapclaw-go/internal/agentcore/session/session.go:1.13,40.2 228 1
github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/rail.go:1.13,10.2 20 0
`

// twoSubsystemProfile 两子系统真实覆盖（单模块性质，不达标）。
const twoSubsystemProfile = `mode: set
github.com/uapclaw/uapclaw-go/internal/agentcore/harness/agent.go:1.13,80.2 130 1
github.com/uapclaw/uapclaw-go/internal/agentcore/session/session.go:1.13,40.2 24 1
github.com/uapclaw/uapclaw-go/internal/agentcore/runner/runner.go:1.13,60.2 19 1
`

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestVerifyCoverage_基线污染被识破 测冒充目录：各子系统仅 15~24 行（suite 基线污染）
// 被门禁判不通过——这正是 swarm/server 冒充案例的实测形态
func TestVerifyCoverage_基线污染被识破(t *testing.T) {
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "tests/integration/swarm/server",
		PendingCoverage: true,
	}}}
	runner := &fakeRunner{profileContent: baselinePollutionProfile}

	vr, err := VerifyCoverage(&VerifyConfig{
		RepoRoot:      t.TempDir(),
		MinSubsystems: 3,
		MinLines:      30,
		Runner:        runner,
		WorkDir:       t.TempDir(),
	}, audit)
	require.NoError(t, err)
	assert.False(t, vr.Passed)

	dv := vr.Dirs[0]
	assert.False(t, dv.Passed)
	assert.Empty(t, dv.Qualified)
	assert.Contains(t, dv.Reason, "真实覆盖 0 个子系统")
	// 验证请求的正是灰区目录本身
	assert.Equal(t, []string{"tests/integration/swarm/server"}, runner.runDirs)
}

// TestVerifyCoverage_真实跨子系统通过 测 heartbeat 形态：静跨度=1 但覆盖证明 5 个子系统真实打通
func TestVerifyCoverage_真实跨子系统通过(t *testing.T) {
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "tests/integration/agentcore/harness/rails/heartbeat",
		PendingCoverage: true,
	}}}

	vr, err := VerifyCoverage(&VerifyConfig{
		RepoRoot:      t.TempDir(),
		MinSubsystems: 3,
		MinLines:      30,
		Runner:        &fakeRunner{profileContent: realCrossCoverageProfile},
		WorkDir:       t.TempDir(),
	}, audit)
	require.NoError(t, err)
	assert.True(t, vr.Passed)

	dv := vr.Dirs[0]
	assert.True(t, dv.Passed)
	assert.Len(t, dv.Qualified, 5)
	assert.Contains(t, dv.Qualified, "agentcore/harness")
	assert.Contains(t, dv.Qualified, "agentcore/runner")
	// count=0 的块（rails.go 20 行）不计入 harness 聚合值
	assert.Equal(t, 573, dv.SubsystemLines["agentcore/harness"])
}

// TestVerifyCoverage_两子系统不达标 测单模块性质目录（如 task_loop：harness 130 + 基线）
func TestVerifyCoverage_两子系统不达标(t *testing.T) {
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "tests/integration/agentcore/harness/task_loop",
		PendingCoverage: true,
	}}}

	vr, err := VerifyCoverage(&VerifyConfig{
		RepoRoot:      t.TempDir(),
		MinSubsystems: 3,
		MinLines:      30,
		Runner:        &fakeRunner{profileContent: twoSubsystemProfile},
		WorkDir:       t.TempDir(),
	}, audit)
	require.NoError(t, err)
	assert.False(t, vr.Passed)

	dv := vr.Dirs[0]
	assert.False(t, dv.Passed)
	assert.Equal(t, []string{"agentcore/harness"}, dv.Qualified)
}

// TestVerifyCoverage_跳过已有静态违规的灰区目录 测空壳目录不浪费 CI 时间跑覆盖验证
func TestVerifyCoverage_跳过已有静态违规的灰区目录(t *testing.T) {
	audit := &AuditResult{Dirs: []*DirAudit{
		{
			Dir:             "tests/integration/external/mcp",
			PendingCoverage: true,
			Violations:      []string{"有效测试数 0 < 1"},
		},
	}}

	runner := &fakeRunner{profileContent: baselinePollutionProfile}
	vr, err := VerifyCoverage(&VerifyConfig{
		RepoRoot:      t.TempDir(),
		MinSubsystems: 3,
		MinLines:      30,
		Runner:        runner,
		WorkDir:       t.TempDir(),
	}, audit)
	require.NoError(t, err)
	assert.False(t, vr.Passed)

	dv := vr.Dirs[0]
	assert.True(t, dv.Skipped)
	assert.False(t, dv.Passed)
	// 静态违规目录不应触发 go test
	assert.Empty(t, runner.runDirs)
}

// TestVerifyCoverage_执行失败时判不通过 测 go test 失败（如编译错误）不通过且带原因
func TestVerifyCoverage_执行失败时判不通过(t *testing.T) {
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "tests/integration/broken",
		PendingCoverage: true,
	}}}

	vr, err := VerifyCoverage(&VerifyConfig{
		RepoRoot:      t.TempDir(),
		MinSubsystems: 3,
		MinLines:      30,
		Runner:        &fakeRunner{runErr: os.ErrDeadlineExceeded},
		WorkDir:       t.TempDir(),
	}, audit)
	require.NoError(t, err)
	assert.False(t, vr.Passed)

	dv := vr.Dirs[0]
	assert.True(t, dv.Skipped)
	assert.Contains(t, dv.SkipReason, "go test 执行失败")
}

// TestVerifyCoverage_无灰区目录时直接通过 测全部目录静态达标时零运行
func TestVerifyCoverage_无灰区目录时直接通过(t *testing.T) {
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "tests/integration/agentcore/real",
		PendingCoverage: false,
	}}}

	runner := &fakeRunner{}
	vr, err := VerifyCoverage(&VerifyConfig{
		RepoRoot:      t.TempDir(),
		MinSubsystems: 3,
		MinLines:      30,
		Runner:        runner,
		WorkDir:       t.TempDir(),
	}, audit)
	require.NoError(t, err)
	assert.True(t, vr.Passed)
	assert.Empty(t, vr.Dirs)
	assert.Empty(t, runner.runDirs)
}

// TestParseProfileSubsystemLines 测覆盖档案解析：
// 行格式、common 过滤、count=0 排除、mode 行跳过、同子系统多文件聚合
func TestParseProfileSubsystemLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.out")
	content := `mode: set
github.com/uapclaw/uapclaw-go/internal/agentcore/harness/agent.go:1.13,80.2 573 1
github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/rail.go:1.13,10.2 27 1
github.com/uapclaw/uapclaw-go/internal/agentcore/runner/runner.go:1.13,60.2 383 1
github.com/uapclaw/uapclaw-go/internal/common/schema/schema.go:1.13,2.2 5 1
github.com/uapclaw/uapclaw-go/internal/agentcore/session/session.go:1.13,40.2 228 0
github.com/uapclaw/uapclaw-go/agentcore/notinternal.go:1.13,2.2 5 1
github.com/other/project/internal/foo/bar.go:1.13,2.2 5 1
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	got, err := parseProfileSubsystemLines(path)
	require.NoError(t, err)
	// harness 两文件聚合 573+27=600；session count=0 排除；common 过滤；
	// 非本仓库路径排除
	assert.Equal(t, map[string]int{
		"agentcore/harness": 600,
		"agentcore/runner":  383,
	}, got)
}

// TestParseProfileSubsystemLines_空档案 测空文件返回空 map 不报错
func TestParseProfileSubsystemLines_空档案(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.out")
	require.NoError(t, os.WriteFile(path, []byte("mode: set\n"), 0o644))

	got, err := parseProfileSubsystemLines(path)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestSubsystemOfProfileFile 测档案字段到子系统的提取
func TestSubsystemOfProfileFile(t *testing.T) {
	assert.Equal(t, "agentcore/harness",
		subsystemOfProfileFile("github.com/uapclaw/uapclaw-go/internal/agentcore/harness/agent.go:1.13,80.2"))
	assert.Equal(t, "agentcore/runner",
		subsystemOfProfileFile("github.com/uapclaw/uapclaw-go/internal/agentcore/runner/runner.go:1.13"))
	// 一级包内文件（internal/agentcore/runner.go → 包 agentcore）归为其自身
	assert.Equal(t, "agentcore",
		subsystemOfProfileFile("github.com/uapclaw/uapclaw-go/internal/agentcore/runner.go:1.13"))
	assert.Equal(t, "", subsystemOfProfileFile("no-slash-no-go-colon"))
}

// TestCollectCoverPkgs 测 coverpkg 列表收集（含多行 import、非 internal 忽略）
func TestCollectCoverPkgs(t *testing.T) {
	root := t.TempDir()
	src := `//go:build integration

package fake_test

import (
	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	session "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/suite"
)
`
	require.NoError(t, os.MkdirAll(filepath.Join(root, "agentcore", "x"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "agentcore", "x", "x_test.go"), []byte(src), 0o644))

	pkgs, err := collectCoverPkgs(root)
	require.NoError(t, err)
	assert.Equal(t, strings.Join([]string{
		"github.com/uapclaw/uapclaw-go/internal/agentcore/runner",
		"github.com/uapclaw/uapclaw-go/internal/agentcore/session",
	}, ","), pkgs)
}

// TestSlugProfileName 测目录名转档案文件名
func TestSlugProfileName(t *testing.T) {
	assert.Equal(t, "tests_integration_swarm_server.out",
		slugProfileName("tests/integration/swarm/server"))
}

// TestRunGoTest_真实执行器本地运行 测真实执行器可运行 go test 并产出可解析档案。
// 指向本包自身且 -run 匹配空模式：测试二进制缓存命中、零测试执行
// （避免嵌套递归执行自身），CI 上亚秒级完成，也不冷编译 integration 测试树。
// 运行方式: go test -tags=test ./cmd/integration-audit/...
func TestRunGoTest_真实执行器本地运行(t *testing.T) {
	// 手工构造执行器：coverpkg 指向本包（真实 internal coverpkg 会冷编译百余包）
	runner := &realGoTestRunner{
		root:      "../..",
		tags:      "test",
		coverPkgs: "github.com/uapclaw/uapclaw-go/cmd/integration-audit",
	}

	profile := filepath.Join(t.TempDir(), "p.out")
	// -run 空模式：包内无匹配测试 → 0.0% coverage、零执行、无递归
	err := runner.runGoTestWithRunFilter("cmd/integration-audit", profile, "TestZZZNoMatch")
	require.NoError(t, err)

	// 档案存在且含 mode 行与数据行（cmd 包非 internal/，子系统映射为空是预期）
	data, readErr := os.ReadFile(profile)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "mode:")

	_, parseErr := parseProfileSubsystemLines(profile)
	require.NoError(t, parseErr)
}

// TestRunGoTest_真实执行器失败路径 测试不存在的测试目录返回错误
func TestRunGoTest_真实执行器失败路径(t *testing.T) {
	runner := &realGoTestRunner{
		root:      "../..",
		tags:      "test",
		coverPkgs: "github.com/uapclaw/uapclaw-go/cmd/integration-audit",
	}

	profile := filepath.Join(t.TempDir(), "p.out")
	err := runner.RunGoTest("cmd/nonexistent-dir", profile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go test")
}

// TestNewRealGoTestRunner_成功路径 测试 coverpkg 收集与执行器字段
func TestNewRealGoTestRunner_成功路径(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tests", "integration", "x"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tests", "integration", "x", "x_test.go"),
		[]byte("package x_test\nimport \"github.com/uapclaw/uapclaw-go/internal/agentcore/runner\"\n"), 0o644))

	r, err := newRunner(root)
	require.NoError(t, err)
	require.IsType(t, &realGoTestRunner{}, r)
	real := r.(*realGoTestRunner)
	assert.Equal(t, verifyGoTestTags, real.tags)
	assert.Equal(t, "github.com/uapclaw/uapclaw-go/internal/agentcore/runner", real.coverPkgs)
	assert.True(t, real.cgo)
}

// TestNewRealGoTestRunner_空仓库 测试没有 internal import 的目录返回错误
func TestNewRealGoTestRunner_空仓库(t *testing.T) {
	// 构造存在的 tests/integration 目录但无 internal import
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tests", "integration"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tests", "integration", "x_test.go"),
		[]byte("package x_test\n"), 0o644))

	_, err := newRunner(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coverpkg 为空")
}

// TestTruncateOutput 测输出截断
func TestTruncateOutput(t *testing.T) {
	assert.Equal(t, "abc", truncateOutput([]byte("abc")))
	long := strings.Repeat("x", 3000)
	got := truncateOutput([]byte(long))
	assert.Len(t, got, len("...(截断)")+2000)
	assert.True(t, strings.HasSuffix(got, "...(截断)"))
}
