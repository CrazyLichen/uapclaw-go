package lite

import (
	"errors"
	"os"
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// segmenterHandle 分词器最小契约。
// 抽成接口是为了让单测能注入「加载失败」的假实现，验证降级路径；
// 生产实现是 *gse.Segmenter。
type segmenterHandle interface {
	// Cut 分词；hmm=true 时启用 HMM 新词发现
	Cut(str string, hmm ...bool) []string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// segmenterVersion 分词器标识，写入 meta。词典/库升级时改这里 → 触发全量重建。
	segmenterVersion = "gse-zh_s@v1.3.1"
	// maxFTSTokens 关键词检索最多使用的 token 数（AND 连接，过多会让召回过窄）。
	maxFTSTokens = 5
	// segmenterDisableEnv 降级开关：集成测试与线上紧急规避共用
	segmenterDisableEnv = "UAPCLAW_DISABLE_SEGMENTER"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errSegmenterUnavailable 分词器不可用（已降级为不切分）
	errSegmenterUnavailable = errors.New("memory segmenter unavailable")

	// segOnce 保证词典只加载一次（实测 843ms）。
	// 用指针是为了让单测能重置它而不复制 sync.Once（govet copylocks）。
	segOnce = &sync.Once{}
	// segInst 分词器实例；nil 表示不可用
	segInst segmenterHandle
	// segmenterLoadFn 加载函数（单测可替换）
	segmenterLoadFn = loadSegmenter

	// cjkStopWords 中文虚词表。
	// gse 自带的 stop_tokens.txt（1161 行）只含标点与符号，没有"的/了/是"这类虚词
	// （标点已由 isContentToken 过滤）。AND 检索要求所有 token 同时出现，虚词一旦进
	// token 表会无谓收窄召回，所以这里补一份最小中文虚词表。
	cjkStopWords = map[string]struct{}{
		"的": {}, "了": {}, "是": {}, "在": {}, "和": {}, "与": {}, "或": {},
		"也": {}, "就": {}, "都": {}, "而": {}, "及": {}, "等": {}, "被": {},
		"把": {}, "对": {}, "从": {}, "到": {}, "为": {}, "以": {}, "之": {},
		"其": {}, "这": {}, "那": {}, "着": {}, "过": {}, "吗": {}, "呢": {},
		"吧": {}, "啊": {}, "呀": {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemorySegmenterAsync 后台预热分词器。
// gse 加载 35 万词简体词典实测 843ms，预热后不会落在用户第一次 memory_search 上。
func InitMemorySegmenterAsync() {
	go func() { _, _ = ensureSegmenter() }()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureSegmenter 惰性加载分词器；失败返回 (nil, err) 且不再重试（降级不抖动）。
func ensureSegmenter() (segmenterHandle, error) {
	var loadErr error
	segOnce.Do(func() {
		seg, err := segmenterLoadFn()
		if err != nil {
			loadErr = err
			logger.Error(logComponent).
				Str("event_type", "memory_segmenter_unavailable").
				Err(err).
				Msg("memory segmenter unavailable, falling back to unsegmented text")
			return
		}
		segInst = seg
	})
	if segInst == nil && loadErr == nil {
		// once 已经跑过（可能是别的 goroutine 先失败），这里补一个错误值
		loadErr = errSegmenterUnavailable
	}
	return segInst, loadErr
}

// loadSegmenter 加载 gse 内置简体词典 + 停用词表。
// ⚠️ 决策记录：使用 gse 默认 embed（`-tags ne` 未启用）。gse 会把 jp/繁/简/idf
// 四份词典一起编进二进制（实测约 35.9MB），但运行时只加载简体这一份。
func loadSegmenter() (segmenterHandle, error) {
	// 降级开关：集成测试与线上紧急规避（分词出问题时）共用
	if os.Getenv(segmenterDisableEnv) == "1" {
		return nil, errSegmenterUnavailable
	}

	var seg gse.Segmenter
	if err := seg.LoadDictEmbed("zh_s"); err != nil {
		return nil, err
	}
	// 注：gse 的 stop_tokens.txt 只有标点（无"的/了/是"），标点已由 isContentToken
	// 过滤，中文虚词由 cjkStopWords 处理，因此这里不需要 LoadStopEmbed。
	return &seg, nil
}

// segmenterID 返回分词器标识；不可用时 "none"。
// 写入 meta：从 "none" 变回版本号（或反之）都会触发一次全量重建。
func segmenterID() string {
	if os.Getenv(segmenterDisableEnv) == "1" {
		return "none"
	}
	seg, err := ensureSegmenter()
	if err != nil || seg == nil {
		return "none"
	}
	return segmenterVersion
}

// segmentForFTS 分词入口 —— 索引侧与查询侧必须共用，否则 token 对不齐。
// 处理链：gse.Cut → 小写 → 去掉空白/纯标点 → 去重保序。
// 分词器不可用时返回 nil，调用方降级为「原文 + 正则切分 + OR」。
func segmentForFTS(text string) []string {
	// 降级开关在调用时判定：线上紧急规避无需重启进程
	if os.Getenv(segmenterDisableEnv) == "1" {
		return nil
	}
	seg, err := ensureSegmenter()
	if err != nil || seg == nil {
		return nil
	}
	raw := seg.Cut(text, true)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, tok := range raw {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if !isContentToken(tok) {
			continue
		}
		if _, stop := cjkStopWords[tok]; stop {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// joinSegments 把分词结果拼成写进 FTS 的文本形态。
func joinSegments(tokens []string) string {
	return strings.Join(tokens, " ")
}

// isContentToken 判断 token 是否含字母或数字（滤掉空格与纯标点）。
func isContentToken(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
