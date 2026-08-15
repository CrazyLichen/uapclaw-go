# SkillManager 补全 + SkillToolkit 实现设计

> 对应 Python：`jiuwenswarm/server/runtime/skill/skill_manager.py` + `jiuwenswarm/agents/harness/common/tools/skill_toolkits.py`
> 实现计划章节：10.3.19-20 回填 + 9.38-49 回填

## 1. 概述

SkillToolkit 是将 SkillManager 的技能管理能力封装为模型可调用工具的聚合器，提供 3 个工具：search_skill、install_skill、uninstall_skill。

当前 Go 项目中：
- 服务端 SkillManager（`swarm/server/runtime/skill/`）已有基础框架，但 ClawHub/TeamSkillsHub 的 12 个方法返回 `errNotImplemented`
- 3 个本地辅助方法（getLocalSkills/getSkillMeta/isBuiltinSkill）完全缺失
- SkillToolkit 尚未实现，`deep_adapter_tools.go` 第 9 步标记为 `⤵️ 10.6.24`

本次实现分 4 个阶段，从底向上补全：本地辅助 → ClawHub → TeamSkillsHub → SkillToolkit。

## 2. 文件变更清单

| 文件 | 操作 | 说明 |
|---|---|---|
| `internal/swarm/server/runtime/skill/skill_manager.go` | 修改 | 补全 12 个 Handle* 方法 + 3 个本地辅助方法 + 4 个 TeamSkillsHub 内部辅助方法 |
| `internal/swarm/server/runtime/skill/skill_manager_test.go` | 修改 | 补全测试 |
| `internal/swarm/server/runtime/skill/doc.go` | 修改 | 更新文档 |
| `internal/swarm/agents/harness/tools/skill_toolkit.go` | 新建 | SkillToolkit 结构体 + 3 个工具 |
| `internal/swarm/agents/harness/tools/skill_toolkit_test.go` | 新建 | SkillToolkit 测试 |
| `internal/swarm/agents/harness/tools/doc.go` | 新建 | 包文档 |
| `internal/swarm/server/adapter/deep_adapter_tools.go` | 修改 | 第 9 步回填 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 更新状态标记 |

## 3. 阶段 1：本地辅助方法

### 3.1 getLocalSkills

```go
// getLocalSkills 返回本地技能列表。
func (sm *SkillManager) getLocalSkills() []map[string]any
```

- 从 `state["local_skills"]` 读取
- 类型断言 `[]map[string]any`，不存在返回空切片
- 对齐 Python：`SkillManager.get_local_skills()` → `self._state["local_skills"]`

### 3.2 getSkillMeta

```go
// getSkillMeta 从本地技能目录读取解析后的 SKILL.md 元数据。
func (sm *SkillManager) getSkillMeta(name string) map[string]any
```

- 调用 `resolveLocalSkillDir(name)` 定位目录
- 调用 `tryFindSkillFile(dir)` 查找 SKILL.md
- 调用 `parseSkillMD(file)` 解析元数据
- 附加 `skill_dir`/`skill_file` 字段
- 找不到返回 nil
- 对齐 Python：`SkillManager.get_skill_meta()`

### 3.3 isBuiltinSkill

```go
// isBuiltinSkill 判断技能是否为内置技能。
func (sm *SkillManager) isBuiltinSkill(name string) bool
```

- 空名返回 false
- `getBuiltinSkillsDir()` → 空则返回 false
- `safePathName(name)` 校验
- `os.Stat(skillsDir/name)` 和 `os.Stat(builtinDir/name)` 获取 FileInfo
- `os.SameFile(fi1, fi2)` 比较是否指向同一物理路径
- 对齐 Python：`SkillManager.is_builtin_skill()` — 比较路径 resolve 后是否等价

### 3.4 getBuiltinSkillsDir 修改

```go
// getBuiltinSkillsDir 返回内置技能目录路径。
func (sm *SkillManager) getBuiltinSkillsDir() string
```

- 当前返回空字符串，需改为从环境变量 `BUILTIN_SKILLS_DIR` 读取
- 或用默认路径（对齐 Python 的 `get_builtin_skills_dir()`）
- 不存在返回空字符串

## 4. 阶段 2：ClawHub

### 4.1 HandleSkillsClawhubSearch

```go
// HandleSkillsClawhubSearch 从 ClawHub 搜索技能。
func (sm *SkillManager) HandleSkillsClawhubSearch(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：**
- `q`（必需）— 搜索关键词
- `limit`（可选，默认 10）— 结果数量限制

**流程：**
1. `getClawhubToken()` → 无 token 返回 `{"success":false,"detail":"ClawHub token not configured"}`
2. 提取并校验参数
3. 构建请求：`GET https://clawhub.ai/api/v1/search?q={q}&limit={limit}`
   - Header: `Authorization: Bearer {token}`
   - Client: `&http.Client{Timeout: 30 * time.Second}`
4. 发送请求，处理错误（非 2xx 返回错误）
5. 解析 JSON → `data["results"]` → 映射字段：
   - `slug` ← `item["slug"]`
   - `display_name` ← `item["displayName"]`
   - `summary` ← `item["summary"]`
   - `version` ← `item["version"]`
   - `updated_at` ← `item["updatedAt"]`
6. 返回 `{"success":true, "query":q, "count":len, "skills":[...]}`

**对齐 Python：** `SkillManager.handle_skills_clawhub_search()`

### 4.2 HandleSkillsClawhubDownload

```go
// HandleSkillsClawhubDownload 从 ClawHub 下载并安装技能。
func (sm *SkillManager) HandleSkillsClawhubDownload(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：**
- `slug`（必需）— 技能 slug
- `version`（可选）— 版本号
- `tag`（可选）— 标签
- `force`（可选，默认 false）— 强制覆盖

**流程：**
1. `getClawhubToken()` → 无 token 返回错误
2. `safePathName(slug)` 校验 → 非法路径返回错误
3. 检查 `skillsDir/slug` 是否已存在，已存在且非 force 返回错误
4. 构建请求：`GET https://clawhub.ai/api/v1/download?slug={slug}&version={version}&tag={tag}`
   - Header: `Authorization: Bearer {token}`
   - Client: `&http.Client{Timeout: 120 * time.Second}`
5. 读取 `response.Body` → `[]byte`
6. 解压 ZIP 到临时目录（`os.TempDir()` + `safeExtractZIPBytesToDir`）
7. `tryFindSkillFile` → `parseSkillMD` → 获取 `skill_name`
8. `copyDir` 到 `skillsDir/skill_name`
9. `addLocalSkill({"name":skill_name, "origin":"clawhub:{slug}", "source":"clawhub"})`
10. `addInstalledPlugin({"name":skill_name, "marketplace":"clawhub", "source":"clawhub"})`
11. `saveState()`
12. 返回 `{"success":true, "skill":{"name":skill_name, "source":"clawhub"}}`

**对齐 Python：** `SkillManager.handle_skills_clawhub_download()`

## 5. 阶段 3：TeamSkillsHub

### 5.1 环境变量

| 环境变量 | 默认值 | 用途 |
|---|---|---|
| `TEAM_SKILLS_HUB_BASE_URL` | `https://teamskills.openjiuwen.com` | API 基础 URL |
| `TEAM_SKILLS_HUB_TIMEOUT` | `60` | 请求超时秒数 |
| `TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS` | `openjiuwen-market.obs.*.myhuaweicloud.com,127.0.0.1,localhost` | 下载白名单（逗号分隔，支持 `*` 通配符） |

### 5.2 内部辅助方法

#### teamSkillsHubHTTPGet

```go
// teamSkillsHubHTTPGet 向 TeamSkillsHub 发送 GET 请求。
func (sm *SkillManager) teamSkillsHubHTTPGet(ctx context.Context, path string, params url.Values, timeout int, baseURL string) (map[string]any, error)
```

- URL 构建：`baseURL + path + "?" + params.Encode()`
- `&http.Client{Timeout: time.Duration(timeout) * time.Second}`
- 非 2xx → 返回错误
- 解析 JSON → `payload["data"]`，检查 `payload["code"]` 是否为 200

#### assertTeamSkillsHubDownloadURLAllowed

```go
// assertTeamSkillsHubDownloadURLAllowed 校验下载 URL 主机名是否在白名单中。
func (sm *SkillManager) assertTeamSkillsHubDownloadURLAllowed(downloadURL string) error
```

- 从环境变量读取白名单
- 解析 `downloadURL` 的主机名
- 逐条匹配白名单规则（支持 `*` 通配符，如 `*.myhuaweicloud.com`）
- 不匹配返回错误

#### downloadZipAndVerify

```go
// downloadZipAndVerify 下载 ZIP 并校验完整性。
func (sm *SkillManager) downloadZipAndVerify(ctx context.Context, downloadURL, checksumSHA256 string) ([]byte, error)
```

- `&http.Client{Timeout: 120 * time.Second}` 下载
- 校验：1) 非空；2) 以 `PK` 开头（ZIP 魔数）；3) SHA256 校验（如果提供了 checksum）；4) `archive/zip` 读取校验完整性
- 返回 ZIP 字节

#### safeExtractZIPBytesToDir

```go
// safeExtractZIPBytesToDir 安全解压 ZIP 字节到目标目录（防 Zip Slip）。
func safeExtractZIPBytesToDir(zipBytes []byte, destDir string) error
```

- `bytes.NewReader` → `zip.NewReader`
- 遍历每个文件：`filepath.Join(destDir, f.Name)` → `filepath.Rel(destDir, targetPath)` 检查是否 `..` 开头（Zip Slip 防护）
- 创建目录结构 + 写入文件内容

### 5.3 HandleSkillsTeamSkillsHubSearch

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubSearch(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数映射：**
- `q` → `search_keyword`
- `limit` / `page_size` → `page_size`（默认 20，最大 100）
- `page` → `page`（默认 1）
- `skill_type` / `plugin_type` → `plugin_type`
- `author` → `publisher_name`
- `search_asset_id` → `asset_id`
- `search_asset_type` → `asset_type`
- `search_publisher_id` → `publisher_id`
- `order_by` → `order_by`（默认 `install_count`）
- `desc` → `desc`（默认 true）
- `market_url` → 覆盖 base URL

**流程：**
1. 构建查询参数
2. `teamSkillsHubHTTPGet("/api/v1/plugins", params, ...)`
3. 结果标准化：从 `data["items"]` 提取 `asset_id/name/display_name/summary/version/updated_at`
4. 返回 `{"success":true, "query":q, "count":len, "skills":[...]}`

### 5.4 HandleSkillsTeamSkillsHubInstall

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubInstall(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `asset_id`（必需）、`force`（可选）、`version`（可选）、`output`（可选自定义安装路径）、`market_url`（可选）

**流程：**
1. 获取 artifact 元数据：`teamSkillsHubHTTPGet("/api/v1/artifacts/{asset_id}", ...)`
2. 提取 `download_url` 和 `checksum_sha256`
3. `assertTeamSkillsHubDownloadURLAllowed(download_url)` 白名单校验
4. `downloadZipAndVerify(download_url, checksum_sha256)` 下载+校验
5. `safeExtractZIPBytesToDir` 解压到临时目录
6. `tryFindSkillFile` → `parseSkillMD` → 获取 `skill_name`
7. 安装到目标目录（`output` 路径或 `skillsDir/skill_name`）
8. `addLocalSkill` + `addInstalledPlugin` + `saveState`
9. 返回 `{"success":true, "skill":{"name":skill_name, "source":"teamskillshub", "asset_id":asset_id, "path":destPath}}`

### 5.5 HandleSkillsTeamSkillsHubInfo

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubInfo(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `asset_id`（必需）、`version`（可选）、`market_url`（可选）

**流程：** `teamSkillsHubHTTPGet("/api/v1/artifacts/{asset_id}", ...)` → 返回完整元数据

### 5.6 HandleSkillsTeamSkillsHubInit

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubInit(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `name`（必需）、`output`（可选自定义路径）

**流程：**
1. 创建目录结构：`{name}/SKILL.md`、`{name}/tools/`、`{name}/data/`
2. 写入 SKILL.md 骨架文件（含 frontmatter：name/description/version）
3. 返回 `{"success":true, "path":dirPath}`

### 5.7 HandleSkillsTeamSkillsHubValidate

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubValidate(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `path`（必需，目录路径）

**流程：**
1. 检查目录存在
2. 检查 SKILL.md 存在
3. `parseSkillMD` 解析 → 校验 `name`/`description` 必填
4. 返回 `{"success":true, "valid":true, "errors":[]}` 或 `{"success":true, "valid":false, "errors":[...]}`

### 5.8 HandleSkillsTeamSkillsHubPack

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubPack(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `path`（必需，目录路径）、`output`（可选，输出 ZIP 路径）

**流程：**
1. 遍历目录，排除 `.git`、`__pycache__`、`node_modules`
2. `archive/zip` 打包
3. 返回 `{"success":true, "zip_path":outputPath, "size":fileSize}`

### 5.9 HandleSkillsTeamSkillsHubPublish

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubPublish(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `path`（必需，ZIP 文件路径）、`market_url`（可选）

**流程：**
1. 读取 ZIP 文件
2. `multipart/form-data` 构建上传请求
3. `POST {base_url}/api/v1/artifacts`
4. 返回 `{"success":true, "asset_id":id}`

### 5.10 HandleSkillsTeamSkillsHubDelete

```go
func (sm *SkillManager) HandleSkillsTeamSkillsHubDelete(ctx context.Context, params map[string]any) (map[string]any, error)
```

**参数：** `asset_id`（必需）、`market_url`（可选）

**流程：**
1. `DELETE {base_url}/api/v1/artifacts/{asset_id}`
2. 返回 `{"success":true}`

## 6. 阶段 4：SkillToolkit

### 6.1 文件位置

`internal/swarm/agents/harness/tools/skill_toolkit.go`

对齐 Python：`jiwenswarm/agents/harness/common/tools/skill_toolkits.py`

### 6.2 结构体

```go
// SkillToolkit 把 SkillManager 暴露成模型友好的工具集合。
type SkillToolkit struct {
    manager *skill.SkillManager
}
```

### 6.3 导出方法

| 方法 | 签名 | 实现 |
|---|---|---|
| `NewSkillToolkit` | `func NewSkillToolkit(manager *skill.SkillManager) *SkillToolkit` | 构造函数 |
| `SearchSkill` | `func (tk *SkillToolkit) SearchSkill(ctx context.Context, query, source string, limit int) map[string]any` | source 归一化 → 按 source 分发搜索 → 归一化结果 |
| `InstallSkill` | `func (tk *SkillToolkit) InstallSkill(ctx context.Context, identifier, source string, timeoutSec int) map[string]any` | 查重 → 按 source 分发安装 |
| `UninstallSkill` | `func (tk *SkillToolkit) UninstallSkill(ctx context.Context, name string) map[string]any` | isBuiltinSkill 判断 → 查已安装 → 调用 HandleSkillsUninstall |
| `GetTools` | `func (tk *SkillToolkit) GetTools() []tool.Tool` | 返回 3 个 LocalFunction |

### 6.4 SearchSkill 实现

```
1. normalizeSource(source) → "auto"/"skillnet"/"clawhub"/"teamskillshub"
2. query 为空 → 返回 {"success":false, "detail":"query is required"}
3. safeInt(limit, 10)
4. getInstalledNames() → 获取已安装名称集合
5. source="auto" → sources = ["clawhub", "teamskillshub"]（暂不含 skillnet）
   source=具体值 → sources = [source]
6. 遍历 sources:
   - "clawhub" → HandleSkillsClawhubSearch
   - "teamskillshub" → HandleSkillsTeamSkillsHubSearch
   - "skillnet" → 返回 {"success":false, "detail":"skillnet is not yet supported"}
7. normalizeSearchItem() 归一化每个结果
8. 返回 {"success":anySuccess, "source":source, "items":items, "detail":detail}
```

### 6.5 InstallSkill 实现

```
1. normalizeSource(source) → 不允许 "auto"
2. identifier 为空 → 返回错误
3. findInstalledByTarget(identifier, source) → 已安装则跳过
4. 按 source 分发:
   - "clawhub" → HandleSkillsClawhubDownload
   - "teamskillshub" → HandleSkillsTeamSkillsHubInstall
   - "skillnet" → 返回 {"success":false, "detail":"skillnet is not yet supported"}
5. 安装成功 → buildInstalledItem(name, source) → 返回成功信息
```

### 6.6 UninstallSkill 实现

```
1. name 为空 → 返回错误
2. isBuiltinSkill(name) → 是内置则禁止卸载
3. listInstalledSkills() → 查找是否已安装
4. HandleSkillsUninstall → 卸载
5. 返回 {"success":true, "removed":true, "name":name, "detail":"..."}
```

### 6.7 GetTools() 返回 3 个工具

| 工具名 | input_params | func |
|---|---|---|
| `search_skill` | query(必需), source(enum:auto/skillnet/clawhub/teamskillshub,默认skillnet), limit(默认10) | `SearchSkill` |
| `install_skill` | identifier(必需), source(enum:skillnet/clawhub/teamskillshub), timeout_sec(默认60) | `InstallSkill` |
| `uninstall_skill` | name(必需) | `UninstallSkill` |

### 6.8 SkillNet 临时处理

- `source="auto"` 时只搜 ClawHub + TeamSkillsHub
- `source="skillnet"` 时返回 `{"success": false, "detail": "skillnet is not yet supported"}`
- install_skill `source="skillnet"` 同理

### 6.9 DeepAdapter 回填

`deep_adapter_tools.go` 第 9 步，从：

```go
if d.skillManager != nil {
    // ⤵️ 10.6.24: SkillToolkit 工具尚未实现
    logger.Info(logComponent).Msg("getToolCards: SkillManager 已就绪，等待 10.6.24 回填 SkillToolkit")
}
```

改为：

```go
if d.skillManager != nil {
    skillToolkit := tools.NewSkillToolkit(d.skillManager)
    for _, t := range skillToolkit.GetTools() {
        if rm.GetTool([]string{t.Card().ID}) == nil {
            _ = rm.AddTool(t)
        }
        toolCards = append(toolCards, t.Card())
    }
    logger.Info(logComponent).Int("count", len(skillToolkit.GetTools())).Msg("getToolCards: SkillToolkit 已注册")
}
```

## 7. SkillToolkit 内部辅助方法

对齐 Python `skill_toolkits.py` 中的辅助方法：

| 方法 | 签名 | 用途 |
|---|---|---|
| `normalizeSource` | `func normalizeSource(source string) (string, error)` | 归一化 source：auto/skillnet/clawhub/teamskillshub |
| `detectSource` | `func detectSource(target string) (string, error)` | 根据 identifier 模式推断 source（URL→skillnet，slug→clawhub） |
| `safeInt` | `func safeInt(value any, defaultVal int) int` | 安全整数转换，≤0 返回 defaultVal |
| `getInstalledNames` | `func (tk *SkillToolkit) getInstalledNames() map[string]bool` | 获取已安装技能名称集合 |
| `findInstalledByTarget` | `func (tk *SkillToolkit) findInstalledByTarget(identifier, source string) map[string]any` | 按 identifier 反查是否已安装 |
| `buildInstalledItem` | `func (tk *SkillToolkit) buildInstalledItem(name, source string) map[string]any` | 构建已安装技能的展示信息 |
| `normalizeSearchItem` | `func normalizeSearchItem(item map[string]any, source string, installedNames map[string]bool) map[string]any` | 归一化搜索结果为统一字段 |
| `listInstalledSkills` | `func (tk *SkillToolkit) listInstalledSkills(ctx context.Context) map[string]any` | 列出已安装技能（内部复用） |

## 8. 测试策略

### 8.1 本地辅助方法

| 测试 | 方式 | 覆盖内容 |
|---|---|---|
| `TestGetLocalSkills` | `t.TempDir()` + 写入 state | 读取/空状态/类型断言 |
| `TestGetSkillMeta` | `t.TempDir()` + 创建 SKILL.md | 正常解析/不存在/无 SKILL.md |
| `TestIsBuiltinSkill` | `t.TempDir()` + 创建内置目录 | 匹配/不匹配/无内置目录 |

### 8.2 ClawHub

| 测试 | 方式 | 覆盖内容 |
|---|---|---|
| `TestHandleSkillsClawhubSearch` | `httptest.NewServer` | 正常搜索/无 token/API 错误/空结果 |
| `TestHandleSkillsClawhubDownload` | `httptest.NewServer` + ZIP 响应 | 正常下载安装/无 token/已存在/force 覆盖 |

### 8.3 TeamSkillsHub

| 测试 | 方式 | 覆盖内容 |
|---|---|---|
| `TestHandleSkillsTeamSkillsHubSearch` | `httptest.NewServer` | 正常搜索/参数映射/空结果 |
| `TestHandleSkillsTeamSkillsHubInstall` | `httptest.NewServer` + SHA256 ZIP | 正常安装/白名单校验/SHA256 校验失败 |
| `TestHandleSkillsTeamSkillsHubInfo` | `httptest.NewServer` | 正常获取/不存在 |
| `TestHandleSkillsTeamSkillsHubInit` | `t.TempDir()` | 正常创建/目录已存在 |
| `TestHandleSkillsTeamSkillsHubValidate` | `t.TempDir()` | 有效/无效/缺失字段 |
| `TestHandleSkillsTeamSkillsHubPack` | `t.TempDir()` | 正常打包/空目录 |
| `TestHandleSkillsTeamSkillsHubPublish` | `httptest.NewServer` | 正常上传/API 错误 |
| `TestHandleSkillsTeamSkillsHubDelete` | `httptest.NewServer` | 正常删除/API 错误 |
| `TestSafeExtractZIPBytesToDir` | 构造恶意 ZIP | Zip Slip 防护/正常解压 |
| `TestAssertTeamSkillsHubDownloadURLAllowed` | 直接测试 | 白名单匹配/不匹配/通配符 |

### 8.4 SkillToolkit

| 测试 | 方式 | 覆盖内容 |
|---|---|---|
| `TestSearchSkill` | mock SkillManager | auto/skillnet/clawhub/teamskillshub 分发 |
| `TestInstallSkill` | mock SkillManager | 查重/分发/已安装 |
| `TestUninstallSkill` | mock SkillManager | 内置禁止/正常卸载/不存在 |
| `TestGetTools` | 直接验证 | 3 个工具的名称/参数/func |

## 9. 回填标记更新

| 位置 | 变更 |
|---|---|
| `deep_adapter_tools.go` 第 9 步 | 移除 `⤵️ 10.6.24`，实现回填 |
| `IMPLEMENTATION_PLAN.md` 9.38-49 | Skills 标记更新（新增 SkillToolkit） |
| `IMPLEMENTATION_PLAN.md` 10.3.19-20 | 补全 ClawHub/TeamSkillsHub 方法标记 |

## 10. HTTP 客户端模式

遵循项目现有模式：使用 `net/http` 标准库，无第三方依赖。

- 请求构建：`http.NewRequestWithContext(ctx, method, url, body)` + `req.Header.Set()`
- 客户端构建：`&http.Client{Timeout: duration, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}`
- 代理：统一使用 `http.ProxyFromEnvironment`
- JSON 编解码：`encoding/json`
- ZIP 处理：`archive/zip`
- SHA256 校验：`crypto/sha256`
