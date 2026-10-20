package chunker

import (
	"strings"
	"unicode"

	"github.com/dlclark/regexp2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextPreprocessor 文本预处理器抽象接口。
//
// Python __call__ 语法糖由 Go 方法调用天然替代，不单列。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/text_preprocessor.py (TextPreprocessor)
type TextPreprocessor interface {
	// Process 处理输入文本并返回清洗结果。
	Process(text string) string
}

// CharReplacement 单字符替换规则。
//
// 对齐 Python chars_to_replace dict 单项。有序切片替代 dict：
// Python dict 按插入序迭代，链式替换（a→b, b→c 需 a 变 c）依赖顺序。
type CharReplacement struct {
	// Old 被替换字符
	Old string
	// New 替换为的字符
	New string
}

// WhitespaceNormalizer 空白归一化器：连续空白序列 → 单空格 + 两端去除。
//
// Python: text_preprocessor.py (WhitespaceNormalizer)
type WhitespaceNormalizer struct{}

// URLEmailRemover URL/邮箱移除器。
//
// Python: text_preprocessor.py (URLEmailRemover)
type URLEmailRemover struct {
	// RemoveURLs 是否移除 URL（默认 true 语义由构造方保证）
	RemoveURLs bool
	// RemoveEmails 是否移除邮箱
	RemoveEmails bool
	// Replacement 替换字符串（默认 ""）
	Replacement string
}

// SpecialCharacterNormalizer 特殊字符归一化器。
//
// Python: text_preprocessor.py (SpecialCharacterNormalizer)
type SpecialCharacterNormalizer struct {
	// CharsToRemove 待移除字符集（字符串形式，空串表示不移除）
	CharsToRemove string
	// CharsToReplace 有序替换规则（nil 表示无替换）
	CharsToReplace []CharReplacement
}

// PreprocessingPipeline 预处理管线：顺序应用预处理器列表。
//
// Python: text_preprocessor.py (PreprocessingPipeline)
type PreprocessingPipeline struct {
	// preprocessors 按序应用的预处理器
	preprocessors []TextPreprocessor
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// pyWordClassStr Python \w 精确集。
	//
	// Python \w = [a-zA-Z0-9_] + Unicode 词字符，实测含 No/Nl 类
	//（①②③/½/Ⅷ 是词字符）；regexp2(.NET) \w 含 Mn/Pc 但缺 No/Nl——
	// 若用原生 \w，"①②③序号" 会被 REDUNDANT_SYMBOLS 误删（Python 保留）。
	// 显式集消除该差异（D1' 决策）。
	pyWordClassStr = `[\p{L}\p{Nd}\p{Nl}\p{No}_]`
	// pySpaceClassStr Python \s 补偿集。
	//
	// .NET \s 与 Python \s 在 U+0085/U+00A0/U+1680/U+2028/U+3000 等
	// 已一致，唯缺文件分隔符组 \x1c-\x1f，显式补齐（D1' 决策）。
	pySpaceClassStr = `[\s\x{1c}-\x{1f}]`
)

// wCore/sCore 语义集"内芯"（去掉外层方括号），供字符类拼接。
const (
	wCore = `\p{L}\p{Nd}\p{Nl}\p{No}_`
	sCore = `\s\x{1c}-\x{1f}`
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pySpaceSeqPattern Python r"\s+"：连续空白 → 单空格。
	//
	// Python: text_preprocessor.py WhitespaceNormalizer.process L53
	pySpaceSeqPattern = regexp2.MustCompile(pySpaceClassStr+`+`, regexp2.None)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Process 归一化空白：连续 pySpace 序列替换为单空格，两端去除。
//
// 对齐 Python：if not text → 原样；re.sub(r"\s+", " ", text)；text.strip()。
// Python text.strip() 去除的字符集与 \s 一致，故两端 trim 用同一集合。
//
// Python: text_preprocessor.py WhitespaceNormalizer.process
func (n WhitespaceNormalizer) Process(text string) string {
	if text == "" {
		return text
	}
	out, err := pySpaceSeqPattern.Replace(text, " ", 0, -1)
	if err != nil {
		// regexp2 替换纯文本无失败路径（模式编译期已校验），防御性原样返回
		return text
	}
	return strings.TrimFunc(out, isPySpaceRune)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isPySpaceRune 判定 rune 是否属于 Python \s 集合（regexp2 \s + \x1c-\x1f）。
//
// 供 WhitespaceNormalizer 两端 trim 使用（与 pySpaceSeqPattern 同一语义）。
// unicode.IsSpace 与 Python \s 在 \x1c-\x1f 上不同（Go 不含、Python 含），
// 显式补齐后两者集合一致。
func isPySpaceRune(r rune) bool {
	switch {
	case r == 0x1c || r == 0x1d || r == 0x1e || r == 0x1f:
		return true // 补偿集：unicode.IsSpace 缺文件分隔符组
	default:
		return unicode.IsSpace(r)
	}
}
