package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildSkillFixture 构造技能目录夹具。
//
// 结构：root/{SKILL.md, designer/SKILL.md, assets/logo.png, output/x.txt}
func buildSkillFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# demo"), 0o644); err != nil {
		t.Fatalf("写 SKILL.md 失败: %v", err)
	}
	for _, dir := range []string{"designer", "assets", "output"} {
		if err := os.MkdirAll(filepath.Join(root, dir, "sub"), 0o755); err != nil {
			t.Fatalf("建目录 %s 失败: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "designer", "SKILL.md"), []byte("# sub"), 0o644); err != nil {
		t.Fatalf("写子 SKILL.md 失败: %v", err)
	}
	return root
}

// TestBuildSkillDirectoryAsciiTree 目录树渲染
func TestBuildSkillDirectoryAsciiTree(t *testing.T) {
	root := buildSkillFixture(t)
	tree, truncated, errText := buildSkillDirectoryAsciiTree(root)
	if len(tree) != 1 {
		t.Fatalf("期望单元素树文本, 实际 %v", tree)
	}
	if truncated {
		t.Error("小目录不应截断")
	}
	if errText != "" {
		t.Errorf("不应有错误, 实际 %q", errText)
	}
	if !strings.Contains(tree[0], filepath.Base(root)+"/") {
		t.Errorf("树首行应为根名, 实际 %q", tree[0])
	}
	if !strings.Contains(tree[0], "designer/") {
		t.Error("应包含 designer/")
	}
	if strings.Contains(tree[0], "assets/") {
		t.Error("应跳过 assets/")
	}
	if strings.Contains(tree[0], "output/") {
		t.Error("应跳过 output/")
	}
}

// TestBuildSkillDirectoryAsciiTree_非目录 错误分支
func TestBuildSkillDirectoryAsciiTree_非目录(t *testing.T) {
	root := buildSkillFixture(t)
	filePath := filepath.Join(root, "SKILL.md")
	if _, _, errText := buildSkillDirectoryAsciiTree(filePath); errText == "" {
		t.Error("文件路径应返回错误文案")
	}
	if _, _, errText := buildSkillDirectoryAsciiTree(filepath.Join(root, "missing")); errText == "" {
		t.Error("不存在路径应返回错误文案")
	}
}

// TestCollectNestedSkillNames 嵌套子技能收集
func TestCollectNestedSkillNames(t *testing.T) {
	root := buildSkillFixture(t)
	names, truncated := collectNestedSkillNames(root)
	if truncated {
		t.Error("不应截断")
	}
	if len(names) != 1 || names[0] != "designer" {
		t.Errorf("期望 [designer], 实际 %v", names)
	}
	if _, truncated := collectNestedSkillNames(filepath.Join(root, "missing")); truncated {
		t.Error("不存在路径不应报截断")
	}
}

// TestIsSafeRelativeFilePath 路径安全校验
func TestIsSafeRelativeFilePath(t *testing.T) {
	safe := []string{"SKILL.md", "designer/SKILL.md", "a/b/c.md"}
	unsafe := []string{"/etc/passwd", `C:\Windows\win.ini`, "../outside.md", "a/../../b.md", `a\..\b.md`}
	for _, p := range safe {
		if !isSafeRelativeFilePath(p) {
			t.Errorf("%q 应判为安全", p)
		}
	}
	for _, p := range unsafe {
		if isSafeRelativeFilePath(p) {
			t.Errorf("%q 应判为不安全", p)
		}
	}
}

// TestSkillLayoutMetadata 布局元数据字段
func TestSkillLayoutMetadata(t *testing.T) {
	root := buildSkillFixture(t)
	meta := skillLayoutMetadata(root)
	if _, ok := meta["directory_tree"]; !ok {
		t.Error("应包含 directory_tree")
	}
	names, ok := meta["discovered_skill_names"].([]string)
	if !ok || len(names) != 1 || names[0] != "designer" {
		t.Errorf("discovered_skill_names 期望 [designer], 实际 %v", meta["discovered_skill_names"])
	}
	if meta["directory_tree_truncated"] != false {
		t.Error("directory_tree_truncated 期望 false")
	}
}

// TestFormatLayoutAppendixForModel 附录渲染
func TestFormatLayoutAppendixForModel(t *testing.T) {
	layout := map[string]any{
		"directory_tree":                   []string{"demo/\n└── designer/"},
		"directory_tree_truncated":         true,
		"discovered_skill_names":           []string{"designer"},
		"discovered_skill_names_truncated": false,
	}
	text := formatLayoutAppendixForModel(layout, "demo")
	if !strings.Contains(text, "## Directory layout") {
		t.Error("应包含目录布局段")
	}
	if !strings.Contains(text, "…(truncated)") {
		t.Error("应包含截断标记")
	}
	if !strings.Contains(text, "## Nested skills") {
		t.Error("应包含嵌套技能段")
	}
	if !strings.Contains(text, `skill_tool(skill_name="demo", relative_file_path="designer/SKILL.md")`) {
		t.Errorf("示例应带父技能名, 实际 %s", text)
	}

	// 无技能名时用占位；空布局返回空串
	if got := formatLayoutAppendixForModel(map[string]any{}, "demo"); got != "" {
		t.Errorf("空布局期望空串, 实际 %q", got)
	}
	placeholder := formatLayoutAppendixForModel(map[string]any{
		"discovered_skill_names": []string{"designer"},
	}, "  ")
	if !strings.Contains(placeholder, "<this skill>") {
		t.Errorf("空技能名应使用占位, 实际 %s", placeholder)
	}
}

// TestFirstTreeBlock 树文本块解析
func TestFirstTreeBlock(t *testing.T) {
	if got := firstTreeBlock([]string{" demo/ "}); got != "demo/" {
		t.Errorf("期望 demo/, 实际 %q", got)
	}
	if got := firstTreeBlock(" demo/ "); got != "demo/" {
		t.Errorf("期望 demo/, 实际 %q", got)
	}
	if got := firstTreeBlock([]string{}); got != "" {
		t.Errorf("空列表期望空串, 实际 %q", got)
	}
	if got := firstTreeBlock(123); got != "" {
		t.Errorf("非文本期望空串, 实际 %q", got)
	}
}

// TestApplySkillToolMarkdownImagesHint 媒体提示注入与去重
func TestApplySkillToolMarkdownImagesHint(t *testing.T) {
	body := "![shot](assets/a.png)"
	once := applySkillToolMarkdownImagesHint(body, true)
	if !strings.HasPrefix(once, skillToolMarkdownImagesHint) {
		t.Error("应前置读图提示")
	}
	twice := applySkillToolMarkdownImagesHint(once, true)
	if twice != once {
		t.Error("重复注入应被剥离后重建，结果应一致")
	}
	vision := applySkillToolMarkdownImagesHint(body, false)
	if !strings.HasPrefix(vision, skillToolMarkdownImagesVisionHint) {
		t.Error("读图关闭时应使用 vision 提示")
	}
	if got := applySkillToolMarkdownImagesHint("no media", true); got != "no media" {
		t.Errorf("无媒体不应注入提示, 实际 %q", got)
	}
	if !skillMarkdownHasMedia("[v](a.mp4)") || skillMarkdownHasMedia("plain") {
		t.Error("skillMarkdownHasMedia 判定错误")
	}
}

func TestCollectNestedSkillNames_超过上限截断(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < skillNestedNamesMax+2; i++ {
		dir := filepath.Join(root, fmt.Sprintf("sub%03d", i))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# sub"), 0o644))
	}

	names, truncated := collectNestedSkillNames(root)
	assert.Len(t, names, skillNestedNamesMax)
	assert.True(t, truncated)
}

func TestCollectNestedSkillNames_非目录返回空(t *testing.T) {
	file := filepath.Join(t.TempDir(), "note.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	names, truncated := collectNestedSkillNames(file)
	assert.Empty(t, names)
	assert.False(t, truncated)
}

func TestCollectNestedSkillNames_跳过指向根外的符号链接(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("# outside"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked")))

	names, _ := collectNestedSkillNames(root)
	assert.NotContains(t, names, "linked", "指向技能根之外的链接不得算作子技能")
}
