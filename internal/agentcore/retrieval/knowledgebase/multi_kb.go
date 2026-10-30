package knowledgebase

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// multiKBEntry with_source 的聚合中间态。
//
// rawScore/rawScoreScaled 用指针区分"未设置"与"显式 0"（Python None 语义）；
// metadata 取首见值后固定不更新（Python 字面）。
type multiKBEntry struct {
	// score 当前最大相似度
	score float64
	// rawScore 当前最大原始分（nil = 尚未设置）
	rawScore *float64
	// rawScoreScaled 当前最大归一化原始分（nil = 尚未设置）
	rawScoreScaled *float64
	// kbIDs 来源知识库集合（set 语义）
	kbIDs map[string]struct{}
	// metadata 首个命中结果的元数据
	metadata map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMultiKBLimit limit 真值链最终兜底值（Python 字面 5）
	defaultMultiKBLimit = 5
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RetrieveMultiKB 跨知识库检索，按文本去重、分数取最大、降序返回文本列表。
//
// 单库失败只记 Warn 并当作空结果（软失败）；limit 走真值链
// topK → config.TopK → 5。
//
// Python: simple_knowledge_base.py retrieve_multi_kb
func RetrieveMultiKB(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, topK *int) []string {
	if len(kbs) == 0 {
		return []string{}
	}

	allResults := retrieveMultiKBGather(ctx, kbs, query, config, "retrieve_multi_kb")

	merged := make(map[string]float64)
	var order []string
	for _, results := range allResults {
		for _, r := range results {
			if existing, ok := merged[r.Text]; !ok {
				merged[r.Text] = r.Score
				order = append(order, r.Text)
			} else if r.Score > existing {
				merged[r.Text] = r.Score
			}
		}
	}

	ranked := make([]string, len(order))
	copy(ranked, order)
	sort.SliceStable(ranked, func(i, j int) bool {
		return merged[ranked[i]] > merged[ranked[j]]
	})

	return pyTake(ranked, multiKBLimit(topK, config))
}

// RetrieveMultiKBWithSource 跨知识库检索，返回带来源信息的结果。
//
// 聚合语义（逐条对齐 Python）：score 取最大；raw_score/raw_score_scaled
// 在元数据中显式给出时才参与取最大（否则回落本条 score）；kb_ids 取并集
// 后排序；metadata 固定为首次命中的那条。
//
// 元数据中的 raw_score / raw_score_scaled 不是数值时，Python 的 max()
// 会抛 TypeError 中断整条聚合，故这里同样返回 155509（而非静默跳过）。
//
// Python: simple_knowledge_base.py retrieve_multi_kb_with_source
func RetrieveMultiKBWithSource(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, topK *int) ([]common.MultiKBRetrievalResult, error) {
	if len(kbs) == 0 {
		return []common.MultiKBRetrievalResult{}, nil
	}

	allResults := retrieveMultiKBGather(ctx, kbs, query, config, "retrieve_multi_kb_with_source")

	merged := make(map[string]*multiKBEntry)
	var order []string
	for i := range kbs {
		kbID := kbs[i].Config().KBID
		for _, r := range allResults[i] {
			meta := r.Metadata
			if meta == nil {
				meta = map[string]any{}
			}
			score := r.Score
			rawScore, hasRaw, err := pyMetaFloat(meta, "raw_score", score)
			if err != nil {
				return nil, err
			}
			rawScaled, hasScaled, err := pyMetaFloat(meta, "raw_score_scaled", score)
			if err != nil {
				return nil, err
			}

			entry, exists := merged[r.Text]
			if !exists {
				entry = &multiKBEntry{
					score:    score,
					kbIDs:    make(map[string]struct{}),
					metadata: meta,
				}
				if hasRaw {
					v := rawScore
					entry.rawScore = &v
				}
				if hasScaled {
					v := rawScaled
					entry.rawScoreScaled = &v
				}
				merged[r.Text] = entry
				order = append(order, r.Text)
			} else {
				if score > entry.score {
					entry.score = score
				}
				if hasRaw {
					if entry.rawScore == nil || rawScore > *entry.rawScore {
						v := rawScore
						entry.rawScore = &v
					}
				}
				if hasScaled {
					if entry.rawScoreScaled == nil || rawScaled > *entry.rawScoreScaled {
						v := rawScaled
						entry.rawScoreScaled = &v
					}
				}
			}
			entry.kbIDs[kbID] = struct{}{}
		}
	}

	ranked := make([]string, len(order))
	copy(ranked, order)
	sort.SliceStable(ranked, func(i, j int) bool {
		return merged[ranked[i]].score > merged[ranked[j]].score
	})
	ranked = pyTake(ranked, multiKBLimit(topK, config))

	out := make([]common.MultiKBRetrievalResult, 0, len(ranked))
	for _, text := range ranked {
		e := merged[text]
		kbIDs := make([]string, 0, len(e.kbIDs))
		for id := range e.kbIDs {
			kbIDs = append(kbIDs, id)
		}
		sort.Strings(kbIDs)

		item := common.MultiKBRetrievalResult{
			Text:     text,
			Score:    e.score,
			KbIDs:    kbIDs,
			Metadata: e.metadata,
		}
		if e.rawScore != nil {
			item.RawScore = *e.rawScore
		}
		if e.rawScoreScaled != nil {
			item.RawScoreScaled = *e.rawScoreScaled
		}
		out = append(out, item)
	}
	return out, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// retrieveMultiKBGather 并发检索各知识库，返回与 kbs 等长的结果切片。
//
// 裸 gather（13.17/13.25 方案 A 先例）：协程各写各的槽位，无 fail-fast，
// 单库错误只 Warn 并留空槽位，与 Python asyncio.gather 全跑完语义一致。
//
// caller 为调用方函数名，仅用于 Warn 文案——Python 侧两个聚合函数各自
// 打印自己的函数名（`retrieve_multi_kb:` / `retrieve_multi_kb_with_source:`），
// 便于区分失败发生在哪条链路。
func retrieveMultiKBGather(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, caller string) [][]common.RetrievalResult {
	results := make([][]common.RetrievalResult, len(kbs))
	var wg sync.WaitGroup
	for i := range kbs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rs, err := kbs[i].Retrieve(ctx, query, config)
			if err != nil {
				logger.Warn(logComponent).
					Str("kb_id", kbs[i].Config().KBID).
					Err(err).
					Msg(caller + ": kb failed")
				return
			}
			results[i] = rs
		}(i)
	}
	wg.Wait()
	return results
}

// multiKBLimit 计算返回条数上限：Python `top_k or config.top_k or 5`。
//
// 真值链按 Python truthiness 判断（非零即采用，负数同样生效）。
func multiKBLimit(topK *int, config *common.RetrievalConfig) int {
	if topK != nil && *topK != 0 {
		return *topK
	}
	if config != nil && config.TopK != 0 {
		return config.TopK
	}
	return defaultMultiKBLimit
}

// pyTake 复刻 Python 切片 ranked[:limit] 的边界语义。
//
// limit >= 0：取前 limit 条（超长时返回全部）；limit < 0：等价于
// 去掉末尾 |limit| 条（Python 负数切片）。
func pyTake(items []string, limit int) []string {
	n := len(items)
	if limit < 0 {
		n += limit
		if n < 0 {
			n = 0
		}
		return items[:n]
	}
	if limit < n {
		return items[:limit]
	}
	return items
}

// pyMetaFloat 读取元数据中的浮点字段，缺失时回落本条 score。
//
// 对齐 Python `meta.get(key, score)` 与其后的 `if raw_score is not None`：
//   - 键缺失 → 回落 score，且参与取最大（true）
//   - 键存在但值为 None → 不参与取最大（false），装填时为 0
//   - 键存在且为数值 → 参与取最大（true）
//   - 键存在但非数值 → Python 的 max() 抛 TypeError 中断整条聚合，
//     Go 同样返回 155509（不静默跳过）
func pyMetaFloat(meta map[string]any, key string, fallback float64) (float64, bool, error) {
	v, ok := meta[key]
	if !ok {
		return fallback, true, nil
	}
	if v == nil {
		return 0, false, nil
	}
	if f, ok := v.(float64); ok {
		return f, true, nil
	}
	return 0, false, exception.BuildError(
		exception.StatusRetrievalKbMetadataTypeInvalid,
		exception.WithMsg(fmt.Sprintf("metadata %q must be a number, got %T", key, v)),
	)
}
