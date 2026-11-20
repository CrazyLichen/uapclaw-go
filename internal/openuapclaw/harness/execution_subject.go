package harness

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// ExecutionSubject 一个具体 agent 执行的稳定身份与谱系。
//
// Python: harness/execution_subject.py:14-22（frozen dataclass，
// subject_id/display_name/kind 必填，parent_subject_id/session_id 可空）
type ExecutionSubject struct {
	// SubjectID 主体标识
	SubjectID string
	// DisplayName 展示名
	DisplayName string
	// Kind 主体类别
	Kind string
	// ParentSubjectID 父主体（可空）
	ParentSubjectID string
	// SessionID 归属会话（可空）
	SessionID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// executionSubjectKeyType ctx 注入键
// Python: execution_subject.py:24-27（ContextVar）
// Python 侧无对应实现：Go 以 context.Context 值注入替代 contextvars
type executionSubjectKeyType struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithExecutionSubject 把执行主体绑定到 ctx，调用方以 defer 清理承担
// Python execution_subject_scope 的 try/finally 语义（Python 侧无独立 set 函数）
func WithExecutionSubject(ctx context.Context, subject ExecutionSubject) context.Context {
	return context.WithValue(ctx, executionSubjectKeyType{}, &subject)
}

// ExecutionSubjectFromCtx 返回当前执行主体，未绑定返回 nil
// Python: execution_subject.py:31-33 current_execution_subject()
func ExecutionSubjectFromCtx(ctx context.Context) *ExecutionSubject {
	subject, _ := ctx.Value(executionSubjectKeyType{}).(*ExecutionSubject)
	return subject
}
