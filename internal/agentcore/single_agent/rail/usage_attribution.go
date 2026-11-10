// 本文件提供用量归属（usage attribution）的请求级传递面。
// 归属回答"这次模型调用的用量属于谁"：发起方 agent、所属团队/成员、
// 父会话、委派链、调用链路径与深度。它由内层 ReActAgent 在每次模型调用上报时读取，
// 并随快照落到 context.usage 事件里。
//
// Python 用 contextvars 承载（rail/base.py L71-175），因为它单线程共享状态；
// Go 无协程级隐式变量，且 goroutine 不自动继承，故改为 context.Context 派生：
// Bind* 返回携带值的新 ctx，调用方把它传下去即生效，"复位"就是改用父 ctx。

package rail

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/usageattr"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// attributionBox 请求级归属的可变载体。
// Python 把同一个 dict 绑进 ContextVar，之后原地 clear()+update() 让读取方看到新值；
// Go 的 ctx 值不可替换，所以绑一个加锁的盒子，刷新时整体替换。
type attributionBox struct {
	// mu 保护 data 的并发读写（模型调用刷新与并发工具读取）
	mu sync.RWMutex
	// data 当前归属（由绑定方移交所有权，盒子内不再复制）
	data usageattr.Attribution
}

// usageInvocationIDKey 当前调用标识的 ctx 键。
type usageInvocationIDKey struct{}

// usageAttributionKey 当前归属盒子的 ctx 键。
type usageAttributionKey struct{}

// usageDelegationKey 待启动子调用的委派归属 ctx 键。
type usageDelegationKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// CurrentUsageInvocationID 返回当前执行路径上的调用标识，缺失返回空串。
// Python: current_usage_invocation_id (rail/base.py L86-89)
func CurrentUsageInvocationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageInvocationIDKey{}).(string); ok {
		return v
	}
	return ""
}

// BindUsageInvocationID 绑定调用标识，返回派生 ctx（等价 Python 的 bind + token）。
// Python: bind_usage_invocation_id (rail/base.py L91-93)
func BindUsageInvocationID(ctx context.Context, invocationID string) context.Context {
	return context.WithValue(ctx, usageInvocationIDKey{}, invocationID)
}

// CurrentUsageAttribution 返回当前 agent 的请求血缘副本，缺失返回空字典。
// Python: current_usage_attribution (rail/base.py L101-104)
func CurrentUsageAttribution(ctx context.Context) usageattr.Attribution {
	if ctx == nil {
		return usageattr.Attribution{}
	}
	box, ok := ctx.Value(usageAttributionKey{}).(*attributionBox)
	if !ok || box == nil {
		return usageattr.Attribution{}
	}
	return box.snapshot()
}

// BindUsageAttribution 绑定归属，返回派生 ctx。
// 盒子接管 attribution 的所有权，调用方不得在绑定后继续修改它
// （刷新请走 UpdateUsageAttribution）。
// Python: bind_usage_attribution (rail/base.py L106-108)
func BindUsageAttribution(ctx context.Context, attribution usageattr.Attribution) context.Context {
	if attribution == nil {
		attribution = usageattr.Attribution{}
	}
	return context.WithValue(ctx, usageAttributionKey{}, &attributionBox{data: attribution})
}

// UpdateUsageAttribution 整体替换当前绑定的归属（不影响父 ctx）。
// 刷新后的完整归属会立刻对同一条 ctx 链上的读取方可见，等价 Python 的
// ctx.context_usage_attribution.clear() + update()。传入值同样移交所有权。
// Python: react_agent.py L1478-1479 / L1682-1683 / L1757-1758
func UpdateUsageAttribution(ctx context.Context, attribution usageattr.Attribution) {
	if ctx == nil {
		return
	}
	box, ok := ctx.Value(usageAttributionKey{}).(*attributionBox)
	if !ok || box == nil {
		return
	}
	box.replace(attribution)
}

// CurrentUsageDelegation 返回为即将启动的子调用准备的委派归属，缺失返回空字典。
// Python: current_usage_delegation (rail/base.py L116-118)
func CurrentUsageDelegation(ctx context.Context) usageattr.Attribution {
	if ctx == nil {
		return usageattr.Attribution{}
	}
	if v, ok := ctx.Value(usageDelegationKey{}).(usageattr.Attribution); ok && v != nil {
		return copyAttribution(v)
	}
	return usageattr.Attribution{}
}

// BindUsageDelegation 绑定子调用委派归属（不替换父归属），返回派生 ctx。
// Python: bind_usage_delegation (rail/base.py L121-123)
func BindUsageDelegation(ctx context.Context, delegation usageattr.Attribution) context.Context {
	return context.WithValue(ctx, usageDelegationKey{}, copyAttribution(delegation))
}

// BuildUsageDelegationAttribution 为被委派的子 agent 构造请求级元数据。
// 结果经 ctx 在进程内边界传递，可被任务调度器序列化到进程/异步边界。
// 它刻意不进旧版 Agent.invoke 载荷，不选缓存亲和字段的调用方保持原有输入形状。
// Python: build_usage_delegation_attribution (rail/base.py L131-172)
func BuildUsageDelegationAttribution(
	ctx context.Context,
	agentID string,
	parentSessionID string,
	delegationID string,
	parentAttribution usageattr.Attribution,
	parentInvocationID string,
) usageattr.Attribution {
	parent := parentAttribution
	if parent == nil {
		parent = CurrentUsageAttribution(ctx)
	}
	parentPath := utils.MapStringSlice(parent, "agent_path")
	childID := agentID
	if childID == "" {
		childID = "agent"
	}
	var agentPath []string
	if len(parentPath) > 0 {
		agentPath = append(append([]string{}, parentPath...), childID)
	} else {
		agentPath = []string{childID}
	}
	parentDepth := utils.MapInt(parent, "depth")
	resolvedParentInvocationID := parentInvocationID
	if resolvedParentInvocationID == "" {
		resolvedParentInvocationID = CurrentUsageInvocationID(ctx)
	}
	if resolvedParentInvocationID == "" {
		resolvedParentInvocationID = utils.MapString(parent, "invocation_id")
	}

	// agent_id 在 Python 侧允许为 None，此处用 nil 保留同形（键存在、值为空）
	var agentIDValue any
	if agentID != "" {
		agentIDValue = agentID
	}
	depth := 0
	if len(parentPath) > 0 {
		depth = parentDepth + 1
	}
	return usageattr.Attribution{
		"agent_id":              agentIDValue,
		"product_session_id":    utils.FirstNonEmpty(utils.MapString(parent, "product_session_id"), utils.MapString(parent, "execution_session_id"), parentSessionID),
		"parent_session_id":     parentSessionID,
		"parent_invocation_id":  resolvedParentInvocationID,
		"delegation_id":         delegationID,
		"agent_path":            agentPath,
		"depth":                 depth,
		"parent_cache_identity": utils.MapString(parent, "cache_identity"),
		"cache_mode":            utils.FirstNonEmpty(utils.MapString(parent, "cache_mode"), "provider"),
		"cache_scope":           utils.FirstNonEmpty(utils.MapString(parent, "cache_scope"), "session"),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// snapshot 返回归属副本（读取方改不到内部字典）。
func (b *attributionBox) snapshot() usageattr.Attribution {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return copyAttribution(b.data)
}

// replace 整体替换归属（接管所有权，不复制）。
func (b *attributionBox) replace(data usageattr.Attribution) {
	if data == nil {
		data = usageattr.Attribution{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = data
}

// copyAttribution 复制归属字典（对齐 Python 的 dict(...) 语义）。
func copyAttribution(src usageattr.Attribution) usageattr.Attribution {
	if src == nil {
		return usageattr.Attribution{}
	}
	dst := make(usageattr.Attribution, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
