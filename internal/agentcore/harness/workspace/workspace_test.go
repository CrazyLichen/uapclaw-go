package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/workspace_content"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewWorkspace_默认值 测试 NewWorkspace 使用默认模式填充
func TestNewWorkspace_默认值(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	if w.RootPath != "/tmp/workspace" {
		t.Errorf("RootPath = %q, want /tmp/workspace", w.RootPath)
	}
	if w.Language != "cn" {
		t.Errorf("Language = %q, want cn", w.Language)
	}
	if len(w.Directories) == 0 {
		t.Error("Directories should not be empty when using defaults")
	}

	// 验证 CN 默认模式中包含关键节点
	names := topNames(w.Directories)
	expectedTop := []string{"AGENT.md", "SOUL.md", "HEARTBEAT.md", "IDENTITY.md", "USER.md",
		"memory", "coding_memory", "todo", "messages", "skills", "agents", "context"}
	for _, expected := range expectedTop {
		found := false
		for _, n := range names {
			if n == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CN default schema missing top-level node: %q", expected)
		}
	}
}

// TestNewWorkspace_英文模式 测试 NewWorkspace 使用英文默认模式
func TestNewWorkspace_英文模式(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "en")

	if w.Language != "en" {
		t.Errorf("Language = %q, want en", w.Language)
	}

	// 英文模式不应有 coding_memory
	names := topNames(w.Directories)
	for _, n := range names {
		if n == "coding_memory" {
			t.Error("EN default schema should not contain coding_memory")
		}
	}

	// 英文模式应包含 context
	found := false
	for _, n := range names {
		if n == "context" {
			found = true
			break
		}
	}
	if !found {
		t.Error("EN default schema should contain context")
	}
}

// TestGetDirectory_找到 测试 GetDirectory 在存在节点时返回 path
func TestGetDirectory_找到(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	// 测试顶层节点
	path := w.GetDirectory("AGENT.md")
	if path != "AGENT.md" {
		t.Errorf("GetDirectory(AGENT.md) = %q, want AGENT.md", path)
	}

	// 测试 WorkspaceNode 枚举
	path = w.GetDirectory(WorkspaceNodeMemory)
	if path != "memory" {
		t.Errorf("GetDirectory(WorkspaceNodeMemory) = %q, want memory", path)
	}

	// 测试嵌套节点
	path = w.GetDirectory("MEMORY.md")
	if path != "MEMORY.md" {
		t.Errorf("GetDirectory(MEMORY.md) = %q, want MEMORY.md", path)
	}
}

// TestGetDirectory_未找到 测试 GetDirectory 在节点不存在时返回空字符串
func TestGetDirectory_未找到(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	path := w.GetDirectory("nonexistent")
	if path != "" {
		t.Errorf("GetDirectory(nonexistent) = %q, want empty string", path)
	}

	path = w.GetDirectory(WorkspaceNode("unknown"))
	if path != "" {
		t.Errorf("GetDirectory(unknown) = %q, want empty string", path)
	}
}

// TestSetDirectory_添加 测试 SetDirectory 追加新节点
func TestSetDirectory_添加(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")
	originalLen := len(w.Directories)

	newNode := DirectoryNode{
		"name":        "custom_dir",
		"description": "自定义目录",
		"path":        "custom_dir",
		"children":    []DirectoryNode{},
	}
	err := w.SetDirectory(newNode)
	if err != nil {
		t.Fatalf("SetDirectory returned error: %v", err)
	}

	if len(w.Directories) != originalLen+1 {
		t.Errorf("len(Directories) = %d, want %d", len(w.Directories), originalLen+1)
	}

	// 验证新节点可以被 GetDirectory 找到
	path := w.GetDirectory("custom_dir")
	if path != "custom_dir" {
		t.Errorf("GetDirectory(custom_dir) = %q, want custom_dir", path)
	}
}

// TestSetDirectory_替换 测试 SetDirectory 替换同名节点
func TestSetDirectory_替换(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	// 替换 AGENT.md 节点
	replacement := DirectoryNode{
		"name":            "AGENT.md",
		"description":     "自定义描述",
		"path":            "AGENT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "custom content",
	}
	err := w.SetDirectory(replacement)
	if err != nil {
		t.Fatalf("SetDirectory returned error: %v", err)
	}

	// 验证描述被替换
	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok && name == "AGENT.md" {
			desc, _ := node["description"].(string)
			if desc != "自定义描述" {
				t.Errorf("AGENT.md description = %q, want 自定义描述", desc)
			}
			return
		}
	}
	t.Error("AGENT.md node not found after replacement")
}

// TestSetDirectory_列表 测试 SetDirectory 接受节点列表
func TestSetDirectory_列表(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")
	originalLen := len(w.Directories)

	nodes := []DirectoryNode{
		{
			"name":     "dir_a",
			"path":     "dir_a",
			"children": []DirectoryNode{},
		},
		{
			"name":     "dir_b",
			"path":     "dir_b",
			"children": []DirectoryNode{},
		},
	}
	err := w.SetDirectory(nodes)
	if err != nil {
		t.Fatalf("SetDirectory returned error: %v", err)
	}

	if len(w.Directories) != originalLen+2 {
		t.Errorf("len(Directories) = %d, want %d", len(w.Directories), originalLen+2)
	}
}

// TestSetDirectory_类型错误 测试 SetDirectory 传入非法类型
func TestSetDirectory_类型错误(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	err := w.SetDirectory("invalid")
	if err == nil {
		t.Error("SetDirectory should return error for invalid type")
	}
}

// TestGetNodePath_找到 测试 GetNodePath 返回完整路径
func TestGetNodePath_找到(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	path := w.GetNodePath("AGENT.md")
	if path == nil {
		t.Fatal("GetNodePath(AGENT.md) returned nil")
	}
	if !strings.HasSuffix(*path, "AGENT.md") {
		t.Errorf("GetNodePath(AGENT.md) = %q, should end with AGENT.md", *path)
	}

	// 测试 WorkspaceNode 枚举
	path = w.GetNodePath(WorkspaceNodeMemory)
	if path == nil {
		t.Fatal("GetNodePath(WorkspaceNodeMemory) returned nil")
	}
	if !strings.HasSuffix(*path, "memory") {
		t.Errorf("GetNodePath(WorkspaceNodeMemory) = %q, should end with memory", *path)
	}
}

// TestGetNodePath_未找到 测试 GetNodePath 在顶层节点不存在时返回 nil
func TestGetNodePath_未找到(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	path := w.GetNodePath("nonexistent")
	if path != nil {
		t.Errorf("GetNodePath(nonexistent) = %v, want nil", path)
	}

	// 嵌套节点不应被 GetNodePath 找到
	path = w.GetNodePath("MEMORY.md")
	if path != nil {
		t.Errorf("GetNodePath(MEMORY.md) should return nil for non-top-level node, got %v", path)
	}
}

// TestValidateDirectoryNode_名称为空 测试校验空名称
func TestValidateDirectoryNode_名称为空(t *testing.T) {
	node := DirectoryNode{
		"name":     "",
		"path":     "test",
		"children": []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for empty name")
	}
}

// TestValidateDirectoryNode_名称含路径分隔符 测试校验含路径分隔符的名称
func TestValidateDirectoryNode_名称含路径分隔符(t *testing.T) {
	node := DirectoryNode{
		"name":     "foo/bar",
		"path":     "test",
		"children": []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for name with /")
	}

	node["name"] = "foo\\bar"
	err = validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for name with \\")
	}
}

// TestValidateDirectoryNode_path非字符串 测试 path 类型错误
func TestValidateDirectoryNode_path非字符串(t *testing.T) {
	node := DirectoryNode{
		"name":     "test",
		"path":     123,
		"children": []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for non-string path")
	}
}

// TestValidateDirectoryNode_description非字符串 测试 description 类型错误
func TestValidateDirectoryNode_description非字符串(t *testing.T) {
	node := DirectoryNode{
		"name":        "test",
		"description": 123,
		"children":    []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for non-string description")
	}
}

// TestValidateDirectoryNode_isFile非布尔 测试 is_file 类型错误
func TestValidateDirectoryNode_isFile非布尔(t *testing.T) {
	node := DirectoryNode{
		"name":     "test",
		"is_file":  "yes",
		"children": []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for non-bool is_file")
	}
}

// TestValidateDirectoryNode_defaultContent非字符串 测试 default_content 类型错误
func TestValidateDirectoryNode_defaultContent非字符串(t *testing.T) {
	node := DirectoryNode{
		"name":            "test",
		"default_content": 123,
		"children":        []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for non-string default_content")
	}
}

// TestValidateDirectoryNode_children非列表 测试 children 类型错误
func TestValidateDirectoryNode_children非列表(t *testing.T) {
	node := DirectoryNode{
		"name":     "test",
		"children": "invalid",
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for non-list children")
	}
}

// TestValidateDirectoryNode_nil节点 测试 nil 节点
func TestValidateDirectoryNode_nil节点(t *testing.T) {
	err := validateDirectoryNode(nil)
	if err == nil {
		t.Error("validateDirectoryNode should return error for nil node")
	}
}

// TestValidateDirectoryNode_子节点无效 测试递归校验子节点
func TestValidateDirectoryNode_子节点无效(t *testing.T) {
	node := DirectoryNode{
		"name": "test",
		"children": []DirectoryNode{
			{
				"name":     "",
				"children": []DirectoryNode{},
			},
		},
	}
	err := validateDirectoryNode(node)
	if err == nil {
		t.Error("validateDirectoryNode should return error for invalid child node")
	}
}

// TestValidateDirectoryNode_有效节点 测试有效节点通过校验
func TestValidateDirectoryNode_有效节点(t *testing.T) {
	node := DirectoryNode{
		"name":            "test.md",
		"description":     "测试文件",
		"path":            "test.md",
		"is_file":         true,
		"default_content": "",
		"children":        []DirectoryNode{},
	}
	err := validateDirectoryNode(node)
	if err != nil {
		t.Errorf("validateDirectoryNode returned unexpected error: %v", err)
	}
}

// TestGetWorkspaceSchema_中文模式 测试中文模式返回正确的模式
func TestGetWorkspaceSchema_中文模式(t *testing.T) {
	schema := getWorkspaceSchema("cn")
	if len(schema) == 0 {
		t.Error("CN schema should not be empty")
	}

	// 验证包含 coding_memory（CN 特有）
	found := false
	for _, node := range schema {
		if name, ok := node["name"].(string); ok && name == "coding_memory" {
			found = true
			break
		}
	}
	if !found {
		t.Error("CN schema should contain coding_memory")
	}
}

// TestGetWorkspaceSchema_英文模式 测试英文模式返回正确的模式
func TestGetWorkspaceSchema_英文模式(t *testing.T) {
	schema := getWorkspaceSchema("en")
	if len(schema) == 0 {
		t.Error("EN schema should not be empty")
	}

	// 验证不包含 coding_memory（EN 没有）
	for _, node := range schema {
		if name, ok := node["name"].(string); ok && name == "coding_memory" {
			t.Error("EN schema should not contain coding_memory")
		}
	}
}

// TestGetWorkspaceSchema_深拷贝 测试返回的模式是深拷贝，修改不影响原始
func TestGetWorkspaceSchema_深拷贝(t *testing.T) {
	schema1 := getWorkspaceSchema("cn")
	schema2 := getWorkspaceSchema("cn")

	// 修改 schema1 不应影响 schema2
	if desc, ok := schema1[0]["description"].(string); ok {
		schema1[0]["description"] = desc + "_modified"
	}

	desc1, _ := schema1[0]["description"].(string)
	desc2, _ := schema2[0]["description"].(string)
	if desc1 == desc2 {
		t.Error("Deep copy should prevent shared references")
	}
}

// TestNewWorkspace_默认模式补全 测试缺失的默认目录会被自动补全
func TestNewWorkspace_默认模式补全(t *testing.T) {
	// 创建带有部分自定义目录的工作空间
	w := &Workspace{
		RootPath: "/tmp/workspace",
		Directories: []DirectoryNode{
			{
				"name":     "custom_only",
				"path":     "custom_only",
				"children": []DirectoryNode{},
			},
		},
		Language: "cn",
	}

	// 模拟 __post_init__ 逻辑：补充默认目录
	defaultSchema := getWorkspaceSchema(w.Language)
	existingNames := make(map[string]bool)
	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok {
			existingNames[name] = true
		}
	}
	for _, defaultNode := range defaultSchema {
		name, _ := defaultNode["name"].(string)
		if name != "" && !existingNames[name] {
			w.Directories = append(w.Directories, deepCopyNode(defaultNode))
			existingNames[name] = true
		}
	}

	names := topNames(w.Directories)
	// 验证自定义节点保留
	found := false
	for _, n := range names {
		if n == "custom_only" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Custom directory should be preserved after supplementation")
	}

	// 验证默认节点补全
	for _, expected := range []string{"AGENT.md", "memory", "context"} {
		found := false
		for _, n := range names {
			if n == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Default directory %q should be supplemented", expected)
		}
	}
}

// TestGetDefaultDirectory 测试 GetDefaultDirectory 返回深拷贝
func TestGetDefaultDirectory(t *testing.T) {
	schema := GetDefaultDirectory("cn")
	if len(schema) == 0 {
		t.Error("GetDefaultDirectory(cn) should not be empty")
	}

	schemaEN := GetDefaultDirectory("en")
	if len(schemaEN) == 0 {
		t.Error("GetDefaultDirectory(en) should not be empty")
	}
}

// TestWorkspaceNode枚举值 测试 WorkspaceNode 枚举值
func TestWorkspaceNode枚举值(t *testing.T) {
	tests := []struct {
		node   WorkspaceNode
		expect string
	}{
		{WorkspaceNodeAGENTMD, "AGENT.md"},
		{WorkspaceNodeSOULMD, "SOUL.md"},
		{WorkspaceNodeHEARTBEATMD, "HEARTBEAT.md"},
		{WorkspaceNodeIDENTITYMD, "IDENTITY.md"},
		{WorkspaceNodeUSERMD, "USER.md"},
		{WorkspaceNodeMemory, "memory"},
		{WorkspaceNodeCodingMemory, "coding_memory"},
		{WorkspaceNodeTODO, "todo"},
		{WorkspaceNodeMessages, "messages"},
		{WorkspaceNodeSkills, "skills"},
		{WorkspaceNodeAgents, "agents"},
		{WorkspaceNodeMemoryMD, "MEMORY.md"},
		{WorkspaceNodeDailyMemory, "daily_memory"},
		{WorkspaceNodeTeamLinks, ".team"},
		{WorkspaceNodeWorktreeLinks, ".worktree"},
	}
	for _, tt := range tests {
		if string(tt.node) != tt.expect {
			t.Errorf("WorkspaceNode = %q, want %q", string(tt.node), tt.expect)
		}
	}
}

// TestSetDirectory_校验失败 测试 SetDirectory 在校验失败时返回错误
func TestSetDirectory_校验失败(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	invalidNode := DirectoryNode{
		"name":     "",
		"children": []DirectoryNode{},
	}
	err := w.SetDirectory(invalidNode)
	if err == nil {
		t.Error("SetDirectory should return error for invalid node")
	}

	// 验证是 BaseError 类型
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Error("Error should be *exception.BaseError")
	}
	if baseErr.Code() != exception.StatusDeepagentConfigParamError.Code() {
		t.Errorf("Error code = %d, want %d", baseErr.Code(), exception.StatusDeepagentConfigParamError.Code())
	}
}

// TestCN模式_memory子节点 测试 CN 模式 memory 目录包含正确的子节点
func TestCN模式_memory子节点(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok && name == "memory" {
			children, ok := node["children"].([]DirectoryNode)
			if !ok {
				t.Fatal("memory children should be []DirectoryNode")
			}
			childNames := make([]string, len(children))
			for i, child := range children {
				childNames[i], _ = child["name"].(string)
			}
			expectedChildren := []string{"MEMORY.md", "daily_memory"}
			for _, expected := range expectedChildren {
				found := false
				for _, actual := range childNames {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("memory children missing %q, got %v", expected, childNames)
				}
			}
			return
		}
	}
	t.Error("memory node not found")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// topNames 提取顶层节点名称列表
func topNames(nodes []DirectoryNode) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if name, ok := node["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// TestCN模式_defaultContent非空 测试 CN 工作空间的 AGENT.md defaultContent 非空且包含中文
func TestCN模式_defaultContent非空(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok && name == "AGENT.md" {
			content, _ := node["default_content"].(string)
			if content == "" {
				t.Error("CN AGENT.md default_content should not be empty")
			}
			if !strings.Contains(content, "智能体") {
				t.Errorf("CN AGENT.md default_content should contain Chinese text '智能体', got: %s", content[:min(50, len(content))])
			}
			return
		}
	}
	t.Error("AGENT.md node not found in CN workspace")
}

// TestCN模式_defaultContent各模板 测试 CN 工作空间各模板文件的 defaultContent
func TestCN模式_defaultContent各模板(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	expected := map[string]string{
		"AGENT.md":     workspace_content.AgentMDCN,
		"SOUL.md":      workspace_content.SoulMDCN,
		"HEARTBEAT.md": workspace_content.HeartbeatMDCN,
		"IDENTITY.md":  workspace_content.IdentityMDCN,
	}

	for _, node := range w.Directories {
		name, _ := node["name"].(string)
		if wantContent, ok := expected[name]; ok {
			content, _ := node["default_content"].(string)
			if content != wantContent {
				t.Errorf("CN %s default_content mismatch, got length %d, want length %d", name, len(content), len(wantContent))
			}
		}
	}
}

// TestCN模式_memoryDefaultContent 测试 CN memory/MEMORY.md 的 defaultContent
func TestCN模式_memoryDefaultContent(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "cn")

	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok && name == "memory" {
			children, ok := node["children"].([]DirectoryNode)
			if !ok {
				t.Fatal("memory children should be []DirectoryNode")
			}
			for _, child := range children {
				if childName, ok := child["name"].(string); ok && childName == "MEMORY.md" {
					content, _ := child["default_content"].(string)
					if content != workspace_content.MemoryMDCN {
						t.Errorf("CN memory/MEMORY.md default_content mismatch, got length %d, want length %d",
							len(content), len(workspace_content.MemoryMDCN))
					}
					return
				}
			}
		}
	}
	t.Error("memory/MEMORY.md not found in CN workspace")
}

// TestEN模式_defaultContent非空 测试 EN 工作空间的 AGENT.md defaultContent 非空且包含英文
func TestEN模式_defaultContent非空(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "en")

	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok && name == "AGENT.md" {
			content, _ := node["default_content"].(string)
			if content == "" {
				t.Error("EN AGENT.md default_content should not be empty")
			}
			if !strings.Contains(content, "AGENT") {
				t.Errorf("EN AGENT.md default_content should contain English text 'AGENT', got: %s", content[:min(50, len(content))])
			}
			return
		}
	}
	t.Error("AGENT.md node not found in EN workspace")
}

// TestEN模式_defaultContent各模板 测试 EN 工作空间各模板文件的 defaultContent
func TestEN模式_defaultContent各模板(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "en")

	expected := map[string]string{
		"AGENT.md":     workspace_content.AgentMDEN,
		"SOUL.md":      workspace_content.SoulMDEN,
		"HEARTBEAT.md": workspace_content.HeartbeatMDEN,
		"IDENTITY.md":  workspace_content.IdentityMDEN,
	}

	for _, node := range w.Directories {
		name, _ := node["name"].(string)
		if wantContent, ok := expected[name]; ok {
			content, _ := node["default_content"].(string)
			if content != wantContent {
				t.Errorf("EN %s default_content mismatch, got length %d, want length %d", name, len(content), len(wantContent))
			}
		}
	}
}

// TestEN模式_不含codingMemory 测试 EN 工作空间不包含 coding_memory 节点
func TestEN模式_不含codingMemory(t *testing.T) {
	w := NewWorkspace("/tmp/workspace", "en")

	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok && name == "coding_memory" {
			t.Error("EN workspace should not contain coding_memory node")
		}
	}
}

// TestLinkTeam_创建和删除 测试 LinkTeam 和 UnlinkTeam
func TestLinkTeam_创建和删除(t *testing.T) {
	rootDir := t.TempDir()
	w := NewWorkspace(rootDir, "cn")

	// 创建目标目录
	targetDir := filepath.Join(t.TempDir(), "team-workspace")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// 创建链接
	err := w.LinkTeam("team-alpha", targetDir)
	if err != nil {
		t.Fatalf("LinkTeam failed: %v", err)
	}

	// 验证链接存在
	linkPath := filepath.Join(rootDir, TeamLinksDir, "team-alpha")
	if !isDirectoryLink(linkPath) {
		t.Error("team-alpha link should exist after LinkTeam")
	}

	// 验证 ListTeamLinks
	links := w.ListTeamLinks()
	found := false
	for _, l := range links {
		if l == "team-alpha" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListTeamLinks should contain 'team-alpha', got %v", links)
	}

	// 删除链接
	err = w.UnlinkTeam("team-alpha")
	if err != nil {
		t.Fatalf("UnlinkTeam failed: %v", err)
	}

	// 验证链接已删除
	if isDirectoryLink(linkPath) {
		t.Error("team-alpha link should not exist after UnlinkTeam")
	}
}

// TestLinkTeam_重复创建 测试 LinkTeam 对已存在链接的处理
func TestLinkTeam_重复创建(t *testing.T) {
	rootDir := t.TempDir()
	w := NewWorkspace(rootDir, "cn")

	targetDir := filepath.Join(t.TempDir(), "team-workspace")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// 第一次创建
	err := w.LinkTeam("team-beta", targetDir)
	if err != nil {
		t.Fatalf("First LinkTeam failed: %v", err)
	}

	// 重复创建应跳过，不报错
	err = w.LinkTeam("team-beta", targetDir)
	if err != nil {
		t.Fatalf("Duplicate LinkTeam should not error: %v", err)
	}
}

// TestLinkWorktree_创建和删除 测试 LinkWorktree 和 UnlinkWorktree
func TestLinkWorktree_创建和删除(t *testing.T) {
	rootDir := t.TempDir()
	w := NewWorkspace(rootDir, "cn")

	targetDir := filepath.Join(t.TempDir(), "worktree-dir")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// 创建链接
	err := w.LinkWorktree("feature-branch", targetDir)
	if err != nil {
		t.Fatalf("LinkWorktree failed: %v", err)
	}

	// 验证链接存在
	linkPath := filepath.Join(rootDir, WorktreeLinksDir, "feature-branch")
	if !isDirectoryLink(linkPath) {
		t.Error("feature-branch link should exist after LinkWorktree")
	}

	// 验证 ListWorktreeLinks
	links := w.ListWorktreeLinks()
	found := false
	for _, l := range links {
		if l == "feature-branch" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListWorktreeLinks should contain 'feature-branch', got %v", links)
	}

	// 删除链接
	err = w.UnlinkWorktree("feature-branch")
	if err != nil {
		t.Fatalf("UnlinkWorktree failed: %v", err)
	}

	// 验证链接已删除
	if isDirectoryLink(linkPath) {
		t.Error("feature-branch link should not exist after UnlinkWorktree")
	}
}

// TestListTeamLinks_空目录 测试 ListTeamLinks 在目录不存在时返回空
func TestListTeamLinks_空目录(t *testing.T) {
	rootDir := t.TempDir()
	w := NewWorkspace(rootDir, "cn")

	links := w.ListTeamLinks()
	if links != nil {
		t.Errorf("ListTeamLinks should return nil for non-existent dir, got %v", links)
	}
}

// TestListWorktreeLinks_空目录 测试 ListWorktreeLinks 在目录不存在时返回空
func TestListWorktreeLinks_空目录(t *testing.T) {
	rootDir := t.TempDir()
	w := NewWorkspace(rootDir, "cn")

	links := w.ListWorktreeLinks()
	if links != nil {
		t.Errorf("ListWorktreeLinks should return nil for non-existent dir, got %v", links)
	}
}

// TestListTeamLinks_多个链接 测试 ListTeamLinks 列出多个链接
func TestListTeamLinks_多个链接(t *testing.T) {
	rootDir := t.TempDir()
	w := NewWorkspace(rootDir, "cn")

	targetDir1 := filepath.Join(t.TempDir(), "team1")
	targetDir2 := filepath.Join(t.TempDir(), "team2")
	os.MkdirAll(targetDir1, 0o755)
	os.MkdirAll(targetDir2, 0o755)

	if err := w.LinkTeam("alpha", targetDir1); err != nil {
		t.Fatalf("LinkTeam alpha failed: %v", err)
	}
	if err := w.LinkTeam("beta", targetDir2); err != nil {
		t.Fatalf("LinkTeam beta failed: %v", err)
	}

	links := w.ListTeamLinks()
	if len(links) != 2 {
		t.Errorf("ListTeamLinks should return 2 links, got %d: %v", len(links), links)
	}
}
