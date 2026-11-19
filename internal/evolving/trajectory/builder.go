package trajectory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"

	obs "github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
)

// builder.go 离线轨迹数据的规范化 OTLP 组装器。
// 对齐 Python: agent_evolving/trajectory/offline/builder.py（全文，2026-12-04 快照）。
// 接受已规范化的 span 字典；刻意不构造旧 TrajectoryStep/detail 对象；
// 产出单个不可变 Trajectory。

// ──────────────────────────── 常量 ────────────────────────────

// resourceAliases meta 键 → 资源属性键别名。
// Python: offline/builder.py _RESOURCE_ALIASES。
var resourceAliases = map[string]string{
	"session_id":    obs.GenAIConversationID,
	"member_id":     obs.ATMemberName,
	"team_id":       TeamID,
	"source":        TrajectorySource,
	"case_id":       CaseID,
	"trajectory_id": TrajectoryID,
}

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryBuilder 为一次离线执行累积规范 OTLP span。
// Python: offline/builder.py TrajectoryBuilder。
type TrajectoryBuilder struct {
	// mu 保护 spans/identities（Python 单线程 asyncio；Go 回调可并发，
	// 并发移植模式：共享状态加锁）。
	mu           sync.Mutex
	sessionID    string
	source       string
	caseID       *string
	memberID     *string
	teamID       *string
	maxSpans     int // 0 表示无上限（对齐 Python None）；负值构造期拒绝
	trajectoryID string
	traceID      string
	meta         map[string]any
	spans        []map[string]any
	identities   map[SpanIdentity]struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryBuilder 创建离线轨迹组装器。
// maxSpans 传 0 表示无上限（Python None）；负值非法。
// Python: offline/builder.py TrajectoryBuilder.__init__。
func NewTrajectoryBuilder(sessionID, source string, caseID, memberID, teamID *string, meta map[string]any, maxSpans int, trajectoryID, traceID *string) (*TrajectoryBuilder, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if maxSpans < 0 {
		return nil, fmt.Errorf("max_spans must be >= 1")
	}
	var id string
	if trajectoryID != nil && *trajectoryID != "" {
		id = *trajectoryID
	} else {
		id = uuid.NewString()
	}
	trace := id
	if traceID != nil && *traceID != "" {
		trace = *traceID
	}
	copiedMeta := map[string]any{}
	for key, value := range meta {
		if value == nil {
			continue
		}
		copiedMeta[key] = copyJSON(value)
	}
	return &TrajectoryBuilder{
		sessionID:    sessionID,
		source:       source,
		caseID:       caseID,
		memberID:     memberID,
		teamID:       teamID,
		maxSpans:     maxSpans,
		trajectoryID: id,
		traceID:      trace,
		meta:         copiedMeta,
		identities:   map[SpanIdentity]struct{}{},
	}, nil
}

// Spans 返回组装器当前持有的脱离 span 列表。
// Python: offline/builder.py TrajectoryBuilder.spans。
func (b *TrajectoryBuilder) Spans() []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]map[string]any, 0, len(b.spans))
	for _, span := range b.spans {
		result = append(result, copyJSON(span).(map[string]any))
	}
	return result
}

// RecordSpan 记录一个脱离的规范 span，忽略重复身份。
// Python: offline/builder.py TrajectoryBuilder.record_span。
func (b *TrajectoryBuilder) RecordSpan(span map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if span == nil {
		return fmt.Errorf("span must be a mapping")
	}
	normalized, err := NormalizeSpan(span)
	if err != nil {
		return err
	}
	if _, hasTrace := normalized["traceId"]; !hasTrace {
		normalized["traceId"] = b.traceID
	}
	spanID, _ := normalized["spanId"].(string)
	if spanID == "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", b.traceID, len(b.spans), normalized["name"])))
		normalized["spanId"] = hex.EncodeToString(digest[:])[:16]
	}
	identity := SpanIdentityOf(normalized)
	if identity != nil {
		if _, duplicated := b.identities[*identity]; duplicated {
			return nil
		}
		b.identities[*identity] = struct{}{}
	}
	b.spans = append(b.spans, normalized)
	sort.SliceStable(b.spans, func(i, j int) bool {
		return compareSpanSortKey(SpanSortKeyOf(b.spans[i]), SpanSortKeyOf(b.spans[j])) < 0
	})
	if b.maxSpans > 0 && len(b.spans) > b.maxSpans {
		removed := b.spans[:len(b.spans)-b.maxSpans]
		b.spans = b.spans[len(b.spans)-b.maxSpans:]
		for _, removedSpan := range removed {
			if removedIdentity := SpanIdentityOf(removedSpan); removedIdentity != nil {
				delete(b.identities, *removedIdentity)
			}
		}
	}
	return nil
}

// AddSpan 供采集器风格调用方使用的 record_span 别名。
// Python: offline/builder.py TrajectoryBuilder.add_span。
func (b *TrajectoryBuilder) AddSpan(span map[string]any) error {
	return b.RecordSpan(span)
}

// resourceAttributes 组装资源属性（trajectory_id/投影版本/来源/会话 + 可选）。
// Python: offline/builder.py TrajectoryBuilder._resource_attributes。
func (b *TrajectoryBuilder) resourceAttributes() map[string]any {
	attrs := map[string]any{
		TrajectoryID:                    b.trajectoryID,
		TrajectoryProjectionVersionAttr: TrajectoryProjectionVersion,
		TrajectorySource:                b.source,
		obs.GenAIConversationID:         b.sessionID,
	}
	if b.caseID != nil {
		attrs[CaseID] = *b.caseID
	}
	if b.memberID != nil {
		attrs[obs.ATMemberName] = *b.memberID
	}
	if b.teamID != nil {
		attrs[TeamID] = *b.teamID
	}
	for key, value := range b.meta {
		if value == nil {
			continue
		}
		target := key
		if alias, exists := resourceAliases[key]; exists {
			target = alias
		}
		if _, exists := attrs[target]; exists {
			continue
		}
		attrs[target] = copyJSON(value)
	}
	return attrs
}

// Build 构建不可变的规范 OTLP 轨迹快照。
// Python: offline/builder.py TrajectoryBuilder.build。
func (b *TrajectoryBuilder) Build() (*Trajectory, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	spanList := make([]any, 0, len(b.spans))
	for _, span := range b.spans {
		spanList = append(spanList, copyJSON(span))
	}
	payload := map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{"attributes": AttributesFromMap(b.resourceAttributes())},
				"scopeSpans": []any{
					map[string]any{
						"scope": map[string]any{
							"name":    TrajectoryScopeName,
							"version": TrajectoryProjectionVersion,
						},
						"spans": spanList,
					},
				},
			},
		},
	}
	return FromOTLP(payload)
}

// SessionID 返回组装器会话标识。
// Python: offline/builder.py TrajectoryBuilder.session_id 属性。
func (b *TrajectoryBuilder) SessionID() string { return b.sessionID }
