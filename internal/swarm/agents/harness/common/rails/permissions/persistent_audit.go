// 自动权限决策的追加式持久化审计。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/persistent_audit.py（296 行）
//
// 把脱敏后的权限审计记录追加到宿主持有的 JSONL 文件。审计是 observe-only：
// 坏值、编码器或存储异常绝不能改变被观察的权限决策。
//
// 正则说明：按 9.90d-1 计划 Task 7 的对照表，Python re 的 `(?<!\w)`
// lookbehind 用 Go regexp（RE2）的「行首或非单词字符」捕获组替代，
// 其余模式 RE2 原生支持 `(?i)` 与 `\b`。

package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PersistentAuditWriteResult 一次持久化审计写入尝试的结果。
//
// Python: @dataclass(frozen=True) PersistentAuditWriteResult
type PersistentAuditWriteResult struct {
	// Persisted 是否成功落盘
	Persisted bool
	// Degraded 是否降级
	Degraded bool
	// Reason 结果原因
	Reason string
	// Path 审计文件路径
	Path string
}

// PersistentAuditWriter 把脱敏审计记录追加到宿主 JSONL。
//
// Python: class PersistentAuditWriter
type PersistentAuditWriter struct {
	// dataRoot 数据根，nil 表示不可用
	dataRoot *string
}

// DataRoot 返回审计数据根（nil 表示不可用）。
//
// 供子包比较「当前写入器是否已指向目标根」时使用。
func (w *PersistentAuditWriter) DataRoot() *string {
	if w == nil {
		return nil
	}
	return w.dataRoot
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AuditSchemaVersion 审计记录 schema 版本
	AuditSchemaVersion = "auto_permission_audit_v1"
	// AuditSubdirectory 审计子目录
	AuditSubdirectory = "permission_audit"
	// AuditFilename 审计文件名
	AuditFilename = "auto_permission.jsonl"
	// AuditDigestAlgorithm 摘要算法
	AuditDigestAlgorithm = "sha256"
	// DataDirEnvName 数据目录环境变量名（品牌映射 jiuwenswarm → uapclawswarm）
	DataDirEnvName = "UAPCLAW_DATA_DIR"
	// MaxAuditTextLength 审计文本长度上限
	MaxAuditTextLength = 160
	// AuditTextRedacted 脱敏替换串
	AuditTextRedacted = "[redacted]"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ExtraAllowlist extra 中允许进入审计记录的键白名单。
	// Python: EXTRA_ALLOWLIST frozenset
	ExtraAllowlist = map[string]bool{
		"authorization_outcome":    true,
		"authorization_stage":      true,
		"decision_source":          true,
		"host_context_failure":     true,
		"host_route_reason":        true,
		"host_route_source":        true,
		"record_kind":              true,
		"reviewer_fallback_reason": true,
		"reviewer_lifecycle":       true,
		"reviewer_outcome":         true,
		"reviewer_reason_code":     true,
		"reviewer_reason_summary":  true,
		"stage_outcome":            true,
	}

	// safeAuditTextPattern 严格审计文本白名单
	safeAuditTextPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.: -]{0,159}$`)
	// safeNaturalLanguageAuditTextPattern 自然语言审计文本白名单（允许 $）
	safeNaturalLanguageAuditTextPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9$_.: -]{0,159}$`)
	// posixAbsolutePathPattern POSIX 绝对路径。
	// Python 原式 (?<!\w)/(?:[^\s'"\\]+/?)+ 带 lookbehind，RE2 不支持；
	// 改为「行首或非单词字符」+ 捕获绝对路径（计划 Task 7 对照表）。
	posixAbsolutePathPattern = regexp.MustCompile(`(?:^|[^\w])(/[^\s'"\\]+)`)
	// windowsAbsolutePathPattern Windows 绝对路径
	windowsAbsolutePathPattern = regexp.MustCompile(`\b[A-Za-z]:\\[^\s'"]+`)
	// secretLikePattern 疑似密钥
	secretLikePattern = regexp.MustCompile(
		`(?i)(bearer\s+[A-Za-z0-9._~+/-]{8,}|` +
			`sk-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9_]{8,}|` +
			`(?:api[_-]?key|token|password|passwd|secret)\s*[:=]\s*\S+)`)
	// shellControlPattern shell 控制符
	shellControlPattern = regexp.MustCompile(`(&&|\|\||[;<>` + "`" + `$])`)
	// commandLikePattern 命令样式
	commandLikePattern = regexp.MustCompile(
		`(?i)\b(?:bash|cat|cmd|cp|curl|find|git|grep|mv|powershell|python3?|rm|sh|` +
			`sudo|wget)\s+\S+`)

	// naturalLanguageAuditFields 走宽松自然语言通道的字段
	naturalLanguageAuditFields = map[string]bool{"reviewer_reason_summary": true}
	// clearNaturalLanguagePattern 明确的自然语言短语（可先掩码）
	clearNaturalLanguagePattern = regexp.MustCompile(`(?i)\b(?:a|some|the)\s+cat food\b|\bplease\s+find me\b`)
	// contextualPricePattern 上下文价格（可先掩码）
	contextualPricePattern = regexp.MustCompile(
		`(?i)\b(?:amount(?:s)?(?:\s+of)?|budget(?:ed)?|costs?|for|price(?:d)?` +
			`(?:\s+at)?|totals?|worth)\s+\$(?:0|[1-9]\d*)(?:\.\d{1,2})?\b`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPersistentAuditWriter 构造审计写入器。
//
// Python: PersistentAuditWriter.__init__(*, data_root)
func NewPersistentAuditWriter(dataRoot any) *PersistentAuditWriter {
	if dataRoot == nil {
		return &PersistentAuditWriter{}
	}
	// 注意：入参是 any，*string 必须解引用——直接用 fmt.Sprint 会打印指针地址
	// 而不是路径值，导致「同根复用」判定永远失败。
	var s string
	switch typed := dataRoot.(type) {
	case *string:
		if typed == nil {
			return &PersistentAuditWriter{}
		}
		s = *typed
	case string:
		s = typed
	default:
		s = fmt.Sprint(dataRoot)
	}
	return &PersistentAuditWriter{dataRoot: &s}
}

// AuditPath 返回宿主审计根下的 JSONL 路径。
//
// Python: PersistentAuditWriter.audit_path (property)
func (w *PersistentAuditWriter) AuditPath() string {
	if w.dataRoot == nil {
		return ""
	}
	return filepath.Join(*w.dataRoot, AuditSubdirectory, AuditFilename)
}

// Write 追加一条脱敏审计记录，不改变权限结果。
//
// Python: PersistentAuditWriter.write(facts, *, decision, reason, degraded, ...)
func (w *PersistentAuditWriter) Write(
	facts ToolDecisionFacts,
	decision string,
	reason string,
	degraded bool,
	grantID string,
	grantReason string,
	extra map[string]any,
) (result PersistentAuditWriteResult) {
	if w.dataRoot == nil {
		return PersistentAuditWriteResult{
			Persisted: false,
			Degraded:  true,
			Reason:    "audit_root_unavailable",
			Path:      "",
		}
	}
	auditPath := w.AuditPath()
	// 审计是 observe-only：任何异常都降级，绝不影响被观察的权限决策。
	defer func() {
		if r := recover(); r != nil {
			result = PersistentAuditWriteResult{
				Persisted: false, Degraded: true,
				Reason: "audit_write_failed", Path: auditPath,
			}
		}
	}()
	record := buildAuditRecord(facts, decision, reason, degraded, grantID, grantReason, extra)
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o755); err != nil {
		return PersistentAuditWriteResult{
			Persisted: false, Degraded: true,
			Reason: "audit_write_failed", Path: auditPath,
		}
	}
	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return PersistentAuditWriteResult{
			Persisted: false, Degraded: true,
			Reason: "audit_write_failed", Path: auditPath,
		}
	}
	defer func() { _ = f.Close() }()

	// Python: json.dumps(record, sort_keys=True, separators=(",", ":"), ensure_ascii=True)
	encoded, err := json.Marshal(record)
	if err != nil {
		return PersistentAuditWriteResult{
			Persisted: false, Degraded: true,
			Reason: "audit_write_failed", Path: auditPath,
		}
	}
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		return PersistentAuditWriteResult{
			Persisted: false, Degraded: true,
			Reason: "audit_write_failed", Path: auditPath,
		}
	}
	return PersistentAuditWriteResult{
		Persisted: true, Degraded: false,
		Reason: "audit_persisted", Path: auditPath,
	}
}

// ResolvePersistentAuditRoot 从配置或环境解析宿主审计数据根。
//
// Python: resolve_persistent_audit_root(config)
// 优先级：config.persistent_audit_root → config.audit_root →
// config.auto.persistent_audit_root → config.auto.audit_root → env。
func ResolvePersistentAuditRoot(config any) *string {
	cfg, ok := config.(map[string]any)
	if !ok {
		cfg = map[string]any{}
	}
	rawRoot := firstNonEmpty(cfg["persistent_audit_root"], cfg["audit_root"])
	if rawRoot == "" {
		if auto, ok := cfg["auto"].(map[string]any); ok {
			rawRoot = firstNonEmpty(auto["persistent_audit_root"], auto["audit_root"])
		}
	}
	if rawRoot == "" {
		rawRoot = os.Getenv(DataDirEnvName)
	}
	if rawRoot == "" {
		return nil
	}
	s := expandHomeString(rawRoot)
	return &s
}

// BuildSanitizedAuditFields 构造所有审计 sink 共享的白名单、JSON-safe 字段。
//
// Python: build_sanitized_audit_fields(facts, *, decision, reason, degraded, ...)
func BuildSanitizedAuditFields(
	facts ToolDecisionFacts,
	decision string,
	reason string,
	degraded bool,
	grantID string,
	grantReason string,
	extra map[string]any,
) map[string]any {
	effects := make([]string, 0, len(facts.Capability.StaticSideEffects))
	for item := range facts.Capability.StaticSideEffects {
		effects = append(effects, item)
	}
	sort.Strings(effects)
	sanitizedEffects := make([]string, len(effects))
	for i, item := range effects {
		sanitizedEffects[i] = SanitizeAuditField("side_effect", item).(string)
	}

	record := map[string]any{
		"decision":       SanitizeAuditField("decision", decision),
		"degraded":       degraded,
		"accesses_known": facts.AccessesKnown,
		"path_counts": map[string]int{
			"external": len(facts.ExternalPaths),
			"read":     len(facts.ReadPaths),
			"write":    len(facts.WritePaths),
		},
		"reason":        SanitizeAuditField("reason", reason),
		"risk_tier":     SanitizeAuditField("risk_tier", facts.Capability.RiskTier),
		"side_effects":  sanitizedEffects,
		"tool_category": SanitizeAuditField("tool_category", facts.ToolCategory()),
		"tool_name":     SanitizeAuditField("tool_name", facts.ToolName()),
	}
	if grantID != "" {
		record["grant_id"] = SanitizeAuditField("grant_id", grantID)
	}
	if grantReason != "" {
		record["grant_reason"] = SanitizeAuditField("grant_reason", grantReason)
	}
	for key, value := range extra {
		if ExtraAllowlist[key] {
			record[key] = SanitizeAuditField(key, value)
		}
	}
	return record
}

// SanitizeAuditValue 返回 JSON-safe 的审计值，敏感文本脱敏。
//
// Python: sanitize_audit_value(value)
func SanitizeAuditValue(value any) any {
	switch v := value.(type) {
	case string:
		return sanitizeAuditText(v)
	case int, int64, float64, float32, bool, nil:
		return value
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			out[sanitizeAuditText(fmt.Sprint(key))] = SanitizeAuditValue(nested)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = SanitizeAuditValue(item)
		}
		return out
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = SanitizeAuditValue(item)
		}
		return out
	default:
		return sanitizeAuditText(v)
	}
}

// SanitizeAuditField 按字段的窄结构化契约脱敏一个白名单字段。
//
// Python: sanitize_audit_field(key, value)
func SanitizeAuditField(key string, value any) any {
	if naturalLanguageAuditFields[key] {
		if s, ok := value.(string); ok {
			return sanitizeNaturalLanguageAuditText(s)
		}
	}
	return SanitizeAuditValue(value)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAuditRecord 组装完整记录（字段 + schema_version + timestamp）。
//
// Python: PersistentAuditWriter._build_record
func buildAuditRecord(
	facts ToolDecisionFacts,
	decision, reason string,
	degraded bool,
	grantID, grantReason string,
	extra map[string]any,
) map[string]any {
	record := BuildSanitizedAuditFields(facts, decision, reason, degraded, grantID, grantReason, extra)
	record["schema_version"] = AuditSchemaVersion
	// Python time.time() 的秒级浮点
	record["timestamp"] = float64(time.Now().UnixNano()) / 1e9
	return record
}

// sanitizeAuditText 严格通道：敏感内容或非法字符 → [redacted]。
//
// Python: _sanitize_audit_text(value)
func sanitizeAuditText(value any) string {
	text := strings.TrimSpace(fmt.Sprint(valueOrEmpty(value)))
	if text == "" {
		return ""
	}
	if auditTextContainsSensitiveContent(text) {
		return AuditTextRedacted
	}
	if safeAuditTextPattern.MatchString(text) {
		return truncateRunes(text, MaxAuditTextLength)
	}
	return AuditTextRedacted
}

// sanitizeNaturalLanguageAuditText 宽松通道：先掩码明确短语与上下文价格，
// 再复用严格检测器；任何未掩码的命令或 $ 形式仍受保护。
//
// Python: _sanitize_natural_language_audit_text(value)
func sanitizeNaturalLanguageAuditText(value any) string {
	text := strings.TrimSpace(fmt.Sprint(valueOrEmpty(value)))
	if text == "" {
		return ""
	}
	strictView := clearNaturalLanguagePattern.ReplaceAllString(text, "natural phrase")
	strictView = contextualPricePattern.ReplaceAllString(strictView, "price")
	if strings.Contains(strictView, "$") || auditTextContainsSensitiveContent(strictView) {
		return AuditTextRedacted
	}
	if safeNaturalLanguageAuditTextPattern.MatchString(text) {
		return truncateRunes(text, MaxAuditTextLength)
	}
	return AuditTextRedacted
}

// auditTextContainsSensitiveContent 五类敏感内容检测。
//
// Python: _audit_text_contains_sensitive_content(text)
func auditTextContainsSensitiveContent(text string) bool {
	if posixAbsolutePathPattern.MatchString(text) {
		return true
	}
	if windowsAbsolutePathPattern.MatchString(text) {
		return true
	}
	if secretLikePattern.MatchString(text) {
		return true
	}
	if shellControlPattern.MatchString(text) {
		return true
	}
	return commandLikePattern.MatchString(text)
}

// firstNonEmpty 取第一个非空字符串（Python `a or b` 语义）。
func firstNonEmpty(values ...any) string {
	for _, v := range values {
		if v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" {
			return s
		}
	}
	return ""
}

// valueOrEmpty Python `value or ""` 等价。
func valueOrEmpty(value any) any {
	if value == nil {
		return ""
	}
	return value
}

// truncateRunes 按 rune 截断（Python text[:MAX_AUDIT_TEXT_LENGTH]）。
func truncateRunes(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit])
}

// expandHomeString 展开 ~ 前缀（Python Path.expanduser）。
func expandHomeString(value string) string {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~"))
		}
	}
	return value
}
