package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindSkillFile 三变体命中与优先序
func TestFindSkillFile(t *testing.T) {
	dir := t.TempDir()
	if FindSkillFile(dir) != "" {
		t.Fatal("空目录应返回空串")
	}
	// 仅小写变体
	write := func(dir, name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dir, "skill.md")
	if got := FindSkillFile(dir); !strings.HasSuffix(got, "skill.md") {
		t.Fatalf("应命中 skill.md: %s", got)
	}
	// 三个都存在时 SKILL.md 优先（数组顺序即优先序）
	write(dir, "Skill.md")
	write(dir, "SKILL.md")
	if got := FindSkillFile(dir); !strings.HasSuffix(got, "SKILL.md") {
		t.Fatalf("应优先 SKILL.md: %s", got)
	}
	// 同名目录不误判（is_file 判定）
	sub := filepath.Join(dir, "SKILL.md_dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// 覆盖目录名场景：新建只含目录形 SKILL.md 的根
	dir2 := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir2, "skill.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if FindSkillFile(dir2) != "" {
		t.Fatal("目录形 skill.md 不应命中")
	}
}

// TestParseFrontmatter frontmatter 边界
func TestParseFrontmatter(t *testing.T) {
	// 不以 --- 开头 → 空 metadata + 原文
	m, body := ParseFrontmatter("hello\nworld", true)
	if len(m) != 0 || body != "hello\nworld" {
		t.Fatalf("无 frontmatter 应回落: %v %q", m, body)
	}
	// 只有开始分隔符无结束 → 空表
	m, body = ParseFrontmatter("---\nname: x", true)
	if len(m) != 0 || body != "---\nname: x" {
		t.Fatalf("无结束分隔符应回落: %v %q", m, body)
	}
	// 正常解析
	content := "---\nname: my-skill\nversion: 1.0\n---\nBODY TEXT"
	m, body = ParseFrontmatter(content, true)
	if m["name"] != "my-skill" || m["version"] != float64(1) {
		t.Fatalf("frontmatter 解析不符: %v", m) // YAML 1.0→float，Python 同为 float
	}
	if body != "BODY TEXT" {
		t.Fatalf("body 不符: %q", body)
	}
	// 结束符带尾随空白（\n---   \n）
	m, _ = ParseFrontmatter("---\nname: x\n---   \nB", true)
	if m["name"] != "x" {
		t.Fatalf("带尾随空白结束符应解析: %v", m)
	}
}

// TestReadSkillFile 完整读取与哈希
func TestReadSkillFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	raw := "---\nname: s\ndescription: d\n---\npara one\n\npara two"
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, body, hash, err := ReadSkillFile(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if meta["name"] != "s" || body != "para one\n\npara two" {
		t.Fatalf("解析不符: %v %q", meta, body)
	}
	if len(hash) != 64 {
		t.Fatalf("sha256 hex 应为 64 字符: %s", hash)
	}
	// 不存在文件 → error
	if _, _, _, err = ReadSkillFile(filepath.Join(dir, "nope.md"), true); err == nil {
		t.Fatal("不存在文件应报错")
	}
}

// TestSHA256File 分块哈希
func TestSHA256File(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	data := make([]byte, 2*1024*1024+7) // 跨 1MiB 分块边界
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SHA256File(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64 {
		t.Fatalf("hash 长度不符: %d", len(got))
	}
	// 与小文件一致性：同内容同哈希
	p2 := filepath.Join(dir, "g.bin")
	if err := os.WriteFile(p2, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got2, _ := SHA256File(p2)
	if got != got2 {
		t.Fatal("同内容哈希应一致")
	}
}

// TestFirstParagraph 空白行分块与限长
func TestFirstParagraph(t *testing.T) {
	// 首个非空块、行内合并
	got := FirstParagraph("first  line\nsecond line\n\nsecond para", 0)
	if got != "first  line second line" { // Python line.strip() 不去行内空格
		t.Fatalf("首段不符: %q", got)
	}
	// 前导空白块跳过
	got = FirstParagraph("\n\n  \nreal para", 0)
	if got != "real para" {
		t.Fatalf("应跳过空白块: %q", got)
	}
	// limit 截断（按 rune 不越界——Python 按字符截断）
	long := strings.Repeat("x", 600)
	got = FirstParagraph(long, 500)
	if len(got) != 500 {
		t.Fatalf("限长不符: %d", len(got))
	}
	// 空输入
	if FirstParagraph("", 0) != "" || FirstParagraph("\n \n", 0) != "" {
		t.Fatal("空输入应返回空串")
	}
}

// TestCleanFirstParagraph 标题与链接剥离
func TestCleanFirstParagraph(t *testing.T) {
	got := CleanFirstParagraph("## My Title\n\nrest", 500)
	if got != "My Title" {
		t.Fatalf("标题剥离不符: %q", got)
	}
	got = CleanFirstParagraph("see [docs](http://x) here", 500)
	if got != "see docs here" {
		t.Fatalf("链接剥离不符: %q", got)
	}
	// 限长
	got = CleanFirstParagraph(strings.Repeat("y", 600), 500)
	if len(got) != 500 {
		t.Fatalf("限长不符: %d", len(got))
	}
	if CleanFirstParagraph("   ", 500) != "" {
		t.Fatal("空白输入应返回空串")
	}
}

// TestReadTextIfExists 存在性判定
func TestReadTextIfExists(t *testing.T) {
	dir := t.TempDir()
	if ReadTextIfExists(filepath.Join(dir, "nope")) != "" {
		t.Fatal("不存在应返回空串")
	}
	// 目录不误读
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ReadTextIfExists(filepath.Join(dir, "d")) != "" {
		t.Fatal("目录应返回空串")
	}
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("中文内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ReadTextIfExists(p) != "中文内容" {
		t.Fatal("读取不符")
	}
}

// TestParseSimpleFrontmatter 简单解析器全分支
func TestParseSimpleFrontmatter(t *testing.T) {
	// 基本标量 + 引号剥壳
	m := parseSimpleFrontmatter("name: 'q'\ndesc: \"d\"\nnum: 3")
	if m["name"] != "q" || m["desc"] != "d" || m["num"] != "3" {
		t.Fatalf("标量解析不符: %v", m)
	}
	// 注释行/缩进行跳过
	m = parseSimpleFrontmatter("# comment\nname: x\n  indented: y")
	if _, ok := m["indented"]; ok {
		t.Fatalf("缩进行应跳过: %v", m)
	}
	if m["name"] != "x" {
		t.Fatalf("注释后解析失败: %v", m)
	}
	// 无冒号且无缩进行并入上一字符串键（缩进行被跳过，见 Python :109-111）
	m = parseSimpleFrontmatter("name: abc\ncontinuation")
	if m["name"] != "abc\ncontinuation" { // Python f"...\n{stripped}".strip() 保换行
		t.Fatalf("续行并入不符: %v", m)
	}
	// 块标量 |：缩进行收集
	m = parseSimpleFrontmatter("text: |\n  line1\n  line2\nnext: v")
	if m["text"] != "line1\nline2" {
		t.Fatalf("块标量不符: %q", m["text"])
	}
	if m["next"] != "v" {
		t.Fatalf("块标量后解析中断: %v", m)
	}
	// 块标量中空行保留
	m = parseSimpleFrontmatter("text: |\n  a\n\n  b")
	if m["text"] != "a\n\nb" {
		t.Fatalf("块标量空行不符: %q", m["text"])
	}
}

// TestSafeLoadFrontmatter YAML 失败回落简单解析
func TestSafeLoadFrontmatter(t *testing.T) {
	// 合法 YAML
	m := safeLoadFrontmatter("name: x\nnested:\n  a: 1", true)
	if m["name"] != "x" {
		t.Fatalf("YAML 解析不符: %v", m)
	}
	// YAML 语法错 → 回落简单解析（: 后无空格等 yaml 报错场景按行兜住）
	m = safeLoadFrontmatter("name: [unclosed", true)
	if m["name"] != "[unclosed" {
		t.Fatalf("回落解析不符: %v", m)
	}
	// fallback_to_simple=false → 空表
	m = safeLoadFrontmatter("name: [unclosed", false)
	if len(m) != 0 {
		t.Fatalf("禁回落应返回空表: %v", m)
	}
}
