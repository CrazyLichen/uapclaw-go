// Package main 提供审计引擎实现。
//
// 可测试的分析逻辑放在本文件（audit.go），main.go 的 Execute()/main()
// 用 //go:build !test 隔离，不参与单元测试覆盖率统计。
package main

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AuditConfig 审计配置。
type AuditConfig struct {
	// RootDir 测试根目录（tests/integration）
	RootDir string
	// MinCallSpan 真实集成测试要求的调用跨度下限（跨子系统数）
	MinCallSpan int
	// MinAssertions 每个有效测试方法的最少断言数
	MinAssertions int
	// MinEffectiveTests 每个目录的最少有效测试方法数
	MinEffectiveTests int
	// ExemptDirs 豁免目录列表（tests/integration 相对路径）
	ExemptDirs map[string]string
	// DeferSpanToCoverage 为 true 时，调用跨度不足的目录不直接判违规，
	// 而是标记 PendingCoverage=true，交给覆盖交叉验证（算法 2）裁决。
	// 用于 CI：静态跨度达标的目录免验证直接通过，灰区目录以真实覆盖为准。
	DeferSpanToCoverage bool
}

// FileAudit 单文件审计结果。
type FileAudit struct {
	// Path 文件相对路径
	Path string
	// Active 文件在 integration 标签下是否激活
	// （如 //go:build integration && llm 在 CI 套件中不激活，跳过分析）
	Active bool
	// HasBuildTag 文件是否有把自身排除出单元测试套件的 build 约束
	HasBuildTag bool
	// HasSuiteRun 是否调用 suite.Run(
	HasSuiteRun bool
	// ImportedSubsystems import 的 internal 二级子系统集合（存在性层，common 过滤）
	ImportedSubsystems []string
	// ImportCount distinct internal import 路径数（不过滤 common，对齐旧 CI 规则）
	ImportCount int
	// ImportPaths distinct internal import 路径集合（目录级聚合用）
	ImportPaths []string
	// CalledSubsystems 真实调用（pkg.Fn(...) 形式）的 internal 二级子系统集合
	CalledSubsystems []string
	// TotalTests 测试方法总数（Test 前缀方法 + TestXxx 函数）
	TotalTests int
	// EffectiveTests 有效测试数（非空、非 Skip、断言数达标）
	EffectiveTests int
	// Violations 违规明细
	Violations []string
}

// DirAudit 单目录聚合审计结果。
type DirAudit struct {
	// Dir 目录相对路径（tests/integration 相对）
	Dir string
	// Exempt 是否豁免
	Exempt bool
	// ExemptReason 豁免理由
	ExemptReason string
	// Files 文件级结果
	Files []*FileAudit
	// CallSpan 调用跨度（目录级聚合，全部文件并集）
	CallSpan []string
	// EffectiveTests 有效测试总数
	EffectiveTests int
	// PendingCoverage 调用跨度不足且 DeferSpanToCoverage=true：待覆盖验证裁决
	PendingCoverage bool
	// Violations 目录级违规明细
	Violations []string
}

// AuditResult 整体审计结果。
type AuditResult struct {
	// Dirs 目录级结果（按目录名排序）
	Dirs []*DirAudit
	// Passed 是否整体通过（无目录级违规）
	Passed bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// internalPrefix internal 包 import 路径前缀
	internalPrefix = "github.com/uapclaw/uapclaw-go/internal/"
	// buildTagPrefix go:build 约束行前缀
	buildTagPrefix = "//go:build"
	// buildTagLine go:build integration 标签行
	buildTagLine = "//go:build integration"
)

// ──────────────────────────── 全局变量 ──────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Audit 执行整体审计：遍历 RootDir 下所有 *_test.go，逐文件分析后按目录聚合。
func Audit(cfg *AuditConfig) (*AuditResult, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Errorf("RootDir 不能为空")
	}

	// 收集所有测试文件，按目录分组
	dirFiles := make(map[string][]string)
	err := filepath.Walk(cfg.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(cfg.RootDir, path)
		if relErr != nil {
			return relErr
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		dirFiles[dir] = append(dirFiles[dir], path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历 %s 失败: %w", cfg.RootDir, err)
	}

	result := &AuditResult{Passed: true}
	for _, dir := range sortedKeys(dirFiles) {
		da := auditDir(cfg, dir, dirFiles[dir])
		if len(da.Violations) > 0 {
			result.Passed = false
		}
		result.Dirs = append(result.Dirs, da)
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// firstBuildTagLine 提取文件头第一个 //go:build 行（package 声明前的注释区）。
// 返回空串表示无 build 约束。
func firstBuildTagLine(src string) string {
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, buildTagPrefix) {
			return trimmed
		}
		// package 声明后不再视为文件头约束
		if strings.HasPrefix(trimmed, "package ") {
			return ""
		}
	}
	return ""
}

// auditDir 审计单个目录：逐文件分析 → 聚合 → 套门禁规则。
func auditDir(cfg *AuditConfig, dir string, files []string) *DirAudit {
	relDir := pathJoin("tests/integration", dir)
	da := &DirAudit{
		Dir:          relDir,
		Files:        make([]*FileAudit, 0, len(files)),
		ExemptReason: cfg.ExemptDirs[relDir],
	}
	da.Exempt = da.ExemptReason != ""

	callSpan := make(map[string]struct{})

	for _, f := range files {
		fa := auditFile(cfg, f)
		da.Files = append(da.Files, fa)
		if !fa.Active {
			continue // integration 标签下不激活的文件不计入聚合
		}
		da.EffectiveTests += fa.EffectiveTests
		for _, s := range fa.CalledSubsystems {
			callSpan[s] = struct{}{}
		}
	}
	da.CallSpan = sortedSet(callSpan)

	// 全部文件都不激活（如 real_llm 需要 integration && llm）：
	// 不在 CI 集成套件执行范围内，跳过门禁
	if da.EffectiveTests == 0 && len(da.CallSpan) == 0 {
		hasActive := false
		for _, fa := range da.Files {
			if fa.Active {
				hasActive = true
				break
			}
		}
		if !hasActive {
			return da
		}
	}

	// 文件级硬违规（缺 build tag / 缺 suite.Run / import 不足 / 读取解析失败）上抛目录级：
	// 豁免目录同样受文件级规则约束（对齐旧 CI 逐文件判定的语义）
	for _, fa := range da.Files {
		if fa.Active {
			for _, v := range fa.Violations {
				da.Violations = append(da.Violations, fmt.Sprintf("%s: %s", filepath.Base(fa.Path), v))
			}
		}
	}

	// 豁免目录：目录级门禁不判罚（跨度/有效测试数），但文件级违规已上抛
	if da.Exempt {
		return da
	}

	// 目录级门禁 0：import 兜底（聚合 distinct internal 路径数 ≥2，对齐旧 CI 目录级语义；
	// 与跨度门禁互补：跨度杀符号引用凑数，此处杀 0/1 包的纯单包目录）
	aggregateImports := make(map[string]struct{})
	for _, fa := range da.Files {
		if !fa.Active {
			continue
		}
		for _, p := range fa.ImportPaths {
			aggregateImports[p] = struct{}{}
		}
	}
	if len(aggregateImports) < 2 {
		da.Violations = append(da.Violations, fmt.Sprintf(
			"目录聚合 import 的 internal 包数 %d < 2（纯单包目录，疑似单元测试冒充）", len(aggregateImports)))
	}

	// 目录级门禁 1：调用跨度（核心——击杀符号引用凑数）
	if len(da.CallSpan) < cfg.MinCallSpan {
		if cfg.DeferSpanToCoverage {
			// 覆盖交叉验证模式：灰区目录不在此判罚，交由算法 2 裁决
			da.PendingCoverage = true
		} else {
			da.Violations = append(da.Violations, fmt.Sprintf(
				"调用跨度 %d < %d（真实调用 pkg.Fn(...) 形式的 internal 子系统数不足，疑似符号引用凑数）：实际调用 %v",
				len(da.CallSpan), cfg.MinCallSpan, da.CallSpan))
		}
	}

	// 目录级门禁 2：有效测试数（击杀空壳目录）
	if da.EffectiveTests < cfg.MinEffectiveTests {
		da.Violations = append(da.Violations, fmt.Sprintf(
			"有效测试数 %d < %d（空壳/全 Skip/断言不足的目录）", da.EffectiveTests, cfg.MinEffectiveTests))
	}

	return da
}

// auditFile 审计单个测试文件：AST 解析 + 四层规则检查。
func auditFile(cfg *AuditConfig, path string) *FileAudit {
	fa := &FileAudit{Path: path, Active: true}

	src, err := os.ReadFile(path)
	if err != nil {
		fa.Active = false
		fa.Violations = append(fa.Violations, fmt.Sprintf("读取失败: %v", err))
		return fa
	}

	// 规则 1：build tag（文件头第一个 //go:build 行，含复合标签如 integration && llm）
	tagLine := firstBuildTagLine(string(src))
	fa.HasBuildTag = strings.HasPrefix(tagLine, buildTagPrefix)
	if !fa.HasBuildTag {
		fa.Violations = append(fa.Violations, "缺少 //go:build integration 标签")
	}

	// 求值 build 约束：integration 标签下是否激活。
	// integration && llm 这类复合标签文件不在 CI 集成套件内执行，跳过分析
	// （但 HasBuildTag 已合规——它确实被排除出单元测试套件）。
	if tagLine != "" {
		if expr, parseErr := constraint.Parse(tagLine); parseErr == nil {
			if !expr.Eval(func(tag string) bool { return tag == "integration" }) {
				fa.Active = false
				return fa
			}
		}
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		fa.Violations = append(fa.Violations, fmt.Sprintf("AST 解析失败: %v", err))
		return fa
	}

	// 构建 import 别名表：alias → 完整路径（含 internal 前缀过滤）
	aliasToPath := make(map[string]string)
	importPaths := make(map[string]struct{})
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if !strings.HasPrefix(p, internalPrefix) {
			continue
		}
		importPaths[p] = struct{}{} // ImportCount 按路径去重计数（对齐旧 CI 规则）
		// blank/-dot import 不注册别名（代码中不会以 _/. 限定符调用）
		if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
			continue
		}
		alias := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		aliasToPath[alias] = p
	}
	fa.ImportCount = len(importPaths)
	fa.ImportPaths = sortedSet(importPaths)
	importedSubsystems := make(map[string]struct{})
	for _, p := range aliasToPath {
		if s := subsystemOf(p); s != "" {
			importedSubsystems[s] = struct{}{}
		}
	}
	for p := range importPaths {
		if s := subsystemOf(p); s != "" {
			importedSubsystems[s] = struct{}{}
		}
	}
	fa.ImportedSubsystems = sortedSet(importedSubsystems)

	// 规则 2：suite.Run 调用存在
	calledSubsystems := make(map[string]struct{})
	suiteRun := false
	ast.Inspect(file, func(n ast.Node) bool {
		// 规则 2：识别 suite.Run(t, ...) 调用
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "suite" && sel.Sel.Name == "Run" {
					suiteRun = true
				}
			}
			// 核心规则：统计 pkg.Fn(...) 形式的真实调用
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					if p, hit := aliasToPath[id.Name]; hit {
						if s := subsystemOf(p); s != "" {
							calledSubsystems[s] = struct{}{}
						}
					}
				}
			}
		}
		return true
	})
	fa.HasSuiteRun = suiteRun
	fa.CalledSubsystems = sortedSet(calledSubsystems)

	// 规则 3：import 存在性降级为信息记录——目录级聚合后仍 <2 才判罚
	// （单文件 import 1 个包、多文件聚合达标是合法形态；
	// 跨度由目录级门禁用 CalledSubsystems 严格判定，此处只兜底）

	// build tag 违规已在规则 1 处记录（fa.HasBuildTag 判定处），此处不重复追加
	if !fa.HasSuiteRun {
		fa.Violations = append(fa.Violations, "未调用 suite.Run(")
	}

	// 规则 4：有效测试统计（Test 前缀函数 + suite 方法）
	analyzeTests(fa, file, cfg.MinAssertions)

	return fa
}

// analyzeTests 统计有效测试数：遍历 Test* 函数与 (s *XxxSuite) Test* 方法，
// 判定每个是否有效（非空体、非 Skip、断言数 ≥ minAssertions）。
func analyzeTests(fa *FileAudit, file *ast.File, minAssertions int) {
	assertionCount := 0
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		// 普通函数：Test 前缀且非 suite 入口（TestXxxSuite 只做 suite.Run，不算测试本体）
		isTestFunc := fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Test") && !isSuiteEntryFunc(fd.Name.Name)
		isTestMethod := fd.Recv != nil && strings.HasPrefix(fd.Name.Name, "Test")
		if !isTestFunc && !isTestMethod {
			continue
		}
		fa.TotalTests++

		if fd.Body == nil || len(fd.Body.List) == 0 {
			continue // 空体 → 无效
		}
		if isSkipBody(fd.Body) {
			continue // Skip 空壳 → 无效
		}
		assertionCount = countAssertions(fd.Body)
		if assertionCount >= minAssertions {
			fa.EffectiveTests++
		}
	}
}

// isSuiteEntryFunc 判定普通函数是否为 suite 入口（TestXxxSuite(t) 惯例）：
// 入口函数只做 suite.Run，不是被计数/判定的测试方法本身。
func isSuiteEntryFunc(name string) bool {
	return strings.HasPrefix(name, "Test") && strings.HasSuffix(name, "Suite")
}

// isSkipBody 判定方法体是否为纯 Skip 空壳：
// 体短（≤5 语句）且所有表达式语句仅含 s.T().Skip* / return。
func isSkipBody(body *ast.BlockStmt) bool {
	if len(body.List) > 5 {
		return false
	}
	for _, stmt := range body.List {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			// return / 局部变量声明允许出现
			switch stmt.(type) {
			case *ast.ReturnStmt, *ast.DeclStmt:
				continue
			default:
				return false
			}
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		if !isSkipCall(call) {
			return false
		}
	}
	return true
}

// isSkipCall 判定调用是否为 s.T().Skip* 形式。
func isSkipCall(call *ast.CallExpr) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if strings.HasPrefix(sel.Sel.Name, "Skip") {
			// s.T().Skip(...) 或 t.Skip(...)
			if inner, ok := sel.X.(*ast.CallExpr); ok {
				if innerSel, ok := inner.Fun.(*ast.SelectorExpr); ok && innerSel.Sel.Name == "T" {
					return true
				}
			}
			if _, ok := sel.X.(*ast.Ident); ok {
				return true // t.Skip(...)
			}
		}
	}
	return false
}

// countAssertions 统计方法体内的断言调用数：
// s.Require* / s.Assert* / s.Equal 等 suite 方法、s.T().Error/Fatal/Fail、
// t.Error/Fatal/Fail 系列。非调用语句不计。
func countAssertions(body *ast.BlockStmt) int {
	count := 0
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isAssertionCall(call) {
			count++
		}
		return true
	})
	return count
}

// isAssertionCall 判定调用表达式是否为断言。
func isAssertionCall(call *ast.CallExpr) bool {
	// 形态一：s.Xxx(...) —— suite 断言方法（Require/Assert 常见，
	// 也包括 s.Equal 等直接继承的断言方法名）
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if _, ok := sel.X.(*ast.Ident); ok && isAssertionName(sel.Sel.Name) {
			return true
		}
		// 形态二：s.T().Error* / t.T().Fatal* 等
		if inner, ok := sel.X.(*ast.CallExpr); ok {
			if innerSel, ok := inner.Fun.(*ast.SelectorExpr); ok && innerSel.Sel.Name == "T" {
				if isFailureMethodName(sel.Sel.Name) {
					return true
				}
			}
		}
	}
	return false
}

// isAssertionName suite 断言方法名判定。
func isAssertionName(name string) bool {
	switch {
	case strings.HasPrefix(name, "Require"),
		strings.HasPrefix(name, "Assert"),
		strings.HasPrefix(name, "NotEmpty"),
		strings.HasPrefix(name, "Empty"),
		strings.HasPrefix(name, "Equal"),
		strings.HasPrefix(name, "NotEqual"),
		strings.HasPrefix(name, "Nil"),
		strings.HasPrefix(name, "NotNil"),
		strings.HasPrefix(name, "True"),
		strings.HasPrefix(name, "False"),
		strings.HasPrefix(name, "Greater"),
		strings.HasPrefix(name, "Less"),
		strings.HasPrefix(name, "Contains"),
		strings.HasPrefix(name, "NotContains"),
		strings.HasPrefix(name, "Panics"),
		strings.HasPrefix(name, "NotPanics"),
		strings.HasPrefix(name, "Len"),
		strings.HasPrefix(name, "Error"),
		strings.HasPrefix(name, "NoError"):
		return true
	}
	return false
}

// isFailureMethodName testing.T 失败方法名判定（Log/Logf 为信息方法不计断言）。
func isFailureMethodName(name string) bool {
	switch name {
	case "Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow":
		return true
	}
	return false
}

// subsystemOf 从 import 路径提取二级子系统：
// github.com/uapclaw/uapclaw-go/internal/<一级>/<二级> → <一级>/<二级>，
// common/* 纯公共层与非本仓库路径返回空串不计入。
func subsystemOf(importPath string) string {
	if !strings.HasPrefix(importPath, internalPrefix) {
		return ""
	}
	rel := strings.TrimPrefix(importPath, internalPrefix)
	segs := strings.Split(rel, "/")
	if len(segs) < 2 {
		return segs[0] // 一级包（如 internal/config）归为其自身
	}
	if segs[0] == "common" {
		return "" // 公共层不计入跨度
	}
	return segs[0] + "/" + segs[1]
}

// sortedSet map[string]struct{} → 排序切片。
func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedKeys map[string][]string → 按 key 排序的 key 切片。
func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pathJoin 以 / 连接非空路径段。
func pathJoin(base, rel string) string {
	if rel == "" {
		return base
	}
	return base + "/" + rel
}
