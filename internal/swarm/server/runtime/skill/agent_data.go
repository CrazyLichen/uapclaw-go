package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentDataEntry agent-data.json 中的技能条目
type agentDataEntry struct {
	// Name 技能名称
	Name string `json:"name"`
	// Path 技能路径
	Path string `json:"path"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshAgentDataIndexes 生成 agent-data.json 索引文件。
// 遍历 skillsDir 的 parent 目录 + getMirrorSkillsDirs() 的 parent 目录，
// 收集技能信息并生成 agent-data.json。
//
// 对齐 Python: refresh_agent_data_indexes
func (sm *SkillManager) refreshAgentDataIndexes() {
	// 收集需要生成索引的目录
	dirs := []string{filepath.Dir(sm.skillsDir)}
	for _, mirrorDir := range sm.getMirrorSkillsDirs() {
		dirs = append(dirs, filepath.Dir(mirrorDir))
	}

	for _, dir := range dirs {
		generateAgentDataForWorkspace(dir)
	}
}

// generateAgentDataForWorkspace 遍历工作区目录收集技能信息并生成 agent-data.json。
//
// 对齐 Python: generate_agent_data_for_workspace
func generateAgentDataForWorkspace(workspaceRoot string) {
	skillsDir := filepath.Join(workspaceRoot, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return // 目录不存在，跳过
	}

	var dataEntries []agentDataEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(skillsDir, entry.Name(), "skill")
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMd); err != nil {
			continue // 无 SKILL.md，跳过
		}
		meta, _ := parseSKILLMd(skillMd)
		name := entry.Name()
		if meta != nil {
			if n, ok := meta["name"].(string); ok && n != "" {
				name = n
			}
		}
		dataEntries = append(dataEntries, agentDataEntry{
			Name: name,
			Path: skillDir,
		})
	}

	// 写入 agent-data.json
	outputPath := filepath.Join(workspaceRoot, "agent-data.json")
	data, err := json.MarshalIndent(dataEntries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(outputPath, data, 0o644)
}
