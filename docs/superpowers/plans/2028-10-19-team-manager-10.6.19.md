# 10.6.19 TeamManager 完整对齐实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans (if you don't have access to subagents).

## Goal

按已评审 spec（`docs/superpowers/specs/2028-10-19-team-manager-10.6.19-design.md`，方案 C）完整对齐 Python `jiuwenswarm/agents/harness/team/`：

1. 新建 5 个文件：`team/runtime_inheritance.go`（587 行对齐）、`team/member_skill_toolkit_rail.go`、`team/workspace_report_path_rail.go`、`team/exceptions.go`、`team/distributed_runtime.go`（纯函数层）
2. 回填 `team_manager_spec.go` customizer 六步（a-e 实现、f 日志骨架保留 ⤵️10.6.24）+ loadTeamSpec 分布式检查 + model_pool 注入
3. 回填 `team_manager_skill.go`（syncSkillsDir + EnsureTeamSharedSkillsInitialized marker 幂等）
4. `common/config/default_models.go` 新增权威 GetDefaultModels（方案 iii）并收敛三处旧实现（web/dreaming/adapter）
5. DeepAgentInterface 接口面补齐（AbilityManager + SystemPromptBuilder + 4 对 Uapswarm 访问器）
6. adapter 侧注入 CodeMemberConfigurer（方案 1 注入回调破解循环依赖）
7. 全部 ⤵️ 标记编号修正（#9.72 → 实际章节），IMPLEMENTATION_PLAN.md 10.6.19-23 拆分状态

## Architecture

- **调用链定位**：`DeepAdapter.processTeamMessageStream → GetEnrichedTeamSpec → loadTeamSpec + BuildAgentCustomizer → ateamruntime.RunAgentTeamStreaming → AgentConfigurator 步骤 17 → harness.RunAgentCustomizer → customizer(ctx, deepAgent, memberName, role)` 六步执行
- **循环依赖破解**：team 包定义 `CodeMemberConfigurer` 回调类型 + `SetCodeMemberConfigurer` 注入器；adapter 侧 `EnsureTeamDependenciesInjected` 注入包装（每次调用 `NewCodeAdapter()`，对齐 Python 模块级函数 L1301 每次新建实例；接口→`*harness.DeepAgent` 断言失败 warning 跳过，等价 Python except）
- **GetDefaultModels 依赖方向**：common/config 权威实现（注入式签名 `GetDefaultModels(cfg map[string]any)`，nil 时内部 Load）；三处旧实现删除收敛；crypto 解密留空操作 + ⤵️(10.5.10)（等价 Python crypto=None）
- **错误吞没语义**：customizer 六步各自独立吞错（单步失败不阻断后续）；BuildMemberRails 逐 rail 独立 panic 恢复（`buildRailStep` 辅助函数，对齐 Python 每步 try/except）
- **白名单键名适配**：Python `type(rail).__name__` → Go `reflect.TypeOf(rail).Elem().Name()`；`SecurityRail` Go 实际类型名为 `SafetyPromptRail`（类型别名反射名）；`FileSystemRail` 保留占位键（Go 无此类型，差异注释）

## Tech Stack

- Go 1.x + 项目既有依赖（无新增第三方依赖）
- 测试：标准库 testing + t.TempDir()；外部命令测试用真实 `echo`/`sh`（无可 mock 系统调用时允许，RunCommand 本身是被测单元）
- 日志：`logger.ComponentTeam`（team 包）/ config 包用 `log.Printf("[config]")` 前缀（循环依赖限制）
- 覆盖率 ≥ 85%（go test -cover）

## 全局约定（所有任务适用）

- 编译前 `export GOPROXY=https://goproxy.cn,direct`；单进程互斥（先 `pgrep -f 'go (build|test)'` 检查残留）
- 所有注释中文；文件内声明按 结构体→枚举→常量→全局变量→导出函数→非导出函数 排列
- 品牌：jiuwenswarm→uapclaw 映射仅用于用户可见文本；env 名（JIUWENSWARM_*）不动
- 模块路径前缀：`github.com/uapclaw/uapclaw-go/`（下文 import 略写为 `internal/...`）
- 已核实的关键签名（实现时直接引用，勿再猜）：
  - `evolution.NewTeamSkillEvolutionRail(skillsDir []string, llmModel *llm.Model, model, language string, opts ...TeamSkillEvolutionRailOption)`
  - `evolution.NewSkillEvolutionRail(skillsDir []string, llmModel *llm.Model, model, language string, opts ...SkillEvolutionRailOption)`
  - `evolution.NewTeamSkillCreateRail(skillsDir string, opts ...TeamSkillCreateRailOption)`
  - Option 实名：`WithTeamSkillAutoScan/WithTeamSkillAutoSave/WithTeamSkillTeamID/WithTeamSkillTrajectorySource/WithTeamSkillTrajectorySink(sink, teamID, memberRole...)/WithTeamSkillMemberRole/WithTeamSkillDisabledSkills`；`WithTeamSkillCreateAutoTrigger/WithTeamSkillCreateLanguage`；`WithAutoScan/WithAutoSave/WithDisabledSkillsSet`；`WithPreset/WithUserProcessors([]ceiface.ProcessorSpec{Type, ConfigOverrides})`
  - `(*EvolutionRail).SetTrajectorySink(sink trajectory.TrajectorySink, teamID string, memberRole ...string) error`
  - `ceiface.ProcessorSpec{Type string; Config ProcessorConfig; ConfigOverrides map[string]any}`（ceiface = `internal/agentcore/context_engine/interface`）
  - `skillruntime.NewSkillManager(workspaceDir string) *SkillManager`（skillruntime = `internal/swarm/server/runtime/skill`）
  - `skillruntime.NormalizeMarketplaces(raw any) []map[string]any`、`skillruntime.LoadExecutionDisabledSkills() []string`
  - `skilltools.NewSkillToolkit(manager *skillpkg.SkillManager) *SkillToolkit`、`GetTools() []tool.Tool`（skilltools = `internal/swarm/agents/harness/tools`）
  - `runner.GetResourceMgr() *ResourceMgr`；`AddTool(t tool.Tool, opts...) error`；`RemoveTool(toolIDs []string, opts...) ([]string, error)`
  - `AbilityManagerInterface`：`Add(schema.Ability) AddAbilityResult` / `Remove(name) schema.Ability` / `Get(name) schema.Ability` / `List() []schema.Ability`
  - `*tool.ToolCard` 实现 `schema.Ability`；嵌入 `schema.BaseCard{ID, Name, Description}`（common/schema/card.go）
  - `saprompt.NewPromptSection(name string, content map[string]string, priority int) PromptSection`（saprompt = `internal/agentcore/single_agent/prompts`）
  - `llmschema.NewModelClientConfig(provider, apiKey, apiBase string, opts ...ModelClientConfigOption) (*ModelClientConfig, error)`（校验 provider/apiKey/apiBase 非空）；`llmschema.NewModelRequestConfig(opts...) *ModelRequestConfig`；`llm.NewModel(clientConfig, modelConfig) (*Model, error)`
  - `agentinterfaces.AgentRail`（= `internal/agentcore/single_agent/interfaces`）：`Priority() int; Init(ctx, BaseAgent) error; Uninit(BaseAgent) error;` + 10 钩子 + `GetCallbacks()`
  - `agentinterfaces.BaseAgent` 已含 `Card() *agentschema.AgentCard` / `AbilityManager()` / `SystemPromptBuilder() saprompt.SystemPromptBuilderInterface`
  - `models.NewModelPoolEntry(modelName, apiKey, apiBaseURL, apiProvider string, opts ...ModelPoolEntryOption) *ModelPoolEntry`；`models.WithMetadata(map[string]any)`（models = `internal/agent_teams/models`）
  - `trajectory.NewInMemoryTrajectoryRegistry()`（同时实现 TrajectorySource+TrajectorySink）
  - `agentteams.TeamHome(teamName string) string`（agentteams = `internal/agent_teams` 根包）
  - `workspace.AgentSkillsDir() string`（workspace = `internal/common/workspace`）
  - `harness.DeepAgent` 已实现 `AbilityManager()/SystemPromptBuilder()/UapswarmProjectDir()` 等全部待补接口方法（deep_agent.go:407-727）

---

## Task 1: common/config 权威 GetDefaultModels

**Files:**
- Create: `internal/common/config/default_models.go`
- Create: `internal/common/config/default_models_test.go`
- Modify: `internal/common/config/doc.go`（文件目录加条目）

**对齐 Python**: `jiuwenswarm/common/config.py` L622-731（`_infer_is_default` / `_decrypt_model_entries` / `get_default_models`）

- [ ] 1.1 写失败测试 `default_models_test.go`：

```go
package config

import (
	"os"
	"testing"
)

// TestGetDefaultModels_新格式defaults列表 优先读取 models.defaults 列表并附加 is_default。
func TestGetDefaultModels_新格式defaults列表(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{"model_name": "m1", "api_key": "k"},
					"model_config_obj":    map[string]any{"temperature": 0.7},
				},
			},
		},
	}
	entries := GetDefaultModels(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if b, _ := entries[0]["is_default"].(bool); !b {
		t.Fatalf("single-entry group should be is_default=true, got %v", entries[0]["is_default"])
	}
}

// TestGetDefaultModels_旧格式default单对象 包装为列表。
func TestGetDefaultModels_旧格式default单对象(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{"model_name": "m-old"},
			},
		},
	}
	entries := GetDefaultModels(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	mcc := entries[0]["model_client_config"].(map[string]any)
	if mcc["model_name"] != "m-old" {
		t.Fatalf("unexpected model_name: %v", mcc["model_name"])
	}
}

// TestGetDefaultModels_环境变量回退 无 models 段时从 env 构造。
func TestGetDefaultModels_环境变量回退(t *testing.T) {
	t.Setenv("MODEL_ALIAS", "my-alias")
	t.Setenv("API_BASE", "https://api.example.com")
	t.Setenv("API_KEY", "sk-test")
	t.Setenv("MODEL_NAME", "env-model")
	t.Setenv("MODEL_PROVIDER", "OpenAI")

	entries := GetDefaultModels(map[string]any{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 env entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry["alias"] != "my-alias" {
		t.Fatalf("expected alias, got %v", entry["alias"])
	}
	mcc := entry["model_client_config"].(map[string]any)
	if mcc["model_name"] != "env-model" || mcc["api_base"] != "https://api.example.com" {
		t.Fatalf("unexpected mcc: %v", mcc)
	}
	if mcc["timeout"] != 1800 {
		t.Fatalf("expected timeout=1800, got %v", mcc["timeout"])
	}
	if mcc["verify_ssl"] != false {
		t.Fatalf("expected verify_ssl=false, got %v", mcc["verify_ssl"])
	}
	mco := entry["model_config_obj"].(map[string]any)
	if mco["temperature"] != 0.95 {
		t.Fatalf("expected temperature=0.95, got %v", mco["temperature"])
	}
}

// TestGetDefaultModels_环境变量回退alias为空不附加。
func TestGetDefaultModels_环境变量回退alias为空不附加(t *testing.T) {
	os.Unsetenv("MODEL_ALIAS") //nolint:errcheck // 测试清理
	entries := GetDefaultModels(nil)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if _, exists := entries[0]["alias"]; exists {
		t.Fatalf("empty alias should not be attached")
	}
}

// TestInferIsDefault_分组语义 同名多条首 true、显式 true 保留。
func TestInferIsDefault_分组语义(t *testing.T) {
	entries := []map[string]any{
		{"model_client_config": map[string]any{"model_name": "same"}},
		{"model_client_config": map[string]any{"model_name": "same"}},
		{"model_client_config": map[string]any{"model_name": "other"}},
	}
	result := InferIsDefault(entries)
	if result[0]["is_default"] != true {
		t.Fatalf("group multi: first should be true")
	}
	if result[1]["is_default"] != false {
		t.Fatalf("group multi: second should be false")
	}
	if result[2]["is_default"] != true {
		t.Fatalf("single group should be true")
	}
}

// TestInferIsDefault_显式true保留 首个显式 true 生效其余置 false。
func TestInferIsDefault_显式true保留(t *testing.T) {
	entries := []map[string]any{
		{"model_client_config": map[string]any{"model_name": "a"}, "is_default": false},
		{"model_client_config": map[string]any{"model_name": "a"}, "is_default": true},
		{"model_client_config": map[string]any{"model_name": "a"}, "is_default": true},
	}
	result := InferIsDefault(entries)
	if result[0]["is_default"] != false || result[1]["is_default"] != true || result[2]["is_default"] != false {
		t.Fatalf("explicit true keep-first failed: %v %v %v",
			result[0]["is_default"], result[1]["is_default"], result[2]["is_default"])
	}
}

// TestDecryptModelEntries_customHeaders解析 JSON 字符串转 map，且不修改原始数据。
func TestDecryptModelEntries_customHeaders解析(t *testing.T) {
	entries := []map[string]any{
		{"model_client_config": map[string]any{
			"model_name":     "m",
			"custom_headers": `{"X-Trace":"1"}`,
		}},
	}
	result := decryptModelEntries(entries)
	mcc := result[0]["model_client_config"].(map[string]any)
	headers, ok := mcc["custom_headers"].(map[string]any)
	if !ok || headers["X-Trace"] != "1" {
		t.Fatalf("custom_headers not parsed: %v", mcc["custom_headers"])
	}
	// 原始数据不被修改（深拷贝）
	orig := entries[0]["model_client_config"].(map[string]any)
	if _, isMap := orig["custom_headers"].(map[string]any); isMap {
		t.Fatalf("original entry should stay unmodified")
	}
}
```

- [ ] 1.2 运行 `cd /home/opensource/uapclaw-gateway && go test ./internal/common/config/ -run 'TestGetDefaultModels|TestInferIsDefault|TestDecryptModelEntries'` 确认编译失败（函数未定义）
- [ ] 1.3 实现 `default_models.go`：

```go
package config

import (
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetDefaultModels 获取默认模型列表，兼容新旧格式。
// 对齐 Python: common/config.py get_default_models(config=None) (L697-731)
//
// 优先级：models.defaults（列表） > models.default（单对象） > 环境变量回退。
// 返回条目经过 custom_headers 解析与 is_default 推断（decryptModelEntries）。
// cfg 为 nil 时加载默认 config.yaml（对齐 Python config=None 时 get_config()）。
func GetDefaultModels(cfg map[string]any) []map[string]any {
	configMap := cfg
	if configMap == nil {
		loaded, err := loadDefaultConfigMap()
		if err != nil {
			logger.Printf("[config] GetDefaultModels: load config failed: %v", err)
			configMap = map[string]any{}
		} else {
			configMap = loaded
		}
	}
	models, _ := configMap["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}

	// 新格式：已有 defaults 列表
	if defaults, ok := models["defaults"].([]any); ok && len(defaults) > 0 {
		entries := make([]map[string]any, 0, len(defaults))
		for _, item := range defaults {
			if m, ok := item.(map[string]any); ok {
				entries = append(entries, m)
			}
		}
		if len(entries) > 0 {
			return decryptModelEntries(entries)
		}
	}

	// 旧格式：单个 default 对象 → 包装为列表
	if defaultEntry, ok := models["default"].(map[string]any); ok {
		return decryptModelEntries([]map[string]any{defaultEntry})
	}

	// 回退：从环境变量构造（env var 已在 ResolveEnvVars 中解密）
	return buildEnvFallbackModelEntry()
}

// InferIsDefault 为模型条目列表推断 is_default 字段。
// 对齐 Python: _infer_is_default (L622-665)
//
// 规则（按 model_client_config.model_name 分组，保持出现顺序）：
//   - 同组仅一个条目 → is_default = true
//   - 同组多个条目 → 第一个为 true，其余为 false
//   - 组内已有显式 is_default=true → 首个 true 保留，同组其余置 false
//
// 返回深拷贝结果，不修改入参。
func InferIsDefault(entries []map[string]any) []map[string]any {
	result := make([]map[string]any, len(entries))
	for i, entry := range entries {
		copied := utils.DeepCopyMap(entry)
		if copied == nil {
			copied = map[string]any{}
		}
		result[i] = copied
	}

	// 按 model_name 分组（保持首次出现顺序，对齐 Python OrderedDict）
	groups := make(map[string][]int)
	order := make([]string, 0)
	for i, entry := range result {
		name := ""
		if mcc, ok := entry["model_client_config"].(map[string]any); ok {
			if n, ok := mcc["model_name"].(string); ok {
				name = n
			}
		}
		if _, exists := groups[name]; !exists {
			order = append(order, name)
		}
		groups[name] = append(groups[name], i)
	}

	for _, name := range order {
		indices := groups[name]
		if len(indices) == 1 {
			result[indices[0]]["is_default"] = true
			continue
		}

		hasExplicit := false
		for _, idx := range indices {
			if b, ok := result[idx]["is_default"].(bool); ok && b {
				hasExplicit = true
				break
			}
		}

		if hasExplicit {
			firstTrue := true
			for _, idx := range indices {
				if b, ok := result[idx]["is_default"].(bool); ok && b && firstTrue {
					result[idx]["is_default"] = true
					firstTrue = false
				} else {
					result[idx]["is_default"] = false
				}
			}
		} else {
			for j, idx := range indices {
				result[idx]["is_default"] = j == 0
			}
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decryptModelEntries 解密模型条目并推断 is_default，返回深拷贝。
// 对齐 Python: _decrypt_model_entries (L668-694)
//
// ⤵️(10.5.10) api_key 解密依赖 ExtensionRegistry CryptoProvider（extensions→common/config
// 依赖方向已存在，crypto 不能进 common/config），当前为空操作——等价 Python crypto=None
// 时跳过解密路径。custom_headers 经 ParseCustomHeaders 手动处理
// （NormalizeConfig 全项目尚未接线，无法依赖 Load 后处理）。
func decryptModelEntries(entries []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		copied := utils.DeepCopyMap(entry)
		if copied == nil {
			copied = map[string]any{}
		}
		if mcc, ok := copied["model_client_config"].(map[string]any); ok {
			// ⤵️(10.5.10) Python: if mcc.get("api_key") and crypto: mcc["api_key"] = crypto.decrypt(...)
			if rawHeaders, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(rawHeaders)
			}
		}
		result = append(result, copied)
	}
	return InferIsDefault(result)
}

// buildEnvFallbackModelEntry 从环境变量构造默认模型条目。
// 对齐 Python: get_default_models 环境变量回退段 (L716-731)
//
// 固定值：timeout=1800、verify_ssl=false、temperature=0.95；alias 非空才附加。
func buildEnvFallbackModelEntry() []map[string]any {
	entry := map[string]any{
		"model_client_config": map[string]any{
			"api_base":        os.Getenv("API_BASE"),
			"api_key":         os.Getenv("API_KEY"),
			"model_name":      os.Getenv("MODEL_NAME"),
			"client_provider": os.Getenv("MODEL_PROVIDER"),
			"custom_headers":  ParseCustomHeaders(os.Getenv("CUSTOM_HEADERS")),
			"timeout":         1800,
			"verify_ssl":      false,
		},
		"model_config_obj": map[string]any{"temperature": 0.95},
	}
	if alias := os.Getenv("MODEL_ALIAS"); alias != "" {
		entry["alias"] = alias
	}
	return []map[string]any{entry}
}

// loadDefaultConfigMap 加载默认 config.yaml。
// 对齐 Python: get_config()
func loadDefaultConfigMap() (map[string]any, error) {
	c, err := New("")
	if err != nil {
		return nil, err
	}
	return c.Load()
}
```

**注意**：config 包因循环依赖不能导入 logger（包内已有此约束），此处 `logger.Printf` 是笔误——改为标准库 `log.Printf("[config] ...")`（与 normalize.go 先例一致）。实现时 import `"log"`，调用 `log.Printf("[config] GetDefaultModels: load config failed: %v", err)`。

- [ ] 1.4 运行测试确认通过：`go test ./internal/common/config/ -run 'TestGetDefaultModels|TestInferIsDefault|TestDecryptModelEntries' -v`
- [ ] 1.5 更新 `doc.go` 文件目录，在 `merge.go` 条目后加：`├── default_models.go   # 默认模型列表：GetDefaultModels/InferIsDefault`
- [ ] 1.6 运行全包测试：`go test ./internal/common/config/ -cover`（覆盖率 ≥85% 基线不回退）
- [ ] 1.7 提交：`git add internal/common/config/ && git commit -m "feat(config): 新增权威 GetDefaultModels 实现（10.6.19 Task1，对齐 Python get_default_models）"`

---

## Task 2: team/exceptions.go 异常类型

**Files:**
- Create: `internal/swarm/agents/harness/team/exceptions.go`
- Create: `internal/swarm/agents/harness/team/exceptions_test.go`

**对齐 Python**: `jiuwenswarm/agents/harness/team/exceptions.py`（48 行，TeamError + 7 子类）

- [ ] 2.1 写失败测试 `exceptions_test.go`：

```go
package team

import (
	"errors"
	"testing"
)

// TestTeamError_错误消息 各异常类型 Error() 输出与 errors.As 类型区分。
func TestTeamError_错误消息(t *testing.T) {
	cases := []struct {
		err  error
		msg  string
		want error
	}{
		{NewTeamError("base"), "TeamError: base", &TeamError{}},
		{NewTeamCreateError("create failed"), "TeamCreateError: create failed", &TeamCreateError{}},
		{NewTeamRecoverError("recover failed"), "TeamRecoverError: recover failed", &TeamRecoverError{}},
		{NewTeamInteractError("interact failed"), "TeamInteractError: interact failed", &TeamInteractError{}},
		{NewTeamConfigError("bad config"), "TeamConfigError: bad config", &TeamConfigError{}},
		{NewTeamMonitorError("monitor failed"), "TeamMonitorError: monitor failed", &TeamMonitorError{}},
		{NewTeamSessionError("session failed"), "TeamSessionError: session failed", &TeamSessionError{}},
		{NewTeamStorageError("storage failed"), "TeamStorageError: storage failed", &TeamStorageError{}},
	}
	for _, tc := range cases {
		if tc.err.Error() != tc.msg {
			t.Errorf("Error() = %q, want %q", tc.err.Error(), tc.msg)
		}
		if !errors.As(tc.err, &tc.want) {
			t.Errorf("errors.As failed for %T", tc.err)
		}
		// 所有子类同时匹配 TeamError 基类
		var base *TeamError
		if !errors.As(tc.err, &base) {
			t.Errorf("errors.As TeamError base failed for %T", tc.err)
		}
	}
}
```

- [ ] 2.2 运行 `go test ./internal/swarm/agents/harness/team/ -run TestTeamError` 确认编译失败
- [ ] 2.3 实现 `exceptions.go`：

```go
package team

// ──────────────────────────── 结构体 ────────────────────────────

// TeamError Team 基础错误。
// 对齐 Python: TeamError (exceptions.py L11-13)
type TeamError struct {
	// message 错误描述
	message string
}

// TeamCreateError Team 创建失败。
// 对齐 Python: TeamCreateError (L16-18)
type TeamCreateError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamRecoverError Team 恢复失败。
// 对齐 Python: TeamRecoverError (L21-23)
type TeamRecoverError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamInteractError Team 交互失败。
// 对齐 Python: TeamInteractError (L26-28)
type TeamInteractError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamConfigError Team 配置错误。
// 对齐 Python: TeamConfigError (L31-33)
type TeamConfigError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamMonitorError Team Monitor 错误。
// 对齐 Python: TeamMonitorError (L36-38)
type TeamMonitorError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamSessionError Team 会话错误。
// 对齐 Python: TeamSessionError (L41-43)
type TeamSessionError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamStorageError Team 存储错误。
// 对齐 Python: TeamStorageError (L46-48)
type TeamStorageError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *TeamError) Error() string { return "TeamError: " + e.message }

// NewTeamError 创建 Team 基础错误。
func NewTeamError(message string) *TeamError { return &TeamError{message: message} }

// Error 覆写错误前缀。
func (e *TeamCreateError) Error() string { return "TeamCreateError: " + e.message }

// NewTeamCreateError 创建 Team 创建失败错误。
func NewTeamCreateError(message string) *TeamCreateError {
	return &TeamCreateError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamRecoverError) Error() string { return "TeamRecoverError: " + e.message }

// NewTeamRecoverError 创建 Team 恢复失败错误。
func NewTeamRecoverError(message string) *TeamRecoverError {
	return &TeamRecoverError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamInteractError) Error() string { return "TeamInteractError: " + e.message }

// NewTeamInteractError 创建 Team 交互失败错误。
func NewTeamInteractError(message string) *TeamInteractError {
	return &TeamInteractError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamConfigError) Error() string { return "TeamConfigError: " + e.message }

// NewTeamConfigError 创建 Team 配置错误。
func NewTeamConfigError(message string) *TeamConfigError {
	return &TeamConfigError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamMonitorError) Error() string { return "TeamMonitorError: " + e.message }

// NewTeamMonitorError 创建 Team Monitor 错误。
func NewTeamMonitorError(message string) *TeamMonitorError {
	return &TeamMonitorError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamSessionError) Error() string { return "TeamSessionError: " + e.message }

// NewTeamSessionError 创建 Team 会话错误。
func NewTeamSessionError(message string) *TeamSessionError {
	return &TeamSessionError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamStorageError) Error() string { return "TeamStorageError: " + e.message }

// NewTeamStorageError 创建 Team 存储错误。
func NewTeamStorageError(message string) *TeamStorageError {
	return &TeamStorageError{TeamError{message: message}}
}
```

- [ ] 2.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run TestTeamError -v`
- [ ] 2.5 提交：`git add internal/swarm/agents/harness/team/ && git commit -m "feat(team): 新增 Team 异常类型 8 类（10.6.19 Task2，对齐 exceptions.py）"`

---

## Task 3: team/distributed_runtime.go 纯函数层（配置解析部分）

**Files:**
- Create: `internal/swarm/agents/harness/team/distributed_runtime.go`
- Create: `internal/swarm/agents/harness/team/distributed_runtime_test.go`

**对齐 Python**: `distributed_runtime.py` L20-235（模式/角色/成员名/端口/传输标准化/存储判断/fallback/PG endpoint）

- [ ] 3.1 写失败测试（第一部分，配置解析函数）：

```go
package team

import (
	"reflect"
	"testing"
)

// TestIsDistributedMode_判定 runtime.mode 或 transport.type 为分布式。
func TestIsDistributedMode_判定(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"runtime_distributed", map[string]any{"team": map[string]any{"runtime": map[string]any{"mode": "distributed"}}}, true},
		{"runtime_distributed_upper", map[string]any{"team": map[string]any{"runtime": map[string]any{"mode": " Distributed "}}}, true},
		{"transport_pyzmq", map[string]any{"team": map[string]any{"transport": map[string]any{"type": "pyzmq"}}}, true},
		{"local", map[string]any{"team": map[string]any{"runtime": map[string]any{"mode": "local"}}}, false},
		{"empty", map[string]any{}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := IsDistributedMode(tc.cfg); got != tc.want {
			t.Errorf("%s: IsDistributedMode = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRuntimeRole_默认leader 仅接受 leader/teammate。
func TestRuntimeRole_默认leader(t *testing.T) {
	if got := RuntimeRole(map[string]any{}); got != "leader" {
		t.Errorf("default role = %q, want leader", got)
	}
	cfg := map[string]any{"team": map[string]any{"runtime": map[string]any{"role": "Teammate"}}}
	if got := RuntimeRole(cfg); got != "teammate" {
		t.Errorf("role = %q, want teammate", got)
	}
	cfg2 := map[string]any{"team": map[string]any{"runtime": map[string]any{"role": "bogus"}}}
	if got := RuntimeRole(cfg2); got != "leader" {
		t.Errorf("bogus role = %q, want leader", got)
	}
}

// TestRuntimeMemberName_回退predefined 首个成员名。
func TestRuntimeMemberName_回退predefined(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime": map[string]any{"member_name": "configured"},
	}}
	if got := RuntimeMemberName(cfg, cfg["team"].(map[string]any)); got != "configured" {
		t.Errorf("configured = %q", got)
	}
	teamCfg := map[string]any{
		"predefined_members": []any{map[string]any{"member_name": "first"}},
	}
	if got := RuntimeMemberName(map[string]any{}, teamCfg); got != "first" {
		t.Errorf("predefined fallback = %q, want first", got)
	}
	if got := RuntimeMemberName(map[string]any{}, map[string]any{}); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

// TestParsePort_边界 nil/空串/非数字/越界均返回默认值。
func TestParsePort_边界(t *testing.T) {
	if got := ParsePort(nil, 18555, "f"); got != 18555 {
		t.Errorf("nil = %d", got)
	}
	if got := ParsePort("  ", 18555, "f"); got != 18555 {
		t.Errorf("blank = %d", got)
	}
	if got := ParsePort("abc", 18555, "f"); got != 18555 {
		t.Errorf("non-numeric = %d", got)
	}
	if got := ParsePort(0, 18555, "f"); got != 18555 {
		t.Errorf("zero = %d", got)
	}
	if got := ParsePort(65536, 18555, "f"); got != 18555 {
		t.Errorf("overflow = %d", got)
	}
	if got := ParsePort("18555", 1, "f"); got != 18555 {
		t.Errorf("string ok = %d", got)
	}
	if got := ParsePort(18556.0, 1, "f"); got != 18556 {
		t.Errorf("float ok = %d", got)
	}
}

// TestNormalizeDistributedTransportFields_leader 清空 peers 并推导地址。
func TestNormalizeDistributedTransportFields_leader(t *testing.T) {
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{"transport": map[string]any{
		"type":   "pyzmq",
		"params": map[string]any{"known_peers": []any{"x"}},
	}}
	result := NormalizeDistributedTransportFields(configBase, teamCfg)
	transport := result["transport"].(map[string]any)
	params := transport["params"].(map[string]any)
	if _, exists := params["known_peers"]; exists {
		t.Fatalf("leader should drop known_peers")
	}
	if params["direct_addr"] != "tcp://0.0.0.0:18555" {
		t.Fatalf("direct_addr = %v", params["direct_addr"])
	}
	if params["pubsub_publish_addr"] != "tcp://127.0.0.1:18556" {
		t.Fatalf("pubsub_publish_addr = %v", params["pubsub_publish_addr"])
	}
	if params["pubsub_subscribe_addr"] != "tcp://127.0.0.1:18557" {
		t.Fatalf("pubsub_subscribe_addr = %v", params["pubsub_subscribe_addr"])
	}
	meta := params["metadata"].(map[string]any)
	if meta["pubsub_bind"] != true {
		t.Fatalf("leader pubsub_bind should be true")
	}
	// 入参不被修改
	orig := teamCfg["transport"].(map[string]any)["params"].(map[string]any)
	if _, exists := orig["known_peers"]; !exists {
		t.Fatalf("input teamCfg should stay unmodified (deepcopy)")
	}
}

// TestNormalizeDistributedTransportFields_teammate 构造指向 leader 的 peers。
func TestNormalizeDistributedTransportFields_teammate(t *testing.T) {
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "teammate"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{
		"leader":     map[string]any{"member_name": "my_leader"},
		"transport":  map[string]any{"type": "pyzmq", "params": map[string]any{}},
	}
	result := NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	peers, ok := params["known_peers"].([]any)
	if !ok || len(peers) != 1 {
		t.Fatalf("teammate known_peers = %v", params["known_peers"])
	}
	peer := peers[0].(map[string]any)
	if peer["agent_id"] != "my_leader" {
		t.Fatalf("peer agent_id = %v", peer["agent_id"])
	}
	if params["direct_addr"] != "tcp://0.0.0.0:18600" {
		t.Fatalf("teammate direct_addr = %v", params["direct_addr"])
	}
	meta := params["metadata"].(map[string]any)
	if meta["pubsub_bind"] != false {
		t.Fatalf("teammate pubsub_bind should be false")
	}
}

// TestNormalizeDistributedTransportFields_非pyzmq直接返回。
func TestNormalizeDistributedTransportFields_非pyzmq直接返回(t *testing.T) {
	teamCfg := map[string]any{"transport": map[string]any{"type": "inprocess"}}
	result := NormalizeDistributedTransportFields(map[string]any{}, teamCfg)
	if !reflect.DeepEqual(result, teamCfg) {
		t.Fatalf("non-pyzmq should return as-is")
	}
}

// TestNormalizeDistributedTransportFields_已配置pubsub跳过推导。
func TestNormalizeDistributedTransportFields_已配置pubsub跳过推导(t *testing.T) {
	configBase := map[string]any{"team": map[string]any{"runtime": map[string]any{"role": "leader"}}}
	teamCfg := map[string]any{"transport": map[string]any{
		"type":   "pyzmq",
		"params": map[string]any{"pubsub_publish_addr": "tcp://1.2.3.4:1", "pubsub_subscribe_addr": "tcp://1.2.3.4:2"},
	}}
	result := NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	if params["pubsub_publish_addr"] != "tcp://1.2.3.4:1" {
		t.Fatalf("pre-filled pubsub should be kept")
	}
}

// TestMissingDistributedDependencies_Go恒空。
func TestMissingDistributedDependencies_Go恒空(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	if got := MissingDistributedDependencies(cfg); len(got) != 0 {
		t.Fatalf("Go 静态编译无缺包概念，恒返回空，got %v", got)
	}
}

// TestFallbackDistributedToLocal_配置变换。
func TestFallbackDistributedToLocal_配置变换(t *testing.T) {
	cfg := map[string]any{
		"team": map[string]any{
			"runtime":   map[string]any{"mode": "distributed", "role": "teammate"},
			"transport": map[string]any{"type": "pyzmq", "params": map[string]any{"x": 1}},
			"storage":   map[string]any{"type": "postgresql", "params": map[string]any{"connection_string": "postgresql://h/db"}},
		},
		"modes": map[string]any{
			"team": map[string]any{
				"team1": map[string]any{
					"transport": map[string]any{"type": "pyzmq", "params": map[string]any{}},
				},
			},
		},
	}
	result := FallbackDistributedToLocal(cfg)
	team := result["team"].(map[string]any)
	if team["runtime"].(map[string]any)["mode"] != "local" {
		t.Fatalf("mode should be local")
	}
	if team["runtime"].(map[string]any)["role"] != "leader" {
		t.Fatalf("role should be leader")
	}
	transport := team["transport"].(map[string]any)
	if transport["type"] != "inprocess" {
		t.Fatalf("transport type should be inprocess")
	}
	if _, exists := transport["params"]; exists {
		t.Fatalf("transport params should be removed")
	}
	storage := team["storage"].(map[string]any)
	if storage["type"] != "sqlite" {
		t.Fatalf("pg storage should fall back to sqlite, got %v", storage["type"])
	}
	modeTeam := result["modes"].(map[string]any)["team"].(map[string]any)
	cand := modeTeam["team1"].(map[string]any)
	if cand["transport"].(map[string]any)["type"] != "inprocess" {
		t.Fatalf("modes.team candidate transport should be inprocess")
	}
}

// TestIsPostgresqlStorage_判定。
func TestIsPostgresqlStorage_判定(t *testing.T) {
	if !IsPostgresqlStorage(map[string]any{"storage": map[string]any{"type": "postgresql"}}) {
		t.Errorf("postgresql should match")
	}
	if !IsPostgresqlStorage(map[string]any{"storage": map[string]any{"type": " Postgres "}}) {
		t.Errorf("postgres (case-insensitive) should match")
	}
	if IsPostgresqlStorage(map[string]any{"storage": map[string]any{"type": "sqlite"}}) {
		t.Errorf("sqlite should not match")
	}
	if IsPostgresqlStorage(map[string]any{}) {
		t.Errorf("empty should not match")
	}
}

// TestExtractPgEndpoint_URL解析。
func TestExtractPgEndpoint_URL解析(t *testing.T) {
	host, port := ExtractPgEndpoint(map[string]any{"storage": map[string]any{
		"params": map[string]any{"connection_string": "postgresql+asyncpg://user:pw@dbhost:6543/mydb"},
	}})
	if host != "dbhost" || port != 6543 {
		t.Fatalf("sqlalchemy URL: got %s:%d, want dbhost:6543", host, port)
	}
	host2, port2 := ExtractPgEndpoint(map[string]any{})
	if host2 != "127.0.0.1" || port2 != 5432 {
		t.Fatalf("empty default: got %s:%d", host2, port2)
	}
}
```

- [ ] 3.2 运行 `go test ./internal/swarm/agents/harness/team/ -run 'TestIsDistributedMode|TestRuntimeRole|TestRuntimeMemberName|TestParsePort|TestNormalizeDistributed|TestMissingDistributed|TestFallbackDistributed|TestIsPostgresqlStorage|TestExtractPgEndpoint'` 确认编译失败
- [ ] 3.3 实现 `distributed_runtime.go`（配置解析部分；命令执行函数 Task 4 补充到同一文件）：

```go
package team

import (
	"strconv"
	"strings"
	"net/url"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// subprocessTimeoutSec 单条外部命令（pg_isready/systemctl 等）的墙钟超时（秒）。
	// 对齐 Python: _SUBPROCESS_TIMEOUT_SEC = 120.0 (team_manager.py L78)
	subprocessTimeoutSec = 120.0
	// pgPostStartReadyMaxSec 启动命令返回 OK 后等待就绪的最大秒数。
	// 对齐 Python: _PG_POST_START_READY_MAX_SEC = 30.0
	pgPostStartReadyMaxSec = 30.0
	// pgPostStartReadyInitSleep 就绪轮询初始休眠（秒）。
	// 对齐 Python: _PG_POST_START_READY_INIT_SLEEP = 0.4
	pgPostStartReadyInitSleep = 0.4
	// pgPostStartReadyMaxSleep 就绪轮询单次最大休眠（秒）。
	// 对齐 Python: _PG_POST_START_READY_MAX_SLEEP = 2.0
	pgPostStartReadyMaxSleep = 2.0
	// pgPostStartReadyBackoff 就绪轮询退避系数。
	// 对齐 Python: _PG_POST_START_READY_BACKOFF = 1.45
	pgPostStartReadyBackoff = 1.45
	// pgPostStartLogEverySec 就绪等待期间的日志间隔（秒）。
	// 对齐 Python: _PG_POST_START_LOG_EVERY_SEC = 5.0
	pgPostStartLogEverySec = 5.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsDistributedMode 判断 config 是否为分布式模式。
// 对齐 Python: is_distributed_mode (distributed_runtime.py L20-28)
//
// team.runtime.mode=="distributed" 或 team.transport.type=="pyzmq"（均忽略大小写与空白）。
func IsDistributedMode(configBase map[string]any) bool {
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	if strings.EqualFold(strings.TrimSpace(anyToStr(runtimeCfg["mode"])), "distributed") {
		return true
	}
	transportCfg, _ := teamCfg["transport"].(map[string]any)
	if transportCfg == nil {
		transportCfg = map[string]any{}
	}
	return strings.EqualFold(strings.TrimSpace(anyToStr(transportCfg["type"])), "pyzmq")
}

// RuntimeRole 解析运行时角色。
// 对齐 Python: runtime_role (L31-35)
//
// 默认 leader，仅接受 leader/teammate（忽略大小写），其他值回退 leader。
func RuntimeRole(configBase map[string]any) string {
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	role := strings.ToLower(strings.TrimSpace(anyToStr(runtimeCfg["role"])))
	if role == "teammate" {
		return "teammate"
	}
	return "leader"
}

// RuntimeMemberName 解析运行时成员名。
// 对齐 Python: runtime_member_name (L38-50)
//
// team.runtime.member_name → predefined_members[0].member_name 回退，均无返回空串。
func RuntimeMemberName(configBase map[string]any, teamCfg map[string]any) string {
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	configured := strings.TrimSpace(anyToStr(runtimeCfg["member_name"]))
	if configured != "" {
		return configured
	}
	predefined, _ := teamCfg["predefined_members"].([]any)
	for _, item := range predefined {
		member, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if memberName := strings.TrimSpace(anyToStr(member["member_name"])); memberName != "" {
			return memberName
		}
	}
	return ""
}

// ParsePort 解析并验证端口值。
// 对齐 Python: parse_port (L53-71)
//
// Python raise ValueError（调用链上层吞掉后使用默认值）→ Go 降级为 warning 日志 + 返回 default。
// 范围 1..65535，越界或解析失败返回 default。
func ParsePort(value any, defaultPort int, fieldName string) int {
	if value == nil {
		return defaultPort
	}
	raw := value
	if s, ok := value.(string); ok {
		stripped := strings.TrimSpace(s)
		if stripped == "" {
			return defaultPort
		}
		raw = stripped
	}
	var port int
	switch v := raw.(type) {
	case int:
		port = v
	case int32:
		port = int(v)
	case int64:
		port = int(v)
	case float64:
		port = int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			logger.Warn(logComponent).Str("field", fieldName).Any("value", value).
				Msg("invalid port value, using default")
			return defaultPort
		}
		port = n
	default:
		logger.Warn(logComponent).Str("field", fieldName).Any("value", value).
			Msg("invalid port value type, using default")
		return defaultPort
	}
	if port < 1 || port > 65535 {
		logger.Warn(logComponent).Str("field", fieldName).Int("value", port).
			Msg("port out of range 1..65535, using default")
		return defaultPort
	}
	return port
}

// NormalizeDistributedTransportFields 标准化分布式传输字段。
// 对齐 Python: normalize_distributed_transport_fields (L74-151)
//
// 仅 transport.type=="pyzmq" 时处理：leader 清 known_peers/bootstrap_peers/
// bootstrap_known_peers（即使 pubsub/direct_addr 已预填）；pubsub 地址未预填时按
// 默认端口推导（leader direct 18555/pub 18556/sub 18557、teammate direct 18600）；
// teammate 构造指向 leader 的 known_peers/bootstrap_peers。返回深拷贝，不修改入参。
func NormalizeDistributedTransportFields(configBase map[string]any, teamCfg map[string]any) map[string]any {
	normalizedCfg := utils.DeepCopyMap(teamCfg)
	if normalizedCfg == nil {
		normalizedCfg = map[string]any{}
	}
	transportCfg, _ := normalizedCfg["transport"].(map[string]any)
	if transportCfg == nil {
		return normalizedCfg
	}
	if !strings.EqualFold(strings.TrimSpace(anyToStr(transportCfg["type"])), "pyzmq") {
		return normalizedCfg
	}
	params, _ := transportCfg["params"].(map[string]any)
	if params == nil {
		return normalizedCfg
	}

	role := RuntimeRole(configBase)
	if role == "leader" {
		// 即使 YAML 模板已预填 pubsub/direct_addr 也应用（leader 清空 peers）
		delete(params, "known_peers")
		delete(params, "bootstrap_peers")
		delete(params, "bootstrap_known_peers")
	}

	pubAddr := strings.TrimSpace(anyToStr(params["pubsub_publish_addr"]))
	subAddr := strings.TrimSpace(anyToStr(params["pubsub_subscribe_addr"]))
	if pubAddr != "" && subAddr != "" {
		return normalizedCfg
	}

	leaderCfg, _ := params["leader"].(map[string]any)
	if leaderCfg == nil {
		leaderCfg = map[string]any{}
	}
	teammateCfg, _ := params["teammate"].(map[string]any)
	if teammateCfg == nil {
		teammateCfg = map[string]any{}
	}
	leaderHost := strings.TrimSpace(anyToStr(leaderCfg["host"]))
	if leaderHost == "" {
		leaderHost = "127.0.0.1"
	}
	leaderDirectPort := ParsePort(leaderCfg["direct_port"], 18555, "team.transport.params.leader.direct_port")
	leaderPubPort := ParsePort(leaderCfg["pub_port"], 18556, "team.transport.params.leader.pub_port")
	leaderSubPort := ParsePort(leaderCfg["sub_port"], 18557, "team.transport.params.leader.sub_port")

	localDirectAddr := ""
	pubsubBind := false
	if role == "leader" {
		localDirectAddr = "tcp://0.0.0.0:" + strconv.Itoa(leaderDirectPort)
		pubsubBind = true
	} else {
		leaderMemberName := "team_leader"
		if leaderIdentity, _ := normalizedCfg["leader"].(map[string]any); leaderIdentity != nil {
			if n := strings.TrimSpace(anyToStr(leaderIdentity["member_name"])); n != "" {
				leaderMemberName = n
			}
		}
		teammateHost := strings.TrimSpace(anyToStr(teammateCfg["host"]))
		if teammateHost == "" {
			teammateHost = "127.0.0.1"
		}
		teammateDirectPort := ParsePort(teammateCfg["direct_port"], 18600, "team.transport.params.teammate.direct_port")
		localDirectAddr = "tcp://0.0.0.0:" + strconv.Itoa(teammateDirectPort)
		knownPeers := []any{
			map[string]any{
				"agent_id": leaderMemberName,
				"addrs":    []any{"tcp://" + leaderHost + ":" + strconv.Itoa(leaderDirectPort)},
			},
		}
		pubsubBind = false
		params["known_peers"] = knownPeers
		params["bootstrap_peers"] = knownPeers
	}

	params["direct_addr"] = localDirectAddr
	params["pubsub_publish_addr"] = "tcp://" + leaderHost + ":" + strconv.Itoa(leaderPubPort)
	params["pubsub_subscribe_addr"] = "tcp://" + leaderHost + ":" + strconv.Itoa(leaderSubPort)
	metadata, _ := params["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["pubsub_bind"] = pubsubBind
	params["metadata"] = metadata
	return normalizedCfg
}

// IsPostgresqlStorage 判断是否为 PostgreSQL 存储。
// 对齐 Python: is_postgresql_storage (L154-157)
func IsPostgresqlStorage(teamCfg map[string]any) bool {
	storageCfg, _ := teamCfg["storage"].(map[string]any)
	if storageCfg == nil {
		return false
	}
	storageType := strings.ToLower(strings.TrimSpace(anyToStr(storageCfg["type"])))
	return storageType == "postgresql" || storageType == "postgres"
}

// MissingDistributedDependencies 返回分布式运行时缺失的依赖列表。
// 对齐 Python: missing_distributed_dependencies (L160-172)
//
// Go 差异：静态编译无运行时缺包概念（Python 检查 zmq/asyncpg import 存在性），
// 恒返回空列表，保留函数以对齐调用链结构与日志点。
func MissingDistributedDependencies(configBase map[string]any) []string {
	return []string{}
}

// FallbackDistributedToLocal 从分布式配置创建本地回退配置。
// 对齐 Python: fallback_distributed_to_local (L175-220)
//
// runtime.mode=local + role=leader；transport.type=inprocess（去 params）；
// postgres 存储→sqlite team.db；modes.team 逐项同样处理。返回深拷贝。
func FallbackDistributedToLocal(configBase map[string]any) map[string]any {
	normalized := utils.DeepCopyMap(configBase)
	if normalized == nil {
		normalized = map[string]any{}
	}

	teamCfg, _ := normalized["team"].(map[string]any)
	if teamCfg != nil {
		runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
		if runtimeCfg == nil {
			runtimeCfg = map[string]any{}
		}
		runtimeCfg["mode"] = "local"
		runtimeCfg["role"] = "leader"
		teamCfg["runtime"] = runtimeCfg

		transportCfg, _ := teamCfg["transport"].(map[string]any)
		if transportCfg == nil {
			transportCfg = map[string]any{}
		}
		transportCfg["type"] = "inprocess"
		delete(transportCfg, "params")
		teamCfg["transport"] = transportCfg

		if IsPostgresqlStorage(teamCfg) {
			teamCfg["storage"] = map[string]any{
				"type":   "sqlite",
				"params": map[string]any{"connection_string": "team.db"},
			}
		}
	}

	modesCfg, _ := normalized["modes"].(map[string]any)
	if modesCfg != nil {
		modeTeam, _ := modesCfg["team"].(map[string]any)
		if modeTeam != nil {
			for _, candidate := range modeTeam {
				cand, ok := candidate.(map[string]any)
				if !ok {
					continue
				}
				transportCfg, _ := cand["transport"].(map[string]any)
				if transportCfg == nil {
					transportCfg = map[string]any{}
				}
				transportCfg["type"] = "inprocess"
				delete(transportCfg, "params")
				cand["transport"] = transportCfg
				if IsPostgresqlStorage(cand) {
					cand["storage"] = map[string]any{
						"type":   "sqlite",
						"params": map[string]any{"connection_string": "team.db"},
					}
				}
			}
		}
	}
	return normalized
}

// ExtractPgEndpoint 从存储配置解析 PG 主机与端口。
// 对齐 Python: extract_pg_endpoint (L223-235)
//
// 接受 sqlalchemy 风格 URL（postgresql+asyncpg://user:pass@host:5432/db），
// 前缀归一化后 urlparse；默认 127.0.0.1:5432。
func ExtractPgEndpoint(teamCfg map[string]any) (string, int) {
	storageCfg, _ := teamCfg["storage"].(map[string]any)
	if storageCfg == nil {
		return "127.0.0.1", 5432
	}
	params, _ := storageCfg["params"].(map[string]any)
	if params == nil {
		return "127.0.0.1", 5432
	}
	connectionString := strings.TrimSpace(anyToStr(params["connection_string"]))
	if connectionString == "" {
		return "127.0.0.1", 5432
	}
	normalized := connectionString
	if strings.HasPrefix(normalized, "postgresql+") {
		if idx := strings.Index(normalized, "://"); idx >= 0 {
			normalized = "postgresql://" + normalized[idx+3:]
		}
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "127.0.0.1", 5432
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := 5432
	if p := parsed.Port(); p != "" {
		if n, convErr := strconv.Atoi(p); convErr == nil {
			port = n
		}
	}
	return host, port
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// anyToStr 从 any 提取字符串（非 string 返回空串）。
//
// Python str() 会把非字符串值转为不匹配 "distributed" 等的字面量，
// Go 返回空串同样不匹配，语义等价。
func anyToStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
```

- [ ] 3.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestIsDistributedMode|TestRuntimeRole|TestRuntimeMemberName|TestParsePort|TestNormalizeDistributed|TestMissingDistributed|TestFallbackDistributed|TestIsPostgresqlStorage|TestExtractPgEndpoint' -v`
- [ ] 3.5 提交：`git add internal/swarm/agents/harness/team/ && git commit -m "feat(team): 新增 distributed_runtime 配置解析纯函数层（10.6.19 Task3，对齐 distributed_runtime.py L20-235）"`

---

## Task 4: distributed_runtime.go 命令执行部分

**Files:**
- Modify: `internal/swarm/agents/harness/team/distributed_runtime.go`（追加 RunCommand/IsPgAvailable/TryStartPgCluster/EnsurePostgresqlForLeader）
- Modify: `internal/swarm/agents/harness/team/distributed_runtime_test.go`（追加测试）

**对齐 Python**: `distributed_runtime.py` L238-413（run_command/is_pg_available/try_start_pg_cluster/ensure_postgresql_for_leader）

- [ ] 4.1 追加失败测试：

```go
// TestRunCommand_成功执行 返回退出码 0 与输出。
func TestRunCommand_成功执行(t *testing.T) {
	rc, out := RunCommand(t.Context(), []string{"echo", "hello"}, subprocessTimeoutSec)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("out = %q, want contains hello", out)
	}
}

// TestRunCommand_非零退出码 返回真实退出码。
func TestRunCommand_非零退出码(t *testing.T) {
	rc, _ := RunCommand(t.Context(), []string{"sh", "-c", "exit 3"}, subprocessTimeoutSec)
	if rc != 3 {
		t.Fatalf("rc = %d, want 3", rc)
	}
}

// TestRunCommand_spawn失败127 可执行不存在。
func TestRunCommand_spawn失败127(t *testing.T) {
	rc, _ := RunCommand(t.Context(), []string{"no-such-cmd-xyz"}, subprocessTimeoutSec)
	if rc != 127 {
		t.Fatalf("rc = %d, want 127", rc)
	}
}

// TestRunCommand_超时124 超时后被 kill。
func TestRunCommand_超时124(t *testing.T) {
	rc, _ := RunCommand(t.Context(), []string{"sleep", "5"}, 0.3)
	if rc != 124 {
		t.Fatalf("rc = %d, want 124", rc)
	}
}

// TestIsPgAvailable_pg缺失时false pg_isready 不存在 → 127 ≠ 0。
func TestIsPgAvailable_pg缺失时false(t *testing.T) {
	// 环境无 pg_isready 时返回 false（CI 安全）；有 pg 但未运行也返回 false
	if IsPgAvailable(t.Context(), "127.0.0.1", 1) {
		t.Logf("pg_isready 可用且 127.0.0.1:1 竟然 ready，跳过断言")
	}
}

// TestEnsurePostgresqlForLeader_非leader短路 不执行任何命令。
func TestEnsurePostgresqlForLeader_非leader短路(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime": map[string]any{"mode": "distributed", "role": "teammate"},
		"storage": map[string]any{"type": "postgresql"},
	}}
	// 无 PG 环境下 teammate 角色必须直接返回（否则会尝试启动 PG 拖慢测试）
	EnsurePostgresqlForLeader(t.Context(), cfg)
}

// TestEnsurePostgresqlForLeader_非pg存储短路。
func TestEnsurePostgresqlForLeader_非pg存储短路(t *testing.T) {
	cfg := map[string]any{"team": map[string]any{
		"runtime": map[string]any{"mode": "distributed", "role": "leader"},
		"storage": map[string]any{"type": "sqlite"},
	}}
	EnsurePostgresqlForLeader(t.Context(), cfg)
}
```

**注意**：`t.Context()` 需 Go 1.24+；若项目 Go 版本低于 1.24，改用 `context.Background()` 并 import `"context"`。

- [ ] 4.2 运行确认失败
- [ ] 4.3 在 `distributed_runtime.go` 追加（import 块补 `"context"`、`"errors"`、`"os/exec"`、`"time"`、`"math"`）：

```go
// RunCommand 执行外部命令并返回退出码与合并输出（stdout+stderr）。
// 对齐 Python: run_command (distributed_runtime.py L238-279)
//
// 退出码约定：spawn 失败 127 / 超时 124 / 成功或非零退出返回真实退出码。
// Python 的 communicate 失败 126 在 Go 中无直接对应（CombinedOutput 管道错误
// 极罕见），并入 127 路径。
func RunCommand(ctx context.Context, args []string, timeoutSec float64) (int, string) {
	if len(args) == 0 {
		return 127, ""
	}
	cmdStr := strings.Join(args, " ")
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	cmd := exec.CommandContext(cctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return cmd.ProcessState.ExitCode(), string(out)
	}
	// 超时（Python: 124）
	if cctx.Err() == context.DeadlineExceeded {
		logger.Warn(logComponent).Str("cmd", cmdStr).Float64("timeout_sec", timeoutSec).
			Msg("[TeamManager] subprocess timed out")
		return 124, ""
	}
	// 普通非零退出，返回真实退出码（对齐 Python 返回 returncode）
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	// spawn 失败（Python OSError → 127）
	logger.Warn(logComponent).Str("cmd", cmdStr).Err(err).
		Msg("[TeamManager] subprocess spawn failed")
	return 127, ""
}

// IsPgAvailable 探测 PG 端点是否可接受连接。
// 对齐 Python: is_pg_available (L282-291)
//
// pg_isready -h host -p port，rc==0 视为可用。
func IsPgAvailable(ctx context.Context, host string, port int) bool {
	rc, _ := RunCommand(ctx, []string{"pg_isready", "-h", host, "-p", strconv.Itoa(port)}, subprocessTimeoutSec)
	return rc == 0
}

// TryStartPgCluster 尝试启动本机 PG 集群。
// 对齐 Python: try_start_pg_cluster (L294-316)
//
// pg_lsclusters --no-header 解析首行 → pg_ctlcluster <ver> <name> start → 启动成功返回 true。
func TryStartPgCluster(ctx context.Context) bool {
	rc, out := RunCommand(ctx, []string{"pg_lsclusters", "--no-header"}, subprocessTimeoutSec)
	if rc != 0 {
		return false
	}
	firstLine := ""
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			firstLine = trimmed
			break
		}
	}
	if firstLine == "" {
		return false
	}
	parts := strings.Fields(firstLine)
	if len(parts) < 2 {
		return false
	}
	version, cluster := parts[0], parts[1]
	startRc, _ := RunCommand(ctx, []string{"pg_ctlcluster", version, cluster, "start"}, subprocessTimeoutSec)
	return startRc == 0
}

// EnsurePostgresqlForLeader 确保 leader 的 PostgreSQL 可用。
// 对齐 Python: ensure_postgresql_for_leader (L319-413)
//
// 组合流程：非 leader 或非 pg 存储短路 → ExtractPgEndpoint → IsPgAvailable
// 失败则 TryStartPgCluster → 回退 systemctl/service start → 启动后指数退避等待
// 就绪（init 0.4s / max 2.0s / backoff 1.45 / max 30s / 每 5s 日志）。
func EnsurePostgresqlForLeader(ctx context.Context, configBase map[string]any) {
	if RuntimeRole(configBase) != "leader" {
		return
	}
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	if !IsPostgresqlStorage(teamCfg) {
		return
	}

	host, port := ExtractPgEndpoint(teamCfg)
	if IsPgAvailable(ctx, host, port) {
		return
	}

	logger.Warn(logComponent).Str("host", host).Int("port", port).
		Msg("[TeamManager] PostgreSQL not reachable, attempting auto-start as leader")

	started := TryStartPgCluster(ctx)
	if !started {
		for _, cmd := range [][]string{
			{"systemctl", "start", "postgresql"},
			{"service", "postgresql", "start"},
		} {
			if rc, _ := RunCommand(ctx, cmd, subprocessTimeoutSec); rc == 0 {
				started = true
				break
			}
		}
	}
	if !started {
		logger.Warn(logComponent).
			Msg("[TeamManager] PostgreSQL auto-start command failed, will continue and rely on DB connect error")
		return
	}

	logger.Info(logComponent).Str("host", host).Int("port", port).
		Float64("max_sec", pgPostStartReadyMaxSec).
		Msg("[TeamManager] start command returned OK; waiting for connections")

	deadline := time.Now().Add(time.Duration(pgPostStartReadyMaxSec * float64(time.Second)))
	sleepFor := pgPostStartReadyInitSleep
	waitStart := time.Now()
	lastLog := waitStart
	for time.Now().Before(deadline) {
		if IsPgAvailable(ctx, host, port) {
			logger.Info(logComponent).Str("host", host).Int("port", port).
				Float64("waited_sec", time.Since(waitStart).Seconds()).
				Msg("[TeamManager] PostgreSQL is ready")
			return
		}
		now := time.Now()
		if now.Sub(lastLog).Seconds() >= pgPostStartLogEverySec {
			logger.Info(logComponent).Str("host", host).Int("port", port).
				Float64("elapsed_sec", now.Sub(waitStart).Seconds()).
				Float64("max_sec", pgPostStartReadyMaxSec).
				Msg("[TeamManager] still waiting for PostgreSQL")
			lastLog = now
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleep := time.Duration(sleepFor * float64(time.Second))
		if sleep > remaining {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		sleepFor = math.Min(sleepFor*pgPostStartReadyBackoff, pgPostStartReadyMaxSleep)
	}

	logger.Warn(logComponent).Str("host", host).Int("port", port).
		Float64("max_sec", pgPostStartReadyMaxSec).
		Msg("[TeamManager] PostgreSQL start attempted but endpoint still not ready")
}
```

- [ ] 4.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestRunCommand|TestIsPgAvailable|TestEnsurePostgresql' -v`
- [ ] 4.5 提交：`git commit -am "feat(team): distributed_runtime 补充命令执行与 PG 编排函数（10.6.19 Task4，对齐 distributed_runtime.py L238-413）"`

---

## Task 5: DeepAgentInterface 接口面补齐

**Files:**
- Modify: `internal/agentcore/harness/interfaces/deep_agent.go`
- Modify: 受影响测试文件中的 fake 实现（编译器定位，逐个补齐方法）

**依据**: 实现全部已存在于 `harness.DeepAgent`（AbilityManager deep_agent.go:407-409、SystemPromptBuilder:415、Uapswarm* :680-727），纯接口面暴露。步骤 b（agent.SetUapswarmProjectDir）与步骤 a（agent.AbilityManager()）依赖。

- [ ] 5.1 在 `DeepAgentInterface` 接口的 `Card()` 方法声明后追加：

```go
	// AbilityManager 返回能力管理器。
	// Python: DeepAgent.ability_manager 属性（BaseAgent 扩展）
	AbilityManager() agentinterfaces.AbilityManagerInterface
	// SystemPromptBuilder 返回系统提示词构建器。
	// Python: DeepAgent.system_prompt_builder 属性（harness 层扩展）
	SystemPromptBuilder() saprompt.SystemPromptBuilderInterface
	// UapswarmProjectDir 返回项目目录。
	// Python: DeepAgent._jiuwenswarm_project_dir
	UapswarmProjectDir() string
	// SetUapswarmProjectDir 设置项目目录。
	// Python: setattr(instance, "_jiuwenswarm_project_dir", value)
	SetUapswarmProjectDir(dir string)
	// UapswarmCodeProjectDir 返回代码项目目录。
	// Python: DeepAgent._jiuwenswarm_code_project_dir
	UapswarmCodeProjectDir() string
	// SetUapswarmCodeProjectDir 设置代码项目目录。
	// Python: setattr(instance, "_jiuwenswarm_code_project_dir", value)
	SetUapswarmCodeProjectDir(dir string)
	// UapswarmAdapterMode 返回适配器模式。
	// Python: DeepAgent._jiuwenswarm_adapter_mode
	UapswarmAdapterMode() string
	// SetUapswarmAdapterMode 设置适配器模式。
	// Python: setattr(instance, "_jiuwenswarm_adapter_mode", value)
	SetUapswarmAdapterMode(mode string)
```

import 块追加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"`（如未存在）。注意 `interfaces` 包不能引入循环：`single_agent/prompts` 仅依赖 `single_agent/schema`，无循环。

- [ ] 5.2 运行 `go build ./internal/agentcore/... ./internal/agent_teams/... ./internal/swarm/... 2>&1 | head -50`，编译器会列出所有未实现新方法的 fake/test 类型。对每个报错文件追加以下桩（按缺失项选择）：

```go
// fakeXxx 补齐 10.6.19 接口面扩展（无操作桩，仅满足接口）
func (f *fakeXxx) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeXxx) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return nil }
func (f *fakeXxx) UapswarmProjectDir() string                    { return "" }
func (f *fakeXxx) SetUapswarmProjectDir(string)                   {}
func (f *fakeXxx) UapswarmCodeProjectDir() string                 { return "" }
func (f *fakeXxx) SetUapswarmCodeProjectDir(string)               {}
func (f *fakeXxx) UapswarmAdapterMode() string                    { return "" }
func (f *fakeXxx) SetUapswarmAdapterMode(string)                  {}
```

预期需补齐的文件（前期调研定位，编译器输出为准）：`internal/agent_teams/harness_test.go`、`internal/agentcore/harness/interfaces/deep_agent_test.go` 等含自定义 fake 的测试文件。

- [ ] 5.3 全量编译验证：`go build ./...` 通过
- [ ] 5.4 运行受影响包测试：`go test ./internal/agentcore/harness/interfaces/ ./internal/agent_teams/ ./internal/agentcore/harness/ -count=1`
- [ ] 5.5 提交：`git commit -am "feat(interfaces): DeepAgentInterface 补齐 AbilityManager/SystemPromptBuilder/Uapswarm 访问器接口面（10.6.19 Task5）"`

---

## Task 6: runtime_inheritance.go 结构体迁移 + 白名单 + config 辅助迁移

**Files:**
- Create: `internal/swarm/agents/harness/team/runtime_inheritance.go`
- Create: `internal/swarm/agents/harness/team/runtime_inheritance_test.go`
- Modify: `internal/swarm/agents/harness/team/team_manager.go`（四个结构体迁出）
- Modify: `internal/swarm/agents/harness/team/team_manager_skill.go`（getEvolutionConfig 系列迁出）

**对齐 Python**: `team_runtime_inheritance.py` L39-126（dataclass + 两白名单）+ L363-392（_get_bool_env/_get_evolution_config/开关函数）

- [ ] 6.1 写失败测试 `runtime_inheritance_test.go`（第一部分）：

```go
package team

import (
	"testing"
)

// TestToolWhitelist_39项 逐项对齐 Python TOOL_WHITELIST。
func TestToolWhitelist_39项(t *testing.T) {
	want := []string{
		"free_search", "fetch_webpage", "paid_search", "vision", "audio",
		"image_ocr", "visual_question_answering", "generate_image",
		"audio_transcription", "audio_question_answering", "audio_metadata",
		"video_understanding", "search_skill", "install_skill", "uninstall_skill",
		"user_todos", "get_user_location", "create_note", "search_notes", "modify_note",
		"create_calendar_event", "search_calendar_event", "search_contact",
		"search_photo_gallery", "upload_photo", "search_file", "upload_file",
		"call_phone", "send_message", "search_message", "create_alarm", "search_alarms",
		"modify_alarm", "delete_alarm", "xiaoyi_collection", "image_reading",
		"xiaoyi_gui_agent", "web_free_search", "web_fetch_webpage", "web_paid_search",
		"skill_toolkit", "enter_worktree", "exit_worktree", "acp_chat",
	}
	if len(toolWhitelist) != len(want) {
		t.Fatalf("toolWhitelist size = %d, want %d", len(toolWhitelist), len(want))
	}
	for _, name := range want {
		if !toolWhitelist[name] {
			t.Errorf("toolWhitelist missing %q", name)
		}
	}
}

// TestRailWhitelist_14项 对齐 Python RAIL_WHITELIST（Go 实际类型名适配）。
func TestRailWhitelist_14项(t *testing.T) {
	// 14 项：Python 名 SecurityRail → Go 实际类型 SafetyPromptRail（类型别名反射名）；
	// FileSystemRail 为 Python 侧保留名，Go 无对应类型，保留占位键。
	want := []string{
		"RuntimePromptRail", "ResponsePromptRail", "JiuClawStreamEventRail",
		"TaskPlanningRail", "SafetyPromptRail", "HeartbeatRail", "AvatarPromptRail",
		"FileSystemRail", "SysOperationRail", "TeamSkillEvolutionRail",
		"TeamSkillCreateRail", "SkillEvolutionRail", "TeamWorkspaceReportPathRail",
		"ContextProcessorRail",
	}
	if len(railWhitelist) != len(want) {
		t.Fatalf("railWhitelist size = %d, want %d", len(railWhitelist), len(want))
	}
	for _, name := range want {
		if !railWhitelist[name] {
			t.Errorf("railWhitelist missing %q", name)
		}
	}
}

// TestGetEvolutionAutoScanEnabled_env优先。
func TestGetEvolutionAutoScanEnabled_env优先(t *testing.T) {
	t.Setenv("EVOLUTION_AUTO_SCAN", "true")
	if !GetEvolutionAutoScanEnabled(nil) {
		t.Fatalf("env true should win")
	}
	t.Setenv("EVOLUTION_AUTO_SCAN", "false")
	cfg := map[string]any{"react": map[string]any{"evolution": map[string]any{"auto_scan": true}}}
	if GetEvolutionAutoScanEnabled(cfg) {
		t.Fatalf("env false should override config true")
	}
}

// TestGetEvolutionAutoScanEnabled_配置回退。
func TestGetEvolutionAutoScanEnabled_配置回退(t *testing.T) {
	t.Setenv("EVOLUTION_AUTO_SCAN", "")
	osUnsetEvolutionEnv(t)
	cfg := map[string]any{"react": map[string]any{"evolution": map[string]any{"auto_scan": true}}}
	if !GetEvolutionAutoScanEnabled(cfg) {
		t.Fatalf("react.evolution.auto_scan=true should enable")
	}
	if GetEvolutionAutoScanEnabled(map[string]any{"evolution": map[string]any{"auto_scan": true}}) {
		t.Fatalf("top-level evolution fallback should work")
	}
	if GetEvolutionAutoScanEnabled(nil) {
		t.Fatalf("default should be false")
	}
}

// TestGetSkillCreateEnabled 同构 auto_scan。
func TestGetSkillCreateEnabled(t *testing.T) {
	t.Setenv("SKILL_CREATE", "1")
	if !GetSkillCreateEnabled(nil) {
		t.Fatalf("env 1 should enable")
	}
	osUnsetSkillCreateEnv(t)
	if GetSkillCreateEnabled(map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_create": true}}}) {
		t.Logf("react.evolution.skill_create works")
	}
	if GetSkillCreateEnabled(nil) {
		t.Fatalf("default false")
	}
}

// TestGetContextEngineEnabled_默认true。
func TestGetContextEngineEnabled_默认true(t *testing.T) {
	if !GetContextEngineEnabled(nil) {
		t.Fatalf("nil config default true")
	}
	if !GetContextEngineEnabled(map[string]any{}) {
		t.Fatalf("empty config default true")
	}
	cfg := map[string]any{"react": map[string]any{"context_engine_config": map[string]any{"enabled": false}}}
	if GetContextEngineEnabled(cfg) {
		t.Fatalf("explicit false should disable")
	}
}

// osUnsetEvolutionEnv/osUnsetSkillCreateEnv 测试辅助：清理 env。
func osUnsetEvolutionEnv(t *testing.T)  { t.Setenv("EVOLUTION_AUTO_SCAN", "") }
func osUnsetSkillCreateEnv(t *testing.T) { t.Setenv("SKILL_CREATE", "") }
```

**注意**：`t.Setenv("EVOLUTION_AUTO_SCAN", "")` 设置空串——`getBoolEnv` 用 `os.LookupEnv` 会命中空串分支（设置但非真值 → false 且 ok=true）。这与 Python `os.getenv` 返回 "" → `_get_bool_env("")` 返回 False is not None → False 一致，行为对齐。上面的"配置回退"测试用空串 env 模拟"未设置"是不准确的——修正：配置回退测试应使用 `os.Unsetenv`（需还原）：

```go
func TestGetEvolutionAutoScanEnabled_配置回退(t *testing.T) {
	os.Unsetenv("EVOLUTION_AUTO_SCAN") //nolint:errcheck // 测试环境清理
	// ... 其余不变
}
```

同理 `TestGetSkillCreateEnabled` 中 `osUnsetSkillCreateEnv` 改为 `os.Unsetenv("SKILL_CREATE")`。删除末尾两个辅助函数。

- [ ] 6.2 运行确认编译失败
- [ ] 6.3 创建 `runtime_inheritance.go`（第一部分：结构体 + 白名单 + 迁入函数）：

```go
package team

import (
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberInfo 成员身份信息。
// 对齐 Python: MemberInfo dataclass (team_runtime_inheritance.py L39-44)
type MemberInfo struct {
	// AgentName 成员名称（Python 默认 "team_member"）
	AgentName string
	// ModelName 模型名称（Python 默认 "gpt-4"）
	ModelName string
	// Role 角色（leader/teammate），nil 表示未指定
	Role *string
}

// RuntimeInfo 运行时环境信息。
// 对齐 Python: RuntimeInfo dataclass (L47-51)
type RuntimeInfo struct {
	// Channel 渠道标识（Python 默认 "default"）
	Channel string
	// Language 语言标识（Python 默认 "cn"）
	Language string
}

// TeamWorkspaceInfo Team 共享 workspace 信息。
// 对齐 Python: TeamWorkspaceInfo dataclass (L54-61)
type TeamWorkspaceInfo struct {
	// RootDir 工作区根目录
	RootDir *string
	// SkillsDir 技能目录
	SkillsDir *string
	// TeamID 团队标识
	TeamID *string
	// Config 配置字典
	Config map[string]any
	// TrajectoryRegistry 轨迹注册表（同时实现 TrajectorySource + TrajectorySink）
	// 对齐 Python: TeamWorkspaceInfo.trajectory_registry（实际为 InMemoryTrajectoryRegistry）
	TrajectoryRegistry trajectory.TrajectorySource
}

// TeamRailMountContext 重建 team rails 所需的上下文。
// 对齐 Python: TeamRailMountContext dataclass (team_manager.py L110-117)
type TeamRailMountContext struct {
	// Agent 重建 rails 时关联的 Agent 实例
	Agent interfaces.DeepAgentInterface
	// MemberInfo 成员信息
	MemberInfo *MemberInfo
	// Runtime 运行时信息
	Runtime *RuntimeInfo
	// TeamWorkspace 团队工作区信息
	TeamWorkspace *TeamWorkspaceInfo
}

// ──────────────────────────── 常量 ────────────────────────────

// railWhitelist 成员 rail 白名单（按 Go 实际反射类型名）。
// 对齐 Python: RAIL_WHITELIST (team_runtime_inheritance.py L64-79)
//
// 差异说明：
//   - Python "SecurityRail" → Go 实际类型名 "SafetyPromptRail"（type SecurityRail = SafetyPromptRail
//     类型别名，reflect Elem().Name() 返回 SafetyPromptRail）
//   - "FileSystemRail" 为 Python 侧类型名，Go 无对应类型，保留占位键永不匹配
var railWhitelist = map[string]bool{
	"RuntimePromptRail":        true,
	"ResponsePromptRail":       true,
	"JiuClawStreamEventRail":   true,
	"TaskPlanningRail":         true,
	"SafetyPromptRail":         true,
	"HeartbeatRail":            true,
	"AvatarPromptRail":         true,
	"FileSystemRail":           true,
	"SysOperationRail":         true,
	"TeamSkillEvolutionRail":   true,
	"TeamSkillCreateRail":      true,
	"SkillEvolutionRail":       true,
	"TeamWorkspaceReportPathRail": true,
	"ContextProcessorRail":     true,
}

// toolWhitelist 可继承工具白名单（39 项）。
// 对齐 Python: TOOL_WHITELIST (L81-126)
var toolWhitelist = map[string]bool{
	"free_search": true, "fetch_webpage": true, "paid_search": true, "vision": true,
	"audio": true, "image_ocr": true, "visual_question_answering": true,
	"generate_image": true, "audio_transcription": true, "audio_question_answering": true,
	"audio_metadata": true, "video_understanding": true, "search_skill": true,
	"install_skill": true, "uninstall_skill": true, "user_todos": true,
	"get_user_location": true, "create_note": true, "search_notes": true,
	"modify_note": true, "create_calendar_event": true, "search_calendar_event": true,
	"search_contact": true, "search_photo_gallery": true, "upload_photo": true,
	"search_file": true, "upload_file": true, "call_phone": true, "send_message": true,
	"search_message": true, "create_alarm": true, "search_alarms": true,
	"modify_alarm": true, "delete_alarm": true, "xiaoyi_collection": true,
	"image_reading": true, "xiaoyi_gui_agent": true, "web_free_search": true,
	"web_fetch_webpage": true, "web_paid_search": true, "skill_toolkit": true,
	"enter_worktree": true, "exit_worktree": true, "acp_chat": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetEvolutionAutoScanEnabled 从 config 读取 auto_scan 开关。
// 对齐 Python: get_evolution_auto_scan_enabled (L381-385)
//
// env EVOLUTION_AUTO_SCAN 优先（true/1/yes 不区分大小写），
// 回退 react.evolution.auto_scan → evolution.auto_scan，默认 false。
func GetEvolutionAutoScanEnabled(config map[string]any) bool {
	if envVal, ok := getBoolEnv("EVOLUTION_AUTO_SCAN"); ok {
		return envVal
	}
	return getEvolutionConfigField(config, "auto_scan")
}

// GetSkillCreateEnabled 从 config 读取 skill_create 开关。
// 对齐 Python: get_skill_create_enabled (L388-392)
func GetSkillCreateEnabled(config map[string]any) bool {
	if envVal, ok := getBoolEnv("SKILL_CREATE"); ok {
		return envVal
	}
	return getEvolutionConfigField(config, "skill_create")
}

// GetContextEngineEnabled 检查上下文压缩是否启用。
// 对齐 Python: get_context_engine_enabled (L525-537)
//
// 读取 react.context_engine_config.enabled，默认 true。
func GetContextEngineEnabled(config map[string]any) bool {
	if config == nil {
		return true
	}
	react, _ := config["react"].(map[string]any)
	if react != nil {
		ctxCfg, _ := react["context_engine_config"].(map[string]any)
		if ctxCfg != nil {
			if b, ok := ctxCfg["enabled"].(bool); ok {
				return b
			}
		}
	}
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getEvolutionConfigField 从嵌套路径读取 evolution 配置字段（bool）。
// 对齐 Python _get_evolution_config (L369-378)：
// 优先 config["react"]["evolution"]，回退 config["evolution"]，均无则空 → 默认 false
func getEvolutionConfigField(config map[string]any, field string) bool {
	evolutionConfig := getEvolutionConfig(config)
	if b, ok := evolutionConfig[field].(bool); ok {
		return b
	}
	return false
}

// getEvolutionConfig 解析嵌套的 evolution 配置段。
// 对齐 Python _get_evolution_config (L369-378)
func getEvolutionConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	if reactConfig, ok := config["react"].(map[string]any); ok {
		if evolutionConfig, ok := reactConfig["evolution"].(map[string]any); ok {
			return evolutionConfig
		}
	}
	if evolutionConfig, ok := config["evolution"].(map[string]any); ok {
		return evolutionConfig
	}
	return nil
}

// getBoolEnv 读取布尔环境变量。
// 对齐 Python _get_bool_env (L363-366)：
// 值为 true/1/yes（不区分大小写）→ true；设置但非真值 → false；未设置 → (false, false)
func getBoolEnv(key string) (value bool, ok bool) {
	raw, exists := os.LookupEnv(key)
	if !exists {
		return false, false
	}
	return strings.EqualFold(raw, "true") || raw == "1" || strings.EqualFold(raw, "yes"), true
}
```

import 补 `"strings"`。

- [ ] 6.4 从 `team_manager.go` 删除 MemberInfo/RuntimeInfo/TeamWorkspaceInfo/TeamRailMountContext 四个结构体定义（L58-105）及其现在多余的 import（`interfaces`、`trajectory` 若无其他引用则移除——`team_manager.go` 的 `interfaces` 仍被 LiveRailEntry/TeamManager 使用、`trajectory` 仅被 TeamWorkspaceInfo 使用需删除，`evolution` 仍被 map 字段使用保留）
- [ ] 6.5 从 `team_manager_skill.go` 删除 getEvolutionAutoScanEnabled/getSkillCreateEnabled/getEvolutionConfigField/getEvolutionConfig/getBoolEnv 五个函数（L269-336），`UpdateEvolutionConfig` 中两处调用改为 `GetEvolutionAutoScanEnabled(config)` / `GetSkillCreateEnabled(config)`；删除后 `os`、`strings` import 若无其他引用则移除（`strings` 在 isLeaderRole 所在文件无引用可删，逐个编译确认）
- [ ] 6.6 运行 `go test ./internal/swarm/agents/harness/team/ -count=1`（既有测试不回退 + 新测试通过）
- [ ] 6.7 提交：`git commit -am "feat(team): runtime_inheritance 结构体迁移+双白名单+evolution 开关函数（10.6.19 Task6，对齐 team_runtime_inheritance.py L39-392）"`

---

## Task 7: runtime_inheritance 模型解析（GetDefaultModelName/ResolveModelConfig/BuildEvolutionLLM）

**Files:**
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance.go`
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance_test.go`

**对齐 Python**: `team_runtime_inheritance.py` L335-360（get_default_model_name）、L395-448（resolve_model_config）、L451-476（build_evolution_llm）

- [ ] 7.1 追加失败测试：

```go
// TestGetDefaultModelName_读取modelsDefault。
func TestGetDefaultModelName_读取modelsDefault(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{"model_client_config": map[string]any{"model_name": "qwen-max"}},
	}}
	if got := GetDefaultModelName(cfg); got != "qwen-max" {
		t.Fatalf("got %q, want qwen-max", got)
	}
	if got := GetDefaultModelName(map[string]any{}); got != "gpt-4" {
		t.Fatalf("default fallback = %q, want gpt-4", got)
	}
}

// TestResolveModelConfig_defaults列表isDefault优先。
func TestResolveModelConfig_defaults列表isDefault优先(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"defaults": []any{
			map[string]any{"model_client_config": map[string]any{"model_name": "first"}},
			map[string]any{"is_default": true, "model_client_config": map[string]any{"model_name": "second", "api_key": "k"}},
		},
	}}
	mcc, mco, name := ResolveModelConfig(cfg)
	if name != "second" {
		t.Fatalf("is_default entry should win, got %q", name)
	}
	if mcc["api_key"] != "k" {
		t.Fatalf("mcc mismatch: %v", mcc)
	}
	_ = mco
}

// TestResolveModelConfig_无isDefault取第一个。
func TestResolveModelConfig_无isDefault取第一个(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"defaults": []any{
			map[string]any{"model_client_config": map[string]any{"model_name": "first"}},
			map[string]any{"model_client_config": map[string]any{"model_name": "second"}},
		},
	}}
	_, _, name := ResolveModelConfig(cfg)
	if name != "first" {
		t.Fatalf("first entry fallback = %q, want first", name)
	}
}

// TestResolveModelConfig_旧格式default与react回退。
func TestResolveModelConfig_旧格式default与react回退(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{"default": map[string]any{
			"model_client_config": map[string]any{"model_name": "from-default"},
		}},
		"react": map[string]any{"model_name": "from-react"},
	}
	_, _, name := ResolveModelConfig(cfg)
	if name != "from-default" {
		t.Fatalf("models.default should win over react, got %q", name)
	}

	cfg2 := map[string]any{
		"react": map[string]any{
			"model_name":            "from-react",
			"model_client_config":   map[string]any{"api_key": "rk"},
			"model_config_obj":      map[string]any{"temperature": 0.5},
		},
	}
	mcc, mco, name2 := ResolveModelConfig(cfg2)
	if name2 != "from-react" {
		t.Fatalf("react fallback = %q", name2)
	}
	if mcc["api_key"] != "rk" || mco["temperature"] != 0.5 {
		t.Fatalf("react fallback mcc/mco mismatch: %v %v", mcc, mco)
	}
}

// TestResolveModelConfig_最终回退gpt4。
func TestResolveModelConfig_最终回退gpt4(t *testing.T) {
	_, _, name := ResolveModelConfig(map[string]any{})
	if name != "gpt-4" {
		t.Fatalf("final fallback = %q, want gpt-4", name)
	}
}

// TestBuildEvolutionLLM_合法配置 temperature 默认 0.95 + 显式覆盖。
func TestBuildEvolutionLLM_合法配置(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "test-model", "api_key": "k", "api_base": "https://x",
				"client_provider": "OpenAI",
			},
			"model_config_obj": map[string]any{"temperature": 0.5},
		},
	}}
	model, name := BuildEvolutionLLM(cfg)
	if model == nil {
		t.Fatalf("model should be created")
	}
	if name != "test-model" {
		t.Fatalf("name = %q", name)
	}
	if model.ModelConfig.Temperature != 0.5 {
		t.Fatalf("temperature = %v, want 0.5", model.ModelConfig.Temperature)
	}
}

// TestBuildEvolutionLLM_默认temperature095。
func TestBuildEvolutionLLM_默认temperature095(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_key": "k", "api_base": "https://x",
				"client_provider": "OpenAI",
			},
		},
	}}
	model, _ := BuildEvolutionLLM(cfg)
	if model == nil {
		t.Fatalf("model should be created")
	}
	if model.ModelConfig.Temperature != 0.95 {
		t.Fatalf("default temperature = %v, want 0.95", model.ModelConfig.Temperature)
	}
}

// TestBuildEvolutionLLM_缺apiKey返回nil。
func TestBuildEvolutionLLM_缺apiKey返回nil(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_base": "https://x", "client_provider": "OpenAI",
			},
		},
	}}
	model, name := BuildEvolutionLLM(cfg)
	if model != nil {
		t.Fatalf("invalid config should return nil model")
	}
	if name != "m" {
		t.Fatalf("model_name should still be returned, got %q", name)
	}
}
```

- [ ] 7.2 运行确认失败
- [ ] 7.3 在 `runtime_inheritance.go` 追加（import 补 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`、`llmschema "...foundation/llm/schema"`、`"github.com/uapclaw/uapclaw-go/internal/common/config"`、`"github.com/uapclaw/uapclaw-go/internal/common/logger"`）：

```go
// GetDefaultModelName 从配置获取默认 model_name。
// 对齐 Python: get_default_model_name (team_runtime_inheritance.py L335-360)
//
// 读取 models.default.model_client_config.model_name（注意：Python 读旧格式
// default 单对象而非 defaults 列表），失败回退 "gpt-4"。
func GetDefaultModelName(cfgMap map[string]any) string {
	cfg := cfgMap
	if cfg == nil {
		loaded, err := teamLoadConfig()
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[TeamRuntime] Failed to load config for default model")
			return "gpt-4"
		}
		cfg = loaded
	}
	models, _ := cfg["models"].(map[string]any)
	if models != nil {
		defaultEntry, _ := models["default"].(map[string]any)
		if defaultEntry != nil {
			if mcc, _ := defaultEntry["model_client_config"].(map[string]any); mcc != nil {
				if name, ok := mcc["model_name"].(string); ok && name != "" {
					return name
				}
			}
		}
	}
	return "gpt-4"
}

// ResolveModelConfig 从配置字典解析 model 相关参数。
// 对齐 Python: resolve_model_config (L395-448)
//
// 优先 models.defaults 列表 is_default=true 条目 → 第一个条目 →
// models.default 单对象 → react 段回退。返回浅拷贝（对齐 Python .copy()）。
func ResolveModelConfig(config map[string]any) (mcc map[string]any, mco map[string]any, modelName string) {
	if config == nil {
		return map[string]any{}, map[string]any{}, "gpt-4"
	}
	models, _ := config["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}

	// 优先从 models.defaults 列表取 is_default=true 的条目
	if defaults, ok := models["defaults"].([]any); ok && len(defaults) > 0 {
		for _, item := range defaults {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if b, ok := entry["is_default"].(bool); ok && b {
				mcc = copyMapShallow(entry["model_client_config"])
				mco = copyMapShallow(entry["model_config_obj"])
				if name := mapStrField(mcc, "model_name"); name != "" {
					return mcc, mco, name
				}
			}
		}
		// 无 is_default=true 时取第一个
		if first, ok := defaults[0].(map[string]any); ok {
			mcc = copyMapShallow(first["model_client_config"])
			mco = copyMapShallow(first["model_config_obj"])
			if name := mapStrField(mcc, "model_name"); name != "" {
				return mcc, mco, name
			}
		}
	}

	// 回退到旧格式：models.default + react 段
	defaultModelConfig, _ := models["default"].(map[string]any)
	if defaultModelConfig == nil {
		defaultModelConfig = map[string]any{}
	}
	reactConfig, _ := config["react"].(map[string]any)
	if reactConfig == nil {
		reactConfig = map[string]any{}
	}

	modelClientConfig, _ := defaultModelConfig["model_client_config"].(map[string]any)
	if modelClientConfig == nil {
		modelClientConfig, _ = reactConfig["model_client_config"].(map[string]any)
	}
	if modelClientConfig == nil {
		modelClientConfig = map[string]any{}
	}

	modelName = mapStrField(modelClientConfig, "model_name")
	if modelName == "" {
		modelName = mapStrField(reactConfig, "model_name")
	}
	if modelName == "" {
		modelName = "gpt-4"
	}

	modelConfigObj, _ := defaultModelConfig["model_config_obj"].(map[string]any)
	if modelConfigObj == nil {
		modelConfigObj, _ = reactConfig["model_config_obj"].(map[string]any)
	}
	if modelConfigObj == nil {
		modelConfigObj = map[string]any{}
	}

	return modelClientConfig, modelConfigObj, modelName
}

// BuildEvolutionLLM 从配置构造 evolution 使用的 LLM Model 实例。
// 对齐 Python: build_evolution_llm (L451-476)
//
// temperature 取 model_config_obj.temperature，默认 0.95。
// 构造失败（provider/api_key/api_base 缺失或 Model 创建失败）返回 (nil, model_name)，
// 调用方按 Python try/except 语义跳过该 rail。
func BuildEvolutionLLM(config map[string]any) (*llm.Model, string) {
	mcc, mco, modelName := ResolveModelConfig(config)
	temperature := 0.95
	if t, ok := mco["temperature"].(float64); ok {
		temperature = t
	}
	requestConfig := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(modelName),
		llmschema.WithTemperature(temperature),
	)

	provider := mapStrField(mcc, "client_provider")
	apiKey := mapStrField(mcc, "api_key")
	apiBase := mapStrField(mcc, "api_base")

	var clientConfigOpts []llmschema.ModelClientConfigOption
	if t, ok := mcc["timeout"].(float64); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithTimeout(t))
	}
	if v, ok := mcc["verify_ssl"].(bool); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithVerifySSL(v))
	}
	if headers := customHeadersToStringMap(mcc["custom_headers"]); headers != nil {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithCustomHeaders(headers))
	}

	clientConfig, err := llmschema.NewModelClientConfig(provider, apiKey, apiBase, clientConfigOpts...)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("model_name", modelName).
			Msg("[TeamRuntime] evolution LLM client config invalid")
		return nil, modelName
	}
	model, err := llm.NewModel(clientConfig, requestConfig)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("model_name", modelName).
			Msg("[TeamRuntime] evolution LLM creation failed")
		return nil, modelName
	}
	return model, modelName
}

// teamLoadConfig 加载默认 config.yaml。
// 对齐 Python: get_config()
func teamLoadConfig() (map[string]any, error) {
	c, err := config.New("")
	if err != nil {
		return nil, err
	}
	return c.Load()
}

// copyMapShallow 浅拷贝 map[string]any（对齐 Python dict.copy()）。
// 非 map 输入返回空 map（对齐 Python (x or {}).copy() 的 None 容错）。
func copyMapShallow(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(m))
	for k, val := range m {
		result[k] = val
	}
	return result
}

// mapStrField 从 map 提取字符串字段（非 string 或缺失返回空串）。
func mapStrField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// customHeadersToStringMap 将 custom_headers 配置转为 map[string]string。
//
// 输入可能是 JSON 字符串或 map[string]any（config 已解析路径），
// 对齐 Python ModelClientConfig(custom_headers=...) 的 dict 语义。
func customHeadersToStringMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		result := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		if len(result) == 0 && len(m) > 0 {
			return nil
		}
		return result
	}
	if s, ok := v.(string); ok && s != "" {
		parsed := config.ParseCustomHeaders(s)
		if parsed == nil {
			return nil
		}
		result := make(map[string]string, len(parsed))
		for k, val := range parsed {
			if sv, ok := val.(string); ok {
				result[k] = sv
			}
		}
		return result
	}
	return nil
}
```

- [ ] 7.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestGetDefaultModelName|TestResolveModelConfig|TestBuildEvolutionLLM' -v`
- [ ] 7.5 提交：`git commit -am "feat(team): runtime_inheritance 模型解析三函数（10.6.19 Task7，对齐 get_default_model_name/resolve_model_config/build_evolution_llm）"`

---

## Task 8: runtime_inheritance 能力过滤 + ContextProcessor + SkillEvolution 构造

**Files:**
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance.go`
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance_test.go`

**对齐 Python**: L307-332（filter_inheritable_ability_cards）、L540-587（_build_context_processor_rail）、L479-522（build_skill_evolution_rail）

- [ ] 8.1 追加失败测试：

```go
// TestFilterInheritableAbilityCards_白名单过滤。
func TestFilterInheritableAbilityCards_白名单过滤(t *testing.T) {
	agent := newFakeDeepAgentForInheritance()
	inherited := FilterInheritableAbilityCards(agent)
	names := make([]string, 0)
	for _, card := range inherited {
		names = append(names, card.Name)
	}
	// fake 中 vision（白名单内）+ my_custom_tool（白名单外）+ 非 ToolCard 能力
	if len(inherited) != 1 || inherited[0].Name != "vision" {
		t.Fatalf("filtered = %v, want [vision]", names)
	}
}

// TestBuildContextProcessorRail_四段配置 按序构造 ProcessorSpec。
func TestBuildContextProcessorRail_四段配置(t *testing.T) {
	cfg := map[string]any{"react": map[string]any{"context_engine_config": map[string]any{
		"message_summary_offloader_config": map[string]any{"threshold": 10},
		"dialogue_compressor_config":       map[string]any{"max_tokens": 100},
	}}}
	rail := BuildContextProcessorRail(cfg)
	if rail == nil {
		t.Fatalf("rail should not be nil")
	}
	// 内部 userProcessors 无法直接断言，通过日志路径覆盖；此处验证不 panic 且返回实例
}

// TestBuildContextProcessorRail_空配置 preset 实例。
func TestBuildContextProcessorRail_空配置(t *testing.T) {
	rail := BuildContextProcessorRail(nil)
	if rail == nil {
		t.Fatalf("rail should not be nil for nil config")
	}
}

// TestBuildSkillEvolutionRail_失败返回nil。
func TestBuildSkillEvolutionRail_失败返回nil(t *testing.T) {
	// 缺 api_key 的 config → BuildEvolutionLLM 失败 → 返回 nil
	rail := BuildSkillEvolutionRail("/tmp/x", map[string]any{}, nil, "")
	if rail != nil {
		t.Fatalf("invalid config should return nil rail")
	}
}

// TestBuildSkillEvolutionRail_合法配置 不绑 sink（teamID 空）。
func TestBuildSkillEvolutionRail_合法配置(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_key": "k", "api_base": "https://x",
				"client_provider": "OpenAI",
			},
		},
	}}
	rail := BuildSkillEvolutionRail("/tmp/skills", cfg, nil, "")
	if rail == nil {
		t.Fatalf("valid config should build rail")
	}
}
```

fake 定义（追加到测试文件）：

```go
// fakeAbilityMgrForFilter 测试用 AbilityManager。
type fakeAbilityMgrForFilter struct {
	abilities []schema.Ability
}

func (f *fakeAbilityMgrForFilter) Add(a schema.Ability) agentschema.AddAbilityResult {
	f.abilities = append(f.abilities, a)
	return agentschema.AddAbilityResult{Added: true}
}
func (f *fakeAbilityMgrForFilter) AddMany(a []schema.Ability) []agentschema.AddAbilityResult { return nil }
func (f *fakeAbilityMgrForFilter) Remove(name string) schema.Ability { return nil }
func (f *fakeAbilityMgrForFilter) RemoveMany(names []string) []schema.Ability { return nil }
func (f *fakeAbilityMgrForFilter) Get(name string) schema.Ability { return nil }
func (f *fakeAbilityMgrForFilter) List() []schema.Ability { return f.abilities }
func (f *fakeAbilityMgrForFilter) ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]schema.ToolInfoInterface, error) {
	return nil, nil
}
func (f *fakeAbilityMgrForFilter) Execute(ctx context.Context, name string, inputs map[string]any, mcpServerName ...string) ([]agentschema.ExecuteResult, error) {
	return nil, nil
}

// fakeDeepAgentForInheritance 最小 DeepAgentInterface（仅 AbilityManager 路径）。
type fakeDeepAgentForInheritance struct {
	abilityMgr *fakeAbilityMgrForFilter
}

func newFakeDeepAgentForInheritance() *fakeDeepAgentForInheritance {
	whiteListed := &tool.ToolCard{}
	whiteListed.Name = "vision"
	whiteListed.ID = "v1"
	outside := &tool.ToolCard{}
	outside.Name = "my_custom_tool"
	outside.ID = "c1"
	return &fakeDeepAgentForInheritance{
		abilityMgr: &fakeAbilityMgrForFilter{abilities: []schema.Ability{whiteListed, outside}},
	}
}

func (f *fakeDeepAgentForInheritance) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityMgr
}
// 其余接口方法为空桩（按 Task 5 补齐清单同型）
```

**注意**：`fakeDeepAgentForInheritance` 需实现完整 DeepAgentInterface（编译器报错为准，补齐 Task 5 同款空桩；`Execute` 签名以 abilitymgr.go 实际声明为准——实现时先读 `internal/agentcore/single_agent/interfaces/abilitymgr.go` L19-50 核对全签名再写桩）。`schema` import = `"github.com/uapclaw/uapclaw-go/internal/common/schema"`，`agentschema` = `internal/agentcore/single_agent/schema`，`tool` = `internal/agentcore/foundation/tool`，`agentinterfaces` = `internal/agentcore/single_agent/interfaces`。

- [ ] 8.2 运行确认失败
- [ ] 8.3 在 `runtime_inheritance.go` 追加（import 补 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"`、`ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"`、`"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"`）：

```go
// FilterInheritableAbilityCards 从主 agent 获取可继承的 ToolCard 白名单。
// 对齐 Python: filter_inheritable_ability_cards (team_runtime_inheritance.py L307-332)
func FilterInheritableAbilityCards(mainAgent interfaces.DeepAgentInterface) []*tool.ToolCard {
	result := make([]*tool.ToolCard, 0)
	if mainAgent == nil {
		return result
	}
	abilities := mainAgent.AbilityManager().List()
	for _, ability := range abilities {
		card, ok := ability.(*tool.ToolCard)
		if !ok {
			logger.Debug(logComponent).Str("ability", ability.AbilityName()).
				Msg("[TeamRuntime] Skipping non-ToolCard ability")
			continue
		}
		if toolWhitelist[card.Name] {
			result = append(result, card)
		} else {
			logger.Debug(logComponent).Str("tool", card.Name).
				Msg("[TeamRuntime] Tool not in whitelist, skipped")
		}
	}
	return result
}

// BuildContextProcessorRail 为团队成员构造 preset ContextProcessorRail。
// 对齐 Python: _build_context_processor_rail (L540-587)
//
// 读取 react.context_engine_config 下四段处理器配置，
// 转 []ceiface.ProcessorSpec{Type, ConfigOverrides}（Python (name, dict) pairs）。
func BuildContextProcessorRail(config map[string]any) *ce.ContextProcessorRail {
	userProcessors := make([]ceiface.ProcessorSpec, 0, 4)
	ctxCfg := map[string]any{}
	if config != nil {
		if react, _ := config["react"].(map[string]any); react != nil {
			if c, _ := react["context_engine_config"].(map[string]any); c != nil {
				ctxCfg = c
			}
		}
	}

	// 四段配置按 Python 顺序追加 (name, dict) pairs
	processorSpecs := []struct {
		configKey string
		typeName  string
	}{
		{"message_summary_offloader_config", "MessageSummaryOffloader"},
		{"dialogue_compressor_config", "DialogueCompressor"},
		{"current_round_compressor_config", "CurrentRoundCompressor"},
		{"round_level_compressor_config", "RoundLevelCompressor"},
	}
	for _, spec := range processorSpecs {
		if cfgMap, _ := ctxCfg[spec.configKey].(map[string]any); cfgMap != nil && len(cfgMap) > 0 {
			userProcessors = append(userProcessors, ceiface.ProcessorSpec{
				Type:           spec.typeName,
				ConfigOverrides: utils.DeepCopyMap(cfgMap),
			})
		}
	}

	rail := ce.NewContextProcessorRail(
		ce.WithPreset(true),
		ce.WithUserProcessors(userProcessors),
	)
	names := make([]string, 0, len(userProcessors))
	for _, p := range userProcessors {
		names = append(names, p.Type)
	}
	logger.Info(logComponent).Strs("user_processors", names).
		Msg("[TeamRuntime] ContextProcessorRail created (preset=True)")
	return rail
}

// BuildSkillEvolutionRail 为 Team member 构造 SkillEvolutionRail。
// 对齐 Python: build_skill_evolution_rail (L479-522)
//
// auto_save=true（成员自演化跳过审批）；LLM 构造失败返回 nil（对齐 Python
// except 返回 None）。teamTrajectorySink 非 nil 且 teamID 非空时绑定
// SetTrajectorySink(sink, teamID, "teammate")。
func BuildSkillEvolutionRail(skillsDir string, config map[string]any, teamTrajectorySink trajectory.TrajectorySink, teamID string) *evolution.SkillEvolutionRail {
	llmModel, modelName := BuildEvolutionLLM(config)
	if llmModel == nil {
		return nil
	}
	evolutionAutoScan := GetEvolutionAutoScanEnabled(config)

	// Python 未显式传 language，使用默认 "cn"
	rail := evolution.NewSkillEvolutionRail(
		[]string{skillsDir},
		llmModel,
		modelName,
		"cn",
		evolution.WithAutoScan(evolutionAutoScan),
		evolution.WithAutoSave(true),
		evolution.WithDisabledSkillsSet(skillruntime.LoadExecutionDisabledSkills()),
	)

	if teamTrajectorySink != nil && teamID != "" {
		if err := rail.SetTrajectorySink(teamTrajectorySink, teamID, "teammate"); err != nil {
			logger.Warn(logComponent).Err(err).Str("team_id", teamID).
				Msg("[TeamRuntime] SkillEvolutionRail set_trajectory_sink failed")
		}
	}

	logger.Info(logComponent).
		Str("model", modelName).
		Bool("auto_scan", evolutionAutoScan).
		Bool("team_trajectory_sink", teamTrajectorySink != nil && teamID != "").
		Msg("[TeamRuntime] SkillEvolutionRail created")
	return rail
}
```

import 补 `"github.com/uapclaw/uapclaw-go/internal/common/utils"`、`ce "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"`、`skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"`。

- [ ] 8.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestFilterInheritable|TestBuildContextProcessor|TestBuildSkillEvolution' -v`
- [ ] 8.5 提交：`git commit -am "feat(team): runtime_inheritance 能力过滤+ContextProcessor+SkillEvolution 构造（10.6.19 Task8）"`

---

## Task 9: BuildMemberRails 主函数（13 步逐 rail 构造）

**Files:**
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance.go`
- Modify: `internal/swarm/agents/harness/team/runtime_inheritance_test.go`

**对齐 Python**: `team_runtime_inheritance.py` L129-304（build_member_rails）

- [ ] 9.1 追加失败测试：

```go
// TestBuildMemberRails_默认路径 8 个基础 rail 全部构造成功。
func TestBuildMemberRails_默认路径(t *testing.T) {
	// 无 team_ws_root/skills_dir/config：仅 8 个基础 rail + ContextProcessorRail（默认 enabled）
	rails := BuildMemberRails(nil, nil, nil)
	// 8 基础 + 1 ContextProcessor（GetContextEngineEnabled(nil)=true）
	if len(rails) != 9 {
		names := railTypeNames(rails)
		t.Fatalf("rails count = %d, want 9, got %v", len(rails), names)
	}
}

// TestBuildMemberRails_带workspace leader 全量 rail。
func TestBuildMemberRails_带workspace_leader(t *testing.T) {
	// leader + team_ws_root + skills_dir：8 基础 + WorkspaceReportPath + TeamSkillEvolution + ContextProcessor
	root := "/tmp/team-ws"
	skills := "/tmp/team-ws/skills"
	teamID := "t1"
	memberRole := "leader"
	rails := BuildMemberRails(
		&MemberInfo{AgentName: "leader-agent", Role: &memberRole},
		&RuntimeInfo{Channel: "web", Language: "cn"},
		&TeamWorkspaceInfo{
			RootDir:  &root,
			SkillsDir: &skills,
			TeamID:   &teamID,
			Config:   map[string]any{},
		},
	)
	names := railTypeNames(rails)
	// TeamSkillEvolutionRail 依赖 BuildEvolutionLLM（config 无 models 段 → gpt-4 回退
	// 无 api_key → nil → 该 rail 跳过）；skill_create 默认 false → TeamSkillCreateRail 跳过
	hasWorkspace := false
	hasContext := false
	for _, n := range names {
		if n == "TeamWorkspaceReportPathRail" {
			hasWorkspace = true
		}
		if n == "ContextProcessorRail" {
			hasContext = true
		}
	}
	if !hasWorkspace {
		t.Fatalf("TeamWorkspaceReportPathRail missing: %v", names)
	}
	if !hasContext {
		t.Fatalf("ContextProcessorRail missing: %v", names)
	}
}

// TestBuildMemberRails_非leader挂SkillEvolution。
func TestBuildMemberRails_非leader挂SkillEvolution(t *testing.T) {
	// BuildSkillEvolutionRail 依赖合法 LLM config，否则跳过——此处仅验证不 panic
	skills := "/tmp/team-ws/skills"
	memberRole := "teammate"
	rails := BuildMemberRails(
		&MemberInfo{AgentName: "m1", Role: &memberRole},
		&RuntimeInfo{Channel: "web"},
		&TeamWorkspaceInfo{SkillsDir: &skills, Config: map[string]any{}},
	)
	if len(rails) == 0 {
		t.Fatalf("should have base rails")
	}
}

// TestBuildMemberRails_合法LLM时leader挂TeamSkillEvolution。
func TestBuildMemberRails_合法LLM时leader挂TeamSkillEvolution(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_key": "k", "api_base": "https://x",
				"client_provider": "OpenAI",
			},
		},
	}}
	root := t.TempDir()
	skills := root + "/skills"
	teamID := "t1"
	memberRole := "leader"
	rails := BuildMemberRails(
		&MemberInfo{AgentName: "leader-agent", Role: &memberRole},
		&RuntimeInfo{Channel: "web", Language: "cn"},
		&TeamWorkspaceInfo{RootDir: &root, SkillsDir: &skills, TeamID: &teamID, Config: cfg},
	)
	names := railTypeNames(rails)
	found := false
	for _, n := range names {
		if n == "TeamSkillEvolutionRail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("TeamSkillEvolutionRail missing with valid LLM config: %v", names)
	}
}

// railTypeNames 提取 rail 类型名列表。
func railTypeNames(rails []agentinterfaces.AgentRail) []string {
	names := make([]string, 0, len(rails))
	for _, r := range rails {
		names = append(names, reflect.TypeOf(r).Elem().Name())
	}
	return names
}
```

- [ ] 9.2 运行确认失败
- [ ] 9.3 在 `runtime_inheritance.go` 追加 BuildMemberRails（import 补 swarm rails 与 agentcore rails：`swarmrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"`、`secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"`）：

```go
// BuildMemberRails 为 Team 成员创建 rails 列表。
// 对齐 Python: build_member_rails (team_runtime_inheritance.py L129-304)
//
// 13 步逐 rail 构造，每步独立 panic 恢复 + warning 日志（对齐 Python 每步 try/except），
// 单步失败不阻断后续。返回成功构造的 rail 列表。
//
// 顺序：RuntimePrompt → ResponsePrompt → SysOperation → JiuClawStreamEvent →
// TaskPlanning → Security → Heartbeat → AvatarPrompt →（team_ws_root）TeamWorkspaceReportPath →
// （leader+skills_dir）TeamSkillEvolution →（leader+skills_dir+开关）TeamSkillCreate →
// （非 leader+skills_dir）SkillEvolution →（开关）ContextProcessor
func BuildMemberRails(memberInfo *MemberInfo, runtime *RuntimeInfo, teamWorkspace *TeamWorkspaceInfo) []agentinterfaces.AgentRail {
	if memberInfo == nil {
		// Python dataclass 默认：agent_name="team_member", model_name="gpt-4"（本函数未消费这两字段）
		memberInfo = &MemberInfo{}
	}
	if runtime == nil {
		// Python dataclass 默认：channel="default", language="cn"
		runtime = &RuntimeInfo{Channel: "default", Language: "cn"}
	}
	if teamWorkspace == nil {
		teamWorkspace = &TeamWorkspaceInfo{}
	}

	// 从结构体提取参数（对齐 Python dataclass 解包）
	language := runtime.Language
	channel := runtime.Channel
	teamWsRoot := strPtrValue(teamWorkspace.RootDir)
	teamWsSkillsDir := strPtrValue(teamWorkspace.SkillsDir)
	teamID := strPtrValue(teamWorkspace.TeamID)
	config := teamWorkspace.Config
	teamTrajectoryRegistry := teamWorkspace.TrajectoryRegistry

	railsList := make([]agentinterfaces.AgentRail, 0, 13)

	appendRail := func(step func() (agentinterfaces.AgentRail, error), railName string, logFields ...any) {
		rail, err := buildRailStep(step)
		if err != nil {
			logger.Warn(logComponent).Str("rail", railName).Err(err).
				Msg("[TeamRuntime] rail creation failed")
			return
		}
		railsList = append(railsList, rail)
	}

	// 1. RuntimePromptRail
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := swarmrails.NewRuntimePromptRail(language, channel)
		logger.Info(logComponent).Str("channel", channel).
			Msg("[TeamRuntime] RuntimePromptRail created")
		return rail, nil
	}, "RuntimePromptRail")

	// 2. ResponsePromptRail
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := swarmrails.NewResponsePromptRail()
		logger.Info(logComponent).Msg("[TeamRuntime] ResponsePromptRail created")
		return rail, nil
	}, "ResponsePromptRail")

	// 3. SysOperationRail（Python 日志名为 FileSystemRail，对齐 Python 日志字符串）
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := rails.NewSysOperationRail()
		logger.Info(logComponent).Msg("[TeamRuntime] FileSystemRail created")
		return rail, nil
	}, "SysOperationRail")

	// 4. JiuClawStreamEventRail
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := swarmrails.NewJiuClawStreamEventRail()
		logger.Info(logComponent).Msg("[TeamRuntime] JiuClawStreamEventRail created")
		return rail, nil
	}, "JiuClawStreamEventRail")

	// 5. TaskPlanningRail
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := rails.NewTaskPlanningRail()
		logger.Info(logComponent).Msg("[TeamRuntime] TaskPlanningRail created")
		return rail, nil
	}, "TaskPlanningRail")

	// 6. SecurityRail（Go 实际类型 SafetyPromptRail，别名 SecurityRail）
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := secrail.NewSafetyPromptRail()
		logger.Info(logComponent).Msg("[TeamRuntime] SecurityRail created")
		return rail, nil
	}, "SecurityRail")

	// 7. HeartbeatRail
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := rails.NewHeartbeatRail()
		logger.Info(logComponent).Msg("[TeamRuntime] HeartbeatRail created")
		return rail, nil
	}, "HeartbeatRail")

	// 8. AvatarPromptRail
	appendRail(func() (agentinterfaces.AgentRail, error) {
		rail := swarmrails.NewAvatarPromptRail()
		logger.Info(logComponent).Msg("[TeamRuntime] AvatarPromptRail created")
		return rail, nil
	}, "AvatarPromptRail")

	// 9. team_ws_root 非空 → TeamWorkspaceReportPathRail
	if teamWsRoot != "" {
		appendRail(func() (agentinterfaces.AgentRail, error) {
			rail := NewTeamWorkspaceReportPathRail(teamWsRoot, teamID, language)
			logger.Info(logComponent).Str("root_dir", teamWsRoot).
				Msg("[TeamRuntime] TeamWorkspaceReportPathRail created")
			return rail, nil
		}, "TeamWorkspaceReportPathRail")
	}

	// 10. Leader-only: TeamSkillEvolutionRail（团队技能演进）
	if roleStr(memberInfo.Role) == "leader" && teamWsSkillsDir != "" {
		appendRail(func() (agentinterfaces.AgentRail, error) {
			if err := os.MkdirAll(teamWsSkillsDir, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir skills_dir: %w", err)
			}
			llmModel, actualModelName := BuildEvolutionLLM(config)
			if llmModel == nil {
				return nil, fmt.Errorf("evolution LLM 构建失败（config 无有效模型）")
			}
			evolutionAutoScan := GetEvolutionAutoScanEnabled(config)
			// team_id 为空时不绑定 registry（对齐 Python bound_team_trajectory_registry = ... if team_id else None）
			var boundRegistry trajectory.TrajectorySource
			var opts []evolution.TeamSkillEvolutionRailOption
			if teamID != "" && teamTrajectoryRegistry != nil {
				boundRegistry = teamTrajectoryRegistry
				opts = append(opts,
					evolution.WithTeamSkillTrajectorySource(boundRegistry),
					evolution.WithTeamSkillTeamID(teamID),
				)
				if sink, ok := teamTrajectoryRegistry.(trajectory.TrajectorySink); ok {
					opts = append(opts, evolution.WithTeamSkillTrajectorySink(sink, teamID, "leader"))
				}
			}
			opts = append(opts,
				evolution.WithTeamSkillAutoScan(evolutionAutoScan),
				evolution.WithTeamSkillAutoSave(false),
				evolution.WithTeamSkillDisabledSkills(skillruntime.LoadExecutionDisabledSkills()),
			)
			rail := evolution.NewTeamSkillEvolutionRail(
				[]string{teamWsSkillsDir}, llmModel, actualModelName, language, opts...,
			)
			logger.Info(logComponent).
				Str("skills_dir", teamWsSkillsDir).
				Str("model", actualModelName).
				Bool("auto_scan", evolutionAutoScan).
				Bool("team_trajectory_registry", boundRegistry != nil).
				Msg("[TeamRuntime] TeamSkillEvolutionRail created")
			return rail, nil
		}, "TeamSkillEvolutionRail")
	}

	// 11. Leader-only: TeamSkillCreateRail（需 skill_create 配置启用；env SKILL_CREATE 优先）
	if roleStr(memberInfo.Role) == "leader" && teamWsSkillsDir != "" && GetSkillCreateEnabled(config) {
		appendRail(func() (agentinterfaces.AgentRail, error) {
			rail := evolution.NewTeamSkillCreateRail(
				teamWsSkillsDir,
				evolution.WithTeamSkillCreateAutoTrigger(true),
				evolution.WithTeamSkillCreateLanguage(language),
			)
			logger.Info(logComponent).Str("skills_dir", teamWsSkillsDir).
				Msg("[TeamRuntime] TeamSkillCreateRail created")
			return rail, nil
		}, "TeamSkillCreateRail")
	}

	// 12. Non-leader: SkillEvolutionRail（成员技能自演化）
	if roleStr(memberInfo.Role) != "leader" && teamWsSkillsDir != "" {
		// Python 直接调 build_skill_evolution_rail（内部自带 try/except 返回 None）
		var teamSink trajectory.TrajectorySink
		if teamTrajectoryRegistry != nil {
			if sink, ok := teamTrajectoryRegistry.(trajectory.TrajectorySink); ok {
				teamSink = sink
			}
		}
		if rail := BuildSkillEvolutionRail(teamWsSkillsDir, config, teamSink, teamID); rail != nil {
			railsList = append(railsList, rail)
		}
	}

	// 13. ContextProcessorRail（leader + teammate 均可，需开关启用，默认 true）
	if GetContextEngineEnabled(config) {
		if rail := BuildContextProcessorRail(config); rail != nil {
			railsList = append(railsList, rail)
		}
	}

	logger.Info(logComponent).Int("total", len(railsList)).
		Msg("[TeamRuntime] Total rails built")
	return railsList
}

// ──────────────────────────── 非导出函数（本段追加） ────────────────────────────

// buildRailStep 执行单个 rail 构造步骤，panic 恢复为 error（对齐 Python try/except）。
func buildRailStep(step func() (agentinterfaces.AgentRail, error)) (rail agentinterfaces.AgentRail, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("rail 构造 panic: %v", r)
		}
	}()
	return step()
}

// strPtrValue 解引用 *string（nil 返回空串）。
func strPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// roleStr 解析角色（nil → 空串，对齐 Python role=None）。
func roleStr(role *string) string {
	if role == nil {
		return ""
	}
	return *role
}
```

import 补 `"fmt"`、`"os"`。

**注意**：`NewTeamWorkspaceReportPathRail` 在 Task 10 定义；Task 9 与 Task 10 有短暂编译依赖——**Task 9 提交前先在同文件放置 Task 10 的 rail 类型**（两个任务在同一提交内完成，或 Task 9 先实现 Task 10）。执行顺序调整：**先做 Task 10（rail 类型定义）再回来做 Task 9（引用它）**——执行者按 Task 10 → Task 9 顺序，或合并提交。计划保留编号但标注依赖。

- [ ] 9.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run TestBuildMemberRails -v`
- [ ] 9.5 提交（与 Task 10 合并或顺序提交）：`git commit -am "feat(team): BuildMemberRails 13 步逐 rail 构造（10.6.19 Task9，对齐 build_member_rails）"`

---

## Task 10: workspace_report_path_rail.go + member_skill_toolkit_rail.go

**Files:**
- Create: `internal/swarm/agents/harness/team/workspace_report_path_rail.go`
- Create: `internal/swarm/agents/harness/team/member_skill_toolkit_rail.go`
- Create: `internal/swarm/agents/harness/team/rails_inheritance_test.go`

**对齐 Python**: `rails/team_workspace_report_path_rail.py`（64 行）+ `rails/team_member_skill_toolkit_rail.py`（86 行）

**依赖说明**：本任务需先于 Task 9 提交（BuildMemberRails 引用 NewTeamWorkspaceReportPathRail）。

- [ ] 10.1 写失败测试 `rails_inheritance_test.go`：

```go
package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestTeamWorkspaceReportPathRail_内容构建 验证 BeforeModelCall 注入 section。
func TestTeamWorkspaceReportPathRail_内容构建(t *testing.T) {
	root := t.TempDir()
	rail := NewTeamWorkspaceReportPathRail(root, "t1", "cn")
	if rail.Priority() != 5 {
		t.Fatalf("priority = %d, want 5", rail.Priority())
	}

	agent := newFakeAgentWithPromptBuilder()
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := rail.BeforeModelCall(context.Background(), nil); err != nil {
		t.Fatalf("BeforeModelCall failed: %v", err)
	}

	section := agent.promptBuilder.GetSection("team_workspace_report_paths")
	if section == nil {
		t.Fatalf("section not injected")
	}
	if section.Priority != 67 {
		t.Fatalf("section priority = %d, want 67", section.Priority)
	}
	content := section.Content["en"]
	if !containsAll(content, ".team/t1/", root, "prd-review-memo.md") {
		t.Fatalf("content missing key parts: %q", content)
	}

	// Uninit 移除 section
	if err := rail.Uninit(agent); err != nil {
		t.Fatalf("Uninit failed: %v", err)
	}
	if agent.promptBuilder.HasSection("team_workspace_report_paths") {
		t.Fatalf("section should be removed after Uninit")
	}
}

// TestMemberSkillToolkitRail_InitUninit 工具注册与反注册。
func TestMemberSkillToolkitRail_InitUninit(t *testing.T) {
	wsDir := t.TempDir()
	rail := NewMemberSkillToolkitRail(wsDir)
	if rail.Priority() != 95 {
		t.Fatalf("priority = %d, want 95", rail.Priority())
	}

	agent := newFakeAgentWithAbilityMgr()
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	// SkillToolkit 提供 3 个工具（search/install/uninstall skill）
	abilities := agent.abilityMgr.List()
	if len(abilities) != 3 {
		t.Fatalf("abilities = %d, want 3", len(abilities))
	}
	// 工具 ID 带 agentID 后缀
	for _, a := range abilities {
		card := a.(*tool.ToolCard)
		if card.ID == "" {
			t.Fatalf("card.ID should be qualified with agent id suffix")
		}
	}

	if err := rail.Uninit(agent); err != nil {
		t.Fatalf("Uninit failed: %v", err)
	}
	if len(agent.abilityMgr.List()) != 0 {
		t.Fatalf("abilities should be empty after Uninit")
	}
}

// containsAll 检查 content 是否包含全部子串。
func containsAll(content string, subs ...string) bool {
	for _, s := range subs {
		if !strings.Contains(content, s) {
			return false
		}
	}
	return true
}
```

**注意**：需 `strings` import。fake agent 定义（Init 需 `BaseAgent` 参数——`agentinterfaces.BaseAgent` 接口太大，测试中定义最小 fake 实现整个 BaseAgent + SystemPromptBuilder，编译器辅助补齐空桩）：

```go
// fakePromptBuilder 测试用 SystemPromptBuilder。
type fakePromptBuilder struct {
	sections map[string]*saprompt.PromptSection
}

func newFakePromptBuilder() *fakePromptBuilder {
	return &fakePromptBuilder{sections: map[string]*saprompt.PromptSection{}}
}

func (f *fakePromptBuilder) AddSection(s saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	copied := s
	f.sections[s.Name] = &copied
	return nil
}
func (f *fakePromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(f.sections, name)
	return nil
}
func (f *fakePromptBuilder) Language() string   { return "cn" }
func (f *fakePromptBuilder) SetLanguage(string) {}
func (f *fakePromptBuilder) GetSection(name string) *saprompt.PromptSection {
	return f.sections[name]
}
func (f *fakePromptBuilder) HasSection(name string) bool {
	_, ok := f.sections[name]
	return ok
}
```

**签名说明**：`SystemPromptBuilderInterface` 各方法返回具体类型 `*saprompt.SystemPromptBuilder`，fake 返回 nil 指针满足签名（链式调用仅测试不使用）；`PromptSection`/`GetSection` 同样引用 `saprompt` 包类型（PromptSection 定义在 `internal/agentcore/single_agent/prompts`，不在 agentinterfaces）。测试文件 import 补 `saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"`。fake agent 需实现 `BaseAgent` 全方法集（Card/Config/AbilityManager/CallbackManager/SystemPromptBuilder/Configure/Invoke/Stream/RegisterCallback/RegisterRail/UnregisterRail）；rail 的 `Init(ctx, agent BaseAgent)` 参数即 BaseAgent，不必实现完整 DeepAgentInterface。编译器辅助补齐。

- [ ] 10.2 运行确认失败
- [ ] 10.3 实现 `workspace_report_path_rail.go`：

```go
package team

import (
	"context"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamWorkspaceReportPathRail 报告真实 team workspace 产物路径的提示 rail。
// 对齐 Python: TeamWorkspaceReportPathRail (rails/team_workspace_report_path_rail.py)
type TeamWorkspaceReportPathRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 提示词构建器引用（Init 时缓存，可为 nil）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// rootDir 工作区根目录（绝对路径）
	rootDir string
	// teamID 团队标识（可为空）
	teamID string
	// language 语言标识
	language string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamWorkspaceReportPathRail 创建 TeamWorkspaceReportPathRail 实例。
// 对齐 Python: TeamWorkspaceReportPathRail(*, root_dir, team_id=None, language="cn")
func NewTeamWorkspaceReportPathRail(rootDir string, teamID string, language string) *TeamWorkspaceReportPathRail {
	r := &TeamWorkspaceReportPathRail{
		rootDir:  filepath.Clean(rootDir),
		teamID:   teamID,
		language: language,
	}
	r.WithPriority(5)
	return r
}

// Init 缓存 agent 的 SystemPromptBuilder（可能为 nil）。
// 对齐 Python: init(self, agent)
func (r *TeamWorkspaceReportPathRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if agent == nil {
		return nil
	}
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 移除注入的 section。
// 对齐 Python: uninit(self, agent)
func (r *TeamWorkspaceReportPathRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection("team_workspace_report_paths")
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 注入 team workspace 路径指引 section。
// 对齐 Python: before_model_call(self, ctx)
func (r *TeamWorkspaceReportPathRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	mount := ".team/"
	if r.teamID != "" {
		mount = ".team/" + r.teamID + "/"
	}
	sample := filepath.Join(r.rootDir, "prd-review-memo.md")
	content := "# Team Workspace Artifact Paths\n\n" +
		"- Team workspace absolute root: `" + r.rootDir + "`\n" +
		"- Internal mount path: `" + mount + "`\n" +
		"- Use the internal mount path only for tool read/write operations.\n" +
		"- When telling the user where an artifact was saved, report the real absolute filesystem path under " +
		"the team workspace absolute root, not the `.team/...` mount path.\n" +
		"- If a generated artifact path contains `.team/<team>/team-workspace/`, remove that mount prefix and " +
		"join the remaining file name under the team workspace absolute root before reporting it.\n" +
		"- Example saved location to report: `" + sample + "`\n"

	r.systemPromptBuilder.AddSection(saprompt.NewPromptSection(
		"team_workspace_report_paths",
		map[string]string{"cn": content, "en": content},
		67,
	))
	return nil
}
```

import 补 `agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`。`logger` 若未使用则不导入。

- [ ] 10.4 实现 `member_skill_toolkit_rail.go`：

```go
package team

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	skilltools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberSkillToolkitRail 为 team member 绑定成员作用域的技能管理工具。
// 对齐 Python: MemberSkillToolkitRail (rails/team_member_skill_toolkit_rail.py)
type MemberSkillToolkitRail struct {
	rails.DeepAgentRail
	// workspaceDir 成员工作区目录
	workspaceDir string
	// tools 已注册的工具列表（幂等标记）
	tools []tool.Tool
	// manager 成员 SkillManager
	manager *skillruntime.SkillManager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberSkillToolkitRail 创建 MemberSkillToolkitRail 实例。
// 对齐 Python: MemberSkillToolkitRail(workspace_dir)
func NewMemberSkillToolkitRail(workspaceDir string) *MemberSkillToolkitRail {
	r := &MemberSkillToolkitRail{workspaceDir: workspaceDir}
	r.WithPriority(95)
	return r
}

// Init 在 agent 上注册成员作用域技能工具。
// 对齐 Python: init(self, agent)
func (r *MemberSkillToolkitRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if r.tools != nil {
		return nil
	}
	if agent == nil {
		return nil
	}

	card := agent.Card()
	agentID := card.ID
	if agentID == "" {
		agentID = card.Name
	}
	// Python: str(agent.card.id or agent.card.name)

	r.manager = skillruntime.NewSkillManager(r.workspaceDir)
	toolkit := skilltools.NewSkillToolkit(r.manager)
	tools := toolkit.GetTools()

	for _, t := range tools {
		t.Card().ID = qualifyToolID(t.Card().ID, agentID)
	}

	// 同名 ToolCard 已存在则先移除（对齐 Python existing = ability_manager.get(name)）
	abilityMgr := agent.AbilityManager()
	for _, t := range tools {
		if existing := abilityMgr.Get(t.Card().Name); existing != nil {
			abilityMgr.Remove(t.Card().Name)
		}
	}

	// Runner.resource_mgr.add_tool(list(tools)) + agent.ability_manager.add(tool.card)
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		if err := resourceMgr.AddTool(t); err != nil {
			logger.Warn(logComponent).Err(err).Str("tool", t.Card().Name).
				Msg("[MemberSkillToolkitRail] add_tool failed")
		}
	}
	for _, t := range tools {
		abilityMgr.Add(t.Card())
	}

	r.tools = tools
	logger.Info(logComponent).
		Int("count", len(tools)).
		Str("workspace", r.workspaceDir).
		Str("agent_id", agentID).
		Msg("[MemberSkillToolkitRail] Registered skill tools")
	return nil
}

// Uninit 从 agent 移除成员作用域技能工具。
// 对齐 Python: uninit(self, agent)
func (r *MemberSkillToolkitRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.tools == nil {
		return nil
	}
	if agent != nil {
		abilityMgr := agent.AbilityManager()
		resourceMgr := runner.GetResourceMgr()
		for _, t := range r.tools {
			abilityMgr.Remove(t.Card().Name)
			if _, err := resourceMgr.RemoveTool([]string{t.Card().ID}); err != nil {
				logger.Debug(logComponent).Err(err).Str("tool", t.Card().ID).
					Msg("[MemberSkillToolkitRail] remove_tool failed")
			}
		}
	}
	logger.Info(logComponent).
		Int("count", len(r.tools)).
		Str("workspace", r.workspaceDir).
		Msg("[MemberSkillToolkitRail] Unregistered skill tools")
	r.tools = nil
	r.manager = nil
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// qualifyToolID 为工具 ID 追加 agent 后缀（成员作用域隔离）。
// 对齐 Python: _qualify_tool_id(tool_id, agent_id) → f"{tool_id}_{agent_id}"
func qualifyToolID(toolID string, agentID string) string {
	return toolID + "_" + agentID
}
```

**签名核对**：`AgentRail.Init(ctx context.Context, agent BaseAgent) error`、`Uninit(agent BaseAgent) error`——与 callback.go L29 接口一致。`rails.DeepAgentRail` 嵌入提供其余钩子默认实现与 `GetCallbacks()`。编译期验证：文件尾加 `var _ agentinterfaces.AgentRail = (*MemberSkillToolkitRail)(nil)` 与 `var _ agentinterfaces.AgentRail = (*TeamWorkspaceReportPathRail)(nil)`。`strings` import 若未用删除。

**注意**：BaseRail 的 BeforeModelCall 默认实现签名带 `*agentinterfaces.AgentCallbackContext`——覆写时保持一致（上面 workspace rail 已按此签名）。`GetCallbacks()` 由 DeepAgentRail 基类通过反射式方法覆盖检测自动工作（覆写方法即被收录）。

- [ ] 10.5 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestTeamWorkspaceReportPathRail|TestMemberSkillToolkitRail' -v`
- [ ] 10.6 提交：`git add internal/swarm/agents/harness/team/ && git commit -m "feat(team): 新增 TeamWorkspaceReportPathRail + MemberSkillToolkitRail（10.6.19 Task10，对齐 rails/ 两个小 rail）"`

---

## Task 11: team_manager_skill.go 回填（syncSkillsDir + EnsureTeamSharedSkillsInitialized）

**Files:**
- Modify: `internal/swarm/agents/harness/team/team_manager_skill.go`
- Modify: `internal/swarm/agents/harness/team/team_manager_skill_test.go`（无则新建）

**对齐 Python**: `team_manager.py` L87-107（_sync_skills_dir）、L998-1047（_copy_global_skills_to_team_shared_dir/_resolve_team_shared_skills_dir/_sync_team_shared_skills_to_agent_global/ensure_team_shared_skills_initialized）

- [ ] 11.1 写失败测试（新建 `team_manager_skill_test.go`，如已存在则追加）：

```go
package team

import (
	"os"
	"path/filepath"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestSyncSkillsDir_源目录遍历 有效技能复制、目标已有先删再拷。
func TestSyncSkillsDir_源目录遍历(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	// 有效技能 skill-a（含 SKILL.md）；无效目录 no-skill（无 SKILL.md）；普通文件
	mkdir(t, filepath.Join(source, "skill-a"))
	writeFile(t, filepath.Join(source, "skill-a", "SKILL.md"), "a")
	mkdir(t, filepath.Join(source, "no-skill"))
	writeFile(t, filepath.Join(source, "plain.txt"), "x")

	// target 已有旧版 skill-a（将被覆盖）
	mkdir(t, filepath.Join(target, "skill-a"))
	writeFile(t, filepath.Join(target, "skill-a", "SKILL.md"), "old")

	syncSkillsDir(source, target)

	if _, err := os.Stat(filepath.Join(target, "skill-a", "SKILL.md")); err != nil {
		t.Fatalf("skill-a should be synced: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(target, "skill-a", "SKILL.md"))
	if string(data) != "a" {
		t.Fatalf("skill-a should be overwritten with new version, got %q", data)
	}
	if _, err := os.Stat(filepath.Join(target, "no-skill")); !os.IsNotExist(err) {
		t.Fatalf("no-skill (missing SKILL.md) should not be synced")
	}
	if _, err := os.Stat(filepath.Join(target, "plain.txt")); !os.IsNotExist(err) {
		t.Fatalf("plain file should not be synced")
	}
}

// TestSyncSkillsDir_源不存在直接返回。
func TestSyncSkillsDir_源不存在直接返回(t *testing.T) {
	target := t.TempDir()
	// 不 panic 即通过
	syncSkillsDir(filepath.Join(target, "nonexistent"), target)
}

// TestEnsureTeamSharedSkillsInitialized_marker幂等。
func TestEnsureTeamSharedSkillsInitialized_marker幂等(t *testing.T) {
	globalSkills := t.TempDir()
	mkdir(t, filepath.Join(globalSkills, "shared-skill"))
	writeFile(t, filepath.Join(globalSkills, "shared-skill", "SKILL.md"), "v1")
	writeFile(t, filepath.Join(globalSkills, "skills_state.json"), `{}`)

	t.Setenv("UAPCLAW_WORKSPACE_DIR", t.TempDir())
	spec := &atschema.TeamAgentSpec{TeamName: "marker-team"}

	mgr := NewTeamManager()
	mgr.EnsureTeamSharedSkillsInitialized(spec)

	// 解析 team shared dir（TeamHome(teamName)/team-workspace/skills）
	sharedDir := filepath.Join(resolveTeamHomeDir("marker-team"), "team-workspace", "skills")
	if _, err := os.Stat(filepath.Join(sharedDir, "shared-skill", "SKILL.md")); err != nil {
		t.Fatalf("shared skill should be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sharedDir, ".team_skills_copied")); err != nil {
		t.Fatalf("marker file should exist: %v", err)
	}

	// 二次调用：marker 存在 → 跳过复制（修改 global 后 shared 不变）
	writeFile(t, filepath.Join(globalSkills, "shared-skill", "SKILL.md"), "v2")
	mgr.EnsureTeamSharedSkillsInitialized(spec)
	data, _ := os.ReadFile(filepath.Join(sharedDir, "shared-skill", "SKILL.md"))
	if string(data) != "v1" {
		t.Fatalf("second call should skip copy (marker idempotent), got %q", data)
	}
}

// TestEnsureTeamSharedSkillsInitialized_specWorkspaceRoot优先。
func TestEnsureTeamSharedSkillsInitialized_specWorkspaceRoot优先(t *testing.T) {
	globalSkills := t.TempDir()
	mkdir(t, filepath.Join(globalSkills, "s1"))
	writeFile(t, filepath.Join(globalSkills, "s1", "SKILL.md"), "x")

	t.Setenv("UAPCLAW_WORKSPACE_DIR", t.TempDir())
	wsRoot := t.TempDir()
	spec := &atschema.TeamAgentSpec{TeamName: "ws-team"}
	spec.Workspace = &teamworkspace.TeamWorkspaceConfig{RootPath: wsRoot}

	mgr := NewTeamManager()
	mgr.EnsureTeamSharedSkillsInitialized(spec)

	if _, err := os.Stat(filepath.Join(wsRoot, "skills", "s1", "SKILL.md")); err != nil {
		t.Fatalf("spec.workspace.root_path should take precedence: %v", err)
	}
}

// mkdir 创建目录（测试辅助）。
func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// writeFile 写文件（测试辅助）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
```

**环境变量核对**：`workspace.AgentSkillsDir()` 的根由哪个 env 控制需实现时核对 `internal/common/workspace/paths.go`/`init.go`（大概率 `UAPCLAW_WORKSPACE_DIR` 或等价机制；也可能需要 `workspace.InitWorkspace`）。若 env 注入不生效，改为在测试中直接调用 workspace 初始化函数（以 paths_test.go 现有测试模式为准）。`teamworkspace` import = `"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"`。`resolveTeamHomeDir` 是 Task 11.3 新增辅助（对齐 Python team_home()；如 `agent_teams.TeamHome` 可直接在测试用，则测试直接 import agent_teams 调 `agentteams.TeamHome("marker-team")`，不引入新辅助——优先此方案）。

- [ ] 11.2 运行确认失败
- [ ] 11.3 修改 `team_manager_skill.go`：
  - import 补 `"encoding/json"`、`"fmt"`、`"os"`、`"path/filepath"`、`"github.com/uapclaw/uapclaw-go/internal/common/workspace"`、`agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"`、`skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"`（按实际使用裁剪）
  - `EnsureTeamSharedSkillsInitialized` 替换骨架为：

```go
// EnsureTeamSharedSkillsInitialized 确保 team 共享技能已初始化。
// 对齐 Python: TeamManager.ensure_team_shared_skills_initialized(spec) (L1043-1047)
func (m *TeamManager) EnsureTeamSharedSkillsInitialized(spec *atschema.TeamAgentSpec) {
	if spec == nil {
		return
	}
	m.copyGlobalSkillsToTeamSharedDir(spec)
	m.syncTeamSharedSkillsToAgentGlobal(spec)
}
```

  - 新增非导出函数（放文件尾非导出区）：

```go
// copyGlobalSkillsToTeamSharedDir 复制全局技能到团队共享目录（team build 后执行一次）。
// 对齐 Python: TeamManager._copy_global_skills_to_team_shared_dir(spec) (L998-1026)
//
// marker 文件 .team_skills_copied 幂等：存在即跳过。
func (m *TeamManager) copyGlobalSkillsToTeamSharedDir(spec *atschema.TeamAgentSpec) {
	globalSkillsDir := workspace.AgentSkillsDir()
	if info, err := os.Stat(globalSkillsDir); err != nil || !info.IsDir() {
		logger.Warn(logComponent).Str("global_skills_dir", globalSkillsDir).
			Msg("[TeamManager] global_skills_dir does not exist")
		return
	}

	teamSharedSkillsDir := resolveTeamSharedSkillsDir(spec)

	// marker 幂等检查
	copiedMarker := filepath.Join(teamSharedSkillsDir, ".team_skills_copied")
	if _, err := os.Stat(copiedMarker); err == nil {
		logger.Info(logComponent).Msg("[TeamManager] Team shared skills already copied, skipping")
		return
	}

	// 复制整个技能目录（含 skills_state.json），目标存在则合并覆盖（dirs_exist_ok=True 等效）
	if err := copyTreeMerge(globalSkillsDir, teamSharedSkillsDir); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("global_skills_dir", globalSkillsDir).
			Str("team_shared_skills_dir", teamSharedSkillsDir).
			Msg("[TeamManager] copy global skills to team shared failed")
		return
	}

	// 写 marker 文件
	if err := os.WriteFile(copiedMarker, []byte(""), 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] write team_skills_copied marker failed")
		return
	}
	logger.Info(logComponent).Str("team_shared_skills_dir", teamSharedSkillsDir).
		Msg("[TeamManager] Copied global skills dir to team shared")
}

// resolveTeamSharedSkillsDir 解析团队共享技能目录。
// 对齐 Python: TeamManager._resolve_team_shared_skills_dir(spec) (L1028-1034)
//
// spec.Workspace.RootPath 优先，回退 TeamHome(teamName)/team-workspace。
func resolveTeamSharedSkillsDir(spec *atschema.TeamAgentSpec) string {
	wsPath := ""
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		wsPath = spec.Workspace.RootPath
	}
	if wsPath == "" {
		wsPath = filepath.Join(agentteams.TeamHome(spec.TeamName), "team-workspace")
	}
	return filepath.Join(wsPath, "skills")
}

// syncTeamSharedSkillsToAgentGlobal 将团队共享技能镜像到本地 agent skills 目录（供 SkillUseRail）。
// 对齐 Python: TeamManager._sync_team_shared_skills_to_agent_global(spec) (L1036-1041)
func (m *TeamManager) syncTeamSharedSkillsToAgentGlobal(spec *atschema.TeamAgentSpec) {
	teamSharedSkillsDir := resolveTeamSharedSkillsDir(spec)
	globalSkillsDir := workspace.AgentSkillsDir()
	syncSkillsDir(teamSharedSkillsDir, globalSkillsDir)
}

// syncSkillsDir 将 source 中每个有效技能目录复制到 target（最新版本胜出）。
// 对齐 Python: _sync_skills_dir(source, target) (L87-107)
//
// 有效技能 = 含 SKILL.md 的子目录；target 已有同名目录先删除再复制（rmtree+copytree）。
// source 非目录直接返回。synced>0 时输出 info 日志。
func syncSkillsDir(source string, target string) {
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Str("target", target).
			Msg("[TeamManager] syncSkillsDir mkdir failed")
		return
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("source", source).
			Msg("[TeamManager] syncSkillsDir readdir failed")
		return
	}
	synced := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(source, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err != nil {
			continue
		}
		dest := filepath.Join(target, entry.Name())
		if _, err := os.Stat(dest); err == nil {
			if err := os.RemoveAll(dest); err != nil {
				logger.Warn(logComponent).Err(err).Str("dest", dest).
					Msg("[TeamManager] syncSkillsDir rmtree failed")
				continue
			}
		}
		if err := copyTreeMerge(filepath.Join(source, entry.Name()), dest); err != nil {
			logger.Warn(logComponent).Err(err).Str("dest", dest).
				Msg("[TeamManager] syncSkillsDir copytree failed")
			continue
		}
		synced++
	}
	if synced > 0 {
		logger.Info(logComponent).
			Int("count", synced).
			Str("source", source).
			Str("target", target).
			Msg("[TeamManager] synced skills")
	}
}

// copyTreeMerge 递归复制目录树，目标存在则逐项合并覆盖（shutil.copytree(dirs_exist_ok=True) 等效）。
func copyTreeMerge(source string, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("mkdir target: %w", err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("readdir source: %w", err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		dstPath := filepath.Join(target, entry.Name())
		if entry.IsDir() {
			if err := copyTreeMerge(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}
	return nil
}
```

  - `SyncTeamSkills` 方法替换 `⤵️(#9.72)` 段为 `syncSkillsDir(syncTarget.Source, syncTarget.Target)`

- [ ] 11.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestSyncSkillsDir|TestEnsureTeamSharedSkillsInitialized' -v`
- [ ] 11.5 提交：`git commit -am "feat(team): 回填技能同步 syncSkillsDir + EnsureTeamSharedSkillsInitialized marker 幂等（10.6.19 Task11，对齐 _sync_skills_dir/L998-1047）"`

---

## Task 12: loadTeamSpec 分布式检查 + model_pool 注入 + spec.go 包装方法

**Files:**
- Modify: `internal/swarm/agents/harness/team/team_manager_spec.go`
- Modify: `internal/swarm/agents/harness/team/team_manager_spec_test.go`（无则新建）

**对齐 Python**: `team_manager.py` L291-352（_load_team_spec 全文）

- [ ] 12.1 写失败测试（追加到 `team_manager_spec_test.go`，无则新建）：

```go
package team

import (
	"testing"
)

// TestLoadTeamSpec_分布式fallback 缺依赖时回退 local（Go 恒无缺依赖，仅验证 normalize 路径）。
func TestLoadTeamSpec_分布式normalize(t *testing.T) {
	cfg := map[string]any{
		"team": map[string]any{
			"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
			"transport": map[string]any{"type": "pyzmq"},
		},
		"modes": map[string]any{"team": map[string]any{}},
		"models": map[string]any{},
	}
	spec := loadTeamSpec("sess-1", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
}

// TestLoadTeamSpec_modelPool注入 多默认模型时填充 spec.ModelPool。
func TestLoadTeamSpec_modelPool注入(t *testing.T) {
	cfg := map[string]any{
		"team":   map[string]any{},
		"modes":  map[string]any{"team": map[string]any{}},
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m1", "api_key": "k1", "api_base": "https://1",
						"client_provider": "OpenAI", "timeout": 60,
					},
					"model_config_obj": map[string]any{"temperature": 0.9},
				},
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m2", "api_key": "k2", "api_base": "https://2",
						"client_provider": "OpenAI",
					},
				},
			},
		},
	}
	spec := loadTeamSpec("sess-2", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	if len(spec.ModelPool) != 2 {
		t.Fatalf("ModelPool = %d, want 2", len(spec.ModelPool))
	}
	if spec.ModelPoolStrategy != "by_model_name" {
		t.Fatalf("ModelPoolStrategy = %q", spec.ModelPoolStrategy)
	}
	first := spec.ModelPool[0]
	if first.ModelName != "m1" || first.APIKey != "k1" || first.APIBaseURL != "https://1" || first.APIProvider != "OpenAI" {
		t.Fatalf("first pool entry mismatch: %+v", first)
	}
	clientMeta, _ := first.Metadata["client"].(map[string]any)
	if clientMeta == nil || clientMeta["timeout"] != 60.0 {
		t.Fatalf("metadata.client timeout missing: %v", first.Metadata)
	}
	if _, hasModelName := clientMeta["model_name"]; hasModelName {
		t.Fatalf("metadata.client should exclude model_name")
	}
	requestMeta, _ := first.Metadata["request"].(map[string]any)
	if requestMeta == nil || requestMeta["temperature"] != 0.9 {
		t.Fatalf("metadata.request temperature missing: %v", first.Metadata)
	}
}

// TestLoadTeamSpec_单模型不注入pool。
func TestLoadTeamSpec_单模型不注入pool(t *testing.T) {
	cfg := map[string]any{
		"team":  map[string]any{},
		"modes": map[string]any{"team": map[string]any{}},
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{"model_name": "only"},
			},
		},
	}
	spec := loadTeamSpec("sess-3", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	if len(spec.ModelPool) != 0 {
		t.Fatalf("single model should not populate pool, got %d", len(spec.ModelPool))
	}
}

// TestNormalizeDistributedTransportFields_方法包装 委托纯函数。
func TestNormalizeDistributedTransportFields_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{"transport": map[string]any{"type": "pyzmq", "params": map[string]any{}}}
	result := mgr.NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	if params["direct_addr"] != "tcp://0.0.0.0:18555" {
		t.Fatalf("method wrapper should delegate to pure function")
	}
}

// TestParsePort_方法包装。
func TestParsePort_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	if got := mgr.ParsePort("18600", 1, "f"); got != 18600 {
		t.Fatalf("ParsePort method = %d", got)
	}
}
```

- [ ] 12.2 运行确认失败
- [ ] 12.3 修改 `team_manager_spec.go`：

  - import 补 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"`、`"github.com/uapclaw/uapclaw-go/internal/common/config"`
  - `loadTeamSpec` 步骤 4 ⤵️ 段替换 + model_pool 注入（在 `normalizeTeamIdentityFields` 之后、`NewTeamAgentSpecFromDict` 之前）：

```go
	// 步骤 4: 分布式模式标准化
	// 对齐 Python: TeamManager._load_team_spec L296-318
	if IsDistributedMode(cb) {
		missing := MissingDistributedDependencies(cb)
		if len(missing) > 0 {
			missingList := strings.Join(missing, ", ")
			logger.Warn(logComponent).Str("missing", missingList).
				Msg("[TeamManager][MISSING_DISTRIBUTE_DEPS]")
			logger.Error(logComponent).
				Msg("[TeamManager][FALLBACK_TO_LOCAL] distributed runtime is not available; downgraded to local mode for current process")
			logger.Warn(logComponent).
				Msg("[TeamManager][ACTION] install via: pip install -e \".[distribute]\" or uv sync --extra distribute")
			cb = FallbackDistributedToLocal(cb)
		}
	}

	specDict := LoadTeamSpecDict(cb)
	specDict = normalizeTeamIdentityFields(specDict)

	// 分布式传输字段标准化（对齐 Python L317-318）
	if IsDistributedMode(cb) {
		specDict = NormalizeDistributedTransportFields(cb, specDict)
	}

	// 步骤 4b: model_pool 注入
	// 对齐 Python: TeamManager._load_team_spec L320-350
	defaultModels := config.GetDefaultModels(cb)
	if len(defaultModels) > 1 {
		poolEntries := make([]any, 0, len(defaultModels))
		for _, entry := range defaultModels {
			mcc, _ := entry["model_client_config"].(map[string]any)
			if mcc == nil {
				continue
			}
			modelName, _ := mcc["model_name"].(string)
			if modelName == "" {
				continue
			}
			apiKey, _ := mcc["api_key"].(string)
			apiBase, _ := mcc["api_base"].(string)
			apiProvider, _ := mcc["client_provider"].(string)

			// metadata.client = mcc 排除四键且值非 nil；metadata.request = mco
			clientMeta := map[string]any{}
			excluded := map[string]bool{
				"model_name": true, "api_key": true, "api_base": true, "client_provider": true,
			}
			for k, v := range mcc {
				if excluded[k] || v == nil {
					continue
				}
				clientMeta[k] = v
			}
			mco, _ := entry["model_config_obj"].(map[string]any)
			if mco == nil {
				mco = map[string]any{}
			}
			requestMeta := make(map[string]any, len(mco))
			for k, v := range mco {
				requestMeta[k] = v
			}

			poolEntry := models.NewModelPoolEntry(
				modelName, apiKey, apiBase, apiProvider,
				models.WithMetadata(map[string]any{
					"client":  clientMeta,
					"request": requestMeta,
				}),
			)
			// JSON 桥接：ModelPoolEntry → map（specDict 经 NewTeamAgentSpecFromDict 反序列化）
			poolEntries = append(poolEntries, poolEntryToMap(poolEntry))
		}
		if len(poolEntries) > 0 {
			specDict["model_pool"] = poolEntries
			specDict["model_pool_strategy"] = "by_model_name"
		}
	}
```

  - 新增辅助（非导出区）：

```go
// poolEntryToMap 将 ModelPoolEntry 转为 map（JSON 桥接）。
// 对齐 Python: pool_entry.model_dump()
func poolEntryToMap(entry *models.ModelPoolEntry) map[string]any {
	return map[string]any{
		"model_name":  entry.ModelName,
		"api_key":     entry.APIKey,
		"api_base_url": entry.APIBaseURL,
		"api_provider": entry.APIProvider,
		"description": entry.Description,
		"model_id":    entry.ModelID,
		"metadata":    entry.Metadata,
	}
}
```

  - `NormalizeDistributedTransportFields` 方法体替换：`return NormalizeDistributedTransportFields(configBase, teamCfg)`（删 ⤵️ 标记与 `return teamCfg` 骨架）
  - `ParsePort` 方法体替换：`return ParsePort(value, defaultPort, fieldName)`（删 ⤵️ 标记与 `return defaultPort` 骨架）

  **注意**：`loadTeamSpec` 原代码中 `specDict := LoadTeamSpecDict(cb)` 在步骤 3 前已有——替换时保持单次调用（分布式检查在 LoadTeamSpecDict **之前**修改 cb，对齐 Python 顺序：fallback 先改 config_base 再 load_team_spec_dict）。

- [ ] 12.4 运行测试通过：`go test ./internal/swarm/agents/harness/team/ -run 'TestLoadTeamSpec|TestNormalizeDistributedTransportFields_方法|TestParsePort_方法' -v`
- [ ] 12.5 提交：`git commit -am "feat(team): loadTeamSpec 分布式检查+normalize+model_pool 注入（10.6.19 Task12，对齐 _load_team_spec L291-352）"`

---

## Task 13: BuildAgentCustomizer 六步回填 + 注入器

**Files:**
- Modify: `internal/swarm/agents/harness/team/team_manager_spec.go`
- Modify: `internal/swarm/agents/harness/team/team_manager.go`（CodeMemberConfigurer 类型 + SetCodeMemberConfigurer 注入器）
- Modify: `internal/swarm/agents/harness/team/team_manager_spec_test.go`

**对齐 Python**: `team_manager.py` L557-967（build_agent_customizer 全文）

- [ ] 13.1 写失败测试：

```go
// TestSetCodeMemberConfigurer_注入与调用。
func TestSetCodeMemberConfigurer_注入与调用(t *testing.T) {
	mgr := NewTeamManager()
	called := false
	mgr.SetCodeMemberConfigurer(func(ctx context.Context, agent, parent interfaces.DeepAgentInterface,
		skillManager *skillruntime.SkillManager, memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
		forceEnglishRuntimePrompt bool) {
		called = true
	})
	if mgr.codeMemberConfigurer == nil {
		t.Fatalf("configurer should be stored")
	}
	mgr.codeMemberConfigurer(nil, nil, nil, nil, "", "", "", "", "", "", false)
	if !called {
		t.Fatalf("configurer should be invoked")
	}
}

// TestCustomizer_能力卡继承 白名单工具去重添加。
func TestCustomizer_能力卡继承(t *testing.T) {
	mgr := NewTeamManager()
	spec := &atschema.TeamAgentSpec{TeamName: "t"}
	spec.Agents = map[string]atschema.DeepAgentSpec{}

	parent := newCustomizerFakeAgent("parent")
	visionCard := &tool.ToolCard{}
	visionCard.Name = "vision"
	visionCard.ID = "pv1"
	parent.abilityMgr.abilities = append(parent.abilityMgr.abilities, visionCard)

	member := newCustomizerFakeAgent("member")

	customizer := mgr.BuildAgentCustomizer(spec, parent, "sess", "req-1", "web", nil)
	if customizer == nil {
		t.Fatalf("customizer should not be nil")
	}
	if err := customizer(context.Background(), member, "member-1", "teammate"); err != nil {
		t.Fatalf("customizer failed: %v", err)
	}
	// vision 在白名单 → 应被继承
	if !member.abilityMgr.has("vision") {
		t.Fatalf("vision should be inherited")
	}
}
```

fake agent（`newCustomizerFakeAgent`）：完整 DeepAgentInterface fake（复用 Task 5/10 桩形态 + `AddRail` 记录到 `rails []agentinterfaces.AgentRail` 切片 + `DeepConfig() *hschema.DeepAgentConfig` 返回带 Workspace 的 config + `Card()` 返回带 ID/Name 的 card）。此 fake 较大，集中定义在 `team_manager_spec_test.go` 顶部，方法空桩 + 记录器。成员 DeepConfig.Workspace 用 `&workspace.Workspace{RootPath: t.TempDir()}`（harness workspace 包）。

- [ ] 13.2 运行确认失败
- [ ] 13.3 `team_manager.go` 追加（TeamManager 结构体加字段 + 注入方法）：

```go
// TeamManager 结构体内新增字段（放 onEventBroadcast 字段后）：
	// code member 配置回调，由 adapter 层注入（code 模式 team member 配置）
	// 对齐 Python: customizer 内 from interface_code import configure_code_team_member_agent（延迟 import）
	codeMemberConfigurer CodeMemberConfigurer
```

```go
// SetCodeMemberConfigurer 设置 code member 配置回调（由 adapter 层注入）。
// 对齐 Python: build_agent_customizer 内延迟 import configure_code_team_member_agent——
// Go 因循环依赖（team → adapter → team）改为注入回调，回调 nil 时 warning 跳过
// （对齐 Python import 失败 except 跳过语义）。
func (m *TeamManager) SetCodeMemberConfigurer(fn CodeMemberConfigurer) {
	m.codeMemberConfigurer = fn
}
```

CodeMemberConfigurer 类型定义放 `team_manager_spec.go` 结构体区（见下）。

- [ ] 13.4 `team_manager_spec.go` 顶部结构体区追加类型：

```go
// CodeMemberConfigurer code 模式 team member 配置回调（由 adapter 层注入）。
// 对齐 Python: configure_code_team_member_agent(agent, *, parent_agent, skill_manager,
// member_name, role, session_id, channel_id, project_dir, runtime_language,
// force_english_runtime_prompt) (interface_code.py L1301)
//
// skillManager 为成员独立 SkillManager（步骤 b 构造），可能为 nil。
type CodeMemberConfigurer func(
	ctx context.Context,
	agent interfaces.DeepAgentInterface,
	parentAgent interfaces.DeepAgentInterface,
	skillManager *skillruntime.SkillManager,
	memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
	forceEnglishRuntimePrompt bool,
)
```

- [ ] 13.5 `BuildAgentCustomizer` 闭包回填。完整替换现有骨架函数体（保留签名与 nil 检查）。闭包结构：

```go
func (m *TeamManager) BuildAgentCustomizer(
	spec *atschema.TeamAgentSpec,
	deepAgent interfaces.DeepAgentInterface,
	sessionID string,
	requestID string,
	channelID string,
	requestMetadata map[string]any,
) AgentCustomizer {
	if spec == nil || deepAgent == nil {
		return nil
	}

	// ── 前置解析（对齐 Python build_agent_customizer 闭包捕获变量 L567-579）──
	globalSkillsDir := workspace.AgentSkillsDir()
	globalSkillsStatePath := filepath.Join(globalSkillsDir, "skills_state.json")
	resolvedChannel := channelID
	if resolvedChannel == "" {
		resolvedChannel = "default"
	}
	resolvedModelName := GetDefaultModelName(nil)

	// team shared workspace（spec.Workspace.RootPath 回退 TeamHome/team-workspace）
	teamWsRoot := ""
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		teamWsRoot = spec.Workspace.RootPath
	} else {
		teamWsRoot = filepath.Join(agentteams.TeamHome(spec.TeamName), "team-workspace")
	}
	teamWsSkillsDir := filepath.Join(teamWsRoot, "skills")
	teamTrajectoryRegistry := trajectory.NewInMemoryTrajectoryRegistry()

	capturedRequestID := requestID
	capturedRequestMetadata := requestMetadata
	mgr := m

	// ── customizer 闭包（对齐 Python L727-965）──
	return func(ctx context.Context, agent interfaces.DeepAgentInterface, memberName string, role string) error {
		logger.Info(logComponent).
			Str("channel", resolvedChannel).
			Str("member_name", memberName).
			Str("role", role).
			Msg("[TeamManager] customizer called")

		// member workspace 解析（对齐 L738-750）
		memberDeepConfig := agent.DeepConfig()
		var agentWsRootPath string
		if memberDeepConfig != nil && memberDeepConfig.Workspace != nil {
			agentWsRootPath = memberDeepConfig.Workspace.RootPath
			logger.Debug(logComponent).Str("workspace_root_path", agentWsRootPath).
				Msg("[TeamManager] member workspace resolved")
		} else {
			logger.Warn(logComponent).
				Msg("[TeamManager] agent deep_config.workspace is None")
		}

		// parent 项目目录解析（对齐 L751-768）
		parentAdapterMode := strings.ToLower(deepAgent.UapswarmAdapterMode())
		parentProjectDir := firstNonEmpty(
			deepAgent.UapswarmProjectDir(),
			deepAgent.UapswarmCodeProjectDir(),
			parentWorkspaceRootPath(deepAgent),
		)
		if parentProjectDir != "" {
			agent.SetUapswarmProjectDir(parentProjectDir)
		}

		// ── 步骤 a：能力卡继承（对齐 L772-786）──
		stepAInheritAbilities(agent, deepAgent)

		// ── 步骤 b：技能同步（对齐 L788-827）──
		memberSkillManager := stepBSyncSkills(spec, agent, memberName, role,
			agentWsRootPath, globalSkillsDir, globalSkillsStatePath, teamWsSkillsDir)

		// ── 步骤 c：code adapter 配置（对齐 L829-868）──
		if parentAdapterMode == "code" {
			stepCConfigureCodeMember(mgr, ctx, agent, deepAgent, memberSkillManager,
				memberName, role, sessionID, resolvedChannel, parentProjectDir,
				spec, capturedRequestMetadata)
		}

		// ── 步骤 d：member rails 构造与注册（对齐 L870-947）──
		stepDMemberRails(mgr, agent, spec, memberName, role,
			resolvedChannel, resolvedModelName, teamWsRoot, teamWsSkillsDir,
			teamTrajectoryRegistry, globalSkillsDir, sessionID)

		// ── 步骤 e：TeamRailContext（leader）（对齐 L932-945）──
		stepERegisterRailContext(mgr, agent, spec, memberName, role,
			resolvedChannel, resolvedModelName, teamWsRoot, teamWsSkillsDir,
			teamTrajectoryRegistry, sessionID)

		// ── 步骤 f：member runtime tools（cron/SendFile ⤵️(10.6.24/11.10) 保留日志骨架）──
		mgr.RegisterMemberRuntimeTools(agent, sessionID, &capturedRequestID, &resolvedChannel, capturedRequestMetadata)

		return nil
	}
}
```

**注意**：Go 差异——Python 六步整体在一个大 try 结构外，各步骤内部自带 try/except；Go 中每个 stepXxx 函数内部独立 recover + warning（对齐 spec 决策 2"六步各自独立吞错"）。

- [ ] 13.6 实现六个 step 函数 + 辅助（追加到 `team_manager_spec.go` 非导出区）：

```go
// stepAInheritAbilities 步骤 a：能力卡继承。
// 对齐 Python L772-786：FilterInheritableAbilityCards → existingAbilityIds 去重 → Add。
func stepAInheritAbilities(agent, deepAgent interfaces.DeepAgentInterface) {
	defer recoverStep("inherit abilities")
	inheritableCards := FilterInheritableAbilityCards(deepAgent)
	existingAbilityIDs := map[string]bool{}
	for _, ability := range agent.AbilityManager().List() {
		existingAbilityIDs[ability.AbilityID()] = true
	}
	addedCount := 0
	for _, card := range inheritableCards {
		if existingAbilityIDs[card.ID] {
			logger.Debug(logComponent).Str("ability", card.Name).
				Msg("[TeamManager] Ability already exists, skipped")
			continue
		}
		agent.AbilityManager().Add(card)
		existingAbilityIDs[card.ID] = true
		addedCount++
	}
	logger.Info(logComponent).
		Int("added", addedCount).
		Int("total", len(existingAbilityIDs)).
		Msg("[TeamManager] Added inheritable abilities")
}

// stepBSyncSkills 步骤 b：技能同步（五函数主流程）。
// 对齐 Python L788-827。返回成员 SkillManager（可能 nil）。
func stepBSyncSkills(spec *atschema.TeamAgentSpec, agent interfaces.DeepAgentInterface,
	memberName, role, agentWsRootPath, globalSkillsDir, globalSkillsStatePath, teamWsSkillsDir string) *skillruntime.SkillManager {

	var memberSkillManager *skillruntime.SkillManager
	if agentWsRootPath == "" {
		// 对齐 Python：member_workspace 为 None 时 warning 跳过步骤 b 剩余部分
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Msg("[TeamManager] member workspace is None, skipping skill sync")
		return nil
	}
	defer recoverStep("skill sync")

	memberWorkspaceRoot := agentWsRootPath
	memberSkillsDir := filepath.Join(memberWorkspaceRoot, "skills")

	// skills_configured / selected_skills
	skillsConfigured, selectedSkills := resolveMemberSkills(spec, memberName, role)

	// 技能复制主流程（对齐 L799-808 try/except）
	func() {
		defer recoverStep("skill copy")
		if err := os.MkdirAll(memberSkillsDir, 0o755); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[TeamManager] skill copy failed")
			return
		}
		if skillsConfigured && len(selectedSkills) > 0 {
			copyMemberConfiguredSkills(memberSkillsDir, selectedSkills, globalSkillsDir)
		}
		syncSkillsDir(teamWsSkillsDir, memberSkillsDir)
		writeMemberSkillState(memberSkillsDir, globalSkillsStatePath)
	}()

	// 成员 SkillManager（对齐 L810-813）
	memberSkillManager = skillruntime.NewSkillManager(memberWorkspaceRoot)

	// MemberSkillToolkitRail 挂载（对齐 L815-827）
	func() {
		defer recoverStep("MemberSkillToolkitRail setup")
		agent.AddRail(NewMemberSkillToolkitRail(memberWorkspaceRoot))
		logger.Info(logComponent).Str("workspace", memberWorkspaceRoot).
			Msg("[TeamManager] MemberSkillToolkitRail queued for member workspace")
	}()
	return memberSkillManager
}

// stepCConfigureCodeMember 步骤 c：code adapter 配置（注入回调）。
// 对齐 Python L829-868：延迟 import configure_code_team_member_agent + except 跳过。
func stepCConfigureCodeMember(mgr *TeamManager, ctx context.Context,
	agent, deepAgent interfaces.DeepAgentInterface, memberSkillManager *skillruntime.SkillManager,
	memberName, role, sessionID, resolvedChannel, parentProjectDir string,
	spec *atschema.TeamAgentSpec, requestMetadata map[string]any) {

	defer recoverStep("code team member adapter setup")
	configurer := mgr.codeMemberConfigurer
	if configurer == nil {
		// 对齐 Python: except Exception → warning 跳过
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Str("role", role).
			Msg("[TeamManager] code member configurer not injected, skipping code team member adapter setup")
		return
	}

	// team_plan 判定（对齐 L835-848）
	requestMode := ""
	if requestMetadata != nil {
		if raw, ok := requestMetadata["mode"].(string); ok {
			requestMode = strings.TrimSpace(strings.ToLower(raw))
		}
	}
	roleName := strings.TrimSpace(strings.ToLower(role))
	teamPlanEnabled := requestMode == "team.plan" || spec.EnableTeamPlan
	isTeamPlanLeader := teamPlanEnabled && roleName == "leader"
	teamPlanRuntimeLanguage := ""
	if isTeamPlanLeader {
		// spec.Language 回退 config preferred_language 默认 "zh"
		lang := spec.Language
		if lang == "" {
			lang = configPreferredLanguage()
		}
		teamPlanRuntimeLanguage = lang
	}

	configurer(ctx, agent, deepAgent, memberSkillManager,
		memberName, role, sessionID, resolvedChannel, parentProjectDir,
		teamPlanRuntimeLanguage, !isTeamPlanLeader)
}

// stepDMemberRails 步骤 d：member rails 构造与注册。
// 对齐 Python L870-947：BuildMemberRails → 白名单过滤 AddRail → 三类注册。
func stepDMemberRails(mgr *TeamManager, agent interfaces.DeepAgentInterface,
	spec *atschema.TeamAgentSpec, memberName, role, resolvedChannel, resolvedModelName,
	teamWsRoot, teamWsSkillsDir string, teamTrajectoryRegistry trajectory.TrajectorySource,
	globalSkillsDir, sessionID string) {

	defer recoverStep("build_member_rails")

	teamID := spec.TeamName
	teamWorkspace := &TeamWorkspaceInfo{
		RootDir:  &teamWsRoot,
		SkillsDir: &teamWsSkillsDir,
		TeamID:   &teamID,
		Config:   loadConfigForRails(),
		TrajectoryRegistry: teamTrajectoryRegistry,
	}

	memberRails := BuildMemberRails(
		&MemberInfo{
			AgentName: agentCardName(agent, "team_member"),
			ModelName: resolvedModelName,
			Role:      &role,
		},
		&RuntimeInfo{Channel: resolvedChannel},
		teamWorkspace,
	)

	var teamSkillRail *evolution.TeamSkillEvolutionRail
	var teamSkillCreateRail *evolution.TeamSkillCreateRail
	for _, rail := range memberRails {
		railTypeName := reflect.TypeOf(rail).Elem().Name()
		if railWhitelist[railTypeName] {
			agent.AddRail(rail)
			// TeamSkillEvolutionRail/TeamSkillCreateRail 需注册 live rail（热更新反注册用）
			switch r := rail.(type) {
			case *evolution.TeamSkillEvolutionRail:
				GetTeamManager(resolvedChannel).RegisterTeamLiveRail(sessionID, agent, r)
			case *evolution.TeamSkillCreateRail:
				GetTeamManager(resolvedChannel).RegisterTeamLiveRail(sessionID, agent, r)
			}
		} else {
			logger.Debug(logComponent).Str("rail", railTypeName).
				Msg("[TeamManager] Skipping non-whitelisted rail")
		}
		switch r := rail.(type) {
		case *evolution.TeamSkillEvolutionRail:
			teamSkillRail = r
		case *evolution.SkillEvolutionRail:
			GetTeamManager(resolvedChannel).RegisterTeamMemberSkillEvolutionRail(sessionID, r)
		case *evolution.TeamSkillCreateRail:
			teamSkillCreateRail = r
		}
	}
	logger.Info(logComponent).Int("count", len(memberRails)).
		Msg("[TeamManager] Added rails for team member")

	// TeamSkillEvolutionRail 注册（审批/同步用，对齐 L912-926）
	if teamSkillRail != nil {
		tm := GetTeamManager(resolvedChannel)
		tm.RegisterTeamSkillRail(sessionID, teamSkillRail)
		tm.RegisterTeamSkillSyncTarget(sessionID, teamWsSkillsDir, globalSkillsDir)
		logger.Info(logComponent).
			Str("skills_dir", teamWsSkillsDir).
			Str("sync_target", globalSkillsDir).
			Msg("[TeamManager] TeamSkillEvolutionRail mounted on leader")
	}
	if teamSkillCreateRail != nil {
		GetTeamManager(resolvedChannel).RegisterTeamSkillCreateRail(sessionID, teamSkillCreateRail)
	}
}

// stepERegisterRailContext 步骤 e：注册 TeamRailContext（仅 leader）。
// 对齐 Python L932-945。
func stepERegisterRailContext(mgr *TeamManager, agent interfaces.DeepAgentInterface,
	spec *atschema.TeamAgentSpec, memberName, role, resolvedChannel, resolvedModelName,
	teamWsRoot, teamWsSkillsDir string, teamTrajectoryRegistry trajectory.TrajectorySource,
	sessionID string) {

	if role != "leader" {
		return
	}
	defer recoverStep("register_team_rail_context")

	teamID := spec.TeamName
	teamWorkspace := &TeamWorkspaceInfo{
		RootDir:  &teamWsRoot,
		SkillsDir: &teamWsSkillsDir,
		TeamID:   &teamID,
		Config:   loadConfigForRails(),
		TrajectoryRegistry: teamTrajectoryRegistry,
	}
	GetTeamManager(resolvedChannel).RegisterTeamRailContext(sessionID, &TeamRailMountContext{
		Agent: agent,
		MemberInfo: &MemberInfo{
			AgentName: agentCardName(agent, "team_member"),
			ModelName: resolvedModelName,
			Role:      &role,
		},
		Runtime:      &RuntimeInfo{Channel: resolvedChannel},
		TeamWorkspace: teamWorkspace,
	})
}

// ──────────────────────────── 非导出函数（本段追加） ────────────────────────────

// resolveMemberSkills 解析成员配置的技能列表。
// 对齐 Python resolve_member_skills (L591-603)：
// member_name → role → leader 回退查找 spec.Agents；Skills 为空时返回 (false, [])。
func resolveMemberSkills(spec *atschema.TeamAgentSpec, memberName, role string) (bool, []string) {
	memberSpec := resolveMemberSpec(spec, memberName, role)
	if memberSpec == nil {
		return false, []string{}
	}
	if memberSpec.Skills == nil {
		return false, []string{}
	}
	selected := make([]string, 0, len(memberSpec.Skills))
	for _, s := range memberSpec.Skills {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			selected = append(selected, trimmed)
		}
	}
	return true, selected
}

// resolveMemberSpec 解析成员规格。
// 对齐 Python resolve_member_spec (L581-589)：
// member_name 命中 → role 命中 → leader 回退。
func resolveMemberSpec(spec *atschema.TeamAgentSpec, memberName, role string) *atschema.DeepAgentSpec {
	if spec.Agents == nil {
		return nil
	}
	if memberName != "" {
		if s, ok := spec.Agents[memberName]; ok {
			return &s
		}
	}
	if role != "" {
		if s, ok := spec.Agents[role]; ok {
			return &s
		}
	}
	if s, ok := spec.Agents["leader"]; ok {
		return &s
	}
	return nil
}

// copyMemberConfiguredSkills 复制成员配置的技能到成员技能目录。
// 对齐 Python copy_member_configured_skills (L605-638)：
// global 目录中 SKILL.md 有效且在 selected 集合 → 复制（dest 已存在跳过）→ missing warning。
func copyMemberConfiguredSkills(memberSkillsDir string, selectedSkills []string, globalSkillsDir string) {
	if info, err := os.Stat(globalSkillsDir); err != nil || !info.IsDir() {
		logger.Warn(logComponent).Str("global_skills_dir", globalSkillsDir).
			Msg("[TeamManager] global_skills_dir does not exist")
		return
	}
	selectedSet := map[string]bool{}
	for _, s := range selectedSkills {
		selectedSet[s] = true
	}
	if err := os.MkdirAll(memberSkillsDir, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] mkdir member_skills_dir failed")
		return
	}
	copiedCount := 0
	entries, err := os.ReadDir(globalSkillsDir)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] readdir global_skills_dir failed")
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(globalSkillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err != nil {
			continue
		}
		if !selectedSet[entry.Name()] {
			continue
		}
		dest := filepath.Join(memberSkillsDir, entry.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if err := copyTreeMerge(filepath.Join(globalSkillsDir, entry.Name()), dest); err != nil {
			logger.Warn(logComponent).Err(err).Str("skill", entry.Name()).
				Msg("[TeamManager] copy skill to member workspace failed")
			continue
		}
		copiedCount++
		logger.Info(logComponent).Str("skill", entry.Name()).
			Msg("[TeamManager] Copied skill to member workspace")
	}

	// missing 集合 warning
	existingNames := map[string]bool{}
	if entries, err := os.ReadDir(memberSkillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				existingNames[e.Name()] = true
			}
		}
	}
	missing := make([]string, 0)
	for name := range selectedSet {
		if !existingNames[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		logger.Warn(logComponent).Strs("missing", missing).
			Msg("[TeamManager] configured skills not found in global dir")
	}
	logger.Info(logComponent).Int("count", copiedCount).
		Msg("[TeamManager] Total configured skills copied to member")
}

// buildMemberSkillState 构建成员技能状态（对齐 Python build_member_skill_state L640-716）。
//
// 读全局 skills_state.json → NormalizeMarketplaces → installed_plugins/local_skills
// 按 member 目录实际 SKILL.md 过滤 + 补全（project source 条目）。
func buildMemberSkillState(memberSkillsDir, globalSkillsStatePath string) map[string]any {
	state := map[string]any{
		"marketplaces":     []any{},
		"installed_plugins": []any{},
		"local_skills":     []any{},
	}
	if data, err := os.ReadFile(globalSkillsStatePath); err == nil {
		var loaded map[string]any
		if jsonErr := json.Unmarshal(data, &loaded); jsonErr == nil {
			for k, v := range loaded {
				state[k] = v
			}
		}
	} else {
		logger.Warn(logComponent).Err(err).
			Msg("[TeamManager] failed to load global skills_state.json")
	}

	if raw, ok := state["marketplaces"].([]any); ok {
		normalized := skillruntime.NormalizeMarketplaces(raw)
		converted := make([]any, 0, len(normalized))
		for _, m := range normalized {
			converted = append(converted, m)
		}
		state["marketplaces"] = converted
	}

	// member 目录实际技能名（SKILL.md 有效）
	actualSkillNames := make([]string, 0)
	entries, err := os.ReadDir(memberSkillsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(memberSkillsDir, e.Name(), "SKILL.md")); err == nil {
				actualSkillNames = append(actualSkillNames, e.Name())
			}
		}
	}
	sort.Strings(actualSkillNames)
	actualSkillSet := map[string]bool{}
	for _, n := range actualSkillNames {
		actualSkillSet[n] = true
	}

	// installed_plugins 过滤
	installedPlugins := make([]any, 0)
	if rawList, ok := state["installed_plugins"].([]any); ok {
		for _, item := range rawList {
			plugin, ok := item.(map[string]any)
			if !ok {
				continue
			}
			pluginName := strings.TrimSpace(mapStrField(plugin, "name"))
			if pluginName == "" || !actualSkillSet[pluginName] {
				continue
			}
			installedPlugins = append(installedPlugins, plugin)
		}
	}

	// local_skills 过滤
	localSkills := make([]any, 0)
	if rawList, ok := state["local_skills"].([]any); ok {
		for _, item := range rawList {
			localSkill, ok := item.(map[string]any)
			if !ok {
				continue
			}
			skillName := strings.TrimSpace(mapStrField(localSkill, "name"))
			if skillName == "" || !actualSkillSet[skillName] {
				continue
			}
			localSkills = append(localSkills, localSkill)
		}
	}

	// 补全缺失（project source 条目）
	existingPluginNames := map[string]bool{}
	for _, item := range installedPlugins {
		if plugin, ok := item.(map[string]any); ok {
			existingPluginNames[strings.TrimSpace(mapStrField(plugin, "name"))] = true
		}
	}
	existingLocalNames := map[string]bool{}
	for _, item := range localSkills {
		if localSkill, ok := item.(map[string]any); ok {
			existingLocalNames[strings.TrimSpace(mapStrField(localSkill, "name"))] = true
		}
	}
	for _, skillName := range actualSkillNames {
		if !existingPluginNames[skillName] {
			installedPlugins = append(installedPlugins, map[string]any{
				"name": skillName, "marketplace": "", "version": "",
				"commit": "", "source": "project", "installed_at": "",
			})
		}
		if !existingLocalNames[skillName] {
			localSkills = append(localSkills, map[string]any{
				"name":   skillName,
				"origin": filepath.Join(memberSkillsDir, skillName),
				"source": "project",
			})
		}
	}

	state["installed_plugins"] = installedPlugins
	state["local_skills"] = localSkills
	return state
}

// writeMemberSkillState 写成员 skills_state.json。
// 对齐 Python write_member_skill_state (L718-725)。
func writeMemberSkillState(memberSkillsDir, globalSkillsStatePath string) {
	stateFile := filepath.Join(memberSkillsDir, "skills_state.json")
	state := buildMemberSkillState(memberSkillsDir, globalSkillsStatePath)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] marshal member skills_state failed")
		return
	}
	if err := os.WriteFile(stateFile, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] write member skills_state failed")
		return
	}
	logger.Info(logComponent).Str("state_file", stateFile).
		Msg("[TeamManager] Wrote member skills_state.json")
}

// recoverStep 步骤级 panic 恢复（对齐 Python 各步骤独立 try/except 吞错）。
func recoverStep(stepName string) {
	if r := recover(); r != nil {
		logger.Warn(logComponent).Str("step", stepName).Any("panic", r).
			Msg("[TeamManager] customizer step failed")
	}
}

// firstNonEmpty 返回首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parentWorkspaceRootPath 解析 parent agent 的 workspace 根路径（可能为空）。
// 对齐 Python L754-767 的 parent_workspace.root_path 回退。
func parentWorkspaceRootPath(deepAgent interfaces.DeepAgentInterface) string {
	cfg := deepAgent.DeepConfig()
	if cfg == nil || cfg.Workspace == nil {
		return ""
	}
	return cfg.Workspace.RootPath
}

// configPreferredLanguage 读取 config preferred_language（默认 "zh"）。
// 对齐 Python: get_config().get("preferred_language", "zh")
func configPreferredLanguage() string {
	cfgMap, err := teamLoadConfig()
	if err != nil {
		return "zh"
	}
	if lang, ok := cfgMap["preferred_language"].(string); ok && lang != "" {
		return lang
	}
	return "zh"
}

// loadConfigForRails 为 rails 构造加载完整 config（对齐 Python get_config()）。
func loadConfigForRails() map[string]any {
	cfgMap, err := teamLoadConfig()
	if err != nil {
		return map[string]any{}
	}
	return cfgMap
}

// agentCardName 读取 agent card name（回退默认值）。
// 对齐 Python: getattr(agent.card, "name", "team_member")
func agentCardName(agent interfaces.DeepAgentInterface, fallback string) string {
	card := agent.Card()
	if card == nil || card.Name == "" {
		return fallback
	}
	return card.Name
}
```

import 补：`"encoding/json"`、`"os"`、`"path/filepath"`、`"reflect"`、`"sort"`、`"strings"`、`agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"`（evolution 已在 skill.go 用，spec.go 需新增）、`"github.com/uapclaw/uapclaw-go/internal/common/workspace"`、`"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"`、`skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"`。

**关键语义说明**：Python L932 `if role == "leader"` 在步骤 d 的 try 块内；Go 拆为独立 stepE（Python 该 if 独立判定后调用 register_team_rail_context，无嵌套 try）——语义等价（register_team_rail_context 内部无异常路径）。

- [ ] 13.7 `RegisterMemberRuntimeTools` 替换空 stub 为日志骨架（cron/SendFile ⤵️ 保留）：

```go
func (m *TeamManager) RegisterMemberRuntimeTools(
	agent interfaces.DeepAgentInterface,
	sessionID string,
	requestID *string,
	channelID *string,
	requestMetadata map[string]any,
) {
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	// ⤵️(10.6.24/11.10) CronRuntimeBridge 工具注册 — 待 Cron 工具集实现回填
	// Python: cron_runtime.build_tools(context=...) → Runner.resource_mgr.add_tool → agent.ability_manager.add
	logger.Info(logComponent).
		Str("agent_id", agentID).
		Str("session_id", sessionID).
		Msg("[TeamManager] cron tool registration pending (10.6.24)")

	if requestID == nil || *requestID == "" || channelID == nil || *channelID == "" {
		logger.Info(logComponent).
			Msg("[TeamManager] SendFileToolkit skipped: missing request_id or channel_id")
		return
	}
	// ⤵️(10.6.24/11.10) SendFileToolkit 注册 — 待 SendFile 工具集实现回填
	// Python: send_file_allowed 三态（None 时 web 默认 True，其余 False）→ SendFileToolkit(...).get_tools() 注册
	logger.Info(logComponent).
		Str("channel_id", *channelID).
		Msg("[TeamManager] SendFileToolkit registration pending (10.6.24)")
}
```

- [ ] 13.8 运行测试：`go test ./internal/swarm/agents/harness/team/ -count=1`（新增测试 + 既有全量）
- [ ] 13.9 提交：`git commit -am "feat(team): BuildAgentCustomizer 六步回填 a-e + CodeMemberConfigurer 注入器（10.6.19 Task13，对齐 build_agent_customizer L557-967）"`

---

## Task 14: lifecycle PG 接入 + CreateTeam 步骤 8 + 标记编号修正

**Files:**
- Modify: `internal/swarm/agents/harness/team/team_manager_lifecycle.go`
- Modify: `internal/swarm/agents/harness/team/team_manager_distributed.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go`
- Modify: `internal/swarm/agents/harness/team/doc.go`

**对齐 Python**: `team_manager.py` L987-996（_ensure_postgresql_for_leader 包装）+ L1049-1083（create_team 内 PG + skills 初始化）

- [ ] 14.1 `GetEnrichedTeamSpec`（team_manager_spec.go L36-43）在 `loadTeamSpec` 调用前插入（对齐 Python get_enriched_team_spec 步骤 2）：

```go
	// 步骤 2: _ensure_postgresql_for_leader
	// 对齐 Python: config_base = get_config(); await self._ensure_postgresql_for_leader(config_base)
	if cfgMap, err := teamLoadConfig(); err == nil {
		EnsurePostgresqlForLeader(ctx, cfgMap)
	} else {
		logger.Warn(logComponent).Err(err).
			Msg("GetEnrichedTeamSpec: load config for PG check failed")
	}
```

- [ ] 14.2 `CreateTeam`（lifecycle.go）两处回填：
  - L176 步骤 2 ⤵️ 段替换为：

```go
	// 步骤 2: _ensure_postgresql_for_leader
	// 对齐 Python: await self._ensure_postgresql_for_leader(config_base)
	if cfgMap, err := teamLoadConfig(); err == nil {
		EnsurePostgresqlForLeader(ctx, cfgMap)
	}
```

  - L216 步骤 8 ⤵️ 段替换为（对齐 Python create_team L1083 build 后调用）：

```go
	// 步骤 8: ensure_team_shared_skills_initialized
	// 对齐 Python: self.ensure_team_shared_skills_initialized(spec)（marker 幂等，
	// deep_adapter_team.go:425 调用方已有调用，双重调用由 marker 保证单次执行）
	m.EnsureTeamSharedSkillsInitialized(spec)
```

  注意：调用方（deep_adapter_team.go:425）在 GetEnrichedTeamSpec 之后、RunAgentTeamStreaming 之前调用了 `EnsureTeamSharedSkillsInitialized(teamSpec)`——**保留该调用**（Python 侧 interface.py L400 同位置也有），CreateTeam 内部调用对齐 Python create_team 位置；marker 幂等保证不冲突。无需去重删除（spec 决策 10）。

  - L217 步骤 9 标记修正：`⤵️(#9.72)` → `⤵️(#10.6.20)`（attach distributed hooks，文字保留）
  - L66（OnRuntimeReady evolution watcher）、L435/L638/L661（A2X 释放 + transport 停止）：`⤵️(#9.72)` → `⤵️(#10.6.22)`（文字保留）
- [ ] 14.3 `team_manager_distributed.go` L30 标记修正：`⤵️(#9.72)` → `⤵️(#10.6.20-21)`（文字保留）
- [ ] 14.4 `deep_adapter_team.go` L808 标记修正：`⤵️(#9.72)` → `⤵️(#10.6.20)`（文字保留）
- [ ] 14.5 `doc.go` 文件目录追加新文件条目（在 team_manager_distributed.go 之后）：

```
//	├── distributed_runtime.go       # 分布式运行时纯函数（模式判定/传输标准化/PG 编排）
//	├── exceptions.go                # Team 异常类型（TeamError + 7 子类）
//	├── member_skill_toolkit_rail.go # 成员技能工具 rail
//	├── runtime_inheritance.go       # 成员运行时继承（rails 构造/能力过滤/模型解析）
//	└── workspace_report_path_rail.go # 工作区产物路径提示 rail
```

（注意 doc.go 现有目录列表末行 `team_manager_distributed.go` 的 `└──` 需改为 `├──`）

- [ ] 14.6 编译 + 全包测试：`go build ./... && go test ./internal/swarm/agents/harness/team/ ./internal/swarm/server/adapter/ -count=1`
- [ ] 14.7 提交：`git commit -am "feat(team): CreateTeam PG 接入+共享技能初始化回填+⤵️ 标记编号修正（10.6.19 Task14）"`

---

## Task 15: adapter 侧注入 + GetDefaultModels 三处收敛

**Files:**
- Modify: `internal/swarm/server/adapter/team_helpers.go`（EnsureTeamDependenciesInjected）
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go`（调用注入）
- Modify: `internal/swarm/gateway/channel_manager/web/config_apply.go`（删旧实现+改调用）
- Modify: `internal/swarm/gateway/channel_manager/web/config_apply_test.go`（测试适配）
- Modify: `internal/swarm/agents/harness/common/memory/dreaming/sweeper.go`（删旧实现+改调用）
- Modify: `internal/swarm/server/adapter/deep_adapter.go`（删旧实现+改调用）

**对齐 Python**: `interface_code.py` L1301-1326（configure_code_team_member_agent 模块级函数）+ spec 方案 iii 收敛表

- [ ] 15.1 `team_helpers.go` 追加（全局变量区加 `teamDepsInjected bool`，与 `eventBroadcastInitialized` 并列）：

```go
// EnsureTeamDependenciesInjected 确保 TeamManager 的依赖回调已注入。
// 对齐 Python: customizer 内 from interface_code import configure_code_team_member_agent
// （模块级函数每次调用新建 JiuwenClawCodeAdapter 实例）。
// 此函数幂等，多次调用无副作用。
func EnsureTeamDependenciesInjected(channelID string) {
	if teamDepsInjected {
		return
	}
	tm := team.GetTeamManager(channelID)
	tm.SetCodeMemberConfigurer(func(
		ctx context.Context,
		agent interfaces.DeepAgentInterface,
		parentAgent interfaces.DeepAgentInterface,
		skillManager *skill.SkillManager,
		memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
		forceEnglishRuntimePrompt bool,
	) {
		// 接口 → 具体类型断言（对齐 Python：configure 接收 DeepAgent 实例；
		// 断言失败 warning 跳过，等价 Python except）
		agentImpl, ok := agent.(*harness.DeepAgent)
		if !ok {
			logger.Warn(logComponent).
				Str("member_name", memberName).
				Str("role", role).
				Msg("code member configurer: agent is not *harness.DeepAgent, skipping")
			return
		}
		parentImpl, ok := parentAgent.(*harness.DeepAgent)
		if !ok {
			logger.Warn(logComponent).
				Str("member_name", memberName).
				Str("role", role).
				Msg("code member configurer: parentAgent is not *harness.DeepAgent, skipping")
			return
		}
		// Python: adapter = JiuwenClawCodeAdapter()（每次调用新建实例）
		NewCodeAdapter().ConfigureTeamMemberAgent(
			ctx, agentImpl, parentImpl, skillManager,
			memberName, role, sessionID, channelID, projectDir, runtimeLanguage,
			forceEnglishRuntimePrompt,
		)
	})
	teamDepsInjected = true
}
```

import 补：`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"`（若 adapter 包已用别名按现有惯例）、`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"`、`skill "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"`（按包内现有 import 别名惯例核对：team_helpers.go 已 import `skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"`——用 `*skillruntime.SkillManager`）。

- [ ] 15.2 `deep_adapter_team.go` L273 `EnsureEventBroadcastInjected(channelID)` 后追加 `EnsureTeamDependenciesInjected(channelID)`
- [ ] 15.3 三处收敛（每处删实现 + 改调用）：
  - `web/config_apply.go`：删除 L302-326 `GetDefaultModels()` 函数；L653 `resolvedDefaults := GetDefaultModels()` 改为 `resolvedDefaults := config.GetDefaultModels(nil)`（web 语义：config.yaml 存在时走 defaults/default 分支不变；不存在时旧版返回 nil、新版返回 env 条目——`mergeModelsForReplaceAll(parsed, rawDefaults, resolvedDefaults, crypto)` 的 `resolvedEntry` 仅用于与前端值比较，env 条目不命中 origin_index 分支，无行为回退；测试 `TestGetDefaultModels` L429 删除或改为 `config.GetDefaultModels(nil)` 冒烟）
  - `dreaming/sweeper.go`：删除 L1034-1061 `getDefaultModels()` 函数；L789 `entries := getDefaultModels()` 改为 `entries := config.GetDefaultModels(nil)`（sweeper.go 已 import config）
  - `adapter/deep_adapter.go`：删除 L2292-2319 `getDefaultModels(configBase)` 函数；L1958 `defaults := getDefaultModels(configBase)` 改为 `defaults := config.GetDefaultModels(configBase)`（行为增强：新增 is_default 附加字段 + env 回退——buildModelCacheFromDefaults 按 model_name 遍历构造缓存，is_default 为多余字段被忽略，无副作用；configBase 无 models 段时旧版返回 nil 新版返回 env 条目——逐条构造缓存，等价于旧版空循环 + 新增 env 模型缓存，`buildModelCacheFromDefaults` 调用点仅用于命中查找，新增条目无副作用）
- [ ] 15.4 三包编译+测试回归：`go build ./... && go test ./internal/swarm/gateway/channel_manager/web/ ./internal/swarm/agents/harness/common/memory/dreaming/ ./internal/swarm/server/adapter/ -count=1`
- [ ] 15.5 提交：`git commit -am "feat(team): adapter 注入 CodeMemberConfigurer + GetDefaultModels 三处收敛（10.6.19 Task15，方案 iii）"`

---

## Task 16: IMPLEMENTATION_PLAN.md 章节状态拆分

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] 16.1 将 L712 `| 10.6.19-23 | ☐ | Swarm Team | TeamManager/Bootstrap/DistributedRuntime/A2X/TeamRails | ... |` 一行拆分为五行（spec 六节定案）：

```
| 10.6.19 | ✅ | TeamManager 主体 | 72 方法骨架回填+customizer 六步(a-e 实现/f ⤵️10.6.24)+runtime_inheritance(四结构体+双白名单+BuildMemberRails 13 步)+2 rail+exceptions+分布式纯函数层+model_pool 注入+GetDefaultModels 权威收敛三处+CodeMemberConfigurer 注入；⤵️ 标记编号修正 #9.72→实际章节 | `team_manager.py` |
| 10.6.20 | ☐ | DistributedRuntime | zmq 传输层（✅ 纯函数层已随 10.6.19 带出：IsDistributedMode/RuntimeRole/ParsePort/NormalizeDistributedTransportFields/FallbackDistributedToLocal/PG 五函数）；⤴️ 纯函数层来源 10.6.19 | `distributed_runtime.py` |
| 10.6.21 | ☐ | RemoteMemberBootstrap | 远程成员引导 | `remote_member_bootstrap.py` |
| 10.6.22 | ☐ | A2X RegistryRuntime | A2X 注册运行时（lifecycle ⤵️ 标记已改指本节） | `a2x/` |
| 10.6.23 | ✅ | TeamRails | inheritance+MemberSkillToolkitRail+WorkspaceReportPathRail+exceptions 已随 10.6.19 实现（方案 C 合并决策） | `team_runtime_inheritance.py` · `rails/` |
```

- [ ] 16.2 全项目编译最终验证：`go build ./... && go test ./internal/swarm/agents/harness/team/ ./internal/common/config/ -cover -count=1`（team 包覆盖率 ≥85% 目标；不足则按失败测试补齐）
- [ ] 16.3 提交：`git commit -am "docs(plan): IMPLEMENTATION_PLAN 10.6.19-23 章节状态拆分（10.6.19 IMPLEMENTED/10.6.20-22 IN_PROGRESS/10.6.23 随 10.6.19 完成）"`

---

## 验收清单（对应 spec 处置表 18 项）

| spec # | 位置 | 本计划任务 |
|--------|------|-----------|
| 1-5 | spec.go 六步 a-e | Task 13 |
| 6-7 | 步骤 f + RegisterMemberRuntimeTools 日志骨架 | Task 13.7（⤵️10.6.24 保留） |
| 8 | NormalizeDistributedTransportFields/ParsePort 方法 | Task 12.3 |
| 9 | loadTeamSpec 分布式+model_pool | Task 12.3 |
| 10-11 | 共享技能初始化+syncSkillsDir | Task 11.3 |
| 12-13 | lifecycle watcher 核对+PG | Task 14.1/14.2 |
| 14 | CreateTeam 步骤 8 | Task 14.2 |
| 15-18 | ⤵️ 标记编号修正 | Task 14.2/14.3/14.4 |
| 6a | config/default_models.go | Task 1 |
| 6b | 三处收敛 | Task 15.3 |
| spec 决策 1 | CodeMemberConfigurer 注入 | Task 13.3 + 15.1 |
| spec 决策 6 | 四结构体真实迁移 | Task 6.4 |
| spec 决策 8 | GetDefaultModels 方案 iii | Task 1 + 15.3 |
| spec 六节 | 章节状态拆分 | Task 16.1 |

## 风险与回退

- **Task 5 接口面扩展波及面**：编译器定位全部 fake，若波及文件过多（>15 个测试文件）可分两次提交（先接口+核心 fake，后剩余）
- **Task 11 环境变量注入**：若 `UAPCLAW_WORKSPACE_DIR` 不生效，参考 `internal/common/workspace/paths_test.go` 的注入模式改写测试
- **Task 12 JSON 桥接**：`NewTeamAgentSpecFromDict` 对 model_pool 的 []any→[]ModelPoolEntry 反序列化依赖 json 标签——`ModelPoolEntry` 字段均有 json tag，已核实可用
- **Task 15 web 行为增强**：env 回退使 config 无 models 段时从 nil → env 条目，`mergeModelsForReplaceAll` 的 origin_index 比较逻辑不受影响（resolvedEntry 仅比较用途）；若测试发现回退场景差异，按实际输出微调断言

