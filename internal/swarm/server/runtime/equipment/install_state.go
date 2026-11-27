package equipment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HubInstallStateRecord marketplace.json plugins 数组的单条记录。
// Python: marketplace/hub_install_state.py 本地持久化条目 + extension_package_manager
// _upsert_marketplace_entry 的 record 形状（{"id", ...fields}）。
type HubInstallStateRecord struct {
	// ID 记录主键：本地包为包目录名；Hub 包为 asset_id（D-1：Hub 半区不移植）
	ID string `json:"id"`
	// PackageID 本地运行时包目录名（Hub 包记录经 get_by_package_id 反查）
	PackageID string `json:"package_id,omitempty"`
	// AssetID Hub 资产 UUID（本地包缺省）
	AssetID string `json:"asset_id,omitempty"`
	// Kind 资产种类（hub 记录携带；本地 upsert 记录不带）
	Kind string `json:"kind,omitempty"`
	// Installed 是否已安装
	Installed bool `json:"installed"`
	// Source 来源："local" | "hub"
	Source string `json:"source,omitempty"`
	// Version Hub 包版本（本地缺省）
	Version string `json:"version,omitempty"`
	// UpdatedAtNs 分组卡片排序键（agent_groups upsert 写入）
	UpdatedAtNs int64 `json:"updated_at_ns,omitempty"`
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readMarketplaceEntries 读 marketplace.json 的 plugins 数组；缺失/损坏返回空表。
// Python: _read_marketplace_entries。
func readMarketplaceEntries(marketplacePath string) []map[string]any {
	data, err := os.ReadFile(marketplacePath)
	if err != nil {
		return []map[string]any{}
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return []map[string]any{}
	}
	items, _ := doc["plugins"].([]any)
	out := []map[string]any{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// writeMarketplaceEntries 原子写回（对齐 _write_json 语义：mkdir + 临时写 + rename）。
func writeMarketplaceEntries(marketplacePath string, entries []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(marketplacePath), 0o755); err != nil {
		return err
	}
	payload := map[string]any{"plugins": entries}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := marketplacePath + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, marketplacePath)
}

// upsertMarketplaceEntry 更新/追加一条记录并剥离目录缓存遗留键。
// Python: _upsert_marketplace_entry——合并既有字段后丢弃
// name/description/version/enabled 等目录缓存残留。
func upsertMarketplaceEntry(kind, packageID string, fields map[string]any) error {
	marketplacePath := equipmentMarketplacePath(kind, "")
	entries := readMarketplaceEntries(marketplacePath)
	record := map[string]any{"id": packageID}
	for k, v := range fields {
		record[k] = v
	}
	for i, entry := range entries {
		if id, _ := entry["id"].(string); id == packageID {
			merged := map[string]any{}
			for k, v := range entry {
				merged[k] = v
			}
			for k, v := range record {
				merged[k] = v
			}
			entries[i] = merged
			record = merged
			break
		}
	}
	// 剥离目录缓存遗留键（对齐 _upsert_marketplace_entry 的注释语义）
	for k := range record {
		switch k {
		case "name", "description", "version", "enabled":
			delete(record, k)
		}
	}
	// 覆盖或追加
	replaced := false
	for i, entry := range entries {
		if id, _ := entry["id"].(string); id == packageID {
			entries[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, record)
	}
	return writeMarketplaceEntries(marketplacePath, entries)
}

// removeMarketplaceEntry 删除一条记录；无变化不写盘。
// Python: _remove_marketplace_entry。
func removeMarketplaceEntry(kind, packageID string) error {
	marketplacePath := equipmentMarketplacePath(kind, "")
	entries := readMarketplaceEntries(marketplacePath)
	kept := entries[:0:0]
	for _, entry := range entries {
		if id, _ := entry["id"].(string); id != packageID {
			kept = append(kept, entry)
		}
	}
	if len(kept) != len(entries) {
		return writeMarketplaceEntries(marketplacePath, kept)
	}
	return nil
}

// marketplaceIndex 按 id 建索引。Python: _marketplace_index (:395-402)。
func marketplaceIndex(entries []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, entry := range entries {
		if id, ok := entry["id"].(string); ok && id != "" {
			out[id] = entry
		}
	}
	return out
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadAgentTemplateMarketplaceEntries 读模板安装状态表。
// Python: read_agent_template_marketplace_entries (:1142-1144)。
func ReadAgentTemplateMarketplaceEntries() []map[string]any {
	return readMarketplaceEntries(equipmentMarketplacePath(agentTemplateKind, ""))
}

// ReadAgentGroupMarketplaceEntries 读分组安装状态表。
// Python: read_agent_group_marketplace_entries (:1150-1152)。
func ReadAgentGroupMarketplaceEntries() []map[string]any {
	return readMarketplaceEntries(equipmentMarketplacePath(agentGroupKind, ""))
}

// UpsertAgentTemplateMarketplaceEntry 更新模板安装状态。
// Python: upsert_agent_template_marketplace_entry (:1157-1161)。
func UpsertAgentTemplateMarketplaceEntry(packageID string, installed bool, source string) error {
	return upsertMarketplaceEntry(agentTemplateKind, packageID,
		map[string]any{"installed": installed, "source": source})
}

// UpsertAgentGroupMarketplaceEntry 更新分组安装状态（带 updated_at_ns 排序键）。
// Python: upsert_agent_group_marketplace_entry (:1175-1183)。
func UpsertAgentGroupMarketplaceEntry(packageID string, installed bool, source string) error {
	return upsertMarketplaceEntry(agentGroupKind, packageID, map[string]any{
		"installed": installed, "source": source,
		"updated_at_ns": time.Now().UnixNano(),
	})
}

// RemoveAgentTemplateMarketplaceEntry 删除模板状态条目。
// Python: remove_agent_template_marketplace_entry (:1185-1188)。
func RemoveAgentTemplateMarketplaceEntry(packageID string) error {
	return removeMarketplaceEntry(agentTemplateKind, packageID)
}

// RemoveAgentGroupMarketplaceEntry 删除分组状态条目。
// Python: remove_agent_group_marketplace_entry (:1197-1200)。
func RemoveAgentGroupMarketplaceEntry(packageID string) error {
	return removeMarketplaceEntry(agentGroupKind, packageID)
}

// HubInstallStateStore 设备安装状态存储（本地段）。
// Python: marketplace/hub_install_state.py HubInstallStateStore（get/get_by_package_id/
// set/remove 本地段）；Hub 远端同步函数不移植（偏差 D-1）。
type HubInstallStateStore struct {
	// kind 设备种类
	kind string
}

// NewHubInstallStateStore 构造。Python: _hub_install_state_store。
func NewHubInstallStateStore(kind string) *HubInstallStateStore {
	return &HubInstallStateStore{kind: kind}
}

// Get 按记录主键（包名或 asset_id）取记录。
// Python: HubInstallStateStore.get。
func (s *HubInstallStateStore) Get(id string) *HubInstallStateRecord {
	entries := readMarketplaceEntries(equipmentMarketplacePath(s.kind, ""))
	for _, entry := range entries {
		if eid, _ := entry["id"].(string); eid == id {
			return recordFromEntry(entry)
		}
	}
	return nil
}

// GetByPackageID 按本地包目录名反查记录。
// Python: HubInstallStateStore.get_by_package_id。
func (s *HubInstallStateStore) GetByPackageID(packageID string) *HubInstallStateRecord {
	entries := readMarketplaceEntries(equipmentMarketplacePath(s.kind, ""))
	for _, entry := range entries {
		if pid, _ := entry["package_id"].(string); pid != "" && pid == packageID {
			return recordFromEntry(entry)
		}
	}
	return nil
}

// Set 写/更新一条记录。
// Python: HubInstallStateStore.set。
func (s *HubInstallStateStore) Set(record *HubInstallStateRecord) error {
	marketplacePath := equipmentMarketplacePath(s.kind, "")
	entries := readMarketplaceEntries(marketplacePath)
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var asMap map[string]any
	if err := json.Unmarshal(payload, &asMap); err != nil {
		return err
	}
	// 过滤零值（对齐 Python record 的 omit-none 语义）
	for k, v := range asMap {
		if v == nil || v == "" || v == false || v == float64(0) {
			delete(asMap, k)
		}
	}
	replaced := false
	for i, entry := range entries {
		if id, _ := entry["id"].(string); id == record.ID {
			entries[i] = asMap
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, asMap)
	}
	return writeMarketplaceEntries(marketplacePath, entries)
}

// Remove 删除一条记录（按主键）。
// Python: HubInstallStateStore.remove。
func (s *HubInstallStateStore) Remove(id string) error {
	return removeMarketplaceEntry(s.kind, id)
}

// recordFromEntry entry → 强类型记录。
func recordFromEntry(entry map[string]any) *HubInstallStateRecord {
	id, _ := entry["id"].(string)
	rec := &HubInstallStateRecord{ID: id}
	if v, ok := entry["package_id"].(string); ok {
		rec.PackageID = v
	}
	if v, ok := entry["asset_id"].(string); ok {
		rec.AssetID = v
	}
	if v, ok := entry["kind"].(string); ok {
		rec.Kind = v
	}
	if v, ok := entry["installed"].(bool); ok {
		rec.Installed = v
	}
	if v, ok := entry["source"].(string); ok {
		rec.Source = v
	}
	if v, ok := entry["version"].(string); ok {
		rec.Version = v
	}
	if v, ok := entry["updated_at_ns"].(float64); ok {
		rec.UpdatedAtNs = int64(v)
	}
	return rec
}

// logUnreadableMarketplace 不可读 marketplace 的告警（对齐 Python logger.warning）。
func logUnreadableMarketplace(marketplacePath string) {
	logger.Warn(logger.ComponentAgentServer).Str("path", marketplacePath).
		Msg("[equipment] unreadable marketplace")
}
