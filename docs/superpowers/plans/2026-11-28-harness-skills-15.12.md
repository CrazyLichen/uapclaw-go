# 15.12 skills 与工作区约定 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 1:1 对齐 `harness_providers/skills.py`（portable skills 复制）与 `external_cli_spawn.py` 路径段（cwd/workspace/project_root 三分离 + workspace meta），提前落 15.12。

**Architecture:** `harness_providers/skills.go` 独立落地（provider 接线挂 ⏵ 15.7-15.9）；新包 `internal/agent_teams/runtime/externalcli/` 放三分离纯函数（spawn 主体挂 ⏵ 15.13）；`paths.go` 补 `TeamWorkspaceDir`、schema 补 `BuildContext` 槽位两个前置件缺口。

**Tech Stack:** Go 1.x、gopkg.in/yaml.v3（已在 go.mod）、标准库 os/filepath/sync。

**Spec:** `docs/superpowers/specs/2026-11-28-harness-skills-15.12-design.md`（偏差 D-15.12-a..e 全部经用户 2026-11-28 拍板）

**上游锚点：**
- `agent-core/openjiuwen/harness_providers/skills.py`（221 行）
- `agent-core/openjiuwen/agent_teams/spawn/external_cli_spawn.py:239-298`
- `agent-core/tests/unit_tests/harness_providers/test_skills.py`（175 行，11 用例）

**测试命令约定：** 单包 `go test ./internal/agentcore/harness_providers/ -run Xxx -v`；全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（记忆 testing-build-tags）。注释一律中文（规范 1），声明按规范 2 排序。

---

### Task 1: paths.go 补 TeamWorkspaceDir（前置件缺口①）

**Files:**
- Modify: `internal/agent_teams/paths.go`
- Test: `internal/agent_teams/paths_test.go`（已存在则追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestTeamWorkspaceDir_布局 验证共享工作区默认路径。
// Python: team_workspace_dir (paths.py:168-179)
func TestTeamWorkspaceDir_布局(t *testing.T) {
	ConfigureHome(t.TempDir())
	defer ResetHome()
	got := TeamWorkspaceDir("alpha")
	want := filepath.Join(GetAgentTeamsHome(), "alpha", "team-workspace")
	if got != want {
		t.Fatalf("TeamWorkspaceDir 期望 %s，实际 %s", want, got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agent_teams/ -run TestTeamWorkspaceDir_布局 -v`
Expected: FAIL（undefined: TeamWorkspaceDir）

- [ ] **Step 3: 最小实现**

在 `paths.go` 导出函数区追加（`DefaultTeamMemoryDir` 之后）：

```go
// TeamWorkspaceDir 返回团队共享工作空间根目录。
// Python: team_workspace_dir(team_name)
// 布局：{TeamHome(teamName)}/team-workspace/
func TeamWorkspaceDir(teamName string) string {
	return filepath.Join(TeamHome(teamName), "team-workspace")
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agent_teams/ -run TestTeamWorkspaceDir_布局 -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git status --porcelain   # 确认无他任务脏文件搭车
git add internal/agent_teams/paths.go internal/agent_teams/paths_test.go
git commit -m "feat(15.12): paths 补 TeamWorkspaceDir 对齐 paths.py:168"
```

---

### Task 2: TeamAgentSpec 补 BuildContext 槽位（前置件缺口②）

Python `TeamAgentSpec.build_context` 是 `Optional[Any]`（blueprint.py:645），经 `materialize_build_context` 动态物化；Go 侧 TeamAgentSpec（blueprint.go:51）无此字段。加 `any` 槽位（与 Python Optional[Any] 对齐，不猜具体类型），消费侧（Task 7）按鸭子类型接口断言。

**Files:**
- Modify: `internal/agent_teams/schema/blueprint.go`（TeamAgentSpec 结构体，Workspace 字段附近）
- Test: `internal/agent_teams/schema/blueprint_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestTeamAgentSpec_BuildContext槽位 验证 build_context any 槽位往返。
func TestTeamAgentSpec_BuildContext槽位(t *testing.T) {
	s := `{"agents":{},"team_name":"t","lifecycle":"static","enable_team_plan":false,
		"teammate_mode":"paired","spawn_mode":"manual","leader":{},
		"predefined_members":[],"model_pool_strategy":"static",
		"build_context":{"project_dir":"/tmp/p"}}`
	var spec TeamAgentSpec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m, ok := spec.BuildContext.(map[string]any)
	if !ok || m["project_dir"] != "/tmp/p" {
		t.Fatalf("BuildContext 槽位未按 JSON 原样装载: %#v", spec.BuildContext)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agent_teams/schema/ -run TestTeamAgentSpec_BuildContext槽位 -v`
Expected: FAIL（spec.BuildContext undefined）

- [ ] **Step 3: 最小实现**

blueprint.go `TeamAgentSpec` 的 `Workspace` 字段后追加：

```go
	// BuildContext 构建上下文槽位（物化前为 dict，物化后为 BuildContext 实例）
	// Python: build_context (blueprint.py:645, Optional[Any])
	BuildContext any `json:"build_context,omitempty"`
```

- [ ] **Step 4: 跑测试确认通过 + 包回归**

Run: `go test ./internal/agent_teams/schema/ -v`
Expected: 全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/schema/blueprint.go internal/agent_teams/schema/blueprint_test.go
git commit -m "feat(15.12): TeamAgentSpec 补 build_context any 槽位对齐 blueprint.py:645"
```

---

### Task 3: skills.go — SkillSource 与 NormalizeSkills

**Files:**
- Create: `internal/agentcore/harness_providers/skills.go`
- Create: `internal/agentcore/harness_providers/skills_test.go`
- Modify: `internal/agentcore/harness_providers/doc.go`（文件树追加 skills.go）

- [ ] **Step 1: 写失败测试**

```go
// TestNewSkillSource_构造校验 对齐 __post_init__（skills.py:31-40）。
func TestNewSkillSource_构造校验(t *testing.T) {
	if _, err := NewSkillSource("", "all", nil); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("空 dir 应报错: %v", err)
	}
	if _, err := NewSkillSource("/d", "merge", nil); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("非法 mode 应报错: %v", err)
	}
	if _, err := NewSkillSource("/d", "all", []string{"a", 1}); err == nil || !strings.Contains(err.Error(), "array of names") {
		t.Fatalf("enabled_skills 非字符串数组应报错: %v", err)
	}
	s, err := NewSkillSource("/d", "auto_list", []string{"a", "b"})
	if err != nil || s.Dir != "/d" || s.Mode != "auto_list" || len(s.EnabledSkills) != 2 {
		t.Fatalf("合法构造失败: %v %+v", err, s)
	}
}

// TestNormalizeSkills_三形态 对齐 normalize_skills（skills.py:43-59）。
func TestNormalizeSkills_三形态(t *testing.T) {
	if _, err := NormalizeSkills(nil, "merge"); err == nil {
		t.Fatal("非法 conflict 应报错")
	}
	src, _ := NewSkillSource("/d", "all", nil)
	out, err := NormalizeSkills([]any{src, "/x", map[string]any{"dir": "/y"}}, "skip")
	if err != nil || len(out) != 3 || out[0].Dir != "/d" || out[1].Dir != "/x" || out[2].Dir != "/y" {
		t.Fatalf("三形态归一失败: %v %+v", err, out)
	}
	if _, err := NormalizeSkills([]any{42}, "skip"); err == nil {
		t.Fatal("非法元素应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/harness_providers/ -run "SkillSource|NormalizeSkills" -v`
Expected: FAIL（undefined）

- [ ] **Step 3: 实现 SkillSource + NormalizeSkills + 常量**

新建 `skills.go`（声明顺序按规范 2：结构体→常量→全局变量→导出函数→非导出函数）：

```go
// Package harness_providers — 本文件对齐 openjiuwen/harness_providers/skills.py。
// 把 portable 技能包整目录复制进各 CLI 的发现目录。
package harness_providers

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// SkillSource 一个 manifest 技能目录或含技能目录的技能库。
// Python: SkillSource (skills.py:23-40, frozen dataclass)
type SkillSource struct {
	// Dir 技能包或技能库根目录
	Dir string
	// Mode 取值 all 或 auto_list
	Mode string
	// EnabledSkills auto_list 模式下的技能名白名单（nil 表示全选）
	EnabledSkills []string
}

// NewSkillSource 构造并校验 SkillSource。
// Python: __post_init__（skills.py:31-40）
func NewSkillSource(dir string, mode string, enabledSkills []string) (SkillSource, error) {
	if dir == "" {
		return SkillSource{}, fmt.Errorf("skill dir must be a non-empty path")
	}
	if mode != "all" && mode != "auto_list" {
		return SkillSource{}, fmt.Errorf("skill mode must be all or auto_list")
	}
	// Python: enabled_skills 为 None 表示全选；传数组时元素必须全是字符串
	// （Go 侧 []string 类型系统已保证元素为字符串，保留空参校验语义）
	return SkillSource{Dir: dir, Mode: mode, EnabledSkills: enabledSkills}, nil
}

// ──────────────────────────── 常量 ────────────────────────────

// scanDirs provider 名 → 发现目录（项目相对路径）。
// Python: _SCAN_DIRS (skills.py:19)
var scanDirs = map[string]string{
	"claudecode": ".claude/skills",
	"codex":      ".agents/skills",
	"dsh":        ".dsh/skills",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// copyLock 进程内复制互斥锁。
// Python: _COPY_LOCK (skills.py:20)
var copyLock sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeSkills 冻结 provider 配置，构造期不访问源文件。
// Python: normalize_skills (skills.py:43-59)
func NormalizeSkills(values []any, conflict string) ([]SkillSource, error) {
	if conflict != "skip" && conflict != "replace" && conflict != "append" {
		return nil, fmt.Errorf("skill_conflict must be skip, replace or append")
	}
	result := make([]SkillSource, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case SkillSource:
			result = append(result, v)
		case string:
			result = append(result, SkillSource{Dir: v, Mode: "all"})
		case map[string]any:
			// Python: SkillSource(**dict(value))——走 __post_init__ 校验
			dir, _ := v["dir"].(string)
			mode, _ := v["mode"].(string)
			if mode == "" {
				mode = "all"
			}
			var enabled []string
			if raw, ok := v["enabled_skills"].([]any); ok {
				for _, item := range raw {
					s, _ := item.(string)
					enabled = append(enabled, s)
				}
			}
			src, err := NewSkillSource(dir, mode, enabled)
			if err != nil {
				return nil, err
			}
			result = append(result, src)
		default:
			return nil, fmt.Errorf("skill source must be a directory or manifest skill declaration")
		}
	}
	return result, nil
}
```

（`sync` 加入 import；`doc.go` 文件树追加一行 `skills.go # portable skills 复制`。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/harness_providers/ -run "SkillSource|NormalizeSkills" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness_providers/skills.go internal/agentcore/harness_providers/skills_test.go internal/agentcore/harness_providers/doc.go
git commit -m "feat(15.12): SkillSource/NormalizeSkills 对齐 skills.py:23-59"
```

---

### Task 4: skills.go — front matter 名解析与发现

**Files:**
- Modify: `internal/agentcore/harness_providers/skills.go`
- Modify: `internal/agentcore/harness_providers/skills_test.go`

- [ ] **Step 1: 写失败测试**

```go
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
	return root
}

// TestSkillName_frontMatter 对齐 _name（skills.py:62-80）。
func TestSkillName_frontMatter(t *testing.T) {
	dir := writeBundle(t, filepath.Join(t.TempDir(), "pkg"), "example", "instructions")
	if got := skillName(dir); got != "example" {
		t.Fatalf("期望 example，实际 %s", got)
	}
	// 缺 front matter → 目录名兜底
	bare := filepath.Join(t.TempDir(), "bare")
	if err := os.WriteFile(filepath.Join(bare, "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := skillName(bare); got != "bare" {
		t.Fatalf("无 front matter 应取目录名: %s", got)
	}
	// 非法名（../escape）→ 报错含 "name"
	bad := writeBundle(t, filepath.Join(t.TempDir(), "esc"), "../escape", "x")
	if _, err := parseSkillName(bad); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("非法名应报错: %v", err)
	}
	// 未闭合 front matter → 报错含 "unterminated"
	unclosed := filepath.Join(t.TempDir(), "unclosed")
	os.WriteFile(filepath.Join(unclosed, "SKILL.md"), []byte("---\nname: x\n"), 0o644)
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/harness_providers/ -run "SkillName|DiscoverBundles" -v`
Expected: FAIL

- [ ] **Step 3: 实现**

skills.go 非导出函数区追加：

```go
// skillFrontMatter 解析 SKILL.md 的 YAML front matter，返回元数据。
// 无 front matter 返回 nil map；utf-8-sig BOM 剥离对齐（D-15.12-d）。
// Python: _name 内嵌逻辑（skills.py:63-77）
func skillFrontMatter(dir string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]any{}, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated skill front matter: %s", dir)
	}
	var metadata map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &metadata); err != nil {
		return nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return metadata, nil
}

// parseSkillName 解析技能名并校验格式。
// Python: _name (skills.py:62-80)
func parseSkillName(dir string) (string, error) {
	metadata, err := skillFrontMatter(dir)
	if err != nil {
		return "", err
	}
	name, _ := metadata["name"].(string)
	if name == "" {
		name = filepath.Base(dir)
	}
	// Python: re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", name)
	re := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	if !re.MatchString(name) {
		return "", fmt.Errorf("invalid portable skill name: %q", name)
	}
	return name, nil
}

// skillName parseSkillName 的无错误包装：错误直接上抛由调用方区分。
// Python 侧 _name 抛 ValueError/YAMLError，Go 统一 error。
func skillName(dir string) string {
	name, err := parseSkillName(dir)
	if err != nil {
		panic(err) // 契约：调用前已确认包合法；测试外不触达
	}
	return name
}

// discoverBundles 发现 root 下的技能包。
// root 自含 SKILL.md → 单包；否则排序递归子目录（跳过软链目录）。
// Python: _discover (skills.py:83-90)
func discoverBundles(root string) ([]string, error) {
	if fi, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil && fi.Mode().IsRegular() {
		return []string{root}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		// Python: sorted(root.iterdir())——ReadDir 已按文件名序
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			sub, err := discoverBundles(filepath.Join(root, entry.Name()))
			if err != nil {
				return nil, err
			}
			result = append(result, sub...)
		}
	}
	return result, nil
}
```

（import 追加 `os`/`path/filepath`/`regexp`/`strings`/`sync`/`gopkg.in/yaml.v3`。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/harness_providers/ -run "SkillName|DiscoverBundles" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness_providers/skills.go internal/agentcore/harness_providers/skills_test.go
git commit -m "feat(15.12): 技能名解析与包发现对齐 skills.py:62-90"
```

---

### Task 5: skills.go — 安全校验与重名匹配

**Files:**
- Modify: `internal/agentcore/harness_providers/skills.go`
- Modify: `internal/agentcore/harness_providers/skills_test.go`

- [ ] **Step 1: 写失败测试**

```go
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
	if got := nextCopyName(scan, "example"); got != "example" {
		t.Fatalf("无冲突应原样返回: %s", got)
	}
	writeBundle(t, filepath.Join(scan, "example"), "example", "x")
	if got := nextCopyName(scan, "example"); got != "example-2" {
		t.Fatalf("期望 example-2，实际 %s", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/harness_providers/ -run "ValidateTree|MatchSkills|NextCopyName" -v`
Expected: FAIL

- [ ] **Step 3: 实现**

```go
// resolvePath EvalSymlinks 组合解析（D-15.12-a：替代 Python resolve(strict=True)）。
// 不存在 → error（等价 strict 抛 FileNotFoundError）。
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// isRelative 判断 child 是否位于 parent 之下（均须已解析）。
// Python: Path.is_relative_to
func isRelative(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// validateTree 校验技能树：软链逃逸/环/非常规文件三类拒绝。
// Python: _validate_tree (skills.py:93-103)
func validateTree(path, root string, ancestors map[string]bool) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	if !isRelative(root, resolved) {
		return fmt.Errorf("skill symlink escapes its bundle: %s", path)
	}
	fi, err := os.Stat(resolved) // Stat 跟随软链：Python resolved.is_dir() 同义
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if ancestors[resolved] {
			return fmt.Errorf("cyclic skill directory: %s", path)
		}
		next := make(map[string]bool, len(ancestors)+1)
		for k, v := range ancestors {
			next[k] = v
		}
		next[resolved] = true
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := validateTree(filepath.Join(resolved, entry.Name()), root, next); err != nil {
				return err
			}
		}
		return nil
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("skill contains a non-regular file: %s", path)
	}
	return nil
}

// casefoldName Go 侧简化折叠（D-15.12-b：strings.EqualFold 替代 str.casefold）。
func casefoldName(s string) string { return strings.ToLower(s) }

// matchSkills 在 scan 目录中找与 name 匹配的既有技能目录（可能多个）。
// 目录名与 front-matter 名双轨比对；front-matter 解析失败静默跳过（用目录名）。
// Python: _matches (skills.py:106-117)
func matchSkills(scan, name string) []string {
	entries, err := os.ReadDir(scan)
	if err != nil {
		return nil
	}
	var matches []string
	for _, entry := range entries {
		existing := entry.Name()
		if entry.Type()&os.ModeSymlink == 0 && entry.IsDir() {
			if fi, err := os.Stat(filepath.Join(scan, entry.Name(), "SKILL.md")); err == nil && fi.Mode().IsRegular() {
				if parsed, err := parseSkillName(filepath.Join(scan, entry.Name())); err == nil {
					existing = parsed
				}
			}
		}
		if casefoldName(name) == casefoldName(entry.Name()) || casefoldName(name) == casefoldName(existing) {
			matches = append(matches, filepath.Join(scan, entry.Name()))
		}
	}
	return matches
}

// nextCopyName append 策略下的下一个可用名：-2 起步，总长截到 128。
// Python: _next_copy_name (skills.py:120-127)
func nextCopyName(scan, name string) string {
	for index := 2; ; index++ {
		suffix := fmt.Sprintf("-%d", index)
		candidate := name
		if len(candidate) > 128-len(suffix) {
			// Python 按字符截断；技能名 ASCII 限集下字节==字符
			candidate = candidate[:128-len(suffix)]
		}
		candidate += suffix
		if len(matchSkills(scan, candidate)) == 0 {
			return candidate
		}
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/harness_providers/ -run "ValidateTree|MatchSkills|NextCopyName" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness_providers/skills.go internal/agentcore/harness_providers/skills_test.go
git commit -m "feat(15.12): 安全校验/重名匹配/改名序列对齐 skills.py:93-127"
```

---

### Task 6: skills.go — renameCopiedSkill 与 InstallSkills 主体

**Files:**
- Modify: `internal/agentcore/harness_providers/skills.go`
- Modify: `internal/agentcore/harness_providers/skills_test.go`

- [ ] **Step 1: 写失败测试（核心 11 用例中的主体）**

```go
// TestInstallSkills_完整包与三策略 对齐 test_complete_bundle_and_conflict_policies（test_skills.py:24-52）。
func TestInstallSkills_完整包与三策略(t *testing.T) {
	for _, tc := range []struct{ provider, relative string }{
		{"claudecode", ".claude/skills"}, {"codex", ".agents/skills"}, {"dsh", ".dsh/skills"},
	} {
		tmp := t.TempDir()
		source := writeBundle(t, filepath.Join(tmp, "source"), "example", "instructions")
		os.WriteFile(filepath.Join(source, ".hidden"), []byte("\x00\x01resource"), 0o644)
		os.MkdirAll(filepath.Join(source, "scripts"), 0o755)
		os.WriteFile(filepath.Join(source, "scripts", "run.sh"), []byte("echo resource"), 0o755)
		project := filepath.Join(tmp, "project")
		os.MkdirAll(project, 0o755)
		target := filepath.Join(project, tc.relative, "example")

		installed, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "skip", "")
		if err != nil || len(installed) != 1 || installed[0] != target {
			t.Fatalf("[%s] 首装失败: %v %v", tc.provider, err, installed)
		}
		if data, _ := os.ReadFile(filepath.Join(target, ".hidden")); string(data) != "\x00\x01resource" {
			t.Fatalf("隐藏文件未复制")
		}
		// skip：二装返回空且不动旧内容
		os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: example\n---\nupdated\n"), 0o644)
		os.WriteFile(filepath.Join(target, "stale"), []byte("old"), 0o644)
		installed, err = InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "skip", "")
		if err != nil || len(installed) != 0 {
			t.Fatalf("[%s] skip 二装应空: %v %v", tc.provider, err, installed)
		}
		// replace：更新且 stale 消失
		if _, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, tc.provider, project, "replace", ""); err != nil {
			t.Fatalf("[%s] replace 失败: %v", tc.provider, err)
		}
		if _, err := os.Stat(filepath.Join(target, "stale")); !os.IsNotExist(err) {
			t.Fatalf("[%s] replace 后 stale 应消失", tc.provider)
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
	}
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
	sources, err := NormalizeSkills([]any{map[string]any{"dir": library, "enabled_skills": []any{"selected"}, "mode": "auto_list"}}, "skip")
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
	os.Chdir(current)
	defer os.Chdir(orig)

	installed, err := InstallSkills([]SkillSource{{Dir: source, Mode: "all"}}, "codex", "", "append", "")
	if err != nil || len(installed) != 1 || installed[0] != filepath.Join(current, ".agents", "skills", "example") {
		t.Fatalf("cwd 缺省应取进程 cwd: %v %v", err, installed)
	}
	// 非法 conflict
	if _, err := NormalizeSkills(nil, "merge"); err == nil {
		t.Fatal("非法 conflict 应报错")
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
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatal("源不应被改动")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/harness_providers/ -run InstallSkills -v`
Expected: FAIL（undefined: InstallSkills/copyFn/renameFn）

- [ ] **Step 3: 实现 renameCopiedSkill + InstallSkills**

skills.go 追加（导出函数区 `InstallSkills`；非导出区 `renameCopiedSkill`；全局变量区 `copyFn`/`renameFn` 注入点——对齐 Python monkeypatch 面，生产代码走默认值）：

```go
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// copyFn 目录复制注入点（测试替换对齐 Python monkeypatch shutil.copytree）
	copyFn = copyTree
	// renameFn 重命名注入点（测试替换对齐 Python monkeypatch Path.rename）
	renameFn = os.Rename
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InstallSkills 运行时启动前把完整技能包复制进发现目录，保留既有项目技能。
// replace 在发现目录外暂存，rename 失败回滚；复制产物在 harness 停止后留存。
// 内部软链物化；逃逸/环软链复制前拒绝。
// Python: install_skills (skills.py:146-221)——步骤逐行对齐。
func InstallSkills(sources []SkillSource, provider string, cwd string, conflict string, scanDir string) ([]string, error) {
	// Python: if not sources: return ()
	if len(sources) == 0 {
		return nil, nil
	}
	if conflict != "skip" && conflict != "replace" && conflict != "append" {
		return nil, fmt.Errorf("skill_conflict must be skip, replace or append")
	}
	relative := scanDir
	if relative == "" {
		relative = scanDirs[provider]
	}
	if relative == "" {
		return nil, fmt.Errorf("provider %q declares no skill discovery directory", provider)
	}
	projectRaw := cwd
	if projectRaw == "" {
		projectRaw, _ = os.Getwd()
	}
	project, err := resolvePath(projectRaw) // Python: expanduser().resolve(strict=True)
	if err != nil {
		return nil, err
	}
	scan := filepath.Join(project, relative)
	var planned []struct {
		name   string
		source string
	}
	for _, source := range sources {
		root, err := resolvePath(source.Dir)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("skill source is not a directory: %s", root)
		}
		bundles, err := discoverBundles(root)
		if err != nil {
			return nil, err
		}
		if len(bundles) == 0 {
			return nil, fmt.Errorf("no SKILL.md bundles found in %s", root)
		}
		for _, bundle := range bundles {
			name, err := parseSkillName(bundle)
			if err != nil {
				return nil, err
			}
			if len(source.EnabledSkills) == 0 || containsString(source.EnabledSkills, name) {
				planned = append(planned, struct{ name, source string }{name, bundle})
			}
		}
	}
	var installed []string
	// Python: with _COPY_LOCK —— 多成员同项目启动互斥
	copyLock.Lock()
	defer copyLock.Unlock()
	if scanResolved, err := resolvePath(scan); err == nil && !isRelative(project, scanResolved) {
		return nil, fmt.Errorf("skill discovery directory escapes the project")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(scan, 0o755); err != nil { // parents=True, exist_ok=True
		return nil, err
	}
	for _, item := range planned {
		matches := matchSkills(scan, item.name)
		if len(matches) > 0 && conflict == "skip" {
			continue
		}
		if len(matches) > 1 && conflict == "replace" {
			return nil, fmt.Errorf("multiple existing directories declare skill %q", item.name)
		}
		copyName := item.name
		if len(matches) > 0 && conflict == "append" {
			copyName = nextCopyName(scan, item.name)
		}
		var destination string
		if len(matches) > 0 && conflict == "replace" {
			destination = matches[0]
		} else {
			destination = filepath.Join(scan, copyName)
		}
		// Python: target_location = destination.parent.resolve() / destination.name
		parentResolved, err := resolvePath(filepath.Dir(destination))
		if err != nil {
			return nil, err
		}
		targetLocation := filepath.Join(parentResolved, filepath.Base(destination))
		sourceResolved, err := resolvePath(item.source)
		if err != nil {
			return nil, err
		}
		if sourceResolved == targetLocation {
			continue // 自复制 no-op
		}
		if isRelative(sourceResolved, targetLocation) || isRelative(targetLocation, sourceResolved) {
			return nil, fmt.Errorf("skill source and destination must not contain each other")
		}
		if err := validateTree(item.source, item.source, nil); err != nil {
			return nil, err
		}
		// Python: TemporaryDirectory(prefix=".openjiuwen-skill-", dir=project)
		// D-15.12-c：os.MkdirTemp + defer 手动清理
		stage, err := os.MkdirTemp(project, ".openjiuwen-skill-*")
		if err != nil {
			return nil, err
		}
		func() {
			defer os.RemoveAll(stage)
			copied := filepath.Join(stage, "new")
			backup := filepath.Join(stage, "previous")
			if err := copyFn(item.source, copied); err != nil {
				return // 暂存失败直接上抛（defer 已清理 stage）
			}
			if copyName != item.name {
				if err := renameCopiedSkill(copied, copyName); err != nil {
					return
				}
			}
			existed := false
			if _, err := os.Lstat(destination); err == nil {
				existed = true
			}
			if existed && conflict == "append" {
				return // Python: FileExistsError —— 用 error 变量承载后统一上抛
			}
			if existed {
				if err := renameFn(destination, backup); err != nil {
					return
				}
			}
			if err := renameFn(copied, destination); err != nil {
				if existed {
					_ = renameFn(backup, destination) // Python: backup.rename(destination) 回滚
				}
				return
			}
			installed = append(installed, destination)
			stageCommitted = true
		}()
		// 内层闭包失败需上抛：实现时以 named error 变量改写（见下方实现注记①）
	}
	return installed, nil
}
```

**实现注记①（必读）**：上面骨架为可读性把闭包错误写成 `return`，实际实现必须用命名 error 变量上抛（Python 的 `raise` 语义）：

```go
		for _, item := range planned {
			// ...同上前置分支...
			var installErr error
			stage, err := os.MkdirTemp(project, ".openjiuwen-skill-*")
			if err != nil {
				return nil, err
			}
			committed := false
			func() {
				defer func() {
					if !committed {
						os.RemoveAll(stage)
					}
				}()
				copied := filepath.Join(stage, "new")
				backup := filepath.Join(stage, "previous")
				if err := copyFn(item.source, copied); err != nil {
					installErr = err
					return
				}
				if copyName != item.name {
					if err := renameCopiedSkill(copied, copyName); err != nil {
						installErr = err
						return
					}
				}
				existed := lstatExists(destination)
				if existed && conflict == "append" {
					installErr = fmt.Errorf("skill destination appeared during copy: %s", destination)
					return
				}
				if existed {
					if err := renameFn(destination, backup); err != nil {
						installErr = err
						return
					}
				}
				if err := renameFn(copied, destination); err != nil {
					if existed {
						_ = renameFn(backup, destination)
					}
					installErr = err
					return
				}
				committed = true
				installed = append(installed, destination)
			}()
			if installErr != nil {
				return nil, installErr
			}
		}
```

（`lstatExists(path)` 为非导出助手：Lstat err==nil → true；`stageCommitted` 骨架变量删除，以 `committed` 局部变量为准。copyTree 实现：`os.CopyFS`（Go 1.23+）或 filepath.WalkDir 手写——手写版对软链 `os.Symlink(filepath.EvalSymlinks(target), dst)` 物化、对普通文件保留 mode（`os.Chmod` 复制 FileInfo.Mode()），对齐 `shutil.copytree(symlinks=False)` 语义。）

```go
// renameCopiedSkill 把复制出的包 SKILL.md front matter 的 name 改为 copyName。
// 无 front matter 直接返回（目录名即技能名）。
// Python: _rename_copied_skill (skills.py:130-143)
func renameCopiedSkill(bundleDir string, name string) error {
	docPath := filepath.Join(bundleDir, "SKILL.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		return err
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF")
	// Python: splitlines(keepends=True) —— 保留行尾
	lines := splitLinesKeepEnds(text)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil // Python 的 next(...) 在此不可达（_name 已校验）；防御返回
	}
	var metadata map[string]any
	body := strings.Join(stripEnds(lines[1:end]), "\n")
	if err := yaml.Unmarshal([]byte(body), &metadata); err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["name"] = name
	var buf strings.Builder
	buf.WriteString("---\n")
	out, err := yaml.Marshal(metadata) // yaml.v3 默认不转义 Unicode、保插入序（D-15.12-d）
	if err != nil {
		return err
	}
	buf.Write(out)
	buf.WriteString("---\n")
	for _, line := range lines[end+1:] {
		buf.WriteString(line)
	}
	return os.WriteFile(docPath, []byte(buf.String()), 0o644)
}

// splitLinesKeepEnds 保留行尾的按行切分（Python splitlines(keepends=True) 语义，13.18 惯例）。
func splitLinesKeepEnds(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// stripEnds 去掉各行行尾换行。
func stripEnds(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.TrimRight(line, "\r\n")
	}
	return out
}

// containsString 判含。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// lstatExists Lstat 判存在（软链也算存在，对齐 destination.exists() or is_symlink()）。
func lstatExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// copyTree 目录复制（shutil.copytree(symlinks=False) 语义：软链物化、保留 mode）。
func copyTree(src, dst string) error {
	return copyTreeWalk(src, dst)
}

// copyTreeWalk copyTree 的递归实现。
func copyTreeWalk(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		// 内部软链物化：读真实目标内容复制（symlinks=False）
		real, err := resolvePath(src)
		if err != nil {
			return err
		}
		return copyTreeWalk(real, dst)
	}
	if !fi.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, fi.Mode().Perm()); err != nil {
			return err
		}
		return os.Chmod(dst, fi.Mode().Perm())
	}
	if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTreeWalk(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: 跑全部 skills 测试**

Run: `go test ./internal/agentcore/harness_providers/ -run "Skill|Install" -v`
Expected: 全 PASS；`go test ./internal/agentcore/harness_providers/ -cover` 覆盖率 ≥85%

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness_providers/skills.go internal/agentcore/harness_providers/skills_test.go
git commit -m "feat(15.12): InstallSkills 主体对齐 skills.py:130-221（三策略/暂存回滚/软链物化）"
```

---

### Task 7: externalcli 包 — 三分离纯函数

**Files:**
- Create: `internal/agent_teams/runtime/externalcli/doc.go`
- Create: `internal/agent_teams/runtime/externalcli/paths.go`
- Create: `internal/agent_teams/runtime/externalcli/paths_test.go`

- [ ] **Step 1: 写失败测试**

```go
package externalcli

// TestPathValue 对齐 _path_value（external_cli_spawn.py:239-243）。
func TestPathValue(t *testing.T) {
	if pathValue("") != "" || pathValue("   ") != "" {
		t.Fatal("空/空白应返回空串")
	}
	if got := pathValue("/tmp/x"); got != "/tmp/x" {
		t.Fatalf("期望 /tmp/x，实际 %s", got)
	}
}

// TestSamePath 对齐 _same_path（246-247）：normcase+normpath。
func TestSamePath(t *testing.T) {
	if !samePath("/tmp/x/", "/tmp/x") || !samePath("/tmp/./x", "/tmp/x") {
		t.Fatal("normpath 等价应成立")
	}
	if samePath("/tmp/x", "/tmp/y") {
		t.Fatal("不同路径不应相等")
	}
}

// TestAppendExtraDir 对齐 _append_extra_dir（250-257）：nil 跳过、与 cwd 相同跳过、重复跳过。
func TestAppendExtraDir(t *testing.T) {
	var result []string
	appendExtraDir(&result, nil, "/c")
	appendExtraDir(&result, "/c", "/c")     // 与 cwd 相同
	appendExtraDir(&result, "/a", "/c")
	appendExtraDir(&result, "/a", "/c")     // 重复
	appendExtraDir(&result, "/b", "/c")
	if strings.Join(result, ",") != "/a,/b" {
		t.Fatalf("期望 /a,/b，实际 %v", result)
	}
}

// TestTeamWorkspacePath 对齐 _team_workspace_path（260-264）。
func TestTeamWorkspacePath(t *testing.T) {
	// 配置了 root_path → 直接用
	spec := &schema.TeamAgentSpec{Workspace: &teamworkspace.TeamWorkspaceConfig{Enabled: true, RootPath: "/ws/root"}}
	if got := teamWorkspacePath(spec, "alpha"); got != "/ws/root" {
		t.Fatalf("期望 /ws/root，实际 %s", got)
	}
	// 未配置 → 默认拼出
	spec = &schema.TeamAgentSpec{}
	agentteams.ConfigureHome(t.TempDir())
	defer agentteams.ResetHome()
	if got := teamWorkspacePath(spec, "alpha"); got != agentteams.TeamWorkspaceDir("alpha") {
		t.Fatalf("默认应转调 TeamWorkspaceDir: %s", got)
	}
}

// TestBuildContextExtractors 对齐 _build_context_project_dir/_build_context_team_outputs_dir（267-278）。
// 物化实例走 GetProjectDir/GetTeamOutputsDir 接口断言（Python getattr 语义）。
func TestBuildContextExtractors(t *testing.T) {
	pd := "/p"
	bc := &harnessschema.BuildContext{ProjectDir: &pd, TeamOutputsDir: "/o"}
	spec := &schema.TeamAgentSpec{BuildContext: bc}
	if got := buildContextProjectDir(spec); got != "/p" {
		t.Fatalf("ProjectDir 提取失败: %s", got)
	}
	if got := buildContextTeamOutputsDir(spec); got != "/o" {
		t.Fatalf("TeamOutputsDir 提取失败: %s", got)
	}
	spec.BuildContext = nil
	if buildContextProjectDir(spec) != "" || buildContextTeamOutputsDir(spec) != "" {
		t.Fatal("nil build_context 应返回空串")
	}
}

// TestResolveExternalPaths 对齐 _resolve_external_paths（281-298）：
// cwd 四级顺序 + extra_dirs 全量去重。
func TestResolveExternalPaths(t *testing.T) {
	agentteams.ConfigureHome(t.TempDir())
	defer agentteams.ResetHome()
	// 显式 cwd 优先；worktree/project/workspace 进 extra_dirs
	pd := "/proj"
	spec := &schema.TeamAgentSpec{
		Workspace: &teamworkspace.TeamWorkspaceConfig{Enabled: true, RootPath: "/ws"},
		BuildContext: &harnessschema.BuildContext{ProjectDir: &pd},
	}
	ctx := &schema.TeamRuntimeContext{WorktreePath: "/wt"}
	cwd, extra := ResolveExternalPaths(spec, ctx, "/explicit", "alpha")
	if cwd != "/explicit" {
		t.Fatalf("cwd 应取显式值: %s", cwd)
	}
	if strings.Join(extra, ",") != "/wt,/proj,/ws" {
		t.Fatalf("extra_dirs 顺序错误: %v", extra)
	}
	// 无显式 cwd → worktree 顶上
	cwd, _ = ResolveExternalPaths(spec, ctx, "", "alpha")
	if cwd != "/wt" {
		t.Fatalf("cwd 应回落 worktree: %s", cwd)
	}
	// 全空 → 落 team_workspace
	specEmpty := &schema.TeamAgentSpec{}
	ctxEmpty := &schema.TeamRuntimeContext{}
	cwd, extra = ResolveExternalPaths(specEmpty, ctxEmpty, "", "alpha")
	if cwd != agentteams.TeamWorkspaceDir("alpha") || len(extra) != 0 {
		t.Fatalf("全空应落 workspace 且 extra 为空: %s %v", cwd, extra)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agent_teams/runtime/externalcli/ -v`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现 doc.go + paths.go**

`doc.go`（规则 1 模板，注明 15.13 续写）：

```go
// Package externalcli 提供外部 CLI 团队成员的运行时支撑。
//
// 本包对应 Python openjiuwen/agent_teams/spawn/external_cli_spawn.py。
// 按用户 2026-11-28 拍板（方案①），15.12 先落路径段（cwd 三分离与
// workspace meta 提取）；spawn 主体（成员生命周期、team-join 描述符、
// CLI 进程启动）挂 ⏵ 15.13 续写本包（偏差 D-15.12-e：Python 单文件
// 由两批填充同一 Go 包）。
//
// 文件目录：
//
//	externalcli/
//	├── doc.go           # 包文档
//	└── paths.go        # cwd 三分离与 workspace meta 路径段
//
// 对应 Python 代码：openjiuwen/agent_teams/spawn/external_cli_spawn.py（行 239-298 段）
package externalcli
```

`paths.go`：

```go
package externalcli

import (
	"os"
	"path/filepath"
	"strings"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveExternalPaths 解析外部成员的 cwd 与额外可访问目录。
// cwd 选择顺序：显式配置 → worktree → 项目目录 → 团队工作区；
// 四个候选中非 cwd 者（去重）作为额外目录（如 Claude --add-dir）。
// Python: _resolve_external_paths (external_cli_spawn.py:281-298)
func ResolveExternalPaths(spec *schema.TeamAgentSpec, ctx *schema.TeamRuntimeContext,
	configuredCwd string, teamName string) (string, []string) {
	explicitCwd := pathValue(configuredCwd)
	worktreePath := pathValue(ctx.WorktreePath)
	projectDir := buildContextProjectDir(spec)
	teamWorkspace := teamWorkspacePath(spec, teamName)
	cwd := explicitCwd
	if cwd == "" {
		cwd = worktreePath
	}
	if cwd == "" {
		cwd = projectDir
	}
	if cwd == "" {
		cwd = teamWorkspace
	}
	var extraDirs []string
	for _, path := range []string{explicitCwd, worktreePath, projectDir, teamWorkspace} {
		appendExtraDir(&extraDirs, path, cwd)
	}
	return cwd, extraDirs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pathValue 非字符串化空值归一：空白/空串返回空串。
// Python: _path_value (239-243)
func pathValue(value string) string {
	stripped := strings.TrimSpace(value)
	return stripped
}

// samePath 路径等价比较（normcase+normpath 语义）。
// Python: _same_path (246-247)
func samePath(left, right string) bool {
	return normalizePath(left) == normalizePath(right)
}

// normalizePath normpath+normcase 的 Go 等价（Clean + 小写化，Windows 大小写不敏感语义保留）。
func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// appendExtraDir 追加额外目录：nil/与 cwd 相同/重复者跳过。
// Python: _append_extra_dir (250-257)
func appendExtraDir(result *[]string, path string, cwd string) {
	if path == "" {
		return
	}
	if cwd != "" && samePath(path, cwd) {
		return
	}
	for _, existing := range *result {
		if samePath(path, existing) {
			return
		}
	}
	*result = append(*result, path)
}

// teamWorkspacePath 团队共享工作区绝对路径：配置 root_path 优先，否则默认布局。
// Python: _team_workspace_path (260-264)
func teamWorkspacePath(spec *schema.TeamAgentSpec, teamName string) string {
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		return spec.Workspace.RootPath
	}
	return agentteams.TeamWorkspaceDir(teamName)
}

// buildContextProjectDir 从 build_context 提取项目目录。
// Python: _build_context_project_dir (267-271)；getattr 鸭子类型 → 接口断言
func buildContextProjectDir(spec *schema.TeamAgentSpec) string {
	if spec.BuildContext == nil {
		return ""
	}
	if bc, ok := spec.BuildContext.(*harnessschema.BuildContext); ok {
		return bc.GetProjectDir()
	}
	return ""
}

// buildContextTeamOutputsDir 从 build_context 提取团队产物目录。
// Python: _build_context_team_outputs_dir (274-278)
func buildContextTeamOutputsDir(spec *schema.TeamAgentSpec) string {
	if spec.BuildContext == nil {
		return ""
	}
	if bc, ok := spec.BuildContext.(*harnessschema.BuildContext); ok {
		return bc.GetTeamOutputsDir()
	}
	return ""
}
```

（import 实际以编译为准；`os` 若未用到删除。Python `_path_value` 作用于 `object`，Go 侧签名收窄为 string——`ResolveExternalPaths` 入参 `configuredCwd string` 已对齐调用面语义，登记偏差 D-15.12-f 详见 Task 9 spec 回填。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agent_teams/runtime/externalcli/ -v`
Expected: 全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/runtime/externalcli/
git commit -m "feat(15.12): externalcli 三分离路径段对齐 external_cli_spawn.py:239-298"
```

---

### Task 8: 全量回归 + 计划表回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（15.12 行 ☐→✅；15.7/15.8/15.9 行追加 ⏵ 接线回填标记；15.13 行追加 ⏵ spawn 主体/tracker 回填标记）

- [ ] **Step 1: 编译 + vet**

Run: `go build ./... && go vet ./...`
Expected: 零错误

- [ ] **Step 2: 全量回归（CI 同款标签）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL（295+ 包）

- [ ] **Step 3: 覆盖率核查**

Run: `go test ./internal/agentcore/harness_providers/ ./internal/agent_teams/runtime/externalcli/ ./internal/agent_teams/ -cover`
Expected: 各包 ≥85%

- [ ] **Step 4: 锚点核查（spec §7）**

Run:
```bash
grep -rn "InstallSkills" internal/ --include="*.go" | grep -v harness_providers
grep -rn "ResolveExternalPaths" internal/ --include="*.go" | grep -v externalcli
```
Expected: 均零命中（无越界消费者）

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md 并提交**

15.12 行 ☐→✅ 并按既有行文风格写交付摘要（含偏差 D-15.12-a..f 与 ⏵ 标记）；提交：

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 15.12 skills 与工作区约定交付（skills.go+externalcli 路径段，6 偏差）"
```

- [ ] **Step 6: 推送**

```bash
git push
```

---

## 自查记录

- **Spec 覆盖**：spec §3.1（skills.go 9 符号）→ Task 3-6；§3.2（externalcli 7 符号）→ Task 7；前置件缺口（TeamWorkspaceDir/BuildContext）→ Task 1-2；§5 测试面 11 用例 → Task 6（含 2 并发 + 2 注入用例）；§6 删除清单空 → 无对应任务；§7 锚点 → Task 8 Step 4。
- **占位符扫描**：无 TBD/TODO；Task 6 注记①给出了闭包错误的最终写法，骨架代码仅作阅读引导。
- **类型一致性**：`copyFn`/`renameFn` 在 Task 6 全局变量区定义、测试同文件引用；`ResolveExternalPaths` 返回 `(string, []string)` 与测试一致；`teamWorkspacePath`/`buildContextProjectDir` 为非导出、测试同包可见。
- **新增偏差 D-15.12-f**（`_path_value` 参数从 object 收窄为 string）：Task 8 回填 spec 时补入 spec §4 表。
