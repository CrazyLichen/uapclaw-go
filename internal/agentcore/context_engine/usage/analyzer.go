package usage

import "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// SnapshotInput 构建快照所需的全部输入。
// 由调用方（rail / agent）从上下文统计与模型响应里取好再传入，
// analyzer 不反向依赖 ModelContext，便于单测与复用。
type SnapshotInput struct {
	// Phase 采集时点
	Phase string
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识
	SessionID string
	// ProductSessionID 产品侧会话标识，空则回落 SessionID
	ProductSessionID string
	// AgentID agent 标识
	AgentID string
	// TeamID 团队标识
	TeamID string
	// MemberName 团队成员名
	MemberName string
	// Role 团队角色
	Role string
	// AgentPath 调用链路径
	AgentPath []string
	// Depth 调用深度
	Depth int
	// Attribution 用量归属（owner 与调用血缘），键存在即覆盖上面的默认值
	// Python: build_snapshot(..., attribution=...)（analyzer.py L59）
	Attribution map[string]any
	// LimitTokens 窗口上限，<=0 表示未知
	LimitTokens int
	// SystemPromptTokens 系统提示词 token
	SystemPromptTokens int
	// ToolsTokens 工具定义 token
	ToolsTokens int
	// SkillsTokens 技能 token
	SkillsTokens int
	// MessagesTokens 消息 token
	MessagesTokens int
	// Usage provider 上报的本轮用量，nil 表示未上报
	Usage *schema.UsageMetadata
	// SessionCache 会话级聚合，nil 表示不附带
	SessionCache *SessionKVCacheUsage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildSnapshot 汇总分类用量与 provider 权威总量，产出 v1 快照。
// 归因规则：provider 上报了输入量时以其为权威总量，差额全部记到 messages
// （本地估算漏计的部分通常是消息序列化开销）；差额为负则取 0，不做二次缩放。
// Python: ContextUsageAnalyzer.snapshot_from_report()
func BuildSnapshot(in SnapshotInput) ContextUsageSnapshot {
	// 1. 各分类 token 数先按本地估算装配
	parts := map[string]ContextPartUsage{
		CategorySystemPrompt: newPart(CategorySystemPrompt, in.SystemPromptTokens),
		CategoryTools:        newPart(CategoryTools, in.ToolsTokens),
		CategorySkills:       newPart(CategorySkills, in.SkillsTokens),
		CategoryMessages:     newPart(CategoryMessages, in.MessagesTokens),
	}

	// 2. provider 有真实用量时以其为准，与本地估算的差额并入消息分类
	localTotal := in.SystemPromptTokens + in.ToolsTokens + in.SkillsTokens + in.MessagesTokens
	requestCache := RequestCacheFromUsage(in.Usage)
	inputTokens := requestCache.InputTokens
	tokensSource := "local_estimate"
	total := localTotal
	if inputTokens != nil {
		tokensSource = "provider_usage"
		total = *inputTokens
		if delta := *inputTokens - localTotal; delta > 0 {
			adjusted := parts[CategoryMessages]
			adjusted.Tokens += delta
			parts[CategoryMessages] = adjusted
		}
	}

	// 3. 有窗口上限时算出各分类占比与整体占用率
	window := ContextWindowUsage{TokensSource: tokensSource}
	if in.LimitTokens > 0 {
		limit := in.LimitTokens
		window.LimitTokens = &limit
		for category, part := range parts {
			percentage := float64(part.Tokens) / float64(limit)
			part.PercentageOfWindow = &percentage
			parts[category] = part
		}
		if total > 0 {
			occupancy := float64(total) / float64(limit)
			window.OccupancyRate = &occupancy
		}
	} else if total > 0 {
		totalCopy := total
		window.InputTokens = &totalCopy
	}
	if inputTokens != nil {
		window.InputTokens = inputTokens
	}

	// 4. 补齐会话/路径缺省值后组装最终快照
	productSessionID := in.ProductSessionID
	if productSessionID == "" {
		productSessionID = in.SessionID
	}
	agentPath := in.AgentPath
	if agentPath == nil {
		agentPath = []string{}
	}

	snapshot := ContextUsageSnapshot{
		EventType:             EventTypeContextUsage,
		SchemaVersion:         SchemaVersion,
		Phase:                 in.Phase,
		RequestID:             in.RequestID,
		SessionID:             in.SessionID,
		ProductSessionID:      productSessionID,
		AgentID:               in.AgentID,
		TeamID:                in.TeamID,
		MemberName:            in.MemberName,
		Role:                  in.Role,
		AgentPath:             agentPath,
		Depth:                 in.Depth,
		ContextWindow:         window,
		Parts:                 parts,
		RequestCache:          requestCache,
		SessionCache:          in.SessionCache,
		SessionKVCacheHitRate: nil,
	}
	// 归属字段最后覆盖：Python 在构造参数里展开 _attribution_fields(attribution)，
	// 因此归属值优先于上面算出的缺省值。
	// Python: analyzer.py L206 / L339
	applyAttributionFields(&snapshot, in.Attribution)
	if in.SessionCache != nil {
		snapshot.SessionKVCacheHitRate = in.SessionCache.WeightedHitRate
	}
	return snapshot
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyAttributionFields 把归属字典里存在的稳定 owner/血缘字段写进快照。
// 只覆盖键存在的项，其余保留调用方算出的缺省值。
// Python: ContextUsageAnalyzer._attribution_fields (analyzer.py L357-380)
func applyAttributionFields(snapshot *ContextUsageSnapshot, attribution map[string]any) {
	if snapshot == nil || attribution == nil {
		return
	}
	if v, ok := attributionString(attribution, "product_session_id"); ok {
		snapshot.ProductSessionID = v
	}
	if v, ok := attributionString(attribution, "execution_session_id"); ok {
		snapshot.ExecutionSessionID = v
	}
	if v, ok := attributionString(attribution, "context_owner_id"); ok {
		snapshot.ContextOwnerID = v
	}
	if v, ok := attributionString(attribution, "agent_id"); ok {
		snapshot.AgentID = v
	}
	if v, ok := attributionString(attribution, "team_id"); ok {
		snapshot.TeamID = v
	}
	if v, ok := attributionString(attribution, "member_name"); ok {
		snapshot.MemberName = v
	}
	if v, ok := attributionString(attribution, "invocation_id"); ok {
		snapshot.InvocationID = v
	}
	if v, ok := attributionString(attribution, "parent_invocation_id"); ok {
		snapshot.ParentInvocationID = v
	}
	if v, ok := attributionString(attribution, "delegation_id"); ok {
		snapshot.DelegationID = v
	}
	if v, ok := attributionString(attribution, "cache_identity"); ok {
		snapshot.CacheIdentity = v
	}
	if v, ok := attributionString(attribution, "parent_cache_identity"); ok {
		snapshot.ParentCacheIdentity = v
	}
	if v, ok := attributionString(attribution, "cache_mode"); ok {
		snapshot.CacheMode = v
	}
	if v, ok := attributionString(attribution, "cache_scope"); ok {
		snapshot.CacheScope = v
	}
	if path, ok := attributionAgentPath(attribution); ok {
		snapshot.AgentPath = path
	}
	if depth, ok := attributionIntValue(attribution, "depth"); ok {
		snapshot.Depth = depth
	}
}

// attributionString 读取归属字典里的字符串值；非字符串视为未提供。
func attributionString(m map[string]any, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// attributionAgentPath 读取归属字典里的 agent_path（[]string 或 []any）。
func attributionAgentPath(m map[string]any) ([]string, bool) {
	raw, ok := m["agent_path"]
	if !ok {
		return nil, false
	}
	switch v := raw.(type) {
	case []string:
		return append([]string{}, v...), true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

// attributionIntValue 读取归属字典里的整数值（兼容 int/int64/float64）。
func attributionIntValue(m map[string]any, key string) (int, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

// newPart 构造分类条目（估算来源固定为 tokenizer 计数）。
func newPart(category string, tokens int) ContextPartUsage {
	if tokens < 0 {
		tokens = 0
	}
	return ContextPartUsage{
		Category:  category,
		Tokens:    tokens,
		Estimated: true,
		Source:    "tokenizer",
	}
}
