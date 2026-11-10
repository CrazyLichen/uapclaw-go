// 本文件提供 ReActAgent 的用量归属解析：把"这次模型调用的用量属于谁"落成
// 一组稳定的 owner/血缘字段，供 context.usage 快照与观测层使用。
// 解析刻意做成能力探测：核心 Session 提供会话标识与来源元数据，团队会话额外提供
// team id，独立会话也能产出一份完整的单 agent 归属，不导入团队或子代理包。

package agents

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/usageattr"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 接口 ────────────────────────────

// UsageIdentityProvider 成员身份来源（BuildContext 的最小面）。
// Python: getattr(build_context, "member_name", None)（react_agent.py L1361-1363）
type UsageIdentityProvider interface {
	// GetMemberName 返回团队成员名
	GetMemberName() string
}

// usageSourceMetadataProvider 会话来源元数据能力（团队会话带 source_team_id 等）。
// Python: getattr(session, "get_source_metadata", None)（react_agent.py L1349-1355）
type usageSourceMetadataProvider interface {
	// GetSourceMetadata 返回来源元数据
	GetSourceMetadata() map[string]any
}

// usageTeamIDProvider 团队标识能力。
// Python: getattr(session, "get_team_id", lambda: None)()（react_agent.py L1365-1369）
type usageTeamIDProvider interface {
	// GetTeamID 返回团队标识
	GetTeamID() string
}

// usageParentSessionIDProvider 子会话血缘能力。
// Python: getattr(session, "get_parent_session_id", None)（react_agent.py L1371-1374）
type usageParentSessionIDProvider interface {
	// GetParentSessionID 返回产品侧父会话标识
	GetParentSessionID() string
}

// usageCacheIdentityProvider 缓存身份能力。
// Python: getattr(session, "get_cache_identity", None)（react_agent.py L1385-1397）
type usageCacheIdentityProvider interface {
	// GetCacheIdentity 返回 provider 侧缓存身份
	GetCacheIdentity() (kv_cache.KVCacheIdentity, error)
}

// usageContextUsageMetadataProvider 会话级用量元数据能力。
// Python: getattr(session, "get_context_usage_metadata", None)（react_agent.py L1338-1345）
type usageContextUsageMetadataProvider interface {
	// GetContextUsageMetadata 返回会话级用量元数据
	GetContextUsageMetadata() map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

// kvCacheAffinityParentSessionIDEnv 子会话缓存亲和父会话的环境变量键。
// Python: KV_CACHE_AFFINITY_PARENT_SESSION_ID_ENV（core/kv_cache/kv_cache_metadata.py L14）
const kvCacheAffinityParentSessionIDEnv = kv_cache.KVCacheAffinityParentSessionIDEnv

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveContextUsageAttribution 解析一次模型调用上报所需的 owner 与调用血缘。
// Python: ReActAgent._context_usage_attribution (react_agent.py L1326-1418)
func (a *ReActAgent) resolveContextUsageAttribution(cbc *interfaces.AgentCallbackContext) usageattr.Attribution {
	values := usageattr.Attribution{}
	if cbc != nil {
		// 会话级用量元数据（Python 先取 session 元数据，再用 ctx 归属覆盖）
		if provider, ok := cbc.Session().(usageContextUsageMetadataProvider); ok {
			for k, v := range provider.GetContextUsageMetadata() {
				values[k] = v
			}
		}
		for k, v := range cbc.ContextUsageAttribution() {
			values[k] = v
		}
	}

	sourceMetadata := map[string]any{}
	if cbc != nil {
		if provider, ok := cbc.Session().(usageSourceMetadataProvider); ok {
			for k, v := range provider.GetSourceMetadata() {
				sourceMetadata[k] = v
			}
		}
	}

	// agent id：ctx 归属 → card.id → card.name
	// （Python 的 _usage_agent_id 与本 agent 的 card.id 恒等，Go 不重复保存）
	agentID := utils.MapString(values, "agent_id")
	if agentID == "" && a.card != nil {
		agentID = a.card.ID
		if agentID == "" {
			agentID = a.card.Name
		}
	}

	// team id：ctx 归属 → 来源元数据 → 会话能力
	teamID := utils.MapString(values, "team_id")
	if teamID == "" {
		teamID = utils.MapString(sourceMetadata, "source_team_id")
	}
	if teamID == "" {
		if provider, ok := cbc.Session().(usageTeamIDProvider); ok {
			teamID = provider.GetTeamID()
		}
	}

	// 成员名：ctx 归属 → 来源元数据 → build context → agent 自身
	// Python: react_agent.py L1367-1371（四级，最后一级是 getattr(self, "member_name", None)）
	memberName := utils.MapString(values, "member_name")
	if memberName == "" {
		memberName = utils.MapString(sourceMetadata, "source_member_name")
	}
	if memberName == "" && a.usageBuildContext != nil {
		memberName = a.usageBuildContext.GetMemberName()
	}
	if memberName == "" {
		// Python 的 ReActAgent 本身不声明 member_name 属性，这一级恒为空；
		// 保留该步骤是为了与上游逐行可对照（可由宿主动态挂属性）。
		// Python: member_name = getattr(self, "member_name", None)（L1370-1371）
		memberName = a.usageMemberName
	}

	executionSessionID := usageSessionID(cbc)
	parentSessionID := utils.MapString(values, "parent_session_id")
	if parentSessionID == "" && cbc != nil {
		if provider, ok := cbc.Session().(usageParentSessionIDProvider); ok {
			parentSessionID = provider.GetParentSessionID()
		}
	}
	if parentSessionID == "" && cbc != nil && cbc.Session() != nil {
		// Python: session.get_env(KV_CACHE_AFFINITY_PARENT_SESSION_ID_ENV)
		if raw := cbc.Session().GetEnv(kvCacheAffinityParentSessionIDEnv); raw != nil {
			if s, ok := raw.(string); ok {
				parentSessionID = strings.TrimSpace(s)
			}
		}
	}
	productSessionID := utils.FirstNonEmpty(
		utils.MapString(values, "product_session_id"), parentSessionID, executionSessionID)

	cacheIdentity := utils.MapString(values, "cache_identity")
	parentCacheIdentity := utils.MapString(values, "parent_cache_identity")
	if cbc != nil {
		if provider, ok := cbc.Session().(usageCacheIdentityProvider); ok {
			// 遥测不得影响模型执行：取身份失败只记 debug，继续用兜底值。
			// Python: try/except → logger.debug("Failed to resolve session cache identity ...")（react_agent.py L1393-1397）
			if identity, err := provider.GetCacheIdentity(); err != nil {
				logger.Debug(logComponent).Err(err).Msg("failed to resolve session cache identity for usage telemetry")
			} else {
				if cacheIdentity == "" {
					cacheIdentity = identity.CacheID
				}
				if parentCacheIdentity == "" {
					parentCacheIdentity = identity.ParentCacheID
				}
			}
		}
	}

	contextID := usageContextID(cbc, executionSessionID)
	contextOwnerID := utils.MapString(values, "context_owner_id")
	if contextOwnerID == "" {
		ownerName := utils.FirstNonEmpty(memberName, agentID, "agent")
		contextOwnerID = joinNonEmpty("|", teamID, ownerName, executionSessionID, contextID)
	}

	agentPath := utils.MapStringSlice(values, "agent_path")
	if len(agentPath) == 0 {
		agentPath = []string{utils.FirstNonEmpty(memberName, agentID, "agent")}
	}
	depth := utils.MapInt(values, "depth")
	if depth == 0 && len(agentPath)-1 > 0 {
		depth = len(agentPath) - 1
	}

	out := make(usageattr.Attribution, len(values)+9)
	for k, v := range values {
		out[k] = v
	}
	out["product_session_id"] = productSessionID
	out["execution_session_id"] = executionSessionID
	out["context_owner_id"] = contextOwnerID
	out["agent_id"] = agentID
	out["team_id"] = teamID
	out["member_name"] = memberName
	out["agent_path"] = agentPath
	out["depth"] = depth
	out["cache_identity"] = utils.FirstNonEmpty(cacheIdentity, executionSessionID)
	out["parent_cache_identity"] = utils.FirstNonEmpty(parentCacheIdentity, productSessionID)
	out["cache_mode"] = utils.FirstNonEmpty(utils.MapString(values, "cache_mode"), "provider")
	out["cache_scope"] = utils.FirstNonEmpty(utils.MapString(values, "cache_scope"), "session")
	return out
}

// usageSessionID 解析执行会话标识，跨支持的 session/context 实现。
// Python: ReActAgent._usage_session_id (react_agent.py L1314-1319)
func usageSessionID(cbc *interfaces.AgentCallbackContext) string {
	if cbc == nil {
		return "default"
	}
	if cbc.Session() != nil {
		if id := strings.TrimSpace(cbc.Session().GetSessionID()); id != "" {
			return id
		}
	}
	if mc := cbc.ModelContext(); mc != nil {
		if id := strings.TrimSpace(mc.SessionID()); id != "" {
			return id
		}
	}
	return "default"
}

// usageContextID 解析上下文标识，轻量上下文回退到会话标识。
// Python: ReActAgent._usage_context_id (react_agent.py L1321-1324)
func usageContextID(cbc *interfaces.AgentCallbackContext, fallback string) string {
	if cbc != nil {
		if mc := cbc.ModelContext(); mc != nil {
			if id := strings.TrimSpace(mc.ContextID()); id != "" {
				return id
			}
			if id := strings.TrimSpace(mc.SessionID()); id != "" {
				return id
			}
		}
	}
	return fallback
}

// joinNonEmpty 用 sep 连接非空片段（对齐 Python "|".join(part for part in ... if part)）。
func joinNonEmpty(sep string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, sep)
}
