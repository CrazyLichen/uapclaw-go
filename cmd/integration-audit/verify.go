// Package main 提供覆盖交叉验证引擎（算法 2）。
//
// 对静态审计（算法 1）判定的灰区目录（调用跨度不足但无其他静态违规），
// 独立运行 go test 收集目录级覆盖档案，按 internal 二级子系统聚合覆盖行数：
// 覆盖行数 ≥ MinLines 的子系统计为"真实覆盖"，真实覆盖子系统数须 ≥ MinSubsystems。
// 共享 suite 基类的 SetupTest 初始化会给每个目录带来约 15~24 行的"基线污染"，
// MinLines 阈值（默认 30）用于区分基线污染与测试体驱动的真实覆盖。
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoTestRunner go test 执行器抽象（单元测试可注入 mock）。
type GoTestRunner interface {
	// RunGoTest 运行测试目录（仓库相对路径）的测试，把覆盖率档案写入 profilePath。
	RunGoTest(testDir string, profilePath string) error
}

// VerifyConfig 覆盖交叉验证配置。
type VerifyConfig struct {
	// RepoRoot 仓库根目录（go test 的执行目录）
	RepoRoot string
	// MinSubsystems 真实覆盖子系统数下限
	MinSubsystems int
	// MinLines 每个子系统的覆盖行数下限（高于 suite 基线污染）
	MinLines int
	// Runner go test 执行器（nil 时构造真实执行器）
	Runner GoTestRunner
	// WorkDir 覆盖档案输出目录（空则使用临时目录）
	WorkDir string
}

// DirVerify 单目录覆盖验证结果。
type DirVerify struct {
	// Dir 目录（仓库相对路径）
	Dir string `json:"dir"`
	// Skipped 是否跳过验证（已有静态违规或执行失败）
	Skipped bool `json:"skipped"`
	// SkipReason 跳过/失败原因
	SkipReason string `json:"skip_reason,omitempty"`
	// SubsystemLines 各子系统覆盖行数（含未达标的）
	SubsystemLines map[string]int `json:"subsystem_lines"`
	// Qualified 覆盖行数达标的子系统列表（排序）
	Qualified []string `json:"qualified"`
	// Passed 是否通过
	Passed bool `json:"passed"`
	// Reason 判定说明
	Reason string `json:"reason"`
}

// VerifyResult 覆盖验证整体结果。
type VerifyResult struct {
	// Dirs 各目录验证结果
	Dirs []*DirVerify `json:"dirs"`
	// Passed 是否全部通过
	Passed bool `json:"passed"`
}

// realGoTestRunner 真实 go test 执行器。
type realGoTestRunner struct {
	// root 仓库根目录（命令执行目录）
	root string
	// tags go test 构建标签（与 CI 集成测试主运行保持一致以复用构建缓存）
	tags string
	// coverPkgs 逗号分隔的 coverpkg 列表
	coverPkgs string
	// cgo 是否设置 CGO_ENABLED=1（sqlite_fts5 需要）
	cgo bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultVerifySubsystems 默认真实覆盖子系统数下限
	defaultVerifySubsystems = 3
	// defaultVerifyLines 默认每子系统覆盖行数下限：
	// 实测 suite 基线污染最高约 24 行（agentcore/session），30 行留出安全分隔
	defaultVerifyLines = 30
	// verifyGoTestTags 验证运行的构建标签（与 CI 集成测试主运行一致）
	verifyGoTestTags = "sqlite_fts5 test integration"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// internalImportRe 从源码中提取 internal import 路径的正则（对齐 CI 的 grep 语义）。
var internalImportRe = regexp.MustCompile(`"` + regexp.QuoteMeta(internalPrefix) + `([^"]*)"`)

// newRunner 真实执行器构造工厂（测试可替换为 mock）。
var newRunner = newRealGoTestRunner

// ──────────────────────────── 导出函数 ────────────────────────────

// VerifyCoverage 对灰区目录执行覆盖交叉验证：
// 逐目录独立运行 go test 收集覆盖档案 → 按子系统聚合覆盖行数 → 应用
// MinSubsystems × MinLines 门禁。已有静态违规的目录跳过运行（直接判不通过）。
func VerifyCoverage(cfg *VerifyConfig, audit *AuditResult) (*VerifyResult, error) {
	runner := cfg.Runner
	if runner == nil {
		r, err := newRunner(cfg.RepoRoot)
		if err != nil {
			return nil, fmt.Errorf("构造 go test 执行器失败: %w", err)
		}
		runner = r
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		tmp, err := os.MkdirTemp("", "integration-verify-")
		if err != nil {
			return nil, fmt.Errorf("创建临时目录失败: %w", err)
		}
		workDir = tmp
	} else if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建档案目录 %s 失败: %w", workDir, err)
	}

	vr := &VerifyResult{Passed: true}
	for _, d := range audit.Dirs {
		if !d.PendingCoverage {
			continue
		}
		// 已有静态违规（如空壳目录）的灰区目录：无需浪费 CI 时间跑覆盖验证
		if len(d.Violations) > 0 {
			vr.Dirs = append(vr.Dirs, &DirVerify{
				Dir:            d.Dir,
				Skipped:        true,
				SkipReason:     "已有静态违规，无需覆盖验证",
				SubsystemLines: map[string]int{},
				Passed:         false,
			})
			vr.Passed = false
			continue
		}
		dv := verifyDir(cfg, runner, workDir, d)
		vr.Dirs = append(vr.Dirs, dv)
		if !dv.Passed {
			vr.Passed = false
		}
	}
	return vr, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// verifyDir 验证单个灰区目录：运行测试 → 解析档案 → 套门禁。
func verifyDir(cfg *VerifyConfig, runner GoTestRunner, workDir string, d *DirAudit) *DirVerify {
	dv := &DirVerify{Dir: d.Dir, SubsystemLines: map[string]int{}}

	profilePath := filepath.Join(workDir, slugProfileName(d.Dir))
	if err := runner.RunGoTest(d.Dir, profilePath); err != nil {
		dv.Skipped = true
		dv.SkipReason = fmt.Sprintf("go test 执行失败: %v", err)
		dv.Passed = false
		return dv
	}

	subLines, err := parseProfileSubsystemLines(profilePath)
	if err != nil {
		dv.Skipped = true
		dv.SkipReason = fmt.Sprintf("覆盖率档案解析失败: %v", err)
		dv.Passed = false
		return dv
	}
	dv.SubsystemLines = subLines

	for sub, lines := range subLines {
		if lines >= cfg.MinLines {
			dv.Qualified = append(dv.Qualified, sub)
		}
	}
	sort.Strings(dv.Qualified)

	if len(dv.Qualified) >= cfg.MinSubsystems {
		dv.Passed = true
		dv.Reason = fmt.Sprintf("真实覆盖 %d 个子系统（要求 ≥ %d）", len(dv.Qualified), cfg.MinSubsystems)
	} else {
		dv.Passed = false
		dv.Reason = fmt.Sprintf("真实覆盖 %d 个子系统（要求 ≥ %d，每子系统 ≥ %d 行）",
			len(dv.Qualified), cfg.MinSubsystems, cfg.MinLines)
	}
	return dv
}

// slugProfileName 目录路径转覆盖档案文件名（/ 替换为 _）。
func slugProfileName(dir string) string {
	return strings.ReplaceAll(dir, "/", "_") + ".out"
}

// parseProfileSubsystemLines 解析 go test 覆盖档案，按 internal 二级子系统聚合覆盖行数。
// 档案行格式：`包路径/文件.go:起始行.列,结束行.列 语句数 执行次数`；
// 仅统计执行次数 > 0 的块；common/* 公共层与非本仓库路径不计入。
func parseProfileSubsystemLines(profilePath string) (map[string]int, error) {
	f, err := os.Open(profilePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		// 只读文件，Close 失败不影响解析结果，显式忽略
		_ = f.Close()
	}()

	result := make(map[string]int)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || count <= 0 || stmts <= 0 {
			continue
		}
		sub := subsystemOfProfileFile(fields[0])
		if sub == "" {
			continue
		}
		result[sub] += stmts
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// subsystemOfProfileFile 从覆盖档案的 `路径/文件.go:位置` 字段提取二级子系统。
func subsystemOfProfileFile(fileField string) string {
	path := fileField
	if i := strings.Index(path, ":"); i >= 0 {
		path = path[:i]
	}
	// 去掉文件名得到包路径
	i := strings.LastIndex(path, "/")
	if i < 0 || !strings.HasSuffix(path, ".go") {
		return ""
	}
	return subsystemOf(path[:i])
}

// newRealGoTestRunner 构造真实 go test 执行器：
// 扫描 tests/integration 下全部 .go 文件的 internal import，
// 生成与 CI 集成测试主运行一致的 coverpkg 列表。
func newRealGoTestRunner(root string) (GoTestRunner, error) {
	coverPkgs, err := collectCoverPkgs(filepath.Join(root, "tests", "integration"))
	if err != nil {
		return nil, err
	}
	return &realGoTestRunner{
		root:      root,
		tags:      verifyGoTestTags,
		coverPkgs: coverPkgs,
		cgo:       true,
	}, nil
}

// RunGoTest 运行指定测试目录的测试并写出覆盖率档案。
func (r *realGoTestRunner) RunGoTest(testDir string, profilePath string) error {
	return r.runGoTestWithRunFilter(testDir, profilePath, "")
}

// runGoTestWithRunFilter 运行 go test，可附加 -run 过滤（空则不过滤）。
// 单元测试自指向场景传空匹配模式：零测试执行、零递归、档案仍正常产出。
func (r *realGoTestRunner) runGoTestWithRunFilter(testDir string, profilePath string, runFilter string) error {
	args := []string{"test", "-mod=mod"}
	if r.tags != "" {
		args = append(args, "-tags", r.tags)
	}
	if runFilter != "" {
		args = append(args, "-run", runFilter)
	}
	args = append(args,
		"-coverprofile="+profilePath,
		"-coverpkg="+r.coverPkgs,
		"./"+testDir+"/...")

	cmd := exec.Command("go", args...)
	cmd.Dir = r.root
	env := os.Environ()
	if r.cgo {
		env = append(env, "CGO_ENABLED=1")
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go test %s 失败: %w\n%s", testDir, err, truncateOutput(out))
	}
	return nil
}

// truncateOutput 截断过长的命令输出（仅错误诊断用）。
func truncateOutput(out []byte) string {
	const max = 2000
	if len(out) <= max {
		return string(out)
	}
	return string(out[:max]) + "...(截断)"
}

// collectCoverPkgs 扫描测试树下全部 .go 文件，收集 distinct internal import 路径，
// 返回逗号分隔的 coverpkg 列表（对齐 CI 的 grep 语义，不过滤 build tag）。
func collectCoverPkgs(testRoot string) (string, error) {
	set := make(map[string]struct{})
	err := filepath.Walk(testRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, m := range internalImportRe.FindAllStringSubmatch(string(data), -1) {
			set[internalPrefix+m[1]] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("扫描 %s 失败: %w", testRoot, err)
	}
	if len(set) == 0 {
		return "", fmt.Errorf("%s 下未发现 internal import，coverpkg 为空", testRoot)
	}
	pkgs := make([]string, 0, len(set))
	for p := range set {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return strings.Join(pkgs, ","), nil
}
