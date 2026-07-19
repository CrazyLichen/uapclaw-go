package tool_call

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// DefaultConfigEg Example Stage 默认配置。
//
// 对齐 Python: default_config_eg
var DefaultConfigEg = map[string]any{
	"gen_model_id":       "gpt-5-mini",
	"eval_model_id":      "gpt-5-mini",
	"verbose":            1,
	"num_init_loop":      1,
	"num_refine_steps":   1,
	"num_feedback_steps": 2,
	"score_eval_weight":  0.0,
	"beam_width":         2,
	"expand_num":         3,
	"max_depth":          2,
	"num_workers":        2,
	"top_k":              5,
}

// DefaultConfigDesc Description Stage 默认配置。
//
// 对齐 Python: default_config_desc
var DefaultConfigDesc = map[string]any{
	"gen_model_id":          "gpt-5-mini",
	"eval_model_id":         "gpt-5-mini",
	"verbose":               1,
	"num_init_loop":         1,
	"num_feedback_steps":    2,
	"score_eval_weight":     0.0,
	"num_examples_for_desc": 4,
	"beam_width":            2,
	"expand_num":            2,
	"max_depth":             2,
	"num_workers":           2,
	"top_k":                 3,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
