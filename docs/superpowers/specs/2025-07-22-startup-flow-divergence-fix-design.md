# uapclaw app 启动流程差异修复设计

## 背景

在实现 `uapclaw app` 启动流程对齐 Python 时，发现 4 个差异问题需要修复：

1. **D1**：logger 的 `resolveConfigFilePath()` 路径逻辑与 `workspace.ConfigFile()` 不一致，且存在 logger↔workspace 循环依赖（通过代码重复规避）
2. **D2**：`ConfigFile()`/`EnvFile()` 基于 `WorkspaceDir()`（无回退），Python 基于 `get_config_dir()`（有 resources 回退）
3. **D3**：`ParseCustomHeaders` 返回 `any`，调用方需要再断言
4. **D4**：`ParseCustomHeaders` 解析失败时缺少 Warn 日志，不符合项目规则 3（日志同步）

## 决策记录

| 问题 | 决策 | 理由 |
|------|------|------|
| D1 | 提取 `utils/path` 子包，消除循环依赖和代码重复 | Python 日志模块完全委托 `get_config_file()` 共享路径函数，Go 应同样走共享路径 |
| D2 | `ConfigFile()`/`EnvFile()` 改为基于 `ConfigDir()` | 对齐 Python `get_config_file()` → `get_config_dir()` 链，未初始化场景也能找到配置 |
| D3 | `ParseCustomHeaders` 返回 `map[string]any` | 更具体的返回类型，减少调用方类型断言，赋值到 `map[string]any` 的 value 无需再断言 |
| D4 | 补 Warn 日志 | 对齐 Python `_parse_custom_headers` 的 warning（Python 两条，Go 一条，因为 Go json.Unmarshal 非对象直接报错），符合项目规则 3 |

## 设计

### 1. 新建 `internal/common/utils/path/` 包

提取纯路径计算逻辑（不含 logger 调用），作为 workspace、logger、config 的共同依赖。

对应 Python `jiuwenswarm/common/utils.py` 中的路径函数族。

#### 导出常量

```go
const (
    EnvHome            = "UAPCLAW_HOME"
    EnvDataDir         = "UAPCLAW_DATA_DIR"
    EnvResourcesDir    = "UAPCLAW_RESOURCES_DIR"
    DefaultDir         = ".uapclaw"
    DefaultInstancesDir = ".uapclaw-instances"
)
```

#### 导出结构体

```go
// ResolvedPaths 缓存已解析的 ConfigDir 和 AgentWorkspaceDir。
// 对应 Python: _config_dir / _workspace_dir 全局变量。
type ResolvedPaths struct {
    ConfigDir    string // 配置目录
    WorkspaceDir string // Agent 工作空间目录
}
```

#### 导出函数

| 函数 | 签名 | 说明 | 对应 Python |
|------|------|------|-------------|
| `UserHomeDir()` | `string` | UAPCLAW_HOME → os.UserHomeDir() → "." | `get_user_home()` |
| `WorkspaceDir()` | `string` | UAPCLAW_DATA_DIR → UserHomeDir()/.uapclaw | `get_user_workspace_dir()` |
| `ConfigDir()` | `string` | 已初始化→WorkspaceDir()/config，未初始化→回退 ResourcesDir() | `get_config_dir()` |
| `ConfigFile()` | `string` | **ConfigDir()/config.yaml**（D2 修改） | `get_config_file()` |
| `EnvFile()` | `string` | **ConfigDir()/.env**（D2 修改） | `get_config_file()` 同级 |
| `AgentWorkspaceDir()` | `string` | 已初始化→WorkspaceDir()/agent/workspace，未初始化→回退 | `get_workspace_dir()` |
| `ResourcesDir()` | `(string, error)` | 三级回退：env → exec 同目录 → cwd | Go 特有 |
| `IsInitialized()` | `bool` | WorkspaceDir()/config 是否存在 | `_resolve_paths()` 判断 |
| `SetUserHome(path)` | — | 重置所有缓存 | `set_user_home(path)` |
| `ResetCache()` | — | 重置所有 sync.Once 缓存 | — |

#### 非导出函数

| 函数 | 说明 |
|------|------|
| `getResolvedPaths()` | 核心路径解析逻辑（对应 Python `_resolve_paths()`），纯计算无日志 |
| `dirExists(path)` | 目录存在性检查 |

#### 缓存

与 workspace 当前实现一致：`sync.Once` + 全局变量保证幂等。

#### 不导入

- ❌ `logger` — 避免循环依赖（workspace → logger → path → logger 会循环）
- ❌ `config` — 避免循环依赖（config → path → config 会循环）

### 2. workspace 改为调用 path 包

`workspace/paths.go` 变为薄代理层：

- 常量改为从 path 包引用（或重导出）
- `UserHomeDir()`、`WorkspaceDir()` 等函数调用 `path.Xxx()`
- `getResolvedPaths()` 逻辑迁移到 path 包
- workspace 在需要时自行补日志（如 `ConfigDir()` 调用后检查回退场景补 Warn/Info）

workspace 的其他路径辅助函数（`AgentRootDir()`、`LogsDir()` 等）保持基于 `WorkspaceDir()` 派生不变（这些不受 D2 影响，Python 也是同样的行为）。

### 3. logger 删除 `resolveConfigFilePath()`

- 删除 `resolveConfigFilePath()` 函数
- `loadLoggingConfigFromYAML()` 改为调用 `path.ConfigFile()`
- import 新增 `"github.com/uapclaw/uapclaw-go/internal/common/utils/path"`

### 4. config 的 `resolveConfigPath()` 改为调用 path 包

- 删除 `resolveConfigPath()` 函数
- `New()` 中 path 为空时改为调用 `path.ConfigFile()`
- import 新增 `"github.com/uapclaw/uapclaw-go/internal/common/utils/path"`

### 5. D3：`ParseCustomHeaders` 返回 `map[string]any`

```go
// 改前
func ParseCustomHeaders(value any) any

// 改后
func ParseCustomHeaders(value any) map[string]any
```

调用方 `NormalizeConfig` 中 `mcc["custom_headers"] = ParseCustomHeaders(raw)` 无需改动（`map[string]any` 的 value 本身是 `any`，隐式转换）。

### 6. D4：补 Warn 日志

对齐 Python `_parse_custom_headers` 的 warning：

```go
func ParseCustomHeaders(value any) map[string]any {
    // ... nil/空检查 ...

    var result map[string]any
    if err := json.Unmarshal([]byte(s), &result); err != nil {
        // 对齐 Python: logger.warning(f"custom_headers JSON parse failed: {e}")
        logger.Warn(logger.ComponentCommon).
            Str("value", truncate(s, 100)).
            Err(err).
            Msg("custom_headers JSON 解析失败")
        return nil
    }
    return result
}
```

注意：Python 中 `if isinstance(result, dict)` 检查在 Go 中不需要——`json.Unmarshal` 到 `map[string]any` 时，非 object 类型会直接返回 error，已被上面的 error 分支覆盖。所以只需补一条 Warn 日志。

## 依赖关系图

### 修复前

```
workspace → logger
logger → config
logger 内部硬编码 resolveConfigFilePath()（代码重复，路径逻辑不一致）
config 内部硬编码 resolveConfigPath()（代码重复，路径逻辑不一致）

潜在循环：logger → workspace → logger（当前通过代码重复规避）
```

### 修复后

```
workspace → path, logger       （workspace 调 path 做路径计算，调 logger 做日志）
logger → config, path          （logger 调 path.ConfigFile() 找配置文件）
config → path                  （config 调 path.ConfigFile() 找配置文件）
path → (无内部依赖，纯计算)     （零依赖，只导入 os/path/filepath/sync）

无循环 ✅
```

## 影响范围

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/common/utils/path/` | 新建 | 纯路径计算包（doc.go + paths.go + paths_test.go） |
| `internal/common/workspace/paths.go` | 重构 | 改为调用 path 包，自身变薄代理 |
| `internal/common/workspace/paths_test.go` | 调整 | 测试改为验证代理行为 |
| `internal/common/logger/logger.go` | 删除+修改 | 删除 resolveConfigFilePath()，改用 path.ConfigFile() |
| `internal/common/logger/logger_test.go` | 调整 | 删除 TestResolveConfigFilePath，改用 path 测试 |
| `internal/common/config/config.go` | 修改 | 删除 resolveConfigPath()，改用 path.ConfigFile() |
| `internal/common/config/config_test.go` | 调整 | 适配新的路径解析 |
| `internal/common/config/normalize.go` | 修改 | ParseCustomHeaders 返回 map[string]any + 补 Warn 日志 |
| `internal/common/config/normalize_test.go` | 调整 | 适配返回类型变更 |
