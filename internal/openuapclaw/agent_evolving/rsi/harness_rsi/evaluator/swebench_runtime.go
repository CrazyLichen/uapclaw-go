// swebench_runtime.go SWE-bench 用例准备与官方验证助手。
// Python: openjiuwen/rsi/harness_rsi/evaluator/swebench_runtime.py（833 行全量对齐）。
//
// 对齐说明：
// - subprocess 注入点：Python monkeypatch subprocess.run → Go 包级函数变量
//   swebenchRun（实现与 terminal_bench 共用 subprocessRunWithCwd）；非架构缝。
// - os.name 判定 → 包级变量 swebenchOsName（runtime.GOOS；测试替换，非架构缝）。
// - _swebench_official_support/sitecustomize.py 以 go:embed 资产保留（父 spec §8）；
//   Python 是磁盘目录直接作 PYTHONPATH，Go 在首次使用时解包到临时目录
//   （ensureOfficialSupportDir），语义等价登记于父 spec §10 偏差 3 的执行细节。
// - subprocess.run(timeout=N) → context 限时 + errTBDeadline 哨兵（同 terminal_bench）。

package evaluator

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	judger "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// logComponentSweb SWE-bench runtime 日志组件。
const logComponentSweb = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// SWEbenchInfrastructureError 官方验证器无法产出可信模型信号时抛出
// （Python swebench_runtime.py:46 SWEbenchInfrastructureError(
// EvaluationInfrastructureError)）。
type SWEbenchInfrastructureError struct {
	// EvaluationInfrastructureError 基类（Go 无继承，嵌入承载）
	*EvaluationInfrastructureError
}

// PrepareSwebenchParams PrepareSwebenchWorkspace 参数
// （Python prepare_swebench_workspace 关键字参数）。
type PrepareSwebenchParams struct {
	// Case 考题映射
	Case map[string]any
	// WorkspaceDir 工作区目录
	WorkspaceDir string
	// TimeoutSec docker cp 超时秒数
	TimeoutSec int
}

// RunOfficialParams RunOfficialSwebenchEvaluation 参数
// （Python run_official_swebench_evaluation 关键字参数）。
type RunOfficialParams struct {
	// Case 考题映射
	Case map[string]any
	// ModelPatch 模型 patch 原文
	ModelPatch string
	// OutputDir 用例输出目录
	OutputDir string
}

// officialRunContext _run_official_command_with_retries 内部传参包。
type officialRunContext struct {
	command        []string
	timeoutSec     int
	verifierDir    string
	runID          string
	reportPath     string
	testOutputPath string
	config         map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// swebenchDefaultWslDistro Windows/WSL 分支默认发行版（Python DEFAULT_WSL_DISTRO）
	swebenchDefaultWslDistro = "Ubuntu-24.04"
	// swebenchDefaultWslPython Windows/WSL 分支默认解释器（Python DEFAULT_WSL_PYTHON）
	swebenchDefaultWslPython = "python3"
	// swebenchTestOutputExcerptChars 测试输出摘录总长（Python _TEST_OUTPUT_EXCERPT_CHARS）
	swebenchTestOutputExcerptChars = 8000
	// swebenchTestOutputExcerptHeadChars 测试输出摘录头长（Python _TEST_OUTPUT_EXCERPT_HEAD_CHARS）
	swebenchTestOutputExcerptHeadChars = 1500
)

// swebenchInfrastructureFailurePatterns 环境类失败模式表
// （Python DEFAULT_INFRASTRUCTURE_FAILURE_PATTERNS 逐字对齐）。
var swebenchInfrastructureFailurePatterns = []string{
	"AttributeError: `np.Inf` was removed in the NumPy 2.0 release",
	"numpy.dtype size changed, may indicate binary incompatibility",
	"ImportError: libGL.so.1: cannot open shared object file",
	"ImportError: libXrender.so.1: cannot open shared object file",
}

// swebenchTransientNetworkFailurePatterns 瞬态网络失败模式表
// （Python DEFAULT_TRANSIENT_NETWORK_FAILURE_PATTERNS 逐字对齐）。
var swebenchTransientNetworkFailurePatterns = []string{
	"requests.exceptions.ConnectionError",
	"RemoteDisconnected",
	"Connection aborted",
	"Connection reset by peer",
	"Temporary failure in name resolution",
	"Name or service not known",
	"requests.exceptions.ConnectTimeout",
	"requests.exceptions.ReadTimeout",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// swebenchRun Python subprocess.run 注入点（swebench_runtime 全部命令执行共用；
// 实现与 terminal_bench 共用 subprocessRunWithCwd）。
var swebenchRun = func(command []string, check bool, timeoutSec int, cwd string, inputText string) (tbRunResult, error) {
	var input []byte
	if inputText != "" {
		input = []byte(inputText)
	}
	var timeout float64
	if timeoutSec > 0 {
		timeout = float64(timeoutSec)
	}
	result, err := subprocessRunWithCwd(command[0], command[1:], timeout, input, cwd)
	if err != nil {
		return result, err
	}
	if check && result.ExitCode != 0 {
		return result, fmt.Errorf(
			"command failed: %s\nstdout:\n%s\nstderr:\n%s",
			strings.Join(command, " "), result.Stdout, result.Stderr)
	}
	return result, nil
}

// swebenchOsName os.name 等价判定（Linux/Darwin→""、Windows→"windows"；
// Python os.name == "nt" 语义，测试替换）。
var swebenchOsName = runtime.GOOS

// swebenchDependencyCacheRootEnv 依赖缓存根环境变量名（Python
// SWEBENCH_DEPENDENCY_CACHE_ROOT）。
const swebenchDependencyCacheRootEnv = "SWEBENCH_DEPENDENCY_CACHE_ROOT"

// swebenchDownloadProxyEnv 依赖清单下载代理环境变量名（Python SWEBENCH_DOWNLOAD_PROXY）。
const swebenchDownloadProxyEnv = "SWEBENCH_DOWNLOAD_PROXY"

// swebenchOfficialSupportFS 嵌入的官方验证支持资产
// （Python _swebench_official_support/sitecustomize.py 原样保留）。
//
//go:embed resource/swebench_official_support/sitecustomize.py
var swebenchOfficialSupportFS embed.FS

// ──────────────────────────── 导出函数 ────────────────────────────

// SwebenchImageName 官方实例镜像名（Python swebench_image_name L50-58：
// SWEBENCH_ARCH=arm64 选 arm64 否则 x86_64；instance_id 小写且 __→_1776_）。
func SwebenchImageName(instanceID string, namespace, tag string) string {
	arch := "x86_64"
	if strings.EqualFold(os.Getenv("SWEBENCH_ARCH"), "arm64") {
		arch = "arm64"
	}
	remoteInstanceID := strings.ReplaceAll(strings.ToLower(instanceID), "__", "_1776_")
	return fmt.Sprintf("%s/sweb.eval.%s.%s:%s", namespace, arch, remoteInstanceID, tag)
}

// PrepareSwebenchWorkspace 从官方实例镜像拷出原始 /testbed 检出
// （Python prepare_swebench_workspace L61-107 步骤序逐条对齐：
// 配置解析 → 镜像核验/准备 → 清理重建工作区 → create(bind mount)+start →
// detach symlinks → docker cp → restore symlinks → 配置 git → 缓存依赖清单 →
// finally rm -f 容器）。
func PrepareSwebenchWorkspace(params PrepareSwebenchParams) (string, error) {
	config, err := swebenchConfig(params.Case)
	if err != nil {
		return "", err
	}
	instanceID := strings.TrimSpace(stringOrTP(config["instance_id"], stringOrTP(params.Case["case_id"], "")))
	if instanceID == "" {
		return "", fmt.Errorf("swebench.instance_id is required")
	}
	image, err := swebenchConfiguredImage(config, instanceID)
	if err != nil {
		return "", err
	}
	if err := swebenchEnsureConfiguredImage(config, instanceID, image, params.TimeoutSec); err != nil {
		return "", err
	}

	if _, statErr := os.Stat(params.WorkspaceDir); statErr == nil {
		if rmErr := os.RemoveAll(params.WorkspaceDir); rmErr != nil { // Python shutil.rmtree
			return "", rmErr
		}
	}
	if mkErr := os.MkdirAll(params.WorkspaceDir, 0o755); mkErr != nil {
		return "", mkErr
	}
	container := fmt.Sprintf("ach-sweb-prepare-%s-%s", swebenchSafeID(instanceID), uuidHex8())
	defer func() {
		_, _ = swebenchRun([]string{"docker", "rm", "-f", container}, false, 60, "", "")
	}()
	if _, err := swebenchRun([]string{
		"docker", "create", "--name", container,
		"--mount", fmt.Sprintf("type=bind,source=%s,target=/ach-workspace", params.WorkspaceDir),
		image, "sleep", "infinity",
	}, true, 120, "", ""); err != nil {
		return "", err
	}
	if _, err := swebenchRun([]string{"docker", "start", container}, true, 120, "", ""); err != nil {
		return "", err
	}
	symlinks, err := swebenchDetachContainerSymlinks(container)
	if err != nil {
		return "", err
	}
	if _, err := swebenchRun([]string{
		"docker", "cp", fmt.Sprintf("%s:/testbed/.", container), params.WorkspaceDir,
	}, true, params.TimeoutSec, "", ""); err != nil {
		return "", err
	}
	if err := swebenchRestoreWorkspaceSymlinks(container, symlinks); err != nil {
		return "", err
	}
	if err := swebenchConfigureWorkspaceGit(params.WorkspaceDir); err != nil {
		return "", err
	}
	if _, err := swebenchCacheOfficialDependencyFiles(config, params.WorkspaceDir); err != nil {
		return "", err
	}
	return image, nil
}

// CollectModelPatch 收集工作区模型 patch（Python collect_model_patch L198-206：
// git diff --binary HEAD；失败抛 SWEbenchInfrastructureError）。
func CollectModelPatch(workspaceDir string) (string, error) {
	result, err := swebenchRun([]string{
		"git", "-C", workspaceDir, "diff", "--binary", "HEAD",
	}, false, 120, "", "")
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", &SWEbenchInfrastructureError{NewEvaluationInfrastructureError(
			fmt.Sprintf("failed to collect SWE-bench patch: %s", result.Stderr))}
	}
	return result.Stdout, nil
}

// RunOfficialSwebenchEvaluation 用官方 SWE-bench harness 评估一个预测
// （Python run_official_swebench_evaluation L209-359 步骤序逐条对齐）。
func RunOfficialSwebenchEvaluation(params RunOfficialParams) (map[string]any, error) {
	config, err := swebenchConfig(params.Case)
	if err != nil {
		return nil, err
	}
	instanceID := strings.TrimSpace(stringOrTP(config["instance_id"], stringOrTP(params.Case["case_id"], "")))
	datasetPath, err := filepath.Abs(pathutil.ExpandHome(stringOrTP(config["official_dataset_path"], "")))
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(datasetPath); statErr != nil || info.IsDir() {
		return nil, fmt.Errorf("SWE-bench official dataset not found: %s", datasetPath)
	}

	verifierDir := filepath.Join(absPath(params.OutputDir), "verifier", "swebench")
	if mkErr := os.MkdirAll(verifierDir, 0o755); mkErr != nil {
		return nil, mkErr
	}
	runID := fmt.Sprintf("ach_%s_%s", swebenchSafeID(instanceID), uuidHex8())
	modelName := "openjiuwen_single_harness"
	predictionsPath := filepath.Join(verifierDir, "predictions.jsonl")
	predictionRecord, _ := json.Marshal(map[string]any{
		"instance_id":        instanceID,
		"model_patch":        params.ModelPatch,
		"model_name_or_path": modelName,
	})
	if wErr := os.WriteFile(predictionsPath, append(predictionRecord, '\n'), 0o644); wErr != nil {
		return nil, wErr
	}

	reportPath := filepath.Join(verifierDir, fmt.Sprintf("%s.%s.json", modelName, runID))
	stdoutPath := filepath.Join(verifierDir, "stdout.log")
	stderrPath := filepath.Join(verifierDir, "stderr.log")
	instanceArtifactDir := filepath.Join(verifierDir, "logs", "run_evaluation", runID, modelName, instanceID)
	instanceReportPath := filepath.Join(instanceArtifactDir, "report.json")
	testOutputPath := filepath.Join(instanceArtifactDir, "test_output.txt")
	if strings.TrimSpace(params.ModelPatch) == "" {
		// Python L246-289 空 patch 分支：官方 harness 跳过执行，写空报告收卷。
		report := map[string]any{
			"total_instances":       1,
			"submitted_instances":   1,
			"completed_instances":   0,
			"resolved_instances":    0,
			"unresolved_instances":  0,
			"error_instances":       0,
			"empty_patch_instances": 1,
			"submitted_ids":         []string{instanceID},
			"completed_ids":         []string{},
			"resolved_ids":          []string{},
			"unresolved_ids":        []string{},
			"error_ids":             []string{},
			"empty_patch_ids":       []string{instanceID},
			"incomplete_ids":        []string{},
		}
		reportRaw, _ := json.MarshalIndent(report, "", "  ")
		if wErr := os.WriteFile(reportPath, append(reportRaw, '\n'), 0o644); wErr != nil {
			return nil, wErr
		}
		if wErr := os.WriteFile(stdoutPath,
			[]byte("Skipped official execution: model patch is empty.\n"), 0o644); wErr != nil {
			return nil, wErr
		}
		if wErr := os.WriteFile(stderrPath, []byte(""), 0o644); wErr != nil {
			return nil, wErr
		}
		return map[string]any{
			"passed":               false,
			"score":                0.0,
			"reason":               "official SWE-bench evaluation received an empty model patch",
			"report_path":          reportPath,
			"stdout_path":          stdoutPath,
			"stderr_path":          stderrPath,
			"predictions_path":     predictionsPath,
			"run_id":               runID,
			"official_attempts":    0,
			"exit_code":            0,
			"report":               report,
			"instance_report_path": instanceReportPath,
			"test_output_path":     testOutputPath,
			"test_output_excerpt":  "",
			"instance_report":      map[string]any{},
			"empty_patch":          true,
		}, nil
	}

	timeoutSec := 1800
	if raw, ok := config["timeout_sec"]; ok {
		if f, ok := toFloatAny(raw); ok {
			timeoutSec = int(f)
		}
	}
	command, err := swebenchOfficialCommand(swebenchOfficialCommandParams{
		config:              config,
		datasetPath:         datasetPath,
		predictionsPath:     predictionsPath,
		instanceID:          instanceID,
		runID:               runID,
		timeoutSec:          timeoutSec,
		dependencyCacheRoot: swebenchAvailableDependencyCache(config),
	})
	if err != nil {
		return nil, err
	}
	completed, officialAttempts, err := swebenchRunOfficialCommandWithRetries(officialRunContext{
		command:        command,
		timeoutSec:     timeoutSec,
		verifierDir:    verifierDir,
		runID:          runID,
		reportPath:     reportPath,
		testOutputPath: testOutputPath,
		config:         config,
	})
	if err != nil {
		return nil, err
	}
	if wErr := os.WriteFile(stdoutPath, []byte(completed.Stdout), 0o644); wErr != nil {
		return nil, wErr
	}
	if wErr := os.WriteFile(stderrPath, []byte(completed.Stderr), 0o644); wErr != nil {
		return nil, wErr
	}

	report := swebenchReadJSON(reportPath)
	instanceReport := swebenchReadJSON(instanceReportPath)
	testOutput := swebenchReadText(testOutputPath)
	infrastructureReason, err := swebenchInfrastructureFailureReason(swebenchFailureReasonParams{
		config: config, instanceID: instanceID, completed: completed,
		report: report, instanceReport: instanceReport, testOutput: testOutput,
	})
	if err != nil {
		return nil, err
	}
	if infrastructureReason != "" {
		failureDetail := swebenchBoundedInfrastructureFailureDetail(completed, testOutput)
		detailSuffix := ""
		if failureDetail != "" {
			detailSuffix = "; detail=" + failureDetail
		}
		return nil, &SWEbenchInfrastructureError{NewEvaluationInfrastructureError(fmt.Sprintf(
			"SWE-bench verifier infrastructure failed for %s: %s; test_output=%s%s",
			instanceID, infrastructureReason, testOutputPath, detailSuffix))}
	}
	resolved := swebenchIDIn(report["resolved_ids"], instanceID)
	emptyPatch := swebenchIDIn(report["empty_patch_ids"], instanceID)
	reason := ""
	if !resolved {
		if emptyPatch {
			reason = "official SWE-bench evaluation received an empty model patch"
		} else {
			reason = "official SWE-bench evaluation did not resolve the instance"
		}
	}
	score := 0.0
	if resolved {
		score = 1.0
	}
	return map[string]any{
		"passed":               resolved,
		"score":                score,
		"reason":               reason,
		"report_path":          reportPath,
		"stdout_path":          stdoutPath,
		"stderr_path":          stderrPath,
		"predictions_path":     predictionsPath,
		"run_id":               runID,
		"official_attempts":    officialAttempts,
		"exit_code":            completed.ExitCode,
		"report":               report,
		"instance_report_path": instanceReportPath,
		"test_output_path":     testOutputPath,
		"test_output_excerpt":  swebenchBoundedTestOutputExcerpt(testOutput),
		"instance_report":      instanceReport,
		"empty_patch":          emptyPatch,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// swebenchDetachContainerSymlinks 在 Windows docker cp 前摘除镜像内符号链接并返回
// 清单（Python _detach_container_symlinks L110-150：容器内 python 脚本枚举+删除，
// stdout JSON → 校验相对安全路径）。
func swebenchDetachContainerSymlinks(container string) ([][2]string, error) {
	script := `import json
import os

root = "/testbed"
links = []
for base, dirs, files in os.walk(root, followlinks=False):
    for name in [*dirs, *files]:
        path = os.path.join(base, name)
        if os.path.islink(path):
            links.append([os.path.relpath(path, root), os.readlink(path)])
for relative_path, _ in links:
    os.unlink(os.path.join(root, relative_path))
print(json.dumps(links))
`
	completed, err := swebenchRun([]string{
		"docker", "exec", container,
		"/opt/miniconda3/envs/testbed/bin/python", "-c", script,
	}, true, 120, "", "")
	if err != nil {
		return nil, err
	}
	var raw any
	if jsonErr := json.Unmarshal([]byte(completed.Stdout), &raw); jsonErr != nil {
		return nil, fmt.Errorf("invalid SWE-bench workspace symlink manifest")
	}
	rawList, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid SWE-bench workspace symlink manifest")
	}
	links := make([][2]string, 0, len(rawList))
	for _, item := range rawList {
		pair, ok := item.([]any)
		if !ok || len(pair) != 2 {
			return nil, fmt.Errorf("invalid SWE-bench workspace symlink entry")
		}
		relativePath := fmt.Sprintf("%v", pair[0])
		target := fmt.Sprintf("%v", pair[1])
		if filepath.IsAbs(relativePath) || strings.HasPrefix(relativePath, "..") ||
			strings.Contains(relativePath, "/../") {
			return nil, fmt.Errorf("unsafe SWE-bench workspace symlink path: %s", relativePath)
		}
		links = append(links, [2]string{relativePath, target})
	}
	return links, nil
}

// swebenchRestoreWorkspaceSymlinks 经 Linux bind mount 重建符号链接
// （Python _restore_workspace_symlinks L153-175：Docker Desktop 的 Windows 侧
// docker cp 缺符号链接权限，由容器在挂载点内补建）。
func swebenchRestoreWorkspaceSymlinks(container string, symlinks [][2]string) error {
	for _, link := range symlinks {
		if _, err := swebenchRun([]string{
			"docker", "exec", container, "ln", "-s", "--",
			link[1], "/ach-workspace/" + link[0],
		}, true, 60, "", ""); err != nil {
			return err
		}
	}
	return nil
}

// swebenchConfigureWorkspaceGit 稳定 Windows bind mount 下的 git 检出
// （Python _configure_workspace_git L178-195：四个 core.* 配置逐条设置）。
func swebenchConfigureWorkspaceGit(workspaceDir string) error {
	for _, kv := range [][2]string{
		{"core.autocrlf", "false"},
		{"core.eol", "lf"},
		{"core.safecrlf", "false"},
		{"core.filemode", "false"},
	} {
		if _, err := swebenchRun([]string{
			"git", "-C", workspaceDir, "config", kv[0], kv[1],
		}, true, 60, "", ""); err != nil {
			return err
		}
	}
	return nil
}

// officialRetryParams _run_official_command_with_retries 的重试参数解析结果。
type officialRetryParams struct {
	maxAttempts   int
	retryDelaySec float64
}

// swebenchRunOfficialCommandWithRetries 只对官方 harness 的瞬态网络装机失败重试
// （Python _run_official_command_with_retries L362-411 步骤序逐条对齐）。
func swebenchRunOfficialCommandWithRetries(ctx officialRunContext) (tbRunResult, int, error) {
	params, err := swebenchParseRetryParams(ctx.config)
	if err != nil {
		return tbRunResult{}, 0, err
	}
	var completed tbRunResult
	for attempt := 1; attempt <= params.maxAttempts; attempt++ {
		if attempt > 1 {
			_ = os.Remove(ctx.reportPath) // Python unlink(missing_ok=True)
			_ = os.RemoveAll(filepath.Join(ctx.verifierDir, "logs", "run_evaluation", ctx.runID))
			if params.retryDelaySec > 0 {
				time.Sleep(time.Duration(params.retryDelaySec*float64(attempt-1)) * time.Second)
			}
		}
		completed, err = swebenchRun(ctx.command, false, ctx.timeoutSec+600, ctx.verifierDir, "")
		if err != nil {
			return completed, attempt, err
		}
		if completed.ExitCode == 0 {
			return completed, attempt, nil
		}
		diagnostic := strings.Join([]string{
			completed.Stdout,
			completed.Stderr,
			swebenchReadText(ctx.testOutputPath),
		}, "\n")
		if !swebenchIsTransientNetworkFailure(diagnostic) {
			return completed, attempt, nil
		}
	}
	return completed, params.maxAttempts, nil
}

// swebenchParseRetryParams 重试参数解析与校验（Python L373-380：attempts 1-5、
// delay 0-60）。
func swebenchParseRetryParams(config map[string]any) (officialRetryParams, error) {
	maxAttempts := 3
	if raw, ok := config["infrastructure_retry_attempts"]; ok {
		if f, ok := toFloatAny(raw); ok {
			maxAttempts = int(f)
		}
	}
	if maxAttempts < 1 || maxAttempts > 5 {
		return officialRetryParams{}, fmt.Errorf(
			"swebench.infrastructure_retry_attempts must be between 1 and 5")
	}
	retryDelaySec := 2.0
	if raw, ok := config["infrastructure_retry_delay_sec"]; ok {
		if f, ok := toFloatAny(raw); ok {
			retryDelaySec = f
		}
	}
	if retryDelaySec < 0 || retryDelaySec > 60 {
		return officialRetryParams{}, fmt.Errorf(
			"swebench.infrastructure_retry_delay_sec must be between 0 and 60")
	}
	return officialRetryParams{maxAttempts: maxAttempts, retryDelaySec: retryDelaySec}, nil
}

// swebenchIsTransientNetworkFailure 瞬态网络失败判定（Python L414-416：casefold
// 子串包含；Go strings.ToLower 等价 ASCII/Unicode 降形）。
func swebenchIsTransientNetworkFailure(value string) bool {
	folded := strings.ToLower(value)
	for _, pattern := range swebenchTransientNetworkFailurePatterns {
		if strings.Contains(folded, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// swebenchBoundedTestOutputExcerpt 有界测试输出摘录（Python L419-426：≤8000 原样；
// 超长 → 头 1500 + "... N characters omitted ..." + 尾 6500）。
func swebenchBoundedTestOutputExcerpt(testOutput string) string {
	text := strings.TrimSpace(testOutput)
	if len(text) <= swebenchTestOutputExcerptChars {
		return text
	}
	tailChars := swebenchTestOutputExcerptChars - swebenchTestOutputExcerptHeadChars
	omitted := len(text) - swebenchTestOutputExcerptChars
	return text[:swebenchTestOutputExcerptHeadChars] +
		fmt.Sprintf("\n... %d characters omitted ...\n", omitted) +
		text[len(text)-tailChars:]
}

// swebenchBoundedInfrastructureFailureDetail 有界基建失败细节
// （Python L429-440：非空段 strip 后拼接 → 摘录 → 取尾 2000）。
func swebenchBoundedInfrastructureFailureDetail(completed tbRunResult, testOutput string) string {
	parts := make([]string, 0, 3)
	for _, text := range []string{completed.Stdout, completed.Stderr, testOutput} {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	combined := strings.Join(parts, "\n")
	if combined == "" {
		return ""
	}
	excerpt := swebenchBoundedTestOutputExcerpt(combined)
	if len(excerpt) > 2000 {
		return excerpt[len(excerpt)-2000:]
	}
	return excerpt
}

// swebenchDependencyCacheDir 依赖缓存目录（Python _dependency_cache_dir L443-449：
// 环境变量覆写须绝对路径；repo/commit 安全段两级的 cache 根）。
func swebenchDependencyCacheDir(config map[string]any) (string, error) {
	repo := strings.TrimSpace(stringOrTP(config["repo"], "unknown_repo"))
	commit := strings.TrimSpace(stringOrTP(config["environment_setup_commit"],
		stringOrTP(config["base_commit"], "unknown_commit")))
	root := strings.TrimSpace(os.Getenv(swebenchDependencyCacheRootEnv))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".cache/openjiuwen/rsi/swebench_dependency_cache")
	}
	root = pathutil.ExpandHome(root)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("SWEBENCH_DEPENDENCY_CACHE_ROOT must be an absolute path")
	}
	return filepath.Join(root, swebenchSafeID(repo), swebenchSafeID(commit)), nil
}

// swebenchDownloadDependencyManifest 只对瞬态失败的清单下载重试
// （Python _download_dependency_manifest L452-476：404→None；连接/超时类重试
// 3 次全失败才抛；HTTPError 仅 429/5xx 重试；指数退避 2**attempt）。
func swebenchDownloadDependencyManifest(rawURL string) ([]byte, error) {
	transport := &http.Transport{}
	if proxy := strings.TrimSpace(os.Getenv(swebenchDownloadProxyEnv)); proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: transport}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := client.Get(rawURL)
		if err != nil {
			// Python requests.ConnectionError/Timeout → 重试，3 次失败重抛。
			lastErr = err
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return nil, nil // Python 404 → None
			}
			body, readErr := io.ReadAll(resp.Body)
			if resp.StatusCode >= 400 {
				isRetryable := resp.StatusCode == http.StatusTooManyRequests ||
					resp.StatusCode == http.StatusInternalServerError ||
					resp.StatusCode == http.StatusBadGateway ||
					resp.StatusCode == http.StatusServiceUnavailable ||
					resp.StatusCode == http.StatusGatewayTimeout
				if !isRetryable || attempt == 2 {
					return nil, fmt.Errorf("manifest download failed: %s", resp.Status)
				}
				lastErr = fmt.Errorf("manifest download failed: %s", resp.Status)
			} else if readErr != nil {
				lastErr = readErr
			} else {
				return body, nil
			}
		}
		time.Sleep(time.Duration(1<<attempt) * time.Second) // Python time.sleep(2**attempt)
	}
	return nil, lastErr
}

// swebenchCacheOfficialDependencyFiles 在 solver 动手前缓存原始依赖清单
// （Python _cache_official_dependency_files L479-549 步骤序逐条对齐）。
func swebenchCacheOfficialDependencyFiles(config map[string]any, workspaceDir string) (string, error) {
	target, err := swebenchDependencyCacheDir(config)
	if err != nil {
		return "", err
	}
	marker := filepath.Join(target, "cache.json")
	if swebenchReadJSON(marker)["version"] == 2 {
		return target, nil
	}
	repo := strings.TrimSpace(stringOrTP(config["repo"], ""))
	commit := strings.TrimSpace(stringOrTP(config["environment_setup_commit"],
		stringOrTP(config["base_commit"], "")))
	baseCommit := strings.TrimSpace(stringOrTP(config["base_commit"], ""))
	copied := []string{}
	walkErr := filepath.WalkDir(workspaceDir, func(root string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" {
				return filepath.SkipDir // Python dirs[:] = [d for d in dirs if d != ".git"]
			}
			return nil
		}
		// Python os.walk 的 root 恒为目录；Go WalkDir 对文件回调的 path 是文件本身，
		// 相对目录取 filepath.Dir(root)。
		relative, relErr := filepath.Rel(workspaceDir, filepath.Dir(root))
		if relErr != nil {
			return relErr
		}
		pathParts := []string{}
		if relative != "." {
			for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
				pathParts = append(pathParts, strings.ToLower(part))
			}
		}
		folded := strings.ToLower(name)
		isRequirement := (strings.HasSuffix(folded, ".txt") || strings.HasSuffix(folded, ".in")) &&
			(strings.Contains(folded, "requirement") || containsString(pathParts, "requirement"))
		isEnvFile := folded == "environment.yml" || folded == "environment.yaml" ||
			folded == "conda.yml" || folded == "conda.yaml"
		if !isRequirement && !isEnvFile {
			return nil
		}
		source := root // WalkDir 文件回调：root 即文件路径
		relativePosix := filepath.ToSlash(filepath.Join(relative, name))
		destination := filepath.Join(target, filepath.FromSlash(relativePosix))
		var content []byte
		if commit != "" && commit != baseCommit {
			// 镜像检出可能比官方 setup 版本新（Python L517-523）。
			downloaded, dlErr := swebenchDownloadDependencyManifest(fmt.Sprintf(
				"https://raw.githubusercontent.com/%s/%s/%s", repo, commit, relativePosix))
			if dlErr != nil {
				return dlErr
			}
			if downloaded == nil {
				return nil // Python continue
			}
			content = downloaded
		}
		if mkErr := os.MkdirAll(filepath.Dir(destination), 0o755); mkErr != nil {
			return mkErr
		}
		if content == nil {
			if copyErr := copyFilePreservingMode(source, destination); copyErr != nil {
				return copyErr // Python shutil.copy2
			}
		} else if wErr := os.WriteFile(destination, content, 0o644); wErr != nil {
			return wErr
		}
		copied = append(copied, relativePosix)
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(copied) == 0 {
		return "", nil
	}
	files := append([]string{}, copied...)
	sort.Strings(files)
	markerRecord, _ := json.MarshalIndent(map[string]any{
		"version":                  2,
		"repo":                     stringOrTP(config["repo"], ""),
		"environment_setup_commit": commit,
		"files":                    files,
	}, "", "  ")
	if mkErr := os.MkdirAll(filepath.Dir(marker), 0o755); mkErr != nil {
		return "", mkErr
	}
	if wErr := os.WriteFile(marker, append(markerRecord, '\n'), 0o644); wErr != nil {
		return "", wErr
	}
	return target, nil
}

// swebenchAvailableDependencyCache 已有缓存的依赖清单目录（Python L552-554）。
func swebenchAvailableDependencyCache(config map[string]any) string {
	target, err := swebenchDependencyCacheDir(config)
	if err != nil {
		return ""
	}
	if info, statErr := os.Stat(filepath.Join(target, "cache.json")); statErr == nil && !info.IsDir() {
		return target
	}
	return ""
}

// swebenchOfficialCommandParams _official_command 参数包。
type swebenchOfficialCommandParams struct {
	config              map[string]any
	datasetPath         string
	predictionsPath     string
	instanceID          string
	runID               string
	timeoutSec          int
	dependencyCacheRoot string
}

// swebenchOfficialCommand 组装官方验证命令（Python _official_command L557-610：
// 基础参数 → namespace/instance_image_tag → 非 Windows 加 python 前缀；
// Windows 走 wsl -d distro -- 前缀且路径翻译）。
func swebenchOfficialCommand(params swebenchOfficialCommandParams) ([]string, error) {
	config := params.config
	baseArgs := []string{
		"-m", "swebench.harness.run_evaluation",
		"-d", params.datasetPath,
		"-s", stringOrTP(config["split"], "dev"),
		"-i", params.instanceID,
		"-p", params.predictionsPath,
		"--max_workers", "1",
		"-t", fmt.Sprintf("%d", params.timeoutSec),
		"--cache_level", stringOrTP(config["cache_level"], "instance"),
		"--clean", "false",
		"-id", params.runID,
	}
	namespace, instanceImageTag, err := swebenchImageCoordinates(config)
	if err != nil {
		return nil, err
	}
	baseArgs = append(baseArgs, "--namespace", namespace, "--instance_image_tag", instanceImageTag)
	if swebenchOsName != "windows" {
		prefix := swebenchOfficialPythonPrefix(swebenchPythonPrefixParams{
			pythonPath:          stringOrTP(config["python_path"], "python"),
			supportDir:          swebenchOfficialSupportDir(),
			dependencyCacheRoot: params.dependencyCacheRoot,
		})
		return append(prefix, baseArgs...), nil
	}
	distro := stringOrTP(config["wsl_distro"],
		stringOrTP(os.Getenv("SWEBENCH_WSL_DISTRO"), swebenchDefaultWslDistro))
	pythonPath := stringOrTP(config["python_path"],
		stringOrTP(os.Getenv("SWEBENCH_WSL_PYTHON"), swebenchDefaultWslPython))
	translated := append([]string{}, baseArgs...)
	for _, hostPath := range []string{params.datasetPath, params.predictionsPath} {
		linuxPath, err := swebenchWslPath(hostPath, distro)
		if err != nil {
			return nil, err
		}
		for i, arg := range translated {
			if arg == hostPath {
				translated[i] = linuxPath
				break
			}
		}
	}
	supportDir, err := swebenchWslPath(swebenchOfficialSupportDir(), distro)
	if err != nil {
		return nil, err
	}
	var cacheRoot string
	if params.dependencyCacheRoot != "" {
		cacheRoot, err = swebenchWslPath(params.dependencyCacheRoot, distro)
		if err != nil {
			return nil, err
		}
	}
	prefix := swebenchOfficialPythonPrefix(swebenchPythonPrefixParams{
		pythonPath: pythonPath, supportDir: supportDir, dependencyCacheRoot: cacheRoot,
	})
	return append([]string{"wsl", "-d", distro, "--"}, append(prefix, translated...)...), nil
}

// swebenchPythonPrefixParams _official_python_prefix 参数包。
type swebenchPythonPrefixParams struct {
	pythonPath          string
	supportDir          string
	dependencyCacheRoot string
}

// swebenchOfficialPythonPrefix python 前缀（Python L613-622：env PYTHONPATH=…
// [ACH_SWEBENCH_DEPENDENCY_CACHE=…] python）。
func swebenchOfficialPythonPrefix(params swebenchPythonPrefixParams) []string {
	prefix := []string{"env", fmt.Sprintf("PYTHONPATH=%s", params.supportDir)}
	if params.dependencyCacheRoot != "" {
		prefix = append(prefix, fmt.Sprintf("ACH_SWEBENCH_DEPENDENCY_CACHE=%s", params.dependencyCacheRoot))
	}
	return append(prefix, params.pythonPath)
}

// swebenchWslPath Windows 路径 → WSL 内路径（Python _wsl_path L625-634：有盘符
// → /mnt/<drive>/…；否则 wslpath -a 解析）。
func swebenchWslPath(path string, distro string) (string, error) {
	// Python Path.drive 在 resolve 前取原始路径（Linux 上 filepath.Abs 会吞掉
	// `D:\` 形路径的盘符语义，故盘符解析先于 Abs）。
	drive := ""
	if len(path) >= 2 && path[1] == ':' && isASCIILetter(rune(path[0])) {
		drive = strings.ToLower(path[:1])
	}
	resolved := path
	if abs, err := filepath.Abs(pathutil.ExpandHome(path)); err == nil {
		resolved = abs
	}
	if drive == "" {
		if volume := filepath.VolumeName(resolved); volume != "" {
			drive = strings.ToLower(strings.TrimSuffix(volume, ":"))
		}
	}
	if drive != "" {
		var relative string
		if len(path) >= 2 && path[1] == ':' && isASCIILetter(rune(path[0])) {
			relative = path[2:]
		} else {
			relative = resolved[len(filepath.VolumeName(resolved)):]
		}
		relative = strings.ReplaceAll(relative, `\`, `/`)
		relative = strings.TrimPrefix(relative, "/")
		return fmt.Sprintf("/mnt/%s/%s", drive, relative), nil
	}
	result, err := swebenchRun([]string{
		"wsl", "-d", distro, "--", "wslpath", "-a", resolved,
	}, true, 30, "", "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// swebenchConfig 解析 case.swebench（Python _config L637-641）。
func swebenchConfig(caseMapping map[string]any) (map[string]any, error) {
	value, ok := caseMapping["swebench"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("case.swebench must be a mapping")
	}
	return value, nil
}

// swebenchImageCoordinates 镜像 namespace/tag（Python _image_coordinates L644-656：
// setup_commands 非空时 tag=ach-<sha256[:12]>，否则 latest）。
func swebenchImageCoordinates(config map[string]any) (string, string, error) {
	namespace := strings.TrimSpace(stringOrTP(config["namespace"], "swebench"))
	tag := strings.TrimSpace(stringOrTP(config["instance_image_tag"], ""))
	setupCommands, err := swebenchSetupCommands(config)
	if err != nil {
		return "", "", err
	}
	if tag == "" {
		if len(setupCommands) > 0 {
			serialized, _ := json.Marshal(setupCommands)
			digest := sha256.Sum256(serialized)
			tag = fmt.Sprintf("ach-%s", hex.EncodeToString(digest[:])[:12])
		} else {
			tag = "latest"
		}
	}
	if namespace == "" || tag == "" {
		return "", "", fmt.Errorf("swebench namespace and instance_image_tag must be non-empty")
	}
	return namespace, tag, nil
}

// swebenchConfiguredImage 校验并返回镜像名（Python _configured_image L659-668：
// docker_image 与 namespace/tag 推导不一致时报错，保证 solver 与官方验证同镜像）。
func swebenchConfiguredImage(config map[string]any, instanceID string) (string, error) {
	namespace, tag, err := swebenchImageCoordinates(config)
	if err != nil {
		return "", err
	}
	expected := SwebenchImageName(instanceID, namespace, tag)
	configured := strings.TrimSpace(stringOrTP(config["docker_image"], ""))
	if configured != "" && configured != expected {
		return "", fmt.Errorf(
			"swebench.docker_image must match namespace/instance_image_tag so the solver "+
				"and official verifier use one image: expected %s, got %s", expected, configured)
	}
	if configured != "" {
		return configured, nil
	}
	return expected, nil
}

// swebenchSetupCommands verifier 镜像构建命令清单（Python _setup_commands L671-677）。
func swebenchSetupCommands(config map[string]any) ([]string, error) {
	value, exists := config["verifier_setup_commands"]
	if !exists || value == nil {
		return []string{}, nil
	}
	rawList, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("swebench.verifier_setup_commands must be a list of shell commands")
	}
	commands := []string{}
	for _, item := range rawList {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("swebench.verifier_setup_commands must be a list of shell commands")
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			commands = append(commands, trimmed)
		}
	}
	return commands, nil
}

// swebenchEnsureConfiguredImage 保证镜像可用（Python _ensure_configured_image
// L680-731 步骤序：inspect → 命中即返回 → 无 setup 直接 pull → 有 setup 基于
// base 镜像 docker build（build 失败仍以本地 inspect 为准））。
func swebenchEnsureConfiguredImage(config map[string]any, instanceID, image string, timeoutSec int) error {
	inspect, err := swebenchRun([]string{"docker", "image", "inspect", image}, false, 60, "", "")
	if err != nil {
		return err
	}
	if inspect.ExitCode == 0 {
		return nil
	}
	setupCommands, err := swebenchSetupCommands(config)
	if err != nil {
		return err
	}
	if len(setupCommands) == 0 {
		_, err = swebenchRun([]string{"docker", "pull", image}, true, timeoutSec, "", "")
		return err
	}
	baseImage := strings.TrimSpace(stringOrTP(config["base_docker_image"], ""))
	if baseImage == "" {
		baseImage = SwebenchImageName(instanceID, "swebench", "latest")
	}
	baseInspect, err := swebenchRun([]string{"docker", "image", "inspect", baseImage}, false, 60, "", "")
	if err != nil {
		return err
	}
	if baseInspect.ExitCode != 0 {
		if _, err := swebenchRun([]string{"docker", "pull", baseImage}, true, timeoutSec, "", ""); err != nil {
			return err
		}
	}
	dockerfileLines := []string{fmt.Sprintf("FROM %s", baseImage)}
	for _, command := range setupCommands {
		dockerfileLines = append(dockerfileLines, fmt.Sprintf("RUN %s", command))
	}
	dockerfile := strings.Join(dockerfileLines, "\n") + "\n"
	build, err := swebenchRun([]string{
		"docker", "build", "--pull=false", "--provenance=false", "-t", image, "-",
	}, false, timeoutSec, "", dockerfile)
	if err != nil {
		return err
	}
	if build.ExitCode != 0 {
		// Docker Desktop 可能在 BuildKit 刷新 registry 元数据时返回非零——只信本地镜像
		// （Python L722-731 注释逐字对齐）。
		builtImage, err := swebenchRun([]string{"docker", "image", "inspect", image}, false, 60, "", "")
		if err != nil {
			return err
		}
		if builtImage.ExitCode != 0 {
			return fmt.Errorf("failed to build SWE-bench verifier image %s:\n%s", image, build.Stderr)
		}
	}
	return nil
}

// swebenchFailureReasonParams _infrastructure_failure_reason 参数包。
type swebenchFailureReasonParams struct {
	config         map[string]any
	instanceID     string
	completed      tbRunResult
	report         map[string]any
	instanceReport map[string]any
	testOutput     string
}

// swebenchInfrastructureFailureReason 基建失败归因（Python L734-767 步骤序逐条
// 对齐：非零退出 → 无聚合报告 → error/incomplete → empty patch 合法放行 →
// 无 per-instance 报告 → 配置模式表合并匹配）。
func swebenchInfrastructureFailureReason(params swebenchFailureReasonParams) (string, error) {
	if params.completed.ExitCode != 0 {
		return fmt.Sprintf("official harness exited with code %d", params.completed.ExitCode), nil
	}
	if len(params.report) == 0 {
		return "official aggregate report is missing", nil
	}
	if swebenchIDIn(params.report["error_ids"], params.instanceID) {
		return "official report classified the instance as an error", nil
	}
	if swebenchIDIn(params.report["incomplete_ids"], params.instanceID) {
		return "official report classified the instance as incomplete", nil
	}
	if swebenchIDIn(params.report["empty_patch_ids"], params.instanceID) {
		// 官方 harness 对空 patch 有意跳过执行——合法模型结果而非验证器缺基建。
		return "", nil
	}
	if len(params.instanceReport) == 0 {
		return "official per-instance report is missing", nil
	}
	rawPatterns := []string{}
	if value, exists := params.config["infrastructure_failure_patterns"]; exists && value != nil {
		rawList, ok := value.([]any)
		if !ok {
			return "", fmt.Errorf("swebench.infrastructure_failure_patterns must be a list of strings")
		}
		for _, item := range rawList {
			text, ok := item.(string)
			if !ok {
				return "", fmt.Errorf("swebench.infrastructure_failure_patterns must be a list of strings")
			}
			rawPatterns = append(rawPatterns, text)
		}
	}
	foldedOutput := strings.ToLower(params.testOutput)
	for _, pattern := range append(append([]string{}, swebenchInfrastructureFailurePatterns...), rawPatterns...) {
		normalized := strings.TrimSpace(pattern)
		if normalized != "" && strings.Contains(foldedOutput, strings.ToLower(normalized)) {
			return fmt.Sprintf("configured infrastructure failure matched: %s", normalized), nil
		}
	}
	return "", nil
}

// swebenchReadJSON 读 JSON 报告（Python _read_json L770-776：缺失/非 dict → {}）。
func swebenchReadJSON(path string) map[string]any {
	raw, err := os.ReadFile(swebenchNativePath(path))
	if err != nil {
		return map[string]any{}
	}
	var value any
	if jsonErr := json.Unmarshal(raw, &value); jsonErr != nil {
		return map[string]any{}
	}
	if dict, ok := value.(map[string]any); ok {
		return dict
	}
	return map[string]any{}
}

// swebenchReadText 读文本（Python _read_text L779-784：缺失 → ""）。
func swebenchReadText(path string) string {
	raw, err := os.ReadFile(swebenchNativePath(path))
	if err != nil {
		return ""
	}
	return string(raw)
}

// swebenchNativePath 扩展 Windows 路径使深层报告可读（Python _native_path L787-795；
// 非 Windows 原样 resolve）。
func swebenchNativePath(path string) string {
	abs, err := filepath.Abs(pathutil.ExpandHome(path))
	if err != nil {
		abs = path
	}
	if swebenchOsName != "windows" {
		return abs
	}
	if strings.HasPrefix(abs, `\\?\`) || strings.HasPrefix(abs, `\\.\`) {
		return abs
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(abs, `\\`)
	}
	volume := filepath.VolumeName(abs)
	return `\\?\` + volume + `\` + strings.TrimPrefix(strings.TrimPrefix(abs, volume), `\`)
}

// swebenchSafeID 安全段（Python _safe_id L798-799：小写 → 非字母数字 '-' →
// strip('-') → 截 48）。
func swebenchSafeID(value string) string {
	return tbSafeSegment(value)
}

// swebenchOfficialSupportDir 解包嵌入的官方验证支持资产并返回目录
// （Python _OFFICIAL_SUPPORT_DIR = Path(__file__).with_name("_swebench_official_support")；
// Go embed 资产在首次使用时写出到进程临时目录，一次性写齐）。
func swebenchOfficialSupportDir() string {
	target := filepath.Join(os.TempDir(), "ach-swebench-official-support")
	marker := filepath.Join(target, "sitecustomize.py")
	if _, statErr := os.Stat(marker); statErr == nil {
		return target
	}
	content, err := swebenchOfficialSupportFS.ReadFile(
		"resource/swebench_official_support/sitecustomize.py")
	if err == nil {
		if mkErr := os.MkdirAll(target, 0o755); mkErr == nil {
			_ = os.WriteFile(marker, content, 0o644)
		}
	}
	return target
}

// swebenchIDIn 报告 ID 列表包含判定（Python instance_id in set(report.get(key) or [])）。
func swebenchIDIn(value any, instanceID string) bool {
	list, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		if fmt.Sprintf("%v", item) == instanceID {
			return true
		}
	}
	return false
}

// containsString 字符串切片包含。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// copyFilePreservingMode 复制文件并保留权限位（Python shutil.copy2 语义；
// mtime 不复制——对 mtime 不敏感，登记注释）。
func copyFilePreservingMode(source, destination string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, content, info.Mode().Perm())
}

// ──────────────────────────── 初始化 ────────────────────────────

// swebenchVerifierBridge judger.SwebenchVerifierView 的真身桥
// （Python script_based.py 直接引用 evaluator.swebench_runtime；Go 包树不可环，
// 经 judger 注册缝注入——spec §5 语言差异强制缝）。
type swebenchVerifierBridge struct{}

// CollectModelPatch 桥接 patch 收集。
func (swebenchVerifierBridge) CollectModelPatch(workspaceDir string) (string, error) {
	return CollectModelPatch(workspaceDir)
}

// RunOfficialEvaluation 桥接官方验证。
func (swebenchVerifierBridge) RunOfficialEvaluation(caseMapping map[string]any, modelPatch, outputDir string) (map[string]any, error) {
	return RunOfficialSwebenchEvaluation(RunOfficialParams{
		Case: caseMapping, ModelPatch: modelPatch, OutputDir: outputDir,
	})
}

// init 注册官方验证真身（plan Task 1 Step 4：RegisterSwebenchVerifier 真身注册）。
func init() {
	judger.RegisterSwebenchVerifier(swebenchVerifierBridge{})
	logger.Info(logComponentSweb).Msg("swebench official verifier registered")
}

// 编译期兜底：真身满足判卷侧验证视图。
var _ judger.SwebenchVerifierView = swebenchVerifierBridge{}
