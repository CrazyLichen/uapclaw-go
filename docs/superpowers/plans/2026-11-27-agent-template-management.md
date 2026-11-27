# agent_templates/agent_groups 管理面移植实施计划（15.18a 回收批）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 移植 Python `extension_package_manager.py` 本地半区 + `_handle_package_catalog_request` 分派，让智能体设计页（模板/分组列表、详情、文件树、本地导入/创建/更新/删除/卸载）在 Go 上可用，并回收 15.18a/15.16e 全部挂账。

**Architecture:** 在既有 `internal/swarm/server/runtime/equipment/` 包内按 Python 半区拆 6 个新文件（一文件对一文件），卡片投影复用 15.14 `template_card.go`、组包加载复用 15.16e `experts/agent_group.go`、路径助手复用 15.18a 既有切片；`server/handle_equipment_catalog.go` 做统一分派；Hub 半区不移植（偏差 D-1/D-2，install 返回 `MARKET_UNAVAILABLE`）。

**Tech Stack:** Go 1.22 + encoding/json + testify；集成测试走 `testhelpers.ServerEnv`（E2A 信封投递）。

**Spec:** `docs/superpowers/specs/2026-11-27-agent-template-management-design.md`（裁决记录/偏差表/回填闭合清单，实施时逐节核对）

**Python 真相源约定：** 每个任务标注 Python 文件与行号区间；移植时**逐行对照**，签名按规则 9（snake_case → CamelCase，归属不变）。发现 Python 有而本计划漏写的步骤 → 按规则 13.2 提出讨论，不许默默跳过。

**测试根目录铁律：** 任何触发目录扫描的测试禁止传真实 `/tmp`，一律临时目录 + `pathutil.ResetCache()`（对齐 SessionPinSuite 口径）。

---

### Task 0: 工作树与基座

**Files:**
- Create: worktree 分支 `feat/15.18a-equipment-local-mgmt`（基于 main）

- [ ] **Step 1: 建工作树**

```bash
cd /home/opensource/uap-claw-go
git worktree add /tmp/wt-15.18a-mgmt -b feat/15.18a-equipment-local-mgmt main
cd /tmp/wt-15.18a-mgmt
export GOPROXY=https://goproxy.cn,direct
```

- [ ] **Step 2: 基线确认**——`go build ./... && go vet ./internal/swarm/server/runtime/equipment/` 通过；读 `equipment_slice.go` 全文（既有符号：`ResolveEquipmentRuntimeID`/`ResolveAgentTemplateDir`/`agentTemplateKind`/`agentGroupKind`/`kindRoot`），后续任务直接复用不改名。

### Task 1: ReqMethod 常量 + AllReqMethods（第四层注册点先钉死）

**Files:**
- Modify: `internal/swarm/schema/req_method.go`
- Test: `internal/swarm/schema/req_method_test.go`（既有 TestAllReqMethods 计数断言）

- [ ] **Step 1: 加常量**（Python `/tmp/agent-protocol/openjiuwen-protocol/openjiuwen_protocol/message.py` ReqMethod 枚举段；放在项目控制面常量块后）：

```go
// ─── 扩展设备管理（extension_package_manager.py）───

// ReqMethodAgentTemplatesList 模板列表（Python AGENT_TEMPLATES_LIST）
ReqMethodAgentTemplatesList ReqMethod = "agent_templates.list"
// ReqMethodAgentTemplatesShow 模板详情（Python AGENT_TEMPLATES_SHOW）
ReqMethodAgentTemplatesShow ReqMethod = "agent_templates.show"
// ReqMethodAgentTemplatesFileList 模板文件树（Python AGENT_TEMPLATES_FILE_LIST）
ReqMethodAgentTemplatesFileList ReqMethod = "agent_templates.file.list"
// ReqMethodAgentTemplatesFileRead 读模板包内文件（Python AGENT_TEMPLATES_FILE_READ）
ReqMethodAgentTemplatesFileRead ReqMethod = "agent_templates.file.read"
// ReqMethodAgentTemplatesCreate 创建模板包（Python AGENT_TEMPLATES_CREATE）
ReqMethodAgentTemplatesCreate ReqMethod = "agent_templates.create"
// ReqMethodAgentTemplatesImportLocal 本地导入模板（Python AGENT_TEMPLATES_IMPORT_LOCAL）
ReqMethodAgentTemplatesImportLocal ReqMethod = "agent_templates.import_local"
// ReqMethodAgentTemplatesInstall Hub 安装（Python AGENT_TEMPLATES_INSTALL；本批 D-1 拒绝）
ReqMethodAgentTemplatesInstall ReqMethod = "agent_templates.install"
// ReqMethodAgentTemplatesUninstall 卸载模板（Python AGENT_TEMPLATES_UNINSTALL）
ReqMethodAgentTemplatesUninstall ReqMethod = "agent_templates.uninstall"
// ReqMethodAgentTemplatesUpdate 更新模板（Python AGENT_TEMPLATES_UPDATE）
ReqMethodAgentTemplatesUpdate ReqMethod = "agent_templates.update"
// ReqMethodAgentTemplatesDelete 删除模板（Python AGENT_TEMPLATES_DELETE）
ReqMethodAgentTemplatesDelete ReqMethod = "agent_templates.delete"
// ReqMethodAgentGroupsList 分组列表（Python AGENT_GROUPS_LIST）
ReqMethodAgentGroupsList ReqMethod = "agent_groups.list"
// ReqMethodAgentGroupsShow 分组详情（Python AGENT_GROUPS_SHOW）
ReqMethodAgentGroupsShow ReqMethod = "agent_groups.show"
// ReqMethodAgentGroupsFileList 分组文件树（Python AGENT_GROUPS_FILE_LIST）
ReqMethodAgentGroupsFileList ReqMethod = "agent_groups.file.list"
// ReqMethodAgentGroupsFileRead 读分组包内文件（Python AGENT_GROUPS_FILE_READ）
ReqMethodAgentGroupsFileRead ReqMethod = "agent_groups.file.read"
// ReqMethodAgentGroupsCreate 创建分组（Python AGENT_GROUPS_CREATE）
ReqMethodAgentGroupsCreate ReqMethod = "agent_groups.create"
// ReqMethodAgentGroupsImportLocal 本地导入分组（Python AGENT_GROUPS_IMPORT_LOCAL）
ReqMethodAgentGroupsImportLocal ReqMethod = "agent_groups.import_local"
// ReqMethodAgentGroupsInstall Hub 安装分组（Python AGENT_GROUPS_INSTALL；本批 D-1 拒绝）
ReqMethodAgentGroupsInstall ReqMethod = "agent_groups.install"
// ReqMethodAgentGroupsUninstall 卸载分组（Python AGENT_GROUPS_UNINSTALL）
ReqMethodAgentGroupsUninstall ReqMethod = "agent_groups.uninstall"
// ReqMethodPluginPackagesInstall Hub 安装插件（Python PLUGIN_PACKAGES_INSTALL；D-1 拒绝）
ReqMethodPluginPackagesInstall ReqMethod = "plugin_packages.install"
// ReqMethodPluginPackagesUninstall 卸载插件（Python PLUGIN_PACKAGES_UNINSTALL）
ReqMethodPluginPackagesUninstall ReqMethod = "plugin_packages.uninstall"
```

- [ ] **Step 2: 全部 20 个加进 `AllReqMethods()` 尾部**；`req_method_test.go` 计数 202 → **222**（注释同步：+20 扩展设备管理）。
- [ ] **Step 3: 跑测试** `go test ./internal/swarm/schema/ -count=1` → PASS。
- [ ] **Step 4: Commit** `feat(schema): 扩展设备管理 20 个 ReqMethod 常量进清单`。

### Task 2: packageDirs——三目录扫描与冲突校验

**Files:**
- Create: `internal/swarm/server/runtime/equipment/package_dirs.go`
- Test: `internal/swarm/server/runtime/equipment/package_dirs_test.go`
- Python: `extension_package_manager.py:923-958`（`_equipment_package_dirs`）+ `:1012-1050`（`_kind_root`/`_built_in_root`/`_local_root`/`_marketplace_path`/`_resources_root`）+ `:1937` 附近 `_iter_local_package_dirs`/`_iter_resource_package_dirs`

- [ ] **Step 1: 写失败测试**——临时数据根建三目录各放一个包（含 manifest.json），断言：返回 `[(dir, source)]` 顺序 resources→local→built_in 去重；local 与 resources 同名 → error "package conflict: %s exists in both local and resources"；local 与 built_in 同名 → 同型报错；resources 与 built_in 同名**不报错**（resources 优先，built_in 跳过——对照 Python :936-943 确认）。

```go
func TestEquipmentPackageDirs_三目录合并(t *testing.T) {
	root := t.TempDir()
	t.Setenv(pathutil.EnvDataDir, root)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	// resources：bin/resources/plugins/agent_templates/pkg_a；
	// built_in：root/agent/agent_templates/built_in/pkg_b；
	// local：root/agent/agent_templates/local/pkg_c
	// 期望：[(pkg_a,builtin),(pkg_b,builtin),(pkg_c,local)]
}
func TestEquipmentPackageDirs_冲突报错(t *testing.T) { /* local∩resources 与 local∩built_in 两例 */ }
```

- [ ] **Step 2: 跑测试确认 FAIL**（符号未定义）。
- [ ] **Step 3: 实现**——复用 `equipment_slice.go` 既有 `kindRoot`；新增：

```go
// EquipmentPackageDirs 三目录扫描（resources → built_in → local，冲突校验）。
// Python: _equipment_package_dirs (:923-958)
func EquipmentPackageDirs(kind string) ([]PackageDirEntry, error)

type PackageDirEntry struct {
	// Dir 包目录绝对路径
	Dir string
	// Source "builtin" | "local"
	Source string
}
```

resources 根 = 可执行文件旁 `resources/plugins/<kind>/`（15.18a 既有 `resourcesRoot` 助手，若无则按 `equipment_slice.go` 现有 resources 定位逻辑补）。
- [ ] **Step 4: 测试 PASS → Commit** `feat(equipment): 三目录扫描与冲突校验`。

### Task 3: installState——marketplace.json 本地读写

**Files:**
- Create: `internal/swarm/server/runtime/equipment/install_state.go`
- Test: `install_state_test.go`
- Python: `marketplace/hub_install_state.py` 本地读写段 + `extension_package_manager.py:1041`（`_marketplace_path`）+ `_marketplace_index`（list 时构建 `pkg_name → entry` 索引）

- [ ] **Step 1: 失败测试**：写入一条记录 → `GetByPackageID` 命中；文件损坏/缺失 → 空表不报错；`kind` 不匹配的记录不返回（Python `record.kind == kind` 判断）。
- [ ] **Step 2: FAIL → 实现**：

```go
// HubInstallStateRecord marketplace.json 单条安装状态。
type HubInstallStateRecord struct {
	// AssetID Hub 资产 UUID（本地包为空）
	AssetID string `json:"asset_id"`
	// Kind 资产种类：agent_template | agent_group | plugin
	Kind string `json:"kind"`
	// PackageID 本地包目录名
	PackageID string `json:"package_id"`
	// Installed 是否已安装
	Installed bool `json:"installed"`
	// UpdatedAtNs 更新时间（纳秒）
	UpdatedAtNs int64 `json:"updated_at_ns"`
}

// InstallStateStore marketplace.json 本地读写（Hub 同步段不移植，D-1）。
// Python: marketplace/hub_install_state.py HubInstallStateStore 本地半区。
type InstallStateStore struct { /* kind + root */ }
func NewInstallStateStore(kind string) *InstallStateStore
func (s *InstallStateStore) GetByPackageID(packageID string) *HubInstallStateRecord
func (s *InstallStateStore) SetInstalled(packageID string, installed bool) error
```

- [ ] **Step 3: PASS → Commit** `feat(equipment): marketplace.json 安装状态本地读写`。

### Task 4: cards——模板/分组列表卡投影

**Files:**
- Create: `internal/swarm/server/runtime/equipment/cards.go`
- Test: `cards_test.go`
- Python: `_build_agent_template_list_card` :771-830、`_build_agent_group_card`（grep 定位）、`_apply_list_source_filter` :2041、`_omit_enterprise_hidden_cards` :2485、`_filter_allowed_cards` :2504、`_marketplace_index`、`list_agent_templates` :2073-2098、`list_agent_groups` :2387-2450 的本地段

- [ ] **Step 1: 失败测试**：手写两个包目录（模板包 manifest 带 display_name/display_description 语言 map/tags/avatar；组包走 `experts.LoadAgentGroupPackageBundle` 路径），断言：模板卡 13 字段投影与 15.14 `ParseAgentTemplateCard` 输出一致（id=目录名、displayName 回退目录名、i18n 按 cn/zh/en 键序）；组卡含 agents 花名册摘要；`filter=local/mine` 只留 local、`filter=builtin` 只留 builtin；enterprise hidden 恒放行（D-4）。
- [ ] **Step 2: FAIL → 实现**（签名）：

```go
// BuildAgentTemplateListCard 模板列表卡（复用 15.14 卡片解析）。
// Python: _build_agent_template_list_card (:771-830)
func BuildAgentTemplateListCard(pkgDir, source string, marketplace map[string]any, includeTeamCompatibility bool) (map[string]any, error)
// ListAgentTemplates 模板列表（本地三目录；Hub 合并段跳过 D-2）。
// Python: list_agent_templates (:2073-2098) + list_agent_templates_with_hub 本地段 (:2299-2312)
func ListAgentTemplates(params map[string]any) ([]map[string]any, error)
// ListAgentGroups 分组列表（复用 15.16e LoadAgentGroupPackage）。
// Python: list_agent_groups (:2387-2440 本地段)
func ListAgentGroups(params map[string]any) ([]map[string]any, error)
// applyListSourceFilter Python: _apply_list_source_filter (:2041)
func applyListSourceFilter(cards []map[string]any, params map[string]any) []map[string]any
```

i18n 键序用 `template_card.go` 既有 `localizedText` 助手；`_i18n(value, fallback)` 语义对齐。
- [ ] **Step 3: PASS → Commit** `feat(equipment): 模板/分组列表卡投影`。

### Task 5: show + files——详情与包内文件树

**Files:**
- Create: `internal/swarm/server/runtime/equipment/show.go`、`files.go`
- Test: `show_test.go`、`files_test.go`
- Python: `show_agent_template_with_hub` :2965、`show_agent_group_with_hub` :2694、`_resolve_show_package_dir`（本地→built_in→resources 顺序）、`list_agent_template_files_with_hub` :3610、`read_agent_template_file_with_hub` :3628、组文件树同构段

- [ ] **Step 1: 失败测试**：show 命中三来源各一次（顺序 local > built_in > resources）；不存在 → NOT_FOUND 语义 error；allowlist 门卫结构保留（D-4 恒放行）。file.read：包内相对路径正常读；`../` 穿越 → error；绝对路径 → error；二进制/超限按 Python 行为（对照 :3628-3680 逐行）。
- [ ] **Step 2: FAIL → 实现**：

```go
// ShowAgentTemplate 模板详情。Python: show_agent_template_with_hub 本地段 (:2965-2976)
func ShowAgentTemplate(name string) (map[string]any, error) // nil 卡 → NotFoundError
// ShowAgentGroup 分组详情。Python: show_agent_group_with_hub 本地段 (:2694)
func ShowAgentGroup(name string) (map[string]any, error)
// ListPackageFiles 包内文件树。Python: list_agent_template_files_with_hub (:3610)
func ListPackageFiles(kind, name string) (any, error)
// ReadPackageFile 读包内文件（安全路径校验）。Python: read_agent_template_file_with_hub (:3628)
func ReadPackageFile(kind, name, path string) (map[string]any, error)
```

- [ ] **Step 3: PASS → Commit** `feat(equipment): 设备详情与包内文件树`。

### Task 6: localMgmt——create/import_local/update/delete

**Files:**
- Create: `internal/swarm/server/runtime/equipment/local_mgmt.go`
- Test: `local_mgmt_test.go`
- Python: `create_agent_template` :3735-3784、`update_agent_template` :3785-3846、`delete_agent_template` :3847-3932、`create_agent_group` :3933-4378（组创建体量大：花名册生成/成员骨架，逐段移植）、`import_agent_template` :4379-4388、`import_agent_group` :4389-4400

- [ ] **Step 1: 失败测试**：create 生成 `local/<name>/manifest.json`（name=目录名强校验）；重名 → error；import_local 从临时源目录拷贝 + 安全名校验（`..`/绝对路径拒绝）；update 替换 local 包 + 先调 `UnloadLiveEquipment`（本任务先以可注入 stub 断言调用顺序，Task 8 换真实现）；delete 仅删 local 包（builtin/resources 包删除 → error，对照 Python :3847-3932 确认保护逻辑）；组 create 校验 agents 花名册含 leader（复用 15.16e 校验语义）。
- [ ] **Step 2: FAIL → 实现**（签名）：

```go
// CreateAgentTemplate 创建模板包骨架。Python: create_agent_template (:3735-3784)
func CreateAgentTemplate(params map[string]any) error
// UpdateAgentTemplate 更新本地模板（unload-live 前置）。Python: update_agent_template (:3785-3846)
func UpdateAgentTemplate(params map[string]any, unloadLive UnloadLiveFunc) error
// DeleteAgentTemplate 删除本地模板（unload-live 前置）。Python: delete_agent_template (:3847-3932)
func DeleteAgentTemplate(params map[string]any, unloadLive UnloadLiveFunc) error
// CreateAgentGroup 创建分组包。Python: create_agent_group (:3933-4378)
func CreateAgentGroup(params map[string]any) (map[string]any, error)
// ImportAgentTemplate / ImportAgentGroup 本地导入。Python: :4379-4400
func ImportAgentTemplate(params map[string]any) (map[string]any, error)
func ImportAgentGroup(params map[string]any) (map[string]any, error)
// UnloadLiveFunc 注入缝（Task 8 换真实现；对齐 Python unload_live 属性探测）。
type UnloadLiveFunc func(kind, packageID string) error
```

- [ ] **Step 3: PASS → Commit** `feat(equipment): 设备本地管理 create/import/update/delete`。

### Task 7: uninstall——卸载通知 + 组织包

**Files:**
- Create: `internal/swarm/server/runtime/equipment/uninstall_notice.go`、`organization_packages.go`
- Test: `uninstall_notice_test.go`
- Python: `uninstall_equipment_with_notice` :3248-3300（含事件通知形状）、`uninstall_organization_agent_group`（organization_packages.py:48-100）

- [ ] **Step 1: 失败测试**：uninstall local 包 → 目录删除 + 通知载荷形状断言；builtin 包 → error；组卸载走 organization 路径（对照 :48-100 行为）；unload-live 错误上抛 → 包不删除。
- [ ] **Step 2: FAIL → 实现**：

```go
// UninstallEquipmentWithNotice 卸载设备并广播通知。Python: :3248-3300
func UninstallEquipmentWithNotice(kind string, params map[string]any, unloadLive UnloadLiveFunc) (map[string]any, error)
// UninstallOrganizationAgentGroup 组织分组卸载。Python: organization_packages.py:48-100
func UninstallOrganizationAgentGroup(params map[string]any) error
```

通知事件经既有 server 推送面（对齐 `send_event` 语义；若 Go 推送口在 adapter 缺失，按 Python 形状落 log + 返回载荷，缺口提讨论）。
- [ ] **Step 3: PASS → Commit** `feat(equipment): 设备卸载与组织分组卸载`。

### Task 8: unloadLive——15.18 半区回收

**Files:**
- Create: `internal/swarm/server/runtime/equipment/unload_live.go`
- Modify: `internal/swarm/server/adapter/external_harness_helpers.go`（unload 半区挂点）
- Test: `unload_live_test.go`
- Python: `interface.py:6016-6026`（`_unload_live_equipment`）+ adapter 侧 `unload_equipment_if_loaded`

- [ ] **Step 1: 失败测试**：会话未加载该设备 → no-op nil；已加载 → 卸载生效（15.18 绑定表查询断言）；卸载内部错误 → error 上抛（Task 6/7 依赖此语义中止删除）。
- [ ] **Step 2: FAIL → 实现**——15.18 交付记录中"unload 半区 ⤴️"的挂点：adapter 侧 `UnloadEquipmentIfLoaded(kind, packageID)` + equipment 侧：

```go
// UnloadLiveFunc 真实现：经 adapter 绑定表卸载运行会话已加载设备。
// Python: interface.py:6016-6026（adapter 缺失 no-op；错误上抛）。
func NewUnloadLiveFunc(adapter EquipmentUnloadAdapter) UnloadLiveFunc
```

- [ ] **Step 3: PASS → 更新 Task 6/7 调用点换真实现 → 全包测试 PASS → Commit** `feat(equipment): unload-live 运行时联动（回收 15.18 unload 半区挂账）`。

### Task 9: 分派层 + gateway 注册

**Files:**
- Create: `internal/swarm/server/handle_equipment_catalog.go` + `handle_equipment_catalog_test.go`
- Modify: `internal/swarm/server/handle_message.go`（分派分支）、`internal/swarm/gateway/channel_manager/web/app_web_handlers.go`（WS 注册 20 方法）、`internal/swarm/server/runtime/equipment/doc.go`（文件树条目）

- [ ] **Step 1: 失败测试**（分派单测，同 `project_repository_test.go` 口径）：`id`/`name` 别名兼容；`agent_templates.list` payload `{"templates":[...]}`；`install` 三处 → `MARKET_UNAVAILABLE`；未知方法 → BAD_REQUEST "unsupported package catalog method"。
- [ ] **Step 2: FAIL → 实现**：

```go
// HandlePackageCatalogRequest 设备目录/生命周期请求统一入口。
// Python: interface.py:2980-3120 _handle_package_catalog_request。
func HandlePackageCatalogRequest(request *schema.AgentRequest) *schema.AgentResponse
```

分派块逐 case 对齐 Python :2988-3120（payload 键名 `templates`/`agentGroups`/`group`/`template`/`tree` 原样）；update/delete/uninstall 传 `NewUnloadLiveFunc(...)`。
- [ ] **Step 3: handle_message.go 加 20 个 case（含 `schema.ReqMethodAgentTemplates*` 等）；app_web_handlers.go 追加：

```go
// ─── 扩展设备管理 20 方法（Python app_web_handlers.py 注册段——
// E2A 转发 AgentServer 控制面 handle_equipment_catalog）───
for _, m := range []string{
	"agent_templates.list", "agent_templates.show", "agent_templates.file.list",
	"agent_templates.file.read", "agent_templates.create", "agent_templates.import_local",
	"agent_templates.install", "agent_templates.uninstall", "agent_templates.update",
	"agent_templates.delete",
	"agent_groups.list", "agent_groups.show", "agent_groups.file.list",
	"agent_groups.file.read", "agent_groups.create", "agent_groups.import_local",
	"agent_groups.install", "agent_groups.uninstall",
	"plugin_packages.install", "plugin_packages.uninstall",
} {
	wc.registerMethod(m, configForwardToAgent(wc, agentClient, m), false)
}
```

- [ ] **Step 4: doc.go 条目 + go build/vet + 全包测试 → Commit** `feat(server): 设备目录分派层与 gateway 注册（20 方法）`。

### Task 10: 前端 Hub 裁剪

**Files:**
- Modify: `internal/swarm/channels/web/frontend/src/features/marketplace/InstallationFilterSelect.tsx`（去 hub 选项）、`components/AgentManagementPanel/index.tsx`（`pendingInstallQueue`/`installOutcome` 挂载点裁剪）、`viewModel.ts:114/146`（catalog 过滤兜底注释）

- [ ] **Step 1: 裁剪**——按 spec §5：过滤下拉去 hub 项；市场安装流程组件不挂载；`AgentGroupUploadDialog` **保留**（import_local 入口）；i18n 键不删。
- [ ] **Step 2: `npx tsc --noEmit` 通过 → `make web-build` → dist 入库 → Commit** `feat(web): 设计页 Hub 入口裁剪（本地导入保留）`。

### Task 11: 集成测试 + audit

**Files:**
- Create: `tests/integration/swarm/server/equipment_catalog_test.go`

- [ ] **Step 1: 套件**（对齐 SessionPinSuite 口径：`EnvDataDir` 隔离 + `pathutil.ResetCache` + ServerEnv），跨 4 子系统断言：

```go
type EquipmentCatalogSuite struct{ isuite.BaseIntegrationSuite; env *testhelpers.ServerEnv; dataDir string }
// TestList_Show_FileRead：手写 local 包 → session.pin 式 WS 投递 →
//   agent_templates.list 断言卡片字段 → show 断言详情 → file.read 断言内容
// TestCreate_Update_Delete：create → list 含新包 → update（unload-live stub 断言顺序）
//   → delete 后 list 不含
// TestUninstall_Notice：uninstall → 目录消失 + 载荷形状
// TestInstall_MarketUnavailable：三处 install → MARKET_UNAVAILABLE
// TestGroups：group create → list → uninstall（organization 路径）
```

- [ ] **Step 2: `CGO_ENABLED=1 go test -tags "integration sqlite_fts5 test" ./tests/integration/swarm/server/ -run TestEquipmentCatalogSuite -count=1` PASS。
- [ ] **Step 3: `go build -o /tmp/intaudit ./cmd/integration-audit/ && /tmp/intaudit -verify -only-changed` 0 失败 → Commit** `test(integration): 设备目录管理全链套件`。

### Task 12: 回收闭合 + PLAN 改写 + 全量回归

- [ ] **Step 1: 锚点归零**（规则 8.3）：

```bash
grep -rn "D-18-a-1\|D-18-a-2\|D-18-a-3\|D-e12" internal/ tests/ cmd/   # 0 命中（spec/plan 历史记载除外）
grep -rn "挂账 ⤴️扩展包管理章节" internal/                               # 0 命中（equipment_slice.go:4 注释已删）
```

- [ ] **Step 2: IMPLEMENTATION_PLAN.md 15.18a 行**：⤴️ → ✅，交付记录改写（本批提交号 + 偏差 D-1/D-2/D-3/D-4 登记引用 spec §4 + 挂账闭合清单）；15.16e 挂账⑦标注"已随 15.18a 回收批闭合（2026-11-27）"。
- [ ] **Step 3: AGENTS.md 双向同步**：`internal/swarm/server/runtime/` 对应包 agent.md/AGENTS.md 若涉及 equipment 契约则补一段。
- [ ] **Step 4: 全量门禁**：`go build ./... && go vet ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（分片跑）+ push。
- [ ] **Step 5: Commit** `docs(plan): 15.18a 回收批闭合——挂账清零与交付记录`。

---

## Self-Review 结论

- **Spec 覆盖**：§2 表 18+2 方法 → Task 1/9；三目录 → Task 2；installState → Task 3；卡片 → Task 4；show/files → Task 5；CRUD → Task 6；uninstall → Task 7；unload-live → Task 8；前端 → Task 10；回填闭合 → Task 12。无缺口。
- **占位符**：无 TBD；移植任务以 Python 行号锚点 + Go 签名为交付内容（本仓对齐惯例），签名与后续任务引用一致。
- **类型一致性**：`UnloadLiveFunc` Task 6 定义 → Task 7/8/9 复用同名；`PackageDirEntry`/`InstallStateStore` 各任务引用一致。
