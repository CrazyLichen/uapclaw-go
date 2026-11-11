package recommendation

import (
	"errors"
	"strconv"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RecMeta 调用方已知的推荐元数据。
//
// 显式反馈入口能从 chunk payload 的 proactive_type/proactive_target 拿到这三个值。
// recommendation_history 尚未写入时（_on_delivered 异步、可能晚于用户点赞）
// 用它兜底填充反馈记录，避免时序竞态丢反馈。
//
// 聚合成对象而非散参：recordFeedback 若收 6 个散参超 huawei-too-many-arguments（≤5），
// 这三个一组语义内聚，合一个 meta 降到合规参数数。
//
// Python: feedback_collector.py:24 RecMeta
type RecMeta struct {
	// RecType 推荐类型
	RecType string
	// RecTarget 推荐目标
	RecTarget string
	// RecContent 推荐话术
	RecContent string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// feedbackBufferCapacity 反馈缓冲容量上限（FIFO 截断）
	// Python: feedback_collector.py:133 if len(state.feedback_buffer) > 20
	feedbackBufferCapacity = 20
	// feedbackTypeImplicit 隐式反馈的类型占位
	// Python: feedback_collector.py:167 record_feedback(rec_id, "implicit", user_reply)
	feedbackTypeImplicit = "implicit"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RecordExplicitFeedback 记录显式反馈（赞/踩按钮）。
// Python: feedback_collector.py:141 record_explicit_feedback
func RecordExplicitFeedback(recID, feedbackType string, meta *RecMeta) {
	RecordFeedback(recID, feedbackType, "", meta)
}

// RecordImplicitFeedback 记录隐式反馈（用户文本回复）。
//
// 不在此做情感分类：关键词正则误判率高（"好"子串撞"不好"等），错误 label 会污染
// 梯度更新。直接把回复原文存进 buffer，feedbackType 占位 "implicit"，情感与规则
// 提取交给梯度更新器里的 LLM（看 userReply 原文判断）。中性回复也不预先丢弃——
// 是否产生梯度规则由梯度 LLM 自行决定（无 operations 即等效中性）。
//
// Python: feedback_collector.py:157 record_implicit_feedback
func RecordImplicitFeedback(recID, userReply string) {
	RecordFeedback(recID, feedbackTypeImplicit, userReply, nil)
}

// RecordFeedback 把反馈写入缓冲区（不调 LLM，零成本）。
//
// 反馈先入 buffer，下次 tick 批量消费做梯度更新。
//
// Python: feedback_collector.py:47 record_feedback
func RecordFeedback(recID, feedbackType, userReply string, meta *RecMeta) {
	state := LoadRecommendationState("")
	if meta == nil {
		meta = &RecMeta{}
	}

	// 查推荐记录（可能还没写入：_on_delivered 在后台主 agent 跑完后才执行，
	// 而 chunk 流式推送在前，用户可能在 history 写入前就点赞）
	var recData map[string]any
	for _, rec := range state.RecommendationHistory {
		if stringField(rec, "id") == recID {
			recData = rec
			break
		}
	}
	if recData == nil {
		// 查不到 history 不丢弃反馈——用调用方传入的元数据照样记录，
		// 否则时序竞态会让点赞丢失，buffer 永远空，梯度更新无米下锅。
		logger.Debug(logComponent).Str("rec_id", recID).
			Msg("recommendation not in history yet, recording with caller-provided metadata")
		recData = map[string]any{
			"type":    meta.RecType,
			"target":  meta.RecTarget,
			"content": meta.RecContent,
		}
	}

	newFeedback := map[string]any{
		"rec_id":        recID,
		"rec_type":      firstNonEmpty(stringField(recData, "type"), meta.RecType),
		"rec_target":    firstNonEmpty(stringField(recData, "target"), meta.RecTarget),
		"rec_content":   firstNonEmpty(stringField(recData, "content"), meta.RecContent),
		"feedback_type": feedbackType,
		"user_reply":    userReply,
		"timestamp":     float64(time.Now().UnixNano()) / 1e9,
	}

	// 去重：同一条推荐，显式反馈（赞/踩）和隐式反馈（用户回复原文）各保留第一条。
	// 两类信号都有效、都喂梯度——显式是明确表态、隐式是用户补充的具体场景文本，
	// 互补不互斥。同一类后续反馈丢弃：显式是单选语义（二次点赞/踩丢），
	// 隐式只采紧跟推荐后的第一条回复（后续对话往往转到别的话题）。
	newIsExplicit := isExplicitFeedback(feedbackType)
	for _, fb := range state.FeedbackBuffer {
		if stringField(fb, "rec_id") != recID {
			continue
		}
		if isExplicitFeedback(stringField(fb, "feedback_type")) == newIsExplicit {
			kind := "implicit"
			if newIsExplicit {
				kind = "explicit"
			}
			logger.Debug(logComponent).Str("kind", kind).Str("rec_id", recID).
				Msg("duplicate feedback dropped, keeping first")
			return
		}
	}

	state.FeedbackBuffer = append(state.FeedbackBuffer, newFeedback)

	// FIFO，限容量
	if len(state.FeedbackBuffer) > feedbackBufferCapacity {
		state.FeedbackBuffer = state.FeedbackBuffer[len(state.FeedbackBuffer)-feedbackBufferCapacity:]
	}

	state.Touch()
	SaveRecommendationState(state, "")

	logger.Info(logComponent).Str("rec_id", recID).Str("feedback_type", feedbackType).
		Msg("recorded feedback")
}

// ConsumeFeedbackBuffer 取出并清空反馈缓冲（tick 中梯度更新前调用）。
// Python: feedback_collector.py:172 consume_feedback_buffer
func ConsumeFeedbackBuffer(state *RecommendationState) []map[string]any {
	if state == nil || len(state.FeedbackBuffer) == 0 {
		return nil
	}
	feedbacks := make([]map[string]any, len(state.FeedbackBuffer))
	copy(feedbacks, state.FeedbackBuffer)
	state.FeedbackBuffer = nil
	state.Touch()
	SaveRecommendationState(state, "")

	logger.Info(logComponent).Int("count", len(feedbacks)).Msg("consumed feedbacks from buffer")
	return feedbacks
}

// FindLatestRecommendation 查找某会话最近一条推荐。
//
// 用于隐式反馈：把用户回复关联到该会话最近一条推荐。不设时间窗——
// 「很久后的回复是否相关、是否产生梯度」交给梯度更新器的模型判断（看 recContent
// 与 userReply 语义），不靠时间硬挡。配合 RecordFeedback「同 recID 只采紧跟第一条、
// 后续丢弃」，每条推荐只关联它之后紧跟的第一条用户回复。
//
// maxAgeSeconds 为 0（默认）不做过期检查；>0 时只返回 tick_at 在该秒数内的推荐。
//
// Python: feedback_collector.py:199 find_latest_recommendation
func FindLatestRecommendation(sessionID string, maxAgeSeconds float64) map[string]any {
	state := LoadRecommendationState("")
	maxAge := maxAgeSeconds
	cutoff := 0.0
	if maxAge > 0 {
		cutoff = float64(time.Now().UnixNano())/1e9 - maxAge
	}

	// history 是追加序，反向遍历拿最新
	for i := len(state.RecommendationHistory) - 1; i >= 0; i-- {
		rec := state.RecommendationHistory[i]
		if stringField(rec, "session_id") != sessionID {
			continue
		}
		// 防御性 tick_at 解析：脏数据/旧记录可能缺字段或非数值，跳过而非崩
		tickAt, err := parseTickAt(rec["tick_at"])
		if err != nil {
			continue
		}
		if tickAt < cutoff {
			// 反向遍历已是最新优先，遇到过期项说明更新鲜的都没命中，停。
			return nil
		}
		return rec
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isExplicitFeedback 是否显式反馈（赞/踩）。
func isExplicitFeedback(feedbackType string) bool {
	return feedbackType == "explicit_like" || feedbackType == "explicit_dislike"
}

// firstNonEmpty 返回首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseTickAt 解析 tick_at 字段为浮点秒。
// 缺失/类型不符/无法解析时返回错误，由调用方跳过该记录。
func parseTickAt(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case string:
		return strconv.ParseFloat(typed, 64)
	case nil:
		// Python: float(rec.get("tick_at", 0.0)) —— 缺失取 0.0，不报错
		return 0, nil
	default:
		return 0, errUnparsableTickAt
	}
}

// errUnparsableTickAt tick_at 无法解析为数值。
var errUnparsableTickAt = errors.New("cannot parse tick_at as float")
