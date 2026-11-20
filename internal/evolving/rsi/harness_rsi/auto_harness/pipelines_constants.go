// Pipeline 常量前置件（17.4e2 方案A）。
//
// schema.py 直接消费 pipelines 包的流水线名常量与 normalize_pipeline_name，
// 故将 pipelines/__init__.py 的四个符号前置到本文件；完整的流水线定义
// （base.py 的 PipelineStageMap/BasePipeline 与 extended_evolve/meta_evolve
// 两条流水线）留待 17.4e4 装配层实施（⤴️e4 回填）。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/auto_harness/pipelines/__init__.py
package autoharness

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MetaEvolvePipeline meta 演进流水线名
	MetaEvolvePipeline = "meta_evolve_pipeline"
	// ExtendedEvolvePipeline extended 演进流水线名
	ExtendedEvolvePipeline = "extended_evolve_pipeline"
)

// pipelineNameAliases 用户侧流水线名别名表
var pipelineNameAliases = map[string]string{
	"pr_pipeline":               MetaEvolvePipeline,
	"extended_harness_pipeline": ExtendedEvolvePipeline,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizePipelineName 将旧流水线名归一化为当前内置名；未知名原样返回。
//
// 对齐 Python normalize_pipeline_name：
//
//	def normalize_pipeline_name(name: str) -> str:
//	    return _PIPELINE_NAME_ALIASES.get(name, name)
func NormalizePipelineName(name string) string {
	if aliased, ok := pipelineNameAliases[name]; ok {
		return aliased
	}
	return name
}
