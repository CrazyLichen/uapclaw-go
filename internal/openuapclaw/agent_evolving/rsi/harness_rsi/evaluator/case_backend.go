// case_backend.go 用例执行后端——考场搭建与答卷收集。
// Python: openjiuwen/rsi/harness_rsi/evaluator/case_backend.py（836 行）。
// 17.6b（plan Task 7）补齐考场主流程；本提交仅为 Task 4 前置切片：
// CaseExecutionResult 数据契约（Python case_backend.py:64 dataclass）。

package evaluator

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CaseExecutionResult 用例执行后端返回的结果。
// Python: case_backend.py:64 CaseExecutionResult（dataclass）。
type CaseExecutionResult struct {
	// Response agent 的实际回答（Python response: Any）
	Response any
	// ExecutionStatus 执行状态（"passed" / 失败态）
	ExecutionStatus string
	// Error 执行错误描述（缺省空串）
	Error string
	// JudgeResult 判分结果（缺省 nil）
	JudgeResult *judger.JudgeResult
	// WorkspaceDir 执行工作区路径（缺省空串）
	WorkspaceDir string
	// Metadata 附加元数据
	Metadata map[string]any
}

// ResponseView 响应视图（judger.CaseExecutionView 反转缝实现）。
func (r *CaseExecutionResult) ResponseView() any { return r.Response }

// ExecutionStatusView 执行状态视图。
func (r *CaseExecutionResult) ExecutionStatusView() string { return r.ExecutionStatus }

// ErrorView 错误视图。
func (r *CaseExecutionResult) ErrorView() string { return r.Error }

// WorkspaceDirView 工作区路径视图。
func (r *CaseExecutionResult) WorkspaceDirView() string { return r.WorkspaceDir }

// JudgeResultView 判分结果视图。
func (r *CaseExecutionResult) JudgeResultView() *judger.JudgeResult { return r.JudgeResult }

var _ judger.CaseExecutionView = (*CaseExecutionResult)(nil)
