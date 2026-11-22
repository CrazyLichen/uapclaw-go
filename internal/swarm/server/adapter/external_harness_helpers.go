// Python 源：jiuwenswarm/server/runtime/agent_adapter/external_harness_helpers.py（1020 行，
// 15.18 一文件对一文件移植；本文件当前含绑定半区，流/中断/启动面随批追加）。
//
// 单会话 External Harness 会话接入：会话不可变绑定（resolve/persist/load）、
// 面板配置合入、harness 懒启动/停止、输出流投影、checkpoint 导入导出、取消映射。
package adapter

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	equipmentpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalHarnessBindingError 请求与会话不可变 runtime kind 冲突。
// Python: external_harness_helpers.py:141-142 ExternalHarnessBindingError(ValueError)。
type ExternalHarnessBindingError struct {
	// Message 展示给用户的错误文案（已本地化）
	Message string
}

// Error 实现 error 接口。
func (e *ExternalHarnessBindingError) Error() string { return e.Message }

// ResolvedExternalHarnessBinding 一次绑定解析的结果。
// Python: resolve_external_harness_binding 返回 dict（kind/agent_template_name/
// provider_name/runtime/package_path）；deep_agent 结果仅含 kind。
// 偏差（登记 D-18-5）：Python 鸭子 dict → Go 具体类型，wire 键名由 json tag 承担。
type ResolvedExternalHarnessBinding struct {
	// Kind 绑定种类：external_harness / deep_agent
	Kind string `json:"kind"`
	// AgentTemplateName 专家模板名
	AgentTemplateName string `json:"agent_template_name,omitempty"`
	// ProviderName provider 名
	ProviderName string `json:"provider_name,omitempty"`
	// Runtime 专家包声明的 runtime（deep_agent 为 nil）
	Runtime *hschema.AgentRuntimeSpec `json:"-"`
	// PackagePath 专家包目录
	PackagePath string `json:"-"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// externalHarnessKind External Harness 绑定种类标识。
	// Python: :50 _EXTERNAL_HARNESS_KIND
	externalHarnessKind = "external_harness"
	// deepAgentKind 原生 DeepAgent 绑定种类标识。
	// Python: :235/:239 {"kind": "deep_agent"}
	deepAgentKind = "deep_agent"
	// sessionRuntimeBindingKey 会话元数据中绑定键。
	// Python: :212/:271 metadata["session_runtime_binding"]
	sessionRuntimeBindingKey = "session_runtime_binding"
	// agentTemplatesKind 设备包 kind（切片函数入参）。
	// Python: :178-182 equipment.resolve_equipment_runtime_id("agent_templates", ...)
	agentTemplatesKind = "agent_templates"
)

// errorTexts 双语错误文案表。Python: :60-138 _ERROR_TEXT（19 键一比一）。
// 占位符沿用 Python format 键（{template!r} 等；!r → Go %q 语义）。
var errorTexts = map[string][2]string{
	"template_name_type": {
		"agent_template_name 必须是字符串",
		"agent_template_name must be a string",
	},
	"binding_conflict": {
		"当前会话已绑定三方专家 {template!r}（provider={provider!r}）；请新建会话后切换专家",
		"This session is already bound to external expert {template!r} " +
			"(provider={provider!r}); start a new session to switch experts",
	},
	"cli_not_enabled": {
		"请先在配置界面启用 {label}，再使用该三方专家",
		"Enable {label} in settings before using this external expert",
	},
	"invalid_runtime_config": {
		"runtime.config 无效: {detail}",
		"runtime.config is invalid: {detail}",
	},
	"binding_missing": {
		"缺少 External Harness 绑定",
		"External Harness binding is missing",
	},
	"no_pending_interaction": {
		"当前没有可恢复的交互请求",
		"No interaction is waiting for an answer",
	},
	"waiting_for_interaction": {
		"任务正在等待交互输入，请先回答交互或取消当前任务",
		"The task is waiting for an answer; reply to the question or cancel it",
	},
	"task_running": {
		"任务正在进行中，请先取消后再发送新消息",
		"A task is still running; cancel it before sending a new message",
	},
	"startup_failed": {
		"External Harness 启动失败",
		"External Harness startup failed",
	},
	"input_rejected": {
		"External Harness 已拒绝该输入",
		"External Harness input was rejected",
	},
	"turn_not_accepted": {
		"External Harness 未受理新的一轮",
		"External Harness did not accept a new turn",
	},
	"turn_failed": {
		"External Harness 本轮执行失败",
		"External Harness turn failed",
	},
	"stream_failed": {
		"External Harness 输出流失败",
		"External Harness output stream failed",
	},
	"stream_closed": {
		"External Harness 输出流在本轮完成前关闭",
		"External Harness output stream closed before turn completion",
	},
	"unsupported_intent": {
		"不支持中断意图: {intent}",
		"Unsupported interrupt intent: {intent}",
	},
	"nothing_to_cancel": {
		"当前没有可取消的 External Harness 任务",
		"There is no External Harness task to cancel",
	},
	"cancel_unsupported": {
		"当前 External Harness 不支持取消",
		"This External Harness cannot be cancelled",
	},
	"cancel_failed": {
		"External Harness 取消失败",
		"External Harness cancel failed",
	},
	"cancel_requested": {
		"已请求取消 External Harness 任务",
		"External Harness cancel requested",
	},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// bindingLock 绑定读写锁（守卫会话元数据的读改写段）。
// Python: :49 _BINDING_LOCK = threading.RLock()；Go 侧无同 goroutine 递归进入点，
// 用 Mutex 等价（偏差登记 D-18-6：RLock→Mutex，无递归消费方）。
var bindingLock sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveExternalHarnessBinding 解析并强制一个会话的不可变 External Harness 绑定。
// Python: :200-250 resolve_external_harness_binding。步骤对齐：
// 读卡片在锁外（锁只护元数据比对段，不得跨 IO 持锁）→ 锁内读元数据比对 →
// 已绑定：请求缺失/换名/换 provider 即冲突；未绑定：无请求或无 runtime → deep_agent。
func ResolveExternalHarnessBinding(sessionID string, params map[string]any) (*ResolvedExternalHarnessBinding, error) {
	requestedName, err := requestedTemplateName(params)
	if err != nil {
		return nil, err
	}
	// 读卡片在拿锁之前：锁只守卫下面的元数据比对段，不得跨 IO 持锁。
	// Python: :206-208 注释同义。
	var requested *requestedRuntime
	if requestedName != "" {
		if requested, err = loadRequestedRuntime(requestedName); err != nil {
			return nil, err
		}
	}

	bindingLock.Lock()
	defer bindingLock.Unlock()

	metadata := freshSessionMetadata(sessionID)
	stored, _ := metadata[sessionRuntimeBindingKey].(map[string]any)
	if kind, _ := stored["kind"].(string); kind == externalHarnessKind {
		boundName, _ := stored["agent_template_name"].(string)
		boundProvider, _ := stored["provider_name"].(string)
		if requested == nil {
			return nil, bindingConflictError(boundName, boundProvider)
		}
		if requested.runtime == nil ||
			requested.runtimeName != boundName ||
			requested.runtime.ProviderName != boundProvider {
			return nil, bindingConflictError(boundName, boundProvider)
		}
		return &ResolvedExternalHarnessBinding{
			Kind:              externalHarnessKind,
			AgentTemplateName: requested.runtimeName,
			ProviderName:      requested.runtime.ProviderName,
			Runtime:           requested.runtime,
			PackagePath:       requested.packagePath,
		}, nil
	}

	if requested == nil {
		return &ResolvedExternalHarnessBinding{Kind: deepAgentKind}, nil
	}
	if requested.runtime == nil {
		return &ResolvedExternalHarnessBinding{Kind: deepAgentKind}, nil
	}
	return &ResolvedExternalHarnessBinding{
		Kind:              externalHarnessKind,
		AgentTemplateName: requested.runtimeName,
		ProviderName:      requested.runtime.ProviderName,
		Runtime:           requested.runtime,
		PackagePath:       requested.packagePath,
	}, nil
}

// PersistExternalHarnessBinding 会话子端接受绑定后持久化不可变 External 绑定。
// Python: :253-284 persist_external_harness_binding。非 external/字段缺失静默返回；
// 已有同绑定幂等；冲突抛 ExternalHarnessBindingError。
func PersistExternalHarnessBinding(sessionID string, binding *ResolvedExternalHarnessBinding) error {
	if binding == nil || binding.Kind != externalHarnessKind {
		return nil
	}
	agentTemplateName := strings.TrimSpace(binding.AgentTemplateName)
	providerName := strings.TrimSpace(binding.ProviderName)
	if agentTemplateName == "" || providerName == "" {
		return nil
	}
	bindingLock.Lock()
	defer bindingLock.Unlock()

	metadata := freshSessionMetadata(sessionID)
	stored, _ := metadata[sessionRuntimeBindingKey].(map[string]any)
	if kind, _ := stored["kind"].(string); kind == externalHarnessKind {
		boundName, _ := stored["agent_template_name"].(string)
		boundProvider, _ := stored["provider_name"].(string)
		if boundName == agentTemplateName && boundProvider == providerName {
			return nil
		}
		return bindingConflictError(boundName, boundProvider)
	}
	updateSessionMetadataFields(sessionID, map[string]any{
		sessionRuntimeBindingKey: map[string]any{
			"kind":               externalHarnessKind,
			"agent_template_name": agentTemplateName,
			"provider_name":      providerName,
		},
	})
	return nil
}

// LoadPersistedExternalHarnessBinding 返回已持久化的 External 绑定（含 runtime 与包路径）。
// Python: :287-301 load_persisted_external_harness_binding。无绑定/名字缺失返回 nil。
func LoadPersistedExternalHarnessBinding(sessionID string) (*ResolvedExternalHarnessBinding, error) {
	metadata := freshSessionMetadata(sessionID)
	stored, _ := metadata[sessionRuntimeBindingKey].(map[string]any)
	if kind, _ := stored["kind"].(string); kind != externalHarnessKind {
		return nil, nil
	}
	templateName := strings.TrimSpace(storedString(stored, "agent_template_name"))
	if templateName == "" {
		return nil, nil
	}
	return ResolveExternalHarnessBinding(sessionID, map[string]any{
		"agent_template_name": templateName,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requestedRuntime _load_requested_runtime 的返回三元组。
type requestedRuntime struct {
	runtimeName string
	packagePath string
	runtime     *hschema.AgentRuntimeSpec
}

// errorText 取双语错误文案并代入占位符。
// Python: :145-156 _error_text（preferred_language=="en" 走英文；读配置失败回中文）。
// values 键为 Python format 名；{name!r} 用 %q 形态替换。
func errorText(key string, values map[string]string) string {
	pair, ok := errorTexts[key]
	if !ok {
		return key
	}
	text := pair[0]
	if preferredLanguageEnglish() {
		text = pair[1]
	}
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"!r}", fmt.Sprintf("%q", value))
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// preferredLanguageEnglish 判定宿主首选语言是否英文。
// Python: :148-155（get_config().preferred_language == "en"；异常回中文）。
// Go 侧读 config 失败同样回中文（偏差 D-18-3：config API 形态差异）。
func preferredLanguageEnglish() bool {
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return false
	}
	raw, err := cfg.Load()
	if err != nil {
		return false
	}
	lang, _ := raw["preferred_language"].(string)
	return strings.TrimSpace(lang) == "en"
}

// requestedTemplateName 取请求中的 agent_template_name 并校验类型。
// Python: :159-167 _requested_template_name（None→空串；非字符串→报错；strip 空归空）。
func requestedTemplateName(params map[string]any) (string, error) {
	if params == nil {
		return "", nil
	}
	value, ok := params["agent_template_name"]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s", errorText("template_name_type", nil))
	}
	text = strings.TrimSpace(text)
	return text, nil
}

// loadRequestedRuntime 定位专家并从 manifest 卡片读 runtime（不整包加载）。
// Python: :170-184 _load_requested_runtime（equipment 查表 → 目录解析 → 只读
// manifest.json 卡片 → agent_template_runtime 校验；整包解析在 create_harness 内）。
func loadRequestedRuntime(agentTemplateName string) (*requestedRuntime, error) {
	runtimeName, err := equipmentpkg.ResolveEquipmentRuntimeID(agentTemplatesKind, agentTemplateName)
	if err != nil {
		return nil, err
	}
	packagePath, err := equipmentpkg.ResolveAgentTemplateDir(runtimeName)
	if err != nil {
		return nil, err
	}
	card, err := hresources.ReadAgentTemplateCard(filepath.Join(packagePath, "manifest.json"))
	if err != nil {
		return nil, err
	}
	runtime, err := equipmentpkg.AgentTemplateRuntime(card)
	if err != nil {
		return nil, err
	}
	return &requestedRuntime{runtimeName: runtimeName, packagePath: packagePath, runtime: runtime}, nil
}

// bindingConflictError 构造绑定冲突错误。
// Python: :187-197 _binding_conflict_error。
func bindingConflictError(boundTemplateName, boundProviderName string) error {
	return &ExternalHarnessBindingError{Message: errorText("binding_conflict", map[string]string{
		"template": boundTemplateName,
		"provider": boundProviderName,
	})}
}

// freshSessionMetadata 绕缓存读会话元数据（cache_bust=True 等价）。
// 偏差 D-18-3：Go 无 update_session_metadata 任意键入口与 cache_bust 形参——
// 读走 ReadSessionMetadata 直读磁盘，写走 updateSessionMetadataFields（写后清缓存）。
func freshSessionMetadata(sessionID string) map[string]any {
	return session.ReadSessionMetadata(session.GetSessionsDir(), sessionID)
}

// updateSessionMetadataFields 合并写任意键并清缓存。
// 偏差 D-18-3：Python update_session_metadata(sync_write=True, cache_bust=True,
// touch_last_message_at=False) → Go 直写盘（WriteSessionMetadata 同步）+ 清缓存；
// 不触碰 last_message_at（本助手只写调用方给定键）。
func updateSessionMetadataFields(sessionID string, fields map[string]any) {
	metadata := session.ReadSessionMetadata(session.GetSessionsDir(), sessionID)
	if metadata == nil {
		metadata = map[string]any{}
	}
	for key, value := range fields {
		metadata[key] = value
	}
	_ = session.WriteSessionMetadata(session.GetSessionsDir(), sessionID, metadata)
	session.RemoveSessionMetadataCache(sessionID)
}

// storedString 从元数据 map 取字符串字段（非字符串归空串）。
func storedString(stored map[string]any, key string) string {
	text, _ := stored[key].(string)
	return text
}
