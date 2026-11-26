package checkpointing

// store_options.go —— store 记录操作的 kwarg 承载选项。
// Python: delete_records/mark_records_applied/render_evolution_markdown 的
// subject_kind 关键字参数（store_records.py:326-360 / store_projection.py）。

// ──────────────────────────── 结构体 ────────────────────────────

// recordsOptions 记录操作选项集合。
type recordsOptions struct {
	// subjectKind 主体类型（空串 = 不过滤，Python None 语义）
	subjectKind string
}

// ──────────────────────────── 枚举 ────────────────────────────

// RecordsOption 记录操作选项函数。
type RecordsOption func(*recordsOptions)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithRecordsSubjectKind 指定 subject_kind 过滤。
// Python: delete_records(..., subject_kind=...) 关键字参数。
func WithRecordsSubjectKind(kind string) RecordsOption {
	return func(o *recordsOptions) { o.subjectKind = kind }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordsSubjectKindOf 汇总选项中的 subject_kind（缺省空串）。
func recordsSubjectKindOf(opts []RecordsOption) string {
	o := &recordsOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o.subjectKind
}
