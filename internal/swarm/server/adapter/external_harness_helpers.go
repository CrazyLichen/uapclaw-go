// Python 源：jiuwenswarm/server/runtime/agent_adapter/external_harness_helpers.py（1020 行，
// 15.18 一文件对一文件移植；本文件当前含绑定半区，流/中断/启动面随批追加）。
//
// 单会话 External Harness 会话接入：会话不可变绑定（resolve/persist/load）、
// 面板配置合入、harness 懒启动/停止、输出流投影、checkpoint 导入导出、取消映射。
package adapter

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	workspacepkg "github.com/uapclaw/uapclaw-go/internal/common/workspace"
	interaction "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	stream "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	claudecode "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/claudecode"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	schema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	equipmentpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
	session "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	streamutils "github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
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
			"kind":                externalHarnessKind,
			"agent_template_name": agentTemplateName,
			"provider_name":       providerName,
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

// sessionHarnessCheckpointKey 会话元数据中 checkpoint 键。
// Python: :395/:490 metadata["session_harness_checkpoint"]
const sessionHarnessCheckpointKey = "session_harness_checkpoint"

// ──────────────────────────── 导出函数：checkpoint 半区 ────────────────────────────

// SupportsCheckpoint 判断 harness 是否声明 CHECKPOINT 能力。
// Python: :383-386 _supports_checkpoint（card.supports callable 探测）。
func SupportsCheckpoint(harness harnessprotocol.HarnessProtocol) bool {
	card := harness.Card()
	return card.Supports(harnessprotocol.HarnessCapabilityCheckpoint)
}

// LoadExternalHarnessCheckpoint 从会话元数据加载 External Harness checkpoint。
// Python: :389-437 _load_external_harness_checkpoint。步骤对齐：键缺失/模板或
// provider 不符 → nil；payload 构造失败 → Warn 丢弃；agent_id/host_session_id
// 越界 → Warn 丢弃。
func LoadExternalHarnessCheckpoint(sessionID, agentTemplateName, providerName string) *harnessprotocol.HarnessCheckpoint {
	metadata := freshSessionMetadata(sessionID)
	stored, _ := metadata[sessionHarnessCheckpointKey].(map[string]any)
	if stored == nil ||
		storedString(stored, "agent_template_name") != agentTemplateName ||
		storedString(stored, "provider_name") != providerName {
		return nil
	}
	payload, _ := stored["checkpoint"].(map[string]any)
	if payload == nil {
		return nil
	}
	checkpoint, err := harnessCheckpointFromPayload(payload)
	if err != nil {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Str("detail", visibleError(err, "invalid checkpoint")).
			Msg("Ignoring invalid External Harness checkpoint")
		return nil
	}
	expectedAgentID := expectedExternalHarnessAgentID(providerName, agentTemplateName)
	if checkpoint.AgentID != expectedAgentID || checkpoint.HostSessionID != sessionID {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Msg("Ignoring out-of-scope External Harness checkpoint")
		return nil
	}
	return checkpoint
}

// harnessCheckpointFromPayload 从元数据 payload 构造 HarnessCheckpoint。
// Python: :407-417 HarnessCheckpoint(...)（KeyError/TypeError/ValueError→异常）。
// sequence 兼容 int/float64/json.Number（JSON round-trip 数字形态，见覆盖率门禁记忆）。
func harnessCheckpointFromPayload(payload map[string]any) (*harnessprotocol.HarnessCheckpoint, error) {
	provider := storedString(payload, "provider")
	schemaVersion := storedString(payload, "schema_version")
	agentID := storedString(payload, "agent_id")
	hostSessionID := storedString(payload, "host_session_id")
	checkpointID := storedString(payload, "checkpoint_id")
	if provider == "" || schemaVersion == "" || agentID == "" || hostSessionID == "" || checkpointID == "" {
		return nil, fmt.Errorf("missing required checkpoint field")
	}
	sequence := 0
	switch value := payload["sequence"].(type) {
	case int:
		sequence = value
	case float64:
		sequence = int(value)
	case fmt.Stringer:
		// 不可达：JSON 解析只产生 int/float64
	default:
		return nil, fmt.Errorf("invalid sequence %v", payload["sequence"])
	}
	data := JsonObjectOf(payload["data"])
	var providerSessionID *string
	if text, ok := payload["provider_session_id"].(string); ok {
		providerSessionID = &text
	}
	var revision *string
	if text, ok := payload["revision"].(string); ok {
		revision = &text
	}
	return &harnessprotocol.HarnessCheckpoint{
		Provider:          provider,
		SchemaVersion:     schemaVersion,
		AgentID:           agentID,
		HostSessionID:     hostSessionID,
		CheckpointID:      checkpointID,
		Sequence:          sequence,
		Data:              data,
		ProviderSessionID: providerSessionID,
		Revision:          revision,
	}, nil
}

// JsonObjectOf 任意值→JsonObject（非 dict 归空 dict，对齐 Python `if isinstance(payload.get("data"), dict) else {}`）。
func JsonObjectOf(value any) harnessprotocol.JsonObject {
	if typed, ok := value.(harnessprotocol.JsonObject); ok {
		return typed
	}
	if typed, ok := value.(map[string]any); ok {
		return harnessprotocol.JsonObject(typed)
	}
	return harnessprotocol.JsonObject{}
}

// CheckpointMetadata checkpoint 落盘形态（模板/provider 外层 + checkpoint payload）。
// Python: :440-460 _checkpoint_metadata；data 经 json_value_to_builtin。
func CheckpointMetadata(checkpoint *harnessprotocol.HarnessCheckpoint, agentTemplateName, providerName string) map[string]any {
	return map[string]any{
		"agent_template_name": agentTemplateName,
		"provider_name":       providerName,
		"checkpoint": map[string]any{
			"provider":            checkpoint.Provider,
			"schema_version":      checkpoint.SchemaVersion,
			"agent_id":            checkpoint.AgentID,
			"host_session_id":     checkpoint.HostSessionID,
			"checkpoint_id":       checkpoint.CheckpointID,
			"sequence":            checkpoint.Sequence,
			"data":                harnessprotocol.JsonValueToBuiltin(checkpoint.Data),
			"provider_session_id": checkpoint.ProviderSessionID,
			"revision":            checkpoint.Revision,
		},
	}
}

// PersistExternalHarnessCheckpoint 导出并持久化一个 External Harness checkpoint。
// Python: :463-506 _persist_external_harness_checkpoint。
// 偏差（登记 D-18-5）：Python 经 getattr(adapter, "_external_harness_binding") 取绑定——
// Go 侧改构造期传参（binding），鸭子探测不可达。
// 步骤对齐：能力不支持/无绑定 → 跳过；导出 nil → 跳过；越界 → Warn 跳过；
// 异常 → Warn 不上抛。
func PersistExternalHarnessCheckpoint(ctx context.Context, binding *ResolvedExternalHarnessBinding, io *harnessproviders.HarnessIOAdapter, sessionID string) {
	harness := io.Harness()
	if binding == nil || !SupportsCheckpoint(harness) {
		return
	}
	checkpoint, err := harness.ExportCheckpoint(ctx)
	if err != nil {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Str("detail", visibleError(err, "checkpoint export failed")).
			Msg("External Harness checkpoint export failed")
		return
	}
	if checkpoint == nil {
		return
	}
	agentTemplateName := binding.AgentTemplateName
	providerName := binding.ProviderName
	expectedAgentID := expectedExternalHarnessAgentID(providerName, agentTemplateName)
	if checkpoint.AgentID != expectedAgentID || checkpoint.HostSessionID != sessionID {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Msg("Skipping out-of-scope External Harness checkpoint")
		return
	}
	updateSessionMetadataFields(sessionID, map[string]any{
		sessionHarnessCheckpointKey: CheckpointMetadata(checkpoint, agentTemplateName, providerName),
	})
}

// expectedExternalHarnessAgentID External Harness 的 agent 标识约定。
// Python: :425-427/:478-480 f"external_harness:{provider}:{template}"。
func expectedExternalHarnessAgentID(providerName, agentTemplateName string) string {
	return "external_harness:" + providerName + ":" + agentTemplateName
}

// visibleError 对外可见错误文案：脱敏 + 去空白 + 空回退。
// Python: :620-622 _visible_error（mask_sensitive(str(error))）。
func visibleError(err error, fallback string) string {
	message := strings.TrimSpace(utils.MaskSensitive(err.Error()))
	if message == "" {
		return fallback
	}
	return message
}

// ──────────────────────────── 常量：启动面 ────────────────────────────

// providerToCliAgent provider 名 → 面板 cli_agent 名。
// Python: :52-55 _PROVIDER_TO_CLI_AGENT = {claudecode→claude, codex→codex}；
// 偏差 D-18-1：codebuddy 为 Go 扩面（15.22 收编口径）。
var providerToCliAgent = map[string]string{
	"claudecode": "claude",
	"codex":      "codex",
	"codebuddy":  "codebuddy",
}

// cliAgentLabels 面板 cli_agent 名 → 展示名。
// Python: :56-59 _CLI_AGENT_LABELS；codebuddy 同 D-18-1。
var cliAgentLabels = map[string]string{
	"claude":    "Claude",
	"codex":     "Codex",
	"codebuddy": "Codebuddy",
}

// codexDefaultTurnIdleTimeout codex 默认轮空闲超时（秒）。
// Python: :351 turn_idle_timeout_s = 300.0
const codexDefaultTurnIdleTimeout = 300.0

// ──────────────────────────── 导出函数：启动面 ────────────────────────────

// MergeHostExternalCliConfig 把面板 external_cli_agent_* 配置合入 provider config。
// Python: :331-380 _merge_host_external_cli_config。步骤对齐：映射无此 provider →
// 原样返回；面板 entry 缺失 → cli_not_enabled；codex/claudecode 各自 setdefault
// （manifest 键优先，含显式 false/null）；cli_path 合入 + 保留路径翻译。
// activate_external_cli_runtime_paths()（:343-345）为桌面打包版专属，D-d7 已裁决
// 不移植，此处结构性省略。
func MergeHostExternalCliConfig(providerName string, packageConfig map[string]any) (map[string]any, error) {
	cliAgent, ok := providerToCliAgent[providerName]
	if !ok {
		return packageConfig, nil
	}
	entry := hostCliAgentEntry(cliAgent)
	if entry == nil {
		return nil, fmt.Errorf("%s", errorText("cli_not_enabled",
			map[string]string{"label": cliAgentLabels[cliAgent]}))
	}
	merged := make(map[string]any, len(packageConfig)+4)
	for key, value := range packageConfig {
		merged[key] = value
	}
	// manifest 键优先，含显式 false/null（setdefault 语义）。
	if providerName == "codex" {
		setDefault(merged, "bypass_approvals_and_sandbox", true)
		setDefault(merged, "system_prompt_mode", "replace")
		turnIdleTimeout := codexDefaultTurnIdleTimeout
		if raw, present := entry["codex_turn_idle_timeout_s"]; present && raw != nil {
			parsed, parseErr := toPositiveFloat(raw)
			if parseErr != nil || parsed <= 0 {
				logger.Warn(logComponent).
					Str("raw", fmt.Sprintf("%v", raw)).
					Msg("Invalid codex_turn_idle_timeout_s in external_cli_agents; using 300s")
			} else {
				turnIdleTimeout = parsed
			}
		}
		setDefault(merged, "turn_idle_timeout_s", turnIdleTimeout)
	} else if providerName == "claudecode" {
		setDefault(merged, "permission_mode", "bypassPermissions")
		setDefault(merged, "system_prompt_mode", "append")
		setDefault(merged, "max_buffer_size", claudecode.DefaultClaudeMaxBufferSize)
	}
	// codebuddy（D-18-1）：无默认注入分支，仅走下方 cli_path 合入与保留路径翻译。
	if cliPath := strings.TrimSpace(storedString(entry, "cli_path")); cliPath != "" {
		setDefault(merged, "cli_path", cliPath)
	}
	fields := []string{"cli_path"}
	if providerName == "codex" {
		fields = append(fields, "codex_bin")
	}
	for _, field := range fields {
		if text, _ := merged[field].(string); text != "" {
			merged[field] = swarmcommon.ResolvePreservedCLIPath(text)
		}
	}
	return merged, nil
}

// StartExternalHarness 创建并启动一个会话私有的 External Harness 适配器。
// Python: :509-558 start_external_harness。步骤对齐：面板配置合入 → create_harness
// （ValueError→invalid_runtime_config 包装）→ checkpoint 恢复（能力门卫）→
// build_harness_context（agent_id 约定 + ResumePolicy 分支）→ IOAdapter 启动
// （EmitTurnLifecycle=true）。
// 偏差（登记 D-18-8）：Python create_harness(manifest=package_path) 在工厂内部整包
// 解析；Go CreateHarness 吃已加载 spec，故在此处 LoadAgentTemplatePackage 一次
// （Python 注释「parsed in full once, inside create_harness」语义等价）。
func StartExternalHarness(ctx context.Context, sessionID, agentTemplateName, providerName string,
	binding *ResolvedExternalHarnessBinding, language, cwd string, env map[string]string,
) (*harnessproviders.HarnessIOAdapter, error) {
	packageConfig := runtimeConfigOf(binding)
	config, err := MergeHostExternalCliConfig(providerName, packageConfig)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(binding.PackagePath, "manifest.json")
	spec, err := hresources.LoadAgentTemplatePackage(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("%s", errorText("invalid_runtime_config",
			map[string]string{"detail": visibleError(err, "invalid manifest")}))
	}
	harness, err := harnessproviders.CreateHarness(spec, harnessproviders.CreateHarnessOptions{
		Provider: providerName,
		Config:   config,
		Language: language,
	})
	if err != nil {
		return nil, fmt.Errorf("%s", errorText("invalid_runtime_config",
			map[string]string{"detail": visibleError(err, "invalid runtime config")}))
	}
	var checkpoint *harnessprotocol.HarnessCheckpoint
	if SupportsCheckpoint(harness) {
		checkpoint = LoadExternalHarnessCheckpoint(sessionID, agentTemplateName, providerName)
	}
	resumePolicy := harnessprotocol.ResumePolicyNew
	if checkpoint != nil {
		resumePolicy = harnessprotocol.ResumePolicyResumeIfAvailable
	}
	harnessContext, err := harnessproviders.BuildHarnessContext(spec, harnessproviders.BuildHarnessContextOptions{
		Provider:      providerName,
		HostSessionID: sessionID,
		AgentID:       expectedExternalHarnessAgentID(providerName, agentTemplateName),
		AgentName:     agentTemplateName,
		Language:      language,
		Cwd:           cwd,
		Env:           env,
		ResumePolicy:  resumePolicy,
		Checkpoint:    checkpoint,
	})
	if err != nil {
		return nil, err
	}
	io := harnessproviders.NewHarnessIOAdapter(harness, harnessproviders.HarnessIOAdapterOptions{
		EmitTurnLifecycle: true,
	})
	if err := io.Start(ctx, harnessContext); err != nil {
		return nil, err
	}
	return io, nil
}

// StopExternalHarness 停止并摘除一个会话私有的 External Harness 适配器。
// Python: :561-570 stop_external_harness。步骤对齐：无 io → no-op；有 session_id →
// 先持久化 checkpoint → io.Stop。Python 的 adapter._external_harness = None 归属
// 会话子适配器状态（Task 9 接线点④），此处由调用方管理引用。
func StopExternalHarness(ctx context.Context, binding *ResolvedExternalHarnessBinding,
	io *harnessproviders.HarnessIOAdapter, sessionID string) error {
	if io == nil {
		return nil
	}
	if sessionID != "" {
		PersistExternalHarnessCheckpoint(ctx, binding, io, sessionID)
	}
	return io.Stop(ctx)
}

// ──────────────────────────── 非导出函数：启动面 ────────────────────────────

// runtimeConfigOf 绑定中 runtime.config 的拷贝。
// Python: :304-308 _runtime_config（RuntimeSpec 分支）；
// 偏差（登记 D-18-5 同条）：鸭子 Mapping 分支不可达，Go 只收具体类型。
func runtimeConfigOf(binding *ResolvedExternalHarnessBinding) map[string]any {
	if binding == nil || binding.Runtime == nil {
		return map[string]any{}
	}
	config := make(map[string]any, len(binding.Runtime.Config))
	for key, value := range binding.Runtime.Config {
		config[key] = value
	}
	return config
}

// hostExternalCliAgents 读面板 external_cli_agents 配置列表。
// Python: :311-319 _host_external_cli_agents（沿 EXTERNAL_CLI_AGENTS_CONFIG_PATH
// = modes.team.jiuwen_team.external_cli_agents 逐段取值；非映射/非列表归空）。
func hostExternalCliAgents() []any {
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return nil
	}
	raw, err := cfg.Load()
	if err != nil {
		return nil
	}
	var current any = raw
	for _, segment := range []string{"modes", "team", "jiuwen_team", "external_cli_agents"} {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = mapping[segment]
	}
	list, ok := current.([]any)
	if !ok {
		return nil
	}
	return list
}

// hostCliAgentEntry 在面板配置中找指定 cli_agent 的 entry。
// Python: :322-328 _host_cli_agent_entry（字符串项 → {"cli_agent": name}；
// dict 项按 cli_agent 字段匹配）。
func hostCliAgentEntry(cliAgent string) map[string]any {
	for _, item := range hostExternalCliAgents() {
		if text, ok := item.(string); ok && text == cliAgent {
			return map[string]any{"cli_agent": cliAgent}
		}
		if mapping, ok := item.(map[string]any); ok &&
			storedString(mapping, "cli_agent") == cliAgent {
			return mapping
		}
	}
	return nil
}

// setDefault 键缺失时写入（Python dict.setdefault 等价；manifest 显式键优先）。
func setDefault(target map[string]any, key string, value any) {
	if _, present := target[key]; !present {
		target[key] = value
	}
}

// toPositiveFloat 面板超时值解析（int/float64/json.Number/字符串数字）。
// Python: :354-358 float(raw) + try/except。
func toPositiveFloat(raw any) (float64, error) {
	switch value := raw.(type) {
	case int:
		return float64(value), nil
	case float64:
		return value, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid number: %v", raw)
	}
}

// ──────────────────────────── 常量：流面/中断面 ────────────────────────────

const (
	// interactionChunkType 交互块类型。
	// Python: :599/:745 chunk.type == INTERACTION（openjiuwen INTERACTION 常量）
	interactionChunkType = "__interaction__"
	// externalWorkspaceInputsKey inputs 中 project_dir 键。
	// Python: :629-630 inputs.get("project_dir")
	externalWorkspaceInputsKey = "project_dir"
)

// terminalLifecycleKinds 轮终态 kind 集。
// Python: :51 _TERMINAL_KINDS = {"finished", "failed", "aborted"}
var terminalLifecycleKinds = map[string]bool{
	"finished": true, "failed": true, "aborted": true,
}

// ──────────────────────────── 结构体：会话运行态 ────────────────────────────

// ExternalHarnessSessionState 会话子适配器上的 External Harness 运行态。
// Python: interface_deep.py adapter 字段 _external_harness_binding / _external_harness /
// _project_dir / _parent_session_id（鸭子 getattr → Go 具体类型，偏差 D-18-5）。
type ExternalHarnessSessionState struct {
	// Binding 已解析绑定（Task 3 面）
	Binding *ResolvedExternalHarnessBinding
	// IO 会话私有 IOAdapter（懒启动，nil=未启动）
	IO *harnessproviders.HarnessIOAdapter
	// ParentSessionID 父会话标识
	ParentSessionID string
	// ProjectDir 对话项目目录（一次采纳）
	ProjectDir string
	// ResolveLanguage 运行时语言解析（nil 回 "zh"；Python _resolve_runtime_language）
	ResolveLanguage func() string
	// Converter 交互 payload 转换函数（透传 utils.ParseStreamChunk）
	Converter streamutils.InteractionConverterFunc
}

// ──────────────────────────── 导出函数：流面 ────────────────────────────

// StreamTurnOutput 只投递当前请求边界内的有序输出。
// Python: :573-598 stream_turn_output。步骤对齐：own 初值=resume；turn_lifecycle
// started 圈定本轮 turn_id；own 且终态 → yield 并收口；非 own 帧跳过；
// INTERACTION 帧 yield 后收口。
func StreamTurnOutput(ctx context.Context, io *harnessproviders.HarnessIOAdapter,
	receiptTurnID string, resume bool) <-chan stream.OutputSchema {
	out := make(chan stream.OutputSchema)
	go func() {
		defer close(out)
		iterator := io.Outputs()
		own := resume
		for {
			item, ok := iterator.Next(ctx)
			if !ok {
				return
			}
			chunk, ok := item.(stream.OutputSchema)
			if !ok {
				continue
			}
			if chunk.Type == harnessproviders.TurnLifecycleChunkType {
				payload := payloadToMap(chunk.Payload)
				kind := storedString(payload, "kind")
				if kind == "started" {
					own = storedString(payload, "turn_id") == receiptTurnID
					continue
				}
				if own && terminalLifecycleKinds[kind] {
					out <- chunk
					return
				}
				// 非本轮生命周期标记跳过（Python debug 日志同义）
				continue
			}
			if !own {
				continue
			}
			out <- chunk
			if chunk.Type == interactionChunkType {
				return
			}
		}
	}()
	return out
}

// ProcessExternalHarnessStream 运行一个 External Harness 请求并投影聊天流。
// Python: :784-952 process_external_harness_stream（async generator → channel；
// 步骤逐条对齐：绑定工作区 → 懒启动 → resume/RUNNING 门卫 → send →
// turn 边界流 → 终态三分支/交互挂起 → 异常收口 → 提前关闭收口）。
// 偏差（登记 D-18-10）：Python AgentResponseChunk 携带 request.metadata；
// Go chunk schema 无 metadata 字段（internal/swarm/schema/agent.go:81），
// 元数据不透传，登记 schema 形态差异。
func (d *DeepAdapter) ProcessExternalHarnessStream(ctx context.Context,
	state *ExternalHarnessSessionState, request *schema.AgentRequest,
	inputs map[string]any) <-chan *schema.AgentResponseChunk {
	out := make(chan *schema.AgentResponseChunk, 64)
	go func() {
		defer close(out)
		responseChunk := func(payload map[string]any, complete bool, runtimeCompletion string) *schema.AgentResponseChunk {
			// Python: :603-617 _response_chunk（metadata 透传见 D-18-10）
			chunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID, payload)
			chunk.IsComplete = complete
			chunk.RuntimeCompletion = runtimeCompletion
			return chunk
		}

		// 1. 采纳对话项目（一次）
		bindExternalWorkspace(state, inputs)
		// 2. 懒启动
		io, err := d.ensureExternalHarness(ctx, state, request)
		if err != nil {
			logger.Warn(logComponent).Str("session_id", requestSessionID(request)).
				Str("detail", visibleError(err, "startup failed")).
				Msg("External Harness startup failed")
			out <- responseChunk(map[string]any{
				"event_type": "chat.error",
				"error":      visibleError(err, errorText("startup_failed", nil)),
			}, true, "")
			return
		}

		// 3. resume/RUNNING 门卫
		query := inputs["query"]
		resumeInput, resume := query.(*interaction.InteractiveInput)
		if resume && !io.IsPendingInterruptResumeValid(resumeInput) {
			out <- responseChunk(map[string]any{
				"event_type": "chat.error",
				"error":      errorText("no_pending_interaction", nil),
			}, true, "")
			return
		}
		if !resume && io.State() == harnessprotocol.HarnessStateRunning {
			message := errorText("task_running", nil)
			if io.HasPendingInterrupt() {
				message = errorText("waiting_for_interaction", nil)
			}
			out <- responseChunk(map[string]any{
				"event_type": "chat.error",
				"error":      message,
			}, true, "")
			return
		}

		// 4. send
		receipt, err := io.Send(ctx, query, false)
		if err != nil {
			logger.Warn(logComponent).Str("session_id", requestSessionID(request)).
				Str("detail", visibleError(err, "input rejected")).
				Msg("External Harness input was rejected")
			out <- responseChunk(map[string]any{
				"event_type": "chat.error",
				"error":      visibleError(err, errorText("input_rejected", nil)),
			}, true, "")
			return
		}
		if !resume && receipt == nil {
			out <- responseChunk(map[string]any{
				"event_type": "chat.error",
				"error":      errorText("turn_not_accepted", nil),
			}, true, "")
			return
		}

		// 5. turn 边界流
		receiptTurnID := ""
		if receipt != nil {
			receiptTurnID = receipt.TurnID
		}
		st := newStreamParseState()
		for chunk := range StreamTurnOutput(ctx, io, receiptTurnID, resume) {
			// 5a. 终态：persist checkpoint + 收口（三分支）
			if chunk.Type == harnessproviders.TurnLifecycleChunkType {
				payload := payloadToMap(chunk.Payload)
				kind := storedString(payload, "kind")
				PersistExternalHarnessCheckpoint(ctx, state.Binding, io, requestSessionID(request))
				if kind == "finished" {
					finalOutput, _ := payload["final_output"].(string)
					out <- responseChunk(map[string]any{
						"event_type": "chat.final",
						"content":    finalOutput,
					}, true, "completed")
				} else if kind == "failed" {
					out <- responseChunk(map[string]any{
						"event_type": "chat.error",
						"error":      visibleErrorStr(storedString(payload, "error_message"), errorText("turn_failed", nil)),
					}, true, "completed")
				} else {
					out <- responseChunk(map[string]any{
						"event_type": "chat.final",
						"content":    "",
					}, true, "completed")
				}
				return
			}
			// 5b. 逐块投影
			parsed := d.parseExternalChunk(chunk, st)
			if parsed == nil {
				continue
			}
			if chunk.Type == "llm_output" && storedString(parsed, "event_type") == "chat.delta" {
				st.hasStreamedContent = true
			}
			isInteraction := chunk.Type == interactionChunkType
			runtimeCompletion := ""
			if isInteraction {
				runtimeCompletion = "suspended"
			}
			out <- responseChunk(parsed, isInteraction, runtimeCompletion)
			if isInteraction {
				return
			}
		}

		// 6. 流在本轮完成前关闭
		out <- responseChunk(map[string]any{
			"event_type": "chat.error",
			"error":      errorText("stream_closed", nil),
		}, true, "")
	}()
	return out
}

// parseExternalChunk external 侧逐块投影。
// Python: :769-781 _parse_external_chunk —— user_input 中断先投影 ask_user 卡片，
// 其余全走 parse_stream_chunk（run_stream_parser）。对齐后 utils.ParseStreamChunk
// 已含 llm_output/llm_reasoning parse 路径分支（stream_utils.go，Python :21161），
// 不再经循环侧的 parseAccumulatingChunk；llm_usage 在 parse 路径无分支 → nil 跳过
// （Python 同义）。has_streamed_answer 由调用方按 delta 帧置位。
func (d *DeepAdapter) parseExternalChunk(chunk stream.OutputSchema, st *streamParseState) map[string]any {
	if projected := projectExternalUserInput(chunk); projected != nil {
		return projected
	}
	return streamutils.ParseStreamChunk(&chunk, st.usage, st.emittedAskUserIDs, d.interactionConverter,
		st.hasStreamedContent)
}

// projectExternalUserInput 把 harness user_input 中断投影成 ask_user 卡片。
// Python: :743-766 _project_external_user_input。payload 双形态（对象 id/value 属性
// 或 dict 键）；value.kind == "user_input" 才投影。
func projectExternalUserInput(chunk stream.OutputSchema) map[string]any {
	if chunk.Type != interactionChunkType {
		return nil
	}
	// Python :748-755 的「对象带 id/value 属性」分支在 Go 侧不可达——interactionChunk
	// 投影的 payload 恒为 dict（偏差登记 D-18-5）；只走 dict 路径。
	payload := payloadToMap(chunk.Payload)
	if payload == nil {
		return nil
	}
	requestID := storedString(payload, "id")
	value := payload["value"]
	valueMap, ok := value.(map[string]any)
	if !ok || storedString(valueMap, "kind") != "user_input" {
		return nil
	}
	return map[string]any{
		"event_type":   "chat.ask_user_question",
		"request_id":   requestID,
		"questions":    externalAskQuestions(valueMap),
		"source":       "ask_user_interrupt",
		"interrupt_id": requestID,
		"kind":         saschema.InterruptKindAskUser,
	}
}

// externalQuestionOptions 归一化问题选项并追加 Other 兜底项。
// Python: :676-693 _external_question_options。
func externalQuestionOptions(rawOptions any) []map[string]string {
	list, ok := rawOptions.([]any)
	if !ok {
		return nil
	}
	var options []map[string]string
	for _, item := range list {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := strings.TrimSpace(storedString(mapping, "label"))
		if label == "" {
			continue
		}
		normalized := map[string]string{"label": label}
		if description := strings.TrimSpace(storedString(mapping, "description")); description != "" {
			normalized["description"] = description
		}
		options = append(options, normalized)
	}
	if len(options) > 0 {
		options = append(options, map[string]string{"label": "Other", "description": "Custom input"})
	}
	return options
}

// externalAskQuestions 归一化 user_input 值的问题列表；无 questions 时回退 prompt+choices。
// Python: :696-740 _external_ask_questions。
func externalAskQuestions(value map[string]any) []map[string]any {
	var rawQuestions any
	if providerData, ok := value["provider_data"].(map[string]any); ok {
		rawQuestions = providerData["questions"]
		if _, isList := rawQuestions.([]any); !isList {
			if toolInput, ok := providerData["input"].(map[string]any); ok {
				rawQuestions = toolInput["questions"]
			}
		}
	}
	questions := []map[string]any{}
	if list, ok := rawQuestions.([]any); ok {
		for _, item := range list {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			text := strings.TrimSpace(storedString(mapping, "question"))
			if text == "" {
				continue
			}
			multiSelect := false
			switch typed := mapping["multi_select"].(type) {
			case bool:
				multiSelect = typed
			case nil:
				if ms, ok := mapping["multiSelect"].(bool); ok {
					multiSelect = ms
				}
			}
			header := strings.TrimSpace(storedString(mapping, "header"))
			if header == "" {
				header = "Question"
			}
			questions = append(questions, map[string]any{
				"question":     text,
				"header":       header,
				"options":      externalQuestionOptions(mapping["options"]),
				"multi_select": multiSelect,
			})
		}
	}
	if len(questions) > 0 {
		return questions
	}
	// 回退 prompt + choices
	prompt := strings.TrimSpace(storedString(value, "prompt"))
	if prompt == "" {
		prompt = "需要你的回答"
	}
	options := []map[string]string{}
	if choices, ok := value["choices"].([]any); ok {
		for _, choice := range choices {
			if text, ok := choice.(string); ok {
				if label := strings.TrimSpace(text); label != "" {
					options = append(options, map[string]string{"label": label})
				}
			}
		}
	}
	return []map[string]any{{
		"question":     prompt,
		"header":       "需要你的回答",
		"options":      options,
		"multi_select": false,
	}}
}

// ──────────────────────────── 导出函数：中断/工作区面 ────────────────────────────

// ProcessExternalHarnessInterrupt 把 External Harness 取消请求映射为优雅中止。
// Python: :975-1020 process_external_harness_interrupt。五分支对齐：
// 非 cancel 意图拒绝 / 无任务 / 不支持取消 / 失败 / 已请求。
func (d *DeepAdapter) ProcessExternalHarnessInterrupt(ctx context.Context,
	state *ExternalHarnessSessionState, request *schema.AgentRequest) *schema.AgentResponse {
	interruptResponse := func(success bool, message string) *schema.AgentResponse {
		response := schema.NewAgentResponse(request.RequestID, request.ChannelID)
		response.Payload = map[string]any{
			"event_type": "chat.interrupt_result",
			"intent":     "cancel",
			"success":    success,
			"message":    message,
		}
		response.Metadata = request.Metadata
		return response
	}

	params := parseParams(request.Params)
	intent := strings.TrimSpace(storedString(params, "intent"))
	if intent == "" {
		intent = "cancel"
	}
	if intent != "cancel" {
		return interruptResponse(false, errorText("unsupported_intent", map[string]string{"intent": intent}))
	}

	io := state.IO
	if io == nil || io.State() != harnessprotocol.HarnessStateRunning {
		return interruptResponse(true, errorText("nothing_to_cancel", nil))
	}
	if err := io.Abort(ctx, false); err != nil {
		var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
		if errorsAs(err, &unsupported) {
			return interruptResponse(false, errorText("cancel_unsupported", nil))
		}
		logger.Warn(logComponent).Str("session_id", requestSessionID(request)).
			Str("detail", visibleError(err, "abort failed")).
			Msg("External Harness graceful abort failed")
		return interruptResponse(false, visibleError(err, errorText("cancel_failed", nil)))
	}
	return interruptResponse(true, errorText("cancel_requested", nil))
}

// BindExternalWorkspace 采纳对话项目目录（一次；已有会话根不动）。
// Python: :625-634 _bind_external_workspace。
func BindExternalWorkspace(state *ExternalHarnessSessionState, inputs map[string]any) {
	bindExternalWorkspace(state, inputs)
}

// bindExternalWorkspace 内部实现。
func bindExternalWorkspace(state *ExternalHarnessSessionState, inputs map[string]any) {
	if strings.TrimSpace(state.ProjectDir) != "" {
		return
	}
	selected, ok := inputs[externalWorkspaceInputsKey].(string)
	if !ok {
		return
	}
	if selected = strings.TrimSpace(selected); selected != "" {
		state.ProjectDir = selected
	}
}

// ExternalWorkspace external harness 的 cwd 选择：项目目录优先，否则默认会话工作区。
// Python: :637-641 _external_workspace（expanduser + resolve(strict=False)；
// Go: Abs 近似，无 strict 语义——D-15.12-a 既有口径）。
func ExternalWorkspace(state *ExternalHarnessSessionState, sessionID string) (string, error) {
	projectDir := strings.TrimSpace(state.ProjectDir)
	if projectDir != "" {
		return filepath.Abs(projectDir)
	}
	return workspacepkg.DefaultProjectSessionWorkspaceDir(sessionID)
}

// ensureExternalHarness 取会话私有 IOAdapter，未启动则懒启动。
// Python: :644-673 _ensure_external_harness。步骤对齐：已有 → 返回；binding 缺失 →
// binding_missing；session_id 三级回退；语言解析回退 zh；启动后挂回 state。
func (d *DeepAdapter) ensureExternalHarness(ctx context.Context,
	state *ExternalHarnessSessionState, request *schema.AgentRequest) (*harnessproviders.HarnessIOAdapter, error) {
	if state.IO != nil {
		return state.IO, nil
	}
	if state.Binding == nil {
		return nil, fmt.Errorf("%s", errorText("binding_missing", nil))
	}
	sessionID := state.ParentSessionID
	if sessionID == "" && request.SessionID != nil && *request.SessionID != "" {
		sessionID = *request.SessionID
	}
	if sessionID == "" {
		sessionID = "default"
	}
	language := "zh"
	if state.ResolveLanguage != nil {
		language = state.ResolveLanguage()
	}
	io, err := StartExternalHarness(ctx, sessionID,
		state.Binding.AgentTemplateName, state.Binding.ProviderName,
		state.Binding, language,
		mustExternalWorkspace(state, sessionID), environMap())
	if err != nil {
		return nil, err
	}
	state.IO = io
	return io, nil
}

// mustExternalWorkspace cwd 解析失败回退空串（Python resolve 失败抛错——
// Go 磁盘错误登记 Warn 语义由调用方错误链承载；空 cwd 交 provider 默认）。
func mustExternalWorkspace(state *ExternalHarnessSessionState, sessionID string) string {
	cwd, err := ExternalWorkspace(state, sessionID)
	if err != nil {
		return ""
	}
	return cwd
}

// requestSessionID 请求会话标识（nil→空串）。
func requestSessionID(request *schema.AgentRequest) string {
	if request.SessionID == nil {
		return ""
	}
	return *request.SessionID
}

// bindingToMap 绑定 → 线格式 map（会话调度缝 sessionAdapterOptions 的载体）。
// Python: :226-232/:241-250 dict 形态直传；runtime/package_path 经 any 携带。
func bindingToMap(binding *ResolvedExternalHarnessBinding) map[string]any {
	return map[string]any{
		"kind":                binding.Kind,
		"agent_template_name": binding.AgentTemplateName,
		"provider_name":       binding.ProviderName,
		"runtime":             binding.Runtime,
		"package_path":        binding.PackagePath,
	}
}

// bindingFromMap 线格式 map → 绑定（runtime 类型断言失败归 nil）。
func bindingFromMap(raw map[string]any) *ResolvedExternalHarnessBinding {
	if raw == nil {
		return nil
	}
	binding := &ResolvedExternalHarnessBinding{
		Kind:              storedString(raw, "kind"),
		AgentTemplateName: storedString(raw, "agent_template_name"),
		ProviderName:      storedString(raw, "provider_name"),
	}
	if runtime, ok := raw["runtime"].(*hschema.AgentRuntimeSpec); ok {
		binding.Runtime = runtime
	}
	if path, ok := raw["package_path"].(string); ok {
		binding.PackagePath = path
	}
	return binding
}

// externalBindingOfChild 读子适配器上的 External 绑定（非 DeepAdapter 归 nil）。
func externalBindingOfChild(child AgentAdapter) *ResolvedExternalHarnessBinding {
	if typed, ok := child.(*DeepAdapter); ok {
		return typed.externalHarnessBinding
	}
	return nil
}

// environMap os.environ → map（Python dict(os.environ)）。
func environMap() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		if idx := strings.IndexByte(item, '='); idx > 0 {
			env[item[:idx]] = item[idx+1:]
		}
	}
	return env
}

// errorsAs 标准库 errors.As 薄包装（保持本文件 import 面集中）。
func errorsAs(err error, target any) bool {
	return stderrors.As(err, target)
}

// visibleErrorStr 字符串版对外错误文案（error_message payload 字段）。
// Python: :895-898 _visible_error(payload.get("error_message"), fallback)。
func visibleErrorStr(message, fallback string) string {
	masked := strings.TrimSpace(utils.MaskSensitive(message))
	if masked == "" {
		return fallback
	}
	return masked
}
