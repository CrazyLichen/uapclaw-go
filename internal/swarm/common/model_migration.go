// 一次性的、幂等的旧模型名到稳定 ID 迁移。
// 对应 Python 代码：jiuwenswarm/common/model_migration.py:1-73。
package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MigrationReport 迁移结果报告。
// Python: model_migration.py:30 report = {"sessions": 0, "cron": 0, "ambiguous": 0}。
type MigrationReport struct {
	// Sessions 已迁移的会话数
	Sessions int
	// Cron 已迁移的 cron 任务数（⤵️ cron 章节，裁决 E，恒 0）
	Cron int
	// Ambiguous 因名字歧义跳过的数量
	Ambiguous int
}

// nameMap 构建无歧义模型名 → model_id 映射。
// Python: model_migration.py:14-22 _name_map。
// 名字集合 = {model_name, alias}；仅恰好一个 model_id 的名字进入映射。
func nameMap(catalog *ModelCatalog) map[string]string {
	if catalog == nil {
		catalog = NewModelCatalog(nil)
	}
	candidates := map[string][]string{}
	for _, model := range catalog.ListPublicModels() {
		modelID := asString(model["model_id"])
		seen := map[string]bool{}
		for _, name := range []string{asString(model["model_name"]), asString(model["alias"])} {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if !containsString(candidates[name], modelID) {
				candidates[name] = append(candidates[name], modelID)
			}
		}
	}
	out := map[string]string{}
	for name, ids := range candidates {
		if len(ids) == 1 {
			out[name] = ids[0]
		}
	}
	return out
}

// containsString 切片包含判定（Python `not in`）。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// MigrateLegacyModelSelections 把会话 metadata 的旧 model 名迁移为稳定 ID 引用。
// Python: model_migration.py:24-52 migrate_legacy_model_selections。
// 步骤：①构建映射 ②扫描会话目录 ③已有 model_selection 跳过 ④无 model 字段跳过
// ⑤无映射计 ambiguous ⑥经共享写边界更新（expected_selection 乐观锁语义）。
// cron 迁移分支（Python :55-73 migrate_legacy_cron_selections）⤵️ cron 章节（裁决 E）。
func MigrateLegacyModelSelections(catalog *ModelCatalog) (*MigrationReport, error) {
	mapping := nameMap(catalog)
	report := &MigrationReport{}
	sessionRoot := workspace.AgentSessionsDir()
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, err
	}
	for _, dirEntry := range entries {
		if !dirEntry.IsDir() {
			continue
		}
		path := filepath.Join(sessionRoot, dirEntry.Name(), "metadata.json")
		if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
			continue
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			// Python: :40 except (OSError, ValueError): continue
			continue
		}
		var data map[string]any
		if jsonErr := json.Unmarshal(raw, &data); jsonErr != nil {
			continue
		}
		// Python: :36-38 已有 model_selection 跳过
		if _, isMap := data["model_selection"].(map[string]any); isMap {
			continue
		}
		old := strings.TrimSpace(asString(data["model"]))
		if old == "" {
			continue
		}
		modelID, ok := mapping[old]
		if !ok {
			report.Ambiguous++
			continue
		}
		// Python: :44-47 update_session_model_selection(...) 带 expected_selection
		// 乐观锁（Go 侧 UpdateSessionModelSelection 无 expected 参数，平台差异注释）。
		if err := swarmsession.UpdateSessionModelSelection(dirEntry.Name(), map[string]any{"type": "model", "id": modelID}); err != nil {
			continue
		}
		report.Sessions++
	}
	return report, nil
}
