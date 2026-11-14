package recommendation

// topic_interest_feedback.go 一比一对齐上游 topic_product.py:5535-6368 的
// InterestFeedback（一个 scope 的负偏好账本与方向状态机）及其行读取/方向
// 匹配助手（⤴️31c 归位：随 topic_product 主体迁移时移入对应文件）。

import (
	"sort"
	"strings"
	"sync"
)

// ──────────────────────────── 常量 ────────────────────────────

// 全局否定先隐藏一段时间再降级放回；第三次永久（spec D10 revision 65）。
// 按在力 strike 数索引。
var (
	// DismissalHiddenDays 否定隐藏天数（topic_product.py:5621）
	DismissalHiddenDays = [2]int{7, 30}
	// DismissalReducedUntilDays 降级放回持续天数（:5622）
	DismissalReducedUntilDays = [2]int{30, 90}
	// DismissalPermanentStrikes 永久 strike 次数（:5623）
	DismissalPermanentStrikes = 3
	// InterestPauseDays 暂停兴趣天数（:5624）
	InterestPauseDays = 7
	// DaySeconds 一天秒数（:5625）
	DaySeconds = 86400.0
)

// statusOrderInForce 方向状态序（:5626）与在力状态集（:5627）。
var statusOrder = []string{"available", "reduced", "hidden", "permanent"}

// hostOnlyDismissalFields 宿主保留的计数字段；模型看到的否定不含它们（:5633-5637）。
var hostOnlyDismissalFields = map[string]bool{
	"dismissed_at": true, "evidence_session_ids": true, "news_source_url": true,
	"news_event": true, "news_match": true, "dismissal_kind": true,
	"record_id": true, "card_id": true, "exposure_id": true,
	"card_revision_id": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// DismissedDirection 一个方向截至反馈时点的否定状态
// （topic_product.py:5641-5664 DismissedDirection）。
type DismissedDirection struct {
	// Key 方向键（direction/unit/events 三形态）
	Key []string
	// UnitID 当前持有该方向的 Memory 单元
	UnitID string
	// Evidence 该方向的证据事件
	Evidence map[string]bool
	// Records 时间线上的否定/暂停记录
	Records []map[string]any
	// Status 当前状态
	Status string
	// PermanentSince 永久起点（非 permanent 为 nil）
	PermanentSince any
	// DirectionAnchor 具名方向锚点（无名方向为空）
	DirectionAnchor string
	// HeldUnits 暂停或隐藏/永久方向持有的单元
	HeldUnits map[string]bool
	// HeldEvidence 持有的证据事件
	HeldEvidence map[string]bool
}

// DirectionID 方向 ID（对齐 :5666 direction_id 属性）。
func (d *DismissedDirection) DirectionID() string { return strings.Join(d.Key, "|") }

// NaturalConversation 自然完结会话携带的源事件与新见证（:5689 注释）。
type NaturalConversation struct {
	// CompletedAt 完结时间
	CompletedAt float64
	// Events 源事件（memory_evidence_id 形态）
	Events map[string]bool
	// Sources 新的自然用户见证（时间, 文本）
	Sources [][2]any
}

// OpenedCard 点开的卡片携带时间、单元、源事件与用户兴趣方向（:5691）。
type OpenedCard struct {
	// OpenedAt 点开时间
	OpenedAt float64
	// Units 卡片来源单元
	Units map[string]bool
	// Events 兴趣的会话事件
	Events map[string]bool
	// Direction 兴趣方向
	Direction string
}

// InterestFeedback 一个 scope 的否定账本，带把兴趣放进单元的 Memory 证据
// （topic_product.py:5685-5700 InterestFeedback）。
type InterestFeedback struct {
	// Now 反馈时点
	Now float64
	// NegativePreferences 本 scope 精确的负偏好记录
	NegativePreferences []map[string]any
	// EvidenceByUnit 每单元的证据（memory_evidence_id 集合）
	EvidenceByUnit map[string]map[string]bool
	// SessionsByUnit 每单元抽取证据的会话集合
	SessionsByUnit map[string]map[string]bool
	// NaturalConversations 自然完结：源事件与新见证及其观察时间
	NaturalConversations []NaturalConversation
	// OpenedCards 点开的卡：时间、单元、源事件与方向
	OpenedCards []OpenedCard

	standingOnce  sync.Once
	standingValue *interestStanding
}

// interestStanding 缓存的账本投影（:5708-5716 _Standing）。
type interestStanding struct {
	inForce           []map[string]any
	dismissedCards    []map[string]any
	directions        map[string]*DismissedDirection
	directionOrder    []string
	blockedNewsUrls   map[string]bool
	blockedNewsEvents map[string]bool
	asWrittenUnits    map[string]bool
	asWrittenEvents   map[string]bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// InterestFeedbackFromState 从一个 v4 状态构造反馈
// （topic_product.py:5702-5735 from_state）。
func InterestFeedbackFromState(state map[string]any, contextScope string,
	projectID any, now float64) *InterestFeedback {
	var sidecar map[string]any
	if value, ok := state["memory_sidecar"].(map[string]any); ok {
		sidecar = value
	}
	var preferences any
	if sidecar != nil {
		preferences = sidecar["negative_preferences"]
	}
	return &InterestFeedback{
		NegativePreferences: exactScopedNegativePreferences(preferences, contextScope, projectID),
		EvidenceByUnit:      memoryEvidenceByUnit(state),
		SessionsByUnit:      MemorySessionsByUnit(state),
		Now:                 now,
	}
}

// InterestFeedbackLoad 反馈在 now 时点的形态，带首个计时否定以来重置的
// strike（topic_product.py:5737-5791 load）。
func InterestFeedbackLoad(store *TopicStarterStore, state map[string]any,
	contextScope string, projectID any, now float64) (*InterestFeedback, error) {
	feedback := InterestFeedbackFromState(state, contextScope, projectID, now)
	since := -1.0
	for _, preference := range feedback.NegativePreferences {
		if !isDismissal(preference) {
			continue
		}
		if dismissedAt, present := preference["dismissed_at"]; present {
			value := toFloatOr(dismissedAt, 0)
			if since < 0 || value < since {
				since = value
			}
		}
	}
	if since < 0 {
		return feedback, nil
	}
	// strike 按 scope 计数，谈论方向的重置也按 scope。
	var naturalConversations []NaturalConversation
	rows, err := rowsSince(store, "session_summary", since,
		map[string]any{"context_scope": contextScope})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		rowPayload, _, ok := payloadMap(row["payload"])
		if !ok {
			continue
		}
		if rowPayload["natural_revisit_eligible"] != true {
			continue
		}
		completedAt := toFloatOr(rowPayload["completed_at"], toFloatOr(row["created_at"], 0))
		events := map[string]bool{}
		for _, eventID := range pyListStrings(rowPayload["source_event_ids"]) {
			events[MemoryEvidenceID(pyStr(rowPayload["session_id"]), eventID)] = true
		}
		naturalConversations = append(naturalConversations, NaturalConversation{
			CompletedAt: completedAt,
			Events:      events,
			Sources:     naturalDirectionSources(rowPayload, state, since),
		})
	}
	var openedCards []OpenCardTuple
	clickRows, err := rowsSince(store, "topic_click", since,
		map[string]any{"context_scope": contextScope, "project_id": projectID})
	if err != nil {
		return nil, err
	}
	for _, row := range clickRows {
		if opened := openedCard(store, row); opened != nil {
			openedCards = append(openedCards, *opened)
		}
	}
	feedback.NaturalConversations = naturalConversations
	feedback.OpenedCards = toOpenedCards(openedCards)
	return feedback, nil
}

// Version scope 内的偏好只追加，用数量版本化（:5793-5797）。
func (f *InterestFeedback) Version() int { return len(f.NegativePreferences) }

// Payloads scope 偏好的普通字典形态（:5799-5802）。
func (f *InterestFeedback) Payloads() []map[string]any {
	out := make([]map[string]any, 0, len(f.NegativePreferences))
	for _, preference := range f.NegativePreferences {
		copied := map[string]any{}
		for key, value := range preference {
			copied[key] = value
		}
		out = append(out, copied)
	}
	return out
}

// ModelPayloads 在力否定的模型可见形态，剔除宿主计数字段
// （:5804-5814）。dismissedCards=false 时 RID 只读方向否定。
func (f *InterestFeedback) ModelPayloads(dismissedCards bool) []map[string]any {
	standing := f.standing()
	records := append([]map[string]any(nil), standing.inForce...)
	if dismissedCards {
		records = append(records, standing.dismissedCards...)
	}
	out := make([]map[string]any, 0, len(records))
	for _, preference := range records {
		filtered := map[string]any{}
		for key, value := range preference {
			if !hostOnlyDismissalFields[key] {
				filtered[key] = value
			}
		}
		out = append(out, filtered)
	}
	return out
}

// DirectionPayloads 已知方向与当前层级（:5816-5820）。
func (f *InterestFeedback) DirectionPayloads() []map[string]string {
	standing := f.standing()
	out := []map[string]string{}
	for _, key := range standing.directionOrder {
		item := standing.directions[key]
		if item.DirectionAnchor != "" {
			out = append(out, map[string]string{
				"direction": item.DirectionAnchor, "status": item.Status,
			})
		}
	}
	return out
}

// AsWritten 某负偏好版本的池写入时点在力的反馈（:5822-5825）。
func (f *InterestFeedback) AsWritten(version int, at float64) *InterestFeedback {
	cut := version
	if cut > len(f.NegativePreferences) {
		cut = len(f.NegativePreferences)
	}
	return &InterestFeedback{
		Now:                  at,
		NegativePreferences:  append([]map[string]any(nil), f.NegativePreferences[:cut]...),
		EvidenceByUnit:       f.EvidenceByUnit,
		SessionsByUnit:       f.SessionsByUnit,
		NaturalConversations: f.NaturalConversations,
		OpenedCards:          f.OpenedCards,
	}
}

// StillWanted 页面展示口径下没有在力否定命中该卡（:6110-6142）。
func (f *InterestFeedback) StillWanted(card map[string]any) bool {
	standing := f.standing()
	bundle, _, _ := payloadMap(card["evidence_bundle"])
	if CardIsNews(card) && f.BlocksNews(pyStr(bundle["source_url"])) {
		return false
	}
	// 暂停兴趣持有其单元的卡，无论当初按什么方向名写入。
	for _, unit := range pyListStrings(card["source_memory_unit_ids"]) {
		if f.heldUnits()[unit] {
			return false
		}
	}
	// 携带卡按自己的卡 ID 重规划。
	cardID := pyStr(card["card_id"])
	if cardID == "" {
		cardID = pyStr(card["candidate_id"])
	}
	if cardID != "" {
		for _, record := range standing.dismissedCards {
			if pyStr(record["card_id"]) == cardID {
				return false
			}
		}
	}
	candidate := map[string]any{
		"intent":                  card["intent"],
		"persona":                 card["title"],
		"primary_query":           card["prompt"],
		"fallback_query":          card["title"],
		"source_memory_unit_ids":  anySliceToStrings(card["source_memory_unit_ids"]),
	}
	var legacy []map[string]any
	for _, record := range append(append([]map[string]any(nil), standing.inForce...), standing.dismissedCards...) {
		if pyStr(record["direction_anchor"]) == "" {
			legacy = append(legacy, record)
		}
	}
	survivors, _ := ApplyNegativePreferences([]map[string]any{candidate}, legacy)
	if len(survivors) > 0 {
		var directional []map[string]any
		for _, record := range standing.inForce {
			if pyStr(record["direction_anchor"]) != "" {
				updated := map[string]any{}
				for key, value := range record {
					updated[key] = value
				}
				updated["source_id"] = ""
				directional = append(directional, updated)
			}
		}
		directionCandidate := candidate
		if card["direction"] != nil {
			directionCandidate = map[string]any{"identity_anchor": card["direction"]}
		}
		survivors, _ = ApplyNegativePreferences([]map[string]any{directionCandidate}, directional)
	}
	return len(survivors) > 0
}

// BlocksNews 该来源的新闻卡被否定：事件永久不回（:6144-6147）。
func (f *InterestFeedback) BlocksNews(sourceURL string) bool {
	return f.standing().blockedNewsUrls[sourceURL]
}

// BlocksNewsEvent 热榜事件被否定：其报道与热榜卡都不回（:6149-6152）。
func (f *InterestFeedback) BlocksNewsEvent(event string) bool {
	return f.standing().blockedNewsEvents[event]
}

// Withheld 完全由用户否定内容构成的会话兴趣（:6154-6173）。
func (f *InterestFeedback) Withheld(eventsByInterest map[string][]string) map[string]bool {
	standing := f.standing()
	dismissed := map[string]bool{}
	for unitID := range standing.asWrittenUnits {
		for event := range f.EvidenceOf([]string{unitID}) {
			dismissed[event] = true
		}
	}
	for event := range standing.asWrittenEvents {
		dismissed[event] = true
	}
	for event := range f.evidenceWith(inForceStatuses()) {
		dismissed[event] = true
	}
	result := map[string]bool{}
	for interestID, events := range eventsByInterest {
		if withinEvents(events, dismissed) {
			result[interestID] = true
		}
	}
	return result
}

// StatusOf 兴趣或卡所属方向的层级（:6175-6186）。
func (f *InterestFeedback) StatusOf(unitIDs []string, events []string, direction string) string {
	standing := f.standing()
	best := -1
	status := "available"
	for _, key := range standing.directionOrder {
		item := standing.directions[key]
		var matched bool
		if item.DirectionAnchor != "" {
			matched = MatchesDirection(direction, item.DirectionAnchor)
		} else {
			unitSet := stringSet(unitIDs)
			matched = (item.UnitID != "" && unitSet[item.UnitID]) || withinEvents(events, item.Evidence)
		}
		if !matched {
			continue
		}
		if index := statusIndex(item.Status); index > best {
			best = index
			status = item.Status
		}
	}
	return status
}

// Admitted 池中没有否定把它拿下的卡（:6188-6205）。全局页才会从会话摘要
// 构卡，项目卡只延续项目自己的会话。
func (f *InterestFeedback) Admitted(manifest map[string]any, cards []map[string]any) []map[string]any {
	withheld := map[string]bool{}
	if pyStr(manifest["context_scope"]) == GlobalNewChatScope {
		withheld = f.Withheld(InterestEvidenceIds(manifest))
	}
	result := []map[string]any{}
	for _, card := range cards {
		if !f.StillWanted(card) {
			continue
		}
		if withheld[pyStr(card["interest_summary_id"])] {
			continue
		}
		result = append(result, card)
	}
	return result
}

// Reduced 方向回归但降级的卡修订；新闻卡保持首位（:6207-6225）。
func (f *InterestFeedback) Reduced(manifest map[string]any, cards []map[string]any) map[string]bool {
	if pyStr(manifest["context_scope"]) != GlobalNewChatScope ||
		len(f.directionsWith([]string{"reduced"})) == 0 {
		return map[string]bool{}
	}
	eventsByInterest := InterestEvidenceIds(manifest)
	reduced := map[string]bool{}
	for _, card := range cards {
		if CardIsNews(card) {
			continue
		}
		status := f.StatusOf(anySliceToStrings(card["source_memory_unit_ids"]),
			eventsByInterest[pyStr(card["interest_summary_id"])],
			pyStrOr(card["direction"], pyStr(card["intent"])))
		if status != "reduced" {
			continue
		}
		reduced[pyStr(card["card_revision_id"])] = true
	}
	return reduced
}

// HoldingUnits 证据涵盖会话兴趣全部事件的首个候选单元
// （:6227-6238）。
func (f *InterestFeedback) HoldingUnits(events []string,
	candidates [][]string) []string {
	for _, units := range candidates {
		if withinEvents(events, f.EvidenceOf(units)) {
			return units
		}
	}
	return nil
}

// UnitsSharing 证据包含任一事件的单元（:6240-6249）。
func (f *InterestFeedback) UnitsSharing(events []string) map[string]bool {
	eventSet := stringSet(events)
	result := map[string]bool{}
	for unitID, evidence := range f.EvidenceByUnit {
		for event := range eventSet {
			if evidence[event] {
				result[unitID] = true
				break
			}
		}
	}
	return result
}

// EvidenceOf 单元集合的证据并集（:6107-6108）。
func (f *InterestFeedback) EvidenceOf(unitIDs []string) map[string]bool {
	result := map[string]bool{}
	for _, unitID := range unitIDs {
		for event := range f.EvidenceByUnit[unitID] {
			result[event] = true
		}
	}
	return result
}

// DismissedUnits 当前没有读取方展示其卡的单元（:6061-6067）。
func (f *InterestFeedback) DismissedUnits() map[string]bool {
	standing := f.standing()
	result := map[string]bool{}
	for unitID := range standing.asWrittenUnits {
		result[unitID] = true
	}
	for _, key := range standing.directionOrder {
		item := standing.directions[key]
		if item.UnitID != "" && item.DirectionAnchor == "" && inForceStatusSet[item.Status] {
			result[item.UnitID] = true
		}
	}
	for unitID := range f.heldUnits() {
		result[unitID] = true
	}
	return result
}

// PermanentDirections 永久方向（:6082-6084）。
func (f *InterestFeedback) PermanentDirections() []*DismissedDirection {
	return f.directionsWith([]string{"permanent"})
}

// RestoreRecord 解除方向的记录：strike 清零并可回归（:6086-6099）。
func (f *InterestFeedback) RestoreRecord(direction *DismissedDirection, at float64) map[string]any {
	record := map[string]any{
		"context_scope":        GlobalNewChatScope,
		"project_id":           nil,
		"reason":               "restored",
		"restored_at":          at,
		"source_id":            direction.UnitID,
		"evidence_session_ids": sortedSet(f.SessionsByUnit[direction.UnitID]),
	}
	if direction.UnitID == "" {
		record["source_event_ids"] = sortedSet(direction.Evidence)
	}
	if direction.DirectionAnchor != "" {
		record["direction_anchor"] = direction.DirectionAnchor
		record["direction"] = direction.DirectionAnchor
	}
	return record
}

// DismissedCardTitles 被否定且未撤销的卡的问题（:6101-6105）。
func (f *InterestFeedback) DismissedCardTitles() []string {
	var titles []string
	for _, record := range f.standing().dismissedCards {
		for _, alias := range pyListStrings(record["aliases"]) {
			titles = append(titles, alias)
		}
	}
	return titles
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// OpenCardTuple _opened_card 的四元组（:6305-6333）。
type OpenCardTuple struct {
	openedAt  float64
	units     map[string]bool
	events    map[string]bool
	direction string
}

// standing 惰性账本投影（对齐 :5874 cached_property）。
func (f *InterestFeedback) standing() *interestStanding {
	f.standingOnce.Do(func() { f.standingValue = f.computeStanding() })
	return f.standingValue
}

// computeStanding 账本时间线推导（对齐 :5875-6056 _standing）。
func (f *InterestFeedback) computeStanding() *interestStanding {
	var inForce []map[string]any
	var dismissedCards []map[string]any
	asWrittenUnits := map[string]bool{}
	asWrittenEvents := map[string]bool{}
	timeline := map[string][]timelineEntry{}
	var timelineOrder []string
	withdrawn := map[string]bool{}
	for _, record := range f.NegativePreferences {
		if pyStr(record["reason"]) == "withdrawn" && pyStr(record["withdraws"]) != "" {
			withdrawn[pyStr(record["withdraws"])] = true
		}
	}
	type standingRecord struct {
		record   map[string]any
		recordID int
	}
	var standing []standingRecord
	for index, record := range f.NegativePreferences {
		if !withdrawn[pyStr(record["record_id"])] {
			standing = append(standing, standingRecord{record, index})
		}
	}
	blockedNewsUrls := map[string]bool{}
	blockedNewsEvents := map[string]bool{}
	for _, item := range standing {
		record := item.record
		if isDismissal(record) && pyStr(record["news_source_url"]) != "" {
			blockedNewsUrls[pyStr(record["news_source_url"])] = true
		}
		if isDismissal(record) && pyStr(record["news_event"]) != "" {
			blockedNewsEvents[pyStr(record["news_event"])] = true
		}
	}
	for _, item := range standing {
		record := item.record
		if !isDismissal(record) {
			key := f.directionKey(record)
		if key != "" && pyStr(record["reason"]) == "restored" {
			appendTimeline(timeline, &timelineOrder, key, timelineEntry{
				at: toFloatOr(record["restored_at"], 0), kind: "reset", record: nil})
		}
			continue
		}
		if pyStr(record["dismissal_kind"]) == "card" {
			// 只拿下这张卡（按 ID 与问题）；方向与来源保留。
			dismissedCards = append(dismissedCards, record)
			continue
		}
		if pyStrOr(record["context_scope"], GlobalNewChatScope) != GlobalNewChatScope {
			// 项目卡早于"只拿下卡本身"语义的否定：按问题拿下点名的那张卡，
			// 项目的单元与兴趣保留。
			trimmed := map[string]any{}
			for _, key := range []string{"context_scope", "project_id", "reason", "aliases"} {
				if value, present := record[key]; present {
					trimmed[key] = value
				}
			}
			dismissedCards = append(dismissedCards, trimmed)
			continue
		}
		if pyStr(record["news_match"]) == "closest" {
			// closest 卡只点名新闻事件，不指向用户兴趣方向。
			continue
		}
		var key string
		if pyStr(record["context_scope"]) == GlobalNewChatScope {
			if _, hasTime := record["dismissed_at"]; hasTime {
				key = f.directionKey(record)
			}
		}
		if key == "" && pyStr(record["dismissal_kind"]) == "pause" {
			// 没有方向可暂停：没有别的东西持有这张卡的兴趣。
			continue
		}
		if key == "" {
			// 早于计时写入的否定，或其方向已无单元或剩余会话持有：
			// 只保留写入时所针对的内容，不计入任何方向。
			inForce = append(inForce, record)
			asWrittenUnits[pyStr(record["source_id"])] = true
			for _, event := range pyListStrings(record["source_event_ids"]) {
				asWrittenEvents[event] = true
			}
			continue
		}
		kind := "dismissal"
		if pyStr(record["dismissal_kind"]) == "pause" {
			kind = "pause"
		}
		appendTimeline(timeline, &timelineOrder, key, timelineEntry{
			at: toFloatOr(record["dismissed_at"], 0), kind: kind, record: record,
			recordID: item.recordID})
	}
	directions := map[string]*DismissedDirection{}
	for _, key := range timelineOrder {
		entries := timeline[key]
		directionAnchor := ""
		if strings.HasPrefix(key, "direction\x00") {
			directionAnchor = strings.TrimPrefix(key, "direction\x00")
		}
		var sourceRecords []map[string]any
		for _, entry := range entries {
			if entry.record != nil {
				sourceRecords = append(sourceRecords, entry.record)
			}
		}
		unitID := ""
		if directionAnchor != "" {
			unitID = f.followingUnit(sourceRecords[len(sourceRecords)-1])
		} else if strings.HasPrefix(key, "unit\x00") {
			unitID = strings.TrimPrefix(key, "unit\x00")
		}
		evidence := map[string]bool{}
		if unitID != "" {
			evidence = f.EvidenceByUnit[unitID]
			if evidence == nil {
				evidence = map[string]bool{}
			}
		} else {
			for _, record := range sourceRecords {
				for _, event := range pyListStrings(record["source_event_ids"]) {
					evidence[event] = true
				}
			}
		}
		// 否定后用户自己对方向的会话重置它。
		for _, conversation := range f.NaturalConversations {
			var resets bool
			if directionAnchor != "" {
				lastDismissal := 0.0
				for _, entry := range entries {
					if withholdingKinds[entry.kind] && entry.at < conversation.CompletedAt && entry.at > lastDismissal {
						lastDismissal = entry.at
					}
				}
				for _, source := range conversation.Sources {
					observedAt := toFloatOr(source[0], 0)
					text := pyStr(source[1])
					if observedAt > lastDismissal && MatchesDirection(text, directionAnchor) {
						resets = true
						break
					}
				}
			} else {
				resets = belongsDirection(unitID, evidence, map[string]bool{}, conversation.Events)
			}
			if resets {
				entries = append(entries, timelineEntry{at: conversation.CompletedAt, kind: "reset"})
			}
		}
		// 暂停在用户继续其暂停会话、或自己完成落进暂停单元的会话时结束；
		// 同方向的其他暂停保留。
		var paused []timelineEntry
		for _, entry := range entries {
			if entry.kind == "pause" {
				paused = append(paused, entry)
			}
		}
		for _, conversation := range f.NaturalConversations {
			for _, pauseEntry := range paused {
				if f.resumes(pauseEntry.record, conversation.CompletedAt, conversation.Events, conversation.Sources) {
					entries = append(entries, timelineEntry{
						at: conversation.CompletedAt, kind: "resumed",
						record: pauseEntry.record, recordID: pauseEntry.recordID})
				}
			}
		}
		for _, opened := range f.OpenedCards {
			var openedMatches bool
			if directionAnchor != "" {
				openedMatches = MatchesDirection(opened.Direction, directionAnchor)
			} else {
				openedMatches = belongsDirection(unitID, evidence, opened.Units, opened.Events)
			}
			if openedMatches {
				entries = append(entries, timelineEntry{at: opened.OpenedAt, kind: "opened"})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].at != entries[j].at {
				return entries[i].at < entries[j].at
			}
			return withholdingKinds[entries[i].kind] && !withholdingKinds[entries[j].kind]
		})
		strikes, last, pausedUntil := 0, 0.0, 0.0
		var since any
		type pauseEntry struct {
			until    float64
			record   map[string]any
			recordID int
		}
		var pauses []pauseEntry
		var strikeRecords []map[string]any
		var records []map[string]any
		for _, entry := range entries {
			resetCondition := entry.kind == "reset" || (strikes > 0 &&
				strikes < DismissalPermanentStrikes &&
				entry.at >= last+float64(DismissalHiddenDays[strikes-1])*DaySeconds)
			if entry.kind == "dismissal" {
				strikes++
				last = entry.at
				records = append(records, entry.record)
				strikeRecords = append(strikeRecords, entry.record)
				if strikes == DismissalPermanentStrikes {
					since = entry.at
				}
			} else if entry.kind == "pause" {
				// 暂停不计 strike：每次暂停七天，然后没有了。
				pausedUntil = entry.at + float64(InterestPauseDays)*DaySeconds
				pauses = append(pauses, pauseEntry{pausedUntil, entry.record, entry.recordID})
				records = append(records, entry.record)
			} else if entry.at > f.Now {
				// 本时点之后发生的事还不能重置它。
				continue
			} else if entry.kind == "resumed" {
				var remaining []pauseEntry
				for _, held := range pauses {
					if held.recordID != entry.recordID {
						remaining = append(remaining, held)
					}
				}
				pauses = remaining
				pausedUntil = 0.0
				for _, held := range pauses {
					if held.until > pausedUntil {
						pausedUntil = held.until
					}
				}
			} else if resetCondition {
				strikes, since, strikeRecords = 0, nil, nil
			}
		}
		if len(records) == 0 {
			continue
		}
		{
			status := dismissalStatus(strikes, last, f.Now)
			// 隐藏或永久方向与暂停持有其卡来源的兴趣：现在持有卡的单元的
			// 会话的单元，或从会话摘要构卡的会话事件。
			var held []map[string]any
			for _, heldPause := range pauses {
				if f.Now < heldPause.until {
					held = append(held, heldPause.record)
				}
			}
			if inForceStatusSet[status] {
				held = append(held, strikeRecords...)
			}
			if f.Now < pausedUntil && statusIndex(status) < statusIndex("hidden") {
				status = "hidden"
			}
			var followedUnits []string
			for _, heldRecord := range held {
				if pyStr(heldRecord["source_id"]) == "" {
					continue
				}
				if following := f.followingUnit(heldRecord); following != "" {
					followedUnits = append(followedUnits, following)
				}
			}
			heldUnits := stringSet(followedUnits)
			resumedEvents := map[string]bool{}
			for _, heldRecord := range held {
				if pyStr(heldRecord["source_id"]) != "" {
					continue
				}
				for _, event := range pyListStrings(heldRecord["source_event_ids"]) {
					resumedEvents[event] = true
				}
			}
			heldEvidence := f.EvidenceOf(sortedSetKeys(heldUnits))
			for event := range resumedEvents {
				heldEvidence[event] = true
			}
			directions[key] = &DismissedDirection{
				Key:             splitTimelineKey(key),
				UnitID:          unitID,
				Evidence:        evidence,
				Records:         records,
				Status:          status,
				PermanentSince:  since,
				DirectionAnchor: directionAnchor,
				HeldUnits:       heldUnits,
				HeldEvidence:    heldEvidence,
			}
			if inForceStatusSet[status] {
				for _, record := range records {
					updated := map[string]any{}
					for recordKey, value := range record {
						updated[recordKey] = value
					}
					updated["source_id"] = unitID
					inForce = append(inForce, updated)
				}
			}
		}
	}
	asWrittenUnitsClean := map[string]bool{}
	for unitID := range asWrittenUnits {
		if unitID != "" {
			asWrittenUnitsClean[unitID] = true
		}
	}
	return &interestStanding{
		inForce:           inForce,
		dismissedCards:    dismissedCards,
		directions:        directions,
		directionOrder:    timelineOrder,
		blockedNewsUrls:   blockedNewsUrls,
		blockedNewsEvents: blockedNewsEvents,
		asWrittenUnits:    asWrittenUnitsClean,
		asWrittenEvents:   asWrittenEvents,
	}
}

// directionsWith 处于给定状态的方向（:6058-6059）。
func (f *InterestFeedback) directionsWith(statuses []string) []*DismissedDirection {
	statusSet := stringSet(statuses)
	standing := f.standing()
	var result []*DismissedDirection
	for _, key := range standing.directionOrder {
		if statusSet[standing.directions[key].Status] {
			result = append(result, standing.directions[key])
		}
	}
	return result
}

// heldUnits 在力方向持有的单元（:6078-6080 cached_property）。
func (f *InterestFeedback) heldUnits() map[string]bool {
	result := map[string]bool{}
	for _, item := range f.directionsWith(inForceStatuses()) {
		for unitID := range item.HeldUnits {
			result[unitID] = true
		}
	}
	return result
}

// evidenceWith 整体被持有的证据（:6069-6076）。
func (f *InterestFeedback) evidenceWith(statuses []string) map[string]bool {
	result := map[string]bool{}
	for _, item := range f.directionsWith(statuses) {
		if item.DirectionAnchor == "" {
			for event := range item.Evidence {
				result[event] = true
			}
		}
		for event := range item.HeldEvidence {
			result[event] = true
		}
	}
	return result
}

// followingUnit 当前在画像里持有方向过半会话的单元（:5827-5841）。
func (f *InterestFeedback) followingUnit(record map[string]any) string {
	sourceID := pyStr(record["source_id"])
	present := map[string]bool{}
	for _, recordSession := range pyListStrings(record["evidence_session_ids"]) {
		for _, sessions := range f.SessionsByUnit {
			if sessions[recordSession] {
				present[recordSession] = true
			}
		}
	}
	type holder struct {
		isSource bool
		negative int
		unitID   string
	}
	var holders []holder
	for unitID, sessions := range f.SessionsByUnit {
		overlap := 0
		for session := range present {
			if sessions[session] {
				overlap++
			}
		}
		if len(present) > 0 && 2*overlap > len(present) {
			holders = append(holders, holder{unitID != sourceID, -overlap, unitID})
		}
	}
	sort.Slice(holders, func(i, j int) bool {
		if holders[i].isSource != holders[j].isSource {
			return !holders[i].isSource
		}
		if holders[i].negative != holders[j].negative {
			return holders[i].negative < holders[j].negative
		}
		return holders[i].unitID < holders[j].unitID
	})
	if len(holders) > 0 {
		return holders[0].unitID
	}
	if f.SessionsByUnit[sourceID] != nil {
		return sourceID
	}
	return ""
}

// resumes 暂停后的自然会话延续其会话或落进其单元（:5843-5861）。
func (f *InterestFeedback) resumes(pause map[string]any, completedAt float64,
	events map[string]bool, sources [][2]any) bool {
	pausedAt := toFloatOr(pause["dismissed_at"], 0)
	if completedAt <= pausedAt {
		return false
	}
	sessions := map[string]bool{}
	for _, session := range pyListStrings(pause["evidence_session_ids"]) {
		sessions[session] = true
	}
	sawWitness := false
	for _, source := range sources {
		if toFloatOr(source[0], 0) > pausedAt {
			sawWitness = true
			break
		}
	}
	if sawWitness {
		for event := range events {
			if sessions[strings.SplitN(event, ":", 2)[0]] {
				return true
			}
		}
	}
	unitID := ""
	if pyStr(pause["source_id"]) != "" {
		unitID = f.followingUnit(pause)
	}
	return unitID != "" && withinSet(events, f.EvidenceByUnit[unitID])
}

// directionKey 方向键（:5863-5872）：锚点 / 单元 / 排序事件集。
func (f *InterestFeedback) directionKey(record map[string]any) string {
	if pyStr(record["direction_anchor"]) != "" {
		return "direction\x00" + pyStr(record["direction_anchor"])
	}
	unitID := f.followingUnit(record)
	if unitID != "" {
		return "unit\x00" + unitID
	}
	events := pyListStrings(record["source_event_ids"])
	if len(events) > 0 {
		sort.Strings(events)
		return "events\x00" + strings.Join(events, "\x00")
	}
	return ""
}

// ──────────────────────────── 包级助手（对齐 :6252-6368） ────────────────────────────

// rowsSince since 之后写入的某类型账本行，最新在前（:6252-6277）。
func rowsSince(store *TopicStarterStore, eventType string, since float64,
	payloadEquals map[string]any) ([]map[string]any, error) {
	var rows []map[string]any
	page := 200
	for {
		batch, err := store.QueryEvents(QueryEventsOptions{
			EventType:     eventType,
			PolicyID:      stringPtr(POLICY_ID),
			PayloadEquals: payloadEquals,
			Limit:         page,
			Offset:        len(rows),
			NewestFirst:   true,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range batch {
			if toFloatOr(row["created_at"], 0) <= since {
				return rows, nil
			}
			rows = append(rows, row)
		}
		if len(batch) < page {
			return rows, nil
		}
	}
}

// naturalDirectionSources 摘要精确源 locator 处的新自然用户见证
// （:6280-6302）。
func naturalDirectionSources(summary map[string]any, state map[string]any, since float64) [][2]any {
	history, _ := state["history_ledger"].(map[string]any)
	sessions, _ := history["sessions"].(map[string]any)
	var sources [][2]any
	for _, raw := range pyListMaps(summary["source_locators"]) {
		session, _ := sessions[pyStr(raw["session_id"])].(map[string]any)
		var entries []any
		if session != nil {
			entries, _ = session["entries"].([]any)
		}
		var entry map[string]any
		for _, rawEntry := range entries {
			item, _ := rawEntry.(map[string]any)
			if item == nil || toFloatOr(item["ordinal"], -1) != toFloatOr(raw["ordinal"], -1) {
				continue
			}
			if pythonTruthy(item["retracted"]) {
				continue
			}
			entry = item
			break
		}
		var witness map[string]any
		if entry != nil {
			witness, _ = entry["witness"].(map[string]any)
		}
		if witness == nil {
			continue
		}
		observedAt := toFloatOr(witness["timestamp_or_0"], 0)
		if witness["supported"] == true && pyStr(witness["role"]) == "user" &&
			since < observedAt && observedAt <= toFloatOr(summary["completed_at"], 0) {
			sources = append(sources, [2]any{observedAt, pyStr(witness["content"])})
		}
	}
	return sources
}

// openedCard 卡片点开的时间与兴趣的单元、会话事件（:6305-6333）。
func openedCard(store *TopicStarterStore, click map[string]any) *OpenCardTuple {
	payload, _, ok := payloadMap(click["payload"])
	if !ok {
		return nil
	}
	manifestID := pyStr(payload["manifest_id"])
	cards, err := store.QueryEvents(QueryEventsOptions{
		EventType:   "starter_card",
		SlateID:     stringPtr(manifestID),
		TopicID:     stringPtr(pyStr(payload["card_revision_id"])),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil {
		return nil
	}
	var card map[string]any
	if len(cards) > 0 {
		card, _, _ = payloadMap(cards[0]["payload"])
	}
	if card == nil {
		return nil
	}
	manifests, err := store.QueryEvents(QueryEventsOptions{
		EventType:   "starter_slate_manifest",
		SlateID:     stringPtr(manifestID),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil {
		return nil
	}
	var manifest map[string]any
	if len(manifests) > 0 {
		manifest, _, _ = payloadMap(manifests[0]["payload"])
	}
	events := InterestEvidenceIds(manifest)[pyStr(card["interest_summary_id"])]
	units := map[string]bool{}
	for _, unit := range pyListStrings(card["source_memory_unit_ids"]) {
		units[unit] = true
	}
	eventSet := map[string]bool{}
	for _, event := range events {
		eventSet[event] = true
	}
	return &OpenCardTuple{
		openedAt:  toFloatOr(click["created_at"], 0),
		units:     units,
		events:    eventSet,
		direction: pyStr(card["direction"]),
	}
}

// belongsDirection 卡或会话属于该方向（:6336-6341）。
func belongsDirection(unitID string, evidence map[string]bool,
	units map[string]bool, events map[string]bool) bool {
	if unitID != "" {
		if units[unitID] {
			return true
		}
		return withinSet(events, evidence)
	}
	for event := range events {
		if evidence[event] {
			return true
		}
	}
	return false
}

// MatchesDirection 文本被方向否定命中即无幸存（:6344-6348）。
func MatchesDirection(text string, direction string) bool {
	survivors, _ := ApplyNegativePreferences(
		[]map[string]any{{"identity_anchor": text}},
		[]map[string]any{{"direction": direction, "aliases": []any{}, "anchors": []any{}}},
	)
	return len(survivors) == 0
}

// isDismissal 记录是否否定（:6351-6352）。
func isDismissal(record map[string]any) bool {
	return pyStrOr(record["reason"], "explicit_not_interested") == "explicit_not_interested"
}

// withholdingKinds 持有方向的时间线条目：strike 或暂停（:6356）。
var withholdingKinds = map[string]bool{"dismissal": true, "pause": true}

// dismissalStatus 否定状态的层级推导（:6359-6368）。
func dismissalStatus(strikes int, last, now float64) string {
	if strikes == 0 {
		return "available"
	}
	if strikes >= DismissalPermanentStrikes {
		return "permanent"
	}
	if now < last+float64(DismissalHiddenDays[strikes-1])*DaySeconds {
		return "hidden"
	}
	if now < last+float64(DismissalReducedUntilDays[strikes-1])*DaySeconds {
		return "reduced"
	}
	return "available"
}

// exactScopedNegativePreferences 只保留本 scope/project 对写入的偏好
// （:5606-5620）。
func exactScopedNegativePreferences(preferences any, contextScope string, projectID any) []map[string]any {
	items, ok := preferences.([]any)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, raw := range items {
		preference, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if pyStr(preference["context_scope"]) != contextScope {
			continue
		}
		if _, hasProject := preference["project_id"]; !hasProject {
			continue
		}
		if pyStr(preference["project_id"]) != pyStr(projectID) {
			continue
		}
		copied := map[string]any{}
		for key, value := range preference {
			copied[key] = value
		}
		result = append(result, copied)
	}
	return result
}

// memoryEvidenceByUnit 每单元证据（memory_evidence_id 形态）（:5521-5532）。
func memoryEvidenceByUnit(state map[string]any) map[string]map[string]bool {
	memory, _ := state["memory"].(map[string]any)
	units, _ := memory["units"].([]any)
	result := map[string]map[string]bool{}
	for _, companion := range unitCompanions(units) {
		evidence := map[string]bool{}
		for _, evidenceID := range pyListStrings(companion["evidence_ids"]) {
			evidence[evidenceID] = true
		}
		result[pyStr(companion["unit_id"])] = evidence
	}
	return result
}

// ──────────────────────────── 时间线与小工具 ────────────────────────────

// timelineEntry 方向时间线条目；recordID 是记录在 NegativePreferences 中的
// 下标（Python 用 dict 身份比较暂停与恢复的同一记录）。
type timelineEntry struct {
	at       float64
	kind     string
	record   map[string]any
	recordID int
}

// appendTimeline 追加条目并保持 key 的首次出现序。
func appendTimeline(timeline map[string][]timelineEntry, order *[]string,
	key string, entry timelineEntry) {
	if _, seen := timeline[key]; !seen {
		*order = append(*order, key)
	}
	timeline[key] = append(timeline[key], entry)
}

// splitTimelineKey 还原方向键分片。
func splitTimelineKey(key string) []string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) == 2 {
		if parts[0] == "events" {
			return append(parts, strings.Split(parts[1], "\x00")...)
		}
		return parts
	}
	return []string{key}
}

// inForceStatuses 在力状态集。
func inForceStatuses() []string { return []string{"hidden", "permanent"} }

// inForceStatusSet 在力状态集合（成员判定用）。
var inForceStatusSet = map[string]bool{"hidden": true, "permanent": true}

// statusIndex 状态序号。
func statusIndex(status string) int {
	for index, value := range statusOrder {
		if value == status {
			return index
		}
	}
	return -1
}

// withinEvents 事件集整体落在证据内（:5628-5630 _within）。
func withinEvents(events []string, evidence map[string]bool) bool {
	if len(events) == 0 {
		return false
	}
	for _, event := range events {
		if !evidence[event] {
			return false
		}
	}
	return true
}

// withinSet 同 _within 的集合形态。
func withinSet(events, evidence map[string]bool) bool {
	if len(events) == 0 {
		return false
	}
	for event := range events {
		if !evidence[event] {
			return false
		}
	}
	return true
}

// stringSet 字符串集合。
func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

// sortedSet 集合的排序输出。
func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// sortedSetKeys 集合键的排序输出。
func sortedSetKeys(values map[string]bool) []string { return sortedSet(values) }

// pyListMaps 映射列表（非列表返回 nil）。
func pyListMaps(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// toOpenedCards 打开卡四元组列表转换。
func toOpenedCards(tuples []OpenCardTuple) []OpenedCard {
	out := make([]OpenedCard, 0, len(tuples))
	for _, tuple := range tuples {
		out = append(out, OpenedCard{
			OpenedAt: tuple.openedAt, Units: tuple.units,
			Events: tuple.events, Direction: tuple.direction,
		})
	}
	return out
}
