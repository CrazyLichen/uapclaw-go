# SkillEvolutionRail / SkillCreateRail 注册链路对齐 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修通"字段非 nil ⇒ 已注册"这条不变量，让演进 rail 在计划模式真正注册、热重载只就地更新、单 Agent 技能创建 rail 按 Python 语义工作，并把热重载装配面从 12~17 个 rail 收敛到 Python 的 1 新建 + 5 复用。

**Architecture:** 改动集中在 `internal/swarm/server/adapter` 的 rail 装配路径（`buildAgentRails` / `updatePlanModeRails` / `updateAgentModeRails` / `getCurrentAgentRails` / slash 懒加载 / 动态 rail），外加 `internal/agentcore/harness/rails/evolution` 新增一个单 Agent `SkillCreateRail` 与 `SkillEvolutionRail.ModelName()`。所有新行为均先写失败测试再实现。

**Tech Stack:** Go 1.22、testify（单元断言在 adapter 包内用标准库 `testing`）、`tests/integration` 的 suite + mockllm、`make test` / `make test-integration`。

**Spec:** `docs/superpowers/specs/2026-11-04-skill-evolution-rail-registration-design.md`

---

## 前置约定

- 单元测试命令（单进程互斥，规则 5）：
  `pgrep -f 'go (build|test)' || CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./<pkg>/... -run <TestName> -v`
- 集成测试命令：
  `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/ -run <SuiteName> -v`
- 每次提交前跑 `gofmt -l <改动文件>`，必须无输出。
- 提交粒度：一个 Task 一个提交，message 用 `fix(rails): ...` / `feat(rails): ...` / `test(rails): ...`。

---

### Task 1: 先落 ⑤ 的红灯集成用例

**Files:**
- Create: `tests/integration/swarm/server/adapter/deep_adapter_rail_registration_test.go`

- [ ] **Step 1: 写测试骨架与 ⑤ 用例**

```go
//go:build integration

package adapter_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// RailRegistrationSuite 覆盖演进/创建 rail 的注册生命周期：
// 冷启动、计划模式消息、热重载、slash 首轮、skill_create 开关。
type RailRegistrationSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
}

// TestRailRegistrationSuite 运行 rail 注册生命周期集成测试。
func TestRailRegistrationSuite(t *testing.T) {
	suite.Run(t, new(RailRegistrationSuite))
}

// SetupTest 每测试隔离数据目录与 mock LLM provider。
func (s *RailRegistrationSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_rail_registration")
}

// TestSlashFirstMessage_EvolutionRailRegistered 会话首条消息为 /evolve_list 时
// 演进 rail 必须被注册（当前实现只建不注册，改动前必红）。
func (s *RailRegistrationSuite) TestSlashFirstMessage_EvolutionRailRegistered() {
	d := s.newAdapter("agent.plan", evolutionExtra(true, false, false))
	s.Require().Equal(0, s.countEvolutionRails(d), "冷启动不应注册演进 rail")

	req := schema.NewAgentRequest("req-slash", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "/evolve_list", "mode": "agent.plan"}),
		schema.WithAgentSessionID("itest-rail"))
	_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
	s.Require().NoError(err)

	s.Equal(1, s.countEvolutionRails(d), "首条 /evolve_list 之后演进 rail 必须已注册")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAdapter 写入配置并装配 DeepAdapter。
func (s *RailRegistrationSuite) newAdapter(mode, reactExtra string) *adapter.DeepAdapter {
	s.env.WriteConfigYAML(s.T(), reactExtra)
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, mode, ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// evolutionExtra 生成 react.evolution 配置片段。
func evolutionExtra(enabled, autoScan, skillCreate bool) string {
	return "  evolution:\n" +
		"    enabled: " + boolText(enabled) + "\n" +
		"    auto_scan: " + boolText(autoScan) + "\n" +
		"    skill_create: " + boolText(skillCreate) + "\n"
}

// boolText 把 bool 转成 YAML 小写字面量。
func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// countEvolutionRails 返回实例上已注册的 SkillEvolutionRail 数量。
func (s *RailRegistrationSuite) countEvolutionRails(d *adapter.DeepAdapter) int {
	return len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{})))
}

// jsonParams 将 map 序列化为请求参数。
func (s *RailRegistrationSuite) jsonParams(params map[string]any) json.RawMessage {
	data, err := json.Marshal(params)
	s.Require().NoError(err)
	return data
}
```

- [ ] **Step 2: 跑一次，确认红灯**

Run:
```bash
pgrep -f 'go (build|test)' || CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/ -run TestRailRegistrationSuite -v
```
Expected: FAIL，输出 `首条 /evolve_list 之后演进 rail 必须已注册`，实际 `0 != 1`。

- [ ] **Step 3: 提交红灯用例（带 Skip 说明可不加，直接提交失败用例到分支即可）**

```bash
git add tests/integration/swarm/server/adapter/deep_adapter_rail_registration_test.go
git commit -m "test(rails): 固化 slash 首轮演进 rail 注册用例（当前必红）"
```

---

### Task 2: 组 1 移除冷启动构建

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:183-206`
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`（新建）

- [ ] **Step 1: 写失败测试**

新建 `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`：

```go
package adapter

import (
	"testing"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestBuildAgentRails_不构建演进与创建rail 冷启动装配不得构建两条按模式注册的 rail，
// 否则"字段非 nil ⇒ 已注册"的判据会被提前填充的字段打破。
func TestBuildAgentRails_不构建演进与创建rail(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"enabled": true, "skill_create": true, "auto_scan": true},
	}, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"

	railsList := d.buildAgentRails(map[string]any{}, map[string]any{}, "agent")

	if d.skillEvolutionRail != nil {
		t.Fatalf("冷启动不应构建 SkillEvolutionRail，实际 = %T", d.skillEvolutionRail)
	}
	if d.skillCreateRail != nil {
		t.Fatalf("冷启动不应构建 SkillCreateRail，实际 = %T", d.skillCreateRail)
	}
	for _, r := range railsList {
		switch r.(type) {
		case *coreevolution.SkillEvolutionRail, *coreevolution.SkillCreateRail:
			t.Fatalf("冷启动 railsList 不应包含 %T", r)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestBuildAgentRails_不构建演进与创建rail -v`
Expected: FAIL（`冷启动不应构建 SkillEvolutionRail`）

- [ ] **Step 3: 删除冷启动构建**

把 `deep_adapter_rails.go:183-192` 的演进 rail 构建块与 `:194-196` 的创建 rail 构建块替换为注释：

```go
	// ─── 不在冷启动时挂载的 Rail（由 updateRailsForMode 按需注册） ───

	// skillEvolutionRail / skillCreateRail — 冷启动既不构建也不注册。
	// 两条 rail 的字段语义是「非 nil ⇒ 已注册」，构建提前写字段会让
	// updatePlanModeRails 的判空条件恒为假、注册分支被跳过。
	// Python: interface_deep.py:2158「SkillEvolutionRail 不在冷启动时挂载」，
	// 字段保持 None，仅在 _update_plan_mode_rails / _ensure_evolution_rail_for_slash 内赋值。

	// memoryRail — 冷启动不构建（对齐 Python：_memory_rail 仅在
	// _handle_memory_rail_by_config 内构建，冷启动只置 None）。
	// 此前 Go 在冷启动构建并存字段，导致后续 handleMemoryRailByConfig
	// 误判"已注册且类型相同"而跳过 RegisterRail——MemoryRail 永不挂载。
	d.memoryRail = nil
```

- [ ] **Step 4: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestBuildAgentRails -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/swarm/server/adapter/deep_adapter_rails.go
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go
git commit -m "fix(rails): 冷启动不再构建演进/创建 rail，恢复字段注册语义"
```

---

### Task 3: 组 2a 收窄 builder 签名并去断言

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:340`（签名）、`:1106-1123`（plan 注册）
- Modify: `internal/swarm/server/adapter/deep_adapter_slash.go:88-93`

- [ ] **Step 1: 改签名**

```go
// buildSkillEvolutionRail 构建技能演进护栏。
// 对齐 Python: _build_skill_evolution_rail() (interface_deep.py:1948-1968)
//
// 返回具体类型：调用方无需类型断言，避免断言失败时静默丢弃注册。
func (d *DeepAdapter) buildSkillEvolutionRail() *evolution.SkillEvolutionRail {
```

- [ ] **Step 2: 改 plan 注册分支（注册成功才写字段）**

把 `deep_adapter_rails.go:1106-1123` 整段替换为：

```go
	// 7. SkillEvolutionRail — 按 evolution 配置注册/注销
	evolutionConfig, _ := cache["evolution"].(map[string]any)
	evolutionEnabled, _ := evolutionConfig["enabled"].(bool)
	if evolutionEnabled {
		if d.skillEvolutionRail == nil {
			rail := d.buildSkillEvolutionRail()
			if rail != nil && d.instance != nil {
				if err := d.instance.RegisterRail(ctx, rail); err != nil {
					logger.Error(logComponent).Err(err).Msg("Failed to register SkillEvolutionRail")
				} else {
					d.skillEvolutionRail = rail
					logger.Info(logComponent).Msg("SkillEvolutionRail registered (plan mode)")
				}
			}
		}
	} else {
		if d.skillEvolutionRail != nil {
			if d.instance != nil {
				if err := d.instance.UnregisterRail(ctx, d.skillEvolutionRail); err != nil {
					logger.Error(logComponent).Err(err).Msg("Failed to unregister SkillEvolutionRail")
				}
			}
			d.skillEvolutionRail = nil
			logger.Info(logComponent).Msg("SkillEvolutionRail unregistered (evolution.enabled=false)")
		}
	}
```

- [ ] **Step 3: 改 slash 懒加载去断言**

`deep_adapter_slash.go:88-93` 替换为：

```go
	// 懒加载：如果 rail 为 nil，尝试构建（注册在任务 8 补齐）
	if d.skillEvolutionRail == nil {
		d.skillEvolutionRail = d.buildSkillEvolutionRail()
	}
```

- [ ] **Step 4: 编译并跑包内测试**

Run: `CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/swarm/server/adapter/
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter_slash.go
git commit -m "refactor(rails): 收窄 buildSkillEvolutionRail 返回类型并去掉三处断言"
```

---

### Task 4: 组 2b ModelName 访问器 + 热重载就地更新

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`（新增字段、构造函数赋值、UpdateLLM 赋值、ModelName 方法）
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`（新增 `evolutionAutoScan` / `updateSkillEvolutionRail`）
- Modify: `internal/swarm/server/adapter/deep_adapter.go:3072-3074`（`getCurrentAgentRails` 顶部调用）
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `deep_adapter_evolution_rail_test.go`：

```go
// newTestEvolutionRail 构造一个带可用 model 的演进 rail。
func newTestEvolutionRail(t *testing.T, modelName string) *coreevolution.SkillEvolutionRail {
	t.Helper()
	return coreevolution.NewSkillEvolutionRail(
		[]string{t.TempDir()},
		usableTestModel(),
		modelName,
		"cn",
		coreevolution.WithAutoScan(false),
		coreevolution.WithAutoSave(false),
	)
}

// TestUpdateSkillEvolutionRail_就地更新 热重载必须就地换 LLM 与 auto_scan，实例不变。
func TestUpdateSkillEvolutionRail_就地更新(t *testing.T) {
	d := NewDeepAdapter()
	rail := newTestEvolutionRail(t, "old-model")
	d.skillEvolutionRail = rail
	d.model = usableTestModel()
	d.defaultModelName = "new-model"

	d.updateSkillEvolutionRail(map[string]any{
		"evolution": map[string]any{"auto_scan": true},
	})

	if d.skillEvolutionRail != rail {
		t.Fatal("就地更新不得替换 rail 实例")
	}
	if got := rail.ModelName(); got != "new-model" {
		t.Fatalf("ModelName = %q, want new-model", got)
	}
	if !rail.AutoScan() {
		t.Fatal("auto_scan 应跟随配置更新为 true")
	}
}

// TestUpdateSkillEvolutionRail_环境变量优先 EVOLUTION_AUTO_SCAN=1 覆盖配置里的 false。
func TestUpdateSkillEvolutionRail_环境变量优先(t *testing.T) {
	t.Setenv("EVOLUTION_AUTO_SCAN", "1")
	d := NewDeepAdapter()
	rail := newTestEvolutionRail(t, "m")
	d.skillEvolutionRail = rail
	d.model = usableTestModel()
	d.defaultModelName = "m"

	d.updateSkillEvolutionRail(map[string]any{
		"evolution": map[string]any{"auto_scan": false},
	})

	if !rail.AutoScan() {
		t.Fatal("环境变量 1 应覆盖配置的 false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败（编译失败即可）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestUpdateSkillEvolutionRail -v`
Expected: FAIL — `undefined: d.updateSkillEvolutionRail` / `rail.ModelName undefined`

- [ ] **Step 3: 给 rail 加字段与访问器**

`skill_evolution_rail.go` 字段区（`language` 之后）新增：

```go
	// modelName 当前 LLM 模型名（UpdateLLM 与构造时同步），供排障与断言
	modelName string
```

构造函数 `NewSkillEvolutionRail` 的初始化列表里加一行：

```go
		modelName:                model,
```

`UpdateLLM` 内补一行：

```go
func (r *SkillEvolutionRail) UpdateLLM(llmModel *llm.Model, model string) {
	r.modelName = model
	r.evolver.UpdateLLM(llmModel, model)
	r.scorer.UpdateLLM(llmModel, model)
	if r.keywordExtractor != nil {
		r.keywordExtractor.UpdateLLM(llmModel, model)
	}
}
```

访问器放在 `AutoSave()` 附近：

```go
// ModelName 返回当前 LLM 模型名。
func (r *SkillEvolutionRail) ModelName() string { return r.modelName }
```

- [ ] **Step 4: 加 adapter 侧 helper**

在 `deep_adapter_rails.go` 的非导出函数区（`buildSkillEvolutionRail` 之前）新增：

```go
// evolutionAutoScan 计算 auto_scan：环境变量优先，其次 config，缺省关闭。
// 对齐 Python: interface_deep.py:1951-1955 与 2317-2328
// （注意 rail 构造默认值为 true，必须显式传入，不能省略）
func evolutionAutoScan(config map[string]any) bool {
	if env := os.Getenv("EVOLUTION_AUTO_SCAN"); env != "" {
		switch strings.ToLower(strings.TrimSpace(env)) {
		case "true", "1", "yes":
			return true
		default:
			return false
		}
	}
	evolutionConfig, _ := config["evolution"].(map[string]any)
	as, _ := evolutionConfig["auto_scan"].(bool)
	return as
}

// updateSkillEvolutionRail 热重载时就地更新演进 rail 的 LLM 与 auto_scan。
// Python: _get_current_agent_rails 顶部（interface_deep.py:2314-2328）——
// 该 rail 不重建、不重新注册，只换 LLM 与开关。
func (d *DeepAdapter) updateSkillEvolutionRail(config map[string]any) {
	if d.skillEvolutionRail == nil {
		return
	}
	d.skillEvolutionRail.UpdateLLM(d.modelSnapshot(), d.defaultModelName)
	d.skillEvolutionRail.SetAutoScan(evolutionAutoScan(config))
	logger.Info(logComponent).
		Str("model", d.defaultModelName).
		Bool("auto_scan", d.skillEvolutionRail.AutoScan()).
		Msg("SkillEvolutionRail hot-updated in place")
}
```

- [ ] **Step 5: 在热重载路径接入**

`deep_adapter.go` 的 `getCurrentAgentRails` 改为（Task 10 会整体重写，这里先接一行）：

```go
func (d *DeepAdapter) getCurrentAgentRails(config map[string]any, configBase map[string]any) []sainterfaces.AgentRail {
	d.updateSkillEvolutionRail(config)
	return d.buildAgentRails(config, configBase, d.mode)
}
```

- [ ] **Step 6: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestUpdateSkillEvolutionRail -v`
Expected: PASS（2 个用例）

- [ ] **Step 7: 提交**

```bash
gofmt -l internal/swarm/server/adapter/ internal/agentcore/harness/rails/evolution/
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go
git commit -m "fix(rails): 热重载就地更新演进 rail 的 LLM 与 auto_scan"
```

---

### Task 5: 组 4 构造参数与 Python 对齐

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:338-410`
- Modify: `resources/config.yaml:263-274`
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试（表驱动）**

```go
// TestBuildSkillEvolutionRail_参数矩阵 构造参数必须与 Python 对齐：
// auto_scan 缺省关闭、env 认 1/yes、auto_save 恒 false、sharing 不接、model_name 兜底。
func TestBuildSkillEvolutionRail_参数矩阵(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	t.Run("缺auto_scan默认关闭", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true}})
		rail := d.buildSkillEvolutionRail()
		if rail == nil {
			t.Fatal("rail 不应为 nil")
		}
		if rail.AutoScan() {
			t.Fatal("auto_scan 缺省必须为 false（对齐 Python :1955）")
		}
	})

	t.Run("环境变量1开启", func(t *testing.T) {
		t.Setenv("EVOLUTION_AUTO_SCAN", "1")
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true, "auto_scan": false}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || !rail.AutoScan() {
			t.Fatal("EVOLUTION_AUTO_SCAN=1 应开启 auto_scan（对齐 Python :1953）")
		}
	})

	t.Run("环境变量yes开启", func(t *testing.T) {
		t.Setenv("EVOLUTION_AUTO_SCAN", "yes")
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || !rail.AutoScan() {
			t.Fatal("EVOLUTION_AUTO_SCAN=yes 应开启 auto_scan")
		}
	})

	t.Run("auto_save被忽略", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true, "auto_save": true}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.AutoSave() {
			t.Fatal("auto_save 必须恒为 false（Python :1962 硬编码）")
		}
	})

	t.Run("sharing被忽略", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{
			"enabled": true, "sharing": map[string]any{"enabled": true},
		}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.IsSharingEnabled() {
			t.Fatal("adapter 路径不应接 sharing（Python builder 不传 sharing_config）")
		}
	})

	t.Run("model_name兜底配置", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true}, "model_name": "cfg-model"})
		d.defaultModelName = ""
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.ModelName() != "cfg-model" {
			t.Fatalf("model_name 应回落到 config.model_name，实际 %v", rail)
		}
	})
}

// newRailBuildAdapter 构造只用于 rail 构建的 adapter。
func newRailBuildAdapter(t *testing.T, cache map[string]any) *DeepAdapter {
	t.Helper()
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(cache, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"
	return d
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestBuildSkillEvolutionRail_参数矩阵 -v`
Expected: FAIL（`auto_scan 缺省必须为 false` 等）

- [ ] **Step 3: 改 buildSkillEvolutionRail**

整段替换为：

```go
// buildSkillEvolutionRail 构建技能演进护栏。
// 对齐 Python: _build_skill_evolution_rail() (interface_deep.py:1948-1968)
//
// 与 Python 一致：本函数不判 evolution.enabled（门控在调用方），
// 不接 sharing 配置，auto_save 恒 false（需审批），auto_scan 缺省关闭。
func (d *DeepAdapter) buildSkillEvolutionRail() *evolution.SkillEvolutionRail {
	model := d.modelSnapshot()
	if model == nil {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: model is nil, skipping")
		return nil
	}

	cache := d.configCacheSnapshot()

	// 禁用技能列表
	var disabledSkills []string
	if d.skillManager != nil {
		disabledSkills = d.skillManager.ListExecutionDisabledSkills()
	}

	// skills 目录
	skillsDir := workspace.AgentSkillsDir()
	if skillsDir == "" {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: skills dir is empty, skipping")
		return nil
	}

	// model_name 兜底链：默认模型 → config.model_name → 模板默认值
	modelName := d.defaultModelName
	if modelName == "" {
		if v, ok := cache["model_name"].(string); ok && v != "" {
			modelName = v
		}
	}
	if modelName == "" {
		modelName = "qwen-max"
	}

	rail := evolution.NewSkillEvolutionRail(
		[]string{skillsDir},
		model,
		modelName,
		d.resolveRuntimeLanguage(),
		evolution.WithAutoScan(evolutionAutoScan(cache)),
		// auto_save 恒 false：rail 构造默认 true（skill_evolution_rail.go:192），
		// 省略该选项等于放开审批，必须显式传 false。
		evolution.WithAutoSave(false),
		evolution.WithDisabledSkillsSet(disabledSkills),
	)

	logger.Info(logComponent).
		Str("model", modelName).
		Bool("auto_scan", rail.AutoScan()).
		Msg("SkillEvolutionRail created")
	return rail
}
```

- [ ] **Step 4: 更新配置模板注释**

`resources/config.yaml` 的 `evolution:` 段改为：

```yaml
  # 在线技能自演进（对齐 Python 模板 react.evolution）
  # 代码读取：deep_adapter_rails.go（enabled 门控在调用方）、updatePlanModeRails、
  #          runtime_inheritance.go（auto_scan/skill_create）、web_handlers.go（skill_create）
  # 注：auto_save 固定 false（需审批），sharing 未接线（Python adapter 同样不接），
  #     两者不再作为配置键暴露。
  evolution:
    # 是否启用在线 Skills 自演进
    enabled: true
    # 每轮对话后自动扫描（缺省 false，对齐 Python；可用 EVOLUTION_AUTO_SCAN=true/1/yes 覆盖）
    auto_scan: false
    # 注册单 Agent SkillCreateRail（阈值检测 → 提议创建新技能）
    skill_create: false
    # Skill 根目录（模板对齐项：Go 侧取 workspace.AgentSkillsDir()、
    # Python 侧用 get_agent_skills_dir()，两端代码均未消费本键）
    skill_base_dir: "agent/skills"
```

- [ ] **Step 5: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run 'TestBuildSkillEvolutionRail|TestBuildAgentRails' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
gofmt -l internal/swarm/server/adapter/
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go resources/config.yaml
git commit -m "fix(rails): 演进 rail 构造参数对齐 Python（auto_scan/auto_save/sharing/model_name）"
```

---

### Task 6: 组 3a 新增单 Agent SkillCreateRail

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/skill_create_rail.go`
- Modify: `internal/agentcore/harness/rails/evolution/doc.go`
- Test: `internal/agentcore/harness/rails/evolution/skill_create_rail_test.go`

- [ ] **Step 1: 写失败测试**

```go
package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// fakeLoopController 记录 follow_up 文本。
type fakeLoopController struct {
	controller.ControllerInterface
	prompts []string
}

// EnqueueFollowUp 记录入队文本。
func (f *fakeLoopController) EnqueueFollowUp(prompt string) { f.prompts = append(f.prompts, prompt) }

// fakeLoopAgent 只实现 LoopController() 的 agent 桩（其余方法靠嵌入接口占位）。
type fakeLoopAgent struct {
	agentinterfaces.BaseAgent
	ctl controller.ControllerInterface
}

// LoopController 返回注入的 controller。
func (f *fakeLoopAgent) LoopController() controller.ControllerInterface { return f.ctl }

// newSkillCreateRailForTest 构造带 step 的 rail 与回调上下文。
func newSkillCreateRailForTest(t *testing.T, toolNames []string) (*SkillCreateRail, *agentinterfaces.AgentCallbackContext, *fakeLoopController) {
	t.Helper()
	rail := NewSkillCreateRail(t.TempDir())
	builder := trajectory.NewTrajectoryBuilder("sess-1", "test")
	for _, name := range toolNames {
		builder.RecordStep(&trajectory.TrajectoryStep{
			Kind:   trajectory.StepKindTool,
			Detail: &trajectory.ToolCallDetail{ToolName: name},
		})
	}
	rail.builder = builder

	ctl := &fakeLoopController{}
	agent := &fakeLoopAgent{ctl: ctl}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	return rail, cbc, ctl
}

// TestSkillCreateRail_阈值不足不提议 工具数与种类未达阈值时不入队。
func TestSkillCreateRail_阈值不足不提议(t *testing.T) {
	rail, cbc, ctl := newSkillCreateRailForTest(t, []string{"read_file", "write_file"})
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.prompts) != 0 {
		t.Fatalf("阈值不足不应入队，实际 %d 条", len(ctl.prompts))
	}
}

// TestSkillCreateRail_满足阈值只提议一次 达阈值入队且每轮仅一次。
func TestSkillCreateRail_满足阈值只提议一次(t *testing.T) {
	tools := make([]string, 0, 10)
	for _, name := range []string{"read_file", "write_file", "bash", "code", "edit_file"} {
		tools = append(tools, name, name)
	}
	rail, cbc, ctl := newSkillCreateRailForTest(t, tools)

	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.prompts) != 1 {
		t.Fatalf("达阈值应入队 1 条，实际 %d", len(ctl.prompts))
	}
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.prompts) != 1 {
		t.Fatalf("同一轮不应重复入队，实际 %d", len(ctl.prompts))
	}
}

// TestSkillCreateRail_BeforeInvoke重置提议标记 新一轮开始后可再次提议。
func TestSkillCreateRail_BeforeInvoke重置提议标记(t *testing.T) {
	tools := make([]string, 0, 10)
	for _, name := range []string{"read_file", "write_file", "bash", "code", "edit_file"} {
		tools = append(tools, name, name)
	}
	rail, cbc, ctl := newSkillCreateRailForTest(t, tools)
	_ = rail.OnAfterTaskIteration(context.Background(), cbc)
	_ = rail.OnBeforeInvoke(context.Background(), cbc)
	_ = rail.OnAfterTaskIteration(context.Background(), cbc)
	if len(ctl.prompts) != 2 {
		t.Fatalf("before_invoke 后应可再次提议，实际 %d", len(ctl.prompts))
	}
}

// TestSkillCreateRail_无Controller时不入队 拿不到 LoopController 时丢弃提议且不置标记。
func TestSkillCreateRail_无Controller时不入队(t *testing.T) {
	tools := make([]string, 0, 10)
	for _, name := range []string{"read_file", "write_file", "bash", "code", "edit_file"} {
		tools = append(tools, name, name)
	}
	rail, _, _ := newSkillCreateRailForTest(t, tools)
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeLoopAgent{}, nil, nil)
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if rail.proposalSent {
		t.Fatal("无 controller 时不应置提议标记")
	}
}

// TestSkillCreateRail_Priority 优先级对齐 Python 的 85。
func TestSkillCreateRail_Priority(t *testing.T) {
	if got := NewSkillCreateRail(t.TempDir()).Priority(); got != 85 {
		t.Fatalf("Priority = %d, want 85", got)
	}
}

// TestSkillCreateRail_CN与EN文案含skillsDir 文案按语言选择并替换目录。
func TestSkillCreateRail_CN与EN文案含skillsDir(t *testing.T) {
	railCN := NewSkillCreateRail("/tmp/skills")
	if p := railCN.buildPrompt(); !strings.Contains(p, "/tmp/skills") || !strings.Contains(p, "skill-creator") {
		t.Fatalf("CN 文案应包含目录与 skill-creator: %s", p)
	}
	railEN := NewSkillCreateRail("/tmp/skills", WithSkillCreateLanguage("en"))
	if p := railEN.buildPrompt(); !strings.Contains(p, "/tmp/skills") || !strings.Contains(p, "skill-creator") {
		t.Fatalf("EN 文案应包含目录与 skill-creator: %s", p)
	}
}
```

> 说明：`rail.builder` 是 `EvolutionRail` 的未导出字段，测试与实现同包可直接赋值。

- [ ] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/evolution/ -run TestSkillCreateRail -v`
Expected: FAIL — `undefined: NewSkillCreateRail`

- [ ] **Step 3: 实现 rail**

新建 `internal/agentcore/harness/rails/evolution/skill_create_rail.go`：

```go
package evolution

import (
	"context"
	"strings"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillCreateRailOption 构造选项函数。
type SkillCreateRailOption func(*SkillCreateRail)

// SkillCreateRail 单 Agent 技能创建护栏。
//
// 在任务循环每次迭代后统计本轮轨迹的工具调用总数与种类数，达阈值时通过
// TaskLoopController 注入一条 follow-up，请模型调用 skill-creator 创建新技能。
// 与 TeamSkillCreateRail 的区别：不依赖团队完成信号，纯阈值驱动。
//
// 对齐 Python: openjiuwen/harness/rails/skills/skill_create_rail.py::SkillCreateRail
type SkillCreateRail struct {
	*EvolutionRail

	// skillsDir 技能根目录（写入 follow-up 文案）
	skillsDir string
	// language 语言（"cn" 或 "en"）
	language string
	// autoTrigger 是否自动检测触发
	autoTrigger bool
	// toolCallThreshold 工具调用总数阈值
	toolCallThreshold int
	// toolDiversityThreshold 工具种类阈值
	toolDiversityThreshold int
	// proposalSent 本轮是否已提议（before_invoke 重置）
	proposalSent bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skillCreatePriority 技能创建轨道优先级
	// 对齐 Python: SkillCreateRail.priority = 85
	skillCreatePriority = 85
	// defaultToolCallThreshold 默认工具调用总数阈值
	defaultToolCallThreshold = 10
	// defaultToolDiversityThreshold 默认工具种类阈值
	defaultToolDiversityThreshold = 5
)

// skillCreatePromptCN 中文确认提示词（一比一复刻 Python _FOLLOW_UP_PROMPT_CN）
const skillCreatePromptCN = "**重要：你必须先向用户确认，，不可跳过此步骤。**\n" +
	"系统检测到对话中存在可复用模式，可能值得创建新技能。请按以下步骤执行：\n" +
	"1. 直接询问或调用 ask_user 工具向用户确认：\n" +
	"   - 问题：\"我检测到您可能值得创建一个新技能。是否创建？\"\n" +
	"   - 选项：[\"创建\"，\"跳过\"，\"自定义指令：（请描述需求）\"]\n" +
	"2. 如果用户选择\"创建\"或提供了自定义指令，请调用 **skill-creator** 技能，" +
	"根据用户的要求和当前对话上下文执行技能创建。\n" +
	"   新技能应保存到技能目录：{skills_dir}"

// skillCreatePromptEN 英文确认提示词（一比一复刻 Python _FOLLOW_UP_PROMPT_EN）
const skillCreatePromptEN = "**Important: You MUST confirm with the user first. Do not skip this step.**\n" +
	"The system detected a reusable pattern that may be worth creating as a new skill. " +
	"Please follow these steps:\n" +
	"1. Directly inquire or invoke the `ask_user` tool to confirm with the user:\n" +
	"   - Question: \"I detected a pattern that may be worth creating as a new skill. Create it?\"\n" +
	"   - Options: [\"Create\", \"Skip\", \"Custom instruction: (describe your needs)\"]\n" +
	"2. If user chooses \"Create\" or provides a custom instruction, invoke the **skill-creator** skill " +
	"to execute the skill creation.\n" +
	"   Save the new skill to: {skills_dir}"

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译期验证 SkillCreateRail 满足 EvolutionExtension 接口
var _ EvolutionExtension = (*SkillCreateRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillCreateRail 创建单 Agent 技能创建护栏。
// 对齐 Python: SkillCreateRail(skills_dir, language="cn", auto_trigger=True,
// tool_call_threshold=10, tool_diversity_threshold=5)
func NewSkillCreateRail(skillsDir string, opts ...SkillCreateRailOption) *SkillCreateRail {
	r := &SkillCreateRail{
		skillsDir:              skillsDir,
		language:               "cn",
		autoTrigger:            true,
		toolCallThreshold:      defaultToolCallThreshold,
		toolDiversityThreshold: defaultToolDiversityThreshold,
	}
	// evolution_trigger=NONE：不自动触发演化，只在迭代回调里做阈值检测
	r.EvolutionRail = NewEvolutionRail(r, WithEvolutionTrigger(TriggerNone))
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithSkillCreateAutoTrigger 设置是否自动检测触发。
func WithSkillCreateAutoTrigger(auto bool) SkillCreateRailOption {
	return func(r *SkillCreateRail) { r.autoTrigger = auto }
}

// WithSkillCreateLanguage 设置语言。
func WithSkillCreateLanguage(lang string) SkillCreateRailOption {
	return func(r *SkillCreateRail) { r.language = lang }
}

// WithSkillCreateThresholds 设置工具调用总数与种类阈值。
func WithSkillCreateThresholds(calls, unique int) SkillCreateRailOption {
	return func(r *SkillCreateRail) {
		r.toolCallThreshold = calls
		r.toolDiversityThreshold = unique
	}
}

// Priority 返回轨道优先级。
func (r *SkillCreateRail) Priority() int { return skillCreatePriority }

// AutoTrigger 返回是否自动检测触发。
func (r *SkillCreateRail) AutoTrigger() bool { return r.autoTrigger }

// OnBeforeInvoke 重置本轮提议标记。
func (r *SkillCreateRail) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.proposalSent = false
	return nil
}

// OnAfterTaskIteration 任务迭代后做阈值检测并提议。
func (r *SkillCreateRail) OnAfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.maybePropose(cbc)
	return nil
}

// OnAfterModelCall 空实现。
func (r *SkillCreateRail) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 空实现。
func (r *SkillCreateRail) OnAfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterInvoke 空实现（Python 仅在 after_task_iteration 检测）。
func (r *SkillCreateRail) OnAfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterEvolutionTriggered 空实现。
func (r *SkillCreateRail) OnAfterEvolutionTriggered(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AllowEvolutionTrigger 始终返回 true（NONE 模式下不会被自动触发）。
func (r *SkillCreateRail) AllowEvolutionTrigger(_ EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	return true
}

// SnapshotForEvolution 返回 nil（本 rail 不参与演化快照）。
func (r *SkillCreateRail) SnapshotForEvolution(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	return nil
}

// RunEvolution 空实现。
func (r *SkillCreateRail) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *EvolutionSnapshot) error {
	return nil
}

// GetEvolutionTotalTimeoutSecs 返回 0（无后台演化）。
func (r *SkillCreateRail) GetEvolutionTotalTimeoutSecs() float64 { return 0 }

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybePropose 阈值检测 → 通过 LoopController 注入 follow-up。
// 对齐 Python: _on_after_task_iteration(ctx)
func (r *SkillCreateRail) maybePropose(cbc *agentinterfaces.AgentCallbackContext) {
	if !r.autoTrigger || r.proposalSent {
		return
	}

	builder := r.Builder()
	if builder == nil {
		logger.Debug(logComponent).Msg("SkillCreateRail trajectory builder is nil, skipping")
		return
	}

	total, unique := countToolCalls(builder.GetSteps())
	if total < r.toolCallThreshold || unique < r.toolDiversityThreshold {
		logger.Debug(logComponent).
			Int("total_calls", total).
			Int("unique_tools", unique).
			Int("call_threshold", r.toolCallThreshold).
			Int("diversity_threshold", r.toolDiversityThreshold).
			Msg("SkillCreateRail below threshold, skipping")
		return
	}

	if cbc == nil || cbc.Agent() == nil {
		return
	}
	ctl := getLoopController(cbc.Agent())
	if ctl == nil {
		logger.Warn(logComponent).
			Msg("skill creation proposal dropped: no TaskLoopController available (task-loop mode required)")
		return
	}

	prompt := r.buildPrompt()
	logger.Info(logComponent).
		Str("language", r.language).
		Str("skills_dir", r.skillsDir).
		Int("prompt_length", len(prompt)).
		Int("total_calls", total).
		Int("unique_tools", unique).
		Msg("SkillCreateRail threshold met, enqueuing follow_up")
	ctl.EnqueueFollowUp(prompt)
	r.proposalSent = true
}

// countToolCalls 统计工具调用总数与去重后的种类数。
// 对齐 Python: _should_propose_new_skill()
func countToolCalls(steps []*trajectory.TrajectoryStep) (total int, unique int) {
	seen := make(map[string]bool)
	for _, step := range steps {
		if step == nil || step.Kind != trajectory.StepKindTool {
			continue
		}
		detail, ok := step.Detail.(*trajectory.ToolCallDetail)
		if !ok || detail == nil || detail.ToolName == "" {
			continue
		}
		total++
		seen[detail.ToolName] = true
	}
	return total, len(seen)
}

// buildPrompt 按语言选择文案并替换技能目录。
// 对齐 Python: prompt = _FOLLOW_UP_PROMPT_CN/EN.format(skills_dir=self._skills_dir)
func (r *SkillCreateRail) buildPrompt() string {
	if r.language == "cn" {
		return strings.ReplaceAll(skillCreatePromptCN, "{skills_dir}", r.skillsDir)
	}
	return strings.ReplaceAll(skillCreatePromptEN, "{skills_dir}", r.skillsDir)
}
```

- [ ] **Step 4: 更新 doc.go 文件目录**

`internal/agentcore/harness/rails/evolution/doc.go` 的文件树中，在 `team_skill_create_rail.go` 之后加一行：

```
//	├── skill_create_rail.go            # SkillCreateRail 单 Agent 技能创建护栏（Priority=85+阈值检测）
```

- [ ] **Step 5: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/evolution/ -run TestSkillCreateRail -v`
Expected: PASS（5 个用例）

- [ ] **Step 6: 提交**

```bash
gofmt -l internal/agentcore/harness/rails/evolution/
git add internal/agentcore/harness/rails/evolution/skill_create_rail.go internal/agentcore/harness/rails/evolution/skill_create_rail_test.go internal/agentcore/harness/rails/evolution/doc.go
git commit -m "feat(rails): 移植单 Agent SkillCreateRail（阈值检测→follow_up 提议）"
```

---

### Task 7: 组 3b adapter 接入新 rail + 门控 + 字段类型

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:185-187`（字段类型）
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:412-439`（builder）、`:1136-1147`（plan 门控）、`:480-492`（删 getMinTeamMembersForCreate）
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go:62-78`（删死函数）
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestBuildSkillCreateRail_未启用 未开启时返回 nil 且不写字段。
func TestBuildSkillCreateRail_未启用(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{"evolution": map[string]any{"skill_create": false}}, map[string]any{})

	if rail := d.buildSkillCreateRail(); rail != nil {
		t.Fatalf("skill_create=false 应返回 nil，实际 %T", rail)
	}
	if d.skillCreateRail != nil {
		t.Fatal("builder 不得写字段")
	}
}

// TestBuildSkillCreateRail_启用返回单Agent版 启用时构建单 Agent 版且 auto_trigger 为真。
func TestBuildSkillCreateRail_启用返回单Agent版(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"skill_create": true},
		"language":  "cn",
	}, map[string]any{})

	rail := d.buildSkillCreateRail()
	if rail == nil {
		t.Fatal("skill_create=true 应构建 rail")
	}
	if !rail.AutoTrigger() {
		t.Fatal("auto_trigger 必须恒为 true（Python :1988）")
	}
	if d.skillCreateRail != nil {
		t.Fatal("builder 不得写字段，由调用方在注册成功后赋值")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestBuildSkillCreateRail -v`
Expected: FAIL（未启用时仍构建 Team 版；启用时断言类型不符）

- [ ] **Step 3: 改字段类型**

`deep_adapter.go:185-187`：

```go
	// skillCreateRail 技能创建护栏（单 Agent 版）
	// 对齐 Python: _skill_create_rail: SkillCreateRail | None
	skillCreateRail *evolution.SkillCreateRail
```

- [ ] **Step 4: 改 builder**

`deep_adapter_rails.go:412-439` 整段替换为：

```go
// buildSkillCreateRail 构建单 Agent 技能创建护栏。
// 对齐 Python: _build_skill_create_rail() (interface_deep.py:1972-1997)
//
// Python 语义：skill_create 未启用时返回 None 且不写字段；启用时 auto_trigger 恒 True。
func (d *DeepAdapter) buildSkillCreateRail() *evolution.SkillCreateRail {
	if !d.getSkillCreateEnabled() {
		logger.Debug(logComponent).Msg("skill_create disabled by config, skipping SkillCreateRail")
		return nil
	}

	skillsDir := d.getSkillsDir()
	if skillsDir == "" {
		logger.Warn(logComponent).Msg("buildSkillCreateRail: skillsDir is empty, skipping creation")
		return nil
	}

	rail := evolution.NewSkillCreateRail(skillsDir,
		evolution.WithSkillCreateAutoTrigger(true),
		evolution.WithSkillCreateLanguage(d.getLanguage()),
	)
	logger.Info(logComponent).
		Str("skills_dir", skillsDir).
		Bool("auto_trigger", rail.AutoTrigger()).
		Msg("SkillCreateRail created")
	return rail
}
```

- [ ] **Step 5: plan 模式加 enabled 门控**

`deep_adapter_rails.go:1136-1147` 整段替换为：

```go
	// 8. SkillCreateRail — 计划模式按 skill_create 配置注册/注销
	// 对齐 Python: interface_deep.py:2825-2845
	if d.getSkillCreateEnabled() {
		if d.skillCreateRail == nil {
			rail := d.buildSkillCreateRail()
			if rail != nil && d.instance != nil {
				if err := d.instance.RegisterRail(ctx, rail); err != nil {
					logger.Error(logComponent).Err(err).Msg("Failed to register SkillCreateRail")
				} else {
					d.skillCreateRail = rail
					logger.Info(logComponent).Msg("SkillCreateRail registered (plan mode)")
				}
			}
		}
	} else if d.skillCreateRail != nil {
		if d.instance != nil {
			if err := d.instance.UnregisterRail(ctx, d.skillCreateRail); err != nil {
				logger.Error(logComponent).Err(err).Msg("Failed to unregister SkillCreateRail")
			}
		}
		d.skillCreateRail = nil
		logger.Info(logComponent).Msg("SkillCreateRail unregistered (skill_create=false)")
	}
```

- [ ] **Step 6: 删两处死代码**

删除 `deep_adapter_rails.go` 的 `getMinTeamMembersForCreate`（`:480-492`，改后无调用方）。

删除 `deep_adapter_team.go:62-78` 的 `findSkillCreateRail`（全项目无调用方且引用旧类型）。删完后若 `reflect` 或 `coreevolution` 在该文件不再使用，同步删除 import。

- [ ] **Step 7: 跑测试确认通过**

Run: `CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run 'TestBuildSkillCreateRail|TestBuildAgentRails' -v`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
gofmt -l internal/swarm/server/adapter/
git add internal/swarm/server/adapter/
git commit -m "fix(rails): skill_create 补 enabled 门控并切换到单 Agent SkillCreateRail"
```

---

### Task 8: 组 5 slash 懒加载补注册（转绿 Task 1）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_slash.go:73-101`（签名与实现）、`:39-70`（5 个调用点）

- [ ] **Step 1: 改签名与实现**

```go
// ensureEvolutionRailForSlash 检查演进功能可用性，必要时懒加载并注册 SkillEvolutionRail。
// Python: _ensure_evolution_rail_for_slash() (interface_deep.py:4286-4303)
//
// 与 Python 的偏差（有意）：Python 只构建不注册，导致会话首条消息为 /evolve* 时
// 该实例永不注册、被动演进全灭。这里在构建后立即注册，维持
// 「字段非 nil ⇒ 已注册」的不变量。
func (d *DeepAdapter) ensureEvolutionRailForSlash(ctx context.Context, mode string) string {
	if mode != "agent.plan" {
		return "当前模式下演进功能不可用，仅 agent.plan 模式支持。"
	}
	evolutionConfig, _ := d.configCacheSnapshot()["evolution"].(map[string]any)
	enabled, _ := evolutionConfig["enabled"].(bool)
	if !enabled {
		return "演进功能未启用。"
	}
	if d.skillEvolutionRail == nil {
		rail := d.buildSkillEvolutionRail()
		if rail != nil {
			if d.instance != nil {
				if err := d.instance.RegisterRail(ctx, rail); err != nil {
					logger.Error(logComponent).Err(err).Msg("Failed to register SkillEvolutionRail (slash)")
					return "演进功能初始化失败。"
				}
			}
			d.skillEvolutionRail = rail
		}
	}
	if d.skillEvolutionRail == nil {
		return "演进功能初始化失败。"
	}
	return ""
}
```

- [ ] **Step 2: 更新 5 个调用点**

`deep_adapter_slash.go:39-70` 中 5 处 `d.ensureEvolutionRailForSlash(mode)` 改为 `d.ensureEvolutionRailForSlash(ctx, mode)`。

- [ ] **Step 3: 跑集成用例确认转绿**

Run:
```bash
pgrep -f 'go (build|test)' || CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/ -run TestRailRegistrationSuite -v
```
Expected: PASS（`TestSlashFirstMessage_EvolutionRailRegistered`）

- [ ] **Step 4: 跑包内单测防回归**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/swarm/server/adapter/
git add internal/swarm/server/adapter/deep_adapter_slash.go
git commit -m "fix(rails): slash 懒加载演进 rail 后立即注册，保住字段不变量"
```

---

### Task 9: 组 6 动态 rail 路径写字段

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:1640-1641`
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestBuildDynamicRail_写字段 动态路径构建演进 rail 后必须登记字段。
func TestBuildDynamicRail_写字段(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{"evolution": map[string]any{"enabled": true}}, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"

	rail := d.buildDynamicRail("buildSkillEvolutionRail", map[string]any{})
	if rail == nil {
		t.Fatal("动态路径应构建出 rail")
	}
	if d.skillEvolutionRail == nil {
		t.Fatal("动态路径必须把实例登记到字段，否则注册后无人可追踪")
	}
	if d.skillEvolutionRail != rail {
		t.Fatal("字段与返回值必须是同一实例")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestBuildDynamicRail_写字段 -v`
Expected: FAIL（字段为 nil）

- [ ] **Step 3: 改 buildDynamicRail 分支**

```go
	case "buildSkillEvolutionRail":
		rail := d.buildSkillEvolutionRail()
		// 动态路径（code 模式 rails 配置点名）只负责构建并登记字段；
		// 注册由本次 CreateDeepAgent 装配完成，装配后不变量成立。
		if rail != nil {
			d.skillEvolutionRail = rail
		}
		return rail
```

- [ ] **Step 4: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestBuildDynamicRail -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/swarm/server/adapter/
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go
git commit -m "fix(rails): 动态 rail 路径构建演进 rail 后登记字段"
```

---

### Task 10: 组 7 拆分冷启动与热重载装配

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:3072-3074`
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestGetCurrentAgentRails_仅skill重建其余复用 热重载只新建 skillRail，
// 其余 5 个 rail 复用现有实例，有状态 rail 不进列表。
func TestGetCurrentAgentRails_仅skill重建其余复用(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"enabled": true, "auto_scan": false},
	}, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"
	d.instanceOverrides = map[string]any{}

	reused := []sainterfaces.AgentRail{
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
	}
	d.contextAssembleRail = reused[0]
	d.contextProcessorRail = reused[1]
	d.memoryRail = reused[2]
	d.avatarRail = reused[3]
	d.permissionRail = reused[4]

	railsList := d.getCurrentAgentRails(map[string]any{}, map[string]any{})

	if d.skillRail == nil {
		t.Fatal("skillRail 应被重建")
	}
	for i, want := range reused {
		found := false
		for _, r := range railsList {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("复用实例 #%d 应出现在热重载列表中", i)
		}
	}
	// 有状态 rail 不得入列（否则会被重建并丢内存状态）
	for _, r := range railsList {
		if _, ok := r.(*rails.TaskPlanningRail); ok {
			t.Fatal("TaskPlanningRail 不得进入热重载列表")
		}
		if _, ok := r.(*commrails.JiuClawStreamEventRail); ok {
			t.Fatal("StreamEventRail 不得进入热重载列表")
		}
	}
}

- [ ] **Step 2: 补齐测试文件 import**

在 `deep_adapter_evolution_rail_test.go` 的 import 块追加：

```go
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
```

- [ ] **Step 3: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestGetCurrentAgentRails_仅skill重建其余复用 -v`
Expected: FAIL（当前 getCurrentAgentRails 走 buildAgentRails，列表里没有 5 个复用实例）

- [ ] **Step 4: 重写 getCurrentAgentRails**

```go
// getCurrentAgentRails 获取热重载时需要重新装配的 rail 列表。
// Python: _get_current_agent_rails(config, config_base) (interface_deep.py:2304-2352)
//
// 与 buildAgentRails 的冷启动语义完全不同：
//  1. 就地更新：仅演进 rail（换 LLM 与 auto_scan），实例与注册状态不动；
//  2. 新建：仅 skillRail（include_tools 取决于 profile，靠重建切换）；
//  3. 复用现有实例重新 init：contextAssemble / contextProcessor / memory / avatar / permission；
//  4. 其余 rail（runtimePrompt/responsePrompt/streamEvent/taskPlanning/security/heartbeat/
//     subagent/filesystem/evolution/skillCreate 等）不进列表，保持原实例与原注册状态。
//     全量重建会清空 StreamEventRail 的 abortRequested 与 TaskPlanningRail 的 usageRecords。
func (d *DeepAdapter) getCurrentAgentRails(config map[string]any, configBase map[string]any) []sainterfaces.AgentRail {
	// 1. 就地更新（必须在列表构造前，对齐 Python :2314-2328）
	d.updateSkillEvolutionRail(config)

	// 2. 唯一新建项
	d.skillRail = d.buildSkillRailWithIncludeTools(d.skillIncludeToolsForProfile(d.instanceOverrides))

	// 3. 复用现有实例（非 nil 才入列）
	railsList := make([]sainterfaces.AgentRail, 0, 6)
	if d.skillRail != nil {
		railsList = append(railsList, d.skillRail)
	}
	if d.contextAssembleRail != nil {
		railsList = append(railsList, d.contextAssembleRail)
	}
	if d.contextProcessorRail != nil {
		railsList = append(railsList, d.contextProcessorRail)
	}
	if d.memoryRail != nil {
		railsList = append(railsList, d.memoryRail)
	}
	if d.avatarRail != nil {
		railsList = append(railsList, d.avatarRail)
	}
	if d.permissionRail != nil {
		railsList = append(railsList, d.permissionRail)
	}

	logger.Info(logComponent).Int("rails_count", len(railsList)).Msg("getCurrentAgentRails completed")
	return railsList
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -run TestGetCurrentAgentRails -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
gofmt -l internal/swarm/server/adapter/
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_evolution_rail_test.go
git commit -m "fix(rails): 拆分热重载装配，仅重建 skillRail 并复用有状态 rail"
```

---

### Task 11: 补齐剩余集成用例

**Files:**
- Modify: `tests/integration/swarm/server/adapter/deep_adapter_rail_registration_test.go`

- [ ] **Step 1: 追加 4 个用例**

```go
// TestPlanMessage_EvolutionRailRegistered 计划模式消息后注册且幂等。
func (s *RailRegistrationSuite) TestPlanMessage_EvolutionRailRegistered() {
	d := s.newAdapter("agent", evolutionExtra(true, false, false))
	s.Require().Equal(0, s.countEvolutionRails(d), "冷启动（agent 模式）不应注册演进 rail")

	for i := 0; i < 2; i++ {
		req := schema.NewAgentRequest("req-plan", "itest-channel", schema.ReqMethodChatSend,
			s.jsonParams(map[string]any{"query": "你好", "mode": "agent.plan"}),
			schema.WithAgentSessionID("itest-rail"))
		_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
		s.Require().NoError(err)
		s.Equal(1, s.countEvolutionRails(d), "第 %d 条计划消息后应恰好注册 1 个", i+1)
	}
}

// TestHotReload_EvolutionRailInPlace 热重载不重建演进 rail，只刷新开关。
func (s *RailRegistrationSuite) TestHotReload_EvolutionRailInPlace() {
	d := s.newAdapter("agent.plan", evolutionExtra(true, false, false))
	rails := d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{}))
	s.Require().Len(rails, 1, "计划模式冷启动后应已注册演进 rail")
	before, ok := rails[0].(*coreevolution.SkillEvolutionRail)
	s.Require().True(ok)
	s.False(before.AutoScan(), "初始 auto_scan 应为 false")

	s.env.WriteConfigYAML(s.T(), evolutionExtra(true, true, false))
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	rails = d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{}))
	s.Require().Len(rails, 1)
	after, ok := rails[0].(*coreevolution.SkillEvolutionRail)
	s.Require().True(ok)
	s.Same(before, after, "热重载不得替换演进 rail 实例")
	s.True(after.AutoScan(), "auto_scan 应就地刷新为 true")
}

// TestHotReload_PreservesStatefulRails 有状态 rail 不得被热重载重建。
func (s *RailRegistrationSuite) TestHotReload_PreservesStatefulRails() {
	d := s.newAdapter("agent.plan", evolutionExtra(true, false, false))
	req := schema.NewAgentRequest("req-plan", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "你好", "mode": "agent.plan"}),
		schema.WithAgentSessionID("itest-rail"))
	_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
	s.Require().NoError(err)

	planBefore := d.Instance().FindRailsByType(reflect.TypeOf(&rails.TaskPlanningRail{}))
	s.Require().Len(planBefore, 1)
	skillBefore := d.Instance().FindRailsByType(reflect.TypeOf(&skillrails.SkillUseRail{}))

	s.env.WriteConfigYAML(s.T(), evolutionExtra(true, false, false))
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	planAfter := d.Instance().FindRailsByType(reflect.TypeOf(&rails.TaskPlanningRail{}))
	s.Require().Len(planAfter, 1)
	s.Same(planBefore[0], planAfter[0], "TaskPlanningRail 不得被重建")

	skillAfter := d.Instance().FindRailsByType(reflect.TypeOf(&skillrails.SkillUseRail{}))
	s.NotEmpty(skillAfter, "skillRail 应仍注册")
	if len(skillBefore) == 1 && len(skillAfter) == 1 {
		s.NotSame(skillBefore[0], skillAfter[0], "skillRail 是唯一重建项")
	}
}

// TestSkillCreate_EnabledRegistersSingleAgentRail skill_create=true 挂单 Agent 版。
func (s *RailRegistrationSuite) TestSkillCreate_EnabledRegistersSingleAgentRail() {
	d := s.newAdapter("agent.plan", evolutionExtra(true, false, true))
	s.Equal(1, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillCreateRail{}))),
		"应注册单 Agent SkillCreateRail")
	s.Equal(0, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.TeamSkillCreateRail{}))),
		"不应注册 TeamSkillCreateRail")
}

// TestSkillCreate_DisabledThenHotEnabled 关闭时不注册，热重载打开后生效。
func (s *RailRegistrationSuite) TestSkillCreate_DisabledThenHotEnabled() {
	d := s.newAdapter("agent.plan", evolutionExtra(true, false, false))
	s.Equal(0, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillCreateRail{}))),
		"skill_create=false 不应注册")

	s.env.WriteConfigYAML(s.T(), evolutionExtra(true, false, true))
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	req := schema.NewAgentRequest("req-plan", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "你好", "mode": "agent.plan"}),
		schema.WithAgentSessionID("itest-rail"))
	_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
	s.Require().NoError(err)
	s.Equal(1, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillCreateRail{}))),
		"热重载打开 skill_create 后应注册")
}
```

同时在 import 块补：

```go
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
```

- [ ] **Step 2: 跑整套集成用例**

Run:
```bash
pgrep -f 'go (build|test)' || CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/ -run TestRailRegistrationSuite -v
```
Expected: PASS（5 个用例）

- [ ] **Step 3: 提交**

```bash
gofmt -l tests/integration/swarm/server/adapter/
git add tests/integration/swarm/server/adapter/deep_adapter_rail_registration_test.go
git commit -m "test(rails): 补齐计划模式/热重载/skill_create 注册集成用例"
```

---

### Task 12: 全量门禁与 CI

- [ ] **Step 1: 本地门禁**

Run:
```bash
gofmt -l internal/ tests/ resources/
CGO_ENABLED=1 go vet ./internal/swarm/server/adapter/ ./internal/agentcore/harness/rails/evolution/
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ ./internal/agentcore/harness/rails/evolution/ -count=1
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/ -count=1
go run ./cmd/integration-audit -verify
```
Expected: gofmt 无输出；三条测试全 PASS；audit 无新增不合规目录。

- [ ] **Step 2: 推送并跟踪 CI**

```bash
git push
gh run list --limit 3
gh run watch <run-id>
```
Expected: Build / Lint / Unit Test / Integration Test 四个 job 全绿（规则 6）；若有红灯，按失败 job 修复后重新推送直至全绿。

- [ ] **Step 3: 收尾提交（如有 CI 修复）**

```bash
git add -A && git commit -m "fix(rails): 修复 CI 反馈"
git push
```

---

## 自检结论（写计划后复核）

- **spec 覆盖**：组 1→Task 2；组 2a→Task 3；组 2b→Task 4；组 4→Task 5；组 3a→Task 6；组 3b→Task 7；组 5→Task 8；组 6→Task 9；组 7→Task 10；测试矩阵→Task 1/2/4/5/6/7/9/10/11；CI→Task 12。
- **占位符扫描**：无 TBD/TODO；所有步骤含可执行代码或确切命令。
- **类型一致性**：`buildSkillEvolutionRail` 返回 `*evolution.SkillEvolutionRail`（Task 3 起）、`buildSkillCreateRail` 返回 `*evolution.SkillCreateRail`（Task 7 起）、`getCurrentAgentRails` 返回 `[]sainterfaces.AgentRail`（Task 10）、`SkillCreateRail.AutoTrigger()` 与 `SkillEvolutionRail.ModelName()`（Task 4/6）在后续任务与测试中一致引用。
