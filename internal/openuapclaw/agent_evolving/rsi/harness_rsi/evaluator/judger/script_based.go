// script_based.go 与基准无关的脚本结果判分器。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/script_based.py（159 行全量对齐）。
// 基准包应自带 EvaluationJudger 实现，而非向 RSI 添加基准运行时；
// 容器搭建、验证器调用与基建失败归类属基准自有的 backend/judger 对。

package judger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ScriptBasedJudger 对后端结果打分或与显式参考答案比对。
// Python: script_based.py:30 ScriptBasedJudger(EvaluationJudger)。
type ScriptBasedJudger struct{}

var _ EvaluationJudger = (*ScriptBasedJudger)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// MethodName 方法标识（Python 类属性 method = "script_based"）。
func (j *ScriptBasedJudger) MethodName() string { return "script_based" }

// ValidateCase 校验用例判分适配面。
// Python: script_based.py:38 validate_case。
func (j *ScriptBasedJudger) ValidateCase(caseMapping map[string]any) error {
	reference, _ := caseMapping["reference"].(map[string]any)
	if reference == nil {
		reference = map[string]any{}
	}
	if _, hasRubric := reference["rubric"]; hasRubric {
		return evalerrors.NewEvaluationInfrastructureError(
			"script_based does not evaluate reference.rubric; configure a rubric-capable judger")
	}
	if swebench, ok := caseMapping["swebench"].(map[string]any); ok {
		path := stringOr(swebench["official_dataset_path"], "")
		if !fileExists(path) {
			return evalerrors.NewEvaluationInfrastructureError("SWE-bench official_dataset_path is missing")
		}
		return nil
	}
	if _, hasFiles := reference["files"]; hasFiles {
		return evalerrors.NewEvaluationInfrastructureError("reference.files has no supported verifier adapter")
	}
	if ReferenceAnswer(caseMapping) == nil {
		return evalerrors.NewEvaluationInfrastructureError(
			"script_based cannot score this case: no backend JudgeResult or reference answer. " +
				"Successful execution is not evidence of correctness.")
	}
	return nil
}

// Judge 打分入口：后端判分直通 → swebench 官方判分 → 参考答案比对。
// Python: script_based.py:60 judge。
func (j *ScriptBasedJudger) Judge(_ context.Context, caseMapping map[string]any,
	executionResult CaseExecutionView, outputDir string, _ BaselineContextView) (*JudgeResult, error) {
	// Python: if execution_result.judge_result is not None: return execution_result.judge_result
	if judge := executionResult.JudgeResultView(); judge != nil {
		return judge, nil
	}
	if _, ok := caseMapping["swebench"].(map[string]any); ok {
		if executionResult.ExecutionStatusView() != "passed" {
			return failureResult(j.MethodName(), executionResult.ErrorView()), nil
		}
		return judgeSwebench(caseMapping, executionResult, outputDir)
	}
	if err := j.ValidateCase(caseMapping); err != nil {
		return nil, err
	}
	expected := ReferenceAnswer(caseMapping)
	if executionResult.ExecutionStatusView() != "passed" {
		return failureResult(j.MethodName(), executionResult.ErrorView()), nil
	}
	passed := ComparableResponse(executionResult.ResponseView(), expected) == expected
	reason := ""
	if !passed {
		reason = "response does not match reference"
	}
	return &JudgeResult{
		Method:   j.MethodName(),
		Score:    passedScore(passed),
		Passed:   passed,
		Reason:   reason,
		Metadata: map[string]any{"rule_engine_status": "reference_comparison"},
	}, nil
}

// FailureResult 执行失败的规范化 0 分结果（Python _failure_result 经基类）。
func (j *ScriptBasedJudger) FailureResult(errText string) *JudgeResult {
	return failureResult(j.MethodName(), errText)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// judgeSwebench SWE-bench 官方判分（swebench 分支真身）。
// Python: script_based.py:95 _judge_swebench。
func judgeSwebench(caseMapping map[string]any, executionResult CaseExecutionView,
	outputDir string) (*JudgeResult, error) {
	workspaceDir, err := filepath.Abs(executionResult.WorkspaceDirView())
	if err != nil {
		return nil, err
	}
	modelPatch, err := loadSwebenchModelPatch(executionResult, workspaceDir)
	if err != nil {
		// Python: except SWEbenchInfrastructureError: raise；其余异常 0 分兜底。
		var infra *evalerrors.EvaluationInfrastructureError
		if errors.As(err, &infra) {
			return nil, err
		}
		return &JudgeResult{
			Method:   "swebench_official",
			Score:    0.0,
			Passed:   false,
			Reason:   err.Error(),
			Metadata: map[string]any{"model_patch_chars": 0},
		}, nil
	}
	// Python: patch_path = output_dir / "verifier" / "model.patch"
	patchPath := filepath.Join(outputDir, "verifier", "model.patch")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o755); err != nil {
		// Python: except Exception → 0 分兜底。
		return swebenchZeroScore(err.Error()), nil
	}
	if err := os.WriteFile(patchPath, []byte(modelPatch), 0o644); err != nil {
		return swebenchZeroScore(err.Error()), nil
	}
	if strings.TrimSpace(modelPatch) == "" {
		return &JudgeResult{
			Method: "swebench_official",
			Score:  0.0,
			Passed: false,
			Reason: "official SWE-bench evaluation received an empty model patch",
			Metadata: map[string]any{
				"model_patch_path":  patchPath,
				"model_patch_chars": 0,
				"empty_patch":       true,
			},
		}, nil
	}
	result, evalErr := callRunOfficialSwebenchEvaluation(caseMapping, modelPatch, outputDir)
	if evalErr != nil {
		// Python: except SWEbenchInfrastructureError: raise
		var infra *evalerrors.EvaluationInfrastructureError
		if errors.As(evalErr, &infra) {
			return nil, evalErr
		}
		// Python: except Exception → 0 分兜底。
		return &JudgeResult{
			Method:   "swebench_official",
			Score:    0.0,
			Passed:   false,
			Reason:   evalErr.Error(),
			Metadata: map[string]any{"model_patch_chars": 0},
		}, nil
	}
	metadata := cloneMap(result)
	delete(metadata, "passed")
	delete(metadata, "score")
	delete(metadata, "reason")
	metadata["model_patch_path"] = patchPath
	metadata["model_patch_chars"] = len(modelPatch)
	score := toFloat(result["score"])
	passed, _ := result["passed"].(bool)
	reason, _ := result["reason"].(string)
	return &JudgeResult{
		Method:   "swebench_official",
		Score:    score,
		Passed:   passed,
		Reason:   reason,
		Metadata: metadata,
	}, nil
}

// loadSwebenchModelPatch 载入 Linux 侧捕获的 patch，缺省回退宿主 checkout。
// Python: script_based.py:142 _load_swebench_model_patch。
func loadSwebenchModelPatch(executionResult CaseExecutionView, workspaceDir string) (string, error) {
	capturedPath := ""
	if metadata := judgeResultMetadata(executionResult); metadata != nil {
		if raw, ok := metadata["swebench_model_patch_path"].(string); ok {
			capturedPath = strings.TrimSpace(raw)
		}
	}
	if capturedPath == "" {
		return callCollectModelPatch(workspaceDir)
	}
	resolved, err := filepath.Abs(capturedPath)
	if err != nil {
		return "", evalerrors.NewEvaluationInfrastructureError(
			fmt.Sprintf("failed to read captured SWE-bench patch: %s: %v", capturedPath, err))
	}
	raw, readErr := os.ReadFile(resolved)
	if readErr != nil {
		return "", evalerrors.NewEvaluationInfrastructureError(
			fmt.Sprintf("failed to read captured SWE-bench patch: %s: %v", capturedPath, readErr))
	}
	return string(raw), nil
}

// judgeResultMetadata 从执行结果视图取 metadata（CaseExecutionView 不承载 metadata，
// 经可选接口嗅探；Python metadata 字段属 CaseExecutionResult）。
func judgeResultMetadata(executionResult CaseExecutionView) map[string]any {
	type metadataView interface{ MetadataView() map[string]any }
	if m, ok := executionResult.(metadataView); ok {
		return m.MetadataView()
	}
	return nil
}

// swebenchZeroScore SWE-bench 判分链非基建异常的 0 分兜底结果。
// Python: script_based.py except Exception 分支。
func swebenchZeroScore(reason string) *JudgeResult {
	return &JudgeResult{
		Method:   "swebench_official",
		Score:    0.0,
		Passed:   false,
		Reason:   reason,
		Metadata: map[string]any{"model_patch_chars": 0},
	}
}
