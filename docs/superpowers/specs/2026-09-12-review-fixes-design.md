# 2026-09-10 审查修复设计规格

> 审查来源：`docs/review/2026-09-10-48h-logic-review.md`
> 验证日期：2026-09-12
> Python 参考：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

## 修复原则

1. **normalize_state 优先**：skill_manager 的 state 类型断言根因是 JSON 反序列化后 `[]any` vs `[]map[string]any`，通过 loadState 后统一规范化解决
2. **对齐 Python 行为**：每个修复逐条对照 Python 源码确认
3. **Go 惯用模式**：panic 保护用 `defer/recover`，并发保护用 `sync.Map`，error 传播用返回值

## 已确认修复项（33 项）

### 一、skill_manager（15 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| S-09 | setPluginEnabled 类型断言错误 | normalizeState 规范化后用 `.([]map[string]any)` 断言 | skill_manager.go |
| S-13 | 缺少 normalize_state | 实现 normalizeState()，loadState 后调用，含 setdefault + 类型转换 + 字段规范化 | state_utils.go |
| M-24 | 缺少 normalizeMarketplaces | 随 normalizeState 一起实现：过滤 name/url 为空 + 补全 enabled 默认值 | state_utils.go |
| M-25 | 缺少 collectExistingLocalSkillNames | 随 normalizeState 一起实现：扫描磁盘 skill 目录 + 过滤脏记录 | skill_manager.go |
| S-10 | 锁外 saveState | setPluginEnabled 内部调 saveState（在调用方持锁内执行） | skill_manager.go |
| S-11 | addLocalSkill 缺少去重 + saveState | 遍历去重（同名替换）+ 内部 saveState | skill_manager.go |
| S-12 | removeInstalledPlugin 缺少 saveState | 内部加 saveState，HandleSkillsUninstall 删冗余 saveState | skill_manager.go |
| S-26 | HandleSkillsUninstall 缺少 removeLocalSkill | 新增 removeLocalSkill 方法 + 在 HandleSkillsUninstall 中调用 | skill_manager.go |
| S-14 | 远程导入缺少白名单校验 | 新增 assertImportLocalDownloadURLAllowed：scheme=https + hostname 通配符匹配 | remote_import.go |
| S-22 | Publish 端点错误 | `/api/v1/artifacts` → `/api/v1/plugins` | skill_manager.go |
| S-23 | Delete 端点错误 | 改为 `/api/v1/plugins/{skillID}/versions/{version}` + URL encode | skill_manager.go |
| S-24 | Publish 缺少 auth + 版本参数 | 新增 resolveTeamSkillsHubAuth + 补全 Authorization/X-System-Token + force/version_desc/plugin_version/plugin_id | skill_manager.go |
| S-25 | 路径遍历保护缺失 | 新增 safeChildPath：filepath.EvalSymlinks + strings.HasPrefix | state_utils.go |
| M-22 | detail_key 不一致 | `noToken` → `tokenNotConfigured` + 补全 httpError/searchFailed | skill_manager.go |
| M-23 | 缺少单文件导入 | HandleSkillsImportLocal 增加 isFile 分支 | skill_manager.go |

### 二、DeepAdapter 流式处理（2 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| M-02 | accumulatedText 重复累加 | 删除 llm_output 分支中的 `accumulatedText += textContent` | deep_adapter.go |
| S-04 | controller_output 缺失 interaction 逻辑 | 实现 findInteractionPayloads + parseInteractionPayload，在 controller_output 分支先查找再走 inner type | stream_utils.go |

### 三、EvolutionRail（2 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| M-05 | DrainPendingHostEvents 缺 timeout 回退 | timeout==nil 时调用 r.ext.GetEvolutionTotalTimeoutSecs()，>0 则用 | evolution_rail.go |
| M-06 | SetTrajectorySink 缺 defaultMemberRole 回退 | memberRole 未传入时使用 r.defaultMemberRole | evolution_rail.go |

### 四、安全/权限（5 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| S-08 | ParsePermissionLevel("none") 返回 PermissionLevelNone | `"none"` 输入返回 error（PermissionLevelNone 仅内部使用） | models.go |
| S-22dup/M-36 | PermissionSceneHook 缺 GoCtx | CheckPermission 加 ctx context.Context 参数 + 所有调用方传 ctx | permission_engine.go |
| S-23dup | DedicatedMultimodalModelConfigured 传 configCache | 改为 d.configBase | deep_adapter_tools.go |
| M-09 | EvaluateGlobalPolicyDirectly 错误覆盖面窄 | 签名加 error 返回值，调用方检查 error 对齐 Python except | permission_engine.go |
| M-13 | resolveProjectDir 缺 instance_overrides 回退 | d.projectDir 为空时查 d.instanceOverrides["project_dir"] | deep_adapter_config.go |

### 五、DeepAdapter 配置（1 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| M-15 | createSysOperation 签名 + 缺 panic 保护 | 返回值改为只返回 SysOperation + defer/recover 优雅降级 | deep_adapter_config.go |

### 六、SkillDev（3 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| S-15+T-18 | CalcStats 标准差 + round | n>1 除以 n-1 + math.Round(x*10000)/10000 | schema.go |
| M-39 | Pipeline 未知阶段未设 ERROR | 补全 Stage=Error + Error=消息 + emit ERROR + checkpoint | pipeline.go |
| T-24 | 未使用 sortedStringKeys | 删除 | desc_optimize_stage.go |

### 七、Evolution/Experience（4 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| S-16 | 中文截断乱码 | utf8.RuneCountInString + []rune 切片（3 处） | store_projection.go |
| S-17 | RecordPresented 缺顶层保护 | 无返回值 + defer/recover + 静默处理 error | tracker.go |
| M-27 | evaluate_presented 缺顶层保护 | 同 S-17 | tracker.go |
| M-28 | 包级 map 无并发保护 | 改用 sync.Map | tracker.go |

### 八、Memory（1 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| T-20 | BatchGet 错误被吞掉 | json.Unmarshal 失败返回 error 向上传播 | user_mem_store.go |

### 九、杂项（2 项）

| 编号 | 问题 | 修复方案 | 文件 |
|------|------|---------|------|
| T-19 | fmt.Sprintf 构建路径 | 改为 filepath.Join | validate_stage.go |
| M-34 | ExtractInt/Float 断言失败无日志 | default 分支加 Warn 日志 | stream_utils.go |

## 跳过的项目

| 编号 | 原因 |
|------|------|
| M-12 | string 空值即未设置，保持策略 |
| M-40 | 平局选择行为与 Python 一致（> 保留第一个），审查分析有误 |
| M-41 | Go 返回结构体优于 Python 返回 dict |
| M-42 | Python 中也无调用方，未使用代码 |

## 已修复/不存在（无需处理）

S-01, S-02, M-01, S-03, S-05, S-06, M-07, S-07, M-08, M-10, M-26, M-29, T-03

## 标记延后（不在本次修复）

S-18, S-19, S-20（⤵️ 10.6.3-10）, S-21（⤵️ 9.3）, M-30, M-31（Team 依赖）, M-32, T-04~T-07, T-17
