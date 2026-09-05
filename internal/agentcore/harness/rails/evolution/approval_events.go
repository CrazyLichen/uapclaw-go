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
// 对齐 Python: build_progress_event(prefix: str, message: str) -> OutputSchema
func BuildProgressEvent(prefix, message string) *stream.OutputSchema {
	// 对齐 Python L20-24:
	//   return OutputSchema(type="llm_reasoning", index=0, payload={"content": f"{prefix} {message}\n"})
	return &stream.OutputSchema{
		Type: "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content": fmt.Sprintf("%s %s\n", prefix, message),
		},
	}
}

// BuildEvolutionProgressEvent 构建规范化的演化进度事件。
// 对齐 Python: build_evolution_progress_event(*, rail_kind, stage, message, skill_name=None, request_id=None, prefix=None)
func BuildEvolutionProgressEvent(railKind, stage, message string, opts ...ProgressEventOption) *stream.OutputSchema {
	cfg := &progressEventConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 对齐 Python L37: display_prefix = prefix or "[Evolution]"
	displayPrefix := "[Evolution]"
	if cfg.prefix != nil {
		displayPrefix = *cfg.prefix
	}

	// 对齐 Python L38-45: payload 构造 + _evolution_meta
	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindProgress,
		RailKind:  &railKind,
		Stage:     &stage,
	}.ToPayload()

	// 对齐 Python L46-49: skill_name / request_id 追加到 meta
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
// 对齐 Python: attach_evolution_meta(event: OutputSchema, *, signal_type=None, signal_source=None)
func AttachEvolutionMeta(event *stream.OutputSchema, signalType, signalSource *string) *stream.OutputSchema {
	// 对齐 Python L60: evolution_meta = event.payload.setdefault("_evolution_meta", {})
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

	// 对齐 Python L61: evolution_meta.setdefault("event_kind", "approval")
	if _, exists := evolutionMeta["event_kind"]; !exists {
		evolutionMeta["event_kind"] = EvolutionEventKindApproval
	}

	// 对齐 Python L63-64: if signal_type is not None: evolution_meta["signal_type"] = signal_type
	if signalType != nil {
		evolutionMeta["signal_type"] = *signalType
	}
	// 对齐 Python L65-66: if signal_source is not None: evolution_meta["source"] = signal_source
	if signalSource != nil {
		evolutionMeta["source"] = *signalSource
	}

	return event
}

// BuildSkillApprovalEvent 构建技能经验审批事件。
// 对齐 Python: build_skill_approval_event(skill_name, request_id, records, language="cn", *, is_shared_records=False)
func BuildSkillApprovalEvent(
	skillName, requestID string,
	records []checkpointing.EvolutionRecord,
	language string,
	isSharedRecords bool,
) *stream.OutputSchema {
	// 对齐 Python L79: is_en = _is_en(language)
	en := isEn(language)

	// 对齐 Python L78: questions: List[Dict[str, Any]] = []
	questions := make([]map[string]any, 0, len(records))

	// 对齐 Python L80-83: header 选择
	var header string
	if isSharedRecords {
		// 对齐 Python L81: header = "Shared Experience Approval" if is_en else "在线共享经验审批"
		if en {
			header = "Shared Experience Approval"
		} else {
			header = "在线共享经验审批"
		}
	} else {
		// 对齐 Python L83: header = "Skill Evolution Approval" if is_en else "技能演进审批"
		if en {
			header = "Skill Evolution Approval"
		} else {
			header = "技能演进审批"
		}
	}

	// 对齐 Python L84-116: 遍历 records 构建 questions
	for _, record := range records {
		// 对齐 Python L92: record.change.content[:1000]
		content := record.Change.Content
		if len(content) > 1000 {
			content = content[:1000]
		}

		var question string
		if en {
			// 对齐 Python L88-92（英文模板）:
			//   f"**Skill '{skill_name}' generated a new experience:**\n\n"
			//   f"- **Target**: {record.change.target.value}\n"
			//   f"- **Section**: {record.change.section}\n\n"
			//   f"{record.change.content[:1000]}"
			question = fmt.Sprintf(
				"**Skill '%s' generated a new experience:**\n\n- **Target**: %s\n- **Section**: %s\n\n%s",
				skillName, string(record.Change.Target), record.Change.Section, content,
			)
		} else {
			// 对齐 Python L96-99（中文模板）:
			//   f"**Skill '{skill_name}' 演进生成了新经验：**\n\n"
			//   f"- **目标**: {record.change.target.value}\n"
			//   f"- **章节**: {record.change.section}\n\n"
			//   f"{record.change.content[:1000]}"
			question = fmt.Sprintf(
				"**Skill '%s' 演进生成了新经验：**\n\n- **目标**: %s\n- **章节**: %s\n\n%s",
				skillName, string(record.Change.Target), record.Change.Section, content,
			)
		}

		// 对齐 Python L103-112: options 双语
		var options []map[string]string
		if en {
			// 对齐 Python L105-106
			options = []map[string]string{
				{"label": "Accept", "description": "Keep this evolution experience"},
				{"label": "Reject", "description": "Discard this evolution experience"},
			}
		} else {
			// 对齐 Python L110-111
			options = []map[string]string{
				{"label": "接收", "description": "保留此演进经验"},
				{"label": "拒绝", "description": "丢弃此演进经验"},
			}
		}

		// 对齐 Python L114: "multi_select": False
		questions = append(questions, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	// 对齐 Python L118-123: evolution_meta 构造
	//   source="experience_sharing" if is_shared_records else None
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

	// 对齐 Python L124-125: if is_shared_records: evolution_meta["is_shared_records"] = "true"
	if isSharedRecords {
		meta["is_shared_records"] = "true"
	}

	// 对齐 Python L127-135: return OutputSchema(...)
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
// 对齐 Python: build_simplify_approval_event(skill_name, request_id, actions, language="cn")
func BuildSimplifyApprovalEvent(
	skillName, requestID string,
	actions []map[string]any,
	language string,
) *stream.OutputSchema {
	// 对齐 Python L145: is_en = _is_en(language)
	en := isEn(language)

	// 对齐 Python L146-149: preview 构建
	previewParts := make([]string, 0, len(actions))
	limit := len(actions)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		action := actions[i]
		// 对齐 Python L147: action.get('action', '?')
		act := "?"
		if a, ok := action["action"]; ok {
			if s, ok := a.(string); ok {
				act = s
			}
		}
		// 对齐 Python L147: action.get('record_id', '?')
		recordID := "?"
		if r, ok := action["record_id"]; ok {
			if s, ok := r.(string); ok {
				recordID = s
			}
		}
		// 对齐 Python L147: action.get('reason', '')
		reason := ""
		if r, ok := action["reason"]; ok {
			if s, ok := r.(string); ok {
				reason = s
			}
		}
		// 对齐 Python L147: f"- **{action.get('action', '?')}** `{action.get('record_id', '?')}`: {action.get('reason', '')}"
		previewParts = append(previewParts, fmt.Sprintf("- **%s** `%s`: %s", act, recordID, reason))
	}
	// 对齐 Python L146: preview = "\n".join(...)
	preview := ""
	for i, p := range previewParts {
		if i > 0 {
			preview += "\n"
		}
		preview += p
	}

	var question string
	if en {
		// 对齐 Python L159-162（英文模板）:
		//   f"**Simplify evolution experiences for Skill '{skill_name}'**\n\n"
		//   f"{len(actions)} action(s):\n{preview}\n\n"
		//   "Do you want to execute them?"
		question = fmt.Sprintf(
			"**Simplify evolution experiences for Skill '%s'**\n\n%d action(s):\n%s\n\nDo you want to execute them?",
			skillName, len(actions), preview,
		)
	} else {
		// 对齐 Python L165-168（中文模板）:
		//   f"**精简 Skill '{skill_name}' 的演进经验**\n\n"
		//   f"共 {len(actions)} 项操作：\n{preview}\n\n"
		//   "是否执行？"
		question = fmt.Sprintf(
			"**精简 Skill '%s' 的演进经验**\n\n共 %d 项操作：\n%s\n\n是否执行？",
			skillName, len(actions), preview,
		)
	}

	// 对齐 Python L170-171: header
	var header string
	if en {
		header = "Skill Simplify Approval"
	} else {
		header = "Skill 精简审批"
	}

	// 对齐 Python L173-181: options 双语
	var options []map[string]string
	if en {
		// 对齐 Python L175-176
		options = []map[string]string{
			{"label": "Execute", "description": "Run the simplify actions"},
			{"label": "Cancel", "description": "Discard this simplify request"},
		}
	} else {
		// 对齐 Python L179-180
		options = []map[string]string{
			{"label": "执行", "description": "执行精简操作"},
			{"label": "取消", "description": "放弃本次精简"},
		}
	}

	// 对齐 Python L182: "multi_select": False
	// 对齐 Python L150-186: return OutputSchema(...)
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
// 对齐 Python: build_team_skill_approval_event_from_records(skill_name, request_id, records, language="en")
func BuildTeamSkillApprovalEventFromRecords(
	skillName, requestID, language string,
	records []checkpointing.EvolutionRecord,
) *stream.OutputSchema {
	// 对齐 Python L247-248: 委托 build_team_skill_experience_question_event
	questions := make([]TeamSkillQuestion, 0, len(records))
	for _, record := range records {
		questions = append(questions, TeamSkillQuestion{
			// 对齐 Python L250: section=record.change.section
			Section: record.Change.Section,
			// 对齐 Python L250: content=record.change.content
			Content: record.Change.Content,
		})
	}
	return buildTeamSkillExperienceQuestionEvent(skillName, requestID, language, questions)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isEn 判断语言是否为英文。
// 对齐 Python: _is_en(language: str) -> bool
func isEn(language string) bool {
	// 对齐 Python L14-15: return str(language).lower() == "en"
	return language == "en"
}

// buildTeamSkillExperienceQuestionEvent 从标准化问题输入构建团队技能经验审批事件。
// 对齐 Python: _build_team_skill_experience_question_event(*, skill_name, request_id, questions, language)
func buildTeamSkillExperienceQuestionEvent(
	skillName, requestID, language string,
	questions []TeamSkillQuestion,
) *stream.OutputSchema {
	// 对齐 Python L197: is_en = _is_en(language)
	en := isEn(language)

	// 对齐 Python L198: question_payload = []
	questionPayload := make([]map[string]any, 0, len(questions))

	// 对齐 Python L199-223: 遍历 questions
	for _, q := range questions {
		// 对齐 Python L201: content = question["content"]
		content := q.Content
		// 对齐 Python L204/L207: content[:1000]
		if len(content) > 1000 {
			content = content[:1000]
		}

		var question string
		if en {
			// 对齐 Python L205（英文模板）:
			//   f"**Team Skill '{skill_name}' evolution:**\n\n- **Section**: {section}\n\n{content[:1000]}"
			question = fmt.Sprintf(
				"**Team Skill '%s' evolution:**\n\n- **Section**: %s\n\n%s",
				skillName, q.Section, content,
			)
		} else {
			// 对齐 Python L207（中文模板）:
			//   f"**团队技能 '{skill_name}' 生成了演进经验：**\n\n- **章节**: {section}\n\n{content[:1000]}"
			question = fmt.Sprintf(
				"**团队技能 '%s' 生成了演进经验：**\n\n- **章节**: %s\n\n%s",
				skillName, q.Section, content,
			)
		}

		// 对齐 Python L209-210: header
		var header string
		if en {
			header = "Team Skill Evolution Approval"
		} else {
			header = "团队技能演进审批"
		}

		// 对齐 Python L212-221: options 双语
		var options []map[string]string
		if en {
			// 对齐 Python L214-215
			options = []map[string]string{
				{"label": "Accept", "description": "Keep this evolution"},
				{"label": "Reject", "description": "Discard this evolution"},
			}
		} else {
			// 对齐 Python L218-219
			options = []map[string]string{
				{"label": "接收", "description": "保留此演进经验"},
				{"label": "拒绝", "description": "丢弃此演进经验"},
			}
		}

		// 对齐 Python L222: "multi_select": False
		questionPayload = append(questionPayload, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	// 对齐 Python L228-234: evolution_meta 构造
	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindApproval,
		SkillName: &skillName,
		RequestID: &requestID,
	}.ToPayload()

	// 对齐 Python L225-237: return OutputSchema(...)
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
