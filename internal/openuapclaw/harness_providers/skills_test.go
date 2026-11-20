package harness_providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestNewSkillSource_构造校验 对齐 __post_init__（skills.py:31-40）。
func TestNewSkillSource_构造校验(t *testing.T) {
	if _, err := NewSkillSource("", "all", nil); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("空 dir 应报错: %v", err)
	}
	if _, err := NewSkillSource("/d", "merge", nil); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("非法 mode 应报错: %v", err)
	}
	s, err := NewSkillSource("/d", "auto_list", []string{"a", "b"})
	if err != nil || s.Dir != "/d" || s.Mode != "auto_list" || len(s.EnabledSkills) != 2 {
		t.Fatalf("合法构造失败: %v %+v", err, s)
	}
}

// TestNormalizeSkills_收窄 对齐 normalize_skills（skills.py:43-59）的 conflict 校验；
// 输入面收窄为 []SkillSource（偏差 D-15.12-g，用户 2026-11-28 拍板）。
func TestNormalizeSkills_收窄(t *testing.T) {
	if _, err := NormalizeSkills(nil, "merge"); err == nil {
		t.Fatal("非法 conflict 应报错")
	}
	src, _ := NewSkillSource("/d", "all", nil)
	out, err := NormalizeSkills([]SkillSource{src}, "skip")
	if err != nil || len(out) != 1 || out[0].Dir != "/d" {
		t.Fatalf("归一失败: %v %+v", err, out)
	}
}

// writeBundle 造一个最小技能包（对齐 test_skills.py bundle 助手）。
func writeBundle(t *testing.T, root string, name, text string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: " + name + "\ndescription: Test skill\n---\n" + text + "\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	// Python bundle 助手还建 scripts/run.sh（0o755）与 .hidden 二进制
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "run.sh"), []byte("echo resource"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte{0x00, 0x01, 'r', 'e', 's', 'o', 'u', 'r', 'c', 'e'}, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestSkillName_frontMatter 对齐 _name（skills.py:62-80）。
func TestSkillName_frontMatter(t *testing.T) {
	dir := writeBundle(t, filepath.Join(t.TempDir(), "pkg"), "example", "instructions")
	if got, err := parseSkillName(dir); err != nil || got != "example" {
		t.Fatalf("期望 example，实际 %s err=%v", got, err)
	}
	// 缺 front matter → 目录名兜底
	bare := filepath.Join(t.TempDir(), "bare")
	os.MkdirAll(bare, 0o755)
	if err := os.WriteFile(filepath.Join(bare, "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := parseSkillName(bare); err != nil || got != "bare" {
		t.Fatalf("无 front matter 应取目录名: %s err=%v", got, err)
	}
	// 非法名（../escape）→ 报错含 "name"
	bad := writeBundle(t, filepath.Join(t.TempDir(), "esc"), "../escape", "x")
	if _, err := parseSkillName(bad); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("非法名应报错: %v", err)
	}
	// 未闭合 front matter → 报错含 "unterminated"
	unclosed := filepath.Join(t.TempDir(), "unclosed")
	os.MkdirAll(unclosed, 0o755)
	if err := os.WriteFile(filepath.Join(unclosed, "SKILL.md"), []byte("---\nname: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSkillName(unclosed); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("未闭合应报错: %v", err)
	}
}

// TestDiscoverBundles 对齐 _discover（skills.py:83-90）：根含 SKILL.md 即单包，否则排序递归。
func TestDiscoverBundles(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, filepath.Join(root, "b"), "b", "x")
	writeBundle(t, filepath.Join(root, "a", "nested"), "a", "x")
	bundles, err := discoverBundles(root)
	if err != nil || len(bundles) != 2 {
		t.Fatalf("发现失败: %v %v", err, bundles)
	}
	// 排序序：a/nested 先于 b
	if !strings.HasSuffix(bundles[0], filepath.Join("a", "nested")) {
		t.Fatalf("排序错误: %v", bundles)
	}
}

// TestValidateTree_三类拒绝 对齐 _validate_tree（skills.py:93-103）。
func TestValidateTree_三类拒绝(t *testing.T) {
	tmp := t.TempDir()
	root := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	// 合法树不报错
	if err := validateTree(root, root, nil); err != nil {
		t.Fatalf("合法树误报: %v", err)
	}
	// 逃逸软链 → "escapes"
	outside := filepath.Join(tmp, "outside")
	os.WriteFile(outside, []byte("o"), 0o644)
	os.Symlink(outside, filepath.Join(root, "escape"))
	if err := validateTree(root, root, nil); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("逃逸应报错: %v", err)
	}
	// 内部软链 → 合法（物化在 copy 阶段）
	os.Remove(filepath.Join(root, "escape"))
	os.Symlink(filepath.Join(root, "SKILL.md"), filepath.Join(root, "reference"))
	if err := validateTree(root, root, nil); err != nil {
		t.Fatalf("内部软链误报: %v", err)
	}
	// 环 → "cyclic"
	os.Remove(filepath.Join(root, "reference"))
	os.Symlink(root, filepath.Join(root, "self"))
	if err := validateTree(root, root, nil); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("环应报错: %v", err)
	}
}

// TestMatchSkills_双轨名 对齐 _matches（skills.py:106-117）：目录名与 front-matter 名 casefold 双轨。
func TestMatchSkills_双轨名(t *testing.T) {
	scan := t.TempDir()
	writeBundle(t, filepath.Join(scan, "legacy-folder"), "selected", "keep")
	plain := filepath.Join(scan, "Example")
	os.MkdirAll(plain, 0o755) // 无 SKILL.md → 用目录名比对
	m := matchSkills(scan, "SELECTED")
	if len(m) != 1 || !strings.HasSuffix(m[0], "legacy-folder") {
		t.Fatalf("front-matter 名匹配失败: %v", m)
	}
	m = matchSkills(scan, "example")
	if len(m) != 1 || !strings.HasSuffix(m[0], "Example") {
		t.Fatalf("目录名匹配失败: %v", m)
	}
}

// TestNextCopyName 对齐 _next_copy_name（skills.py:120-127）：-2 起步、128 截断。
func TestNextCopyName(t *testing.T) {
	scan := t.TempDir()
	// Python _next_copy_name 无条件从 -2 起步（skills.py:120-127 循环结构）
	if got := nextCopyName(scan, "example"); got != "example-2" {
		t.Fatalf("空 scan 应返回 example-2: %s", got)
	}
	writeBundle(t, filepath.Join(scan, "example"), "example", "x")
	if got := nextCopyName(scan, "example"); got != "example-2" {
		t.Fatalf("期望 example-2，实际 %s", got)
	}
}

// TestInstallSkills_完整包与三策略 对齐 test_complete_bundle_and_conflict_policies（test_skills.py:24-52）。
func TestInstallSkills_完整包与三策略(t *testing.T) {
	for _, tc := range []struct{ provider, relative string }{
		{"claudecode", ".claude/skills"}, {"codex", ".agents/skills"}, {"dsh", ".dsh/skills"},
	} {
		tmp := t.TempDir()
		source := writeBundle(t, filepath.Join(tmp, "source"), "example", "instructions")
		project := filepath.Join(tmp, "project")
		os.MkdirAll(project, 0o755)
		target := filepath.Join(project, tc.relative, "example")

		installed, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "skip", "")
		if err != nil || len(installed) != 1 || installed[0] != target {
			t.Fatalf("[%s] 首装失败: %v %v", tc.provider, err, installed)
		}
		if data, _ := os.ReadFile(filepath.Join(target, ".hidden")); string(data) != "\x00\x01resource" {
			t.Fatalf("[%s] 隐藏文件未复制", tc.provider)
		}
		if fi, err := os.Stat(filepath.Join(target, "scripts", "run.sh")); err != nil || fi.Mode()&0o111 == 0 {
			t.Fatalf("[%s] run.sh 应保留可执行位", tc.provider)
		}
		// skip：二装返回空且不动旧内容
		os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: example\ndescription: Test skill\n---\nupdated\n"), 0o644)
		os.WriteFile(filepath.Join(target, "stale"), []byte("old"), 0o644)
		installed, err = InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "skip", "")
		if err != nil || len(installed) != 0 {
			t.Fatalf("[%s] skip 二装应空: %v %v", tc.provider, err, installed)
		}
		if data, _ := os.ReadFile(filepath.Join(target, "SKILL.md")); strings.Contains(string(data), "updated") {
			t.Fatalf("[%s] skip 不应更新内容", tc.provider)
		}
		// replace：更新且 stale 消失
		if _, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "replace", ""); err != nil {
			t.Fatalf("[%s] replace 失败: %v", tc.provider, err)
		}
		if data, _ := os.ReadFile(filepath.Join(target, "SKILL.md")); !strings.Contains(string(data), "updated") {
			t.Fatalf("[%s] replace 应更新内容", tc.provider)
		}
		if _, err := os.Stat(filepath.Join(target, "stale")); !os.IsNotExist(err) {
			t.Fatalf("[%s] replace 后 stale 应消失", tc.provider)
		}
		if data, _ := os.ReadFile(filepath.Join(target, "SKILL.md")); string(data) != mustRead(t, filepath.Join(source, "SKILL.md")) {
			t.Fatalf("[%s] replace 后目标应与源一致", tc.provider)
		}
		// append：example-2 / example-3 且 front matter name 改写
		second := filepath.Join(project, tc.relative, "example-2")
		third := filepath.Join(project, tc.relative, "example-3")
		for _, want := range []string{second, third} {
			installed, err = InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "append", "")
			if err != nil || len(installed) != 1 || installed[0] != want {
				t.Fatalf("[%s] append 失败: %v %v", tc.provider, err, installed)
			}
		}
		if data, _ := os.ReadFile(filepath.Join(second, "SKILL.md")); !strings.Contains(string(data), "name: example-2") {
			t.Fatalf("[%s] append 未改写名: %s", tc.provider, data)
		}
		if data, _ := os.ReadFile(filepath.Join(third, "SKILL.md")); !strings.Contains(string(data), "name: example-3") {
			t.Fatalf("[%s] append 未改写名(3): %s", tc.provider, data)
		}
		if data, _ := os.ReadFile(filepath.Join(second, ".hidden")); string(data) != "\x00\x01resource" {
			t.Fatalf("[%s] append 未复制隐藏文件", tc.provider)
		}
		entries, _ := filepath.Glob(filepath.Join(project, ".openjiuwen-skill-*"))
		if len(entries) != 0 {
			t.Fatalf("[%s] 暂存目录应清理: %v", tc.provider, entries)
		}
	}
}

// mustRead 测试读文件助手。
func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestInstallSkills_copy失败保留旧技能 对齐 test_copy_failure_keeps_existing_skill（test_skills.py:70-79）。
// Python monkeypatch shutil.copytree → Go copyFn 函数变量注入。
func TestInstallSkills_copy失败保留旧技能(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	existing := writeBundle(t, filepath.Join(tmp, "project", ".dsh", "skills", "example"), "example", "original")
	orig := copyFn
	copyFn = func(src, dst string) error { return fmt.Errorf("disk full") }
	defer func() { copyFn = orig }()
	_, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "dsh", filepath.Join(tmp, "project"), "replace", "")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("copy 失败应上抛: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(existing, "SKILL.md")); !strings.Contains(string(data), "original") {
		t.Fatal("copy 失败后旧技能应保留")
	}
}

// TestInstallSkills_rename失败回滚 对齐 test_failed_install_rename_restores_previous_directory（test_skills.py:142-154）。
// Python monkeypatch Path.rename（仅拦 "new"）→ Go renameFn 注入。
func TestInstallSkills_rename失败回滚(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	project := filepath.Join(tmp, "project")
	existing := writeBundle(t, filepath.Join(project, ".agents", "skills", "example"), "example", "original")
	orig := renameFn
	renameFn = func(old, new string) error {
		if filepath.Base(old) == "new" {
			return fmt.Errorf("rename failed")
		}
		return os.Rename(old, new)
	}
	defer func() { renameFn = orig }()
	_, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", project, "replace", "")
	if err == nil || !strings.Contains(err.Error(), "rename failed") {
		t.Fatalf("rename 失败应上抛: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(existing, "SKILL.md")); !strings.Contains(string(data), "original") {
		t.Fatal("回滚后旧技能应保留")
	}
	entries, _ := filepath.Glob(filepath.Join(project, ".openjiuwen-skill-*"))
	if len(entries) != 0 {
		t.Fatalf("暂存目录应清理: %v", entries)
	}
}

// TestInstallSkills_逃逸拒绝与自复制 对齐 test_symlink_escape_is_rejected_and_self_copy_is_noop（test_skills.py:82-92）。
func TestInstallSkills_逃逸拒绝与自复制(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	outside := filepath.Join(tmp, "outside")
	os.WriteFile(outside, []byte("o"), 0o644)
	os.Symlink(outside, filepath.Join(source, "escape"))
	if _, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "dsh", tmp, "skip", ""); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("逃逸应报错: %v", err)
	}
	os.Remove(filepath.Join(source, "escape")) // Python: (source / "escape").unlink()
	// 移除逃逸后正常安装；再对目标自复制（replace）→ no-op 返回空
	target := filepath.Join(tmp, ".dsh", "skills", "example")
	installed, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "dsh", tmp, "skip", "")
	if err != nil || len(installed) != 1 {
		t.Fatalf("正常安装失败: %v", err)
	}
	installed, err = InstallSkills([]SkillSource{{Dir: target, Mode: "all"}}, "dsh", tmp, "replace", "")
	if err != nil || len(installed) != 0 {
		t.Fatalf("自复制应 no-op: %v %v", err, installed)
	}
}

// TestInstallSkills_库选择与frontMatter冲突 对齐 test_library_selection_and_frontmatter_name_collision（test_skills.py:55-67）。
func TestInstallSkills_库选择与frontMatter冲突(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeBundle(t, filepath.Join(library, "different-folder"), "selected", "x")
	writeBundle(t, filepath.Join(library, "other"), "other", "x")
	project := filepath.Join(tmp, "project")
	os.MkdirAll(project, 0o755)
	existing := writeBundle(t, filepath.Join(project, ".agents", "skills", "legacy-folder"), "selected", "keep")
	enabled := "selected"
	sources, err := NormalizeSkills([]SkillSource{{Dir: library, Mode: "auto_list", EnabledSkills: []string{enabled}}}, "skip")
	if err != nil {
		t.Fatal(err)
	}
	installed, err := InstallSkills(sources, "codex", project, "skip", "")
	if err != nil || len(installed) != 0 {
		t.Fatalf("skip 命中既有名应跳过: %v %v", err, installed)
	}
	if _, err := InstallSkills(sources, "codex", project, "replace", ""); err != nil {
		t.Fatalf("replace 失败: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(existing, "SKILL.md")); strings.Contains(string(data), "keep") {
		t.Fatal("replace 后应更新")
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "other")); !os.IsNotExist(err) {
		t.Fatal("未选中的 other 不应被复制")
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "selected")); !os.IsNotExist(err) {
		t.Fatal("不应按 front-matter 名另建目录")
	}
}

// TestInstallSkills_并发skip 与 TestInstallSkills_并发append
// 对齐 test_concurrent_*（test_skills.py:95-117）：goroutine + WaitGroup 对齐线程池。
func TestInstallSkills_并发skip(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	results := make([][]string, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", tmp, "skip", "")
		}(i)
	}
	wg.Wait()
	total := 0
	for _, r := range results {
		total += len(r)
	}
	if total != 1 {
		t.Fatalf("并发 skip 总安装数应 1，实际 %d", total)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents", "skills", "example", "scripts", "run.sh")); err != nil {
		t.Fatalf("技能应完整落盘: %v", err)
	}
}

func TestInstallSkills_并发append(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	names := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			installed, _ := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", tmp, "append", "")
			mu.Lock()
			for _, p := range installed {
				names[filepath.Base(p)] = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if !names["example"] || !names["example-2"] || len(names) != 2 {
		t.Fatalf("并发 append 应保留两份: %v", names)
	}
}

// TestInstallSkills_cwd缺省与非法配置 对齐 test_missing_cwd_uses_current_directory（120-132）
// 与 test_invalid_configuration_and_skill_name（134-139）。
func TestInstallSkills_cwd缺省与非法配置(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	current := filepath.Join(tmp, "current")
	os.MkdirAll(current, 0o755)
	orig, _ := os.Getwd()
	if err := os.Chdir(current); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	installed, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", "", "append", "")
	if err != nil || len(installed) != 1 || installed[0] != filepath.Join(current, ".agents", "skills", "example") {
		t.Fatalf("cwd 缺省应取进程 cwd: %v %v", err, installed)
	}
	installed, err = InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", "", "append", "")
	if err != nil || len(installed) != 1 || installed[0] != filepath.Join(current, ".agents", "skills", "example-2") {
		t.Fatalf("cwd 缺省二装应 example-2: %v %v", err, installed)
	}
	// 非法 conflict
	if _, err := NormalizeSkills(nil, "merge"); err == nil || !strings.Contains(err.Error(), "skill_conflict") {
		t.Fatalf("非法 conflict 应报错: %v", err)
	}
	// 非法技能名
	bad := writeBundle(t, filepath.Join(tmp, "source2"), "../escape", "x")
	if _, err := InstallSkills([]SkillSource{{Dir: bad, Mode: "all"}}, "claudecode", tmp, "skip", ""); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("非法名应报错: %v", err)
	}
}

// TestInstallSkills_内部软链物化 对齐 test_internal_symlinks_are_materialized（test_skills.py:157-163）。
func TestInstallSkills_内部软链物化(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	os.Symlink(filepath.Join(source, ".hidden"), filepath.Join(source, "reference"))
	if _, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", tmp, "skip", ""); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, ".agents", "skills", "example", "reference")
	fi, err := os.Lstat(target)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("内部软链应物化: %v", err)
	}
	want, _ := os.ReadFile(filepath.Join(source, ".hidden"))
	got, _ := os.ReadFile(target)
	if string(got) != string(want) {
		t.Fatal("物化内容应一致")
	}
}

// TestInstallSkills_replace软链目标不动源 对齐 test_replace_destination_symlink_copies_files_without_mutating_source（test_skills.py:166-175）。
func TestInstallSkills_replace软链目标不动源(t *testing.T) {
	tmp := t.TempDir()
	source := writeBundle(t, filepath.Join(tmp, "source"), "example", "x")
	scan := filepath.Join(tmp, ".agents", "skills")
	os.MkdirAll(scan, 0o755)
	destination := filepath.Join(scan, "example")
	os.Symlink(source, destination)
	if _, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", tmp, "replace", ""); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(destination)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		t.Fatalf("目标应变为真实目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "scripts", "run.sh")); err != nil {
		t.Fatalf("目标应含完整文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatal("源不应被改动")
	}
}
