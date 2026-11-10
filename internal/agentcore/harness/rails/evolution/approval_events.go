package evolution

import (
	"fmt"
	"strings"

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

	// 最后套上宿主事件元数据，前端据此识别这是演进审批而不是普通提问。
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
	// 按语言决定文案用中文还是英文，下面所有文字都跟着这个开关走。
	en := isEn(language)

	// 一条记录对应一个问题，先按记录数把容器开好。
	questions := make([]map[string]any, 0, len(records))

	// 标题要区分场景：来自共享经验库的和普通技能演进的叫法不一样。
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

	// 逐条记录生成一个问题：正文超过 1000 字就截断，避免弹窗被撑爆。
	for _, record := range records {
		content := record.Change.Content
		if len(content) > 1000 {
			content = content[:1000]
		}

		// 问题正文要写清是哪个技能、改的是哪一块、具体内容是什么。
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

		// 每条只给两个选项：接收就保留这条经验，拒绝就丢掉。
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

		// 把这个问题按统一的问答结构攒进列表，单个问题不做多选。
		questions = append(questions, map[string]any{
			"question":     question,
			"header":       header,
			"options":      options,
			"multi_select": false,
		})
	}

	// 组装附加元信息：标明这是审批事件、属于哪个技能哪个请求；
	// 如果是共享经验，再补上来源和共享标记，供前端区分展示。
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

	// 最终产出给前端的事件：类型是「向用户提问」，带上请求 ID、元信息和所有问题。
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
	// 同样先定语言，后面所有提示文字都按这个来。
	en := isEn(language)

	// 生成操作预览前先限流，最多只列 10 条，免得弹窗太长。
	previewParts := make([]string, 0, len(actions))
	limit := len(actions)
	if limit > 10 {
		limit = 10
	}
	// 每条操作抽三项：动作类型、记录 ID、理由，缺的用 ? 或空串占位。
	// 例如「- **DELETE** `abc`: 内容重复」这样一行，拼起来就是给用户看的预览。
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
	preview := strings.Join(previewParts, "\n")

	// 把预览拼进问句，并说明一共多少条操作，最后问用户要不要执行。
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

	// 标题固定为精简审批。
	var header string
	if en {
		header = "Skill Simplify Approval"
	} else {
		header = "Skill 精简审批"
	}

	// 选项只有执行和取消两个，跟技能审批保持一致。
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

	// 精简审批只有一个问题，直接包进问题列表返回。
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
// Python: str(language).lower() == "en"
func isEn(language string) bool {
	return strings.ToLower(strings.TrimSpace(language)) == "en"
}

// buildTeamSkillExperienceQuestionEvent 从标准化问题输入构建团队技能经验审批事件。
func buildTeamSkillExperienceQuestionEvent(
	skillName, requestID, language string,
	questions []TeamSkillQuestion,
) *stream.OutputSchema {
	en := isEn(language)

	// 每个候选经验转成一条提问：正文截断到 1000 字符，题干、标题、两个选项都按语言出。
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
