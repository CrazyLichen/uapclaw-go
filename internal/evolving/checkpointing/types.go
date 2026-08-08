package checkpointing

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UsageStats 演进经验的使用统计。
//
// 记录演进记录被展示、使用、正/负反馈的次数，
// 用于经验评分和经验淘汰决策。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py UsageStats
type UsageStats struct {
	// TimesPresented 展示次数
	TimesPresented int
	// TimesUsed 使用次数
	TimesUsed int
	// TimesPositive 正反馈次数
	TimesPositive int
	// TimesNegative 负反馈次数
	TimesNegative int
	// LastPresentedAt 最后展示时间（UTC ISO，可选）
	LastPresentedAt *string
	// LastEvaluatedAt 最后评估时间（UTC ISO，可选）
	LastEvaluatedAt *string
}

// EvolutionPatch 单次生成的演进变更。
//
// 由优化器（Optimizer）生成，描述对 SKILL.md 的一个具体变更，
// 包括目标 section、动作类型和变更内容。
// action 属于合法补丁动作集合（append/merge/replace/skip）
// section ∈ VALID_SECTIONS (Instructions/Examples/Troubleshooting/Scripts 等)
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py EvolutionPatch
type EvolutionPatch struct {
	// Section 目标 section (Instructions/Examples/Troubleshooting/Scripts)
	Section string
	// Action 动作 (append/merge/replace/skip)
	Action string
	// Content 变更内容
	Content string
	// Target 演化目标层 (description/body/script)
	Target signal.EvolutionTarget
	// SkipReason skip 时的原因（可选）
	SkipReason *string
	// MergeTarget merge 时替换的目标 record ID（可选）
	MergeTarget *string
	// ScriptFilename 脚本文件名（可选，target=script 时）
	ScriptFilename *string
	// ScriptLanguage 脚本语言（可选，target=script 时）
	ScriptLanguage *string
	// ScriptPurpose 脚本用途（可选，target=script 时）
	ScriptPurpose *string
	// Keywords 关键词（可选）
	Keywords []string
	// Summary 摘要（可选）
	Summary *string
}

// EvolutionRecord 单条持久化的演进记录。
//
// 由 EvolutionPatch 封装为完整记录，包含来源、时间戳、
// 评分和使用统计，持久化于技能目录的 evolutions.json。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py EvolutionRecord
type EvolutionRecord struct {
	// ID 记录标识，格式: ev_{uuid8}
	ID string
	// Source 来源标识
	Source string
	// Timestamp UTC ISO 时间戳
	Timestamp string
	// Context 上下文描述
	Context string
	// Change 变更内容（EvolutionPatch）
	Change EvolutionPatch
	// Applied 是否已应用，默认 false
	Applied bool
	// Score 评分，默认 0.6
	Score float64
	// UsageStats 使用统计（可选）
	UsageStats *UsageStats
	// SkillVersion 技能版本（可选）
	SkillVersion *string
	// Summary 摘要（可选）
	Summary *string
}

// EvolutionLog 单个技能的所有演进记录容器。
//
// 持久化于技能目录的 evolutions.json，包含技能标识、
// 版本号、更新时间和记录列表。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/types.py EvolutionLog
type EvolutionLog struct {
	// SkillID 技能标识
	SkillID string
	// Version 版本号，默认 "1.0.0"
	Version string
	// UpdatedAt 更新时间（UTC ISO）
	UpdatedAt string
	// Entries 演进记录列表
	Entries []EvolutionRecord
}

// PendingChange 等待审批的暂存演进记录快照。
//
// 当优化器生成 EvolutionPatch 并经 Operator 预览后，
// 变更以 PendingChange 形式暂存于 DefaultCheckpointManager._pending 中，
// 等待 ExperienceManager 审批后 commit 到 EvolutionStore。
//
// 此类型在 checkpointing 包中定义（而非 experience 包），
// 因为 Payload 依赖 EvolutionRecord（同包类型），
// Go 不允许 checkpointing ↔ experience 循环引用。
// experience 包将通过类型别名提供等效访问。
//
// 对应 Python: openjiuwen/agent_evolving/experience/types.py PendingChange
type PendingChange struct {
	// OperatorID Operator 标识符，格式: skill_experience_{skill_name}
	OperatorID string
	// SkillName 技能名称
	SkillName string
	// ChangeType 变更类型，默认 "skill_experience_entry"
	ChangeType string
	// Payload 暂存的演进记录列表
	Payload []EvolutionRecord
	// CreatedAt 创建时间（UTC ISO 格式）
	CreatedAt string
	// ChangeID 变更标识，格式: skill_evolve_{uuid8}
	ChangeID string
	// IsSharedRecords 是否为共享记录
	IsSharedRecords bool
	// Trajectory 关联轨迹（可选）
	Trajectory *trajectory.Trajectory
	// Messages 关联消息列表（可选）
	Messages []map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// evolutionPatchOptionalFields EvolutionPatch 可选字段名称列表。
// 对应 Python: EvolutionPatch._OPTIONAL_FIELDS
var evolutionPatchOptionalFields = []string{
	"skip_reason", "merge_target", "script_filename",
	"script_language", "script_purpose",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionPatch 创建 EvolutionPatch 并验证。
//
// 验证 action ∈ VALID_PATCH_ACTIONS, target 为合法 EvolutionTarget,
// action != "skip" 时 section ∈ VALID_SECTIONS。
// 对应 Python: EvolutionPatch.__post_init__
func NewEvolutionPatch(section, action, content string, target signal.EvolutionTarget) (*EvolutionPatch, error) {
	if !schema.ValidPatchActions[action] {
		return nil, fmt.Errorf("无效的演进补丁动作: %s", action)
	}
	// 对齐 Python: isinstance(self.target, EvolutionTarget) 验证
	if _, err := signal.ParseEvolutionTarget(string(target)); err != nil {
		return nil, fmt.Errorf("无效的演进补丁目标: %s: %w", target, err)
	}
	// 对齐 Python: if self.action == "skip": return — skip 不验证 section
	if action != "skip" && !schema.ValidSections[section] {
		return nil, fmt.Errorf("无效的演进补丁区域: %s", section)
	}
	return &EvolutionPatch{
		Section: section,
		Action:  action,
		Content: content,
		Target:  target,
	}, nil
}

// MakeEvolutionRecord 创建 EvolutionRecord 的工厂方法。
//
// 自动生成 ID (ev_{uuid8}) 和 timestamp (UTC ISO)，
// 初始化 UsageStats 为零值实例。
// 对应 Python: EvolutionRecord.make(source, context, change, *, score, skill_version, summary)
func MakeEvolutionRecord(
	source, context string,
	change EvolutionPatch,
	score float64,
	skillVersion *string,
	summary *string,
) *EvolutionRecord {
	if score == 0 {
		score = 0.6
	}
	return &EvolutionRecord{
		ID:           fmt.Sprintf("ev_%08x", time.Now().UnixNano()&0xFFFFFFFF),
		Source:       source,
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		Context:      context,
		Change:       change,
		Applied:      false,
		Score:        score,
		UsageStats:   &UsageStats{},
		SkillVersion: skillVersion,
		Summary:      summary,
	}
}

// EmptyEvolutionLog 创建空的 EvolutionLog。
//
// 对应 Python: EvolutionLog.empty(skill_id)
func EmptyEvolutionLog(skillID string) *EvolutionLog {
	return &EvolutionLog{
		SkillID:   skillID,
		Version:   "1.0.0",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Entries:   []EvolutionRecord{},
	}
}

// NewPendingChange 创建 PendingChange 的工厂方法。
//
// 对应 Python: PendingChange.make(skill_name, records, *, trajectory, messages)
func NewPendingChange(
	skillName string,
	records []EvolutionRecord,
	traj *trajectory.Trajectory,
	messages []map[string]any,
) *PendingChange {
	// 对齐 Python: messages=list(messages) if messages is not None else None
	var msgCopy []map[string]any
	if messages != nil {
		msgCopy = make([]map[string]any, len(messages))
		for i, m := range messages {
			msgCopy[i] = m
		}
	}
	return &PendingChange{
		OperatorID: fmt.Sprintf("skill_experience_%s", skillName),
		SkillName:  skillName,
		ChangeType: schema.SkillExperienceEntry,
		Payload:    records,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		ChangeID:   fmt.Sprintf("skill_evolve_%s", generateUUID8()),
		Trajectory: traj,
		Messages:   msgCopy,
	}
}

// NewPendingChangeForSharedRecords 创建共享记录的 PendingChange。
//
// 对应 Python: PendingChange.make_for_shared_records(skill_name, records, *, trajectory, messages)
func NewPendingChangeForSharedRecords(
	skillName string,
	records []EvolutionRecord,
	traj *trajectory.Trajectory,
	messages []map[string]any,
) *PendingChange {
	pc := NewPendingChange(skillName, records, traj, messages)
	pc.IsSharedRecords = true
	return pc
}

// IsPending 判断 EvolutionRecord 是否为待定状态。
//
// 对应 Python: EvolutionRecord.is_pending (property)
func (r *EvolutionRecord) IsPending() bool {
	return !r.Applied
}

// PendingEntries 返回 EvolutionLog 中所有待定记录。
//
// 对应 Python: EvolutionLog.pending_entries (property)
func (l *EvolutionLog) PendingEntries() []EvolutionRecord {
	var result []EvolutionRecord
	for _, entry := range l.Entries {
		if entry.IsPending() {
			result = append(result, entry)
		}
	}
	return result
}

// ToDict 将 UsageStats 转换为字典形式。
//
// 对应 Python: UsageStats.to_dict()
func (u *UsageStats) ToDict() map[string]any {
	payload := map[string]any{
		"times_presented": u.TimesPresented,
		"times_used":      u.TimesUsed,
		"times_positive":  u.TimesPositive,
		"times_negative":  u.TimesNegative,
	}
	// 对齐 Python: if self.last_presented_at: payload["last_presented_at"] = ...
	if u.LastPresentedAt != nil && *u.LastPresentedAt != "" {
		payload["last_presented_at"] = *u.LastPresentedAt
	}
	if u.LastEvaluatedAt != nil && *u.LastEvaluatedAt != "" {
		payload["last_evaluated_at"] = *u.LastEvaluatedAt
	}
	return payload
}

// FromDictUsageStats 从字典创建 UsageStats。
//
// 对应 Python: UsageStats.from_dict(data)
func FromDictUsageStats(data map[string]any) *UsageStats {
	if data == nil {
		return &UsageStats{}
	}
	us := &UsageStats{
		TimesPresented: getIntFromAny(data["times_presented"], 0),
		TimesUsed:      getIntFromAny(data["times_used"], 0),
		TimesPositive:  getIntFromAny(data["times_positive"], 0),
		TimesNegative:  getIntFromAny(data["times_negative"], 0),
	}
	if v, ok := data["last_presented_at"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		us.LastPresentedAt = &s
	}
	if v, ok := data["last_evaluated_at"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		us.LastEvaluatedAt = &s
	}
	return us
}

// ToDict 将 EvolutionPatch 转换为字典形式。
//
// 对应 Python: EvolutionPatch.to_dict()
func (p *EvolutionPatch) ToDict() map[string]any {
	payload := map[string]any{
		"section": p.Section,
		"action":  p.Action,
		"content": p.Content,
		"target":  string(p.Target),
	}
	// 对齐 Python: for key in self._OPTIONAL_FIELDS: value = getattr(self, key); if value: payload[key] = value
	for _, key := range evolutionPatchOptionalFields {
		value := p.getOptionalFieldValue(key)
		if value != nil {
			payload[key] = value
		}
	}
	return payload
}

// FromDictEvolutionPatch 从字典创建 EvolutionPatch。
//
// 对应 Python: EvolutionPatch.from_dict(data)
func FromDictEvolutionPatch(data map[string]any) (*EvolutionPatch, error) {
	if data == nil {
		data = map[string]any{}
	}
	rawTarget := getStrFromAny(data["target"], "body")
	target, err := signal.ParseEvolutionTarget(rawTarget)
	if err != nil {
		// 对齐 Python: EvolutionTarget(raw_target) — 不验证，直接创建
		target = signal.EvolutionTarget(rawTarget)
	}
	patch := &EvolutionPatch{
		Section: getStrFromAny(data["section"], "Troubleshooting"),
		Action:  getStrFromAny(data["action"], "append"),
		Content: getStrFromAny(data["content"], ""),
		Target:  target,
	}
	if v, ok := data["skip_reason"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		patch.SkipReason = &s
	}
	if v, ok := data["merge_target"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		patch.MergeTarget = &s
	}
	if v, ok := data["script_filename"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		patch.ScriptFilename = &s
	}
	if v, ok := data["script_language"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		patch.ScriptLanguage = &s
	}
	if v, ok := data["script_purpose"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		patch.ScriptPurpose = &s
	}
	return patch, nil
}

// ToDict 将 EvolutionRecord 转换为字典形式。
//
// 对应 Python: EvolutionRecord.to_dict()
func (r *EvolutionRecord) ToDict() map[string]any {
	payload := map[string]any{
		"id":        r.ID,
		"source":    r.Source,
		"timestamp": r.Timestamp,
		"context":   r.Context,
		"change":    r.Change.ToDict(),
		"applied":   r.Applied,
		"score":     r.Score,
	}
	// 对齐 Python: if self.usage_stats is not None: payload["usage_stats"] = self.usage_stats.to_dict()
	if r.UsageStats != nil {
		payload["usage_stats"] = r.UsageStats.ToDict()
	}
	if r.SkillVersion != nil && *r.SkillVersion != "" {
		payload["skill_version"] = *r.SkillVersion
	}
	// 对齐 Python: if self.summary: payload["summary"] = self.summary
	if r.Summary != nil && *r.Summary != "" {
		payload["summary"] = *r.Summary
	}
	return payload
}

// FromDictEvolutionRecord 从字典创建 EvolutionRecord。
//
// 对应 Python: EvolutionRecord.from_dict(data)
func FromDictEvolutionRecord(data map[string]any) (*EvolutionRecord, error) {
	if data == nil {
		data = map[string]any{}
	}
	// 对齐 Python: change=EvolutionPatch.from_dict(data.get("change", {}))
	changeData, _ := data["change"].(map[string]any)
	change, err := FromDictEvolutionPatch(changeData)
	if err != nil {
		return nil, fmt.Errorf("解析 EvolutionPatch 失败: %w", err)
	}

	// 对齐 Python: usage_stats_data = data.get("usage_stats")
	//   usage_stats = UsageStats.from_dict(usage_stats_data) if usage_stats_data else UsageStats()
	var usageStats *UsageStats
	if v, ok := data["usage_stats"]; ok && v != nil {
		if statsMap, ok := v.(map[string]any); ok {
			usageStats = FromDictUsageStats(statsMap)
		} else {
			usageStats = &UsageStats{}
		}
	} else {
		usageStats = &UsageStats{}
	}

	record := &EvolutionRecord{
		ID:         getStrFromAny(data["id"], fmt.Sprintf("ev_%08x", time.Now().UnixNano()&0xFFFFFFFF)),
		Source:     getStrFromAny(data["source"], "unknown"),
		Timestamp:  getStrFromAny(data["timestamp"], ""),
		Context:    getStrFromAny(data["context"], ""),
		Change:     *change,
		Applied:    getBoolFromAny(data["applied"], false),
		Score:      getFloatFromAny(data["score"], 0.6),
		UsageStats: usageStats,
	}
	if v, ok := data["skill_version"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		record.SkillVersion = &s
	}
	if v, ok := data["summary"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		record.Summary = &s
	}
	return record, nil
}

// ToDict 将 EvolutionLog 转换为字典形式。
//
// 对应 Python: EvolutionLog.to_dict()
func (l *EvolutionLog) ToDict() map[string]any {
	entries := make([]map[string]any, len(l.Entries))
	for i, entry := range l.Entries {
		entries[i] = entry.ToDict()
	}
	return map[string]any{
		"skill_id":   l.SkillID,
		"version":    l.Version,
		"updated_at": l.UpdatedAt,
		"entries":    entries,
	}
}

// FromDictEvolutionLog 从字典创建 EvolutionLog。
//
// 对应 Python: EvolutionLog.from_dict(data)
func FromDictEvolutionLog(data map[string]any) (*EvolutionLog, error) {
	if data == nil {
		data = map[string]any{}
	}
	entriesData, _ := data["entries"].([]any)
	entries := make([]EvolutionRecord, 0, len(entriesData))
	for _, item := range entriesData {
		if entryMap, ok := item.(map[string]any); ok {
			record, err := FromDictEvolutionRecord(entryMap)
			if err != nil {
				continue // 对齐 Python: 不因单条记录解析失败中断
			}
			entries = append(entries, *record)
		}
	}
	return &EvolutionLog{
		SkillID:   getStrFromAny(data["skill_id"], ""),
		Version:   getStrFromAny(data["version"], "1.0.0"),
		UpdatedAt: getStrFromAny(data["updated_at"], ""),
		Entries:   entries,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getOptionalFieldValue 根据 optional field key 获取 EvolutionPatch 的值。
// 对齐 Python: getattr(self, key)
func (p *EvolutionPatch) getOptionalFieldValue(key string) any {
	switch key {
	case "skip_reason":
		if p.SkipReason != nil {
			return *p.SkipReason
		}
	case "merge_target":
		if p.MergeTarget != nil {
			return *p.MergeTarget
		}
	case "script_filename":
		if p.ScriptFilename != nil {
			return *p.ScriptFilename
		}
	case "script_language":
		if p.ScriptLanguage != nil {
			return *p.ScriptLanguage
		}
	case "script_purpose":
		if p.ScriptPurpose != nil {
			return *p.ScriptPurpose
		}
	}
	return nil
}

// getIntFromAny 从 any 类型安全提取 int。
func getIntFromAny(v any, defaultVal int) int {
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return defaultVal
	}
}

// getFloatFromAny 从 any 类型安全提取 float64。
func getFloatFromAny(v any, defaultVal float64) float64 {
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return defaultVal
	}
}

// getStrFromAny 从 any 类型安全提取 string。
func getStrFromAny(v any, defaultVal string) string {
	if v == nil {
		return defaultVal
	}
	return fmt.Sprintf("%v", v)
}

// getBoolFromAny 从 any 类型安全提取 bool。
func getBoolFromAny(v any, defaultVal bool) bool {
	if v == nil {
		return defaultVal
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return defaultVal
}

// generateUUID8 生成 8 位 UUID hex。
// 对齐 Python: uuid.uuid4().hex[:8]
func generateUUID8() string {
	return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
}
