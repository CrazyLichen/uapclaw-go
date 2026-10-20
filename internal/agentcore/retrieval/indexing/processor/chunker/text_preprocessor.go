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
	// urlPattern Python URL_PATTERN r"https?://\S+|www\.\S+"。
	//
	// \S → 非 pySpace 集（D1' 决策：显式语义集替换）。
	//
	// Python: text_preprocessor.py URLEmailRemover.URL_PATTERN L66
	urlPattern = regexp2.MustCompile(`https?://[^\s\x{1c}-\x{1f}]+|www\.[^\s\x{1c}-\x{1f}]+`, regexp2.None)
	// comPattern Python COM_PATTERN
	// r"(?:https?://|www\.)?\S+?\.(?:com|net|org|cn)(?:[/?#]\S*)?\b"。
	//
	// \S 同上替换；\b 保留 regexp2 原生语义（偏差 4：No/Nl/Mn/Pc 紧贴
	// 场景与 Python \b 判定分叉，Python 测试不覆盖，设计文档标注）。
	//
	// Python: text_preprocessor.py URLEmailRemover.COM_PATTERN L67
	comPattern = regexp2.MustCompile(
		`(?:https?://|www\.)?[^\s\x{1c}-\x{1f}]+?\.(?:com|net|org|cn)(?:[/?#][^\s\x{1c}-\x{1f}]*)?\b`,
		regexp2.None)
	// emailPattern Python EMAIL_PATTERN r"\b[\w\.-]+@[\w\.-]+\.\w+\b"。
	//
	// \w → 显式集（wCore）；\b 原生（偏差 4）。
	// 实测对拍：`联系①user@example.com` Go/Python 同为吞掉 ①（两侧行为一致）。
	// 注意尾部 `\.\w+\b` 的 + 量词不可丢（设计期实测踩坑：遗漏致匹配失败）。
	//
	// Python: text_preprocessor.py URLEmailRemover.EMAIL_PATTERN L69
	emailPattern = regexp2.MustCompile(
		`\b[`+wCore+`\.-]+@[`+wCore+`\.-]+\.[`+wCore+`]+\b`,
		regexp2.None)
	// controlCharPattern Python CONTROL_CHAR_PATTERN r"[\x00-\x1F\x7F]"。
	//
	// Python: text_preprocessor.py SpecialCharacterNormalizer.CONTROL_CHAR_PATTERN L112
	controlCharPattern = regexp2.MustCompile(`[\x00-\x1F\x7F]`, regexp2.None)
	// redundantSymbolsPattern Python REDUNDANT_SYMBOLS_PATTERN
	// r"(?![\u4e00-\u9fa5，。！？；：“”‘’（）【】《》、…·])(?![#*\-_]{2,})([^\w\s#*_\-\|]){2,}"
	//
	// 双负向前瞻 regexp2 原生支持；[^\w\s#*_\-\|] 内的 \w/\s
	// → 显式语义集（wCore+sCore，D1' 决策——①②③ 保留的根因）。
	//
	// Python: text_preprocessor.py SpecialCharacterNormalizer.REDUNDANT_SYMBOLS_PATTERN L113-115
	redundantSymbolsPattern = regexp2.MustCompile(
		`(?![\x{4e00}-\x{9fa5}，。！？；：“”‘’（）【】《》、…·])(?![#*\-_]{2,})([^`+wCore+sCore+`#*_\-\|]){2,}`,
		regexp2.None)
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

// Process 移除 URL 和/或邮箱地址。
//
// 对齐 Python 执行序：if not text → 原样；remove_urls → URL_PATTERN.sub →
// COM_PATTERN.sub；remove_emails → EMAIL_PATTERN.sub。注意 Python 的
// Go 零值差异：Python 构造默认 remove_urls=remove_emails=true，Go 结构体
// 零值为 false——调用方需显式设置（Python 默认语义不重造构造函数）。
//
// Python: text_preprocessor.py URLEmailRemover.process L84-104
func (r URLEmailRemover) Process(text string) string {
	if text == "" {
		return text
	}
	if r.RemoveURLs {
		text = replaceAll(urlPattern, text, r.Replacement)
		text = replaceAll(comPattern, text, r.Replacement)
	}
	if r.RemoveEmails {
		text = replaceAll(emailPattern, text, r.Replacement)
	}
	return text
}

// Process 归一化特殊字符：控制字符删除 → 冗余符号段删除 →
// 有序替换 → 指定字符删除。
//
// 对齐 Python 执行序 L128-154：if not text → 原样；
// CONTROL.sub("") → REDUNDANT.sub("") → chars_to_replace 依插入序
// str.replace → chars_to_remove 构造字符类 sub("")。
//
// Python: text_preprocessor.py SpecialCharacterNormalizer.process
func (n SpecialCharacterNormalizer) Process(text string) string {
	if text == "" {
		return text
	}
	text = replaceAll(controlCharPattern, text, "")
	text = replaceAll(redundantSymbolsPattern, text, "")
	for _, cr := range n.CharsToReplace {
		text = strings.ReplaceAll(text, cr.Old, cr.New)
	}
	if n.CharsToRemove != "" {
		text = removeChars(text, n.CharsToRemove)
	}
	return text
}

// NewPreprocessingPipeline 构造预处理管线。
//
// preprocessors 可为空（空管线 Process 直通）。
//
// Python: text_preprocessor.py PreprocessingPipeline.__init__
func NewPreprocessingPipeline(preprocessors ...TextPreprocessor) *PreprocessingPipeline {
	return &PreprocessingPipeline{preprocessors: preprocessors}
}

// AddPreprocessor 追加预处理器到管线尾部。
//
// Python: text_preprocessor.py PreprocessingPipeline.add_preprocessor
func (p *PreprocessingPipeline) AddPreprocessor(pre TextPreprocessor) {
	p.preprocessors = append(p.preprocessors, pre)
}

// Process 顺序应用全部预处理器。
//
// Python: text_preprocessor.py PreprocessingPipeline.process
func (p *PreprocessingPipeline) Process(text string) string {
	for _, pre := range p.preprocessors {
		text = pre.Process(text)
	}
	return text
}

// Len 返回预处理器数量（对齐 Python __len__）。
//
// Python: text_preprocessor.py PreprocessingPipeline.__len__
func (p *PreprocessingPipeline) Len() int {
	return len(p.preprocessors)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isPySpaceRune 判定 rune 是否属于 Python \s 集合（regexp2 \s + \x1c-\x1f）。
//
// 供 WhitespaceNormalizer 两端 trim 使用（与 pySpaceSeqPattern 同一语义）。
// unicode.IsSpace 与 Python \s 在 \x1c-\x1f 上不同（Go 不含、Python 含），
// 显式补齐后两者集合一致。
func isPySpaceRune(r rune) bool {
	switch r {
	case 0x1c, 0x1d, 0x1e, 0x1f:
		return true // 补偿集：unicode.IsSpace 缺文件分隔符组
	default:
		return unicode.IsSpace(r)
	}
}

// replaceAll regexp2 版 re.sub（全部替换）。Replace 错误为模式编译
// 期已校验的防御路径，发生时原样返回输入。
func replaceAll(re *regexp2.Regexp, text, replacement string) string {
	out, err := re.Replace(text, replacement, 0, -1)
	if err != nil {
		return text
	}
	return out
}

// removeChars 删除文本中出现的指定字符集字符。
//
// 对齐 Python re.sub("[" + re.escape(chars) + "]", "", text)：
// 逐 rune 判定是否属于 chars，直接扫描无需正则（语义等价且无转义陷阱）。
func removeChars(text, chars string) string {
	charSet := make(map[rune]struct{}, len(chars))
	for _, r := range chars {
		charSet[r] = struct{}{}
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if _, ok := charSet[r]; !ok {
			b.WriteRune(r)
		}
	}
	return b.String()
}
