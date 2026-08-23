package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skillpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewSkillToolkit 验证创建 SkillToolkit
func TestNewSkillToolkit(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)
	if tk == nil {
		t.Fatal("SkillToolkit 不应为 nil")
	}
	if tk.manager != sm {
		t.Error("manager 应为传入的 SkillManager")
	}
}

// TestGetTools 验证返回 3 个工具
func TestGetTools(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	tools := tk.GetTools()
	if len(tools) != 3 {
		t.Fatalf("应返回 3 个工具, got %d", len(tools))
	}
	expectedNames := []string{"search_skill", "install_skill", "uninstall_skill"}
	for i, name := range expectedNames {
		if tools[i].Card().Name != name {
			t.Errorf("工具 %d: name = %q, want %q", i, tools[i].Card().Name, name)
		}
	}
}

// TestNormalizeSource 验证来源规范化
func TestNormalizeSource(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"skillnet", "skillnet", false},
		{"ClawHub", "clawhub", false},
		{"TeamSkillsHub", "teamskillshub", false},
		{"auto", "auto", false},
		{"", "skillnet", false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		got, err := normalizeSource(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("normalizeSource(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("normalizeSource(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestDetectSource 验证标识符推断来源
func TestDetectSource(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"https://example.com/skill", "skillnet", false},
		{"http://example.com/skill", "skillnet", false},
		{"my-awesome-skill", "clawhub", false},
		{"my.skill_v2/path", "clawhub", false},
		{"", "", true},
		{"@invalid", "", true},
	}
	for _, tt := range tests {
		got, err := detectSource(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("detectSource(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("detectSource(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestSafeInt 验证安全整数转换
func TestSafeInt(t *testing.T) {
	tests := []struct {
		input    any
		default_ int
		want     int
	}{
		{10, 5, 10},
		{0, 5, 5},
		{-1, 5, 5},
		{float64(3.7), 5, 3},
		{"7", 5, 7},
		{"abc", 5, 5},
		{nil, 5, 5},
	}
	for _, tt := range tests {
		got := safeInt(tt.input, tt.default_)
		if got != tt.want {
			t.Errorf("safeInt(%v, %d) = %d, want %d", tt.input, tt.default_, got, tt.want)
		}
	}
}

// TestNormalizeSearchItem 验证搜索结果归一化
func TestNormalizeSearchItem(t *testing.T) {
	installedNames := map[string]bool{"installed-skill": true}

	// ClawHub 项目
	clawhubItem := map[string]any{
		"display_name": "Test Skill",
		"slug":         "test-skill",
		"summary":      "A test skill",
		"version":      "1.0.0",
	}
	result := normalizeSearchItem(clawhubItem, "clawhub", installedNames)
	if result.Name != "Test Skill" {
		t.Errorf("name = %v, want Test Skill", result.Name)
	}
	if result.Identifier != "test-skill" {
		t.Errorf("identifier = %v, want test-skill", result.Identifier)
	}
	if result.Source != "clawhub" {
		t.Errorf("source = %v, want clawhub", result.Source)
	}
	if result.Installed != false {
		t.Error("应返回 installed=false")
	}

	// TeamSkillsHub 项目
	tshItem := map[string]any{
		"asset_id":     "asset-123",
		"display_name": "TSH Skill",
		"summary":      "A TSH skill",
		"version":      "2.0.0",
	}
	result = normalizeSearchItem(tshItem, "teamskillshub", installedNames)
	if result.Identifier != "asset-123" {
		t.Errorf("identifier = %v, want asset-123", result.Identifier)
	}
}

// TestSearchSkill_缺query 验证缺少 query 时返回失败
func TestSearchSkill_缺query(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.SearchSkill(context.Background(), map[string]any{
		"source": "clawhub",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 query 应返回 success=false")
	}
}

// TestSearchSkill_无效来源 验证无效来源
func TestSearchSkill_无效来源(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.SearchSkill(context.Background(), map[string]any{
		"query":  "test",
		"source": "invalid",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无效来源应返回 success=false")
	}
}

// TestSearchSkill_ClawHub 验证 ClawHub 搜索
func TestSearchSkill_ClawHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"skills": []map[string]any{
				{"display_name": "Test Skill", "slug": "test-skill", "summary": "A test skill", "version": "1.0.0"},
			},
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")
	t.Setenv("CLAWHUB_BASE_URL", server.URL)

	tk := NewSkillToolkit(sm)

	result, err := tk.SearchSkill(context.Background(), map[string]any{
		"query":  "test",
		"source": "clawhub",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestInstallSkill_缺identifier 验证缺少 identifier
func TestInstallSkill_缺identifier(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"source": "clawhub",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 identifier 应返回 success=false")
	}
}

// TestInstallSkill_缺source 验证缺少 source
func TestInstallSkill_缺source(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 source 应返回 success=false")
	}
}

// TestInstallSkill_auto来源 验证 auto 来源不允许
func TestInstallSkill_auto来源(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier": "test-skill",
		"source":     "auto",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("auto 来源应返回 success=false")
	}
}

// TestInstallSkill_ClawHub 验证 ClawHub 安装
func TestInstallSkill_ClawHub(t *testing.T) {
	// 创建一个合法的 ZIP 响应
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test-skill/SKILL.md")
	fmt.Fprint(w, "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文")
	zipWriter.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		w.Write(zipBuf.Bytes())
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")
	t.Setenv("CLAWHUB_BASE_URL", server.URL)

	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier": "test-skill",
		"source":     "clawhub",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if result["installed"] != true {
		t.Error("应返回 installed=true")
	}
}

// TestInstallSkill_已安装 验证重复安装
func TestInstallSkill_已安装(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 先安装一个技能
	skillDir := filepath.Join(sm.SkillsDir(), "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"), 0o644)

	// 添加到 local_skills
	sm.AddLocalSkill(map[string]any{
		"name":   "test-skill",
		"source": "clawhub",
		"origin": "clawhub:test-skill",
	})

	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier": "test-skill",
		"source":     "clawhub",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if result["already_installed"] != true {
		t.Error("应返回 already_installed=true")
	}
}

// TestUninstallSkill_缺name 验证缺少 name
func TestUninstallSkill_缺name(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.UninstallSkill(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 name 应返回 success=false")
	}
}

// TestUninstallSkill_未安装 验证未安装的技能
func TestUninstallSkill_未安装(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.UninstallSkill(context.Background(), map[string]any{
		"name": "nonexistent-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("未安装技能应返回 success=false")
	}
}

// TestBuildInstalledItem 验证构建已安装技能信息
func TestBuildInstalledItem(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建一个本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "test-skill",
		"source": "clawhub",
		"origin": "clawhub:test-skill",
	})

	tk := NewSkillToolkit(sm)
	item := tk.buildInstalledItem("test-skill", "clawhub")

	if item.Name != "test-skill" {
		t.Errorf("name = %v, want test-skill", item.Name)
	}
	if item.Identifier != "test-skill" {
		t.Errorf("identifier = %v, want test-skill", item.Identifier)
	}
	if item.Installed != true {
		t.Error("应返回 installed=true")
	}
	if item.Source != "clawhub" {
		t.Errorf("source = %v, want clawhub", item.Source)
	}
}

// TestSummarizeSearchPayload 验证搜索结果摘要
func TestSummarizeSearchPayload(t *testing.T) {
	payload := map[string]any{
		"success": true,
		"skills": []map[string]any{
			{"skill_name": "test", "skill_url": "https://example.com", "skill_description": "desc"},
		},
	}
	summary := summarizeSearchPayload("skillnet", "test query", payload)
	if summary["source"] != "skillnet" {
		t.Errorf("source = %v, want skillnet", summary["source"])
	}
	if summary["count"] != 1 {
		t.Errorf("count = %v, want 1", summary["count"])
	}
}

// TestListInstalledSkills 验证列出已安装技能
func TestListInstalledSkills(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建一个本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "test-skill",
		"source": "local",
	})
	sm.AddInstalledPlugin(map[string]any{
		"name":        "test-skill",
		"marketplace": "local",
		"source":      "local",
	})

	tk := NewSkillToolkit(sm)
	result := tk.listInstalledSkills(context.Background())

	if result.Success != true {
		t.Error("应返回 success=true")
	}
	if len(result.Items) == 0 {
		t.Error("应返回至少一个技能")
	}
}

// TestUninstallSkill_正常 验证正常卸载
func TestUninstallSkill_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建一个本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "test-skill",
		"source": "local",
	})
	sm.AddInstalledPlugin(map[string]any{
		"name":        "test-skill",
		"marketplace": "local",
		"source":      "local",
	})

	tk := NewSkillToolkit(sm)

	result, err := tk.UninstallSkill(context.Background(), map[string]any{
		"name": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if result["removed"] != true {
		t.Error("应返回 removed=true")
	}
}

// TestUninstallSkill_内置技能 验证内置技能不可卸载
func TestUninstallSkill_内置技能(t *testing.T) {
	tmpDir := t.TempDir()
	builtinDir := filepath.Join(tmpDir, "builtin_skills")
	os.MkdirAll(builtinDir, 0o755)
	skillDir := filepath.Join(builtinDir, "builtin-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: builtin-skill\ndescription: 内置技能\n---\n技能正文"), 0o644)

	t.Setenv("BUILTIN_SKILLS_DIR", builtinDir)

	sm := skillpkg.NewSkillManager(t.TempDir())
	tk := NewSkillToolkit(sm)

	result, err := tk.UninstallSkill(context.Background(), map[string]any{
		"name": "builtin-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("内置技能应返回 success=false")
	}
}

// TestSearchSkill_TeamSkillsHub 验证 TeamSkillsHub 搜索
func TestSearchSkill_TeamSkillsHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"items": []map[string]any{
					{"asset_id": "asset-123", "display_name": "TSH Skill", "summary": "A TSH skill", "version": "2.0.0"},
				},
				"total": 1,
			},
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.SearchSkill(context.Background(), map[string]any{
		"query":      "test",
		"source":     "teamskillshub",
		"market_url": server.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestSearchSkill_Auto 验证 auto 来源搜索
func TestSearchSkill_Auto(t *testing.T) {
	// ClawHub 搜索服务器
	clawhubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"skills": []map[string]any{
				{"display_name": "ClawHub Skill", "slug": "ch-skill", "summary": "A ClawHub skill"},
			},
		})
	}))
	defer clawhubServer.Close()

	// TeamSkillsHub 搜索服务器
	tshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"items": []map[string]any{
					{"asset_id": "tsh-asset", "display_name": "TSH Skill", "summary": "A TSH skill"},
				},
				"total": 1,
			},
		})
	}))
	defer tshServer.Close()

	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")
	t.Setenv("CLAWHUB_BASE_URL", clawhubServer.URL)

	tk := NewSkillToolkit(sm)

	result, err := tk.SearchSkill(context.Background(), map[string]any{
		"query":      "test",
		"source":     "auto",
		"market_url": tshServer.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestNormalizeSearchItem_SkillNet 验证 SkillNet 搜索结果归一化
func TestNormalizeSearchItem_SkillNet(t *testing.T) {
	installedNames := map[string]bool{}
	item := map[string]any{
		"skill_name":        "Test Skill",
		"skill_description": "A test skill",
		"skill_url":         "https://example.com/skill",
		"author":            "test-author",
		"stars":             5,
	}
	result := normalizeSearchItem(item, "skillnet", installedNames)
	if result.Name != "Test Skill" {
		t.Errorf("name = %v, want Test Skill", result.Name)
	}
	if result.Identifier != "https://example.com/skill" {
		t.Errorf("identifier = %v, want URL", result.Identifier)
	}
	if result.Author != "test-author" {
		t.Errorf("author = %v, want test-author", result.Author)
	}
	if result.Score == nil || *result.Score != 5 {
		t.Errorf("score = %v, want 5", result.Score)
	}
}

// TestFindInstalledByTarget_ClawHub 验证 ClawHub 已安装查找
func TestFindInstalledByTarget_ClawHub(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "test-skill",
		"source": "clawhub",
		"origin": "clawhub:test-skill",
	})

	tk := NewSkillToolkit(sm)

	result := tk.findInstalledByTarget("test-skill", "clawhub")
	if result == nil {
		t.Fatal("应找到已安装的技能")
	}
	if result.Name != "test-skill" {
		t.Errorf("name = %v, want test-skill", result.Name)
	}
}

// TestFindInstalledByTarget_未找到 验证未找到时返回 nil
func TestFindInstalledByTarget_未找到(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result := tk.findInstalledByTarget("nonexistent", "clawhub")
	if result != nil {
		t.Error("未找到应返回 nil")
	}
}

// TestFindInstalledByTarget_空标识符 验证空标识符返回 nil
func TestFindInstalledByTarget_空标识符(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result := tk.findInstalledByTarget("", "clawhub")
	if result != nil {
		t.Error("空标识符应返回 nil")
	}
}

// TestGetInstalledNames_有数据 验证有数据时返回名称集合
func TestGetInstalledNames_有数据(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	sm.AddInstalledPlugin(map[string]any{
		"name":        "test-skill",
		"marketplace": "local",
		"source":      "local",
	})

	tk := NewSkillToolkit(sm)
	names := tk.getInstalledNames()

	if !names["test-skill"] {
		t.Error("应包含 test-skill")
	}
}

// TestToBool 验证 toBool 转换
// TestToIntPtr 验证 toIntPtr 转换
func TestToIntPtr(t *testing.T) {
	tests := []struct {
		input any
		want  *int
	}{
		{nil, nil},
		{float64(42), intPtr(42)},
		{float64(0), intPtr(0)},
		{int(7), intPtr(7)},
		{json.Number("99"), intPtr(99)},
		{json.Number("bad"), nil},
		{"not-a-number", nil},
	}
	for _, tt := range tests {
		got := toIntPtr(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("toIntPtr(%v) = %v, want nil", tt.input, *got)
			}
		} else if got == nil || *got != *tt.want {
			var gotVal int
			if got != nil {
				gotVal = *got
			}
			t.Errorf("toIntPtr(%v) = %v, want %v", tt.input, gotVal, *tt.want)
		}
	}
}

// intPtr 辅助函数，返回 int 指针
func intPtr(v int) *int {
	return &v
}

func TestToBool(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{nil, false},
		{42, false},
	}
	for _, tt := range tests {
		got := toBool(tt.input)
		if got != tt.want {
			t.Errorf("toBool(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestToString 验证 toString 转换
func TestToString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{nil, ""},
		{42, "42"},
	}
	for _, tt := range tests {
		got := toString(tt.input)
		if got != tt.want {
			t.Errorf("toString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestToSliceOfAny 验证 toSliceOfAny 转换
func TestToSliceOfAny(t *testing.T) {
	// []any
	result, ok := toSliceOfAny([]any{"a", "b"})
	if !ok || len(result) != 2 {
		t.Errorf("[]any 转换失败: ok=%v, len=%d", ok, len(result))
	}
	// []map[string]any
	result, ok = toSliceOfAny([]map[string]any{{"key": "val"}})
	if !ok || len(result) != 1 {
		t.Errorf("[]map[string]any 转换失败: ok=%v, len=%d", ok, len(result))
	}
	// nil
	_, ok = toSliceOfAny(nil)
	if ok {
		t.Error("nil 应返回 false")
	}
}

// TestInstallSkill_TeamSkillsHub 验证 TeamSkillsHub 安装
func TestInstallSkill_TeamSkillsHub(t *testing.T) {
	// 创建 ZIP
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test-skill/SKILL.md")
	fmt.Fprint(w, "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文")
	zipWriter.Close()

	// 下载服务器
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		w.Write(zipBuf.Bytes())
	}))
	defer downloadServer.Close()

	// 元数据服务器
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"asset_id":        "test-asset",
				"download_url":    downloadServer.URL + "/file.zip",
				"checksum_sha256": "",
			},
		})
	}))
	defer metadataServer.Close()

	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	t.Setenv("TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS", "127.0.0.1,localhost")

	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier": "test-asset",
		"source":     "teamskillshub",
		"market_url": metadataServer.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestFindInstalledByTarget_SkillNet 验证 SkillNet 已安装查找
func TestFindInstalledByTarget_SkillNet(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "sn-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: sn-skill\ndescription: SkillNet 技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "sn-skill",
		"origin": "https://example.com/sn-skill",
		"source": "skillnet",
	})

	tk := NewSkillToolkit(sm)

	result := tk.findInstalledByTarget("https://example.com/sn-skill", "skillnet")
	if result == nil {
		t.Fatal("应找到已安装的 SkillNet 技能")
	}
	if result.Name != "sn-skill" {
		t.Errorf("name = %v, want sn-skill", result.Name)
	}
}

// TestFindInstalledByTarget_TeamSkillsHub 验证 TeamSkillsHub 已安装查找
func TestFindInstalledByTarget_TeamSkillsHub(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "tsh-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: tsh-skill\ndescription: TSH 技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "tsh-skill",
		"origin": "teamskillshub:tsh-asset-123",
		"source": "teamskillshub",
	})

	tk := NewSkillToolkit(sm)

	// 按 asset_id 查找
	result := tk.findInstalledByTarget("tsh-asset-123", "teamskillshub")
	if result == nil {
		t.Fatal("应找到已安装的 TeamSkillsHub 技能")
	}
	if result.Name != "tsh-skill" {
		t.Errorf("name = %v, want tsh-skill", result.Name)
	}
}

// TestFindInstalledByTarget_PluginSource 验证 installed_plugins 中查找
func TestFindInstalledByTarget_PluginSource(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能（用于 buildInstalledItem）
	skillDir := filepath.Join(sm.SkillsDir(), "plugin-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: plugin-skill\ndescription: 插件技能\n---\n技能正文"), 0o644)

	sm.AddInstalledPlugin(map[string]any{
		"name":        "plugin-skill",
		"marketplace": "clawhub",
		"source":      "clawhub",
	})

	tk := NewSkillToolkit(sm)

	result := tk.findInstalledByTarget("plugin-skill", "clawhub")
	if result == nil {
		t.Fatal("应通过 installed_plugins 找到技能")
	}
	if result.Name != "plugin-skill" {
		t.Errorf("name = %v, want plugin-skill", result.Name)
	}
}

// TestBuildInstalledItem_TeamSkillsHub 验证 TeamSkillsHub 来源清理
func TestBuildInstalledItem_TeamSkillsHub(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能
	skillDir := filepath.Join(sm.SkillsDir(), "tsh-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: tsh-skill\ndescription: TSH 技能\n---\n技能正文"), 0o644)

	sm.AddLocalSkill(map[string]any{
		"name":   "tsh-skill",
		"origin": "teamskillshub:tsh-asset-456",
		"source": "teamskillshub",
	})

	tk := NewSkillToolkit(sm)
	item := tk.buildInstalledItem("tsh-skill", "teamskillshub")

	// 验证 identifier 已清理前缀
	if item.Identifier != "tsh-asset-456" {
		t.Errorf("identifier = %v, want tsh-asset-456", item.Identifier)
	}
}

// TestInstallSkill_SkillNet_轮询成功 验证 SkillNet 安装轮询成功
func TestInstallSkill_SkillNet_轮询成功(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)

	// 创建本地技能目录（用于 buildInstalledItem）
	skillDir := filepath.Join(sm.SkillsDir(), "sn-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: sn-skill\ndescription: SkillNet 技能\n---\n技能正文"), 0o644)

	tk := NewSkillToolkit(sm)

	// 在 goroutine 中延迟更新 install job 为 done
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 等待 install job 被创建
		time.Sleep(800 * time.Millisecond)
		// 找到并更新 job
		for _, jobID := range sm.GetInstallJobIDs() {
			sm.SetSkillnetInstallJob(jobID, map[string]any{
				"status": "done",
				"skill": map[string]any{
					"name": "sn-skill",
				},
			})
		}
		// 添加 local skill 以便 buildInstalledItem 能找到
		sm.AddLocalSkill(map[string]any{
			"name":   "sn-skill",
			"source": "skillnet",
			"origin": "https://example.com/skill",
		})
	}()

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier":  "https://example.com/skill",
		"source":      "skillnet",
		"timeout_sec": 5,
	})
	<-done

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !toBool(result["success"]) {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if result["installed"] != true {
		t.Error("应返回 installed=true")
	}
}

// TestInstallSkill_SkillNet_超时 验证 SkillNet 安装超时
func TestInstallSkill_SkillNet_超时(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier":  "https://example.com/skill",
		"source":      "skillnet",
		"timeout_sec": 1,
	})

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) {
		t.Errorf("超时应返回 success=false, got %v", result)
	}
	detail := toString(result["detail"])
	if !strings.Contains(detail, "timed out") {
		t.Errorf("detail 应包含 'timed out', got %q", detail)
	}
}

// TestInstallSkill_SkillNet_安装失败 验证 SkillNet 安装请求失败
func TestInstallSkill_SkillNet_安装失败(t *testing.T) {
	tmpDir := t.TempDir()
	sm := skillpkg.NewSkillManager(tmpDir)
	tk := NewSkillToolkit(sm)

	// 在 goroutine 中延迟更新 install job 为 failed
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(800 * time.Millisecond)
		for _, jobID := range sm.GetInstallJobIDs() {
			sm.SetSkillnetInstallJob(jobID, map[string]any{
				"status": "failed",
				"detail": "download failed",
			})
		}
	}()

	result, err := tk.InstallSkill(context.Background(), map[string]any{
		"identifier":  "https://example.com/skill",
		"source":      "skillnet",
		"timeout_sec": 5,
	})
	<-done

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) {
		t.Errorf("安装失败应返回 success=false, got %v", result)
	}
}
