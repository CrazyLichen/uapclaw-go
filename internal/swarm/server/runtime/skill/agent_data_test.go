package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeFileForAgentData 在指定目录下写入文件（自动建父目录）
func writeFileForAgentData(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

// readAgentDataFile 读取并解析 agent-data.json
func readAgentDataFile(t *testing.T, workspaceDir string) map[string][]agentDataEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workspaceDir, agentDataFileName))
	if err != nil {
		t.Fatalf("读取 agent-data.json 失败: %v", err)
	}
	var data map[string][]agentDataEntry
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("解析 agent-data.json 失败: %v（内容: %s）", err, string(raw))
	}
	return data
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGenerateAgentDataForWorkspace_按目录分组并标注markdown 验证输出为
// 「文件夹 → 文件数组」且 isMarkdown 正确（对齐前端 Record<string, FileInfo[]> 契约）
func TestGenerateAgentDataForWorkspace_按目录分组并标注markdown(t *testing.T) {
	agentRoot := t.TempDir()
	workspaceDir := filepath.Join(agentRoot, "workspace")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "AGENT.md"), "# agent")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "skills", "demo", "SKILL.md"), "---\nname: demo\n---\n")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "skills", "demo", "notes.txt"), "txt")

	if err := GenerateAgentDataForWorkspace(workspaceDir); err != nil {
		t.Fatalf("重建索引失败: %v", err)
	}

	data := readAgentDataFile(t, workspaceDir)

	rootEntries := data["workspace"]
	if len(rootEntries) != 1 {
		t.Fatalf("workspace 层应有 1 个条目，实际 %d: %v", len(rootEntries), rootEntries)
	}
	if rootEntries[0].Name != "AGENT.md" || rootEntries[0].Path != "agent/workspace/AGENT.md" || !rootEntries[0].IsMarkdown {
		t.Errorf("顶层条目不符合契约: %+v", rootEntries[0])
	}

	skillEntries := data["workspace/skills/demo"]
	if len(skillEntries) != 2 {
		t.Fatalf("skills/demo 应有 2 个条目，实际 %d: %v", len(skillEntries), skillEntries)
	}
	// 按 path 升序：SKILL.md（大写 S）在 notes.txt（小写 n）之前
	if skillEntries[0].Path != "agent/workspace/skills/demo/SKILL.md" || !skillEntries[0].IsMarkdown {
		t.Errorf("首个条目应为 markdown 的 SKILL.md: %+v", skillEntries[0])
	}
	if skillEntries[1].Path != "agent/workspace/skills/demo/notes.txt" || skillEntries[1].IsMarkdown {
		t.Errorf("次个条目应为非 markdown 的 notes.txt: %+v", skillEntries[1])
	}
}

// TestGenerateAgentDataForWorkspace_跳过隐藏文件与目录 验证隐藏项不入索引
func TestGenerateAgentDataForWorkspace_跳过隐藏文件与目录(t *testing.T) {
	agentRoot := t.TempDir()
	workspaceDir := filepath.Join(agentRoot, "workspace")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "AGENT.md"), "# agent")
	writeFileForAgentData(t, filepath.Join(workspaceDir, ".hidden.md"), "hidden")
	writeFileForAgentData(t, filepath.Join(workspaceDir, ".agent_history", "t.md"), "hist")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "skills", ".cache", "x.md"), "cache")

	if err := GenerateAgentDataForWorkspace(workspaceDir); err != nil {
		t.Fatalf("重建索引失败: %v", err)
	}

	data := readAgentDataFile(t, workspaceDir)
	for folder, entries := range data {
		for _, e := range entries {
			if e.Name == ".hidden.md" || e.Path == "agent/workspace/.hidden.md" {
				t.Errorf("隐藏文件不应入索引: folder=%s entry=%+v", folder, e)
			}
			if filepath.Base(filepath.Dir(e.Path)) == ".agent_history" || filepath.Base(filepath.Dir(e.Path)) == ".cache" {
				t.Errorf("隐藏目录下文件不应入索引: folder=%s entry=%+v", folder, e)
			}
		}
	}
	if len(data) != 1 {
		t.Errorf("应只剩 workspace 一个 folder key，实际 %v", data)
	}
}

// TestGenerateAgentDataForWorkspace_语言后缀归一 验证 x_zh.md / x_en.md 归一为 x.md
func TestGenerateAgentDataForWorkspace_语言后缀归一(t *testing.T) {
	agentRoot := t.TempDir()
	workspaceDir := filepath.Join(agentRoot, "workspace")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "IDENTITY_zh.MD"), "zh")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "SOUL_en.mdx"), "en")
	writeFileForAgentData(t, filepath.Join(workspaceDir, "PLAIN.txt"), "txt")

	if err := GenerateAgentDataForWorkspace(workspaceDir); err != nil {
		t.Fatalf("重建索引失败: %v", err)
	}

	data := readAgentDataFile(t, workspaceDir)
	entries := data["workspace"]
	got := make(map[string]string, len(entries))
	for _, e := range entries {
		got[e.Path] = e.Name
	}

	// 对齐 Python：保留原后缀大小写（f"{stem}.{suffix}"），故 _zh.MD → IDENTITY.MD
	if got["agent/workspace/IDENTITY.MD"] != "IDENTITY.MD" {
		t.Errorf("_zh 应归一为 IDENTITY.MD: %v", got)
	}
	if got["agent/workspace/SOUL.mdx"] != "SOUL.mdx" {
		t.Errorf("_en 应归一: %v", got)
	}
	if got["agent/workspace/PLAIN.txt"] != "PLAIN.txt" {
		t.Errorf("非 markdown 不应改后缀: %v", got)
	}
}

// TestGenerateAgentDataForWorkspace_目录不存在返回错误 验证错误路径
func TestGenerateAgentDataForWorkspace_目录不存在返回错误(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-exist")
	if err := GenerateAgentDataForWorkspace(missing); err == nil {
		t.Fatal("目录不存在应返回错误")
	}
}

// TestNormalizeLangSuffix 验证语言后缀归一化的边界
func TestNormalizeLangSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"AGENT.md", "AGENT.md"},
		{"IDENTITY_zh.md", "IDENTITY.md"},
		{"SOUL_en.MD", "SOUL.MD"},
		{"notes_zh.txt", "notes_zh.txt"},
		{"noext", "noext"},
		{"_zh.md", ".md"},
	}
	for _, c := range cases {
		if got := normalizeLangSuffix(c.in); got != c.want {
			t.Errorf("normalizeLangSuffix(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestIsMarkdownFileName 验证 markdown 判定
func TestIsMarkdownFileName(t *testing.T) {
	cases := map[string]bool{
		"a.md": true, "a.MD": true, "a.mdx": true, "a.MDX": true,
		"a.txt": false, "a": false, "a.json": false,
	}
	for name, want := range cases {
		if got := isMarkdownFileName(name); got != want {
			t.Errorf("isMarkdownFileName(%q)=%v，期望 %v", name, got, want)
		}
	}
}
