// Package rulecompression 提供消息级确定性规则压缩（offloader 的规则压缩半区）。
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/offloader/rule_compression/
//
// 文件目录：
//
//	rule_compression/
//	├── doc.go          # 包文档
//	├── types.go       # ContentType/RuleContext/RuleCompressionResult
//	├── common.go      # 计数/阈值/文件路径判定/显示前缀剥离
//	└── query_terms.go # 相关性词项抽取
package rulecompression

// ──────────────────────────── 枚举 ────────────────────────────

// ContentType 内容类型枚举（BUILD_OUTPUT 复用 LOG 值，一比一）。
// Python: types.py:12 (ContentType(str, Enum))
type ContentType string

const (
	// ContentTypeJSONArray JSON 数组
	ContentTypeJSONArray ContentType = "JSON_ARRAY"
	// ContentTypeGitDiff git diff
	ContentTypeGitDiff ContentType = "GIT_DIFF"
	// ContentTypeHTML HTML 文档
	ContentTypeHTML ContentType = "HTML"
	// ContentTypeSearchResults 搜索结果行
	ContentTypeSearchResults ContentType = "SEARCH_RESULTS"
	// ContentTypeLog 日志/构建输出（Python BUILD_OUTPUT 同值 "LOG"）
	ContentTypeLog ContentType = "LOG"
	// ContentTypePlainText 纯文本
	ContentTypePlainText ContentType = "PLAIN_TEXT"
	// ContentTypeSourceFile 按路径读的源码/标记/配置/文档——永不规则压缩。
	// Python: types.py:19 (SOURCE_FILE docstring)
	ContentTypeSourceFile ContentType = "SOURCE_FILE"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuleContext 规则压缩上下文（预算与阈值集合）。
// Python: types.py:25 (RuleContext, @dataclass(frozen=True))
type RuleContext struct {
	// MaxTokens 压缩预算
	MaxTokens int
	// HeadTokens 头部保留
	HeadTokens int
	// TailTokens 尾部保留
	TailTokens int
	// CountTokens 自定义计数（nil 用 len//3 兜底）
	CountTokens func(string) int
	// MinSavingsRatio 最小节省比例
	MinSavingsRatio float64
	// JSONCSVMinDensity JSON/CSV 最小密度
	JSONCSVMinDensity float64
	// QueryTerms 相关性词项
	QueryTerms map[string]bool
	// ToolName 工具名
	ToolName string
	// FilePath 工具参数点名的文件路径（按文件类型路由文件读取）
	FilePath string
	// SearchMaxMatchesPerFile 搜索压缩参数族
	SearchMaxMatchesPerFile int
	// SearchMaxTotalMatches 搜索总匹配上限
	SearchMaxTotalMatches int
	// SearchMaxFiles 搜索文件数上限
	SearchMaxFiles int
	// DiffMinLines diff 最小行数
	DiffMinLines int
	// DiffMaxContextLines diff 上下文行上限
	DiffMaxContextLines int
	// DiffMaxChangedLinesPerHunk 单 hunk 变更行上限
	DiffMaxChangedLinesPerHunk int
	// DiffMaxChangedLinesTotal 变更行总数上限
	DiffMaxChangedLinesTotal int
	// DiffMaxHunksPerFile 单文件 hunk 上限
	DiffMaxHunksPerFile int
	// DiffMaxFiles diff 文件数上限
	DiffMaxFiles int
	// HTMLMinContentChars HTML 最小内容字符
	HTMLMinContentChars int
	// LogMinLines 日志最小行数
	LogMinLines int
	// LogErrorContextLines 错误上下文行
	LogErrorContextLines int
	// LogMaxErrors 错误条数上限
	LogMaxErrors int
	// LogMaxWarnings 警告条数上限
	LogMaxWarnings int
	// LogMaxStackTraces 栈回溯上限
	LogMaxStackTraces int
	// LogStackTraceMaxLines 单栈回溯行上限
	LogStackTraceMaxLines int
	// LogMaxTotalLines 日志总行上限
	LogMaxTotalLines int
}

// NewRuleContext 创建 RuleContext，字段使用 Python dataclass 默认值。
func NewRuleContext(maxTokens int) *RuleContext {
	return &RuleContext{
		MaxTokens:                  maxTokens,
		HeadTokens:                 2000,
		TailTokens:                 2000,
		MinSavingsRatio:            0.1,
		JSONCSVMinDensity:          0.8,
		SearchMaxMatchesPerFile:    5,
		SearchMaxTotalMatches:      30,
		SearchMaxFiles:             15,
		DiffMinLines:               50,
		DiffMaxContextLines:        2,
		DiffMaxChangedLinesPerHunk: 8,
		DiffMaxChangedLinesTotal:   200,
		DiffMaxHunksPerFile:        10,
		DiffMaxFiles:               20,
		HTMLMinContentChars:        100,
		LogMinLines:                50,
		LogErrorContextLines:       3,
		LogMaxErrors:               10,
		LogMaxWarnings:             5,
		LogMaxStackTraces:          3,
		LogStackTraceMaxLines:      20,
		LogMaxTotalLines:           100,
	}
}

// RuleCompressionResult 规则压缩结果。
// Python: types.py:71 (RuleCompressionResult, @dataclass(frozen=True))
type RuleCompressionResult struct {
	// Content 压缩后内容
	Content string
	// ContentType 内容类型
	ContentType ContentType
	// Modified 是否发生修改
	Modified bool
	// Lossy 是否有损
	Lossy bool
	// Details 压缩明细（可 nil）
	Details map[string]any
}
