# agent_templates / agent_groups 管理面 web RPC 移植设计（并入 15.18a 回收批）

- 日期：2026-11-27
- 状态：设计定稿（brainstorming 产出，待用户审阅）
- 真相源：`jiuwenswarm/server/runtime/extension_package_manager.py`（4712 行，本批取**本地半区 ~2100 行**）+ `agent_adapter/interface.py:2980-3120`（`_handle_package_catalog_request` 分派）+ `marketplace/hub_install_state.py`（本地段）+ `organization_packages.py:48`（组卸载）
- 章节定位：**并入 IMPLEMENTATION_PLAN 15.18a 行**（用户 2026-11-27 拍板，方案 B）；交付时 15.18a ⤴️ → ✅

## 0. 用户裁决记录（2026-11-27）

1. **两批走 → 收敛为一批**：讨论中确认 Hub 半区不移植后，"第二批"消失，本地半区一次交付
2. **Hub 半区整体不移植**（产品裁决：个人版无市场服务）：`install`（从 Hub 装）返回 `MARKET_UNAVAILABLE` 结构化错误，不伪装成功；list 的 Hub 目录合并段跳过；**spec §4 登记偏差**（理由 + 回收条件：接市场服务时按 Python hub_asset_port/hub_install_state.py 补）
3. **agent_groups 8 方法与 agent_templates 同批**（共用底座，避免重复造轮子）
4. **前端 Hub 隐藏**：对应方法不调用、UI 入口裁剪（§5）
5. **`_unload_live_equipment` 本批一起做**（顺带回收 15.18 unload 半区挂账）
6. 章节定位方案 B：并入 15.18a 行，不新设"扩展包管理"章节

## 1. 流程位置与作用（事实存档）

```
浏览器（智能体设计页 / 建团队选人页）
   │  WS: agent_templates.* / agent_groups.*（10 + 8 个方法）
   ▼
Gateway WebChannel（薄代理，E2A 转发，无业务）
   ▼
AgentServer handle_message
   │  interface.py:2980 _handle_package_catalog_request（id/name 兼容别名 :2984-2986）
   ▼
extension_package_manager（本地半区）────────────────────────┐
   ├─ 读路径：manifest → 卡片投影（复用 15.14 template_card）  │
   ├─ 本地 CRUD：create/import_local/update/delete/uninstall  │
   └─ 三目录落盘：resources/ + built_in/ + local/ ←───────────┘
   ▼ 消费者（既有链路，零接线）
   ① 15.16e experts 装配链（建团队 / spawn_expert 运行时 locateInstalled 直扫目录）
   ② 15.18 ResolveEquipmentRuntimeID（external harness 绑定）
```

**作用**：专家模板/分组的"货架管理员"。三目录来源隔离（同名包冲突报错，:923-958）：

| 目录 | source | 写入者 |
|---|---|---|
| `resources/plugins/<kind>/`（发行包内） | `builtin` | 打包时 |
| `<workspace>/agent/agent_templates|agent_groups/built_in/` | `builtin` | 运行时/升级 |
| `<workspace>/agent/agent_templates|agent_groups/local/` | `local` | **用户操作**（create/import_local） |

另有 `marketplace.json` 安装状态索引（每 kind 一份，`_marketplace_path` :1041）。

## 2. 方法清单与 Python 锚点（分派 interface.py:2980-3120）

| 方法 | package_manager 调用 | Python 锚点 | Hub 依赖 |
|---|---|---|---|
| `agent_templates.list` | `list_agent_templates_with_hub` → `_list_equipment_with_hub`（hub_port=None 走本地） | :2299 / :2073（`list_agent_templates`）+ `_build_agent_template_list_card` :771 | 无（hub_port=None） |
| `agent_templates.show` | `show_agent_template_with_hub` → `_show_equipment_with_hub` + allowlist/`_package_allowed` 门卫 | :2965 | 无 |
| `agent_templates.file.list` / `file.read` | `list/read_agent_template_files_with_hub` | :3610/:3628 | 无 |
| `agent_templates.create` | `create_agent_template` | :3735 | 无 |
| `agent_templates.import_local` | `import_agent_template` | :4379 | 无 |
| `agent_templates.update` | `update_agent_template` + **unload-live 前置**（:3106-3112） | :3785 | 无 |
| `agent_templates.delete` | `delete_agent_template` + **unload-live 前置**（:3091） | :3847 | 无 |
| `agent_templates.uninstall` | `resolve_equipment_runtime_id` + `unload-live` + `uninstall_equipment_with_notice`（:3077-3090） | :3248 | 无 |
| `agent_templates.install` | `install_equipment_from_hub_gated`（120s） | :3201 | **有 → MARKET_UNAVAILABLE** |
| `agent_groups.list` | `list_agent_groups_with_hub`（:2387，本地 `list_agent_groups` + hub 安装状态改写） | :2387-2450 | 仅状态改写段跳过 |
| `agent_groups.show` / `file.list` / `file.read` | 同构 | :2694 等 | 无 |
| `agent_groups.create` / `import_local` | `create_agent_group` :3933 / `import_agent_group` :4389 | 无 | |
| `agent_groups.install` | Hub gated | — | **有 → MARKET_UNAVAILABLE** |
| `agent_groups.uninstall` | `uninstall_organization_agent_group`（organization_packages.py:48） | 无 | |
| `plugin_packages.install/uninstall` 2 个 | 同分派块内（:3040-3065） | — | install→MARKET_UNAVAILABLE；uninstall 本地段随批 |

辅助件：`_apply_list_source_filter` :2041、`_omit_enterprise_hidden_cards` :2485、`_filter_allowed_cards` :2504、`_read_package_manifest`、`_resolve_show_package_dir`、`_marketplace_index`、`_package_allowed`（allowlist 门卫，个人版恒放行的路径也要保留判断结构）。

**unload-live**（interface.py:6016-6026）：`adapter.unload_equipment_if_loaded(kind, package_id)`——adapter 缺失为 no-op；**卸载错误上抛使 uninstall 不删包**。Go 侧落点：15.18 harness 的 unload 半区挂点（回收 15.18 交付记录中"unload 半区 ⤴️"项）。

## 3. Go 侧实现方式

### 3.1 文件映射（规则 9：一文件对一文件，同一 Python 源不拆包）

| Python | Go | 内容 |
|---|---|---|
| `extension_package_manager.py` 本地半区 | `internal/swarm/server/runtime/equipment/`：`equipment_slice.go`（既有切片保留）+ 新增 `package_dirs.go` / `cards.go` / `install_state.go` / `local_mgmt.go` / `files.go` / `uninstall_notice.go` | 三目录扫描/卡片/安装状态/CRUD/文件树/卸载通知 |
| `marketplace/hub_install_state.py` 本地段 | `equipment/hub_install_state.go` | `HubInstallStateStore` 本地读写；Hub 同步函数**不移植**（偏差①） |
| `organization_packages.py:48` | `equipment/organization_packages.go` | `UninstallOrganizationAgentGroup` |
| `interface.py:2980-3120` 分派 | `internal/swarm/server/handle_equipment_catalog.go` | `HandlePackageCatalogRequest(request)`：18 方法分派 + id/name 别名 + 统一错误包装 |
| `interface.py:6016` unload-live | `equipment/unload_live.go` + 15.18 半区挂点回收 | `UnloadLiveEquipment(kind, packageID)` |
| `schema` | `req_method.go` + **AllReqMethods 同步**（第四层注册点教训） | `AGENT_TEMPLATES_*` 10 + `AGENT_GROUPS_*` 8 + `PLUGIN_PACKAGES_INSTALL/UNINSTALL` 2 |
| gateway | `app_web_handlers.go` 追加 WS 注册（configForwardToAgent，对齐 Python `_FORWARD_REQ_METHODS`/register_method 段） | 20 个方法 |

### 3.2 复用件（不重写）

- 模板卡片投影 → 15.14 `harness/resources/template_card.go`（`read_agent_template_card` 对应物）
- 组包加载/卡片 → 15.16e `agent_teams/experts/agent_group.go`（`load_agent_group_package` 对应物）
- 设备 ID 解析/路径助手 → 15.18a `equipment_slice.go` 既有符号（本批使其从"切片"变为"完整本地半区"）

### 3.3 Hub 缺口处理

- `install`（templates/groups/plugins 三处）→ `{"code": "MARKET_UNAVAILABLE", "error": "专家市场服务未启用"}` 结构化响应
- `list/show` 的 hub 合并段跳过；`HubInstallStateStore` 只保留本地 marketplace.json 读写（`installed` 标记仍对 uninstall 后的重装展示有意义）
- 前端永不收到 `source=hub` 卡片 → §5 前端隐藏为兜底

### 3.4 unload-live 联动（回收 15.18 挂账）

- update/delete/uninstall 三路径前置调用 `UnloadLiveEquipment`
- 语义对齐 :6016：无加载 no-op；卸载错误**上抛中止删除**
- Go 落点：15.18 harness 会话的设备绑定表（`adapter/external_harness_helpers.go` 既有绑定查询）→ 遍历运行会话卸载；15.18 交付记录中"unload 半区 ⤴️"随之回收

## 4. 已知偏差登记（规则 9.6/12.3，需用户确认后实施）

| # | Python 锚点 | 偏差 | 理由 | 回收条件 |
|---|---|---|---|---|
| D-1 | `hub_asset_port.py` / `hub_install_state.py` Hub 同步段 / `install_equipment_from_hub_gated` :3201 | Hub 半区整体不移植，install 返回 MARKET_UNAVAILABLE | 产品裁决：个人版无市场服务（2026-11-27） | 接入市场服务时按 Python 补 |
| D-2 | `list/show_*_with_hub` 的 hub 目录合并段 | 跳过（本地三目录扫描保留） | 同上 | 同上 |
| D-3 | 异步变体（`aload_*`/`to_thread`） | 沿 D-15b 惯例同步化 | 既有偏差惯例 | 不回收 |
| D-4 | 企业版 allowlist（`_filter_allowed_cards`/`hidden_from_enterprise`/`_package_allowed`） | 保留判断结构、个人版恒放行路径照写 | 对齐结构忠实 | 企业版移植时归位 |

## 5. 前端 Hub 隐藏（不改设计页主体）

1. 目录卡片过滤兜底：`viewModel.ts:114/146` 的 `scope==='catalog'` 判断保留（hub 卡天然不出现）
2. `InstallationFilterSelect`（marketplace/）去掉 hub 专属安装状态选项
3. `pendingInstallQueue`/`installOutcome` 市场安装流程组件不再挂载；**本地导入上传对话框（`AgentGroupUploadDialog`）保留**——对应 `import_local`，正常使用
4. i18n 不删市场文案键（回填 Hub 时可复用），仅裁剪挂载点

## 6. 测试

- **单测**（equipment 包）：三目录扫描与冲突（local∩resources/local∩built_in 报错）、模板卡投影（15.14 卡片字段）、组卡投影（花名册含 leader 校验）、filter、import 安全名拒绝（`..`/绝对路径）、installState 读写、uninstall 通知、unload-live 上抛中止删除
- **集成**（`tests/integration/swarm/server/` equipment 套件）：WS 投递 → 分派 → 落盘 → payload 断言；跨 4 子系统（gateway 转发/server 分派/equipment/15.18 harness 联动）
- **回填闭合验证**：15.18a D-18-a-1/a-2/a-3 与 15.16e D-e12 grep 锚点归零；`equipment_slice.go:4` 的"挂账 ⤴️扩展包管理章节"注释删除；IMPLEMENTATION_PLAN 15.18a 行 ⤴️ → ✅ 并改写交付记录
- **门禁**：`go build/vet` + 全量回归 + audit -only-changed

## 7. 规模与交付

- Go 新增/修改 ~1800-2200 行（含测试）；前端裁剪 ~100 行
- 单批交付（本地半区一次到位），工作树分支 `feat/15.18a-equipment-local-mgmt`
- 交付物：代码 + 集成测试 + spec/plan + 15.18a 行改写 + 挂账闭合表

## 8. 明确不做（YAGNI）

- Hub 市场/下载/安装协议面（D-1）
- plugin_packages 的 list/show/file（前端插件页另有数据源，本批只带 install/uninstall 两个分派分支的插件部分：install 拒绝、uninstall 本地）
- 企业版 hidden/allowlist 的真实白名单数据（D-4 仅保留结构）
