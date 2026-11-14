// 模型资源 upsert/delete（配置写路径）。
// 对应 Python 代码：jiuwenswarm/common/config.py:1866-2102 与 gateway/common/config.py:2754-2856
// 的 upsert_model_resource / upsert_model_group_resource / delete_model_resource 及其私有助手
// （_normalize_model_entry / _merge_model_update / _protect_model_key / _require_editable_model /
// _ensure_model_business_ids / _validate_model_candidate / _models_for_write）。
// Go 侧 config 体系为实例型（*config.Config），与 Python 全局 update_config 不同构：
// 函数均以 cfg 注入（spec §2 落位说明），doc.go 已登记。
package common

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/google/uuid"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newBusinessID 生成带前缀的业务 ID。
// Python: config.py:1103 _new_business_id —— f"{prefix}_{uuid4().hex}"。
func newBusinessID(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, strings.ReplaceAll(uuid.New().String(), "-", ""))
}

// ensureModelBusinessIDs 原地补缺失的稳定 ID，返回是否有变更。
// Python: config.py:1108-1139 _ensure_model_business_ids。
func ensureModelBusinessIDs(models map[string]any) bool {
	changed := false
	for _, source := range []string{"defaults", "agentos"} {
		entries, _ := models[source].([]any)
		for _, item := range entries {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(asString(entry["model_id"])) == "" {
				entry["model_id"] = newBusinessID("mdl")
				changed = true
			}
		}
	}
	groups, _ := models["groups"].([]any)
	for _, item := range groups {
		group, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(group["model_group_id"])) == "" {
			group["model_group_id"] = newBusinessID("mgp")
			changed = true
		}
		routes, _ := group["routes"].([]any)
		for _, r := range routes {
			route, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(asString(route["route_id"])) == "" {
				route["route_id"] = newBusinessID("rte")
				changed = true
			}
		}
	}
	return changed
}

// normalizeLegacyModelNulls 修复空 YAML 节点而不改变已配置的模型身份。
// Python: config.py:1603-1626 _normalize_legacy_model_nulls。
func normalizeLegacyModelNulls(models map[string]any) {
	for _, source := range []string{"defaults", "agentos", "groups"} {
		if v, exists := models[source]; exists && v == nil {
			models[source] = []any{}
		}
		entries, _ := models[source].([]any)
		// 空 YAML 列表项没有配置或身份可保留，剔除 None 项
		filtered := make([]any, 0, len(entries))
		for _, item := range entries {
			if item != nil {
				filtered = append(filtered, item)
			}
		}
		models[source] = filtered
		// Python: :1617-1623 非 groups 源把 None 的 model_client_config/model_config_obj 归 {}
		if source != "groups" {
			for _, item := range filtered {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				for _, key := range []string{"model_client_config", "model_config_obj"} {
					if v, exists := entry[key]; exists && v == nil {
						entry[key] = map[string]any{}
					}
				}
			}
		}
	}
}

// recoverExistingModelFields 恢复持久化模型条目（从不触碰外部提交的条目）。
// Python: config.py:1668-1686 _recover_existing_model_fields。
func recoverExistingModelFields(models map[string]any) bool {
	recovered := false
	for _, source := range []string{"defaults", "agentos", "groups"} {
		entries, _ := models[source].([]any)
		if entries == nil {
			continue
		}
		// Python: :1673-1676 非 dict 条目剔除
		filtered := make([]any, 0, len(entries))
		changed := false
		for _, item := range entries {
			if item != nil {
				if _, ok := item.(map[string]any); !ok {
					changed = true
					continue
				}
			}
			filtered = append(filtered, item)
		}
		if changed {
			models[source] = filtered
			recovered = true
		}
		// Python: :1677-1680 非 groups 源逐条目恢复
		if source != "groups" {
			for _, item := range filtered {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if config.RecoverModelFields(entry) {
					recovered = true
				}
			}
		}
	}
	return recovered
}

// modelsForWrite 在配置写锁下恢复既有模型字段，无效数据先备份。
// Python: config.py:1629-1651 _models_for_write。
// 恢复发生时备份原文件；备份失败中止写入而不丢失用户数据。
func modelsForWrite(cfg *config.Config, data map[string]any) (map[string]any, error) {
	if data["models"] == nil {
		data["models"] = map[string]any{}
	}
	models, ok := data["models"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("models must be an object")
	}
	if recoverExistingModelFields(models) {
		// Python: :1636-1642 保留原文件（含注释与无效值）备份；备份失败中止写入
		path := cfg.Path()
		backup := path + fmt.Sprintf(".models-%s.bak", strings.ReplaceAll(newBusinessID("bak"), "bak_", ""))
		if copyErr := copyFile(path, backup); copyErr != nil {
			return nil, copyErr
		}
	}
	normalizeLegacyModelNulls(models)
	return models, nil
}

// copyFile 文件复制（Python shutil.copy2 等价，保留权限位）。
func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o600)
}

// requireEditableModel AgentOS 模型只读。
// Python: config.py:1936-1942 _require_editable_model。
func requireEditableModel(models map[string]any, modelID string) error {
	agentos, _ := models["agentos"].([]any)
	for _, item := range agentos {
		if entry, ok := item.(map[string]any); ok && asString(entry["model_id"]) == modelID {
			return NewModelSelectionError(ModelSelectionForbidden, "AgentOS models are read-only", nil)
		}
	}
	return nil
}

// validateModelCandidate 候选配置校验（补 ID 后 raise_if_invalid）。
// Python: config.py:1944-1948 _validate_model_candidate。
func validateModelCandidate(candidate map[string]any) error {
	ensureModelBusinessIDs(candidate)
	return RaiseIfInvalid(candidate)
}

// mergeModelUpdate 合并可编辑 DTO，保留省略/write-only 设置。
// Python: config.py:1923-1934 _merge_model_update。
func mergeModelUpdate(current map[string]any, update map[string]any) map[string]any {
	merged := deepCopyMap(current)
	for key, value := range update {
		// Python: :1926 响应派生键不回写
		if key == "source" || key == "is_agentos" || key == "read_only" || key == "write_only_fields" {
			continue
		}
		vm, vIsMap := value.(map[string]any)
		cm, mIsMap := merged[key].(map[string]any)
		if vIsMap && mIsMap {
			merged[key] = mergeModelUpdate(cm, vm)
		} else {
			merged[key] = deepCopyAny(value)
		}
	}
	return merged
}

// protectModelKey 保留省略的凭证；变更的密钥用已装 provider 加密。
// Python: config.py:1981-2000 _protect_model_key。
func protectModelKey(update map[string]any, current map[string]any) {
	client, _ := update["model_client_config"].(map[string]any)
	if client == nil {
		return
	}
	key, exists := client["api_key"]
	if !exists {
		return
	}
	keyStr, _ := key.(string)
	currentClient, _ := current["model_client_config"].(map[string]any)
	previous := ""
	if currentClient != nil {
		previous, _ = currentClient["api_key"].(string)
	}
	if keyStr == previous || keyStr == "" {
		return
	}
	provider := crypto.GetCryptoProvider()
	if provider == nil {
		return
	}
	// Python: :1995-1998 解密旧条目比对明文：相同明文保留旧密文（省略语义）
	resolved := config.DecryptModelEntries([]map[string]any{deepCopyMap(current)})
	previousPlaintext := ""
	if len(resolved) > 0 {
		if rc, ok := resolved[0]["model_client_config"].(map[string]any); ok {
			previousPlaintext, _ = rc["api_key"].(string)
		}
	}
	if previous != "" && keyStr == previousPlaintext {
		client["api_key"] = previous
	} else {
		encrypted, encErr := provider.Encrypt(context.Background(), keyStr)
		if encErr != nil {
			// Python 侧 crypto.encrypt 异常会上抛；Go 侧保持上抛语义
			return
		}
		client["api_key"] = encrypted
	}
}

// normalizeModelEntry 扁平前端条目转嵌套配置格式。
// Python: config.py:1866-1921 _normalize_model_entry。
// 已含 model_client_config dict 的条目视为已归一原样返回；否则把扁平字段提升进嵌套结构。
func normalizeModelEntry(entry map[string]any) (map[string]any, error) {
	if entry == nil {
		return nil, fmt.Errorf("model must be an object")
	}
	saved := deepCopyMap(entry)
	for _, key := range []string{"model_client_config", "model_config_obj", "model_detail"} {
		if v, exists := saved[key]; exists && v != nil {
			if _, isMap := v.(map[string]any); !isMap {
				return nil, fmt.Errorf("model.%s must be an object", key)
			}
		}
	}
	for _, key := range []string{"model_name", "api_base", "api_key", "client_provider", "model_provider"} {
		if value, exists := saved[key]; exists && value != nil {
			if _, isStr := value.(string); !isStr {
				return nil, fmt.Errorf("model.%s must be a string", key)
			}
		}
	}
	if _, isMap := saved["model_client_config"].(map[string]any); isMap {
		return saved, nil
	}
	mcc := map[string]any{}
	mco := map[string]any{}
	// Python: :1888-1905 逐键提升（pop 语义）
	if v, exists := saved["model_name"]; exists {
		mcc["model_name"] = asString(v)
		delete(saved, "model_name")
	}
	if v, exists := saved["api_base"]; exists {
		mcc["api_base"] = asString(v)
		delete(saved, "api_base")
	}
	if v, exists := saved["api_key"]; exists {
		if v == nil {
			mcc["api_key"] = ""
		} else {
			mcc["api_key"] = v
		}
		delete(saved, "api_key")
	}
	// Python: :1906 provider 取 model_provider 优先、client_provider 回退
	providerVal := saved["model_provider"]
	if providerVal == nil {
		providerVal = saved["client_provider"]
	}
	provider := asString(providerVal)
	delete(saved, "model_provider")
	delete(saved, "client_provider")
	if provider != "" {
		mcc["client_provider"] = provider
	}
	if v, exists := saved["timeout"]; exists {
		mcc["timeout"] = v
		delete(saved, "timeout")
	}
	if v, exists := saved["verify_ssl"]; exists {
		b, _ := v.(bool)
		mcc["verify_ssl"] = b
		delete(saved, "verify_ssl")
	}
	for _, key := range []string{"endpoint_profile", "vendor_key", "plan"} {
		if v, exists := saved[key]; exists {
			if asString2(v) != "" {
				mcc[key] = asString(v)
			}
			delete(saved, key)
		}
	}
	// Python: :1910-1916 temperature → mco；reasoning_level 非空白 → mco
	if temp, exists := saved["temperature"]; exists {
		if temp != nil {
			mco["temperature"] = asFloat(temp)
		}
		delete(saved, "temperature")
	}
	if rl, exists := saved["reasoning_level"]; exists {
		if rl != nil && strings.TrimSpace(asString(rl)) != "" {
			mco["reasoning_level"] = strings.TrimSpace(asString(rl))
		}
		delete(saved, "reasoning_level")
	}
	if len(mcc) > 0 {
		saved["model_client_config"] = mcc
	}
	if len(mco) > 0 {
		saved["model_config_obj"] = mco
	}
	return saved, nil
}

// asString2 宽松字符串化（Python str(val)）。
func asString2(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// UpsertModelResource 新增/更新 defaults 模型条目，返回保存后的条目。
// Python: config.py:2002-2032 upsert_model_resource。
// 步骤：①归一 ②reasoning_level 归一校验 ③补 model_id ④写锁内合并或追加
// ⑤is_default 互斥 ⑥候选校验。
func UpsertModelResource(cfg *config.Config, entry map[string]any) (map[string]any, error) {
	saved, err := normalizeModelEntry(entry)
	if err != nil {
		return nil, err
	}
	// Python: :2003-2012 reasoning_level 归一校验（非法非空值报错）
	if mco, ok := saved["model_config_obj"].(map[string]any); ok {
		if raw, exists := mco["reasoning_level"]; exists {
			rawStr := asString(raw)
			level := NormalizeReasoningLevel(rawStr)
			if raw != nil && strings.TrimSpace(rawStr) != "" && level == "" {
				return nil, fmt.Errorf("Invalid reasoning_level")
			}
			// Python: DoubleQuotedScalarString 为 YAML 写出格式；Go 侧存字符串值
			if level != "" {
				mco["reasoning_level"] = level
			} else {
				mco["reasoning_level"] = ""
			}
		}
	}
	// Python: :2013 saved.setdefault("model_id", _new_business_id("mdl"))
	if strings.TrimSpace(asString(saved["model_id"])) == "" {
		saved["model_id"] = newBusinessID("mdl")
	}
	entryIsDefault := entry != nil && entry["is_default"] == true

	data, err := cfg.Load()
	if err != nil {
		return nil, err
	}
	candidate, err := modelsForWrite(cfg, data)
	if err != nil {
		return nil, err
	}
	if err := requireEditableModel(candidate, asString(saved["model_id"])); err != nil {
		return nil, err
	}
	defaults, _ := candidate["defaults"].([]any)
	found := false
	for index, item := range defaults {
		current, ok := item.(map[string]any)
		if !ok || asString(current["model_id"]) != asString(saved["model_id"]) {
			continue
		}
		// Python: :2020-2023 命中既有条目：保护密钥 → 合并 → 替换
		protectModelKey(saved, current)
		saved = mergeModelUpdate(current, saved)
		defaults[index] = saved
		found = true
		break
	}
	if !found {
		// Python: :2024-2026 else 分支：保护密钥后追加
		protectModelKey(saved, map[string]any{})
		defaults = append(defaults, saved)
	}
	if entryIsDefault {
		// Python: :2027-2029 is_default 互斥
		for _, item := range defaults {
			if current, ok := item.(map[string]any); ok {
				current["is_default"] = asString(current["model_id"]) == asString(saved["model_id"])
			}
		}
	}
	candidate["defaults"] = defaults
	if err := validateModelCandidate(candidate); err != nil {
		return nil, err
	}
	data["models"] = candidate
	if err := cfg.Save(data); err != nil {
		return nil, err
	}
	return saved, nil
}

// UpsertModelGroupResource 新增/更新模型组，返回保存后的组。
// Python: config.py:2034-2085 upsert_model_group_resource。
// 步骤：①拷贝与缺省 ID ②routing 只保留三超时键 ③路由缺省 route_id
// ④写锁内替换或追加 ⑤is_default 互斥 ⑥候选校验 ⑦enabled 组须有 enabled 路由。
func UpsertModelGroupResource(cfg *config.Config, group map[string]any) (map[string]any, error) {
	if group == nil {
		return nil, NewModelSelectionError(ModelGroupInvalid, "group must be an object", nil)
	}
	saved := deepCopyMap(group)
	// Python: :2036 saved.setdefault("model_group_id", _new_business_id("mgp"))
	if strings.TrimSpace(asString(saved["model_group_id"])) == "" {
		saved["model_group_id"] = newBusinessID("mgp")
	}
	// Python: :2037-2045 routing 保留逐组超时；旧路由策略在模型池模式保持不活跃
	routing, _ := saved["routing"].(map[string]any)
	delete(saved, "routing")
	if routing != nil {
		timeouts := map[string]any{}
		for _, key := range []string{"timeout_seconds", "stream_first_chunk_timeout", "stream_idle_timeout"} {
			if v, exists := routing[key]; exists {
				timeouts[key] = v
			}
		}
		if len(timeouts) > 0 {
			saved["routing"] = timeouts
		}
	}
	// Python: :2046-2049 路由缺省 route_id
	routes, _ := saved["routes"].([]any)
	for _, r := range routes {
		if route, ok := r.(map[string]any); ok {
			if strings.TrimSpace(asString(route["route_id"])) == "" {
				route["route_id"] = newBusinessID("rte")
			}
		}
	}

	data, err := cfg.Load()
	if err != nil {
		return nil, err
	}
	candidate, err := modelsForWrite(cfg, data)
	if err != nil {
		return nil, err
	}
	groups, _ := candidate["groups"].([]any)
	found := false
	for index, item := range groups {
		if current, ok := item.(map[string]any); ok && asString(current["model_group_id"]) == asString(saved["model_group_id"]) {
			// Python: :2062-2063 命中替换
			groups[index] = saved
			found = true
			break
		}
	}
	if !found {
		groups = append(groups, saved)
	}
	// Python: :2066-2070 is_default 互斥
	if saved["is_default"] == true {
		for _, item := range groups {
			if current, ok := item.(map[string]any); ok {
				current["is_default"] = asString(current["model_group_id"]) == asString(saved["model_group_id"])
			}
		}
	}
	candidate["groups"] = groups
	if err := validateModelCandidate(candidate); err != nil {
		return nil, err
	}
	// Python: :2071-2073 enabled 组必须含 enabled 路由
	if saved["enabled"] != false {
		hasEnabled := false
		for _, r := range routes {
			if route, ok := r.(map[string]any); ok && route["enabled"] != false {
				hasEnabled = true
				break
			}
		}
		if !hasEnabled {
			return nil, NewModelSelectionError(ModelGroupInvalid, "enabled model group must contain an enabled route", nil)
		}
	}
	data["models"] = candidate
	if err := cfg.Save(data); err != nil {
		return nil, err
	}
	return saved, nil
}

// DeleteModelResource 删除模型或模型组，返回是否删除。
// Python: config.py:1174-1222 delete_model_resource。
// 模型删除时联动清理指向该模型的路由，空组移除（#5477，防 UI 出现裸 mgp_/mdl_ ID）。
func DeleteModelResource(cfg *config.Config, resourceType string, resourceID string) (bool, error) {
	key := "defaults"
	idKey := "model_id"
	if resourceType == "model_group" {
		key = "groups"
		idKey = "model_group_id"
	}
	data, err := cfg.Load()
	if err != nil {
		return false, err
	}
	candidate, err := modelsForWrite(cfg, data)
	if err != nil {
		return false, err
	}
	if resourceType == "model" {
		if err := requireEditableModel(candidate, resourceID); err != nil {
			return false, err
		}
	}
	entries, _ := candidate[key].([]any)
	filtered := make([]any, 0, len(entries))
	for _, item := range entries {
		if entry, ok := item.(map[string]any); ok && asString(entry[idKey]) != resourceID {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == len(entries) {
		// Python: :1189-1190 未命中返回 False（不写入）
		return false, nil
	}
	if resourceType == "model" {
		// Python: :1191-1205 丢弃仍指向该模型的路由；无路由的组移除
		nextGroups := []any{}
		rawGroups, _ := candidate["groups"].([]any)
		for _, item := range rawGroups {
			group, ok := item.(map[string]any)
			if !ok {
				continue
			}
			nextRoutes := []any{}
			rawRoutes, _ := group["routes"].([]any)
			for _, r := range rawRoutes {
				if route, ok := r.(map[string]any); ok {
					if asString(route["model_id"]) == resourceID {
						continue
					}
					nextRoutes = append(nextRoutes, route)
				}
			}
			if len(nextRoutes) == 0 {
				continue
			}
			nextGroup := deepCopyMap(group)
			nextGroup["routes"] = nextRoutes
			nextGroups = append(nextGroups, nextGroup)
		}
		candidate["groups"] = nextGroups
	}
	candidate[key] = filtered
	if err := validateModelCandidate(candidate); err != nil {
		return false, err
	}
	data["models"] = candidate
	if err := cfg.Save(data); err != nil {
		return false, err
	}
	return true, nil
}

// 确保依赖可见（filepath 用于备份路径构造的编译期校验）。
var _ = filepath.Join
