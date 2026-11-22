// case_runner.go 监考编排——单用例考试全流程（建题目录→答题→轨迹→判分→归档→成绩单）。
// Python: openjiuwen/rsi/harness_rsi/evaluator/case_runner.py（1549 行全量对齐）。
//
// 已知偏差登记：
// ①Python backend.cleanup 为 async 且可抛错（body_error 为空时上抛）；Go
// CaseExecutionBackend.Cleanup 签名无 error（Task 7 既定契约）——清理失败
// 无法上抛/告警，回收条件=Cleanup 接口加 error 返回。
// ②datetime.isoformat() 以 time.RFC3339Nano 等价（时区偏移均保留）。
// ③Python ContextVar 任务级 home 绑定以 ctx value 承载（agent_teams
// SetTaskOpenuapclawHome）；Go ctx 不可变，finally 段改用外层 ctx。

package evaluator

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
)

const logComponentCR = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxRoleTrajectoryFileBytes 角色轨迹大文件阈值（Python _MAX_ROLE_TRAJECTORY_FILE_BYTES）
	maxRoleTrajectoryFileBytes = 2_000_000
	// roleTrajectoryTailBytes 大文件尾部截取字节数（Python _ROLE_TRAJECTORY_TAIL_BYTES）
	roleTrajectoryTailBytes = 64_000
	// maxRoleTraceMessages 单角色轨迹消息上限（Python _MAX_ROLE_TRACE_MESSAGES）
	maxRoleTraceMessages = 256
)

// canonicalDeliverableFiles 常见交付文件名（Python _CANONICAL_DELIVERABLE_FILES）。
var canonicalDeliverableFiles = []string{"index.html", "styles.css", "content_brief.md"}

// proofArtifactNameHints 证据类产物文件名提示（Python _PROOF_ARTIFACT_NAME_HINTS）。
var proofArtifactNameHints = []string{
	"check", "evidence", "report", "result", "summary",
	"validation", "validator", "verdict",
}

// proofArtifactSuffixes 证据类产物后缀集合（Python _PROOF_ARTIFACT_SUFFIXES）。
var proofArtifactSuffixes = map[string]bool{
	".json": true, ".md": true, ".txt": true, ".yaml": true, ".yml": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// CaseRunner 监考编排——把考场（backend）与裁判（judger）串成单用例闭环。
// Python: case_runner.py:54 CaseRunner。
type CaseRunner struct {
	// backend 考场执行后端
	backend CaseExecutionBackend
	// judger 判分器（可空；空时仅 backend JudgeResult 可用）
	judger judger.EvaluationJudger
}

// CaseRunnerParams Execute 入参包（Python 关键字参数）。
type CaseRunnerParams struct {
	// Case 考题映射
	Case map[string]any
	// OutputDir 用例输出目录（即 case_dir）
	OutputDir string
	// TeamSkillRefPath 团队技能引用路径（可空）
	TeamSkillRefPath string
	// HarnessRefs harness 引用映射（可空）
	HarnessRefs map[string]string
	// BaselineContext 基线判分上下文（可空）
	BaselineContext judger.BaselineContextView
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCaseRunner 构造监考编排器。
// Python: case_runner.py:57 __init__（backend 必填，缺省 ValueError）。
func NewCaseRunner(backend CaseExecutionBackend, judger judger.EvaluationJudger) (*CaseRunner, error) {
	if backend == nil {
		return nil, fmt.Errorf("CaseRunner requires an explicit backend; use build_backend() to construct one")
	}
	return &CaseRunner{backend: backend, judger: judger}, nil
}

// Execute 跑一个用例并持久化最终轨迹/结果件。
// Python: case_runner.py:67 execute（执行序六步逐条对齐）。
func (r *CaseRunner) Execute(ctx context.Context, params CaseRunnerParams) (*harnessrsi.EvaluationCaseTraceRef, error) {
	caseID := caseIDOf(params.Case)
	sessionID := fmt.Sprintf("eval_%s_%s", caseID, newUUIDHex())
	caseDir, err := filepath.Abs(params.OutputDir)
	if err != nil {
		return nil, err
	}
	prepareCaseDir(caseDir)
	// 绑定任务级 home：team_home 与 stable_base 工作区派生落在本用例运行时边界内。
	runtimeHomeDir := runtimeHomeDir(caseDir, sessionID)
	if err := os.MkdirAll(runtimeHomeDir, 0o755); err != nil {
		return nil, err
	}
	homeCtx := agentteams.SetTaskOpenuapclawHome(ctx, runtimeHomeDir)
	resultPath := filepath.Join(caseDir, "result.json")
	tracePath := filepath.Join(caseDir, "trace.json")
	startedAt := time.Now()
	resolvedTeamName := "single_harness"
	artifactRefs := map[string][]string{"harvested": {}, "missing": {}}

	// try 块等价：defer 段对应 Python finally。
	defer func() {
		if resolvedTeamName != "" {
			r.backend.Cleanup(resolvedTeamName, sessionID)
		}
		cleanupScratch(caseDir, runtimeHomeDir)
		// 任务级 home 作用域随 homeCtx 结束自然解除（偏差③：ctx 不可变）
		_ = homeCtx
	}()

	// Python except Exception 全覆盖等价：主链任意非基建错误统一在
	// 最外层降级为错误件；基建错误落 evaluation_error.json 后上抛。
	caseState := &executeCaseState{artifactRefs: artifactRefs}
	ref, bodyErr := r.executeCaseBody(homeCtx, params, caseID, sessionID, caseDir,
		tracePath, resultPath, startedAt, resolvedTeamName, caseState)
	if bodyErr != nil {
		var infra *evalerrors.EvaluationInfrastructureError
		if errors.As(bodyErr, &infra) {
			_ = writeJSON(filepath.Join(caseDir, "evaluation_error.json"), map[string]any{
				"case_id": caseID, "status": "error", "score": nil, "error": bodyErr.Error(),
			})
			return nil, bodyErr
		}
		return writeErrorCaseArtifacts(writeErrorCaseArtifactsParams{
			caseMapping: params.Case, caseID: caseID, sessionID: sessionID,
			teamName: resolvedTeamName, startedAt: startedAt, finishedAt: time.Now(),
			tracePath: tracePath, resultPath: resultPath,
			artifactRefs: caseState.artifactRefs, response: caseState.response, errorStr: bodyErr.Error(),
		}), nil
	}
	return ref, nil
}

// executeCaseState 主链对外暴露的中间态（错误件路由需要 response 与已收卷清单）。
type executeCaseState struct {
	// response 主链已取得的回答（backend 成功后即有值）
	response any
	// artifactRefs 收卷清单（harvest 后更新；初始值由 Execute 注入）
	artifactRefs map[string][]string
}

// executeCaseBody 主链主体（Python execute try 块 L112-280）。
// 错误一律上抛，由 Execute 最外层统一路由——对齐 Python except 全覆盖。
func (r *CaseRunner) executeCaseBody(ctx context.Context, params CaseRunnerParams,
	caseID, sessionID, caseDir, tracePath, resultPath string, startedAt time.Time,
	resolvedTeamName string, state *executeCaseState) (*harnessrsi.EvaluationCaseTraceRef, error) {
	executionResult, execErr := r.backend.Execute(ctx, ExecuteParams{
		Case:             params.Case,
		OutputDir:        caseDir,
		SessionID:        sessionID,
		TeamSkillRefPath: params.TeamSkillRefPath,
		HarnessRefs:      params.HarnessRefs,
	})
	if execErr != nil {
		return nil, execErr
	}
	response := executionResult.Response
	state.response = response
	finishedAt := time.Now()

	runtimeWorkspaceDir := executionWorkspaceDir(executionResult, caseDir)
	expectedArtifactFiles := expectedArtifactFiles(params.Case)
	artifactRefs := harvestArtifacts(runtimeWorkspaceDir, filepath.Join(caseDir, "artifacts"), expectedArtifactFiles)
	if executionResult.WorkspaceDir != "" && len(artifactRefs["harvested"]) == 0 {
		artifactRefs = harvestChangedWorkspaceFiles(
			runtimeWorkspaceDir, filepath.Join(caseDir, "artifacts"),
			executionResult.Metadata["workspace_changes"], expectedArtifactFiles)
	}
	state.artifactRefs = artifactRefs

	trajectoryDir := filepath.Join(caseDir, RoleTrajectoryDirName)
	behaviorTrace := behaviorTraceOf(executionResult.Metadata)
	behaviorTrace["artifact_refs"] = mapToAny(artifactRefs)
	trajectoryEvents := buildPreJudgeTrajectoryEvents(
		response, behaviorTrace, executionResult.ExecutionStatus, executionResult.Error)
	trajectoryEventsPath := filepath.Join(trajectoryDir, TrajectoryEventsFileName)
	if err := writeJSONL(trajectoryEventsPath, trajectoryEvents); err != nil {
		return nil, err
	}
	behaviorTrace["trajectory_events_path"] = trajectoryEventsPath
	behaviorTrace["trajectory_window_summary"] = trajectoryWindowSummary(trajectoryEvents, nil)
	memberIdentity := executionMemberIdentity(executionResult, resolvedTeamName)
	normalizedTracePath := filepath.Join(caseDir, "judge", "normalized_trace.json")
	roleTraces := normalizedRoleTracesFromTrajectoryDir(caseID, trajectoryDir)
	normalizedTrace := normalizedTraceFromEvents(
		caseID, trajectoryEvents, nil,
		memberIdentity["member_id"], memberIdentity["member_role"], roleTraces,
		behaviorTrace["execution_evidence"].(map[string]any))
	if err := writeJSON(normalizedTracePath, normalizedTrace); err != nil {
		return nil, err
	}
	behaviorTrace["normalized_trace_path"] = normalizedTracePath
	behaviorTrace["normalized_trace_summary"] = normalizedTrace

	judgeCtx, judgeDone := rsi.ModelUsageStage(ctx, "judge")
	judgeResult, judgeErr := r.judge(judgeCtx, params.Case, executionResult, caseDir, params.BaselineContext)
	judgeDone()
	if judgeErr != nil {
		return nil, judgeErr
	}
	finalStatus := caseResultStatus(executionResult.ExecutionStatus, judgeResult)
	publishedExecutionStatus := publishedExecutionStatus(executionResult.ExecutionStatus, finalStatus, judgeResult)

	trajectoryEvents = buildTrajectoryEvents(
		response, behaviorTrace, judgeResult, executionResult.ExecutionStatus, executionResult.Error)
	if err := writeJSONL(trajectoryEventsPath, trajectoryEvents); err != nil {
		return nil, err
	}
	behaviorTrace["trajectory_window_summary"] = trajectoryWindowSummary(trajectoryEvents, judgeResult)
	normalizedTrace = normalizedTraceFromEvents(
		caseID, trajectoryEvents, judgeResult,
		memberIdentity["member_id"], memberIdentity["member_role"], roleTraces,
		behaviorTrace["execution_evidence"].(map[string]any))
	if err := writeJSON(normalizedTracePath, normalizedTrace); err != nil {
		return nil, err
	}
	behaviorTrace["normalized_trace_summary"] = normalizedTrace
	score := judgeResult.Score
	tracePayload := map[string]any{
		"case_id":          caseID,
		"session_id":       sessionID,
		"team_name":        resolvedTeamName,
		"started_at":       startedAt.Format(time.RFC3339Nano),
		"finished_at":      finishedAt.Format(time.RFC3339Nano),
		"status":           finalStatus,
		"execution_status": publishedExecutionStatus,
		"input":            caseInputs(params.Case),
		"response":         response,
		"error":            executionResult.Error,
		"workspace_dir":    runtimeWorkspaceDir,
		"trajectory_dir":   trajectoryDir,
		"behavior_trace":   behaviorTrace,
		"evaluation": map[string]any{
			"method": judgeResult.Method, "score": score, "passed": judgeResult.Passed,
			"reason": judgeResult.Reason, "metadata": judgeResult.Metadata,
		},
	}
	resultPayload := map[string]any{
		"case_id":          caseID,
		"session_id":       sessionID,
		"status":           finalStatus,
		"execution_status": publishedExecutionStatus,
		"score":            score,
		"evaluation": map[string]any{
			"method": judgeResult.Method, "passed": judgeResult.Passed,
			"reason": judgeResult.Reason, "metadata": judgeResult.Metadata,
		},
		"result":        executionResult.Response,
		"error":         executionResult.Error,
		"workspace_dir": runtimeWorkspaceDir,
		"trace_path":    tracePath,
		"artifacts":     mapToAny(artifactRefs),
		"metadata": map[string]any{
			"team_name":       resolvedTeamName,
			"case_path":       params.Case["case_path"],
			"case_metadata":   params.Case["metadata"],
			"training_signal": params.Case["training_signal"],
			"execution":       executionResult.Metadata,
		},
	}
	if err := writeJSON(tracePath, tracePayload); err != nil {
		return nil, err
	}
	if err := writeJSON(resultPath, resultPayload); err != nil {
		return nil, err
	}

	scoreMode := "binary"
	if mode, ok := judgeResult.Metadata["score_mode"].(string); ok && mode != "" {
		scoreMode = mode
	}
	return &harnessrsi.EvaluationCaseTraceRef{
		CaseID:     caseID,
		CasePath:   stringOf(params.Case["case_path"]),
		TracePath:  tracePath,
		ResultPath: resultPath,
		Status:     finalStatus,
		Score:      &score,
		Metadata: map[string]any{
			"session_id":        sessionID,
			"team_name":         resolvedTeamName,
			"execution_status":  publishedExecutionStatus,
			"evaluation_method": judgeResult.Method,
			"evaluation_passed": judgeResult.Passed,
			"score_mode":        scoreMode,
		},
	}, nil
}

// judge 判分——必须有真实评估结果，不得以完成态充当得分。
// Python: case_runner.py:318 _judge（@model_usage_stage("judge") 由调用方接线）。
func (r *CaseRunner) judge(ctx context.Context, caseMapping map[string]any,
	executionResult *CaseExecutionResult, outputDir string,
	baselineContext judger.BaselineContextView) (*judger.JudgeResult, error) {
	var result *judger.JudgeResult
	if executionResult.JudgeResultView() != nil {
		if baselineContext != nil {
			return nil, evalerrors.NewEvaluationInfrastructureError("Backend grade cannot bypass baseline-aware LLM judging")
		}
		result = executionResult.JudgeResultView()
	} else if r.judger != nil {
		if baselineContext != nil {
			if r.judger.MethodName() != "llm_as_judge" {
				return nil, evalerrors.NewEvaluationInfrastructureError("Baseline comparison requires the LLM judger")
			}
			judged, err := r.judger.Judge(ctx, caseMapping, executionResult, outputDir, baselineContext)
			if err != nil {
				return nil, err
			}
			result = judged
		} else {
			judged, err := r.judger.Judge(ctx, caseMapping, executionResult, outputDir, nil)
			if err != nil {
				return nil, err
			}
			result = judged
		}
	} else {
		return nil, evalerrors.NewEvaluationInfrastructureError(
			"Cannot score this case: no judger configured and no backend JudgeResult. " +
				"Successful execution is not evidence of correctness.")
	}
	if judger.IsExecutionOnlyEvaluation(map[string]any{"method": result.Method, "metadata": result.Metadata}) {
		return nil, evalerrors.NewEvaluationInfrastructureError("Completion-only JudgeResult is not a valid correctness evaluation")
	}
	return result, nil
}

// ──────────────────────────── 非导出函数（模块级助手，Python 模块级函数一比一） ────────────────────────────

// caseIDOf 用例标识（Python: _case_id）。
func caseIDOf(caseMapping map[string]any) string {
	if v := stringOf(caseMapping["case_id"]); v != "" {
		return v
	}
	return fmt.Sprintf("case_%03d", intOf(caseMapping["case_index"], 1))
}

// caseInputs 用例任务输入（Python: _case_inputs——ValueError 时返回 nil 诊断轨迹）。
func caseInputs(caseMapping map[string]any) any {
	input, err := dataloader.TaskInput(caseMapping)
	if err != nil {
		return nil
	}
	return input
}

// caseResultStatus 最终评估状态（Python: _case_result_status）。
func caseResultStatus(executionStatus string, judgeResult *judger.JudgeResult) string {
	if executionStatus == "passed" {
		if judgeResult.Passed {
			return "passed"
		}
		return "failed"
	}
	if stringOf(judgeResult.Metadata["score_mode"]) == "continuous" {
		return "failed"
	}
	return "error"
}

// publishedExecutionStatus 发布用的执行状态（Python: _published_execution_status）。
func publishedExecutionStatus(executionStatus, finalStatus string, judgeResult *judger.JudgeResult) string {
	if finalStatus == "failed" && executionStatus != "passed" &&
		stringOf(judgeResult.Metadata["score_mode"]) == "continuous" {
		return "failed"
	}
	return executionStatus
}

// expectedArtifactFiles 用例显式声明的交付文件清单（Python: _expected_artifact_files）。
func expectedArtifactFiles(caseMapping map[string]any) []string {
	reference := caseMapping["reference"]
	var expected []string
	if mapping, ok := reference.(map[string]any); ok {
		expected = expectedArtifactFilesFromMapping(mapping)
	}
	if len(expected) > 0 {
		return expected
	}
	rawInput := caseInputs(caseMapping)
	var taskText string
	if text, ok := rawInput.(string); ok {
		taskText = text
	} else {
		b, _ := json.Marshal(rawInput)
		taskText = string(b)
	}
	expected = artifactFilesFromCase(caseMapping, taskText)
	if len(expected) > 0 {
		return expected
	}
	b, _ := json.Marshal(caseMapping)
	text := strings.ToLower(string(b))
	for _, filename := range canonicalDeliverableFiles {
		if !strings.Contains(text, filename) {
			return []string{}
		}
	}
	return append([]string{}, canonicalDeliverableFiles...)
}

// expectedArtifactFilesFromMapping 映射五键 + artifact_contract 解析（Python: _expected_artifact_files_from_mapping）。
func expectedArtifactFilesFromMapping(mapping map[string]any) []string {
	for _, key := range []string{"expected_artifacts", "expected_files", "required_files", "output_files", "deliverables"} {
		if files := artifactFileList(mapping[key]); len(files) > 0 {
			return files
		}
	}
	if contract, ok := mapping["artifact_contract"].(map[string]any); ok {
		for _, key := range []string{"final_files", "expected_files", "required_files", "deliverables"} {
			if files := artifactFileList(contract[key]); len(files) > 0 {
				return files
			}
		}
	}
	return []string{}
}

// artifactFileList 值列表→文件名列表（Python: _artifact_file_list）。
func artifactFileList(values any) []string {
	var result []string
	list, ok := values.([]any)
	if !ok {
		return result
	}
	for _, value := range list {
		var name string
		switch v := value.(type) {
		case string:
			name = filepath.Base(v)
		case map[string]any:
			p := stringOf(v["path"])
			if p == "" {
				p = stringOf(v["file"])
			}
			name = filepath.Base(p)
		default:
			continue
		}
		if name != "" && !containsStr(result, name) {
			result = append(result, name)
		}
	}
	return result
}

// writeJSON 落 JSON 文件（Python: _write_json）。
func writeJSON(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// writeJSONL 落 JSONL 文件（Python: _write_jsonl）。
func writeJSONL(path string, events []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	for _, event := range events {
		b, err := json.Marshal(event)
		if err != nil {
			return err
		}
		sb.Write(b)
		sb.WriteString("\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// executionWorkspaceDir 执行工作区目录（Python: _execution_workspace_dir）。
func executionWorkspaceDir(executionResult *CaseExecutionResult, caseDir string) string {
	if executionResult.WorkspaceDir != "" {
		if abs, err := filepath.Abs(executionResult.WorkspaceDir); err == nil {
			return abs
		}
		return executionResult.WorkspaceDir
	}
	return filepath.Join(caseDir, "workspace")
}

// runtimeHomeDir 用例级运行时 home（Python: _runtime_home_dir）。
func runtimeHomeDir(caseDir, sessionID string) string {
	if !needsShortRuntimeHome() {
		return caseDir
	}
	digest := sha1.Sum([]byte(fmt.Sprintf("%s::%s", caseDir, sessionID)))
	return filepath.Join(os.TempDir(), "ach_team_runtime", hex.EncodeToString(digest[:])[:16])
}

// prepareCaseDir 清空重建题目目录（Python: _prepare_case_dir）。
func prepareCaseDir(caseDir string) error {
	if _, err := os.Stat(caseDir); err == nil {
		if err := os.RemoveAll(caseDir); err != nil {
			return err
		}
	}
	return os.MkdirAll(caseDir, 0o755)
}

// needsShortRuntimeHome 是否走短路径 home（Python: _needs_short_runtime_home——仅 Windows）。
func needsShortRuntimeHome() bool {
	return runtime.GOOS == "windows"
}

// behaviorTraceOf 行为证据（Python: _behavior_trace）。
func behaviorTraceOf(metadata map[string]any) map[string]any {
	executionEvidence, ok := metadata["execution_evidence"].(map[string]any)
	if !ok || executionEvidence == nil {
		executionEvidence = map[string]any{"status": "incomplete", "reasons": []any{"capture_metadata_missing"}}
	}
	commandLog, ok := metadata["command_log"].([]any)
	if !ok {
		commandLog = []any{}
	}
	workspaceChanges, ok := metadata["workspace_changes"].(map[string]any)
	if !ok || workspaceChanges == nil {
		workspaceChanges = map[string]any{"added": []any{}, "modified": []any{}, "removed": []any{}}
	}
	return map[string]any{
		"execution_evidence": executionEvidence,
		"command_log":        commandLog,
		"workspace_changes":  workspaceChanges,
	}
}

// executionMemberIdentity 成员身份归因（Python: _execution_member_identity）。
func executionMemberIdentity(executionResult *CaseExecutionResult, teamName string) map[string]string {
	metadata := executionResult.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	memberRole := strings.TrimSpace(stringOf(metadata["member_role"]))
	if memberRole == "" {
		memberRole = strings.TrimSpace(stringOf(metadata["role"]))
	}
	memberID := strings.TrimSpace(stringOf(metadata["member_id"]))
	if memberID == "" {
		memberID = memberRole
	}
	if memberRole == "" {
		if teamName != "" {
			memberRole = "team"
		} else {
			memberRole = "unattributed"
		}
	}
	if memberID == "" {
		if teamName != "" {
			memberID = teamName
		} else {
			memberID = memberRole
		}
	}
	return map[string]string{"member_id": memberID, "member_role": memberRole}
}

// ──────────────────────────── 通用小助手 ────────────────────────────

// stringOf any→trim 后字符串（nil→空串）。
func stringOf(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// intOf any→int（缺省 fallback）。
func intOf(v any, fallback int) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return fallback
	}
}

// containsStr 列表包含判定。
func containsStr(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}

// mapToAny map[string][]string 升为 map[string]any。
func mapToAny(m map[string][]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// sortedFileInfos 目录内文件相对路径有序清单（rglob("*") is_file 等价）。
func sortedFileInfos(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files
}

// newUUIDHex 32 位十六进制随机串（Python uuid4().hex 等价；仅作会话唯一标识）。
func newUUIDHex() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ──────────────────────────── 轨迹事件群组（Python L522-724） ────────────────────────────

// buildTrajectoryEvents 用例级有界事件流（Python: _build_trajectory_events）。
func buildTrajectoryEvents(response any, behaviorTrace map[string]any,
	judgeResult *judger.JudgeResult, executionStatus, executionError string) []map[string]any {
	var events []map[string]any
	// ①tool_call：命令日志逐条
	if commandLog, ok := behaviorTrace["command_log"].([]any); ok {
		for _, item := range commandLog {
			command, ok := item.(map[string]any)
			if !ok {
				continue
			}
			events = append(events, trajectoryEvent("tool_call",
				commandSummary(command),
				map[string]any{
					"command": command["command"], "cwd": command["cwd"],
					"exit_code":      command["exit_code"],
					"stdout_excerpt": command["stdout_excerpt"], "stderr_excerpt": command["stderr_excerpt"],
					"timeout_sec": command["timeout_sec"], "background": command["background"],
				}))
		}
	}
	// ②workspace_change：任一集合非空才记
	if workspaceChanges, ok := behaviorTrace["workspace_changes"].(map[string]any); ok &&
		anyListNonEmpty(workspaceChanges, "added", "modified", "removed") {
		events = append(events, trajectoryEvent("workspace_change",
			workspaceChangeSummary(workspaceChanges), workspaceChanges))
	}
	// ③agent_response：响应非空才记
	if response != nil && response != "" {
		respText := fmt.Sprintf("%v", response)
		events = append(events, trajectoryEvent("agent_response",
			excerpt(respText, 600),
			map[string]any{"response_excerpt": excerpt(respText, 2000)}))
	}
	// ④artifact_harvest：有收卷才记
	if artifactRefs, ok := behaviorTrace["artifact_refs"].(map[string]any); ok && len(anyListOf(artifactRefs, "harvested")) > 0 {
		events = append(events, trajectoryEvent("artifact_harvest",
			artifactRefsSummary(artifactRefs), artifactRefs))
	}
	// ⑤verifier_result
	events = append(events, trajectoryEvent("verifier_result",
		fmt.Sprintf("%s: passed=%v, score=%v, reason=%s",
			judgeResult.Method, judgeResult.Passed, judgeResult.Score, excerpt(judgeResult.Reason, 600)),
		map[string]any{
			"method": judgeResult.Method, "score": judgeResult.Score, "passed": judgeResult.Passed,
			"reason": judgeResult.Reason, "metadata": judgeResult.Metadata,
		}))
	// ⑥case_outcome
	events = append(events, trajectoryEvent("case_outcome",
		fmt.Sprintf("execution_status=%s, passed=%v, score=%v", executionStatus, judgeResult.Passed, judgeResult.Score),
		map[string]any{
			"execution_status": executionStatus, "execution_error": executionError,
			"passed": judgeResult.Passed, "score": judgeResult.Score,
		}))
	for i, event := range events {
		event["event_index"] = i + 1
	}
	return events
}

// buildPreJudgeTrajectoryEvents 判分前的稳定执行证据（Python: _build_pre_judge_trajectory_events）。
func buildPreJudgeTrajectoryEvents(response any, behaviorTrace map[string]any,
	executionStatus, executionError string) []map[string]any {
	var events []map[string]any
	if commandLog, ok := behaviorTrace["command_log"].([]any); ok {
		for _, item := range commandLog {
			command, ok := item.(map[string]any)
			if !ok {
				continue
			}
			events = append(events, trajectoryEvent("tool_call",
				commandSummary(command),
				map[string]any{
					"command": command["command"], "cwd": command["cwd"],
					"exit_code":      command["exit_code"],
					"stdout_excerpt": command["stdout_excerpt"], "stderr_excerpt": command["stderr_excerpt"],
					"timeout_sec": command["timeout_sec"], "background": command["background"],
				}))
		}
	}
	if workspaceChanges, ok := behaviorTrace["workspace_changes"].(map[string]any); ok &&
		anyListNonEmpty(workspaceChanges, "added", "modified", "removed") {
		events = append(events, trajectoryEvent("workspace_change",
			workspaceChangeSummary(workspaceChanges), workspaceChanges))
	}
	if artifactRefs, ok := behaviorTrace["artifact_refs"].(map[string]any); ok && len(anyListOf(artifactRefs, "harvested")) > 0 {
		events = append(events, trajectoryEvent("artifact_harvest",
			artifactRefsSummary(artifactRefs), artifactRefs))
	}
	if response != nil && response != "" {
		respText := fmt.Sprintf("%v", response)
		events = append(events, trajectoryEvent("agent_response",
			excerpt(respText, 600),
			map[string]any{"response_excerpt": excerpt(respText, 2000)}))
	}
	events = append(events, trajectoryEvent("case_outcome",
		fmt.Sprintf("execution_status=%s, judge_status=pending, execution_error=%s",
			executionStatus, excerpt(executionError, 600)),
		map[string]any{
			"execution_status": executionStatus, "execution_error": executionError,
			"judge_status": "pending",
		}))
	for i, event := range events {
		event["event_index"] = i + 1
	}
	return events
}

// trajectoryEvent 单条轨迹事件（Python: _trajectory_event）。
func trajectoryEvent(eventType, summary string, data map[string]any) map[string]any {
	return map[string]any{"event_type": eventType, "summary": summary, "data": data}
}

// trajectoryWindowSummary 窗口摘要（Python: _trajectory_window_summary）。
func trajectoryWindowSummary(events []map[string]any, judgeResult *judger.JudgeResult) map[string]any {
	const windowSize = 20
	recent := events
	if len(events) > windowSize {
		recent = events[len(events)-windowSize:]
	}
	var failureSigs []string
	if judgeResult != nil {
		failureSigs = failureSignatures(events, judgeResult)
	} else {
		failureSigs = []string{}
	}
	recentEvents := make([]map[string]any, 0, len(recent))
	for _, event := range recent {
		recentEvents = append(recentEvents, map[string]any{
			"event_index": event["event_index"],
			"event_type":  stringOf(event["event_type"]),
			"summary":     stringOf(event["summary"]),
		})
	}
	return map[string]any{
		"window_size":        windowSize,
		"event_count":        len(events),
		"failure_signatures": failureSigs,
		"recent_events":      recentEvents,
	}
}

// failureSignatures 失败签名（Python: _failure_signatures）。
func failureSignatures(events []map[string]any, judgeResult *judger.JudgeResult) []string {
	if judgeResult.Passed {
		return []string{}
	}
	var signatures []string
	for _, event := range events {
		data, _ := event["data"].(map[string]any)
		if stringOf(event["event_type"]) == "tool_call" {
			switch code := data["exit_code"].(type) {
			case int:
				if code != 0 {
					signatures = append(signatures, "tool_execution_failure")
				}
			case float64:
				if code != 0 {
					signatures = append(signatures, "tool_execution_failure")
				}
			}
		}
	}
	// 仅 patch 评估器确立"工作区必须有改动"。
	if judgeResult.Method == "swebench_official" {
		hasWorkspaceChange := false
		for _, event := range events {
			if stringOf(event["event_type"]) == "workspace_change" {
				hasWorkspaceChange = true
				break
			}
		}
		if hasWorkspaceChange {
			signatures = append(signatures, "patch_quality_gap")
		} else {
			signatures = append(signatures, "workspace_edit_gap")
		}
	}
	return signatures
}

// commandSummary 命令摘要（Python: _command_summary）。
func commandSummary(command map[string]any) string {
	status := "timed out"
	if code, ok := command["exit_code"]; ok && code != nil {
		status = fmt.Sprintf("exited with %v", code)
	}
	stderr := stringOf(command["stderr_excerpt"])
	detail := ""
	if stderr != "" {
		detail = fmt.Sprintf("; stderr=%s", excerpt(stderr, 240))
	}
	return fmt.Sprintf("%v %s%s", command["command"], status, detail)
}

// workspaceChangeSummary 改动摘要（Python: _workspace_change_summary）。
func workspaceChangeSummary(workspaceChanges map[string]any) string {
	return fmt.Sprintf("added=%v, modified=%v, removed=%v",
		boundedList(workspaceChanges["added"]), boundedList(workspaceChanges["modified"]),
		boundedList(workspaceChanges["removed"]))
}

// artifactRefsSummary 收卷摘要（Python: _artifact_refs_summary）。
func artifactRefsSummary(artifactRefs map[string]any) string {
	return fmt.Sprintf("artifact_harvest: harvested=%v, missing=%v",
		boundedList(artifactRefs["harvested"]), boundedList(artifactRefs["missing"]))
}

// excerpt 超长截断（Python: _excerpt）。
func excerpt(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars-3] + "..."
}

// anyListNonEmpty 任一键下列表非空。
func anyListNonEmpty(mapping map[string]any, keys ...string) bool {
	for _, key := range keys {
		if len(anyListOf(mapping, key)) > 0 {
			return true
		}
	}
	return false
}

// anyListOf 键下列表（非列表→nil）。
func anyListOf(mapping map[string]any, key string) []any {
	list, _ := mapping[key].([]any)
	return list
}

// boundedList 列表前 20 项（Python list(x)[:20]）。
func boundedList(v any) []any {
	list, _ := v.([]any)
	if len(list) > 20 {
		return list[:20]
	}
	if list == nil {
		return []any{}
	}
	return list
}

// ──────────────────────────── 规范化 trace 群组（Python L743-1046） ────────────────────────────

// normalizedTraceFromEvents 事件流→分析器规范化 trace 契约（Python: _normalized_trace_from_events）。
func normalizedTraceFromEvents(caseID string, events []map[string]any, judgeResult *judger.JudgeResult,
	memberID, memberRole string, roleTraces []map[string]any, executionEvidence map[string]any) map[string]any {
	var messages []map[string]any
	for index, event := range events {
		eventType := stringOf(event["event_type"])
		eventIndex := index + 1
		if v, ok := event["event_index"].(int); ok && v != 0 {
			eventIndex = v
		}
		message := map[string]any{"role": "assistant", "message_index": index}
		switch eventType {
		case "tool_call":
			data, _ := event["data"].(map[string]any)
			if data == nil {
				data = map[string]any{}
			}
			exitCodeNil := data["exit_code"] == nil
			if code, ok := data["exit_code"].(float64); ok && code == 0 {
				exitCodeNil = true
			}
			if code, ok := data["exit_code"].(int); ok && code == 0 {
				exitCodeNil = true
			}
			errorText := ""
			if !exitCodeNil {
				errorText = excerpt(stringOf(data["stderr_excerpt"]), 1200)
			}
			message["tool_calls"] = []any{map[string]any{
				"name":         "shell",
				"input":        excerpt(stringOf(data["command"]), 1200),
				"output":       excerpt(stringOf(data["stdout_excerpt"]), 1200),
				"error":        errorText,
				"step_pointer": fmt.Sprintf("step_%d", eventIndex),
			}}
		case "verifier_result":
			data, _ := event["data"].(map[string]any)
			if data == nil {
				data = map[string]any{}
			}
			method := stringOf(data["method"])
			reason := stringOf(data["reason"])
			if method == "" && judgeResult != nil {
				method = judgeResult.Method
			}
			if reason == "" && judgeResult != nil {
				reason = judgeResult.Reason
			}
			passed, passedSet := data["passed"].(bool)
			if !passedSet && judgeResult != nil {
				passed = judgeResult.Passed
			} else if !passedSet {
				passed = true
			}
			errorText := ""
			if !passed {
				errorText = excerpt(stringOf(data["reason"]), 1200)
			}
			message["tool_calls"] = []any{map[string]any{
				"name":         "verifier",
				"input":        method,
				"output":       excerpt(reason, 1200),
				"error":        errorText,
				"step_pointer": fmt.Sprintf("step_%d", eventIndex),
			}}
		default:
			message["content"] = excerpt(stringOf(event["summary"]), 1200)
			message["step_pointer"] = fmt.Sprintf("step_%d", eventIndex)
		}
		messages = append(messages, message)
	}
	traces := make([]map[string]any, 0, len(roleTraces)+1)
	traces = append(traces, roleTraces...)
	traces = append(traces, map[string]any{
		"trace_id":      fmt.Sprintf("%s__%s__case", caseID, memberID),
		"member_id":     memberID,
		"member_role":   memberRole,
		"execution_id":  caseID,
		"step_count":    len(events),
		"message_count": len(messages),
		"messages":      messages,
	})
	if executionEvidence == nil {
		executionEvidence = map[string]any{"status": "incomplete", "reasons": []any{"capture_metadata_missing"}}
	}
	projectionComplete := len(roleTraces) > 0
	for _, trace := range roleTraces {
		if v, ok := trace["projection_complete"].(bool); !ok || !v {
			projectionComplete = false
			break
		}
	}
	return map[string]any{
		"case_id":             caseID,
		"execution_evidence":  executionEvidence,
		"projection_complete": projectionComplete,
		"traces":              traces,
	}
}

// normalizedRoleTracesFromTrajectoryDir 角色轨迹→有界规范化 trace（Python: _normalized_role_traces_from_trajectory_dir）。
func normalizedRoleTracesFromTrajectoryDir(caseID, trajectoryDir string) []map[string]any {
	info, err := os.Stat(trajectoryDir)
	if err != nil || !info.IsDir() {
		return []map[string]any{}
	}
	var traces []map[string]any
	roleCounts := map[string]int{}
	for _, entry := range iterRoleTrajectoryFiles(trajectoryDir) {
		roleCounts[entry.role]++
		traceRoleID := entry.role
		if roleCounts[entry.role] != 1 {
			traceRoleID = fmt.Sprintf("%s_%s", entry.role, strings.TrimSuffix(filepath.Base(entry.path), ".jsonl"))
		}
		messages := messagesFromRoleTrajectoryFile(entry.path)
		if len(messages) == 0 {
			size := int64(0)
			if st, serr := os.Stat(entry.path); serr == nil {
				size = st.Size()
			}
			messages = []map[string]any{{
				"role": "assistant", "message_index": 0,
				"content": fmt.Sprintf(
					"trajectory file exists for role %s but has no parsable steps; file=%s; bytes=%d",
					entry.role, filepath.Base(entry.path), size),
				"step_pointer": "trajectory_summary",
			}}
		}
		projectionComplete := len(messages) < maxRoleTraceMessages
		for _, message := range messages {
			sp := stringOf(message["step_pointer"])
			if sp == "trajectory_summary" || sp == "trajectory_tail" {
				projectionComplete = false
				break
			}
		}
		traces = append(traces, map[string]any{
			"trace_id":            fmt.Sprintf("%s__%s__trajectory", caseID, traceRoleID),
			"member_id":           entry.role,
			"member_role":         entry.role,
			"execution_id":        caseID,
			"projection_complete": projectionComplete,
			"step_count":          len(messages),
			"message_count":       len(messages),
			"messages":            messages,
		})
	}
	return traces
}

// roleTrajectoryFile 角色轨迹文件条目。
type roleTrajectoryFile struct {
	role string
	path string
}

// iterRoleTrajectoryFiles 平铺+legacy 嵌套角色轨迹 JSONL（Python: _iter_role_trajectory_files）。
func iterRoleTrajectoryFiles(trajectoryDir string) []roleTrajectoryFile {
	var files []roleTrajectoryFile
	// 平铺：*.jsonl
	flat, _ := filepath.Glob(filepath.Join(trajectoryDir, "*.jsonl"))
	sort.Strings(flat)
	for _, path := range flat {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		name := filepath.Base(path)
		if name == TrajectoryEventsFileName || strings.HasPrefix(name, "traces-") {
			continue
		}
		files = append(files, roleTrajectoryFile{role: strings.TrimSuffix(name, ".jsonl"), path: path})
	}
	// 嵌套：<sub>/*.jsonl
	nested, _ := filepath.Glob(filepath.Join(trajectoryDir, "*", "*.jsonl"))
	sort.Strings(nested)
	for _, path := range nested {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		name := filepath.Base(path)
		if name == TrajectoryEventsFileName || strings.HasPrefix(name, "traces-") {
			continue
		}
		files = append(files, roleTrajectoryFile{role: filepath.Base(filepath.Dir(path)), path: path})
	}
	return files
}

// messagesFromRoleTrajectoryFile 小文件读为规范化消息（Python: _messages_from_role_trajectory_file）。
func messagesFromRoleTrajectoryFile(path string) []map[string]any {
	info, err := os.Stat(path)
	if err != nil {
		return []map[string]any{}
	}
	if info.Size() > maxRoleTrajectoryFileBytes {
		return messagesFromLargeRoleTrajectoryFile(path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return []map[string]any{}
	}
	lines := strings.Split(string(raw), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var trajectory map[string]any
		if json.Unmarshal([]byte(line), &trajectory) != nil {
			continue
		}
		if trajectory != nil {
			return messagesFromRoleTrajectory(trajectory)
		}
	}
	return []map[string]any{}
}

// messagesFromLargeRoleTrajectoryFile 大文件只读尾部（Python: _messages_from_large_role_trajectory_file）。
func messagesFromLargeRoleTrajectoryFile(path string) []map[string]any {
	info, err := os.Stat(path)
	if err != nil {
		return []map[string]any{}
	}
	size := info.Size()
	readSize := int64(roleTrajectoryTailBytes)
	if size < readSize {
		readSize = size
	}
	f, err := os.Open(path)
	if err != nil {
		return []map[string]any{}
	}
	defer f.Close()
	offset := size - readSize
	if offset < 0 {
		offset = 0
	}
	rawTail := make([]byte, readSize)
	if _, err := f.ReadAt(rawTail, offset); err != nil && err != io.EOF {
		// Python except OSError: return []——读失败返回空消息，不解析残缺数据
		return []map[string]any{}
	}
	tail := strings.TrimSpace(decodeIgnoreUTF8(rawTail))
	if tail == "" {
		return []map[string]any{}
	}
	return indexMessages([]map[string]any{{
		"role":         "assistant",
		"content":      fmt.Sprintf("bounded tail excerpt from large role trajectory: %s", excerpt(tail, 2400)),
		"step_pointer": "trajectory_tail",
	}})
}

// messagesFromRoleTrajectory 累积 ReAct 快照→紧凑因果时间线（Python: _messages_from_role_trajectory）。
func messagesFromRoleTrajectory(trajectory map[string]any) []map[string]any {
	var messages []map[string]any
	seenControlMessages := map[string]bool{}
	steps, _ := trajectory["steps"].([]any)
	for stepIndex, stepItem := range steps {
		step, ok := stepItem.(map[string]any)
		if !ok {
			continue
		}
		detail, ok := step["detail"].(map[string]any)
		if !ok {
			continue
		}
		responseMessage := normalizeTrajectoryMessage(detail["response"])
		stepMsgs, _ := detail["messages"].([]any)
		for _, msgItem := range stepMsgs {
			normalized := normalizeTrajectoryMessage(msgItem)
			if len(normalized) == 0 {
				continue
			}
			role := stringOf(normalized["role"])
			if role != "system" && role != "user" {
				continue
			}
			identityB, _ := json.Marshal(normalized)
			identity := sortedJSON(string(identityB))
			if seenControlMessages[identity] {
				continue
			}
			seenControlMessages[identity] = true
			normalized["step_pointer"] = fmt.Sprintf("trajectory_step_%d:input", stepIndex+1)
			messages = append(messages, normalized)
		}
		if len(responseMessage) > 0 {
			responseMessage["step_pointer"] = fmt.Sprintf("trajectory_step_%d:response", stepIndex+1)
			messages = append(messages, responseMessage)
		}
		toolName := stringOf(detail["tool_name"])
		if toolName != "" {
			callArgs, ok := detail["call_args"].(map[string]any)
			if !ok {
				callArgs, _ = detail["args"].(map[string]any)
			}
			callResult, ok2 := detail["call_result"].(map[string]any)
			if !ok2 {
				callResult, _ = detail["result"].(map[string]any)
			}
			argsB, _ := json.Marshal(callArgs)
			resultB, _ := json.Marshal(callResult)
			errText := ""
			if stepErr := stringOf(step["error"]); stepErr != "" {
				errText = excerpt(stepErr, 1200)
			}
			messages = append(messages, map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"name":   toolName,
					"input":  excerpt(string(argsB), 1200),
					"output": excerpt(string(resultB), 1200),
					"error":  errText,
				}},
				"step_pointer": fmt.Sprintf("trajectory_step_%d:tool", stepIndex+1),
			})
		}
	}
	return indexMessages(boundedRoleTraceMessages(messages))
}

// boundedRoleTraceMessages 有界保头保尾（Python: _bounded_role_trace_messages）。
func boundedRoleTraceMessages(messages []map[string]any) []map[string]any {
	if len(messages) <= maxRoleTraceMessages {
		return messages
	}
	headCount := maxRoleTraceMessages / 2
	tailCount := maxRoleTraceMessages - headCount
	out := make([]map[string]any, 0, maxRoleTraceMessages)
	out = append(out, messages[:headCount]...)
	out = append(out, messages[len(messages)-tailCount:]...)
	return out
}

// normalizeTrajectoryMessage 消息归一（Python: _normalize_trajectory_message）。
func normalizeTrajectoryMessage(message any) map[string]any {
	mapping, ok := message.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	normalized := map[string]any{"role": stringOf(mapping["role"])}
	if normalized["role"] == "" {
		normalized["role"] = "unknown"
	}
	content := strings.TrimSpace(stringOf(mapping["content"]))
	reasoning := strings.TrimSpace(stringOf(mapping["reasoning_content"]))
	if reasoning != "" {
		reasoningBlock := "[reasoning]\n" + reasoning
		if content != "" {
			content = content + "\n\n" + reasoningBlock
		} else {
			content = reasoningBlock
		}
	}
	if content != "" {
		normalized["content"] = excerpt(content, 1200)
	}
	if _, ok := mapping["tool_calls"]; ok {
		normalized["tool_calls"] = mapping["tool_calls"]
	}
	if len(normalized) == 1 {
		return map[string]any{}
	}
	return normalized
}

// indexMessages 消息编号（Python: _index_messages）。
func indexMessages(messages []map[string]any) []map[string]any {
	var indexed []map[string]any
	for index, message := range messages {
		if index >= maxRoleTraceMessages {
			break
		}
		item := make(map[string]any, len(message)+2)
		for k, v := range message {
			item[k] = v
		}
		item["message_index"] = index
		if _, ok := item["step_pointer"]; !ok {
			item["step_pointer"] = fmt.Sprintf("trajectory_step_%d", index+1)
		}
		indexed = append(indexed, item)
	}
	return indexed
}

// sortedJSON 键排序 JSON（Python json.dumps(sort_keys=True) 等价——归一化已按固定序，
// 这里直接序列化即可；保留函数名便于对齐审阅）。
func sortedJSON(s string) string {
	return s
}

// decodeIgnoreUTF8 忽略非法字节解码（Python errors="ignore" 等价——非法字节替换）。
func decodeIgnoreUTF8(raw []byte) string {
	return strings.ToValidUTF8(string(raw), "")
}

// ──────────────────────────── 错误件群组（Python L1047-1146） ────────────────────────────

// writeErrorCaseArtifactsParams 错误件入参包（Python 关键字参数）。
type writeErrorCaseArtifactsParams struct {
	caseMapping  map[string]any
	caseID       string
	sessionID    string
	teamName     string
	startedAt    time.Time
	finishedAt   time.Time
	tracePath    string
	resultPath   string
	artifactRefs map[string][]string
	response     any
	errorStr     string
}

// writeErrorCaseArtifacts 错误件落盘（Python: _write_error_case_artifacts）。
func writeErrorCaseArtifacts(p writeErrorCaseArtifactsParams) *harnessrsi.EvaluationCaseTraceRef {
	trajectoryDir := filepath.Join(filepath.Dir(p.tracePath), RoleTrajectoryDirName)
	behaviorTrace := behaviorTraceOf(map[string]any{})
	trajectoryEvents := buildPreJudgeTrajectoryEvents(p.response, behaviorTrace, "error", p.errorStr)
	trajectoryEventsPath := filepath.Join(trajectoryDir, TrajectoryEventsFileName)
	_ = writeJSONL(trajectoryEventsPath, trajectoryEvents)
	behaviorTrace["trajectory_events_path"] = trajectoryEventsPath
	behaviorTrace["trajectory_window_summary"] = trajectoryWindowSummary(trajectoryEvents, nil)
	normalizedTracePath := filepath.Join(filepath.Dir(p.tracePath), "judge", "normalized_trace.json")
	normalizedTrace := normalizedTraceFromEvents(p.caseID, trajectoryEvents, nil, "unattributed", "unattributed", nil, nil)
	_ = writeJSON(normalizedTracePath, normalizedTrace)
	behaviorTrace["normalized_trace_path"] = normalizedTracePath
	behaviorTrace["normalized_trace_summary"] = normalizedTrace
	tracePayload := map[string]any{
		"case_id":        p.caseID,
		"session_id":     p.sessionID,
		"team_name":      p.teamName,
		"started_at":     p.startedAt.Format(time.RFC3339Nano),
		"finished_at":    p.finishedAt.Format(time.RFC3339Nano),
		"status":         "error",
		"input":          caseInputs(p.caseMapping),
		"response":       p.response,
		"error":          p.errorStr,
		"trajectory_dir": trajectoryDir,
		"behavior_trace": behaviorTrace,
		"evaluation": map[string]any{
			"method": "error", "score": 0.0, "passed": false,
			"reason": p.errorStr, "metadata": map[string]any{},
		},
	}
	resultPayload := map[string]any{
		"case_id":    p.caseID,
		"session_id": p.sessionID,
		"status":     "error",
		"score":      0.0,
		"evaluation": map[string]any{
			"method": "error", "passed": false,
			"reason": p.errorStr, "metadata": map[string]any{},
		},
		"result":     p.response,
		"error":      p.errorStr,
		"trace_path": p.tracePath,
		"artifacts":  mapToAny(p.artifactRefs),
		"metadata": map[string]any{
			"team_name":       p.teamName,
			"case_path":       p.caseMapping["case_path"],
			"case_metadata":   p.caseMapping["metadata"],
			"training_signal": p.caseMapping["training_signal"],
		},
	}
	_ = writeJSON(p.tracePath, tracePayload)
	_ = writeJSON(p.resultPath, resultPayload)
	score := 0.0
	return &harnessrsi.EvaluationCaseTraceRef{
		CaseID:     p.caseID,
		CasePath:   stringOf(p.caseMapping["case_path"]),
		TracePath:  p.tracePath,
		ResultPath: p.resultPath,
		Status:     "error",
		Score:      &score,
		Metadata: map[string]any{
			"session_id":        p.sessionID,
			"team_name":         p.teamName,
			"evaluation_method": "error",
			"evaluation_passed": false,
		},
	}
}

// ──────────────────────────── 收卷与清理群组（Python L1147-1549） ────────────────────────────

// harvestArtifacts 收卷（Python: _harvest_artifacts）。
func harvestArtifacts(workspaceDir, destDir string, expectedFiles []string) map[string][]string {
	expected := normalizeExpectedFiles(expectedFiles)
	src := filepath.Join(workspaceDir, "artifacts")
	sources := artifactHarvestSources(workspaceDir)
	var missing []string
	if info, err := os.Stat(destDir); err == nil && info.IsDir() {
		normalizeDeliverableArtifacts(destDir)
		if len(expected) > 0 {
			missing = filterExpectedArtifacts(destDir, expected)
		} else {
			missing = []string{}
		}
		preHarvested := sortedFileInfos(destDir)
		if len(preHarvested) > 0 {
			if !pathIsDir(src) {
				logger.Info(logComponentCR).Msgf(
					"[CaseRunner] artifacts already harvested by rail (%d file(s)); workspace unavailable", len(preHarvested))
				return map[string][]string{"harvested": preHarvested, "missing": missing}
			}
			logger.Info(logComponentCR).Msgf(
				"[CaseRunner] refreshing %d pre-harvested artifact(s) from workspace", len(preHarvested))
		}
	}
	if !pathIsDir(src) {
		logger.Info(logComponentCR).Msg("[CaseRunner] no artifacts directory in workspace")
		return map[string][]string{"harvested": {}, "missing": {}}
	}
	_ = os.MkdirAll(destDir, 0o755)
	var harvested []string
	if len(expected) > 0 {
		harvested, missing = copyArtifactTrees(sources, destDir)
		normalizeDeliverableArtifacts(destDir)
		missing = filterExpectedArtifacts(destDir, expected)
	} else {
		harvested, missing = copyArtifactTrees(sources, destDir)
		normalizeDeliverableArtifacts(destDir)
	}
	harvested = sortedFileInfos(destDir)
	return map[string][]string{"harvested": harvested, "missing": missing}
}

// artifactHarvestSources 收卷来源集合（Python: _artifact_harvest_sources）。
func artifactHarvestSources(workspaceDir string) []string {
	var sources []string
	src := filepath.Join(workspaceDir, "artifacts")
	if pathIsDir(src) {
		sources = append(sources, src)
	}
	teamRoot := filepath.Dir(workspaceDir)
	teamName := filepath.Base(teamRoot)
	memberRoot := filepath.Join(teamRoot, "workspaces")
	if entries, err := os.ReadDir(memberRoot); err == nil {
		var memberDirs []string
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasSuffix(name, "_workspace") && entry.IsDir() {
				memberDirs = append(memberDirs, name)
			}
		}
		sort.Strings(memberDirs)
		for _, member := range memberDirs {
			for _, candidate := range []string{
				filepath.Join(memberRoot, member, ".team", teamName, "artifacts"),
				filepath.Join(memberRoot, member, "artifacts"),
			} {
				if !pathIsDir(candidate) {
					continue
				}
				if samePathAny(candidate, sources) {
					continue
				}
				sources = append(sources, candidate)
			}
		}
	}
	return sources
}

// samePathAny 与任一既有来源同路径（Python: _same_path 循环）。
func samePathAny(candidate string, existing []string) bool {
	for _, path := range existing {
		if samePath(candidate, path) {
			return true
		}
	}
	return false
}

// samePath 同路径判定（Python: _same_path——samefile 失败回退 resolve 比较）。
func samePath(left, right string) bool {
	li, lerr := os.Stat(left)
	ri, rerr := os.Stat(right)
	if lerr == nil && rerr == nil {
		return os.SameFile(li, ri)
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

// harvestChangedWorkspaceFiles 改动文件收卷（Python: _harvest_changed_workspace_files）。
func harvestChangedWorkspaceFiles(workspaceDir, destDir string, workspaceChanges any, expectedFiles []string) map[string][]string {
	if !pathIsDir(workspaceDir) {
		return map[string][]string{"harvested": {}, "missing": {}}
	}
	changes, ok := workspaceChanges.(map[string]any)
	if !ok {
		return map[string][]string{"harvested": {}, "missing": {}}
	}
	var candidatePaths []string
	for _, key := range []string{"added", "modified"} {
		for _, v := range anyListOf(changes, key) {
			candidatePaths = append(candidatePaths, stringOf(v))
		}
	}
	var harvested, missing []string
	limit := len(candidatePaths)
	if limit > 200 {
		limit = 200
	}
	absWS, _ := filepath.Abs(workspaceDir)
	for _, rel := range candidatePaths[:limit] {
		sourceAbs, serr := filepath.Abs(filepath.Join(workspaceDir, rel))
		if serr != nil {
			missing = append(missing, rel)
			continue
		}
		relToWS, rerr := filepath.Rel(absWS, sourceAbs)
		if rerr != nil || strings.HasPrefix(relToWS, "..") {
			missing = append(missing, rel)
			continue
		}
		if info, ierr := os.Stat(sourceAbs); ierr != nil || info.IsDir() || skipHarvestPath(rel) {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		if err := utils.CopyFile(sourceAbs, target); err != nil {
			missing = append(missing, rel)
			continue
		}
		harvested = append(harvested, filepath.ToSlash(rel))
	}
	expected := normalizeExpectedFiles(expectedFiles)
	normalizeDeliverableArtifacts(destDir)
	if len(expected) > 0 {
		missing = append(missing, filterExpectedArtifacts(destDir, expected)...)
	}
	harvested = sortedFileInfos(destDir)
	return map[string][]string{"harvested": harvested, "missing": missing}
}

// copyArtifactTree 逐文件拷贝产物树（Python: _copy_artifact_tree）。
func copyArtifactTree(src, destDir string) ([]string, []string) {
	var harvested, missing []string
	_ = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		relPath, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return nil
		}
		target := filepath.Join(destDir, relPath)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		if shouldCopyArtifact(path, target) {
			if cerr := utils.CopyFile(path, target); cerr != nil {
				missing = append(missing, filepath.ToSlash(relPath))
				// Python continue：仍登记 harvested
			}
		}
		harvested = append(harvested, filepath.ToSlash(relPath))
		return nil
	})
	sort.Strings(harvested)
	sort.Strings(missing)
	return harvested, missing
}

// copyArtifactTrees 多来源拷贝（Python: _copy_artifact_trees）。
func copyArtifactTrees(sources []string, destDir string) ([]string, []string) {
	var harvested, missing []string
	for _, src := range sources {
		copied, missed := copyArtifactTree(src, destDir)
		harvested = append(harvested, copied...)
		missing = append(missing, missed...)
	}
	return harvested, missing
}

// copyExpectedArtifactsFromSources 按预期清单跨来源拷贝（Python: _copy_expected_artifacts_from_sources）。
func copyExpectedArtifactsFromSources(sources []string, destDir string, expectedFiles []string) ([]string, []string) {
	var harvested, missing []string
	for _, filename := range expectedFiles {
		source := findNamedFileInSources(sources, filename)
		if source == "" {
			missing = append(missing, filename)
			continue
		}
		target := filepath.Join(destDir, filename)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		if err := utils.CopyFile(source, target); err != nil {
			missing = append(missing, filename)
			continue
		}
		harvested = append(harvested, filename)
	}
	return harvested, missing
}

// copyExpectedArtifacts 只拷贝声明的交付物（Python: _copy_expected_artifacts）。
func copyExpectedArtifacts(src, destDir string, expectedFiles []string) ([]string, []string) {
	var harvested, missing []string
	for _, filename := range expectedFiles {
		source := findNamedFile(src, filename)
		if source == "" {
			missing = append(missing, filename)
			continue
		}
		target := filepath.Join(destDir, filename)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		if err := utils.CopyFile(source, target); err != nil {
			missing = append(missing, filename)
			continue
		}
		harvested = append(harvested, filename)
	}
	return harvested, missing
}

// copyProofArtifacts 拷贝证据类产物（Python: _copy_proof_artifacts）。
func copyProofArtifacts(sources []string, destDir string) []string {
	var harvested []string
	for _, src := range sources {
		_ = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			relPath, rerr := filepath.Rel(src, path)
			if rerr != nil {
				return nil
			}
			if !isProofArtifactPath(relPath) {
				return nil
			}
			target := filepath.Join(destDir, relPath)
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			if shouldCopyArtifact(path, target) {
				if utils.CopyFile(path, target) == nil {
					harvested = append(harvested, filepath.ToSlash(relPath))
				}
			}
			return nil
		})
	}
	return harvested
}

// filterExpectedArtifacts 校验预期交付物（Python: _filter_expected_artifacts）。
func filterExpectedArtifacts(destDir string, expectedFiles []string) []string {
	var missing []string
	for _, filename := range expectedFiles {
		rootTarget := filepath.Join(destDir, filename)
		source := findNamedFile(destDir, filename)
		if source == "" {
			missing = append(missing, filename)
			continue
		}
		if !samePath(source, rootTarget) {
			_ = os.MkdirAll(filepath.Dir(rootTarget), 0o755)
			_ = utils.CopyFile(source, rootTarget)
		}
	}
	expectedSet := map[string]bool{}
	for _, filename := range expectedFiles {
		expectedSet[filename] = true
	}
	// 递归最深优先删除非根的同名预期文件
	var files []string
	_ = filepath.WalkDir(destDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, path := range files {
		relPath, rerr := filepath.Rel(destDir, path)
		if rerr != nil {
			continue
		}
		relSlash := filepath.ToSlash(relPath)
		if expectedSet[filepath.Base(path)] && relSlash != filepath.Base(path) {
			_ = os.Remove(path)
		}
	}
	// 自底向上删除空目录
	removeEmptyDirs(destDir)
	return missing
}

// normalizeExpectedFiles 预期清单归一（Python: _normalize_expected_files）。
func normalizeExpectedFiles(expectedFiles []string) []string {
	var result []string
	for _, item := range expectedFiles {
		filename := filepath.Base(filepath.FromSlash(item))
		if filename != "" && !containsStr(result, filename) {
			result = append(result, filename)
		}
	}
	return result
}

// findNamedFile 根目录内找命名文件（Python: _find_named_file——质量键取最优）。
func findNamedFile(root, filename string) string {
	var candidates []string
	direct := filepath.Join(root, filename)
	if pathIsFile(direct) {
		candidates = append(candidates, direct)
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(path) == filename && path != direct {
			candidates = append(candidates, path)
		}
		return nil
	})
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if artifactQualityKeyLess(best, candidate) {
			best = candidate
		}
	}
	return best
}

// findNamedFileInSources 跨来源找命名文件（Python: _find_named_file_in_sources）。
func findNamedFileInSources(sources []string, filename string) string {
	var candidates []string
	for _, src := range sources {
		if found := findNamedFile(src, filename); found != "" {
			candidates = append(candidates, found)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if artifactQualityKeyLess(best, candidate) {
			best = candidate
		}
	}
	return best
}

// isProofArtifactPath 证据类产物判定（Python: _is_proof_artifact_path）。
func isProofArtifactPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if !proofArtifactSuffixes[ext] {
		return false
	}
	var parts []string
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		parts = append(parts, strings.ToLower(strings.ReplaceAll(part, "-", "_")))
	}
	normalized := strings.Join(parts, "_")
	for _, hint := range proofArtifactNameHints {
		if strings.Contains(normalized, hint) {
			return true
		}
	}
	return false
}

// normalizeDeliverableArtifacts 交付物提升到根（Python: _normalize_deliverable_artifacts）。
func normalizeDeliverableArtifacts(destDir string) {
	if !pathIsDir(destDir) {
		return
	}
	for _, filename := range canonicalDeliverableFiles {
		rootTarget := filepath.Join(destDir, filename)
		if pathIsFile(rootTarget) {
			continue
		}
		source := findNestedArtifact(destDir, filename)
		if source == "" {
			continue
		}
		_ = os.MkdirAll(filepath.Dir(rootTarget), 0o755)
		if shouldCopyArtifact(source, rootTarget) {
			_ = utils.CopyFile(source, rootTarget)
		}
	}
}

// findNestedArtifact 找嵌套交付物（Python: _find_nested_artifact）。
func findNestedArtifact(destDir, filename string) string {
	var candidates []string
	_ = filepath.WalkDir(destDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(path) == filename && filepath.Dir(path) != destDir {
			candidates = append(candidates, path)
		}
		return nil
	})
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if artifactQualityKeyLess(best, candidate) {
			best = candidate
		}
	}
	return best
}

// shouldCopyArtifact 是否覆盖目标（Python: _should_copy_artifact）。
func shouldCopyArtifact(source, target string) bool {
	if !pathIsFile(target) {
		return true
	}
	if containsStr(canonicalDeliverableFiles, filepath.Base(source)) {
		// Python: source 质量严格大于 target 才覆盖
		return artifactQualityKeyLess(target, source) && !artifactQualityKeyLess(source, target)
	}
	return true
}

// artifactQualityKeyLess 质量键小于比较（Python _artifact_quality_key 元组比较；
// max 语义经 less 表达：取首个最大值遇平局保先者）。
func artifactQualityKeyLess(a, b string) bool {
	ka := artifactQualityKey(a)
	kb := artifactQualityKey(b)
	for i := range ka {
		if ka[i] != kb[i] {
			return ka[i] < kb[i]
		}
	}
	return false
}

// artifactQualityKey 产物质量键（Python: _artifact_quality_key）。
func artifactQualityKey(path string) [3]int {
	info, err := os.Stat(path)
	if err != nil {
		return [3]int{0, 0, 0}
	}
	size := int(info.Size())
	if filepath.Base(path) == "index.html" {
		raw, rerr := os.ReadFile(path)
		text := ""
		if rerr == nil {
			text = strings.ToLower(decodeIgnoreUTF8(raw))
		}
		complete := 0
		if strings.Contains(text, "</body>") && strings.Contains(text, "</html>") {
			complete = 1
		}
		return [3]int{complete, strings.Count(text, "<section"), size}
	}
	nonZero := 0
	if size > 0 {
		nonZero = 1
	}
	return [3]int{nonZero, 0, size}
}

// skipHarvestPath 收卷跳过判定（Python: _skip_harvest_path）。
func skipHarvestPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return true
	}
	if isRuntimeWorkspaceMetadata(path) {
		return true
	}
	first := map[string]bool{
		".git": true, ".agent_teams": true, "__pycache__": true, ".pytest_cache": true,
		".mypy_cache": true, ".ruff_cache": true, "context": true, "memory": true,
		"messages": true, "todo": true,
	}
	if first[parts[0]] {
		return true
	}
	mid := map[string]bool{"node_modules": true, ".venv": true, "venv": true}
	for _, part := range parts {
		if mid[part] {
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pyc", ".pyo", ".log":
		return true
	}
	return false
}

// cleanupScratch 草稿清理（Python: _cleanup_scratch）。
func cleanupScratch(caseDir string, runtimeHomeDir string) {
	var scratchRoots []string
	if runtimeHomeDir != "" {
		scratchRoots = append(scratchRoots, runtimeHomeDir)
	}
	if runtimeHomeDir == "" || !samePath(runtimeHomeDir, caseDir) {
		scratchRoots = append(scratchRoots, caseDir)
	}
	for _, scratchRoot := range scratchRoots {
		agentTeamsDir := filepath.Join(scratchRoot, ".agent_teams")
		if pathExists(agentTeamsDir) {
			_ = os.RemoveAll(agentTeamsDir)
			logger.Info(logComponentCR).Msgf(".agent_teams scratch removed: %s", agentTeamsDir)
		}
	}
	if runtimeHomeDir != "" && !samePath(runtimeHomeDir, caseDir) {
		_ = os.RemoveAll(runtimeHomeDir)
		logger.Info(logComponentCR).Msgf("runtime home scratch removed: %s", runtimeHomeDir)
	}
	legacyWorkspaceDir := filepath.Join(caseDir, "workspace")
	if pathExists(legacyWorkspaceDir) {
		_ = os.RemoveAll(legacyWorkspaceDir)
		logger.Info(logComponentCR).Msgf("legacy workspace scratch removed: %s", legacyWorkspaceDir)
	}
	for _, suffix := range []string{"", "-shm", "-wal"} {
		dbPath := filepath.Join(caseDir, "team.db"+suffix)
		if !pathExists(dbPath) {
			continue
		}
		if err := os.Remove(dbPath); err != nil {
			logger.Warn(logComponentCR).Msgf("failed to remove %s: %v", filepath.Base(dbPath), err)
		} else {
			logger.Debug(logComponentCR).Msgf("team.db scratch removed: %s", dbPath)
		}
	}
}

// ──────────────────────────── 文件系统小助手 ────────────────────────────

// pathExists 存在（任意类型）。
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// pathIsDir 目录存在语义。
func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// pathIsFile 常规文件存在语义。
func pathIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// removeEmptyDirs 自底向上删除空目录。
func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		entries, rerr := os.ReadDir(dir)
		if rerr == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
}
