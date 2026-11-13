package recommendation

// topic_pipeline_c4.go 一比一对齐上游 topic_pipeline.py:2991-3406 的
// 会话摘要读取与画像/讨论投影助手。
// _rid_decision_context（:3292-3383）依赖 InterestFeedback（topic_product.py
// :5685 ⤴️31c），随 InterestFeedback 移植批次落地。

import (
	"fmt"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// SessionSummaryConfigVersion 会话摘要配置版本（topic_pipeline.py:183）
const SessionSummaryConfigVersion = "topic-starter-v6-session-summary-api-v9"

// RIDFeedbackMessageChars RID 近期自然反馈的内容截断长度（topic_pipeline.py:260）
const RIDFeedbackMessageChars = 800

// ──────────────────────────── 导出函数 ────────────────────────────

// LatestSessionSummaries 一个 scope 内每个会话已存的最新摘要
// （topic_pipeline.py:3006-3054）。摘要配置升级会改写同一完成边界的会话，
// 边界后落成的轮次也可能缺席摘要；并列时先取当前配置版本、再取覆盖更多
// 轮次的摘要。
func LatestSessionSummaries(store *TopicStarterStore, scope string,
	sessions map[string]bool) (map[string]map[string]any, error) {
	latest := map[string]map[string]any{}
	if len(sessions) == 0 {
		return latest, nil
	}
	currentPrefix := SessionSummaryConfigVersion + ":"
	offset := 0
	for {
		rows, err := store.QueryEvents(QueryEventsOptions{
			EventType:   "session_summary",
			SlateID:     stringPtr(scope),
			PolicyID:    stringPtr(POLICY_ID),
			Limit:       200,
			Offset:      offset,
			NewestFirst: true,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			payload, _, ok := payloadMap(row["payload"])
			if !ok {
				payload = map[string]any{}
			}
			sessionID := pyStr(payload["session_id"])
			if !sessions[sessionID] {
				continue
			}
			kept := latest[sessionID]
			if kept == nil ||
				(toFloatOr(payload["completed_at"], 0) == toFloatOr(kept["completed_at"], 0) &&
					summaryTieRankGreater(payload, kept, currentPrefix)) {
				latest[sessionID] = payload
			}
		}
		// 摘要按完成时间落库，一页的末尾低于全部已留边界后，后续页不可能再有并列。
		if len(rows) < 200 {
			break
		}
		if len(sessions) == len(latest) {
			minCompleted := -1.0
			for _, keptPayload := range latest {
				completed := toFloatOr(keptPayload["completed_at"], 0)
				if minCompleted < 0 || completed < minCompleted {
					minCompleted = completed
				}
			}
			if toFloatOr(rows[len(rows)-1]["created_at"], 0) < minCompleted {
				break
			}
		}
		offset += len(rows)
	}
	return latest, nil
}

// ProfileProjectDiscussion 全局画像单元背后的项目里已讨论过的内容
// （topic_pipeline.py:3057-3106）。项目兴趣只能经长期 Memory 到达全局
// scope；单元记录的是用户想要什么，而不是之后已回答了什么。
func ProfileProjectDiscussion(store *TopicStarterStore, state map[string]any,
	projectBySession map[string]string,
	projectIdentifiersBySession map[string][]string) ([]map[string]any, error) {
	memory, _ := state["memory"].(map[string]any)
	units, _ := memory["units"].([]any)
	sourceProjects := map[string]bool{}
	for _, raw := range units {
		unit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		companion, ok := unit["companion"].(map[string]any)
		if !ok {
			continue
		}
		for sessionID := range UnitSourceSessionIDs(companion) {
			sessionProjectID := projectBySession[sessionID]
			if !GlobalProjectBuckets[sessionProjectID] {
				sourceProjects[sessionProjectID] = true
			}
		}
	}
	latestBySession := map[string]map[string]any{}
	projects := make([]string, 0, len(sourceProjects))
	for projectID := range sourceProjects {
		projects = append(projects, projectID)
	}
	sort.Strings(projects)
	for _, projectID := range projects {
		// 已删除或移走的会话不再计入这里的讨论。
		sessions := map[string]bool{}
		for sessionID, owner := range projectBySession {
			if owner == projectID {
				sessions[sessionID] = true
			}
		}
		latest, err := LatestSessionSummaries(store, "project:"+projectID, sessions)
		if err != nil {
			return nil, err
		}
		for sessionID, payload := range latest {
			latestBySession[sessionID] = payload
		}
	}
	identifiers := map[string]bool{}
	for sessionID, projectID := range projectBySession {
		if sourceProjects[projectID] {
			for _, identifier := range projectIdentifiersBySession[sessionID] {
				identifiers[identifier] = true
			}
		}
	}
	identifierList := make([]string, 0, len(identifiers))
	for identifier := range identifiers {
		identifierList = append(identifierList, identifier)
	}
	coverage := []map[string]any{}
	payloads := make([]map[string]any, 0, len(latestBySession))
	for _, payload := range latestBySession {
		payloads = append(payloads, payload)
	}
	sort.Slice(payloads, func(i, j int) bool {
		return toFloatOr(payloads[i]["completed_at"], 0) < toFloatOr(payloads[j]["completed_at"], 0)
	})
	for _, payload := range payloads {
		summary := WithoutIdentifiers(payload["summary_text"], identifierList)
		if summary == "" {
			continue
		}
		coverage = append(coverage, map[string]any{
			"summary":            summary,
			"assistance_origin":  pyStrOr(payload["assistance_origin"], "unknown"),
			"completed_on":       LocalDay(toFloatOr(payload["completed_at"], 0)),
			"from_another_scope": true,
		})
	}
	return coverage, nil
}

// ProjectUnitGlobalDiscussion 全局会话里由本项目的画像兴趣开启并已回答的内容
// （topic_pipeline.py:3109-3181）。只有卡片点击开启的会话计入，条目不带
// 会话身份，与全局页的项目读取一致。
func ProjectUnitGlobalDiscussion(store *TopicStarterStore, state map[string]any,
	projectID string, projectBySession map[string]string) ([]map[string]any, error) {
	memory, _ := state["memory"].(map[string]any)
	units, _ := memory["units"].([]any)
	projectUnits := map[string]bool{}
	for _, raw := range units {
		unit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		companion, ok := unit["companion"].(map[string]any)
		if !ok {
			continue
		}
		sourceSessionIDs := UnitSourceSessionIDs(companion)
		belongs := false
		for sessionID := range sourceSessionIDs {
			if projectBySession[sessionID] == projectID {
				belongs = true
				break
			}
		}
		if !belongs {
			continue
		}
		unitID := pyStr(companion["unit_id"])
		if unitID != "" {
			projectUnits[unitID] = true
		}
	}
	if len(projectUnits) == 0 {
		return nil, nil
	}
	opened := map[string]bool{}
	offset := 0
	for {
		rows, err := store.QueryEvents(QueryEventsOptions{
			EventType:     "session_bound",
			PolicyID:      stringPtr(POLICY_ID),
			PayloadEquals: map[string]any{"context_scope": GlobalNewChatScope},
			Limit:         200,
			Offset:        offset,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			sessionID := pyStr(row["session_id"])
			// 已删除或移入项目的会话不再计入。
			if opened[sessionID] {
				continue
			}
			if _, known := projectBySession[sessionID]; !known {
				continue
			}
			if !GlobalProjectBuckets[projectBySession[sessionID]] {
				continue
			}
			cards, err := store.QueryEvents(QueryEventsOptions{
				EventType: "starter_card",
				TopicID:   stringPtr(pyStr(row["topic_id"])),
				PolicyID:  stringPtr(POLICY_ID),
				Limit:     1,
			})
			if err != nil {
				return nil, err
			}
			if len(cards) > 0 {
				payload, _, ok := payloadMap(cards[0]["payload"])
				if !ok {
					payload = map[string]any{}
				}
				unitIDs, _ := payload["source_memory_unit_ids"].([]any)
				for _, unitID := range unitIDs {
					if projectUnits[pyStr(unitID)] {
						opened[sessionID] = true
						break
					}
				}
			}
		}
		if len(rows) < 200 {
			break
		}
		offset += len(rows)
	}
	latestBySession, err := LatestSessionSummaries(store, GlobalNewChatScope, opened)
	if err != nil {
		return nil, err
	}
	payloads := make([]map[string]any, 0, len(latestBySession))
	for _, payload := range latestBySession {
		payloads = append(payloads, payload)
	}
	sort.Slice(payloads, func(i, j int) bool {
		return toFloatOr(payloads[i]["completed_at"], 0) < toFloatOr(payloads[j]["completed_at"], 0)
	})
	result := []map[string]any{}
	for _, payload := range payloads {
		summaryText := pyStr(payload["summary_text"])
		if strings.TrimSpace(summaryText) == "" {
			continue
		}
		result = append(result, map[string]any{
			"summary":            summaryText,
			"assistance_origin":  pyStrOr(payload["assistance_origin"], "unknown"),
			"completed_on":       LocalDay(toFloatOr(payload["completed_at"], 0)),
			"from_another_scope": true,
		})
	}
	return result, nil
}

// DeprojectedLongProfile 把每个 RecGPT-v3 单元投影成一条无 locator 的全局
// RID 候选（topic_pipeline.py:3184-3275）。
func DeprojectedLongProfile(state map[string]any, profileSemanticVersion int,
	projectBySession map[string]string,
	projectIdentifiersBySession map[string][]string) ([]*LongTermProfileInterestInput, error) {
	memory, _ := state["memory"].(map[string]any)
	units, _ := memory["units"].([]any)
	var interests []*LongTermProfileInterestInput
	for _, raw := range units {
		unit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		core, _ := unit["core"].(map[string]any)
		companion, _ := unit["companion"].(map[string]any)
		if core == nil || companion == nil {
			continue
		}
		sourceUnitID := strings.TrimSpace(pyStr(companion["unit_id"]))
		if sourceUnitID == "" {
			continue
		}
		behaviorPattern := strings.TrimSpace(pyStr(core["behavior_pattern"]))
		var interestCluster string
		if _, isTag := TaxonomyTags[behaviorPattern]; isTag {
			interestCluster = behaviorPattern
		} else if strings.HasPrefix(behaviorPattern, "provisional:") {
			interestCluster = "specialized-interest"
		} else {
			interestCluster = "general-interest"
		}
		sourceSessionIDs := UnitSourceSessionIDs(companion)
		globalOnly := len(sourceSessionIDs) > 0
		for sessionID := range sourceSessionIDs {
			if !GlobalProjectBuckets[projectBySession[sessionID]] {
				globalOnly = false
			}
		}
		var summaryText string
		if len(sourceSessionIDs) > 0 {
			sessionIdentifiers := map[string]bool{}
			for sessionID := range sourceSessionIDs {
				for _, identifier := range projectIdentifiersBySession[sessionID] {
					stripped := strings.TrimSpace(identifier)
					if stripped != "" {
						sessionIdentifiers[stripped] = true
					}
				}
			}
			// 闭包按迭代固定身份：global_only 时不剔除任何标识。
			identifiers := []string{sourceUnitID}
			for _, evidenceID := range pyListStrings(companion["evidence_ids"]) {
				if stripped := strings.TrimSpace(evidenceID); stripped != "" {
					identifiers = append(identifiers, stripped)
				}
			}
			for identifier := range sessionIdentifiers {
				identifiers = append(identifiers, identifier)
			}
			known := identifiers
			if globalOnly {
				known = nil
			}
			withoutKnown := func(value any) string {
				return WithoutIdentifiers(value, known)
			}
			preferenceSummary := withoutKnown(core["preference_summary"])
			var preferredEntities []string
			for _, item := range pyListStrings(core["preferred_entities"]) {
				entity := withoutKnown(item)
				if entity != "" {
					preferredEntities = append(preferredEntities, entity)
				}
			}
			temporalActivity := withoutKnown(core["temporal_activity"])
			details := []string{preferenceSummary}
			if len(preferredEntities) > 0 {
				details = append(details, "具体对象："+strings.Join(preferredEntities, "、"))
			}
			if temporalActivity != "" {
				details = append(details, "时间特征："+temporalActivity)
			}
			var parts []string
			for _, item := range details {
				if item != "" {
					parts = append(parts, item)
				}
			}
			summaryText = strings.Join(parts, "；")
		} else {
			summaryText = TaxonomyTags[interestCluster]
			if summaryText == "" {
				summaryText = "专业兴趣"
			}
		}
		if summaryText == "" {
			continue
		}
		interest, err := NewLongTermProfileInterestInput(
			fmt.Sprintf("long-term-profile:%d:%s", profileSemanticVersion, sourceUnitID),
			GlobalNewChatScope, profileSemanticVersion, interestCluster, summaryText,
			[]string{sourceUnitID})
		if err != nil {
			return nil, err
		}
		interests = append(interests, interest)
	}
	return interests, nil
}

// ShortDeltaSummaryIDs 仍等池回答最新问题的摘要
// （topic_pipeline.py:3278-3289）。
func ShortDeltaSummaryIDs(snapshot *V6ContextSnapshot,
	summaries []*SessionSummaryArtifact) []string {
	requiredDeltaEventIDs := ShortTriggerNaturalDeltaEventIDs(snapshot)
	var result []string
	for _, summary := range summaries {
		for _, eventID := range summary.SourceEventIDs {
			if requiredDeltaEventIDs[eventID] {
				result = append(result, summary.SessionSummaryID)
				break
			}
		}
	}
	return result
}

// MessageFromBehavior 行为记录到来源消息（topic_pipeline.py:3386-3406）。
func MessageFromBehavior(behavior CanonicalBehavior, record map[string]any,
	mapping map[string]any, projectID any) *V6SourceMessage {
	eventID := behavior.RecordID
	if eventID == "" {
		eventID = fmt.Sprintf("behavior:%d", behavior.BehaviorIndex)
	}
	intentCluster := pyStr(mapping["tag"])
	if intentCluster == "" {
		intentCluster = "unmapped"
	}
	actionType := pyStr(mapping["action_type"])
	if actionType == "" {
		actionType = "natural_question"
	}
	return &V6SourceMessage{
		EventID:          eventID,
		SessionID:        behavior.SessionID,
		Ordinal:          behavior.Ordinal,
		Content:          behavior.Content,
		ObservedAt:       behavior.TimestampOr0,
		InputOrigin:      pyStrOr(record["input_origin"], "pasted_or_unknown"),
		AssistanceOrigin: pyStrOr(record["assistance_origin"], "unknown"),
		IntentCluster:    intentCluster,
		ActionType:       actionType,
		ProjectID:        projectID,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// summaryTieRankGreater 前者并列秩是否更大（对齐 :2999-3003 元组比较）。
func summaryTieRankGreater(candidate, kept map[string]any, currentPrefix string) bool {
	candPrefix := strings.HasPrefix(pyStr(candidate["summary_config_version"]), currentPrefix)
	keptPrefix := strings.HasPrefix(pyStr(kept["summary_config_version"]), currentPrefix)
	candLen := len(pyListStrings(candidate["source_event_ids"]))
	keptLen := len(pyListStrings(kept["source_event_ids"]))
	if candPrefix != keptPrefix {
		return candPrefix
	}
	return candLen > keptLen
}

// pyListStrings 列表到字符串列表（非列表返回 nil）。
func pyListStrings(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, pyStr(item))
	}
	return out
}
