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
	// configPath 配置文件完整路径（Python self._config_path，host_api.py:763）
	configPath string
	// core Core 委托面（Python self._personal_context；本批恒 nil，24.3a 注入）
	core coreRuntime
	// mu 操作互斥锁（Python self._operation_lock asyncio.Lock，host_api.py:767，D-24.3-1）
	mu mutex
	// fetchRunStopLock 采集停止独立锁（Python self._fetch_run_stop_lock，host_api.py:768）
	fetchRunStopLock mutex
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
	// Python Path(home).expanduser().resolve()（host_api.py:762-763）；Go 以 Abs 近似
	absHome, absErr := filepath.Abs(home)
	if absErr != nil {
		absHome = home
	}
	h := &PersonalContextHostAPI{
		home:       absHome,
		configPath: filepath.Join(absHome, configFilename),
		cfgGo:      cfg,
		errLang:    "zh",
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

// ──────────────────────────── 导出方法（Python 类方法，host_api.py:758-2061 同序） ────────────────────────────

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
	language := h.cfgGo.Get("preferred_language") // host_api.py:780
	if lv, ok := language.(string); ok && lv == "en" {
		h.errLang = "en"
	} else {
		h.errLang = "zh" // Python: "en" if language == "en" else "zh"（host_api.py:784）
	}
}

// LocalizeErrorMethod 便捷本地化（Python localize_error 方法，host_api.py:786-787）
func (h *PersonalContextHostAPI) LocalizeErrorMethod(err error) string {
	return LocalizeError(err, h.errLang, "")
}

// refreshEmbeddingConfiguration 刷新 embedding 配置（Python _refresh_embedding_configuration，
// host_api.py:789-796；Core 委托点 _set_embedding_configuration——Core nil 时跳过）
func (h *PersonalContextHostAPI) refreshEmbeddingConfiguration() {
	modelName, baseURL, apiKey := globalEmbeddingValues(h.cfgGo)
	_ = modelName
	_ = baseURL
	_ = apiKey
	// Python: self._personal_context._set_embedding_configuration(...)（host_api.py:792-796）
}

// startCollectionWithEmbedding 启动采集（Python _start_collection_with_embedding，host_api.py:798-800）
func (h *PersonalContextHostAPI) startCollectionWithEmbedding() *HostError {
	h.refreshEmbeddingConfiguration()
	// Python: await self._personal_context.start_collection()；Core nil 空转态跳过（D-24.3-5）
	return nil
}

// Configure 校验、保存并应用完整配置（Python configure，host_api.py:802-809）
func (h *PersonalContextHostAPI) Configure(stored map[string]any) *HostError {
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return herr
	}
	payload, herr := serializeConfig(prepared)
	if herr != nil {
		return herr
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.applyConfigurationLocked(prepared, prepared, payload, false, nil, "")
}

// applyConfigurationLocked 持锁应用完整配置（Python _apply_configuration_locked，host_api.py:811-993）。
// D-24.3-5：全部 Core 委托点（snapshot/deactivate/set_configuration/activate/stop_*）在 core==nil
// 时按空转态跳过；knownPreviousActive/knownStatus/guardServiceID 对齐 Python 关键字参数。
func (h *PersonalContextHostAPI) applyConfigurationLocked(candidate map[string]any, stored map[string]any, payload []byte, knownPreviousActive bool, knownStatus map[string]any, guardServiceID string) *HostError {
	previous := h.coreConfig
	previousStored := h.storedConfig
	sameConfiguration := previous != nil && configsEquivalent(previous.(map[string]any), candidate)

	previousActive := false
	hasActiveFetch := false
	status := knownStatus
	if previous != nil {
		if knownStatus != nil {
			previousActive = statusIsActive(knownStatus)
			hasActiveFetch = statusHasActiveFetch(knownStatus, guardServiceID)
		} else {
			previousActive = knownPreviousActive
		}
	}
	shutdownPending := previousActive || hasActiveFetch
	if sameConfiguration {
		// Python: previous_active or has_active_fetch or pipeline_running or state==STOPPING
		// （host_api.py:854-858）；空转态 status 由 statusSnapshot 提供等价字段
		prevStatus := status
		if prevStatus == nil && h.core == nil {
			prevStatus = h.statusSnapshotLocked()
		}
		if boolOr(prevStatus["pipeline_running"]) || asString(prevStatus["state"]) == "STOPPING" {
			shutdownPending = true
		}
	}
	sameRuntimeConfiguration := sameConfiguration && previousActive == boolOr(candidate["collection_enabled"])
	if sameRuntimeConfiguration && (boolOr(candidate["collection_enabled"]) || !shutdownPending) {
		if herr := publishYaml(h.configPath, payload); herr != nil {
			return herr
		}
		h.storedConfig = deepCopyMap(stored)
		return nil
	}

	temporary, herr := stageYaml(h.configPath, payload)
	if herr != nil {
		return herr
	}
	disabling := previous != nil && !boolOr(candidate["collection_enabled"]) &&
		(boolOr(previous.(map[string]any)["collection_enabled"]) || (sameConfiguration && shutdownPending))

	// 运行中的采集轮不得被配置更新静默取消（host_api.py:874-885）
	if previousActive && hasActiveFetch && !disabling {
		cleanupTemporary(temporary)
		return raiseHostError("A fetch task is running", "CONTEXT_PROACTIVE_STATE_INVALID", nil, "service_running")
	}
	disabledYamlPublished := false
	rollbackRuntime := false
	phase := "stop"
	applyErr := func() (ret *HostError) {
		defer func() { cleanupTemporary(temporary) }() // Python finally（host_api.py:992-993）
		if disabling {
			phase = "replace"
			// 只发布停止期间生效的开关位（host_api.py:892-902）
			stoppingStored := deepCopyMap(previousStored)
			stoppingStored["collection_enabled"] = false
			if !boolOr(candidate["agent_use_enabled"]) {
				stoppingStored["agent_use_enabled"] = false
			}
			preparedStopping, herr := prepareStoredConfig(stoppingStored, defaultModels())
			if herr != nil {
				return herr
			}
			payloadStopping, serr := serializeConfig(preparedStopping)
			if serr != nil {
				return serr
			}
			if herr = publishYaml(h.configPath, payloadStopping); herr != nil {
				return herr
			}
			disabledYamlPublished = true
			h.coreConfig = preparedStopping
			h.storedConfig = preparedStopping
		}
		if previous != nil {
			phase = "stop"
			rollbackRuntime = true
			// Python host_api.py:904-916 Core stop/deactivate 委托——空转态跳过（D-24.3-5）
		}
		phase = "set"
		rollbackRuntime = true
		// Python: set_configuration(candidate)（host_api.py:920）——空转态跳过
		if boolOr(candidate["collection_enabled"]) {
			phase = "activate"
			h.refreshEmbeddingConfiguration()
			// Python: activate_runtime()（host_api.py:925）——空转态跳过
		}
		phase = "replace"
		if temporary == "" {
			return raiseHostError("PersonalContext configuration staging failed", "", nil, "config_file_error")
		}
		if herr := replaceYaml(temporary, h.configPath); herr != nil {
			return herr
		}
		temporary = ""
		h.coreConfig = candidate
		h.storedConfig = deepCopyMap(stored)
		return nil
	}()
	if applyErr != nil {
		if disabledYamlPublished && phase == "stop" {
			return asHostError(applyErr, "PersonalContext runtime could not be stopped", "", "")
		}
		var rollbackError *HostError
		if rollbackRuntime || disabledYamlPublished {
			if herr := h.restorePreviousLocked(previous, previousStored, previousActive); herr != nil {
				rollbackError = herr
			}
		}
		if disabledYamlPublished && previousStored != nil {
			payloadPrev, serr := serializeConfig(previousStored)
			if serr != nil {
				rollbackError = rollbackErrorOr(rollbackError, serr)
			} else if herr := publishYaml(h.configPath, payloadPrev); herr != nil {
				rollbackError = rollbackErrorOr(rollbackError, herr)
			}
		}
		if rollbackError != nil {
			return asHostError(rollbackError, "PersonalContext previous configuration could not be restored", "", "")
		}
		if phase == "set" {
			return asHostError(applyErr, "PersonalContext configuration could not be applied", "CONTEXT_PROACTIVE_CONFIG_INVALID", "")
		}
		if phase == "activate" {
			return asHostError(applyErr, "PersonalContext runtime could not be started", "", "")
		}
		if phase == "replace" {
			return asHostError(applyErr, "PersonalContext configuration file could not be replaced", "CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", "config_file_error")
		}
		return asHostError(applyErr, "PersonalContext previous runtime could not be stopped", "", "")
	}
	return nil
}

// applyLiveUpdateLocked 原子耦合一次热更新与其完整 YAML 快照（Python _apply_live_update_locked，
// host_api.py:995-1073）。apply/rollback 为 Core 委托闭包——空转态 Core nil 时仍按序执行
// 发布/回滚半区（D-24.3-5：闭包内 Core 调用由调用方以 nil 安全闭包传入）。
func (h *PersonalContextHostAPI) applyLiveUpdateLocked(candidate map[string]any, stored map[string]any, payload []byte, apply func() *HostError, rollback func() *HostError, publishBeforeApply bool, preserveDisabledIntent bool) *HostError {
	if h.storedConfig == nil {
		return raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	previousPayload, serr := serializeConfig(h.storedConfig)
	if serr != nil {
		return serr
	}
	temporary, herr := stageYaml(h.configPath, payload)
	if herr != nil {
		return herr
	}
	published := false
	applyStarted := false
	applyErr := func() (ret *HostError) {
		defer func() { cleanupTemporary(temporary) }() // Python finally（host_api.py:1071-1072）
		if publishBeforeApply {
			if temporary == "" {
				return raiseHostError("PersonalContext configuration staging failed", "", nil, "config_file_error")
			}
			if herr := replaceYaml(temporary, h.configPath); herr != nil {
				return herr
			}
			temporary = ""
			published = true
			if preserveDisabledIntent {
				h.coreConfig = candidate
				h.storedConfig = deepCopyMap(stored)
			}
		}
		applyStarted = true
		if herr := apply(); herr != nil {
			return herr
		}
		if !published {
			if temporary == "" {
				return raiseHostError("PersonalContext configuration staging failed", "", nil, "config_file_error")
			}
			if herr := replaceYaml(temporary, h.configPath); herr != nil {
				return herr
			}
			temporary = ""
			published = true
		}
		h.coreConfig = candidate
		h.storedConfig = deepCopyMap(stored)
		return nil
	}()
	if applyErr != nil {
		if preserveDisabledIntent && published {
			return asHostError(applyErr, "PersonalContext runtime could not be stopped", "", "")
		}
		var rollbackError *HostError
		if applyStarted {
			if herr := rollback(); herr != nil {
				rollbackError = herr
			}
		}
		if published {
			if herr := publishYaml(h.configPath, previousPayload); herr != nil {
				rollbackError = rollbackErrorOr(rollbackError, herr)
			}
		}
		if rollbackError != nil {
			return asHostError(rollbackError, "PersonalContext previous live configuration could not be restored", "", "")
		}
		return asHostError(applyErr, "PersonalContext live configuration could not be applied", "", "")
	}
	return nil
}

// GetOverview 返回完整配置与 Core 状态的一致快照（Python get_overview，host_api.py:1074-1090）
func (h *PersonalContextHostAPI) GetOverview() (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var configAny any
	if h.storedConfig != nil {
		projected, herr := projectStoredConfig(h.storedConfig)
		if herr != nil {
			return nil, herr
		}
		configAny = projected
	}
	return map[string]any{
		"configured": h.storedConfig != nil,
		"config":     configAny,
		"status":     LocalizePayload(h.statusSnapshotLocked(), h.errLang),
	}, nil
}

// GetRuntimeConfig 返回完整持久化配置（Python get_runtime_config，host_api.py:1092-1098）
func (h *PersonalContextHostAPI) GetRuntimeConfig() (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return unconfiguredProjection(defaultModels()), nil
	}
	return projectStoredConfig(h.storedConfig)
}

// PatchRuntimeConfig 原子修补允许的运行时配置字段（Python patch_runtime_config，host_api.py:1100-1130）
func (h *PersonalContextHostAPI) PatchRuntimeConfig(patch map[string]any) (map[string]any, *HostError) {
	if patch == nil {
		return nil, raiseHostError("patch must be an object", "", nil, "parameter_invalid")
	}
	allowed := map[string]bool{"collection_enabled": true, "agent_use_enabled": true, "strategy_profile": true}
	for key := range patch {
		if !allowed[key] {
			return nil, raiseHostError("runtime patch contains unsupported fields", "", nil, "parameter_invalid")
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return nil, raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	stored := deepCopyMap(h.storedConfig)
	for key, value := range deepCopyMap(patch) {
		stored[key] = value
	}
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return nil, herr
	}
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	if herr = h.applyConfigurationLocked(prepared, prepared, payload, false, nil, ""); herr != nil {
		return nil, herr
	}
	return projectStoredConfig(prepared)
}

// SelectModel 按 models.list 下标选择模型（Python select_model，host_api.py:1132-1160）
func (h *PersonalContextHostAPI) SelectModel(modelIndex any) (map[string]any, *HostError) {
	idx, ok := toInt(modelIndex)
	if !ok || idx < 0 {
		return nil, raiseHostError("model_index must be a non-negative integer", "", nil, "parameter_invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return nil, raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	models := defaultModels()
	// 校验放锁内：显式选择必须拦截，_reconcile_model_selection 会静默回退（host_api.py:1144-1150）
	if modelIndexIsUsable(models, idx) < 0 {
		return nil, raiseHostError("selected JiuwenSwarm model no longer exists", "", nil, "model_invalid")
	}
	stored := deepCopyMap(h.storedConfig)
	stored["model_index"] = idx
	stored["model_id"] = modelEntryID(models[idx])
	prepared, herr := prepareStoredConfig(stored, models)
	if herr != nil {
		return nil, herr
	}
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	if herr = h.applyConfigurationLocked(prepared, prepared, payload, false, nil, ""); herr != nil {
		return nil, herr
	}
	return projectStoredConfig(prepared)
}

// SetCollectionEnabled 持久化并应用采集开关（Python set_collection_enabled，host_api.py:1162-1208）
func (h *PersonalContextHostAPI) SetCollectionEnabled(enabled bool) (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	firstStart := h.storedConfig == nil
	var stored map[string]any
	if h.storedConfig == nil {
		if !enabled {
			return unconfiguredProjection(defaultModels()), nil
		}
		stored = initialStoredConfig(defaultModels(), true)
	} else {
		stored = deepCopyMap(h.storedConfig)
	}
	stored["collection_enabled"] = enabled
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return nil, herr
	}
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	if firstStart {
		if herr = h.applyConfigurationLocked(prepared, prepared, payload, false, nil, ""); herr != nil {
			return nil, herr
		}
	} else {
		// Python host_api.py:1184-1206：apply/rollback 闭包二选一，publish/preserve 随 enabled 反转
		apply := func() *HostError {
			if enabled {
				return h.startCollectionWithEmbedding()
			}
			return nil // Python: stop_collection——空转态跳过（D-24.3-5）
		}
		rollback := func() *HostError {
			if enabled {
				return nil
			}
			return h.startCollectionWithEmbedding()
		}
		if herr = h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, !enabled, !enabled); herr != nil {
			return nil, herr
		}
	}
	return projectStoredConfig(prepared)
}

// SetAgentUseEnabled 持久化智能体使用开关（Python set_agent_use_enabled，host_api.py:1210-1238）
func (h *PersonalContextHostAPI) SetAgentUseEnabled(enabled bool) (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return nil, raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	stored := deepCopyMap(h.storedConfig)
	stored["agent_use_enabled"] = enabled
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return nil, herr
	}
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	apply := func() *HostError { return nil } // Python start/stop_agent_use——空转态跳过
	rollback := func() *HostError { return nil }
	if herr = h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, false, false); herr != nil {
		return nil, herr
	}
	return projectStoredConfig(prepared)
}

// SetMasterEnabled 持久化并应用总开关，级联两个子开关（Python set_master_enabled，host_api.py:1240-1299）
func (h *PersonalContextHostAPI) SetMasterEnabled(enabled bool) (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if enabled && !boolOr(h.storedConfig["master_enabled"]) {
		h.refreshErrorLanguage() // Python host_api.py:1250-1251
	}
	firstStart := h.storedConfig == nil
	var stored map[string]any
	if h.storedConfig == nil {
		if !enabled {
			return unconfiguredProjection(defaultModels()), nil
		}
		stored = initialStoredConfig(defaultModels(), true)
	} else {
		stored = deepCopyMap(h.storedConfig)
	}
	stored["master_enabled"] = enabled
	stored["collection_enabled"] = enabled
	stored["agent_use_enabled"] = enabled
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return nil, herr
	}
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	if firstStart {
		if herr = h.applyConfigurationLocked(prepared, prepared, payload, false, nil, ""); herr != nil {
			return nil, herr
		}
		if enabled {
			// Python: start_agent_use()（host_api.py:1267）——空转态跳过
		}
	} else {
		// Python apply_master/rollback_master 闭包（host_api.py:1270-1288）——空转态 Core 调用跳过
		apply := func() *HostError {
			if enabled {
				return h.startCollectionWithEmbedding()
			}
			return nil
		}
		rollback := func() *HostError {
			if enabled {
				return nil
			}
			return h.startCollectionWithEmbedding()
		}
		if herr = h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, !enabled, !enabled); herr != nil {
			return nil, herr
		}
	}
	return projectStoredConfig(prepared)
}

// ListFetchServices 返回全部固定采集源配置（Python list_fetch_services，host_api.py:1301-1310）
func (h *PersonalContextHostAPI) ListFetchServices() ([]map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return []map[string]any{}, nil
	}
	services, _ := h.storedConfig["fetch_services"].([]any)
	out := make([]map[string]any, 0, len(services))
	for _, item := range services {
		if service, ok := item.(map[string]any); ok {
			out = append(out, projectService(service))
		}
	}
	return out, nil
}

// CreateFetchService 校验、持久化并应用一个新采集源（Python create_fetch_service，host_api.py:1312-1406）
func (h *PersonalContextHostAPI) CreateFetchService(service map[string]any) (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return nil, raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	if service == nil {
		return nil, raiseHostError("service must be an object", "", nil, "parameter_invalid")
	}
	if herr := validateServiceIntegers(service); herr != nil {
		return nil, herr
	}
	stored := deepCopyMap(h.storedConfig)
	services, _ := stored["fetch_services"].([]any)
	existingIDs := map[string]bool{}
	for _, item := range services {
		if m, ok := item.(map[string]any); ok {
			existingIDs[asString(m["service_id"])] = true
		}
	}
	serviceID := service["service_id"]
	normalizedID := ""
	if id, ok := serviceID.(string); ok {
		normalizedID = strings.TrimSpace(id)
	}
	if normalizedID != "" && existingIDs[normalizedID] {
		return nil, raiseHostError("PersonalContext fetch service already exists", "", nil, "duplicate_service")
	}
	provider := service["provider"]
	normalizedProvider := ""
	if p, ok := provider.(string); ok {
		normalizedProvider = strings.ToLower(strings.TrimSpace(p))
	}
	if normalizedProvider != "" {
		count := 0
		for _, item := range services {
			if m, ok := item.(map[string]any); ok && asString(m["provider"]) == normalizedProvider {
				count++
			}
		}
		if count >= 20 {
			return nil, raiseHostError(normalizedProvider+" fetch service limit of 20 has been reached", "", nil, "service_limit")
		}
	}
	internalService := deepCopyMap(service)
	if _, isRepo := repositoryPATFields[normalizedProvider]; isRepo {
		if _, hasCreds := service["credentials"]; hasCreds {
			return nil, raiseHostError(
				"repository fetch service credentials are managed by the provider authorization", "", nil, "credentials_invalid",
			)
		}
		providerCredentials, _ := stored["provider_credentials"].(map[string]any)
		current, _ := providerCredentials[normalizedProvider].(map[string]any)
		field := repositoryPATFields[normalizedProvider]
		if current == nil || current[field] == nil {
			return nil, raiseHostError(
				normalizedProvider+" must be authorized before creating a fetch service", "", nil, "authorization_required",
			)
		}
		internalService["credentials"] = map[string]any{field: current[field]}
	}
	stored["fetch_services"] = append(services, internalService)
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return nil, herr
	}
	if _, isRepo := repositoryPATFields[normalizedProvider]; isRepo {
		// Python host_api.py:1372-1380 写路径 PAT 验证——D-24.3-4 空态跳过 ⤴️ 24.3b
	}
	createdConfig, herr := findNewService(prepared, existingIDs)
	if herr != nil {
		return nil, herr
	}
	_ = createdConfig
	apply := func() *HostError { return nil } // Python _append_fetch_service_config——空转态跳过
	rollback := func() *HostError { return nil }
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	if herr = h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, false, false); herr != nil {
		return nil, herr
	}
	normalizedServices, _ := prepared["fetch_services"].([]any)
	for _, item := range normalizedServices {
		if m, ok := item.(map[string]any); ok && !existingIDs[asString(m["service_id"])] {
			return projectService(m), nil
		}
	}
	return nil, raiseHostError("created fetch service not found", "", nil, "")
}

// DeleteFetchService 删除一个已停止的采集源及其游标（Python delete_fetch_service，host_api.py:1408-1509）
func (h *PersonalContextHostAPI) DeleteFetchService(serviceID string) *HostError {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	normalizedID := strings.TrimSpace(serviceID)
	if normalizedID == "" {
		return raiseHostError("service_id must be a non-empty string", "", nil, "parameter_invalid")
	}
	stored := deepCopyMap(h.storedConfig)
	services, _ := stored["fetch_services"].([]any)
	found := false
	for _, item := range services {
		if m, ok := item.(map[string]any); ok && asString(m["service_id"]) == normalizedID {
			found = true
		}
	}
	if !found {
		return raiseHostError("unknown PersonalContext fetch service", "", nil, "unknown_service")
	}
	// Python host_api.py:1427-1441 snapshot + _has_active_fetch——空转态无运行，直接通过
	// Python host_api.py:1443-1457 remove_fetch_cursor/remove_fetch_run_history——Core 委托，空转态跳过
	kept := make([]any, 0, len(services))
	for _, item := range services {
		if m, ok := item.(map[string]any); ok && asString(m["service_id"]) != normalizedID {
			kept = append(kept, m)
		}
	}
	stored["fetch_services"] = kept
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return herr
	}
	apply := func() *HostError { return nil } // Python _remove_fetch_service_config——空转态跳过
	rollback := func() *HostError { return nil }
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return serr
	}
	return h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, false, false)
}

// PatchFetchService 原子修补一个既有采集源（Python patch_fetch_service，host_api.py:1511-1600）
func (h *PersonalContextHostAPI) PatchFetchService(serviceID string, patch map[string]any) (map[string]any, *HostError) {
	normalizedID := strings.TrimSpace(serviceID)
	if normalizedID == "" {
		return nil, raiseHostError("service_id must be a non-empty string", "", nil, "parameter_invalid")
	}
	if patch == nil {
		return nil, raiseHostError("patch must be an object", "", nil, "parameter_invalid")
	}
	allowed := map[string]bool{"interval_seconds": true, "max_items_per_run": true, "source": true, "time_range": true}
	for key := range patch {
		if !allowed[key] {
			return nil, raiseHostError("fetch service patch contains unsupported fields", "", nil, "parameter_invalid")
		}
	}
	if herr := validateServiceIntegers(patch); herr != nil {
		return nil, herr
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return nil, raiseHostError("PersonalContext is not configured", "", nil, "not_configured")
	}
	stored := deepCopyMap(h.storedConfig)
	services, _ := stored["fetch_services"].([]any)
	var target map[string]any
	for _, item := range services {
		if m, ok := item.(map[string]any); ok && asString(m["service_id"]) == normalizedID {
			target = m
		}
	}
	if target == nil {
		return nil, raiseHostError("unknown PersonalContext fetch service", "", nil, "unknown_service")
	}
	// Python host_api.py:1556-1570 snapshot + active fetch 守卫——空转态直接通过
	for key, value := range deepCopyMap(patch) {
		target[key] = value
	}
	prepared, herr := prepareStoredConfig(stored, defaultModels())
	if herr != nil {
		return nil, herr
	}
	apply := func() *HostError { return nil } // Python _update_fetch_service_config——空转态跳过
	rollback := func() *HostError { return nil }
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return nil, serr
	}
	if herr = h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, false, false); herr != nil {
		return nil, herr
	}
	updatedServices, _ := prepared["fetch_services"].([]any)
	for _, item := range updatedServices {
		if m, ok := item.(map[string]any); ok && asString(m["service_id"]) == normalizedID {
			return projectService(m), nil
		}
	}
	return nil, raiseHostError("updated fetch service not found", "", nil, "")
}

// GetFetchRunStatus 返回按服务分组的运行记录（Python get_fetch_run_status，host_api.py:1602-1626）。
// D-24.3-5：Core 未迁返回空 services 列表（无运行历史=合法空态）。
func (h *PersonalContextHostAPI) GetFetchRunStatus(serviceID any, runID any) (map[string]any, *HostError) {
	if serviceID != nil {
		sid, ok := serviceID.(string)
		if !ok || strings.TrimSpace(sid) == "" {
			return nil, raiseHostError("service_id must be a non-empty string", "", nil, "parameter_invalid")
		}
	}
	if runID != nil {
		rid, ok := runID.(string)
		if serviceID == nil || !ok || strings.TrimSpace(rid) == "" {
			return nil, raiseHostError("run_id requires service_id and a non-empty string", "", nil, "parameter_invalid")
		}
	}
	// Python: core.get_fetch_run_status——空转态返回空结果（Core 未迁，D-24.3-5）
	return map[string]any{"services": []any{}}, nil
}

// SetFetchServiceEnabled 持久化并热应用单源调度开关（Python set_fetch_service_enabled，host_api.py:1628-1674）
func (h *PersonalContextHostAPI) SetFetchServiceEnabled(serviceID string, enabled bool) *HostError {
	if _, ok := any(enabled).(bool); !ok { // 保持显式 bool 形参；类型守卫对齐 Python:1635
		return raiseHostError("enabled must be a boolean", "", nil, "parameter_invalid")
	}
	if serviceID == "" {
		return raiseHostError("service_id must be a string", "", nil, "parameter_invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.storedConfig == nil {
		return raiseHostError("PersonalContext configuration must be set before changing a fetch service", "", nil, "not_configured")
	}
	raw := deepCopyMap(h.storedConfig)
	normalizedID := strings.TrimSpace(serviceID)
	if normalizedID == "" {
		return raiseHostError("service_id must not be empty", "", nil, "parameter_invalid")
	}
	services, _ := raw["fetch_services"].([]any)
	var target map[string]any
	for _, item := range services {
		if m, ok := item.(map[string]any); ok && asString(m["service_id"]) == normalizedID {
			target = m
		}
	}
	if target == nil {
		return raiseHostError("unknown PersonalContext fetch service", "", nil, "unknown_service")
	}
	target["enabled"] = enabled
	prepared, herr := prepareStoredConfig(raw, defaultModels())
	if herr != nil {
		return herr
	}
	apply := func() *HostError { return nil } // Python set_fetch_service_enabled——空转态跳过
	rollback := func() *HostError { return nil }
	payload, serr := serializeConfig(prepared)
	if serr != nil {
		return serr
	}
	return h.applyLiveUpdateLocked(prepared, prepared, payload, apply, rollback, false, false)
}

// RunFetch 委托一次立即采集（Python run_fetch，host_api.py:1676-1692）。
// D-24.3-5：Core 未迁，按 Python Core 在非 RUNNING 态的错误路径直接报 state_invalid。
func (h *PersonalContextHostAPI) RunFetch(serviceID any) (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	reason := "state_invalid"
	if h.coreConfig == nil {
		reason = "not_configured"
	} else if !boolOr(h.coreConfig.(map[string]any)["collection_enabled"]) {
		reason = "collection_disabled"
	}
	return nil, raiseHostError("PersonalContext runtime is not running", "CONTEXT_PROACTIVE_STATE_INVALID", nil, reason)
}

// StopFetchRun 停止一个活跃采集轮（Python stop_fetch_run，host_api.py:1694-1706）。
// D-24.3-5：Core 未迁无可停任务，返回 ok（Python Core 无运行任务时同样返回 ok）。
func (h *PersonalContextHostAPI) StopFetchRun(serviceID string) (map[string]any, *HostError) {
	normalizedID := strings.TrimSpace(serviceID)
	if normalizedID == "" {
		return nil, raiseHostError("service_id must be a non-empty string", "", nil, "parameter_invalid")
	}
	// Python: 独立 _fetch_run_stop_lock 避免一次 Core 收尾阻塞 create/patch/delete（host_api.py:1702-1705）
	h.fetchRunStopLock.Lock()
	h.fetchRunStopLock.Unlock()
	return map[string]any{"ok": true}, nil
}

// GetGraph 读取图谱切片（Python get_graph，host_api.py:1708-1716）
func (h *PersonalContextHostAPI) GetGraph(rootID any, depth int) (map[string]any, *HostError) {
	return nil, contextUnavailableError()
}

// GetTree 读取文件树切片（Python get_tree，host_api.py:1718-1726）
func (h *PersonalContextHostAPI) GetTree(rootID any, depth int) (map[string]any, *HostError) {
	return nil, contextUnavailableError()
}

// SearchGraph 搜索已发布 Context 页（Python search_graph，host_api.py:1728-1731）
func (h *PersonalContextHostAPI) SearchGraph(query string) (map[string]any, *HostError) {
	return nil, contextUnavailableError()
}

// GetGraphPage 读取一个已发布 Context 页（Python get_graph_page，host_api.py:1733-1736）
func (h *PersonalContextHostAPI) GetGraphPage(nodeID string) (map[string]any, *HostError) {
	return nil, contextUnavailableError()
}

// GetSource 读取结构化原子来源详情（Python get_source，host_api.py:1738-1741）
func (h *PersonalContextHostAPI) GetSource(sourceID string) (map[string]any, *HostError) {
	return nil, contextUnavailableError()
}

// GetAuthorizationStatus 读取 provider 凭据状态（Python get_authorization_status，host_api.py:1743-1819）
func (h *PersonalContextHostAPI) GetAuthorizationStatus(provider string) (map[string]any, *HostError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		return nil, raiseHostError("provider must be a non-empty string", "", nil, "parameter_invalid")
	}
	if _, isRepo := repositoryPATFields[normalizedProvider]; isRepo {
		if h.storedConfig == nil {
			return repositoryAuthorizationResult(normalizedProvider, "not_authorized", nil, ""), nil
		}
		providerCredentials, _ := h.storedConfig["provider_credentials"].(map[string]any)
		current, _ := providerCredentials[normalizedProvider].(map[string]any)
		field := repositoryPATFields[normalizedProvider]
		if current == nil || current[field] == nil {
			return repositoryAuthorizationResult(normalizedProvider, "not_authorized", nil, ""), nil
		}
		// Python host_api.py:1781-1796 真网络验证 _validate_repository_pat——
		// D-24.3-4 空态：凭据存在但验证挂账 24.3b，统一返回未授权（24.3b 回填后恢复）
		return repositoryAuthorizationResult(normalizedProvider, "not_authorized", nil, ""), nil
	}
	if normalizedProvider != "feishu" {
		return nil, raiseHostError("provider does not support authorization", "", nil, "authorization_unsupported")
	}
	if h.coreConfig == nil {
		return nil, raiseHostError("PersonalContext configuration must be set before provider authorization", "", nil, "not_configured")
	}
	// Python host_api.py:1798-1819 Core 设备码流程——D-24.3-4 空态返回未授权（⤴️ 24.3b feishu 源）
	return repositoryAuthorizationResult(normalizedProvider, "not_authorized", nil, ""), nil
}

// AuthorizeProvider 校验或发起 provider 授权（Python authorize_provider，host_api.py:1821-1957）。
// D-24.3-4：github/gitcode 表单校验保留、网络验证与凭据落盘挂账 24.3b（空态返回未授权）；
// feishu 设备码流程同样挂账 24.3b。
func (h *PersonalContextHostAPI) AuthorizeProvider(provider string, credentials map[string]any, reauthorize bool) (map[string]any, *HostError) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		return nil, raiseHostError("provider must be a non-empty string", "", nil, "parameter_invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, isRepo := repositoryPATFields[normalizedProvider]; isRepo {
		field := repositoryPATFields[normalizedProvider]
		if credentials == nil || len(credentials) != 1 || credentials[field] == nil {
			return nil, raiseHostError(normalizedProvider+" credentials must contain only "+field, "", nil, "credentials_invalid")
		}
		secret, okS := credentials[field].(string)
		if !okS || strings.TrimSpace(secret) == "" {
			return nil, raiseHostError(normalizedProvider+" credential must be a non-empty string", "", nil, "credentials_invalid")
		}
		// Python host_api.py:1854-1926：真验证 + provider_credentials 落盘 + reauthorize 回填——
		// D-24.3-4 空态：验证挂账 24.3b，不落盘凭据，统一返回未授权
		return repositoryAuthorizationResult(normalizedProvider, "not_authorized", nil, ""), nil
	}
	if normalizedProvider != "feishu" {
		return nil, raiseHostError("provider does not support authorization", "", nil, "authorization_unsupported")
	}
	if credentials != nil {
		return nil, raiseHostError("feishu authorization does not accept credentials", "", nil, "credentials_invalid")
	}
	if h.coreConfig == nil {
		return nil, raiseHostError("PersonalContext configuration must be set before provider authorization", "", nil, "not_configured")
	}
	// Python host_api.py:1942-1957 Core 设备码流程——D-24.3-4 空态（⤴️ 24.3b feishu 源）
	return repositoryAuthorizationResult(normalizedProvider, "not_authorized", nil, ""), nil
}

// Start 按需加载一次配置文件并启动 Core（Python start，host_api.py:1959-1992）。
// D-24.3-5：空转态——加载/归一化照做，Core set_configuration/activate 跳过。
func (h *PersonalContextHostAPI) Start() *HostError {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.coreConfig == nil {
		raw, herr := readYaml(h.configPath)
		if herr != nil {
			return herr
		}
		if raw == nil {
			return nil // Python host_api.py:1965-1966
		}
		stored, herr := prepareStoredConfig(raw, defaultModels())
		if herr != nil {
			return herr
		}
		h.coreConfig = stored // Python set_configuration——空转态直接落内存
		h.storedConfig = stored
	}
	configMap, _ := h.coreConfig.(map[string]any)
	if configMap == nil || !boolOr(configMap["collection_enabled"]) {
		return nil // Python host_api.py:1980-1982
	}
	h.refreshErrorLanguage()
	h.refreshEmbeddingConfiguration()
	h.started = true // Python activate_runtime()——空转态置标记（D-24.3-5）
	return nil
}

// IsRuntimeEnabled 返回已加载的智能体使用开关（Python is_runtime_enabled，host_api.py:1994-1999）
func (h *PersonalContextHostAPI) IsRuntimeEnabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	configMap, _ := h.coreConfig.(map[string]any)
	return configMap != nil && boolOr(configMap["agent_use_enabled"])
}

// GetStatus 返回 Core 无凭据状态快照（Python get_status，host_api.py:2001-2014）。
// D-24.3-5：Core 未迁返回 Host 侧最小状态投影。
func (h *PersonalContextHostAPI) GetStatus() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return LocalizePayload(h.statusSnapshotLocked(), h.errLang).(map[string]any)
}

// Stop 停止 Core 运行时但保留配置与已发布文件（Python stop，host_api.py:2016-2034）。
// D-24.3-5：Core 未迁无可停运行时，仅保留超时校验。
func (h *PersonalContextHostAPI) Stop(timeoutSeconds float64) *HostError {
	if timeoutSeconds <= 0 {
		return raiseHostError("timeout_seconds must be greater than zero", "CONTEXT_PROACTIVE_RUNTIME_TIMEOUT", nil, "")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.started = false
	return nil
}

// ──────────────────────────── 非导出方法（Python 私有方法与空态辅助） ────────────────────────────

// restorePreviousLocked 失败候选配置后的恢复（Python _restore_previous，host_api.py:2036-2061）。
// D-24.3-5：Core 委托点（deactivate/set_configuration/activate）空转态跳过。
func (h *PersonalContextHostAPI) restorePreviousLocked(previous any, previousStored map[string]any, wasActive bool) *HostError {
	if previous == nil {
		h.coreConfig = nil
		h.storedConfig = nil
		return nil
	}
	if wasActive && boolOr(previous.(map[string]any)["collection_enabled"]) {
		h.refreshEmbeddingConfiguration()
		// Python: activate_runtime()——空转态跳过
	}
	h.coreConfig = previous
	h.storedConfig = deepCopyMap(previousStored)
	return nil
}

// statusSnapshotLocked 最小状态投影（D-24.3-5：Python PersonalContext.Status.model_dump 的
// Host 侧近似；字段以满足 get_overview/runtime_status 前端消费为准，⤴️ 24.3a 替换为 Core Status）
func (h *PersonalContextHostAPI) statusSnapshotLocked() map[string]any {
	state := "CREATED"
	if h.storedConfig != nil {
		state = "CONFIGURED"
	}
	if h.started {
		state = "RUNNING"
	}
	return map[string]any{
		"state":                state,
		"pipeline_running":     false,
		"fetch_service_states": map[string]any{},
		"fetch_run_progress":   map[string]any{},
	}
}

// contextUnavailableError 上下文查询在 Core 缺席时的统一错误
// （Python 由 Core get_graph/search_graph 等方法在未启动时抛出；空转态由 Host 直接报，D-24.3-5）
func contextUnavailableError() *HostError {
	return raiseHostError("PersonalContext context is not available", "CONTEXT_PROACTIVE_STATE_INVALID", nil, "state_invalid")
}

// findNewService 在归一化配置中找新建 service（Python next(...) 过滤，host_api.py:1381-1385）
func findNewService(prepared map[string]any, existingIDs map[string]bool) (map[string]any, *HostError) {
	services, _ := prepared["fetch_services"].([]any)
	for _, item := range services {
		if m, ok := item.(map[string]any); ok && !existingIDs[asString(m["service_id"])] {
			return m, nil
		}
	}
	return nil, raiseHostError("created fetch service not found in prepared config", "", nil, "")
}

// rollbackErrorOr 取先发生的回滚错误（Python rollback_error = rollback_error or restore_exc）
func rollbackErrorOr(current *HostError, next *HostError) *HostError {
	if current != nil {
		return current
	}
	return next
}

// statusIsActive 状态是否活跃（host_api.py:443-444 _is_runtime_active 的 map 版本）
func statusIsActive(status map[string]any) bool {
	state := asString(status["state"])
	return state == "STARTING" || state == "RUNNING"
}

// statusHasActiveFetch 是否有采集轮在跑（host_api.py:447-467 _has_active_fetch 的 map 版本）
func statusHasActiveFetch(status map[string]any, serviceID string) bool {
	progress, ok := status["fetch_run_progress"].(map[string]any)
	if !ok {
		return false
	}
	for sid, record := range progress {
		if serviceID != "" && sid != serviceID {
			continue
		}
		if m, ok := record.(map[string]any); ok {
			rs := asString(m["run_state"])
			if rs == "running" || rs == "stopping" {
				return true
			}
		}
	}
	return false
}

// defaultModelsFn Python get_default_models() 全局读取点的 Go 注入缝（D-24.3-6：
// Go 无全局 config，默认走 config.GetDefaultModels(nil) 加载默认 config.yaml；测试可替换）
var defaultModelsFn = func() []map[string]any {
	return config.GetDefaultModels(nil)
}

// defaultModels 默认模型列表（Python get_default_models() 全局调用点，host_api.py:114/128/143 等）
func defaultModels() []map[string]any {
	return defaultModelsFn()
}
