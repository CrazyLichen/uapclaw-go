// 只读模型目录：defaults、AgentOS 与模型组的统一快照及引用扫描。
// 对应 Python 代码：jiuwenswarm/common/model_catalog.py:1-243（完整版基线，裁决 D；
// gateway/common/model_catalog.py 为裁剪副本，仅用于核对 RPC 面引用符号）。
package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

// publicGroupKeys 模型组对外暴露键白名单。
// Python: model_catalog.py:23 _PUBLIC_GROUP_KEYS。
var publicGroupKeys = []string{
	"model_group_id", "display_name", "enabled", "is_default", "routes", "request_config",
}

// publicGroupTimeoutKeys routing 段仅暴露的三个超时键。
// Python: model_catalog.py:31 _PUBLIC_GROUP_TIMEOUT_KEYS。
var publicGroupTimeoutKeys = []string{
	"timeout_seconds", "stream_first_chunk_timeout", "stream_idle_timeout",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SelectionReference 引用位置。
// Python: model_catalog.py:83 SelectionReference（frozen dataclass）。
type SelectionReference struct {
	// Scope 引用范围：model_group / session / cron / agent / team
	Scope string
	// ScopeID 范围内标识
	ScopeID string
}

// ModelCatalogHit 目录命中项（Python 直接返回 {"source", "entry"} dict，Go 用类型化结构承载）。
type ModelCatalogHit struct {
	// Source 来源：defaults / agentos
	Source string
	// Entry 模型条目（含 model_client_config / model_config_obj / model_detail）
	Entry map[string]any
}

// ModelsSnapshot 目录快照（Python load_models_config 返回的 {"defaults","agentos","groups","by_id"} dict）。
type ModelsSnapshot struct {
	// Defaults defaults 模型条目（已解密）
	Defaults []map[string]any
	// Agentos agentos 备份模型条目
	Agentos []map[string]any
	// Groups 模型组条目
	Groups []map[string]any
	// ByID model_id → 命中项索引
	ByID map[string]ModelCatalogHit
}

// ModelCatalog 只读目录。
// Python: model_catalog.py:89 ModelCatalog。
type ModelCatalog struct {
	// Config 原始配置（引用扫描的 agents/team 树来源）
	Config map[string]any
	// Snapshot 加载时的快照（不随配置变更自动刷新，对齐 Python 构造时快照语义）
	Snapshot ModelsSnapshot
}

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadModelsConfig 返回按 ID 索引、已解密的快照，不修改持久化配置。
// Python: jiuwenswarm/common/config.py:1828 load_models_config。
// defaults/agentos 复用既有解密与恢复实现（GetDefaultModels/GetAgentosModels，
// 对齐 _decrypt_model_entries / get_agentos_models）；groups 逐组深拷贝；
// defaults/agentos 条目补 model_detail.fallback_tag = model_name 后进 by_id。
func LoadModelsConfig(cfg map[string]any) ModelsSnapshot {
	configMap := cfg
	if configMap == nil {
		loaded, err := loadDefaultConfigMap()
		if err != nil {
			configMap = map[string]any{}
		} else {
			configMap = loaded
		}
	}
	models, _ := configMap["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}
	// Python: config.py:1832 defaults = _decrypt_model_entries(models.get("defaults") or [])
	// 注意：Go 的 GetDefaultModels 是 Python get_default_models 语义（defaults+agentos 合并），
	// 快照需要分开的两列，故 defaults 列单独解密。
	defaults := extractDefaultsEntries(configMap)
	defaults = config.DecryptModelEntries(defaults)
	// Python: config.py:1833 agentos = get_agentos_models(config)
	agentos := config.GetAgentosModels(configMap)
	// Python: config.py:1834 groups = [deepcopy(g) for g in models.get("groups") or [] if isinstance(g, dict)]
	var groups []map[string]any
	if raw, ok := models["groups"].([]any); ok {
		for _, g := range raw {
			if gm, ok := g.(map[string]any); ok {
				groups = append(groups, deepCopyMap(gm))
			}
		}
	}
	byID := map[string]ModelCatalogHit{}
	for _, pair := range []struct {
		source  string
		entries []map[string]any
	}{{"defaults", defaults}, {"agentos", agentos}} {
		for _, entry := range pair.entries {
			// Python: config.py:1840-1842 fallback_tag 回填 model_client_config.model_name
			detail := map[string]any{}
			if existing, ok := entry["model_detail"].(map[string]any); ok {
				for k, v := range existing {
					detail[k] = v
				}
			}
			mcc, _ := entry["model_client_config"].(map[string]any)
			if mcc == nil {
				mcc = map[string]any{}
			}
			detail["fallback_tag"], _ = mcc["model_name"].(string)
			entry["model_detail"] = detail
			modelID := strings.TrimSpace(asString(entry["model_id"]))
			if modelID != "" {
				byID[modelID] = ModelCatalogHit{Source: pair.source, Entry: entry}
			}
		}
	}
	return ModelsSnapshot{Defaults: defaults, Agentos: agentos, Groups: groups, ByID: byID}
}

// NewModelCatalog 构造目录并构建快照。
// Python: model_catalog.py:90 __init__（config None 时 get_config()）。
func NewModelCatalog(cfg map[string]any) *ModelCatalog {
	configMap := cfg
	if configMap == nil {
		if loaded, err := loadDefaultConfigMap(); err == nil {
			configMap = loaded
		} else {
			configMap = map[string]any{}
		}
	}
	return &ModelCatalog{Config: configMap, Snapshot: LoadModelsConfig(configMap)}
}

// GetModel 按 model_id 取命中项。
// Python: model_catalog.py:94 get_model。
func (c *ModelCatalog) GetModel(modelID string) (ModelCatalogHit, error) {
	hit, ok := c.Snapshot.ByID[modelID]
	if !ok {
		// Python: model_catalog.py:97 f"unknown model_id {model_id!r}"
		return ModelCatalogHit{}, NewModelSelectionError(ModelSelectionNotFound, fmt.Sprintf("unknown model_id %q", modelID), nil)
	}
	return hit, nil
}

// GetGroup 按 model_group_id 取组。
// Python: model_catalog.py:100 get_group。
func (c *ModelCatalog) GetGroup(groupID string) (map[string]any, error) {
	for _, group := range c.Snapshot.Groups {
		if asString(group["model_group_id"]) == groupID {
			return group, nil
		}
	}
	return nil, NewModelSelectionError(ModelSelectionNotFound, fmt.Sprintf("unknown model_group_id %q", groupID), nil)
}

// GroupDisplayName 组显示名，回落 id。
// Python: model_catalog.py:101-110 group_display_name（完整版新增）。
// 各命名模型组的表面（runtime 状态文件、团队池 metadata、runtime 提示词）的
// 唯一命名来源：回落 id 保证目录未命中不让调用方失败。
func (c *ModelCatalog) GroupDisplayName(groupID string) string {
	group, err := c.GetGroup(groupID)
	if err != nil {
		// Python get_group 在此会抛；本方法语义为"目录未命中不让调用方失败"，
		// 对齐完整版实现：get_group 抛出即向上传播——此处保持一致返回空回落 id。
		return groupID
	}
	name := strings.TrimSpace(asString(group["display_name"]))
	if name == "" {
		return groupID
	}
	return name
}

// HasSelection 保存的引用是否仍指向存在的模型/模型组/路由。
// Python: model_catalog.py:106 has_selection。
func (c *ModelCatalog) HasSelection(selection *ModelSelection) bool {
	if selection.Type == "model" {
		_, ok := c.Snapshot.ByID[selection.ID]
		return ok
	}
	for _, group := range c.Snapshot.Groups {
		if asString(group["model_group_id"]) != selection.ID {
			continue
		}
		// Python: model_catalog.py:113-116 route_id 为 None 即视为存在，否则须有匹配路由
		if selection.RouteID == "" {
			return true
		}
		routes, _ := group["routes"].([]any)
		for _, r := range routes {
			if rm, ok := r.(map[string]any); ok && asString(rm["route_id"]) == selection.RouteID {
				return true
			}
		}
		return false
	}
	return false
}

// SafeModel 条目的对外安全投影。
// Python: model_catalog.py:120 _safe_model（静态方法）。
func SafeModel(entry map[string]any, source string) map[string]any {
	mcc, _ := entry["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	mco, _ := entry["model_config_obj"].(map[string]any)
	if mco == nil {
		mco = map[string]any{}
	}
	modelName, _ := mcc["model_name"].(string)
	provider, _ := mcc["client_provider"].(string)
	alias, _ := entry["alias"].(string)
	return map[string]any{
		"model_id":       entry["model_id"],
		"alias":          alias,
		"model_name":     modelName,
		"provider":       provider,
		"source":         source,
		"is_agentos":     source == "agentos",
		"is_default":     entry["is_default"] == true,
		"enabled":        modelName != "",
		"context_window": mco["context_window"],
	}
}

// ListPublicModels 双源公开模型列表。
// Python: model_catalog.py:130 list_public_models。
func (c *ModelCatalog) ListPublicModels() []map[string]any {
	result := []map[string]any{}
	for _, pair := range []struct {
		source  string
		entries []map[string]any
	}{{"defaults", c.Snapshot.Defaults}, {"agentos", c.Snapshot.Agentos}} {
		for _, entry := range pair.entries {
			if entry["model_id"] != nil && asString(entry["model_id"]) != "" {
				result = append(result, SafeModel(entry, pair.source))
			}
		}
	}
	return result
}

// GetPublicModelDetail 模型详情，可选含用户配置的 API Key。
// Python: model_catalog.py:138 get_public_model_detail。
// 更新是合并式：省略的 write-only 字段保持不变；AgentOS 模型有意暴露为只读目录条目。
func (c *ModelCatalog) GetPublicModelDetail(modelID string, includeAPIKey bool) (map[string]any, error) {
	hit, err := c.GetModel(modelID)
	if err != nil {
		return nil, err
	}
	entry := deepCopyMap(hit.Entry)
	// Python: model_catalog.py:149-151 reasoning_level bool 归一为 "on"/"off"
	if mco, ok := entry["model_config_obj"].(map[string]any); ok {
		if rl, isBool := mco["reasoning_level"].(bool); isBool {
			if rl {
				mco["reasoning_level"] = "on"
			} else {
				mco["reasoning_level"] = "off"
			}
		}
	}
	// Python: model_catalog.py:152 is_login_model 接缝——偏差 E-4：Go 侧登录模型
	// 判定未落地（⤵️ 登录/权限章节），接缝处恒 false，注释记录。
	isLoginModel := false
	exposeAPIKey := includeAPIKey && hit.Source == "defaults" && !isLoginModel
	client, _ := entry["model_client_config"].(map[string]any)
	if client == nil {
		client = map[string]any{}
	}
	// Python: model_catalog.py:154-158 编辑对话框对空 YAML 模型也需要这些字符串；
	// 只归一响应，保留持久化原值。
	client["model_name"] = asString(client["model_name"])
	client["api_base"] = asString(client["api_base"])
	entry["model_client_config"] = client
	writeOnlyFields := []string{}
	// Python: model_catalog.py:160-166 api_key / custom_headers 不外发时剔除并记录
	if _, exists := client["api_key"]; exists && !exposeAPIKey {
		delete(client, "api_key")
		writeOnlyFields = append(writeOnlyFields, "model_client_config.api_key")
	}
	if _, exists := client["custom_headers"]; exists {
		delete(client, "custom_headers")
		writeOnlyFields = append(writeOnlyFields, "model_client_config.custom_headers")
	}
	entry["source"] = hit.Source
	entry["is_agentos"] = hit.Source == "agentos"
	entry["read_only"] = hit.Source == "agentos"
	entry["write_only_fields"] = writeOnlyFields
	return entry, nil
}

// ListPublicGroups 公开模型组列表（白名单键 + routing 三超时键）。
// Python: model_catalog.py:173 list_public_groups。
func (c *ModelCatalog) ListPublicGroups() []map[string]any {
	result := []map[string]any{}
	for _, group := range c.Snapshot.Groups {
		public := map[string]any{}
		for _, key := range publicGroupKeys {
			public[key] = deepCopyAny(group[key])
		}
		if routing, ok := group["routing"].(map[string]any); ok {
			timeouts := map[string]any{}
			for _, key := range publicGroupTimeoutKeys {
				if v, exists := routing[key]; exists {
					timeouts[key] = deepCopyAny(v)
				}
			}
			if len(timeouts) > 0 {
				public["routing"] = timeouts
			}
		}
		result = append(result, public)
	}
	return result
}

// FindReferences 找出引用该选择的全部位置。
// Python: model_catalog.py:194 find_references(selection, include_cron=True)。
// 扫描面：模型组 routes → session metadata.json → cron jobs（⤵️ 裁决 E）→ agents/team 配置树。
// 任何扫描失败 = RUNTIME_UNAVAILABLE（不完整扫描无法证明资源可删，防误删语义）。
func (c *ModelCatalog) FindReferences(selection *ModelSelection, includeCron bool) ([]SelectionReference, error) {
	refs := []SelectionReference{}
	// Python: model_catalog.py:196-200 模型组 routes 引用
	if selection.Type == "model" {
		for _, group := range c.Snapshot.Groups {
			routes, _ := group["routes"].([]any)
			for _, r := range routes {
				if rm, ok := r.(map[string]any); ok && asString(rm["model_id"]) == selection.ID {
					refs = append(refs, SelectionReference{Scope: "model_group", ScopeID: asString(group["model_group_id"])})
					break
				}
			}
		}
	}
	// Python: model_catalog.py:201-218 session metadata 扫描
	if err := c.scanSessionReferences(selection, &refs); err != nil {
		return nil, err
	}
	// Python: model_catalog.py:219-233 cron jobs 扫描。
	// 偏差 E-3（裁决 E）：Go 侧 cron 任务存储未落地（⤵️ cron 章节），本扫描点
	// 留空实现——语义为"无可引用 cron 数据"，与上游 cron 数据恒为空等价；
	// cron 章节落地时按 Python :219-233 补真实扫描与 RUNTIME_UNAVAILABLE 传播。
	// includeCron 参数保留（Python :194 形参）。
	_ = includeCron
	// Python: model_catalog.py:234-235 agents/team 配置树递归
	refs = append(refs, findEmbeddedSelections(c.Config["agents"], selection, "agent", nil, "")...)
	refs = append(refs, findEmbeddedSelections(c.Config["team"], selection, "team", nil, "")...)
	// Python: model_catalog.py:236-238 畸形树可能经别名含同一选择；去重保持结果确定性
	return dedupeReferences(refs), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractDefaultsEntries 提取 models.defaults 原始条目（仅提取，不解密）。
// Python: config.py:1832 models.get("defaults") or []。
func extractDefaultsEntries(configMap map[string]any) []map[string]any {
	models, _ := configMap["models"].(map[string]any)
	if models == nil {
		return nil
	}
	raw, _ := models["defaults"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// loadDefaultConfigMap 加载默认 config.yaml。
// Python: jiuwenswarm/common/config.py:1829 get_config()（config=None 分支）。
// 复用既有 config.New("")+Load() 公共 API。
func loadDefaultConfigMap() (map[string]any, error) {
	c, err := config.New("")
	if err != nil {
		return nil, err
	}
	return c.Load()
}

// scanSessionReferences 扫描会话目录的 metadata.json 中的 model_selection。
// Python: model_catalog.py:36 _read_session_selection + :201-218 扫描循环。
func (c *ModelCatalog) scanSessionReferences(selection *ModelSelection, refs *[]SelectionReference) error {
	root := workspace.AgentSessionsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			// Python: model_catalog.py:203 root.exists() 为假直接跳过
			return nil
		}
		return wrapRuntimeUnavailable("session", err)
	}
	for _, dirEntry := range entries {
		if !dirEntry.IsDir() {
			continue
		}
		path := filepath.Join(root, dirEntry.Name(), "metadata.json")
		// Python: model_catalog.py:206 非常规文件跳过
		if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
			continue
		}
		value, readErr := readSessionSelection(path)
		if readErr != nil {
			return wrapRuntimeUnavailable("session", readErr)
		}
		if matchesSelection(selection, value) {
			*refs = append(*refs, SelectionReference{Scope: "session", ScopeID: dirEntry.Name()})
		}
	}
	return nil
}

// readSessionSelection 读 metadata.json 的 model_selection 字段。
// Python: model_catalog.py:36-45 _read_session_selection。
func readSessionSelection(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		// Python: model_catalog.py:40 logger.warning(..., exc_info=True) 后 raise
		return nil, err
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		// Python: model_catalog.py:41-43 JSON 非对象抛 ValueError
		return nil, fmt.Errorf("session metadata must be an object: %w", err)
	}
	value, _ := metadata["model_selection"].(map[string]any)
	return value, nil
}

// matchesSelection 值是否与选择匹配（type/id 相同）。
// Python: model_catalog.py:48 _matches。
func matchesSelection(selection *ModelSelection, value any) bool {
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return asString(m["type"]) == selection.Type && asString(m["id"]) == selection.ID
}

// findEmbeddedSelections 在 Agent/Team 配置树中递归找稳定引用。
// Python: model_catalog.py:56-80 _find_embedded_selections。
// path 记录访问路径、owner_id 继承最近一层有 id/name 的节点，语义照抄。
func findEmbeddedSelections(value any, selection *ModelSelection, scope string, path []string, ownerID string) []SelectionReference {
	refs := []SelectionReference{}
	switch v := value.(type) {
	case map[string]any:
		// Python: model_catalog.py:67 current_owner = str(value.get("id") or value.get("name") or owner_id)
		currentOwner := ownerID
		if id := asString(v["id"]); id != "" {
			currentOwner = id
		} else if name := asString(v["name"]); name != "" {
			currentOwner = name
		}
		if matchesSelection(selection, v["model_selection"]) {
			scopeID := currentOwner
			if scopeID == "" {
				scopeID = strings.Join(path, ".")
			}
			if scopeID == "" {
				scopeID = scope
			}
			refs = append(refs, SelectionReference{Scope: scope, ScopeID: scopeID})
		}
		for key, child := range v {
			childPath := append(append([]string{}, path...), key)
			refs = append(refs, findEmbeddedSelections(child, selection, scope, childPath, currentOwner)...)
		}
	case []any:
		for index, child := range v {
			childPath := append(append([]string{}, path...), fmt.Sprintf("%d", index))
			refs = append(refs, findEmbeddedSelections(child, selection, scope, childPath, ownerID)...)
		}
	}
	return refs
}

// dedupeReferences 去重（Python: model_catalog.py:238 dict.fromkeys 保序去重）。
func dedupeReferences(refs []SelectionReference) []SelectionReference {
	seen := map[SelectionReference]bool{}
	out := make([]SelectionReference, 0, len(refs))
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// wrapRuntimeUnavailable 扫描失败包装为 RUNTIME_UNAVAILABLE。
// Python: model_catalog.py:212-218 / :228-233（文案分 session/cron 两种，cron 侧随 ⤵️ 回填）。
func wrapRuntimeUnavailable(scope string, err error) *ModelSelectionError {
	if scope == "session" {
		return NewModelSelectionError(ModelRuntimeUnavailable,
			"Cannot check model references: session metadata is unreadable or invalid; repair it and retry",
			map[string]any{"cause": err.Error()})
	}
	return NewModelSelectionError(ModelRuntimeUnavailable,
		"Cannot check model references: cron data is unreadable or invalid; repair it and retry",
		map[string]any{"cause": err.Error()})
}

// deepCopyMap 深拷贝 map（Python deepcopy）。
func deepCopyMap(m map[string]any) map[string]any {
	return deepCopyAny(m).(map[string]any)
}

// deepCopyAny 深拷贝任意 JSON 兼容值（Python deepcopy 语义，经 JSON 往返）。
func deepCopyAny(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}
