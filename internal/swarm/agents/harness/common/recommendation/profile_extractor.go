package recommendation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RecommendationState 主动推荐引擎的持久化状态。
//
// 只存引擎运行态——推荐历史、反馈缓冲、策略梯度。
// 用户画像已废弃（所有推荐基于当前对话），故无 profile 字段。
//
// Python: profile_extractor.py:26 RecommendationState
type RecommendationState struct {
	// RecommendationHistory 历史推荐记录（最多 20 条）
	// 每条含 type/target/reason/timestamp，另有 id（唯一标识）与 content（LLM 生成的话术）
	RecommendationHistory []map[string]any
	// FeedbackBuffer 待处理的反馈缓冲（FIFO，最多 20 条）
	// 反馈先入此处，下次 tick 批量消费做梯度更新
	FeedbackBuffer []map[string]any
	// StrategyGradients 策略梯度 / Filter Memory（最多 10 条）
	// 文本形式的推荐策略规则
	StrategyGradients []map[string]any
	// LastUpdated 最后更新时间（RFC3339）
	LastUpdated string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// recommendationStateFileName 状态文件名
	// Python: profile_extractor.py:64 get_agent_workspace_dir() / "recommendation.json"
	recommendationStateFileName = "recommendation.json"
	// maxRecommendationHistory 推荐历史保留上限
	// Python: profile_extractor.py:51-52 len(...) > 20 时截断
	maxRecommendationHistory = 20
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AddRecommendation 追加一条推荐记录，超过 20 条时保留最新 20 条。
// Python: profile_extractor.py:48-52 RecommendationState.add_recommendation
func (s *RecommendationState) AddRecommendation(rec map[string]any) {
	s.RecommendationHistory = append(s.RecommendationHistory, rec)
	if len(s.RecommendationHistory) > maxRecommendationHistory {
		s.RecommendationHistory = s.RecommendationHistory[len(s.RecommendationHistory)-maxRecommendationHistory:]
	}
}

// Touch 更新最后更新时间。
// Python: profile_extractor.py:54-56 RecommendationState.touch
func (s *RecommendationState) Touch() {
	s.LastUpdated = time.Now().UTC().Format(time.RFC3339Nano)
}

// DefaultRecommendationStatePath 返回状态文件默认路径（workspace 下 recommendation.json）。
// Python: profile_extractor.py:62-64 _default_state_path
func DefaultRecommendationStatePath() string {
	return filepath.Join(workspace.AgentWorkspaceDir(), recommendationStateFileName)
}

// LoadRecommendationState 从磁盘加载状态；文件缺失/为空/损坏时返回空状态。
// path 为空串表示用默认路径。
//
// Python: profile_extractor.py:67-97 load_recommendation_state
func LoadRecommendationState(path string) *RecommendationState {
	statePath := path
	if statePath == "" {
		statePath = DefaultRecommendationStatePath()
	}
	info, err := os.Stat(statePath)
	if err != nil || info.Size() == 0 {
		return &RecommendationState{}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("path", statePath).Msg("recommendation state load failed")
		return &RecommendationState{}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		logger.Warn(logComponent).Err(err).Str("path", statePath).Msg("recommendation state load failed")
		return &RecommendationState{}
	}
	if raw == nil {
		return &RecommendationState{}
	}
	// Python: 三个列表字段各自做 isinstance(x, list) 守卫，类型不符回落空列表
	return &RecommendationState{
		RecommendationHistory: toMapSlice(raw["recommendation_history"]),
		FeedbackBuffer:        toMapSlice(raw["feedback_buffer"]),
		StrategyGradients:     toMapSlice(raw["strategy_gradients"]),
		LastUpdated:           toStringValue(raw["last_updated"]),
	}
}

// SaveRecommendationState 持久化状态到磁盘。
// path 为空串表示用默认路径。写入失败只告警，不返回错误（对齐 Python 吞异常）。
//
// Python: profile_extractor.py:100-110 save_recommendation_state
func SaveRecommendationState(state *RecommendationState, path string) {
	statePath := path
	if statePath == "" {
		statePath = DefaultRecommendationStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Str("path", statePath).Msg("recommendation state save failed")
		return
	}
	// Python: json.dumps(asdict(state), ensure_ascii=False, indent=2)
	// Go 的 json.MarshalIndent 不做 HTML 转义之外的处理；ensure_ascii=False 对应不转义非 ASCII
	payload := map[string]any{
		"recommendation_history": nilIfNil(state.RecommendationHistory),
		"feedback_buffer":        nilIfNil(state.FeedbackBuffer),
		"strategy_gradients":     nilIfNil(state.StrategyGradients),
		"last_updated":           state.LastUpdated,
	}
	data, err := utils.MarshalIndentNoEscape(payload)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("path", statePath).Msg("recommendation state save failed")
		return
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Str("path", statePath).Msg("recommendation state save failed")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toMapSlice 把 JSON 解出的值转成 []map[string]any。
// 非数组或元素非对象时按 Python 的 isinstance 守卫语义回落为空切片。
func toMapSlice(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			result = append(result, m)
		}
	}
	return result
}

// toStringValue 取字符串值，非字符串返回空串。
func toStringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// nilIfNil 把 nil 切片换成空切片，保证 JSON 序列化为 [] 而非 null。
// Python asdict 对 list 字段永远输出 []（dataclass default_factory=list）。
func nilIfNil(list []map[string]any) []map[string]any {
	if list == nil {
		return []map[string]any{}
	}
	return list
}
