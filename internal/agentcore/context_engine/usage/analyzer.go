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
	if in.SessionCache != nil {
		snapshot.SessionKVCacheHitRate = in.SessionCache.WeightedHitRate
	}
	return snapshot
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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
