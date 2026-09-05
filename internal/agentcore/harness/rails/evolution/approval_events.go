package evolution

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// progressEventConfig 演化进度事件的可选参数配置。
type progressEventConfig struct {
	skillName *string
	requestID *string
	prefix    *string
}

// ProgressEventOption 演化进度事件的可选参数。
type ProgressEventOption func(*progressEventConfig)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithSkillName 设置技能名称。
func WithSkillName(skillName string) ProgressEventOption {
	return func(c *progressEventConfig) { c.skillName = &skillName }
}

// WithRequestID 设置请求标识。
func WithRequestID(requestID string) ProgressEventOption {
	return func(c *progressEventConfig) { c.requestID = &requestID }
}

// WithPrefix 设置显示前缀。
func WithPrefix(prefix string) ProgressEventOption {
	return func(c *progressEventConfig) { c.prefix = &prefix }
}

// BuildProgressEvent 构建 llm_reasoning 进度事件。
func BuildProgressEvent(prefix, message string) *stream.OutputSchema {
	return &stream.OutputSchema{
		Type:  "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content": fmt.Sprintf("%s %s\n", prefix, message),
		},
	}
}

// BuildEvolutionProgressEvent 构建规范化的演化进度事件。
func BuildEvolutionProgressEvent(railKind, stage, message string, opts ...ProgressEventOption) *stream.OutputSchema {
	cfg := &progressEventConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	displayPrefix := "[Evolution]"
	if cfg.prefix != nil {
		displayPrefix = *cfg.prefix
	}

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindProgress,
		RailKind:  &railKind,
		Stage:     &stage,
	}.ToPayload()

	if cfg.skillName != nil {
		meta["skill_name"] = *cfg.skillName
	}
	if cfg.requestID != nil {
		meta["request_id"] = *cfg.requestID
	}

	return &stream.OutputSchema{
		Type:  "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content":         fmt.Sprintf("%s %s\n", displayPrefix, message),
			"_evolution_meta": meta,
		},
	}
}

// AttachEvolutionMeta 向审批事件的 payload 附加规范化的演化元数据。
func AttachEvolutionMeta(event *stream.OutputSchema, signalType, signalSource *string) *stream.OutputSchema {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		payload = make(map[string]any)
		event.Payload = payload
	}

	evolutionMeta, ok := payload["_evolution_meta"].(map[string]string)
	if !ok {
		evolutionMeta = map[string]string{}
		payload["_evolution_meta"] = evolutionMeta
	}

	if _, exists := evolutionMeta["event_kind"]; !exists {
		evolutionMeta["event_kind"] = EvolutionEventKindApproval
	}

	if signalType != nil {
		evolutionMeta["signal_type"] = *signalType
	}
	if signalSource != nil {
		evolutionMeta["source"] = *signalSource
	}

	return event
}

// BuildSkillApprovalEvent 构建技能经验审批事件。
func BuildSkillApprovalEvent(
	skillName, requestID string,
	records []checkpointing.EvolutionRecord,
	language string,
	isSharedRecords bool,
) *stream.OutputSchema {
	en := isEn(language)

	questions := make([]map[string]any, 0, len(records))

	var header string
	if isSharedRecords {
		if en {
			header = "Shared Experience Approval"
		} else {
			header = "在线共享经验审批"
		}
	} else {
		if en {
			header = "Skill Evolution Approval"
		} else {
			header = "技能演进审批"
		}
	}

	for _, record := range records {
		content := record.Change.Content
		if len(content) > 1000 {
			content = content[:1000]
		}

		var question string
		if en {
			question = fmt.Sprintf(
				"**Skill '%s' generated a new experience:**\n\n- **Target**: %s\n- **Section**: %s\n\n%s",
				skillName, string(record.Change.Target), record.Change.Section, content,
			)
		} else {
			question = fmt.Sprintf(
				"**Skill '%s' 演进生成了新经验：**\n\n- **目标**: %s\n- **章节**: %s\n\n%s",
				skillName, string(record.Change.Target), record.Change.Section, content,
			)
		}

		var options []map[string]string
		if en {
			options = []map[string]string{
				{"label": "Accept", "description": "Keep this evolution experience"},
				{"label": "Reject", "description": "Discard this evolution experience"},
			}
		} else {
			options = []map[string]string{
				{"label": "接收", "description": "保留此演进经验"},
				{"label": "拒绝", "description": "丢弃此演进经验"},
			}
		}

		questions = append(questions, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	var sourceVal *string
	if isSharedRecords {
		s := "experience_sharing"
		sourceVal = &s
	}
	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindApproval,
		SkillName: &skillName,
		RequestID: &requestID,
		Source:    sourceVal,
	}.ToPayload()

	if isSharedRecords {
		meta["is_shared_records"] = "true"
	}

	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id":      requestID,
			"_evolution_meta": meta,
			"questions":       questions,
		},
	}
}

// BuildSimplifyApprovalEvent 构建精简审批事件。
func BuildSimplifyApprovalEvent(
	skillName, requestID string,
	actions []map[string]any,
	language string,
) *stream.OutputSchema {
	en := isEn(language)

	previewParts := make([]string, 0, len(actions))
	limit := len(actions)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		action := actions[i]
		act := "?"
		if a, ok := action["action"]; ok {
			if s, ok := a.(string); ok {
				act = s
			}
		}
		recordID := "?"
		if r, ok := action["record_id"]; ok {
			if s, ok := r.(string); ok {
				recordID = s
			}
		}
		reason := ""
		if r, ok := action["reason"]; ok {
			if s, ok := r.(string); ok {
				reason = s
			}
		}
		previewParts = append(previewParts, fmt.Sprintf("- **%s** `%s`: %s", act, recordID, reason))
	}
	preview := ""
	for i, p := range previewParts {
		if i > 0 {
			preview += "\n"
		}
		preview += p
	}

	var question string
	if en {
		question = fmt.Sprintf(
			"**Simplify evolution experiences for Skill '%s'**\n\n%d action(s):\n%s\n\nDo you want to execute them?",
			skillName, len(actions), preview,
		)
	} else {
		question = fmt.Sprintf(
			"**精简 Skill '%s' 的演进经验**\n\n共 %d 项操作：\n%s\n\n是否执行？",
			skillName, len(actions), preview,
		)
	}

	var header string
	if en {
		header = "Skill Simplify Approval"
	} else {
		header = "Skill 精简审批"
	}

	var options []map[string]string
	if en {
		options = []map[string]string{
			{"label": "Execute", "description": "Run the simplify actions"},
			{"label": "Cancel", "description": "Discard this simplify request"},
		}
	} else {
		options = []map[string]string{
			{"label": "执行", "description": "执行精简操作"},
			{"label": "取消", "description": "放弃本次精简"},
		}
	}

	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id": requestID,
			"questions": []map[string]any{
				{
					"question":     question,
					"header":       header,
					"options":      options,
					"multi_select": false,
				},
			},
		},
	}
}

// BuildTeamSkillApprovalEventFromRecords 从暂存记录构建团队技能审批事件。
func BuildTeamSkillApprovalEventFromRecords(
	skillName, requestID, language string,
	records []checkpointing.EvolutionRecord,
) *stream.OutputSchema {
	questions := make([]TeamSkillQuestion, 0, len(records))
	for _, record := range records {
		questions = append(questions, TeamSkillQuestion{
			Section: record.Change.Section,
			Content: record.Change.Content,
		})
	}
	return buildTeamSkillExperienceQuestionEvent(skillName, requestID, language, questions)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isEn 判断语言是否为英文。
func isEn(language string) bool {
	return language == "en"
}

// buildTeamSkillExperienceQuestionEvent 从标准化问题输入构建团队技能经验审批事件。
func buildTeamSkillExperienceQuestionEvent(
	skillName, requestID, language string,
	questions []TeamSkillQuestion,
) *stream.OutputSchema {
	en := isEn(language)

	questionPayload := make([]map[string]any, 0, len(questions))

	for _, q := range questions {
		content := q.Content
		if len(content) > 1000 {
			content = content[:1000]
		}

		var question string
		if en {
			question = fmt.Sprintf(
				"**Team Skill '%s' evolution:**\n\n- **Section**: %s\n\n%s",
				skillName, q.Section, content,
			)
		} else {
			question = fmt.Sprintf(
				"**团队技能 '%s' 生成了演进经验：**\n\n- **章节**: %s\n\n%s",
				skillName, q.Section, content,
			)
		}

		var header string
		if en {
			header = "Team Skill Evolution Approval"
		} else {
			header = "团队技能演进审批"
		}

		var options []map[string]string
		if en {
			options = []map[string]string{
				{"label": "Accept", "description": "Keep this evolution"},
				{"label": "Reject", "description": "Discard this evolution"},
			}
		} else {
			options = []map[string]string{
				{"label": "接收", "description": "保留此演进经验"},
				{"label": "拒绝", "description": "丢弃此演进经验"},
			}
		}

		questionPayload = append(questionPayload, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindApproval,
		SkillName: &skillName,
		RequestID: &requestID,
	}.ToPayload()

	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id":      requestID,
			"_evolution_meta": meta,
			"questions":       questionPayload,
		},
	}
}
