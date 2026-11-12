package adapter

import (
	"context"
	"errors"
	"strings"

	permissionmode "github.com/uapclaw/uapclaw-go/internal/common/permission_mode"
	permissionsrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sessionAdapterRequestOptions getSessionAdapterForRequest 的可选参数。
// Python: _get_session_adapter_for_request(request, *, reserve_activity, external_harness_binding)
type sessionAdapterRequestOptions struct {
	// ReserveActivity 是否为本次请求保留活动名额
	ReserveActivity bool
	// ExternalHarnessBinding 外部 harness 绑定（非 nil 时走 external_harness 分支）
	ExternalHarnessBinding map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errPermissionResumeOwnerMissing 智能审批启用、审批恢复请求、但 child 不存在。
	// Python: interface_deep.py:3850 RootPermissionQueueError("permission_resume_owner_missing")
	errPermissionResumeOwnerMissing = errors.New("permission_resume_owner_missing")
	// errPermissionExternalInputAdapterUnavailable 安全边界发布后仍拿不到 child。
	// Python: interface_deep.py:3905 RuntimeError("permission_external_input_adapter_unavailable")
	errPermissionExternalInputAdapterUnavailable = errors.New("permission_external_input_adapter_unavailable")
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isHostPermissionUpdateInput 该请求是否可以消费待发布的权限版本。
// Python: interface_deep.py:3762-3783 _is_host_permission_update_input
//
// 与 personDroveTurn（interface.py:467-479）是两个不同函数，不可互替：
//   - personDroveTurn 判 team → false，本函数只判 is_team_params 的 params 标记
//   - 本函数判 req_method != CHAT_SEND → false、判 skip_a2ui → false，personDroveTurn 不判
//   - 本函数单独判 runtime_mode == "auto_harness"
func isHostPermissionUpdateInput(request *schema.AgentRequest) bool {
	if request == nil {
		return false
	}
	params := parseParams(request.Params)
	metadata := request.Metadata

	// Python: :3767-3768 if request.req_method != ReqMethod.CHAT_SEND: return False
	if request.ReqMethod != schema.ReqMethodChatSend {
		return false
	}
	// Python: :3769-3770 if metadata.get("skip_a2ui") is True: return False
	if v, ok := metadata["skip_a2ui"].(bool); ok && v {
		return false
	}
	// Python: :3771-3773
	//   runtime_mode = str(params.get("mode") or "agent").strip().lower()
	//   if is_team_params(params) or runtime_mode == "auto_harness": return False
	if schema.IsTeamMode(paramString(params, "mode", "agent")) {
		return false
	}
	if strings.ToLower(strings.TrimSpace(paramString(params, "mode", "agent"))) == "auto_harness" {
		return false
	}
	// Python: :3778-3783 return is_external_user_authored_dispatch(
	//                          params, channel_id, req_method, metadata)
	return isExternalUserAuthoredDispatch(
		params, request.ChannelID, string(request.ReqMethod), metadata)
}

// getSessionAdapterForRequest 选定一个 child，并只在安全边界上发布 Smart Permission。
// Python: interface_deep.py:3812-3920 _get_session_adapter_for_request
//
// 名字只体现第一步：实际做三件事——①选定 child ②判定该请求是否走「外部用户输入」的
// 权限发布路径 ③在安全边界上把智能审批配置装进 child。三件事不能分开，否则会出现
// 「选到 child → 期间发生权限配置重载 → 往过期 child 装配置」的窗口期。
func (d *DeepAdapter) getSessionAdapterForRequest(
	ctx context.Context,
	request *schema.AgentRequest,
	opts sessionAdapterRequestOptions,
) (AgentAdapter, error) {
	sessionID := ""
	requestID := ""
	if request != nil {
		requestID = request.RequestID
		if request.SessionID != nil {
			sessionID = *request.SessionID
		}
	}

	// 步骤 1: external harness 分支
	// Python: :3827-3837
	if bindingKind(opts.ExternalHarnessBinding) == "external_harness" {
		return d.getOrCreateSessionAdapter(ctx, sessionID, sessionAdapterOptions{
			ReserveActivity:        opts.ReserveActivity,
			RequestID:              requestID,
			ExternalHarnessBinding: opts.ExternalHarnessBinding,
		})
	}

	// 步骤 2: 读缓存的 child
	// Python: :3839 cached = self._get_cached_session_adapter(request.session_id)
	cached := d.getCachedSessionAdapter(sessionID)

	// 步骤 3: smart_lifecycle 判定
	// Python: :3840-3845
	//   smart_lifecycle = self._coordinates_smart_permission_lifecycle(get_config(), sid)
	//                     or (cached is not None and cached._enable_auto_permission)
	smartLifecycle := d.coordinatesSmartPermissionLifecycle(sessionID) ||
		(cached != nil && d.cachedSessionAutoPermissionEnabled())

	// 步骤 4: 智能审批启用 + 审批恢复请求 + child 不存在 → 报错
	// Python: :3846-3850
	if smartLifecycle && schema.IsInterruptResumePayload(parseParams(request.Params)) && cached == nil {
		return nil, errPermissionResumeOwnerMissing
	}

	// 步骤 5: 不走权限发布路径 → 直接取内层
	// Python: :3852-3874
	if !smartLifecycle || !isHostPermissionUpdateInput(request) {
		return d.getOrCreateSessionAdapter(ctx, sessionID, sessionAdapterOptions{
			ReserveActivity:        opts.ReserveActivity,
			RequestID:              requestID,
			ExternalHarnessBinding: opts.ExternalHarnessBinding,
		})
	}

	// 步骤 6: 安全边界发布
	// Python: :3876-3906
	//   selected = None
	//   async def select_and_publish():
	//       nonlocal selected
	//       selected = await self._get_or_create_session_adapter(..., host_external_input=True, ...)
	//   builder = self._permissions_external_input_context_builder
	//   if builder is None:
	//       await select_and_publish()                     ← 本分支
	//   else:
	//       context_factory = builder(select_and_publish)
	//       async with context_factory(): pass              ← ⤵️ 回填
	//   if selected is None: raise RuntimeError("permission_external_input_adapter_unavailable")
	//
	// ⤵️ 回填：权限配置重载子系统，Go 侧 5 件全缺——
	//   _permissions_reload_tail（乐观锁世代号）/ _reload_lock（全局重载锁）/
	//   _permissions_reload_schedule_failure / build_permissions_external_input_context
	//   （agent_manager.py:348 的事务包装，3 次重试 + 世代校验）/
	//   _permissions_external_input_context_builder 注入点。
	//   grep ReloadPermission|permissionsReady|PermissionsReload 均 0 命中。
	//   本节只走上游 builder == nil 的合法分支（:3892-3893），非自创简化。
	selected, err := d.getOrCreateSessionAdapter(ctx, sessionID, sessionAdapterOptions{
		ReserveActivity:        opts.ReserveActivity,
		RequestID:              requestID,
		ExternalHarnessBinding: opts.ExternalHarnessBinding,
		HostExternalInput:      true, // Python: host_external_input=True（:3878）
	})
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, errPermissionExternalInputAdapterUnavailable
	}
	return selected, nil
}

// coordinatesSmartPermissionLifecycle 该会话是否由智能审批生命周期协调。
// Python: interface_deep.py:3840 self._coordinates_smart_permission_lifecycle(get_config(), sid)
// ⤵️ 回填：与 D-5 同一权限配置重载子系统，Go 侧无对应物，暂返回 false。
func (d *DeepAdapter) coordinatesSmartPermissionLifecycle(sessionID string) bool {
	return false
}

// cachedSessionAutoPermissionEnabled 会话缓存 child 是否启用智能审批。
// Python: interface_deep.py:3844 cached._enable_auto_permission
func (d *DeepAdapter) cachedSessionAutoPermissionEnabled() bool {
	return permissionmode.IsAutoPermissionEnabled(
		permissionsrails.DeepCopyPermissionConfig(d.instanceOverridesSnapshot()))
}

// bindingKind 取 external harness 绑定的 kind 字段。
func bindingKind(binding map[string]any) string {
	if binding == nil {
		return ""
	}
	if v, ok := binding["kind"].(string); ok {
		return v
	}
	return ""
}

// paramString 安全取 params 里的字符串。
func paramString(params map[string]any, key, fallback string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return fallback
}
