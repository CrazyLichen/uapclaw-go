// 本文件对齐 Python jiuwenswarm/server/personal_context/host_api.py（2061 行）。
// JiuwenSwarm 内嵌 PersonalContext 运行时的进程内宿主边界：Host 持有配置文件与 Core 生命周期，
// 将 Context 查询委托给 Core。不暴露 transport、不创建第二个服务对象、不自行读取 Context 文件。
//
// 已知偏差（spec docs/superpowers/specs/2026-12-25-24.3-personal-context-host-design.md §7）：
//   - D-24.3-1 asyncio.Lock → sync.Mutex
//   - D-24.3-2 Core PersonalContext.Error/Config 未迁：错误载体 HostError 暂驻本包；
//     _build_core_config/_prepare_stored_config 的 Core 校验半区由本包轻量归一化代替
//   - D-24.3-4 PAT 网络验证（_validate_repository_pat/_validate_repository_pat_for_write/
//     _REPOSITORY_USER_URLS/_MAX_PAT_RESPONSE_BYTES，host_api.py:654-757）⤴️ 24.3b：
//     本批授权方法面保留表单校验，验证统一返回未授权空态
//   - D-24.3-5 生命周期空转态：start 置状态不启动 Core
//   - D-24.3-6 Python 模块级函数读取全局 get_default_models()/get_config()；
//     Go 无全局 config，Host 构造期注入 *config.Config，helper 以参数接收 models 列表

package personalcontext

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PersonalContextHostAPI 配置与控制内嵌 PersonalContext 的唯一 JiuwenSwarm API
// （Python host_api.py:758 PersonalContextHostAPI）。
type PersonalContextHostAPI struct {
	// home 配置文件所在目录（Python self._home，host_api.py:761）
	home string
	// core Core 委托面（Python self._personal_context；本批恒 nil，24.3a 注入）
	core coreRuntime
	// mu 操作互斥锁（Python self._operation_lock asyncio.Lock，D-24.3-1）
	mu mutex
	// storedConfig 已加载的存储配置（Python self._stored_config；nil=未配置）
	storedConfig map[string]any
	// coreConfig Core 配置（Python self._config；Core 未迁恒 nil，D-24.3-2）
	coreConfig any
	// errLang 错误语言（Python self._error_language）
	errLang string
	// cfgGo 全局配置读取面（Python get_config()/get_default_models() 全局，D-24.3-6）
	cfgGo *config.Config
	// started start() 已执行标记（空转态；Python 由 Core Status 表达，D-24.3-5）
	started bool
}

// coreRuntime Core 委托面窄接口（Python Host 全文件唯一 Core import PersonalContext 的
// 边界对应物，host_api.py:24 import + self._personal_context.* 全部委托点）。
// 24.3a Core 落地后由真实 Core 实现并注入；方法集随 24.3a 按委托点逐个扩充（每处注释标 Python 行号）。
type coreRuntime interface {
}

// mutex D-24.3-1：Python asyncio.Lock 在单线程事件循环内重入安全；Go 换 sync.Mutex 并由
// 调用方保证不重入（Python _apply_*_locked 内不再加锁的次序在转写时逐点保持）。
type mutex = sync.Mutex

// ──────────────────────────── 常量 ────────────────────────────

const (
	// configFilename 配置文件名（host_api.py:36 _CONFIG_FILENAME）
	configFilename = "personal_context.yaml"
	// maxConfigBytes 配置文件 4 MiB 上限（host_api.py:37 _MAX_CONFIG_BYTES）
	maxConfigBytes = 4 * 1024 * 1024
	// stopTimeoutSeconds Core 停止预算（host_api.py:38 _STOP_TIMEOUT_SECONDS）
	stopTimeoutSeconds = 60.0
	// personalContextModelMaxRetries 模型重试上限（host_api.py:39）
	personalContextModelMaxRetries = 2
	// patValidationTimeoutSeconds PAT 验证超时（host_api.py:40）⤴️ 24.3b 随网络验证回填
	patValidationTimeoutSeconds = 15.0
)

// repositoryPATFields 仓库类 provider → 凭据字段（host_api.py:42 _REPOSITORY_PAT_FIELDS）
var repositoryPATFields = map[string]string{
	"github":  "token",
	"gitcode": "pat",
}

// directoryCapacityDefaultsMaxPages 目录页数默认（Python config.py:41 DEFAULT_MAX_PAGES_PER_DIRECTORY，
// 经 _directory_capacity_defaults 从 Core Config 读取；Core 未迁暂以常量硬编码，⤴️ 24.3a 改读 Core）
const directoryCapacityDefaultsMaxPages = 20

// directoryCapacityDefaultsMaxSubdirectories 子目录数默认（Python config.py:42，同上 ⤴️ 24.3a）
const directoryCapacityDefaultsMaxSubdirectories = 20

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPersonalContextHostAPI 创建 Host API（Python __init__，host_api.py:761-770）。
func NewPersonalContextHostAPI(home string, cfg *config.Config) *PersonalContextHostAPI {
	// Python __init__ 仅存 home 与 core 占位；error_language 在首次读取时刷新（host_api.py:761-770）
	h := &PersonalContextHostAPI{
		home:    home,
		cfgGo:   cfg,
		errLang: "cn",
	}
	h.refreshErrorLanguage()
	return h
}

// ──────────────────────────── 非导出函数（Python 模块级 helper，同序排列） ────────────────────────────

// directoryCapacityDefaults 目录容量默认值（host_api.py:49-55；Core 未迁以常量代替，⤴️ 24.3a）
func directoryCapacityDefaults() (int, int) {
	return directoryCapacityDefaultsMaxPages, directoryCapacityDefaultsMaxSubdirectories
}

// globalEmbeddingValues 全局 embedding 配置三元组（host_api.py:58-75；异常时全空）。
func globalEmbeddingValues(cfg *config.Config) (string, string, string) {
	defer func() { _ = recover() }() // Python except Exception: return None,None,None（host_api.py:61-62）
	if cfg == nil {
		return "", "", ""
	}
	embedRaw := cfg.Get("embed")
	embed, ok := embedRaw.(map[string]any)
	if !ok {
		return "", "", ""
	}
	model := strings.TrimSpace(asString(embed["embed_model"]))
	baseURL := strings.TrimSpace(asString(embed["embed_base_url"]))
	if baseURL == "" {
		baseURL = strings.TrimSpace(asString(embed["embed_api_base"]))
	}
	apiKey := strings.TrimSpace(asString(embed["embed_api_key"]))
	if model != "" && baseURL != "" && apiKey != "" {
		return model, baseURL, apiKey
	}
	return "", "", ""
}

// isUsableModelEntry 条目是否是可用的默认模型（host_api.py:78-87）
func isUsableModelEntry(entry map[string]any) bool {
	if entry == nil {
		return false
	}
	client, okC := entry["model_client_config"].(map[string]any)
	_, okR := entry["model_config_obj"].(map[string]any)
	if !okC || !okR {
		return false
	}
	return strings.TrimSpace(asString(client["model_name"])) != ""
}

// modelEntryID 生成跨请求稳定的模型标识（host_api.py:90-106）
func modelEntryID(entry map[string]any) string {
	client, ok := entry["model_client_config"].(map[string]any)
	if !ok {
		return ""
	}
	modelName := strings.TrimSpace(asString(client["model_name"]))
	if modelName == "" {
		return ""
	}
	provider := strings.TrimSpace(strings.ToLower(asString(client["client_provider"])))
	if provider != "" {
		return provider + ":" + modelName
	}
	return modelName
}

// findModelIndexByID 按稳定 ID 查找当前下标（host_api.py:109-118）
func findModelIndexByID(models []map[string]any, modelID any) int {
	if id, ok := modelID.(string); ok && id != "" {
		for index, entry := range models {
			if modelEntryID(entry) == id {
				return index
			}
		}
	}
	return -1
}

// firstUsableModel 第一个可用模型及其稳定 ID（host_api.py:121-131）
func firstUsableModel(models []map[string]any) (int, string) {
	for index, entry := range models {
		if isUsableModelEntry(entry) {
			return index, modelEntryID(entry)
		}
	}
	return -1, ""
}

// firstUsableModelIndex 兼容旧调用点的第一个可用模型下标（host_api.py:134-137）
func firstUsableModelIndex(models []map[string]any) int {
	index, _ := firstUsableModel(models)
	return index
}

// modelIndexIsUsable 下标是否落在当前可用模型上（host_api.py:140-146）
func modelIndexIsUsable(models []map[string]any, modelIndex any) int {
	idx, ok := toInt(modelIndex)
	if !ok || idx < 0 || idx >= len(models) {
		return -1
	}
	if isUsableModelEntry(models[idx]) {
		return idx
	}
	return -1
}

// reconcileModelSelection 让「稳定模型 ID + 当前下标 + 采集策略」自洽
// （host_api.py:149-178 _reconcile_model_selection，三分支逐条对应）。
func reconcileModelSelection(stored map[string]any, models []map[string]any) {
	if raw, hasID := stored["model_id"]; hasID && raw != nil {
		if matched := findModelIndexByID(models, raw); matched >= 0 {
			stored["model_index"] = matched
			return
		}
	} else if matched := modelIndexIsUsable(models, stored["model_index"]); matched >= 0 {
		stored["model_id"] = modelEntryID(models[matched])
		return
	}
	fallbackIndex, fallbackID := firstUsableModel(models)
	if fallbackIndex >= 0 {
		stored["model_index"] = fallbackIndex
		stored["model_id"] = fallbackID
		return
	}
	stored["model_index"] = nil
	stored["model_id"] = nil
	if sp, _ := stored["strategy_profile"].(string); sp == "balanced" || sp == "agent" {
		stored["strategy_profile"] = "rules"
	}
}

// defaultStrategyAndModel 默认采集策略与模型（host_api.py:181-191）
func defaultStrategyAndModel(models []map[string]any) (string, any, any) {
	modelIndex, modelID := firstUsableModel(models)
	if modelIndex < 0 {
		return "rules", nil, nil
	}
	return "agent", modelIndex, modelID
}

// initialStoredConfig 初始存储配置（host_api.py:194-208 _initial_stored_config）
func initialStoredConfig(models []map[string]any, collectionEnabled bool) map[string]any {
	maxPages, maxSubdirectories := directoryCapacityDefaults()
	strategyProfile, modelIndex, modelID := defaultStrategyAndModel(models)
	return map[string]any{
		"master_enabled":                   collectionEnabled,
		"collection_enabled":               collectionEnabled,
		"agent_use_enabled":                false,
		"strategy_profile":                 strategyProfile,
		"max_pages_per_directory":          maxPages,
		"max_subdirectories_per_directory": maxSubdirectories,
		"fetch_services":                   []any{},
		"model_index":                      modelIndex,
		"model_id":                         modelID,
		"provider_credentials":             map[string]any{},
	}
}

// unconfiguredProjection 未配置投影（host_api.py:211-225 _unconfigured_projection）
func unconfiguredProjection(models []map[string]any) map[string]any {
	maxPages, maxSubdirectories := directoryCapacityDefaults()
	strategyProfile, modelIndex, modelID := defaultStrategyAndModel(models)
	return map[string]any{
		"configured":                       false,
		"master_enabled":                   false,
		"collection_enabled":               false,
		"agent_use_enabled":                false,
		"strategy_profile":                 strategyProfile,
		"max_pages_per_directory":          maxPages,
		"max_subdirectories_per_directory": maxSubdirectories,
		"model_index":                      modelIndex,
		"model_id":                         modelID,
		"fetch_services":                   []any{},
	}
}

// hostError 构造 Host 错误（host_api.py:228-253 _host_error；cause 非 nil 时记脱敏日志）
func hostError(message string, statusName string, cause error, reason string) *HostError {
	if cause != nil {
		LogError(cause) // Python log_error(cause)（host_api.py:238）
	}
	if statusName == "" {
		statusName = "CONTEXT_PROACTIVE_CONFIG_INVALID"
	}
	return &HostError{
		StatusName: statusName,
		Code:       statusCodeForName(statusName),
		Msg:        message,
		Reason:     reason,
		Cause:      cause,
	}
}

// raiseHostError 返回错误的便捷形式（Python 直接 raise；Go 以返回值传递）
func raiseHostError(message string, statusName string, cause error, reason string) *HostError {
	return hostError(message, statusName, cause, reason)
}

// asHostError 非 HostError 包装（host_api.py:268-277 _as_host_error）
func asHostError(err error, message string, statusName string, reason string) *HostError {
	var host *HostError
	if errors.As(err, &host) {
		return host
	}
	return hostError(message, statusName, err, reason)
}

// statusCodeForName 状态名 → 数字码（Python status_codes.py:32-60 七个 CONTEXT_PROACTIVE_*；
// D-24.3-2：Core 未迁暂驻本包，⤴️ 24.3a 对齐归属）
func statusCodeForName(statusName string) string {
	switch statusName {
	case "CONTEXT_PROACTIVE_CONFIG_INVALID":
		return "154000"
	case "CONTEXT_PROACTIVE_STATE_INVALID":
		return "154001"
	case "CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR":
		return "154002"
	case "CONTEXT_PROACTIVE_FETCH_EXECUTION_ERROR":
		return "154003"
	case "CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR":
		return "154004"
	case "CONTEXT_PROACTIVE_PUBLISH_EXECUTION_ERROR":
		return "154005"
	case "CONTEXT_PROACTIVE_RUNTIME_TIMEOUT":
		return "154006"
	default:
		return "BAD_REQUEST"
	}
}

// validateServiceIntegers 服务整数字段校验（host_api.py:280-291）
func validateServiceIntegers(service map[string]any) *HostError {
	for _, field := range []string{"interval_seconds", "max_items_per_run"} {
		raw, present := service[field]
		if !present || (field == "max_items_per_run" && raw == nil) {
			continue // Python：字段缺失或 max_items_per_run 为 None 时跳过（host_api.py:282-285）
		}
		value, ok := numericValue(raw)
		if !ok || value <= 0 || value != float64(int64(value)) {
			return raiseHostError(field+" must be a positive integer", "", nil, "positive_integer")
		}
	}
	return nil
}

// rejectSymlinkChain 配置路径不得穿过 symlink（host_api.py:293-305）
func rejectSymlinkChain(path string) *HostError {
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return raiseHostError(
				"PersonalContext configuration path must not traverse a symlink",
				"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", nil, "config_file_error",
			)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// serializeConfig 配置序列化与 4 MiB 上限（host_api.py:308-326）
func serializeConfig(stored map[string]any) ([]byte, *HostError) {
	payload, err := yamlMarshal(stored)
	if err != nil {
		return nil, raiseHostError(
			"PersonalContext configuration could not be serialized",
			"", err, "config_file_error",
		)
	}
	if len(payload) > maxConfigBytes {
		return nil, raiseHostError(
			"PersonalContext configuration exceeds the 4 MiB limit", "", nil, "config_file_error",
		)
	}
	return payload, nil
}

// stageYaml 暂存临时文件（host_api.py:328-361 _stage_yaml）
func stageYaml(path string, payload []byte) (string, *HostError) {
	if herr := rejectSymlinkChain(path); herr != nil {
		return "", herr
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", raiseHostError(
			"PersonalContext configuration temporary file could not be written",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "config_file_error",
		)
	}
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", raiseHostError(
			"PersonalContext configuration temporary file could not be written",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "config_file_error",
		)
	}
	name := temporary.Name()
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		_ = os.Remove(name)
		return "", raiseHostError(
			"PersonalContext configuration temporary file could not be written",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "config_file_error",
		)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		_ = os.Remove(name)
		return "", raiseHostError(
			"PersonalContext configuration temporary file could not be written",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "config_file_error",
		)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return "", raiseHostError(
			"PersonalContext configuration temporary file could not be written",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "config_file_error",
		)
	}
	// Python contextlib.suppress(OSError): os.chmod(temporary, 0o600)（host_api.py:344-345）
	_ = os.Chmod(name, 0o600)
	return name, nil
}

// replaceYaml 原子替换（host_api.py:364-378 _replace_yaml）
func replaceYaml(temporary string, path string) *HostError {
	if herr := rejectSymlinkChain(path); herr != nil {
		return herr
	}
	if err := os.Rename(temporary, path); err != nil {
		return raiseHostError(
			"PersonalContext configuration file could not be replaced",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "config_file_error",
		)
	}
	_ = os.Chmod(path, 0o600) // Python contextlib.suppress(OSError)（host_api.py:368-369）
	return nil
}

// cleanupTemporary 清理临时文件（host_api.py:381-384）
func cleanupTemporary(path string) {
	if path != "" {
		_ = os.Remove(path) // Python suppress(FileNotFoundError)
	}
}

// publishYaml 发布配置：暂存→替换→清理（host_api.py:387-392 _publish_yaml）
func publishYaml(path string, payload []byte) *HostError {
	temporary, herr := stageYaml(path, payload)
	if herr != nil {
		return herr
	}
	defer cleanupTemporary(temporary) // Python finally: _cleanup_temporary（host_api.py:391-392）
	return replaceYaml(temporary, path)
}

// readYaml 读取配置；缺文件返回 (nil, nil) 视为未配置（host_api.py:395-441 _read_yaml）
func readYaml(path string) (map[string]any, *HostError) {
	if herr := rejectSymlinkChain(path); herr != nil {
		return nil, herr
	}
	handle, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Python FileNotFoundError → None（host_api.py:414-415）
		}
		return nil, raiseHostError(
			"PersonalContext configuration YAML could not be read",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "",
		)
	}
	defer func() { _ = handle.Close() }()
	info, err := handle.Stat()
	if err != nil {
		return nil, raiseHostError(
			"PersonalContext configuration YAML could not be read",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "",
		)
	}
	if !info.Mode().IsRegular() {
		// Python stat.S_ISREG 检查（host_api.py:401-406）
		return nil, raiseHostError(
			"PersonalContext configuration path is not a file",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", nil, "config_file_error",
		)
	}
	payload := make([]byte, maxConfigBytes+1)
	n, err := handle.Read(payload)
	if err != nil && n == 0 {
		return nil, raiseHostError(
			"PersonalContext configuration YAML could not be read",
			"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", err, "",
		)
	}
	payload = payload[:n]
	if len(payload) > maxConfigBytes {
		return nil, raiseHostError(
			"PersonalContext configuration exceeds the 4 MiB limit", "", nil, "config_file_error",
		)
	}
	loaded, err := yamlUnmarshal(payload)
	if err != nil {
		return nil, raiseHostError(
			"PersonalContext configuration YAML is invalid", "", err, "",
		)
	}
	if _, ok := loaded.(map[string]any); !ok {
		return nil, raiseHostError(
			"PersonalContext configuration YAML must contain an object", "", nil, "",
		)
	}
	return loaded.(map[string]any), nil
}

// isRuntimeActive 运行时是否活跃（host_api.py:443-444；D-24.3-5：Core Status 未迁，
// 空转态由 Host.started 近似表达，⤴️ 24.3a 替换为 Core Status 读取）
func isRuntimeActive(h *PersonalContextHostAPI) bool {
	// Python: getattr(status, "state", None) in {"STARTING", "RUNNING"}
	return false
}

// hasActiveFetch 是否有采集轮在跑（host_api.py:447-467；Core Status 未迁恒 false，⤴️ 24.3a）
func hasActiveFetch(h *PersonalContextHostAPI, serviceID string) bool {
	// Python: fetch_run_progress[*].run_state in {"running", "stopping"}
	return false
}

// resolveModelReference 解析模型下标为 (client, request) 引用（host_api.py:470-502）
func resolveModelReference(models []map[string]any, modelIndex any) (map[string]any, map[string]any, *HostError) {
	idx, ok := toInt(modelIndex)
	if !ok || idx < 0 {
		return nil, nil, raiseHostError("model_index must be a non-negative integer", "", nil, "parameter_invalid")
	}
	if idx >= len(models) {
		return nil, nil, raiseHostError("selected JiuwenSwarm model no longer exists", "", nil, "model_invalid")
	}
	entry := models[idx]
	clientRaw, okC := entry["model_client_config"].(map[string]any)
	requestRaw, okR := entry["model_config_obj"].(map[string]any)
	if !okC || !okR {
		return nil, nil, raiseHostError("selected JiuwenSwarm model is invalid", "", nil, "model_invalid")
	}
	client := deepCopyMap(clientRaw)
	request := deepCopyMap(requestRaw)
	modelName := strings.TrimSpace(popString(client, "model_name"))
	if modelName == "" {
		return nil, nil, raiseHostError("selected JiuwenSwarm model is invalid", "", nil, "model_invalid")
	}
	client["max_retries"] = personalContextModelMaxRetries // Python host_api.py:500
	request["model"] = modelName                           // Python host_api.py:501
	return client, request, nil
}

// normalizeProviderCredentials 归一化仓库凭据（host_api.py:505-538）
func normalizeProviderCredentials(value any) (map[string]map[string]string, *HostError) {
	if value == nil {
		return map[string]map[string]string{}, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, raiseHostError("provider_credentials must be an object", "", nil, "parameter_invalid")
	}
	normalized := map[string]map[string]string{}
	for provider, rawCredentials := range raw {
		field, known := repositoryPATFields[provider]
		if !known {
			return nil, raiseHostError(
				"provider_credentials contains an unsupported provider", "", nil, "parameter_invalid",
			)
		}
		credentials, okC := rawCredentials.(map[string]any)
		if !okC {
			return nil, raiseHostError(
				provider+" provider credentials must be an object", "", nil, "credentials_invalid",
			)
		}
		if len(credentials) != 1 || credentials[field] == nil {
			return nil, raiseHostError(
				provider+" provider credentials must contain only "+field, "", nil, "credentials_invalid",
			)
		}
		secret, okS := credentials[field].(string)
		if !okS || strings.TrimSpace(secret) == "" {
			return nil, raiseHostError(
				provider+" provider credential must be a non-empty string", "", nil, "credentials_invalid",
			)
		}
		normalized[provider] = map[string]string{field: strings.TrimSpace(secret)}
	}
	return normalized, nil
}

// projectService 投影单个 service（剔除 credentials）（host_api.py:541-544）
func projectService(service map[string]any) map[string]any {
	projected := deepCopyMap(service)
	delete(projected, "credentials")
	return projected
}

// projectStoredConfig 投影存储配置（host_api.py:547-560）
func projectStoredConfig(stored map[string]any) (map[string]any, *HostError) {
	projected := deepCopyMap(stored)
	if _, ok := projected["master_enabled"]; !ok {
		projected["master_enabled"] = boolOr(projected["collection_enabled"]) || boolOr(projected["agent_use_enabled"])
	}
	delete(projected, "provider_credentials")
	services, ok := projected["fetch_services"].([]any)
	if !ok {
		return nil, raiseHostError("PersonalContext fetch_services are invalid", "", nil, "")
	}
	projectedServices := make([]any, 0, len(services))
	for _, item := range services {
		service, okS := item.(map[string]any)
		if !okS {
			return nil, raiseHostError("PersonalContext fetch_services are invalid", "", nil, "")
		}
		projectedServices = append(projectedServices, projectService(service))
	}
	projected["fetch_services"] = projectedServices
	return projected, nil
}

// buildCoreConfig 构造 Core 配置（host_api.py:563-581；D-24.3-2：Core Config.from_dict 未迁，
// 本批返回剔除 Core 专属键后的归一化拷贝，⤴️ 24.3a 对齐）
func buildCoreConfig(stored map[string]any, models []map[string]any) (map[string]any, *HostError) {
	raw := deepCopyMap(stored)
	delete(raw, "provider_credentials") // Python host_api.py:565
	delete(raw, "master_enabled")       // Python host_api.py:567：总开关是 Host 侧 UI 状态
	modelIndex := raw["model_index"]
	delete(raw, "model_index")
	delete(raw, "model_id")
	delete(raw, "model_client")
	delete(raw, "model_request")
	if modelIndex != nil {
		client, request, herr := resolveModelReference(models, modelIndex)
		if herr != nil {
			return nil, herr
		}
		raw["model_client"] = client
		raw["model_request"] = request
	}
	return raw, nil
}

// semanticConfigDump 可比较配置快照（host_api.py:584-592；D-24.3-2 下对 stored map 操作）
func semanticConfigDump(stored map[string]any) map[string]any {
	dumped := deepCopyMap(stored)
	if client, ok := dumped["model_client"].(map[string]any); ok {
		// Core ModelClientConfig.client_id 每次 from_dict 都不同（host_api.py:589-591）
		delete(client, "client_id")
	}
	return dumped
}

// configsEquivalent 两份配置语义是否一致（host_api.py:595-606）
func configsEquivalent(left map[string]any, right map[string]any) bool {
	return jsonEqual(semanticConfigDump(left), semanticConfigDump(right))
}

// prepareStoredConfig 归一化存储配置（host_api.py:609-634 _prepare_stored_config）。
// D-24.3-2：Python 交给 Core 校验的 candidate/model_dump 半区由本包归一化拷贝代替。
func prepareStoredConfig(stored map[string]any, models []map[string]any) (map[string]any, *HostError) {
	if stored == nil {
		return nil, raiseHostError("PersonalContext configuration must be an object", "", nil, "")
	}
	normalized := deepCopyMap(stored)
	delete(normalized, "model_client")
	delete(normalized, "model_request")
	providerCredentials, herr := normalizeProviderCredentials(normalized["provider_credentials"])
	if herr != nil {
		return nil, herr
	}
	delete(normalized, "provider_credentials")
	// 模型不可用时先归一化，再交给 Core 校验（host_api.py:620-621）
	reconcileModelSelection(normalized, models)
	candidate, herr := buildCoreConfig(normalized, models)
	if herr != nil {
		return nil, herr
	}
	out := deepCopyMap(candidate)
	delete(out, "model_client")
	delete(out, "model_request")
	// 旧配置无 master_enabled 时按子开关补齐默认，落盘始终携带总开关字段（host_api.py:626-629）
	master := boolOr(normalized["master_enabled"])
	if _, present := normalized["master_enabled"]; !present {
		master = boolOr(normalized["collection_enabled"]) || boolOr(normalized["agent_use_enabled"])
	}
	out["master_enabled"] = master
	if _, present := normalized["model_index"]; present {
		out["model_index"] = normalized["model_index"]
	}
	out["model_id"] = normalized["model_id"]
	credentials := map[string]any{}
	for provider, value := range providerCredentials {
		credentials[provider] = map[string]any{repositoryPATFields[provider]: value[repositoryPATFields[provider]]}
	}
	out["provider_credentials"] = credentials
	return out, nil
}

// repositoryAuthorizationResult 授权状态投影（host_api.py:637-651）
func repositoryAuthorizationResult(provider string, state string, account map[string]string, errMsg string) map[string]any {
	var accountAny any
	if account != nil {
		accountAny = deepCopyMap2(account)
	}
	return map[string]any{
		"provider":         provider,
		"state":            state,
		"account":          accountAny,
		"verification_url": nil,
		"expires_at":       nil,
		"error":            errAnyOrNil(errMsg),
	}
}

// validateRepositoryPAT PAT 网络验证（host_api.py:654-740）⤴️ 24.3b（D-24.3-4：本批不移植，
// httpx→net/http 转写随 fetch 源批回填；调用点在本批走未授权空态）
func validateRepositoryPAT(ctx context.Context, provider string, secret string) (map[string]string, *HostError) {
	return nil, raiseHostError("repository provider authorization is deferred to 24.3b", "", nil, "authorization_failed")
}

// validateRepositoryPATForWrite 写路径 PAT 验证（host_api.py:743-755）⤴️ 24.3b（D-24.3-4）
func validateRepositoryPATForWrite(ctx context.Context, provider string, secret string) (map[string]string, *HostError) {
	return nil, raiseHostError(provider+" credential verification failed", "CONTEXT_PROACTIVE_CONFIG_INVALID", nil, "credentials_invalid")
}

// ──────────────────────────── 小工具（Python 语法的 Go 等价） ────────────────────────────

// toInt 数值到 int（JSON 往返后 int 会变 float64；对应 Python type(x) is int 严格判断的等价形态）
func toInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// numericValue int/float 宽松数值（Python type(value) in (int, float)，host_api.py:287）
func numericValue(raw any) (float64, bool) {
	switch v := raw.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// asString 已在 error_messages.go 定义

// boolOr Python 真值判断（host_api.py:550-551）
func boolOr(value any) bool {
	b, ok := value.(bool)
	return ok && b
}

// popString 弹出字符串键（Python client.pop("model_name", "")，host_api.py:495）
func popString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	delete(m, key)
	s, _ := v.(string)
	return s
}

// deepCopyMap 深拷贝（Python deepcopy；经 JSON 往返保证纯数据语义）
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	encoded, err := json.Marshal(src)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// deepCopyMap2 string 值 map 深拷贝（Python deepcopy(account)，host_api.py:647）
func deepCopyMap2(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// errAnyOrNil None 或字符串（Python error=error，host_api.py:650）
func errAnyOrNil(msg string) any {
	if msg == "" {
		return nil
	}
	return msg
}

// jsonEqual JSON 规范化相等比较（Python dict ==）
func jsonEqual(left any, right any) bool {
	l, errL := json.Marshal(left)
	r, errR := json.Marshal(right)
	return errL == nil && errR == nil && string(l) == string(r)
}

// yamlMarshal 配置序列化（Python yaml.safe_dump(config, allow_unicode=True, sort_keys=False)，host_api.py:310）
func yamlMarshal(value any) ([]byte, error) {
	return yaml.Marshal(value)
}

// yamlUnmarshal 配置解析（Python yaml.safe_load，host_api.py:425）
func yamlUnmarshal(payload []byte) (any, error) {
	var out any
	if err := yaml.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ──────────────────────────── 导出方法（Python 类方法，Task 3 续写） ────────────────────────────

// ErrorLanguage 当前错误语言（Python error_language property，host_api.py:772-774）
func (h *PersonalContextHostAPI) ErrorLanguage() string {
	return h.errLang
}

// refreshErrorLanguage 刷新错误语言（Python _refresh_error_language，host_api.py:776-785）
func (h *PersonalContextHostAPI) refreshErrorLanguage() {
	defer func() { _ = recover() }() // Python try/except: 保持现值
	if h.cfgGo == nil {
		return
	}
	lang := asString(h.cfgGo.Get("language"))
	if lang != "" {
		h.errLang = lang
	}
}

// defaultModels 默认模型列表（Python get_default_models() 全局调用点，host_api.py:114/128/143 等；
// D-24.3-6：Go 无全局，统一走 config.GetDefaultModels(nil) 加载默认 config.yaml）
func defaultModels() []map[string]any {
	return config.GetDefaultModels(nil)
}
