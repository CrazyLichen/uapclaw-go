package logger

import (
	"io"
	"regexp"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Sanitizer 敏感数据脱敏器，对日志文本进行 4 层正则 + 7 种模式的脱敏。
// 对应 Python: _sanitize_log_text + SensitiveDataFilter
type Sanitizer struct {
	// kvPattern 匹配常见敏感字段键值对（key=value / key: value）
	// 对应 Python: _KV_SENSITIVE_PATTERN
	kvPattern *regexp.Regexp
	// namedKVPatternDQ 匹配键名包含敏感关键词且值被双引号包裹的场景
	// 对应 Python: _NAMED_SENSITIVE_KV_PATTERN（双引号版本）
	namedKVPatternDQ *regexp.Regexp
	// namedKVPatternSQ 匹配键名包含敏感关键词且值被单引号包裹的场景
	// 对应 Python: _NAMED_SENSITIVE_KV_PATTERN（单引号版本）
	namedKVPatternSQ *regexp.Regexp
	// bearerPattern 匹配 Authorization Bearer 令牌，保留 "Bearer " 前缀
	// 对应 Python: _BEARER_SENSITIVE_PATTERN
	bearerPattern *regexp.Regexp
	// specificPatterns 特定格式的敏感数据模式列表
	// 对应 Python: _SENSITIVE_PATTERNS
	specificPatterns []*regexp.Regexp
}

// sanitizerWriter 包装 io.Writer，写入前对完整 JSON 行进行脱敏。
// 为什么用 Writer 而非 zerolog Hook：zerolog Hook 在 Event 未 finalize 时调用，
// 此时 buf 中 JSON 不完整，正则替换不可靠。Writer.Write() 时 JSON 已完整。
type sanitizerWriter struct {
	underlying io.Writer
	sanitizer  *Sanitizer
}

// ──────────────────────────── 常量 ────────────────────────────

// SensitiveMask 敏感信息统一掩码值。
// 对应 Python: _SENSITIVE_MASK
const SensitiveMask = "******"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSanitizer 创建脱敏器，编译所有正则模式。
// 注意：Go 的 regexp 使用 RE2 引擎，不支持 lookbehind (?<!...)，
// 因此对 Python 原版正则做了改写，使用单词边界 \b 或捕获组替代。
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		// 第一层：匹配 key=value / key: value 格式的敏感键值对
		// 覆盖: password=abc, api_key: sk-xxx, authorization = Bearer ...
		// 注意：Go RE2 不支持 lookbehind，改用 \b 单词边界 + 捕获组实现
		kvPattern: regexp.MustCompile(
			`(?i)\b` +
				`(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?token|` +
				`refresh[_-]?token|authorization|user[_-]?id|userid)` +
				`\b(\s*[:=]\s*)(["']?)(?:[^,\s"'\]\}]+)(["']?)`),

		// 第二层：匹配键名包含敏感关键词且值被引号包裹的场景
		// 覆盖: 'CAT_CAFE_CALLBACK_TOKEN': 'xxxx', "my_private_key"="xxxx"
		// 注意：Go RE2 不支持反向引用 \2，改为分别匹配双引号和单引号两种情况
		namedKVPatternDQ: regexp.MustCompile(
			`(?i)(["']?[A-Za-z0-9_.-]*` +
				`(?:token|secret|password|passwd|pwd|api[_-]?key|authorization|` +
				`credential|private[_-]?key|user[_-]?id|userid)` +
				`[A-Za-z0-9_.-]*["']?\s*[:=]\s*")([^"]*)(")`),
		namedKVPatternSQ: regexp.MustCompile(
			`(?i)(["']?[A-Za-z0-9_.-]*` +
				`(?:token|secret|password|passwd|pwd|api[_-]?key|authorization|` +
				`credential|private[_-]?key|user[_-]?id|userid)` +
				`[A-Za-z0-9_.-]*["']?\s*[:=]\s*')([^']*)(')`),

		// 第三层：匹配 Authorization Bearer 令牌，保留 "Bearer " 前缀，仅掩码令牌值
		bearerPattern: regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9\-._~+/]+=*`),

		// 第四层：特定格式的敏感数据模式
		specificPatterns: []*regexp.Regexp{
			// JWT（header.payload.signature 三段式，常见以 eyJ 开头）
			regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
			// OpenAI 风格 key（sk- 前缀）
			regexp.MustCompile(`\bsk-[A-Za-z0-9]{8,}\b`),
			// GitHub Personal Access Token（ghp_ 前缀）
			regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`),
			// GitLab Personal Access Token（glpat- 前缀）
			regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
			// 邮箱地址（避免日志中泄露个人身份信息）
			regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[A-Za-z]{2,}\b`),
			// 中国大陆手机号（可带 +86 或 86 前缀，支持空格/短横线分隔）
			// Go RE2 不支持 lookbehind，使用单词边界 \b 替代 (?<!\d)
			regexp.MustCompile(`(?:\+?86[-\s]?)?\b1[3-9]\d{9}\b`),
			// 中国身份证号（18 位，最后一位可为 X/x）
			// Go RE2 不支持 lookbehind，使用捕获组保留前后字符
			regexp.MustCompile(`(^|[^\d])(\d{17}[\dXx])($|[^\d])`),
		},
	}
}

// Sanitize 对文本进行脱敏，脱敏失败时返回原文。
// 对应 Python: _sanitize_log_text
// 脱敏失败时静默返回原文，绝不因脱敏异常而阻止日志输出。
func (s *Sanitizer) Sanitize(text string) string {
	if text == "" {
		return text
	}

	// 脱敏异常时返回原文
	defer func() {
		if r := recover(); r != nil {
			// 静默恢复，返回原文（recover 必须在 defer 中使用）
			_ = r
		}
	}()

	masked := text

	// 第一层：KV 键值对脱敏
	masked = s.kvPattern.ReplaceAllString(masked, `${1}${2}${3}`+SensitiveMask+`${4}`)

	// 第二层：命名 KV 脱敏（双引号）
	masked = s.namedKVPatternDQ.ReplaceAllString(masked, `${1}`+SensitiveMask+`${3}`)
	// 第二层：命名 KV 脱敏（单引号）
	masked = s.namedKVPatternSQ.ReplaceAllString(masked, `${1}`+SensitiveMask+`${3}`)

	// 第三层：Bearer 令牌脱敏（保留 "Bearer " 前缀）
	masked = s.bearerPattern.ReplaceAllString(masked, `${1}`+SensitiveMask)

	// 第四层：特定模式脱敏
	// 前 6 个模式直接全文替换
	for i := 0; i < len(s.specificPatterns)-1; i++ {
		masked = s.specificPatterns[i].ReplaceAllString(masked, SensitiveMask)
	}
	// 身份证号特殊处理：保留前后字符，只脱敏号码部分
	// 正则 (^|[^\d])(\d{17}[\dXx])($|[^\d]) 中 $1=前缀, $2=身份证号, $3=后缀
	masked = s.specificPatterns[len(s.specificPatterns)-1].ReplaceAllString(masked, `${1}`+SensitiveMask+`${3}`)

	return masked
}

// NewSanitizerWriter 创建脱敏 Writer，写入前对内容进行脱敏。
func NewSanitizerWriter(underlying io.Writer, sanitizer *Sanitizer) io.Writer {
	return &sanitizerWriter{
		underlying: underlying,
		sanitizer:  sanitizer,
	}
}

// Write 实现 io.Writer 接口，写入前对内容进行脱敏。
// 脱敏失败时直接写入原文，绝不因脱敏异常而阻止日志输出。
func (w *sanitizerWriter) Write(p []byte) (n int, err error) {
	// 脱敏异常时直接写入原文
	defer func() {
		if r := recover(); r != nil {
			n, err = w.underlying.Write(p)
		}
	}()

	sanitized := w.sanitizer.Sanitize(string(p))
	return w.underlying.Write([]byte(sanitized))
}
