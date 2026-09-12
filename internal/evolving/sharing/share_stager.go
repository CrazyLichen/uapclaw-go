package sharing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ShareStager 筛选、封装并暂存经验，用于跨用户共享。
//
// 边界规则：本组件从不写入 evolutions.json；
// ScreenAndStage 仅通过本地 QC 决定每条记录是否进入共享池，
// 封装通过的记录并推入 sharer 的上传队列。
// 共享侧 QC 的拒绝不影响本地持久化——调用方（SkillEvolutionRail）
// 通过其现有的 auto-save / approval 路径负责持久化记录。
//
// 去重和 Hub 侧的其他验收规则在 sharer 上传到后端时执行，不在此处。
//
// Python: openjiuwen/agent_evolving/sharing/share_stager.py ShareStager
type ShareStager struct {
	// keywordExtractor 关键词提取器，从优化器输出中提取 keywords/summary
	keywordExtractor *KeywordExtractor
	// sharer 经验共享器，接收通过 QC 的经验
	sharer *ExperienceSharer
	// qcScoreThreshold QC 分数阈值，低于此值的记录被丢弃（默认 0.6）
	qcScoreThreshold float64
	// sourceUserID 来源用户 ID（可选）
	sourceUserID *string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewShareStager 创建 ShareStager 实例。
//
// qcScoreThreshold 默认 0.6；sourceUserID 为可选参数。
//
// Python: ShareStager.__init__()
func NewShareStager(
	keywordExtractor *KeywordExtractor,
	sharer *ExperienceSharer,
	qcScoreThreshold float64,
	sourceUserID *string,
) *ShareStager {
	return &ShareStager{
		keywordExtractor: keywordExtractor,
		sharer:           sharer,
		qcScoreThreshold: qcScoreThreshold,
		sourceUserID:     sourceUserID,
	}
}

// QCScoreThreshold 返回 QC 分数阈值。
//
// Python: ShareStager.qc_score_threshold (property)
func (s *ShareStager) QCScoreThreshold() float64 {
	return s.qcScoreThreshold
}

// ScreenAndStage 运行 QC → 封装 → 暂存流水线，用于经验共享。
//
// 本方法不写入 evolutions.json，也不执行上传。
// 仅通过质量门筛选记录，将通过的记录封装为 SharedExperience，
// 推入 sharer 的待上传队列。
//
// Python: ShareStager.screen_and_stage()
func (s *ShareStager) ScreenAndStage(
	ctx context.Context,
	skillName string,
	records []checkpointing.EvolutionRecord,
	messages []map[string]any,
) *StagingResult {
	if len(records) == 0 {
		return EmptyStagingResult()
	}

	var staged []SharedExperience
	var dropped []DroppedRecord

	for _, record := range records {
		keywords, summary := ParseFromOptimizerOutput(record.Change)
		dropReason := s.qc(record, messages)
		if dropReason != "" {
			dropped = append(dropped, DroppedRecord{Record: record, Reason: dropReason})
			logger.Info(logComponent).
				Str("method", "ShareStager.ScreenAndStage").
				Str("record_id", record.ID).
				Str("reason", dropReason).
				Str("skill", skillName).
				Msg("[ShareStager] share-QC dropped record")
			continue
		}
		wrapped := s.wrap(record, keywords, summary, skillName)
		s.sharer.StageForUpload(skillName, &wrapped)
		staged = append(staged, wrapped)
	}

	return &StagingResult{
		StagedForShare:  staged,
		DroppedForShare: dropped,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// qc 对记录执行质量检查，返回丢弃原因字符串；通过时返回空字符串。
//
// Python: ShareStager._qc()
func (s *ShareStager) qc(record checkpointing.EvolutionRecord, messages []map[string]any) string {
	if record.Source == "execution_failure" && !messagesHasSuccessfulTool(messages) {
		return "execution failure without successful follow-up tool call"
	}

	score := record.Score
	if score < s.qcScoreThreshold {
		return fmt.Sprintf("score %.2f below threshold %.2f", score, s.qcScoreThreshold)
	}

	return ""
}

// messagesHasSuccessfulTool 检查消息中是否包含成功的工具执行结果。
//
// QC 不可行性门是保守的：来源为 execution_failure 的记录
// 仅在对话中完全没有任何成功的工具执行时才被丢弃。
// 不尝试按 tool_name 匹配，因为恢复通常通过不同的工具进行。
//
// Python: _messages_has_successful_tool()
func messagesHasSuccessfulTool(messages []map[string]any) bool {
	if messages == nil {
		return false
	}
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "tool" && role != "function" {
			continue
		}
		content := ""
		if v, ok := msg["content"]; ok && v != nil {
			content = fmt.Sprintf("%v", v)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		if signal.MatchFailureKeywords(content) {
			continue
		}
		return true
	}
	return false
}

// wrap 封装 EvolutionRecord 为 SharedExperience。
//
// 创建 SharingMeta，将 record 值拷贝，Keywords 显式 copy。
//
// Python: ShareStager._wrap()
func (s *ShareStager) wrap(
	record checkpointing.EvolutionRecord,
	keywords []string,
	summary string,
	skillName string,
) SharedExperience {
	skillVersion := ""
	if record.SkillVersion != nil {
		skillVersion = *record.SkillVersion
	}
	confidence := record.Score

	meta := &SharingMeta{
		SkillName:     skillName,
		SkillVersion:  skillVersion,
		UploadAt:      time.Now().Format(time.RFC3339),
		UploadTrigger: "user_approval",
		SourceUserID:  s.sourceUserID,
		Confidence:    confidence,
	}

	// 显式拷贝 Keywords 切片，避免共享底层数组
	keywordsCopy := make([]string, len(keywords))
	copy(keywordsCopy, keywords)

	return SharedExperience{
		Record:      record, // EvolutionRecord 是值类型，赋值即拷贝
		Keywords:    keywordsCopy,
		Summary:     summary,
		SharingMeta: meta,
	}
}
