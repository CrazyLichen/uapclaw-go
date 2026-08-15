# SkillManager 补全 + SkillToolkit 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补全 SkillManager 的 ClawHub/TeamSkillsHub 方法和本地辅助方法，实现 SkillToolkit 工具集并回填 DeepAdapter。

**Architecture:** 4 阶段从底向上：本地辅助方法 → ClawHub → TeamSkillsHub → SkillToolkit。SkillManager 在单文件中补全，SkillToolkit 放在 `swarm/agents/harness/tools/` 新包中。HTTP 客户端使用 net/http 标准库。

**Tech Stack:** Go 标准库 net/http, archive/zip, crypto/sha256, encoding/json

---

## Task 1: 本地辅助方法 — getLocalSkills

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:1165` (在 addLocalSkill 之后)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写测试**

在 `skill_manager_test.go` 中添加：

```go
// TestGetLocalSkills_空状态 验证空状态返回空切片
func TestGetLocalSkills_空状态(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result := sm.getLocalSkills()
	if result == nil {
		t.Error("应返回空切片，而非 nil")
	}
	if len(result) != 0 {
		t.Errorf("期望 0 个，实际 %d 个", len(result))
	}
}

// TestGetLocalSkills_有数据 验证读取已添加的本地技能
func TestGetLocalSkills_有数据(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.addLocalSkill(map[string]any{"name": "test-skill", "source": "local"})
	result := sm.getLocalSkills()
	if len(result) != 1 {
		t.Fatalf("期望 1 个，实际 %d 个", len(result))
	}
	if toString(result[0]["name"]) != "test-skill" {
		t.Errorf("name = %q, want %q", toString(result[0]["name"]), "test-skill")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestGetLocalSkills -v -count=1`
Expected: 编译失败（getLocalSkills 未定义）

- [ ] **Step 3: 实现 getLocalSkills**

在 `skill_manager.go` 的 `addLocalSkill` 方法之后添加：

```go
// getLocalSkills 返回本地技能列表
// 对应 Python: SkillManager.get_local_skills()
func (sm *SkillManager) getLocalSkills() []map[string]any {
	raw, ok := sm.state["local_skills"]
	if !ok {
		return []map[string]any{}
	}
	list, ok := toSliceOfAny(raw)
	if !ok {
		return []map[string]any{}
	}
	var result []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestGetLocalSkills -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 getLocalSkills 本地辅助方法"
```

---

## Task 2: 本地辅助方法 — getSkillMeta

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:1230` (在 resolveLocalSkillDir 之后)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写测试**

```go
// TestGetSkillMeta_正常解析 验证正常解析 SKILL.md
func TestGetSkillMeta_正常解析(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建技能目录和 SKILL.md
	skillDir := filepath.Join(tmpDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := sm.getSkillMeta("test-skill")
	if meta == nil {
		t.Fatal("meta 不应为 nil")
	}
	if toString(meta["name"]) != "test-skill" {
		t.Errorf("name = %q, want %q", toString(meta["name"]), "test-skill")
	}
	if toString(meta["skill_dir"]) != skillDir {
		t.Errorf("skill_dir = %q, want %q", toString(meta["skill_dir"]), skillDir)
	}
	if toString(meta["skill_file"]) == "" {
		t.Error("skill_file 不应为空")
	}
}

// TestGetSkillMeta_不存在 验证不存在的技能返回 nil
func TestGetSkillMeta_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	meta := sm.getSkillMeta("nonexistent")
	if meta != nil {
		t.Error("不存在的技能应返回 nil")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestGetSkillMeta -v -count=1`
Expected: 编译失败（getSkillMeta 未定义）

- [ ] **Step 3: 实现 getSkillMeta**

在 `skill_manager.go` 的 `resolveLocalSkillDir` 方法之后添加：

```go
// getSkillMeta 从本地技能目录读取解析后的 SKILL.md 元数据
// 对应 Python: SkillManager.get_skill_meta(skill_name)
func (sm *SkillManager) getSkillMeta(name string) map[string]any {
	skillDir := sm.resolveLocalSkillDir(name)
	if skillDir == "" {
		return nil
	}
	skillFile := sm.tryFindSkillFile(skillDir)
	if skillFile == "" {
		return nil
	}
	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return nil
	}
	meta["skill_dir"] = skillDir
	meta["skill_file"] = skillFile
	return meta
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestGetSkillMeta -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 getSkillMeta 本地辅助方法"
```

---

## Task 3: 本地辅助方法 — isBuiltinSkill + getBuiltinSkillsDir

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:1519-1524` (修改 getBuiltinSkillsDir)
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go` (在 getBuiltinSkillsDir 之后添加 isBuiltinSkill)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写测试**

```go
// TestIsBuiltinSkill_非内置 验证非内置技能返回 false
func TestIsBuiltinSkill_非内置(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	if sm.isBuiltinSkill("some-skill") {
		t.Error("非内置技能应返回 false")
	}
}

// TestIsBuiltinSkill_空名 验证空名称返回 false
func TestIsBuiltinSkill_空名(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	if sm.isBuiltinSkill("") {
		t.Error("空名应返回 false")
	}
}

// TestIsBuiltinSkill_内置技能 验证内置技能返回 true
func TestIsBuiltinSkill_内置技能(t *testing.T) {
	tmpDir := t.TempDir()
	builtinDir := filepath.Join(tmpDir, "builtin_skills")
	skillDir := filepath.Join(builtinDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 设置环境变量指向内置目录
	t.Setenv("BUILTIN_SKILLS_DIR", builtinDir)

	// 在 skillsDir 下创建同名的符号链接
	sm := NewSkillManager(tmpDir)
	linkPath := filepath.Join(sm.skillsDir, "my-skill")
	if err := os.Symlink(skillDir, linkPath); err != nil {
		t.Fatal(err)
	}

	if !sm.isBuiltinSkill("my-skill") {
		t.Error("内置技能应返回 true")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestIsBuiltinSkill -v -count=1`
Expected: 编译失败（isBuiltinSkill 未定义）

- [ ] **Step 3: 修改 getBuiltinSkillsDir 并实现 isBuiltinSkill**

替换 `getBuiltinSkillsDir` 函数：

```go
// getBuiltinSkillsDir 获取内置技能目录
// 对应 Python: get_builtin_skills_dir()
func getBuiltinSkillsDir() string {
	if dir := os.Getenv("BUILTIN_SKILLS_DIR"); dir != "" {
		return dir
	}
	// 后续补充：从 package root 解析 resources/agent/workspace/skills
	return ""
}
```

在 `getBuiltinSkillsDir` 之后添加：

```go
// isBuiltinSkill 判断技能是否为内置技能
// 对应 Python: SkillManager.is_builtin_skill(skill_name)
// 比较用户 skills 目录中的技能与内置目录中的技能是否指向同一物理路径
func (sm *SkillManager) isBuiltinSkill(name string) bool {
	if name == "" {
		return false
	}
	builtinDir := getBuiltinSkillsDir()
	if builtinDir == "" {
		return false
	}
	// 安全校验路径名称
	if _, err := safePathName(name, "skill"); err != nil {
		return false
	}
	// 用户 skills 目录下的技能路径
	userSkillPath := filepath.Join(sm.skillsDir, name)
	userInfo, err := os.Stat(userSkillPath)
	if err != nil {
		return false
	}
	// 内置目录下的技能路径
	builtinSkillPath := filepath.Join(builtinDir, name)
	builtinInfo, err := os.Stat(builtinSkillPath)
	if err != nil {
		return false
	}
	return os.SameFile(userInfo, builtinInfo)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestIsBuiltinSkill -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 isBuiltinSkill 和 getBuiltinSkillsDir 本地辅助方法"
```

---

## Task 4: ClawHub — HandleSkillsClawhubSearch

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:953-957` (替换 stub)
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go` (添加 import net/http, net/url, io, strconv)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写测试**

```go
// TestHandleSkillsClawhubSearch_正常 验证正常搜索
func TestHandleSkillsClawhubSearch_正常(t *testing.T) {
	// 创建模拟 HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("应包含 Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"results":[{"slug":"test-skill","displayName":"Test Skill","summary":"A test skill","version":"1.0.0","updatedAt":1234567890}]}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.setClawhubToken("test-token")

	result, err := sm.HandleSkillsClawhubSearch(context.Background(), map[string]any{
		"q":     "test",
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	skills, ok := result["skills"].([]map[string]any)
	if !ok || len(skills) != 1 {
		t.Fatalf("应返回 1 个技能, got %v", result["skills"])
	}
	if toString(skills[0]["slug"]) != "test-skill" {
		t.Errorf("slug = %q, want %q", toString(skills[0]["slug"]), "test-skill")
	}
}

// TestHandleSkillsClawhubSearch_无Token 验证无 token 返回错误
func TestHandleSkillsClawhubSearch_无Token(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubSearch(context.Background(), map[string]any{
		"q": "test",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无 token 应返回 success=false")
	}
}

// TestHandleSkillsClawhubSearch_缺关键词 验证缺少搜索关键词
func TestHandleSkillsClawhubSearch_缺关键词(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.setClawhubToken("test-token")

	result, err := sm.HandleSkillsClawhubSearch(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少关键词应返回 success=false")
	}
}
```

注意：需要在测试文件顶部添加 import：`"net/http", "net/http/httptest"`

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsClawhubSearch_正常 -v -count=1`
Expected: 测试失败（当前 stub 返回 errNotImplemented）

- [ ] **Step 3: 实现 HandleSkillsClawhubSearch**

替换 `skill_manager.go` 中的 stub：

```go
// HandleSkillsClawhubSearch 从 ClawHub 搜索技能
// 对应 Python: SkillManager.handle_skills_clawhub_search(params)
func (sm *SkillManager) HandleSkillsClawhubSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	query := trimSpace(toString(params["q"]))
	if query == "" {
		return map[string]any{"success": false, "detail": "缺少参数: q"}, nil
	}

	token := sm.getClawhubToken()
	if token == "" {
		return map[string]any{"success": false, "detail": "ClawHub token 未配置", "detail_key": "skills.clawhub.errors.noToken"}, nil
	}

	limit := 10
	if v, ok := params["limit"]; ok {
		if n, err := strconv.Atoi(toString(v)); err == nil && n > 0 {
			limit = n
		}
	}

	// 构建请求 URL
	reqURL, _ := url.Parse("https://clawhub.ai/api/v1/search")
	q := reqURL.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "网络请求失败: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("ClawHub API 返回状态码: %d", resp.StatusCode)}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return map[string]any{"success": false, "detail": "JSON 解析失败: " + err.Error()}, nil
	}

	// 映射结果字段
	rawResults, ok := toSliceOfAny(data["results"])
	if !ok {
		return map[string]any{"success": true, "query": query, "count": 0, "skills": []map[string]any{}}, nil
	}

	var skills []map[string]any
	for _, item := range rawResults {
		if m, ok := item.(map[string]any); ok {
			skills = append(skills, map[string]any{
				"slug":         toString(m["slug"]),
				"display_name": toString(m["displayName"]),
				"summary":      toString(m["summary"]),
				"version":      toString(m["version"]),
				"updated_at":   m["updatedAt"],
			})
		}
	}

	return map[string]any{"success": true, "query": query, "count": len(skills), "skills": skills}, nil
}
```

确保 import 中添加了：`"net/http"`, `"net/url"`, `"strconv"`, `"io"`

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsClawhubSearch -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 HandleSkillsClawhubSearch"
```

---

## Task 5: ClawHub — HandleSkillsClawhubDownload

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:959-963` (替换 stub)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 先实现 safeExtractZIPBytesToDir 辅助方法**

在 `skill_manager.go` 的非导出函数区添加：

```go
// safeExtractZIPBytesToDir 安全解压 ZIP 字节到目标目录（防 Zip Slip）
// 对应 Python: SkillManager._safe_extract_zip_bytes_to_dir(zip_bytes, dest_dir)
func safeExtractZIPBytesToDir(zipBytes []byte, destDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("ZIP 读取失败: %w", err)
	}

	for _, f := range reader.File {
		// Zip Slip 防护：检查解压路径不超出目标目录
		targetPath := filepath.Join(destDir, f.Name)
		relPath, err := filepath.Rel(destDir, targetPath)
		if err != nil {
			return fmt.Errorf("路径解析失败: %w", err)
		}
		if strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("Zip Slip 检测：路径 %q 超出目标目录", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(targetPath, data, f.Mode()); err != nil {
			return err
		}
	}
	return nil
}
```

确保 import 中添加了：`"archive/zip"`, `"bytes"`, `"io"`

- [ ] **Step 2: 写测试**

```go
// TestHandleSkillsClawhubDownload_正常 验证正常下载安装
func TestHandleSkillsClawhubDownload_正常(t *testing.T) {
	// 创建一个合法的 ZIP 响应（含 SKILL.md）
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
	sm := NewSkillManager(tmpDir)
	sm.setClawhubToken("test-token")

	result, err := sm.HandleSkillsClawhubDownload(context.Background(), map[string]any{
		"slug": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestSafeExtractZIPBytesToDir_ZipSlip 验证 Zip Slip 防护
func TestSafeExtractZIPBytesToDir_ZipSlip(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("../../../etc/passwd")
	fmt.Fprint(w, "malicious")
	zipWriter.Close()

	tmpDir := t.TempDir()
	err := safeExtractZIPBytesToDir(zipBuf.Bytes(), tmpDir)
	if err == nil {
		t.Error("应检测到 Zip Slip 并返回错误")
	}
}

// TestHandleSkillsClawhubDownload_无Token 验证无 token 返回错误
func TestHandleSkillsClawhubDownload_无Token(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubDownload(context.Background(), map[string]any{
		"slug": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无 token 应返回 success=false")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run "TestHandleSkillsClawhubDownload|TestSafeExtractZIPBytesToDir" -v -count=1`
Expected: 测试失败

- [ ] **Step 4: 实现 HandleSkillsClawhubDownload**

替换 stub：

```go
// HandleSkillsClawhubDownload 从 ClawHub 下载并安装技能
// 对应 Python: SkillManager.handle_skills_clawhub_download(params)
func (sm *SkillManager) HandleSkillsClawhubDownload(ctx context.Context, params map[string]any) (map[string]any, error) {
	slug, err := safePathName(params["slug"], "skill")
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	token := sm.getClawhubToken()
	if token == "" {
		return map[string]any{"success": false, "detail": "ClawHub token 未配置", "detail_key": "skills.clawhub.errors.noToken"}, nil
	}

	force := toBoolWithDefault(params["force"], false)
	destDir := filepath.Join(sm.skillsDir, slug)
	if dirExists(destDir) && !force {
		return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在，使用 force=true 覆盖", slug)}, nil
	}

	// 构建请求 URL
	reqURL, _ := url.Parse("https://clawhub.ai/api/v1/download")
	q := reqURL.Query()
	q.Set("slug", slug)
	if v := toString(params["version"]); v != "" {
		q.Set("version", v)
	}
	if v := toString(params["tag"]); v != "" {
		q.Set("tag", v)
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "网络请求失败: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("ClawHub API 返回状态码: %d", resp.StatusCode)}, nil
	}

	// 读取 ZIP 内容
	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"success": false, "detail": "读取响应失败: " + err.Error()}, nil
	}

	// 解压到临时目录
	tmpDir, err := os.MkdirTemp("", "jiuwenswarm_clawhub_")
	if err != nil {
		return map[string]any{"success": false, "detail": "创建临时目录失败: " + err.Error()}, nil
	}
	defer os.RemoveAll(tmpDir)

	if err := safeExtractZIPBytesToDir(zipBytes, tmpDir); err != nil {
		return map[string]any{"success": false, "detail": "ZIP 解压失败: " + err.Error()}, nil
	}

	// 定位 SKILL.md
	skillFile := sm.tryFindSkillFile(tmpDir)
	if skillFile == "" {
		return map[string]any{"success": false, "detail": "ZIP 中未找到 SKILL.md"}, nil
	}
	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return map[string]any{"success": false, "detail": "SKILL.md 解析失败"}, nil
	}
	skillName := toString(meta["name"])
	if skillName == "" {
		skillName = slug
	}

	// 安装到 skillsDir
	finalDest := filepath.Join(sm.skillsDir, skillName)
	if dirExists(finalDest) {
		if force {
			os.RemoveAll(finalDest)
		} else {
			return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在", skillName)}, nil
		}
	}
	// 定位 SKILL.md 所在的目录（可能是子目录）
	skillSrcDir := filepath.Dir(skillFile)
	if err := copyDir(skillSrcDir, finalDest); err != nil {
		return map[string]any{"success": false, "detail": "安装失败: " + err.Error()}, nil
	}

	// 记录安装信息
	sm.mu.Lock()
	sm.addLocalSkill(map[string]any{
		"name":        skillName,
		"origin":      "clawhub:" + slug,
		"source":      "clawhub",
		"installed_at": time.Now().Format(time.RFC3339),
	})
	sm.addInstalledPlugin(map[string]any{
		"name":        skillName,
		"marketplace": "clawhub",
		"source":      "clawhub",
		"installed_at": time.Now().Format(time.RFC3339),
	})
	sm.saveState()
	sm.mu.Unlock()

	sm.refreshAgentDataIndexes()

	return map[string]any{
		"success": true,
		"skill":   map[string]any{"name": skillName, "source": "clawhub"},
	}, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run "TestHandleSkillsClawhubDownload|TestSafeExtractZIPBytesToDir" -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 HandleSkillsClawhubDownload 和 safeExtractZIPBytesToDir"
```

---

## Task 6: TeamSkillsHub — 内部辅助方法

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go` (添加常量和辅助方法)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 添加 TeamSkillsHub 常量和环境变量**

在常量区添加：

```go
const (
	// teamSkillsHubBaseURLEnv TeamSkillsHub 基础 URL 环境变量
	teamSkillsHubBaseURLEnv = "TEAM_SKILLS_HUB_BASE_URL"
	// teamSkillsHubTimeoutEnv TeamSkillsHub 超时环境变量
	teamSkillsHubTimeoutEnv = "TEAM_SKILLS_HUB_TIMEOUT"
	// teamSkillsHubAllowedHostsEnv TeamSkillsHub 下载白名单环境变量
	teamSkillsHubAllowedHostsEnv = "TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS"
	// teamSkillsHubDefaultBaseURL TeamSkillsHub 默认基础 URL
	teamSkillsHubDefaultBaseURL = "https://teamskills.openjiuwen.com"
	// teamSkillsHubDefaultTimeout TeamSkillsHub 默认超时秒数
	teamSkillsHubDefaultTimeout = 60
)
```

在全局变量区添加：

```go
var (
	// teamSkillsHubAllowedHostDefaults TeamSkillsHub 下载白名单默认值
	teamSkillsHubAllowedHostDefaults = []string{
		"openjiuwen-market.obs.*.myhuaweicloud.com",
		"127.0.0.1",
		"localhost",
	}
)
```

- [ ] **Step 2: 实现 teamSkillsHubHTTPGet**

```go
// teamSkillsHubHTTPGet 向 TeamSkillsHub 发送 GET 请求
// 对应 Python: SkillManager._team_skills_hub_http_get_data(path, params, timeout, base_url)
func (sm *SkillManager) teamSkillsHubHTTPGet(ctx context.Context, path string, params url.Values, timeout int, baseURL string) (map[string]any, error) {
	if baseURL == "" {
		baseURL = envString(teamSkillsHubBaseURLEnv, teamSkillsHubDefaultBaseURL)
	}
	if timeout <= 0 {
		timeout = envInt(teamSkillsHubTimeoutEnv, teamSkillsHubDefaultTimeout)
	}

	reqURL, _ := url.Parse(baseURL + path)
	if params != nil {
		reqURL.RawQuery = params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回状态码: %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	// 检查业务状态码
	code, _ := payload["code"].(float64)
	if int(code) != 200 {
		return nil, fmt.Errorf("API 业务错误，code=%v, detail=%v", code, payload["detail"])
	}

	data, _ := payload["data"].(map[string]any)
	return data, nil
}
```

- [ ] **Step 3: 实现 assertTeamSkillsHubDownloadURLAllowed**

```go
// assertTeamSkillsHubDownloadURLAllowed 校验下载 URL 主机名是否在白名单中
// 对应 Python: SkillManager._assert_team_skills_hub_download_url_allowed(download_url)
func (sm *SkillManager) assertTeamSkillsHubDownloadURLAllowed(downloadURL string) error {
	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("URL 解析失败: %w", err)
	}
	host := parsed.Hostname()

	// 获取白名单
	allowedHosts := teamSkillsHubAllowedHostDefaults
	if envHosts := os.Getenv(teamSkillsHubAllowedHostsEnv); envHosts != "" {
		allowedHosts = strings.Split(envHosts, ",")
	}

	for _, pattern := range allowedHosts {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchHost(host, pattern) {
			return nil
		}
	}
	return fmt.Errorf("下载 URL 主机名 %q 不在白名单中", host)
}

// matchHost 检查主机名是否匹配模式（支持 * 通配符）
func matchHost(host, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return host == pattern
	}
	// 简单通配符匹配：*.example.com → 匹配 foo.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:]
		return host == suffix || strings.HasSuffix(host, "."+suffix)
	}
	return host == pattern
}
```

- [ ] **Step 4: 实现 downloadZipAndVerify**

```go
// downloadZipAndVerify 下载 ZIP 并校验完整性
// 对应 Python: SkillManager._download_zip_and_verify(download_url, checksum_sha256)
func (sm *SkillManager) downloadZipAndVerify(ctx context.Context, downloadURL, checksumSHA256 string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载返回状态码: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 校验：非空
	if len(data) == 0 {
		return nil, fmt.Errorf("下载内容为空")
	}

	// 校验：ZIP 魔数（PK）
	if len(data) < 2 || data[0] != 'P' || data[1] != 'K' {
		return nil, fmt.Errorf("下载内容不是有效的 ZIP 文件")
	}

	// 校验：SHA256（如果提供了 checksum）
	if checksumSHA256 != "" {
		hash := sha256.Sum256(data)
		actual := hex.EncodeToString(hash[:])
		if !strings.EqualFold(actual, checksumSHA256) {
			return nil, fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", checksumSHA256, actual)
		}
	}

	// 校验：ZIP 完整性
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		return nil, fmt.Errorf("ZIP 完整性校验失败: %w", err)
	}

	return data, nil
}
```

确保 import 中添加了：`"crypto/sha256"`, `"encoding/hex"`

- [ ] **Step 5: 添加 envString 辅助函数**

在 `envInt` 附近添加：

```go
// envString 从环境变量读取字符串，带默认值
func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
```

- [ ] **Step 6: 写测试**

```go
// TestMatchHost 验证主机名匹配
func TestMatchHost(t *testing.T) {
	tests := []struct {
		host    string
		pattern string
		want    bool
	}{
		{"example.com", "example.com", true},
		{"foo.example.com", "*.example.com", true},
		{"example.com", "*.example.com", true},
		{"evil.com", "example.com", false},
		{"evil.com", "*.example.com", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"*", "*", true},
	}
	for _, tt := range tests {
		got := matchHost(tt.host, tt.pattern)
		if got != tt.want {
			t.Errorf("matchHost(%q, %q) = %v, want %v", tt.host, tt.pattern, got, tt.want)
		}
	}
}

// TestAssertTeamSkillsHubDownloadURLAllowed_白名单 验证白名单校验
func TestAssertTeamSkillsHubDownloadURLAllowed_白名单(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	if err := sm.assertTeamSkillsHubDownloadURLAllowed("https://openjiuwen-market.obs.cn-north-4.myhuaweicloud.com/file.zip"); err != nil {
		t.Errorf("白名单内 URL 不应返回错误: %v", err)
	}
	if err := sm.assertTeamSkillsHubDownloadURLAllowed("https://evil.com/file.zip"); err == nil {
		t.Error("白名单外 URL 应返回错误")
	}
}

// TestDownloadZipAndVerify_正常 验证正常下载校验
func TestDownloadZipAndVerify_正常(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test.txt")
	fmt.Fprint(w, "hello")
	zipWriter.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBuf.Bytes())
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	data, err := sm.downloadZipAndVerify(context.Background(), server.URL, "")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(data) == 0 {
		t.Error("应返回非空数据")
	}
}
```

- [ ] **Step 7: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run "TestMatchHost|TestAssertTeamSkillsHub|TestDownloadZipAndVerify" -v -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 TeamSkillsHub 内部辅助方法（HTTP/白名单/下载校验）"
```

---

## Task 7: TeamSkillsHub — Search + Install

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:989-993` (替换 Search stub)
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:995-999` (替换 Install stub)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 写测试**

```go
// TestHandleSkillsTeamSkillsHubSearch_正常 验证正常搜索
func TestHandleSkillsTeamSkillsHubSearch_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":{"items":[{"asset_id":"test-asset","name":"test-skill","display_name":"Test Skill","short_desc":"A test skill","latest_version":"1.0.0","update_time":1234567890}],"total":1}}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubSearch(context.Background(), map[string]any{
		"q":          "test",
		"market_url": server.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubInstall_正常 验证正常安装
func TestHandleSkillsTeamSkillsHubInstall_正常(t *testing.T) {
	// 创建 ZIP 响应
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test-skill/SKILL.md")
	fmt.Fprint(w, "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文")
	zipWriter.Close()
	zipBytes := zipBuf.Bytes()
	sha := sha256.Sum256(zipBytes)
	checksum := hex.EncodeToString(sha[:])

	// artifact 元数据服务器
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s/download","checksum_sha256":"%s"}}`, "http://"+r.Host, checksum)
	}))
	defer artifactServer.Close()

	// ZIP 下载服务器
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer downloadServer.Close()

	// 覆盖 artifact 元数据中的下载 URL
	artifactServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s/download","checksum_sha256":"%s"}}`, downloadServer.URL, checksum)
	}))
	defer artifactServer2.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubInstall(context.Background(), map[string]any{
		"asset_id":   "test-asset",
		"market_url": artifactServer2.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run "TestHandleSkillsTeamSkillsHubSearch_正常|TestHandleSkillsTeamSkillsHubInstall_正常" -v -count=1`
Expected: 测试失败（当前 stub 返回 errNotImplemented）

- [ ] **Step 3: 实现 HandleSkillsTeamSkillsHubSearch**

替换 stub：

```go
// HandleSkillsTeamSkillsHubSearch 从 Team Skills Hub 搜索技能
// 对应 Python: SkillManager.handle_skills_team_skills_hub_search(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	query := trimSpace(toString(params["q"]))
	baseURL := trimSpace(toString(params["market_url"]))

	// 构建查询参数
	queryParams := url.Values{}
	if query != "" {
		queryParams.Set("search_keyword", query)
	}
	if v := trimSpace(toString(params["search_asset_id"])); v != "" {
		queryParams.Set("asset_id", v)
	}
	if v := trimSpace(toString(params["search_asset_type"])); v != "" {
		queryParams.Set("asset_type", v)
	}
	if v := trimSpace(toString(params["search_publisher_id"])); v != "" {
		queryParams.Set("publisher_id", v)
	}
	if v := trimSpace(toString(params["skill_type"])); v != "" {
		queryParams.Set("plugin_type", v)
	} else if v := trimSpace(toString(params["plugin_type"])); v != "" {
		queryParams.Set("plugin_type", v)
	}
	if v := trimSpace(toString(params["author"])); v != "" {
		queryParams.Set("publisher_name", v)
	}
	if v := trimSpace(toString(params["order_by"])); v != "" {
		queryParams.Set("order_by", v)
	} else {
		queryParams.Set("order_by", "install_count")
	}
	if v, ok := params["desc"]; ok {
		queryParams.Set("desc", toString(v))
	} else {
		queryParams.Set("desc", "true")
	}

	// 分页参数
	pageSize := envInt(teamSkillsHubTimeoutEnv, 20)
	if v, ok := params["limit"]; ok {
		if n, err := strconv.Atoi(toString(v)); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	} else if v, ok := params["page_size"]; ok {
		if n, err := strconv.Atoi(toString(v)); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	queryParams.Set("page_size", strconv.Itoa(pageSize))

	if v := trimSpace(toString(params["page"])); v != "" {
		queryParams.Set("page", v)
	} else {
		queryParams.Set("page", "1")
	}

	data, err := sm.teamSkillsHubHTTPGet(ctx, "/api/v1/plugins", queryParams, 0, baseURL)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	// 映射结果字段
	rawItems, ok := toSliceOfAny(data["items"])
	if !ok {
		return map[string]any{"success": true, "query": query, "count": 0, "skills": []map[string]any{}}, nil
	}

	var skills []map[string]any
	for _, item := range rawItems {
		if m, ok := item.(map[string]any); ok {
			assetID := toString(m["asset_id"])
			name := toString(m["name"])
			if name == "" {
				name = assetID
			}
			displayName := toString(m["display_name"])
			if displayName == "" {
				displayName = name
			}
			skills = append(skills, map[string]any{
				"asset_id":     assetID,
				"name":         name,
				"display_name": displayName,
				"summary":      toString(m["short_desc"]),
				"version":      toString(m["latest_version"]),
				"updated_at":   m["update_time"],
			})
		}
	}

	return map[string]any{"success": true, "query": query, "count": len(skills), "skills": skills}, nil
}
```

- [ ] **Step 4: 实现 HandleSkillsTeamSkillsHubInstall**

替换 stub：

```go
// HandleSkillsTeamSkillsHubInstall 从 Team Skills Hub 安装技能
// 对应 Python: SkillManager.handle_skills_team_skills_hub_install(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	assetID := trimSpace(toString(params["asset_id"]))
	if assetID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: asset_id"}, nil
	}

	baseURL := trimSpace(toString(params["market_url"]))
	force := toBoolWithDefault(params["force"], false)
	version := trimSpace(toString(params["version"]))
	output := trimSpace(toString(params["output"]))

	// 获取 artifact 元数据
	queryParams := url.Values{}
	if version != "" {
		queryParams.Set("version", version)
	}
	data, err := sm.teamSkillsHubHTTPGet(ctx, "/api/v1/artifacts/"+assetID, queryParams, 0, baseURL)
	if err != nil {
		return map[string]any{"success": false, "detail": "获取 artifact 元数据失败: " + err.Error()}, nil
	}

	downloadURL := toString(data["download_url"])
	checksumSHA256 := toString(data["checksum_sha256"])

	if downloadURL == "" {
		return map[string]any{"success": false, "detail": "artifact 元数据中缺少 download_url"}, nil
	}

	// 白名单校验
	if err := sm.assertTeamSkillsHubDownloadURLAllowed(downloadURL); err != nil {
		return map[string]any{"success": false, "detail": "下载 URL 校验失败: " + err.Error()}, nil
	}

	// 下载并校验
	zipBytes, err := sm.downloadZipAndVerify(ctx, downloadURL, checksumSHA256)
	if err != nil {
		return map[string]any{"success": false, "detail": "下载校验失败: " + err.Error()}, nil
	}

	// 解压到临时目录
	tmpDir, err := os.MkdirTemp("", "jiuwenswarm_team_skills_hub_")
	if err != nil {
		return map[string]any{"success": false, "detail": "创建临时目录失败: " + err.Error()}, nil
	}
	defer os.RemoveAll(tmpDir)

	if err := safeExtractZIPBytesToDir(zipBytes, tmpDir); err != nil {
		return map[string]any{"success": false, "detail": "ZIP 解压失败: " + err.Error()}, nil
	}

	// 定位 SKILL.md
	skillFile := sm.tryFindSkillFile(tmpDir)
	if skillFile == "" {
		return map[string]any{"success": false, "detail": "ZIP 中未找到 SKILL.md"}, nil
	}
	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return map[string]any{"success": false, "detail": "SKILL.md 解析失败"}, nil
	}
	skillName := toString(meta["name"])
	if skillName == "" {
		skillName = assetID
	}

	// 安装到目标目录
	var finalDest string
	if output != "" {
		finalDest = filepath.Join(output, skillName)
	} else {
		finalDest = filepath.Join(sm.skillsDir, skillName)
	}
	if dirExists(finalDest) && !force {
		return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在", skillName)}, nil
	}
	if dirExists(finalDest) {
		os.RemoveAll(finalDest)
	}
	skillSrcDir := filepath.Dir(skillFile)
	if err := copyDir(skillSrcDir, finalDest); err != nil {
		return map[string]any{"success": false, "detail": "安装失败: " + err.Error()}, nil
	}

	// 记录安装信息（非自定义 output 时）
	if output == "" {
		sm.mu.Lock()
		sm.addLocalSkill(map[string]any{
			"name":        skillName,
			"origin":      "teamskillshub:" + assetID,
			"source":      "teamskillshub",
			"installed_at": time.Now().Format(time.RFC3339),
		})
		sm.addInstalledPlugin(map[string]any{
			"name":        skillName,
			"marketplace": "teamskillshub",
			"source":      "teamskillshub",
			"installed_at": time.Now().Format(time.RFC3339),
		})
		sm.saveState()
		sm.mu.Unlock()
		sm.refreshAgentDataIndexes()
	}

	return map[string]any{
		"success": true,
		"skill":   map[string]any{"name": skillName, "source": "teamskillshub", "asset_id": assetID, "path": finalDest},
	}, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run "TestHandleSkillsTeamSkillsHubSearch_正常|TestHandleSkillsTeamSkillsHubInstall_正常" -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 HandleSkillsTeamSkillsHubSearch 和 HandleSkillsTeamSkillsHubInstall"
```

---

## Task 8: TeamSkillsHub — Info + Init + Validate + Pack + Publish + Delete

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:965-1011` (替换 6 个 stub)
- Test: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 实现 HandleSkillsTeamSkillsHubInfo**

替换 stub：

```go
// HandleSkillsTeamSkillsHubInfo 查询 Team Skills Hub 技能版本详情
// 对应 Python: SkillManager.handle_skills_team_skills_hub_info(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInfo(ctx context.Context, params map[string]any) (map[string]any, error) {
	assetID := trimSpace(toString(params["asset_id"]))
	if assetID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: asset_id"}, nil
	}
	baseURL := trimSpace(toString(params["market_url"]))
	version := trimSpace(toString(params["version"]))

	queryParams := url.Values{}
	if version != "" {
		queryParams.Set("version", version)
	}

	data, err := sm.teamSkillsHubHTTPGet(ctx, "/api/v1/artifacts/"+assetID, queryParams, 0, baseURL)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	return map[string]any{"success": true, "data": data}, nil
}
```

- [ ] **Step 2: 实现 HandleSkillsTeamSkillsHubInit**

```go
// HandleSkillsTeamSkillsHubInit 初始化 TeamSkills 模板目录
// 对应 Python: SkillManager.handle_skills_team_skills_hub_init(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInit(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := trimSpace(toString(params["name"]))
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	output := trimSpace(toString(params["output"]))
	var dirPath string
	if output != "" {
		dirPath = filepath.Join(output, name)
	} else {
		dirPath = filepath.Join(sm.skillsDir, name)
	}

	if dirExists(dirPath) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("目录 %s 已存在", dirPath)}, nil
	}

	// 创建目录结构
	if err := os.MkdirAll(filepath.Join(dirPath, "tools"), 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建目录失败: " + err.Error()}, nil
	}
	if err := os.MkdirAll(filepath.Join(dirPath, "data"), 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建目录失败: " + err.Error()}, nil
	}

	// 写入 SKILL.md 骨架
	skillContent := fmt.Sprintf("---\nname: %s\ndescription: \"\"\nversion: \"1.0.0\"\n---\n", name)
	if err := os.WriteFile(filepath.Join(dirPath, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		return map[string]any{"success": false, "detail": "写入 SKILL.md 失败: " + err.Error()}, nil
	}

	return map[string]any{"success": true, "path": dirPath}, nil
}
```

- [ ] **Step 3: 实现 HandleSkillsTeamSkillsHubValidate**

```go
// HandleSkillsTeamSkillsHubValidate 校验 TeamSkills 目录结构与 SKILL.md 内容
// 对应 Python: SkillManager.handle_skills_team_skills_hub_validate(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubValidate(ctx context.Context, params map[string]any) (map[string]any, error) {
	dirPath := trimSpace(toString(params["path"]))
	if dirPath == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path"}, nil
	}

	var errors []string
	if !dirExists(dirPath) {
		return map[string]any{"success": false, "detail": "目录不存在"}, nil
	}

	skillFile := sm.tryFindSkillFile(dirPath)
	if skillFile == "" {
		return map[string]any{"success": true, "valid": false, "errors": []string{"缺少 SKILL.md 文件"}}, nil
	}

	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return map[string]any{"success": true, "valid": false, "errors": []string{"SKILL.md 解析失败"}}, nil
	}

	if toString(meta["name"]) == "" {
		errors = append(errors, "SKILL.md 缺少 name 字段")
	}
	if toString(meta["description"]) == "" {
		errors = append(errors, "SKILL.md 缺少 description 字段")
	}

	if len(errors) > 0 {
		return map[string]any{"success": true, "valid": false, "errors": errors}, nil
	}
	return map[string]any{"success": true, "valid": true, "errors": []string{}}, nil
}
```

- [ ] **Step 4: 实现 HandleSkillsTeamSkillsHubPack**

```go
// HandleSkillsTeamSkillsHubPack 将 TeamSkills 目录打包为 zip
// 对应 Python: SkillManager.handle_skills_team_skills_hub_pack(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubPack(ctx context.Context, params map[string]any) (map[string]any, error) {
	dirPath := trimSpace(toString(params["path"]))
	if dirPath == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path"}, nil
	}
	if !dirExists(dirPath) {
		return map[string]any{"success": false, "detail": "目录不存在"}, nil
	}

	output := trimSpace(toString(params["output"]))
	if output == "" {
		output = dirPath + ".zip"
	}

	// 排除的目录
	excludeDirs := map[string]bool{".git": true, "__pycache__": true, "node_modules": true}

	outFile, err := os.Create(output)
	if err != nil {
		return map[string]any{"success": false, "detail": "创建 ZIP 文件失败: " + err.Error()}, nil
	}
	defer outFile.Close()

	zipWriter := zip.NewWriter(outFile)
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dirPath, path)
		if rel == "." {
			return nil
		}
		// 检查排除目录
		parts := strings.Split(rel, string(filepath.Separator))
		for _, p := range parts {
			if excludeDirs[p] {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if info.IsDir() {
			return nil
		}
		w, err := zipWriter.Create(rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	zipWriter.Close()
	outFile.Close()

	if err != nil {
		return map[string]any{"success": false, "detail": "打包失败: " + err.Error()}, nil
	}

	fi, _ := os.Stat(output)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	return map[string]any{"success": true, "zip_path": output, "size": size}, nil
}
```

- [ ] **Step 5: 实现 HandleSkillsTeamSkillsHubPublish**

```go
// HandleSkillsTeamSkillsHubPublish 发布 TeamSkills
// 对应 Python: SkillManager.handle_skills_team_skills_hub_publish(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubPublish(ctx context.Context, params map[string]any) (map[string]any, error) {
	zipPath := trimSpace(toString(params["path"]))
	if zipPath == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path"}, nil
	}
	if !fileExists(zipPath) {
		return map[string]any{"success": false, "detail": "ZIP 文件不存在"}, nil
	}

	baseURL := trimSpace(toString(params["market_url"]))
	if baseURL == "" {
		baseURL = envString(teamSkillsHubBaseURLEnv, teamSkillsHubDefaultBaseURL)
	}

	// 读取 ZIP 文件
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		return map[string]any{"success": false, "detail": "读取 ZIP 文件失败: " + err.Error()}, nil
	}

	// 构建 multipart/form-data 上传
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(zipPath))
	if err != nil {
		return map[string]any{"success": false, "detail": "构建上传请求失败: " + err.Error()}, nil
	}
	if _, err := part.Write(zipData); err != nil {
		return map[string]any{"success": false, "detail": "写入上传数据失败: " + err.Error()}, nil
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/artifacts", body)
	if err != nil {
		return map[string]any{"success": false, "detail": "构建请求失败: " + err.Error()}, nil
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "上传失败: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("API 返回状态码: %d", resp.StatusCode)}, nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]any{"success": false, "detail": "JSON 解析失败: " + err.Error()}, nil
	}

	assetID := ""
	if data, ok := result["data"].(map[string]any); ok {
		assetID = toString(data["asset_id"])
	}
	return map[string]any{"success": true, "asset_id": assetID}, nil
}
```

确保 import 中添加了：`"mime/multipart"`

- [ ] **Step 6: 实现 HandleSkillsTeamSkillsHubDelete**

```go
// HandleSkillsTeamSkillsHubDelete 删除 TeamSkills
// 对应 Python: SkillManager.handle_skills_team_skills_hub_delete(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubDelete(ctx context.Context, params map[string]any) (map[string]any, error) {
	assetID := trimSpace(toString(params["asset_id"]))
	if assetID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: asset_id"}, nil
	}
	baseURL := trimSpace(toString(params["market_url"]))
	if baseURL == "" {
		baseURL = envString(teamSkillsHubBaseURLEnv, teamSkillsHubDefaultBaseURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/v1/artifacts/"+assetID, nil)
	if err != nil {
		return map[string]any{"success": false, "detail": "构建请求失败: " + err.Error()}, nil
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "删除失败: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("API 返回状态码: %d", resp.StatusCode)}, nil
	}

	return map[string]any{"success": true}, nil
}
```

- [ ] **Step 7: 写测试**

为每个方法编写 httptest.NewServer 或 t.TempDir() 测试，覆盖正常路径和错误路径。

- [ ] **Step 8: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ -run TestHandleSkillsTeamSkillsHub -v -count=1`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 TeamSkillsHub 全套 8 个方法（Info/Init/Validate/Pack/Publish/Delete）"
```

---

## Task 9: SkillToolkit — 新包 + 结构体 + 辅助方法

**Files:**
- Create: `internal/swarm/agents/harness/tools/doc.go`
- Create: `internal/swarm/agents/harness/tools/skill_toolkit.go`
- Test: `internal/swarm/agents/harness/tools/skill_toolkit_test.go`

- [ ] **Step 1: 创建包目录和 doc.go**

```bash
mkdir -p internal/swarm/agents/harness/tools
```

`doc.go`:

```go
// Package tools 提供面向 Agent 的技能管理工具集合。
//
// SkillToolkit 将 SkillManager 的能力封装为模型可调用的工具，
// 包含 search_skill、install_skill、uninstall_skill 三个工具。
//
// 文件目录：
//
//	tools/
//	├── doc.go              # 包文档
//	└── skill_toolkit.go    # SkillToolkit 技能管理工具集
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/skill_toolkits.py
package tools
```

- [ ] **Step 2: 实现 SkillToolkit 结构体和辅助方法**

在 `skill_toolkit.go` 中实现：
- `SkillToolkit` 结构体
- `NewSkillToolkit` 构造函数
- `normalizeSource`、`safeInt`、`getInstalledNames`、`findInstalledByTarget`、`buildInstalledItem`、`normalizeSearchItem`、`listInstalledSkills` 辅助方法

- [ ] **Step 3: 实现 SearchSkill、InstallSkill、UninstallSkill**

按设计文档 6.4-6.6 的流程实现三个核心方法。

- [ ] **Step 4: 实现 GetTools**

返回 3 个 `tool.MapFunction`（项目使用 MapFunction 而非 LocalFunction，因为参数是动态 map）。

- [ ] **Step 5: 写测试**

```go
// TestNewSkillToolkit 验证创建
// TestNormalizeSource 验证 source 归一化
// TestSafeInt 验证安全整数转换
// TestGetTools 验证 3 个工具名称和参数
// TestSearchSkill_ClawHub 验证 ClawHub 搜索分发
// TestSearchKill_SkillNet_暂不支持 验证 SkillNet 暂不支持
// TestInstallSkill_已安装 验证查重跳过
// TestUninstallSkill_内置禁止 验证内置技能禁止卸载
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/agents/harness/tools/ -v -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/agents/harness/tools/
git commit -m "feat(skill): 实现 SkillToolkit 工具集（search/install/uninstall_skill）"
```

---

## Task 10: DeepAdapter 回填 + IMPLEMENTATION_PLAN 更新

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_tools.go:676-693`
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `internal/swarm/server/runtime/skill/doc.go`

- [ ] **Step 1: 回填 deep_adapter_tools.go**

将第 9 步的注释桩替换为实际注册代码：

```go
// ── 步骤 9: SkillToolkit ──
// 对齐 Python:
//   skill_toolkit = SkillToolkit(manager=self._skill_manager)
//   for tool in skill_toolkit.get_tools():
//       if not Runner.resource_mgr.get_tool(tool.card.id):
//           Runner.resource_mgr.add_tool(tool)
//       tool_cards.append(tool.card)
if d.skillManager != nil {
	skillToolkit := tools.NewSkillToolkit(d.skillManager)
	skillTools := skillToolkit.GetTools()
	for _, t := range skillTools {
		if rm.GetTool([]string{t.Card().ID}) == nil {
			_ = rm.AddTool(t)
		}
		toolCards = append(toolCards, t.Card())
	}
	logger.Info(logComponent).Int("count", len(skillTools)).Msg("getToolCards: SkillToolkit 已注册")
}
```

添加 import：`"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"`

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

更新 9.38-49 行的 Skills 标记和 10.3.19-20 的回填标记。

- [ ] **Step 3: 更新 skill/doc.go**

添加新方法到文档。

- [ ] **Step 4: 运行全量编译确认无错误**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/...`
Expected: 编译成功

- [ ] **Step 5: 运行所有相关测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/runtime/skill/ ./internal/swarm/agents/harness/tools/ ./internal/swarm/server/adapter/ -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_tools.go IMPLEMENTATION_PLAN.md internal/swarm/server/runtime/skill/doc.go
git commit -m "feat(skill): 回填 DeepAdapter SkillToolkit 注册 + 更新实现计划"
```
