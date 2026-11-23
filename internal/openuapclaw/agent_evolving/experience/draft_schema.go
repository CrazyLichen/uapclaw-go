package experience

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// summaryLimit 经验摘要最大长度。
	// Python: _SUMMARY_LIMIT (draft_schema.py:20)
	summaryLimit = 160
	// MaxEvolutionReviewProposals 审查 proposals 上限。
	// Python: MAX_EVOLUTION_REVIEW_PROPOSALS (draft_schema.py:21)
	MaxEvolutionReviewProposals = 3
)

// supportedExperienceSubjectKinds 支持的演进 experience subject kinds。
// Python: SUPPORTED_EXPERIENCE_SUBJECT_KINDS (draft_schema.py:24)
var supportedExperienceSubjectKinds = map[string]bool{"skill": true, "swarm-skill": true}

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionSubject 面向 agent 的演进 subject 信封。
// Python: EvolutionSubject (draft_schema.py:36-49)
type EvolutionSubject struct {
	// Kind subject 类型。
	Kind string `json:"kind"`
	// Name subject 名称。
	Name string `json:"name"`
	// Scope 可选作用域。
	Scope map[string]any `json:"scope,omitempty"`
}

// ToPayload 返回稳定的 agent 侧 subject payload。
// Python: EvolutionSubject.to_payload (draft_schema.py:43-49)
func (s EvolutionSubject) ToPayload() map[string]any {
	payload := map[string]any{"kind": s.Kind, "name": s.Name}
	if len(s.Scope) > 0 {
		scope := make(map[string]any, len(s.Scope))
		for k, v := range s.Scope {
			scope[k] = v
		}
		payload["scope"] = scope
	}
	return payload
}

// ExperienceDraft 面向 agent 的经验草稿基类。
// Python: ExperienceDraft (draft_schema.py:97-100)
type ExperienceDraft struct {
	// Subject 归一化后的 subject。
	Subject EvolutionSubject `json:"subject"`
}

// EvolveDraft 归一化 evolve 草稿，含审批与持久化双视图。
// Python: EvolveDraft (draft_schema.py:103-119)
type EvolveDraft struct {
	ExperienceDraft
	// Experiences 归一化经验条目列表。
	Experiences []map[string]any `json:"experiences"`
}

// ApprovalView 返回审批侧视图（含 run 级 evidence refs）。
// Python: EvolveDraft.approval_view (draft_schema.py:112-115)
func (d EvolveDraft) ApprovalView() map[string]any {
	items := make([]map[string]any, len(d.Experiences))
	for i, item := range d.Experiences {
		copied := make(map[string]any, len(item))
		for k, v := range item {
			copied[k] = v
		}
		items[i] = copied
	}
	return map[string]any{"experiences": items}
}

// PersistenceView 返回持久化侧视图（剔除 run 级 evidence refs）。
// Python: EvolveDraft.persistence_view (draft_schema.py:117-123)
func (d EvolveDraft) PersistenceView() map[string]any {
	items := make([]map[string]any, len(d.Experiences))
	for i, item := range d.Experiences {
		persisted := make(map[string]any, len(item))
		for k, v := range item {
			persisted[k] = v
		}
		delete(persisted, "source_refs")
		items[i] = persisted
	}
	return map[string]any{"experiences": items}
}

// SimplifyDraft 归一化精简草稿。
// Python: SimplifyDraft (draft_schema.py:122-136)
type SimplifyDraft struct {
	ExperienceDraft
	// Actions 归一化精简动作列表。
	Actions []map[string]any `json:"actions"`
}

// ApprovalView 返回审批侧精简草稿视图。
// Python: SimplifyDraft.approval_view (draft_schema.py:128-130)
func (d SimplifyDraft) ApprovalView() map[string]any {
	return map[string]any{"actions": copyActionItems(d.Actions)}
}

// PersistenceView 返回持久化侧精简草稿视图。
// Python: SimplifyDraft.persistence_view (draft_schema.py:132-136)
func (d SimplifyDraft) PersistenceView() map[string]any {
	return map[string]any{"actions": copyActionItems(d.Actions)}
}

// copyActionItems 复制动作条目（Python dict(item) 等价）。
func copyActionItems(items []map[string]any) []map[string]any {
	copied := make([]map[string]any, len(items))
	for i, item := range items {
		c := make(map[string]any, len(item))
		for k, v := range item {
			c[k] = v
		}
		copied[i] = c
	}
	return copied
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeEvolutionSubjectKind 归一化 legacy 演进 subject kind 别名。
// Python: normalize_evolution_subject_kind (draft_schema.py:52-58)
func NormalizeEvolutionSubjectKind(kind string) string {
	normalized := strings.TrimSpace(kind)
	if normalized == "team-skill" {
		return "swarm-skill"
	}
	return normalized
}

// SupportedExperienceSubjectKinds 返回支持的演进 experience subject kinds。
// Python: supported_experience_subject_kinds (draft_schema.py:61-64)
func SupportedExperienceSubjectKinds() map[string]bool {
	out := make(map[string]bool, len(supportedExperienceSubjectKinds))
	for k := range supportedExperienceSubjectKinds {
		out[k] = true
	}
	return out
}

// NormalizeSubject 归一化面向 agent 的 subject 信封。
// Python: normalize_subject (draft_schema.py:67-94)
func NormalizeSubject(raw any, allowedKinds map[string]bool) (EvolutionSubject, error) {
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return EvolutionSubject{}, validationError("subject must be an object")
	}
	kind := strings.TrimSpace(asString(rawMap["kind"]))
	if kind == "" {
		return EvolutionSubject{}, validationError("subject.kind is required")
	}
	allowed := allowedKinds
	if len(allowed) == 0 {
		allowed = make(map[string]bool, len(schema.EvolutionSubjectKindValues))
		for _, v := range schema.EvolutionSubjectKindValues {
			allowed[v] = true
		}
	}
	normalizedAllowed := make(map[string]bool, len(allowed))
	for allowedKind := range allowed {
		normalizedAllowed[NormalizeEvolutionSubjectKind(allowedKind)] = true
	}
	normalizedKind := NormalizeEvolutionSubjectKind(kind)
	if !normalizedAllowed[normalizedKind] {
		return EvolutionSubject{}, validationError(fmt.Sprintf("subject.kind must be one of: %s", strings.Join(sortedKeys(allowed), ", ")))
	}
	kind = normalizedKind
	name := strings.TrimSpace(asString(rawMap["name"]))
	if name == "" {
		return EvolutionSubject{}, validationError("subject.name is required")
	}
	scopeValue, hasScope := rawMap["scope"]
	if hasScope && scopeValue != nil {
		if _, isMap := scopeValue.(map[string]any); !isMap {
			return EvolutionSubject{}, validationError("subject.scope must be an object")
		}
		return EvolutionSubject{Kind: kind, Name: name, Scope: scopeValue.(map[string]any)}, nil
	}
	return EvolutionSubject{Kind: kind, Name: name}, nil
}

// NormalizeEvolveDraft 归一化 agent 提交的 evolve 草稿条目。
// Python: normalize_evolve_draft (draft_schema.py:139-180)
func NormalizeEvolveDraft(subject EvolutionSubject, experiences []any) (EvolveDraft, error) {
	if len(experiences) == 0 {
		return EvolveDraft{}, validationError("experiences must be a non-empty list")
	}
	normalized := make([]map[string]any, 0, len(experiences))
	for index, raw := range experiences {
		rawMap, isMap := raw.(map[string]any)
		if !isMap {
			return EvolveDraft{}, validationError(fmt.Sprintf("experience at index %d must be an object", index))
		}
		content := strings.TrimSpace(asString(rawMap["content"]))
		if content == "" {
			return EvolveDraft{}, validationError("experience content is required")
		}
		target := strings.TrimSpace(orDefault(asString(rawMap["target"]), "body"))
		if !schemaTargetValid(target) {
			return EvolveDraft{}, validationError(fmt.Sprintf("invalid target: %s", target))
		}
		item := map[string]any{
			"summary": "",
			"content": content,
			"target":  target,
			"section": "",
			"reason":  strings.TrimSpace(orDefault(asString(rawMap["reason"]), "")),
		}
		summary, err := normalizeSummary(rawMap)
		if err != nil {
			return EvolveDraft{}, err
		}
		item["summary"] = summary
		section, err := normalizeSection(rawMap, target)
		if err != nil {
			return EvolveDraft{}, err
		}
		item["section"] = section
		scriptFilename, err := normalizeScriptFilename(rawMap["script_filename"])
		if err != nil {
			return EvolveDraft{}, err
		}
		if scriptFilename != "" {
			item["script_filename"] = scriptFilename
		}
		for _, key := range []string{"script_language", "script_purpose"} {
			value := strings.TrimSpace(orDefault(asString(rawMap[key]), ""))
			if value != "" {
				item[key] = value
			}
		}
		refs, err := normalizeSourceRefs(rawMap["source_refs"])
		if err != nil {
			return EvolveDraft{}, err
		}
		if len(refs) > 0 {
			item["source_refs"] = refs
		}
		normalized = append(normalized, item)
	}
	return EvolveDraft{ExperienceDraft: ExperienceDraft{Subject: subject}, Experiences: normalized}, nil
}

// NormalizeSimplifyDraft 归一化 agent 提交的精简动作草稿。
// Python: normalize_simplify_draft (draft_schema.py:183-230)
func NormalizeSimplifyDraft(subject EvolutionSubject, actions []any) (SimplifyDraft, error) {
	if len(actions) == 0 {
		return SimplifyDraft{}, validationError("actions must be a non-empty list")
	}
	normalized := make([]map[string]any, 0, len(actions))
	for index, raw := range actions {
		rawMap, isMap := raw.(map[string]any)
		if !isMap {
			return SimplifyDraft{}, validationError(fmt.Sprintf("action at index %d must be an object", index))
		}
		action := strings.ToUpper(strings.TrimSpace(asString(rawMap["action"])))
		if !simplifyActionValid(action) {
			return SimplifyDraft{}, validationError(fmt.Sprintf("invalid simplify action: %s", action))
		}
		recordID := strings.TrimSpace(asString(rawMap["record_id"]))
		if recordID == "" {
			return SimplifyDraft{}, validationError("record_id is required")
		}
		item := map[string]any{
			"action":    action,
			"record_id": recordID,
			"reason":    strings.TrimSpace(orDefault(asString(rawMap["reason"]), "")),
		}
		newContent := strings.TrimSpace(orDefault(asString(rawMap["new_content"]), ""))
		rawMergeIDs, _ := rawMap["merge_remove_ids"].([]any)
		mergeRemoveIDs, err := normalizeMergeRemoveIDs(rawMergeIDs, recordID)
		if err != nil {
			return SimplifyDraft{}, err
		}
		if action == "REFINE" && newContent == "" {
			return SimplifyDraft{}, validationError("REFINE requires new_content")
		}
		if action == "MERGE" {
			if newContent == "" {
				return SimplifyDraft{}, validationError("MERGE requires new_content")
			}
			if len(mergeRemoveIDs) == 0 {
				return SimplifyDraft{}, validationError("MERGE requires merge_remove_ids")
			}
		}
		if newContent != "" && (action == "REFINE" || action == "MERGE") {
			item["new_content"] = newContent
		}
		if len(mergeRemoveIDs) > 0 {
			item["merge_remove_ids"] = mergeRemoveIDs
		}
		normalized = append(normalized, item)
	}
	return SimplifyDraft{ExperienceDraft: ExperienceDraft{Subject: subject}, Actions: normalized}, nil
}

// ValidateSimplifyRecordRefs 校验精简动作 record 引用与已持久化 record id。
// Python: validate_simplify_record_refs (draft_schema.py:233-243)
func ValidateSimplifyRecordRefs(draft SimplifyDraft, existingRecordIDs map[string]bool) error {
	for _, action := range draft.Actions {
		recordID := asString(action["record_id"])
		if !existingRecordIDs[recordID] {
			return validationError(fmt.Sprintf("record not found: %s", recordID))
		}
		ids, _ := action["merge_remove_ids"].([]string)
		for _, removeID := range ids {
			if !existingRecordIDs[removeID] {
				return validationError(fmt.Sprintf("record not found: %s", removeID))
			}
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validationError 构造校验失败错误。
// Python: _validation_error (draft_schema.py:27-32，ValidationError/SCHEMA_VALIDATE_INVALID)
func validationError(message string) error {
	return exception.NewBaseError(exception.StatusSchemaValidateInvalid,
		exception.WithMsg(message))
}

// schemaTargetValid 校验 target 是否合法。
// Python: target not in _VALID_TARGETS (draft_schema.py:141)
func schemaTargetValid(target string) bool {
	for _, v := range schema.EvolutionTargetValues {
		if v == target {
			return true
		}
	}
	return false
}

// simplifyActionValid 校验精简动作是否合法。
// Python: action not in _SIMPLIFY_ACTIONS (draft_schema.py:190)
func simplifyActionValid(action string) bool {
	for _, v := range schema.SimplifyActionValues {
		if v == action {
			return true
		}
	}
	return false
}

// normalizeSummary 归一化经验摘要。
// Python: _normalize_summary (draft_schema.py:246-255)
func normalizeSummary(raw map[string]any) (string, error) {
	summary := strings.Join(strings.Fields(asString(raw["summary"])), " ")
	if summary == "" {
		return "", validationError("experience summary is required")
	}
	if len([]rune(summary)) > summaryLimit {
		return "", validationError(fmt.Sprintf("experience summary must be at most %d characters", summaryLimit))
	}
	return summary, nil
}

// normalizeSection 归一化 SKILL.md section。
// Python: _normalize_section (draft_schema.py:258-268)
func normalizeSection(raw map[string]any, target string) (string, error) {
	if target == "script" {
		return "Scripts", nil
	}
	defaultSection := "Troubleshooting"
	if target == "description" {
		defaultSection = "Instructions"
	}
	section := strings.TrimSpace(orDefault(asString(raw["section"]), defaultSection))
	if section == "" {
		section = defaultSection
	}
	if !schema.ValidSections[section] {
		return "", validationError(fmt.Sprintf("invalid section: %s", section))
	}
	return section, nil
}

// normalizeScriptFilename 校验 script 文件名必须为纯文件名。
// Python: _normalize_script_filename (draft_schema.py:271-285)
// 偏差 D-15.21-2：Python 用 PurePosixPath/PureWindowsPath，Go 以字符串判据等价
// 实现（绝对路径前缀/盘符/UNC/路径分隔符/base 名相等性）。
func normalizeScriptFilename(value any) (string, error) {
	filename := strings.TrimSpace(asString(value))
	if filename == "" {
		return "", nil
	}
	if isAbsolutePosixPath(filename) || isAbsoluteWindowsPath(filename) {
		return "", validationError("script_filename must be a file name")
	}
	if filename == "." || filename == ".." {
		return "", validationError("script_filename must be a file name")
	}
	if strings.ContainsAny(filename, "/\\") {
		return "", validationError("script_filename must be a file name")
	}
	return filename, nil
}

// isAbsolutePosixPath 判断 POSIX 绝对路径。
// Python: PurePosixPath(filename).is_absolute()
func isAbsolutePosixPath(p string) bool {
	return strings.HasPrefix(p, "/")
}

// isAbsoluteWindowsPath 判断 Windows 绝对路径（盘符或 UNC）。
// Python: PureWindowsPath(filename).is_absolute()
func isAbsoluteWindowsPath(p string) bool {
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return true
	}
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		// 盘符后必须有分隔符才算绝对路径（"C:" 相对盘符路径除外）
		drive := p[0]
		return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
	}
	return false
}

// normalizeSourceRefs 归一化 source refs 列表。
// Python: _normalize_source_refs (draft_schema.py:288-300)
func normalizeSourceRefs(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, validationError("source_refs must be a list")
	}
	refs := make([]string, 0, len(list))
	for _, ref := range list {
		normalizedRef := strings.TrimSpace(asString(ref))
		if normalizedRef != "" {
			refs = append(refs, normalizedRef)
		}
	}
	return refs, nil
}

// normalizeMergeRemoveIDs 归一化并校验 merge_remove_ids。
// Python: _normalize_merge_remove_ids (draft_schema.py:303-319)
func normalizeMergeRemoveIDs(rawIDs []any, recordID string) ([]string, error) {
	seen := make(map[string]bool)
	normalized := make([]string, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		removeID := strings.TrimSpace(asString(rawID))
		if removeID == "" {
			return nil, validationError("merge_remove_ids must not contain empty values")
		}
		if removeID == recordID {
			return nil, validationError("merge_remove_ids must not contain record_id")
		}
		if seen[removeID] {
			return nil, validationError("duplicate merge_remove_id")
		}
		seen[removeID] = true
		normalized = append(normalized, removeID)
	}
	return normalized, nil
}

// asString 宽松转字符串（Python str(v) 等价；nil → ""）。
func asString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// orDefault 空串回退默认值。
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// sortedKeys 排序集合键（Python sorted(...) 展示顺序）。
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
