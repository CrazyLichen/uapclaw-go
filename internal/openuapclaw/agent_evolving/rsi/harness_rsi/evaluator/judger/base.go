// Package judger 提供 LLM 评委基础判定。
//
// 文件目录：
//
//	judger/
//	├── base.go              # 判分器接口 + JudgeResult + 共享助手（judger/base.py）
//	├── scoring.go           # 评分契约 + 判决解析 + 重算
//	├── judge_evidence.go    # 证据工作区准备
//	├── judge_runtime.go     # 裁判 DeepAgent 运行时 + 修复链 + closeout
//	├── judge_diagnostics.go # 模型调用诊断采集
//	├── judge_search.go     # 证据检索
//	├── evidence_guard.go   # 证据守护
//	├── direct_evidence.go  # 证据直取
//	├── errors.go           # EvaluationInfrastructureError 反转缝
//	├── fs_helpers.go       # 文件系统助手
//	├── judge_prompt.md     # 裁判提示词（Python 逐字 embed）
//	└── judge_prompt.legacy.md # 旧版提示词（Python 逐字 embed）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/judger/base.py
package judger

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JudgeResult 评估方法返回的结构化分数。
// Python: judger/base.py:18 JudgeResult（frozen dataclass——Go 值语义承载不可变）。
type JudgeResult struct {
	// Method 判分方法标识
	Method string
	// Score 分数
	Score float64
	// Passed 是否通过
	Passed bool
	// Reason 判分理由
	Reason string
	// Metadata 附加元数据
	Metadata map[string]any
}

// CaseExecutionView 用例执行结果的能力视图（依赖反转缝）。
// Python: case_backend.py:64 CaseExecutionResult——真身属父包 evaluator
// （case_backend.go，Task 7 落地）；judger 不可反向 import（父包已依赖本包），
// 字段访问经 View 方法（与 BaselineContextView 同款决策）。
type CaseExecutionView interface {
	// ResponseView agent 的实际回答
	ResponseView() any
	// ExecutionStatusView 执行状态（"passed" / 失败态）
	ExecutionStatusView() string
	// ErrorView 执行错误描述
	ErrorView() string
	// WorkspaceDirView 执行工作区路径（script_based 用）
	WorkspaceDirView() string
	// JudgeResultView 后端已产出的判分结果（无则返回 nil）
	JudgeResultView() *JudgeResult
}

// EvaluationJudger 全部评估方法的抽象基类。
// Python: judger/base.py:29 EvaluationJudger（ABC）。
type EvaluationJudger interface {
	// MethodName 方法标识（Python 类属性 method）
	MethodName() string
	// ValidateCase 可选的判分前校验（Python validate_case 缺省空实现）
	ValidateCase(caseMapping map[string]any) error
	// Judge 对单个用例回答打分（Python judge 关键字参数 case/execution_result/output_dir/baseline_context）
	Judge(ctx context.Context, caseMapping map[string]any, executionResult CaseExecutionView,
		outputDir string, baselineContext BaselineContextView) (*JudgeResult, error)
	// FailureResult 执行失败的规范化 0 分结果（Python _failure_result）
	FailureResult(err string) *JudgeResult
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildJudger 由评估器配置构造具体判分器。
// Python: judger/__init__.py build_judger。
func BuildJudger(cfg *config.EvaluatorConfig) (EvaluationJudger, error) {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(cfg.EvaluationMethod)), "-", "_")
	switch normalized {
	case "exact_match":
		return &ExactMatchJudger{}, nil
	case "llm_as_judge":
		return NewLlmAsJudgeJudger(cfg)
	case "script_based", "rule_based", "", "default", "pass_through":
		return &ScriptBasedJudger{}, nil
	default:
		return nil, fmt.Errorf("unsupported evaluation_method: %s", cfg.EvaluationMethod)
	}
}

// IsExecutionOnlyEvaluation 识别遗留的仅完成态评分——不得作为证据复用
// （Python judger/base.py _is_execution_only_evaluation；导出因跨包使用）。
// ──────────────────────────── 导出函数 ────────────────────────────

// IsExecutionOnlyEvaluation 识别遗留的仅完成态评分——不得作为证据复用
// （Python judger/base.py _is_execution_only_evaluation；导出因跨包使用）。
func IsExecutionOnlyEvaluation(evaluation map[string]any) bool {
	method, _ := evaluation["method"].(string)
	if method == "none" {
		return true
	}
	metadata, ok := evaluation["metadata"].(map[string]any)
	if !ok {
		return false
	}
	status, _ := metadata["rule_engine_status"].(string)
	return status == "backend_completed"
}

// ReferenceAnswer 返回用例参考答案（向后兼容别名逐层回退）。
// Python: judger/base.py:65 _reference_answer（导出因跨包使用）。
func ReferenceAnswer(caseMapping map[string]any) any {
	if reference, ok := caseMapping["reference"].(map[string]any); ok {
		if answer, exists := reference["answer"]; exists {
			return answer
		}
		if expected, exists := reference["expected_output"]; exists {
			return expected
		}
	}
	if answer, exists := caseMapping["reference_answer"]; exists {
		return answer
	}
	if expected, exists := caseMapping["expected_output"]; exists {
		return expected
	}
	return nil
}

// ComparableResponse 仅解包原生最终答案信封，不解包任意任务 JSON。
// Python: judger/base.py:53 _comparable_response（导出因跨包使用）。
func ComparableResponse(response any, expected any) any {
	_, expectedIsText := expected.(string)
	responseMap, responseIsMap := response.(map[string]any)
	if !expectedIsText || !responseIsMap {
		return response
	}
	if resultType, _ := responseMap["result_type"].(string); resultType == "answer" {
		if output, ok := responseMap["output"].(string); ok {
			return output
		}
	}
	return response
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// failureResult 执行失败的规范化 0 分结果。
// Python: judger/base.py:44 EvaluationJudger._failure_result。
func failureResult(method string, err string) *JudgeResult {
	reason := err
	if reason == "" {
		reason = "case execution failed"
	}
	return &JudgeResult{
		Method: method,
		Score:  0.0,
		Passed: false,
		Reason: reason,
	}
}
