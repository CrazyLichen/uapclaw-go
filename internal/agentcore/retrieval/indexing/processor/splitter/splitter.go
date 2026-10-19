package splitter

import (
	"strings"

	"github.com/sentencizer/sentencizer"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SentenceSplitter 句级分割器：sentencizer 分句 + token 预算打包。
//
// 非并发安全（seg 每文档覆盖，对齐 Python self.seg），按文档使用。
//
// Python: splitter/splitter.py (SentenceSplitter)
type SentenceSplitter struct {
	// BaseSplitter 基类（ChunkSize/ChunkOverlap/enc/dec + 共享方法）
	*BaseSplitter
	// defaultLan 语言码："auto" → ""（每文档 detectChinese），其他原样透传 sentencizer
	defaultLan string
	// seg sentencizer 分句器，每文档重建（对齐 Python self.seg = Segmenter(...)）
	seg sentencizer.Segmenter
}

// sentenceSpan 内部四元组：句子 + 坐标 + token 长度。
//
// 对齐 Python cur_sents 的 (text, start, end, token_len) 四元组；
// tokenLen 不外泄到 ChunkSpan。
type sentenceSpan struct {
	// text 句子文本
	text string
	// start 原文起始字节位置
	start int
	// end 原文结束字节位置
	end int
	// tokenLen 句子 token 数
	tokenLen int
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewSentenceSplitter 构造句级分割器。
//
// 对齐 Python SentenceSplitter.__init__：
//   - lan == "auto" → defaultLan 置空（SplitDoc 时逐文档检测），否则原样保留
//   - dec 为 nil 时长句整句保留（不子切分）
//
// Python: splitter/splitter.py SentenceSplitter.__init__
func NewSentenceSplitter(bs *BaseSplitter, lan string) *SentenceSplitter {
	defaultLan := lan
	if lan == "auto" {
		defaultLan = ""
	}
	ss := &SentenceSplitter{BaseSplitter: bs, defaultLan: defaultLan}
	bs.SetSelf(ss)
	return ss
}

// SplitDoc 分割文档：分句 → token 预算打包 → (text, start, end) 列表。
//
// 严格对齐 Python SentenceSplitter.__call__：
//   - 空文本 → []
//   - lan 路由（defaultLan 或 detectChinese）
//   - sentencizer 每文档重建（默认无 Clean()，对齐 pysbd clean=False）
//   - 单句超预算：flush 后 dec 有则子切分窗口、无则整句保留
//   - 短句累加：超预算 flush，当前句留作下块开头（overlap 修复语义）
//   - 尾部 flush
//
// Python: splitter/splitter.py SentenceSplitter.__call__
func (s *SentenceSplitter) SplitDoc(doc string) ([]ChunkSpan, error) {
	if strings.TrimSpace(doc) == "" {
		return []ChunkSpan{}, nil
	}

	// 语言路由：defaultLan 为空（auto）时逐文档检测，否则透传
	lan := s.defaultLan
	if lan == "" {
		lan = detectChinese(doc, 0.1)
	}
	s.seg = sentencizer.NewSegmenter(lan)

	sentences, err := s.sentencesWithSpans(doc)
	if err != nil {
		return nil, err
	}
	chunks := make([]ChunkSpan, 0)
	// 缓冲 (text, start, end, tokenLen)，打包进 chunk_size 期间持有
	curSents := make([]sentenceSpan, 0)

	for _, sent := range sentences {
		// 单句超 token 预算：flush 缓冲，然后子切分或整句保留
		if sent.tokenLen > s.ChunkSize {
			chunks, curSents = s.flush(chunks, curSents)
			if s.tokenizerDec != nil {
				subChunks, err := s.splitLongSegment(sent.text, sent.start, sent.end)
				if err != nil {
					return nil, err
				}
				chunks = append(chunks, subChunks...)
			} else {
				chunks = append(chunks, ChunkSpan{Text: sent.text, Start: sent.start, End: sent.end})
			}
			continue
		}

		// 多短句打包直到 token 和将超 chunk_size
		curTokenCount := 0
		for _, cs := range curSents {
			curTokenCount += cs.tokenLen
		}
		if curTokenCount+sent.tokenLen <= s.ChunkSize {
			curSents = append(curSents, sent)
		} else {
			chunks, curSents = s.flush(chunks, curSents)
			// Overlap 修复：当前句留给下一块而不是丢弃（对齐 Python 注释）
			curSents = append(curSents, sent)
		}
	}

	// 尾部 flush（下块的 overlap 尾缀在 flush 内部计算）
	chunks, _ = s.flush(chunks, curSents)
	logger.Info(logComponent).
		Int("chunk_count", len(chunks)).
		Msg("Computed the following sentence-level chunks")
	return chunks, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// detectChinese 检测文本主要为中文还是英文（基于字符分布）。
//
// 中文字符（CJK 统一表意 0x4E00-0x9FFF）占比 >= threshold 判 zh；
// 未达标时用标点启发（？> ? 且 ！> ! 判 zh）。
//
// Python 的 numpy UTF-32 向量化与 5000 万字符分块统计在 Go 侧省略
// （numpy 特有内存优化，rune 单循环足够快）；计满阈值即早退为语义
// 等价增强（Python 分块路径的早退行为，统一路径最终判定结果一致）。
//
// Python: splitter/splitter.py SentenceSplitter._detect_chinese
func detectChinese(text string, threshold float64) string {
	if text == "" {
		return "en"
	}
	runes := []rune(text)
	total := len(runes)
	thresholdVal := int(threshold * float64(total))
	chineseCount := 0
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
			// 语义早退：已计满阈值
			if chineseCount >= thresholdVal {
				return "zh"
			}
		}
	}
	isChinese := chineseCount >= thresholdVal
	if !isChinese {
		usingChinesePuncs := strings.Count(text, "？") > strings.Count(text, "?") &&
			strings.Count(text, "！") > strings.Count(text, "!")
		isChinese = usingChinesePuncs
	}
	if isChinese {
		return "zh"
	}
	return "en"
}

// sentencesWithSpans 分句并附加 token 长度。
//
// sentencizer TextSpans 原生产出 (start, end, sentence)——Python 的
// find-span 恢复 + used_spans 去重 + 失败 warning 不可达省略（偏差 2）。
// 空白句子过滤 + enc 编码求 tokenLen；enc 失败返回错误（决策 2b 上抛）。
//
// Python: splitter/splitter.py SentenceSplitter._sentences_with_spans
func (s *SentenceSplitter) sentencesWithSpans(text string) ([]sentenceSpan, error) {
	spans := s.seg.TextSpans(text)
	out := make([]sentenceSpan, 0, len(spans))
	for _, sp := range spans {
		if strings.TrimSpace(sp.Sentence) == "" {
			continue
		}
		tokens, err := s.tokenizerEnc(sp.Sentence)
		if err != nil {
			return nil, err
		}
		out = append(out, sentenceSpan{
			text:     sp.Sentence,
			start:    sp.Start,
			end:      sp.End,
			tokenLen: len(tokens),
		})
	}
	return out, nil
}

// flush 输出当前缓冲为一块，并计算 overlap 尾缀作为下块开头。
//
// 对齐 Python _flush：无分隔符拼接；span 取首尾句坐标；overlap>0 且
// 多于一句时，倒序累加 token 和 <= overlap 的句子前缀、正序放回。
//
// Python: splitter/splitter.py SentenceSplitter._flush
func (s *SentenceSplitter) flush(chunks []ChunkSpan, curSents []sentenceSpan) ([]ChunkSpan, []sentenceSpan) {
	if len(curSents) == 0 {
		return chunks, nil
	}

	// 一块输出：拼接句文本，span 覆盖首→尾句
	var sb strings.Builder
	for _, cs := range curSents {
		sb.WriteString(cs.text)
	}
	chunks = append(chunks, ChunkSpan{
		Text:  sb.String(),
		Start: curSents[0].start,
		End:   curSents[len(curSents)-1].end,
	})

	// flush 的句子前缀中 token 和 <= chunk_overlap 的部分作为下块缓冲开头
	nextCurSents := make([]sentenceSpan, 0)
	if s.ChunkOverlap > 0 && len(curSents) > 1 {
		overlapTokens := 0
		overlapSents := make([]sentenceSpan, 0)
		for i := len(curSents) - 1; i >= 0; i-- {
			cs := curSents[i]
			if overlapTokens+cs.tokenLen <= s.ChunkOverlap {
				overlapSents = append(overlapSents, cs)
				overlapTokens += cs.tokenLen
			} else {
				break
			}
		}
		// 倒序收集，正序放回
		for i, j := 0, len(overlapSents)-1; i < j; i, j = i+1, j-1 {
			overlapSents[i], overlapSents[j] = overlapSents[j], overlapSents[i]
		}
		nextCurSents = overlapSents
	}
	return chunks, nextCurSents
}

// splitLongSegment 把一个长句切成重叠的 token 窗口，保证内容不丢失。
//
// 对齐 Python _split_long_segment：整句 encode 一次 → step=max(1, size-overlap)
// 滑窗 → 每窗口 decode（失败或空白跳过，对齐 safe_decode→""→continue）→
// span 沿用父句坐标；全部窗口为空时整句 fallback。
//
// Python: splitter/splitter.py SentenceSplitter._split_long_segment
func (s *SentenceSplitter) splitLongSegment(sentText string, sentStart, sentEnd int) ([]ChunkSpan, error) {
	// 整句 encode 一次；切片 id 窗口后 decode（需要 tokenizerDec）
	ids, err := s.tokenizerEnc(sentText)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []ChunkSpan{{Text: sentText, Start: sentStart, End: sentEnd}}, nil
	}

	// 滑窗步长；至少 1 保证前进
	step := s.ChunkSize - s.ChunkOverlap
	if step < 1 {
		step = 1
	}
	out := make([]ChunkSpan, 0)
	nIDs := len(ids)
	for windowStart := 0; windowStart < nIDs; windowStart += step {
		windowEnd := windowStart + s.ChunkSize
		if windowEnd > nIDs {
			windowEnd = nIDs
		}
		window := ids[windowStart:windowEnd]
		if len(window) == 0 {
			break
		}
		text, err := s.tokenizerDec(window)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		// 字符 span 留父句（对索引够用）；文本是子窗口
		out = append(out, ChunkSpan{Text: text, Start: sentStart, End: sentEnd})
	}
	if len(out) == 0 {
		return []ChunkSpan{{Text: sentText, Start: sentStart, End: sentEnd}}, nil
	}
	return out, nil
}
