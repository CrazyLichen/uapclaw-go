# vector_fields 通用框架设计

> 对应 Python 源码：`openjiuwen/core/foundation/store/vector_fields/`
> 实现：`internal/agentcore/store/vector_fields/`

## 1. 背景与目标

Python 项目中 `vector_fields/` 包定义了向量索引配置类型层次结构（`VectorField` 基类 + Milvus/Chroma/PG 子类），用于描述向量数据库的索引算法参数。Go 项目已完成 `FieldSchema`/`CollectionSchema`（数据描述层），但索引配置层完全缺失。

本次**只实现通用框架**（基类 + 反射机制），具体后端子类留到 4.8-4.11 各自实现时补充。

### 两个正交体系

| 体系 | 描述 | Go 状态 |
|------|------|---------|
| `FieldSchema`/`CollectionSchema` | 描述集合中存储什么数据（列名、类型、主键、维度） | ✅ 已实现 |
| `VectorField` 层次结构 | 描述向量索引如何配置（算法参数：M、efConstruction、nprobe 等） | ❌ 本次实现 |

## 2. 包结构与文件划分

```
internal/agentcore/store/vector_fields/
├── doc.go              # 包文档
├── base.go             # VectorField 基类 + 枚举 + 常量 + 反射 ToDict
└── base_test.go        # 基类单元测试
```

- 包导入路径：`github.com/xxx/uap-claw-go/internal/agentcore/store/vector_fields`
- `vector_fields` 与 `vector` 包互不导入，无循环依赖
- 后续 Store 实现同时导入两者
- 后续子类在同目录新增文件（`milvus.go`、`chroma.go`、`pg.go`），同一包内

## 3. 核心类型定义

### 3.1 枚举

```go
// DatabaseType 向量数据库类型
type DatabaseType int

const (
    DatabaseTypeMilvus DatabaseType = iota  // "milvus"
    DatabaseTypeChroma                       // "chroma"
    DatabaseTypePG                           // "pg"
    DatabaseTypeGauss                        // "gauss"
    DatabaseTypeES                           // "es"
)

// IndexType 索引类型
type IndexType int

const (
    IndexTypeAUTO  IndexType = iota  // "auto"
    IndexTypeHNSW                     // "hnsw"
    IndexTypeFLAT                     // "flat"
    IndexTypeIVF                      // "ivf"
    IndexTypeSCANN                    // "scann"
    IndexTypeIVFFlat                  // "ivfflat"
)
```

两个枚举都有 `String()` 方法，返回 Python 兼容字符串值。

### 3.2 Stage 常量

```go
const (
    StageConstruct = "construct"  // 构建阶段（建索引时的参数）
    StageSearch    = "search"     // 搜索阶段（查询时的参数）
)
```

### 3.3 VectorField 基类

```go
// VectorField 向量索引配置基类
//
// 子类通过嵌入 VectorField 并在字段上添加 `vf:"construct"` 或 `vf:"search"`
// 结构体标签来标记字段所属阶段。ToDict(stage) 通过反射读取标签，
// 只输出匹配阶段的非零值字段。
//
// 内部字段（DatabaseType、IndexType、VectorFieldName）用 `vf:"-"` 标记，
// 始终被过滤，不会出现在 ToDict 输出中。
type VectorField struct {
    DatabaseType    DatabaseType `vf:"-"`  // 向量数据库类型
    IndexType       IndexType    `vf:"-"`  // 索引类型
    VectorFieldName string       `vf:"-"`  // 向量字段名
}
```

### 3.4 构造函数与钩子

```go
// NewVectorField 创建向量索引配置基类实例
func NewVectorField(dbType DatabaseType, indexType IndexType, fieldName string) *VectorField

// Validate 校验配置参数，子类可覆盖实现自定义校验逻辑
func (vf *VectorField) Validate() error  // 基类默认返回 nil
```

## 4. vf 结构体标签语法

### 4.1 语法格式

```
vf:"<stage>"           — 标记字段属于指定阶段
vf:"<stage>,keepzero"  — 标记字段属于指定阶段且保留零值
vf:"-"                 — 内部字段，始终过滤
(无 vf 标签)            — 当作内部字段，始终过滤
```

### 4.2 阶段值

| 值 | 含义 |
|----|------|
| `construct` | 构建阶段参数（建索引时使用） |
| `search` | 搜索阶段参数（查询时使用） |
| `-` | 内部字段，不出现在 ToDict 输出中 |

### 4.3 keepzero 修饰符

- **默认行为**：带 `vf` 标签的字段，零值（`int` 的 0、`float64` 的 0.0 等）被过滤
- **`keepzero` 修饰**：即使零值也输出到 ToDict 结果中
- 未来可扩展更多修饰符（逗号分隔）

### 4.4 标签解析

```go
// parseVFTag 解析 vf 结构体标签，返回 stage 和是否 keepzero
func parseVFTag(tag string) (stage string, keepZero bool)
```

## 5. ToDict 反射机制

### 5.1 方法签名

```go
func (vf *VectorField) ToDict(stage string) map[string]any
```

### 5.2 工作流程

1. 通过反射遍历接收者的所有字段（包括子类嵌入后的提升字段）
2. 读取每个字段的 `vf` 结构体标签
3. 过滤规则：
   - `vf:"-"` → 跳过（内部字段）
   - `vf` 标签缺失 → 跳过（安全默认）
   - `vf` 标签 stage 与参数不匹配 → 跳过
   - `vf` 标签 stage 匹配 → 进入输出判断
4. 输出判断：
   - 字段名以 `Extra` 开头且类型为 `map[string]any` → 执行合并逻辑（见 5.3）
   - 无 `keepzero` 修饰且值为零值 → 跳过
   - 其他 → 以字段名为 key 写入结果 map

### 5.3 Extra 字段合并机制

Python 中 `extra_construct` / `extra_search` 在 `to_dict` 时被展开合并到结果中，不保留 key 本身。Go 中的约定：

- 子类 Extra 字段命名：`ExtraConstruct map[string]any \`vf:"construct"\``、`ExtraSearch map[string]any \`vf:"search"\``
- 合并规则：
  - Extra 字段为 nil 或空 map → 跳过
  - 否则遍历 map 的每个 entry，直接写入结果 map
  - 如果普通字段 key 与 Extra 字段 key 冲突，Extra 字段覆盖（与 Python `|` 运算符行为一致）

### 5.4 子类示例（未来，不在本次范围）

```go
type MilvusHNSW struct {
    VectorField
    M              int            `vf:"construct"`
    EfConstruction int            `vf:"construct"`
    EfSearchFactor float64        `vf:"search"`
    ExtraConstruct map[string]any `vf:"construct"`
    ExtraSearch    map[string]any `vf:"search"`
}
```

### 5.5 反射实现要点

- `reflect.Value.Elem()` 获取解引用后的值
- `reflect.Value.Field(i)` + `reflect.Type.Field(i)` 遍历所有字段（包含嵌入字段提升的字段）
- 对于嵌入结构体，递归处理其字段（跳过嵌入字段本身）

## 6. 不在本次范围

- MilvusHNSW / ChromaVectorField / PGVectorField 等具体子类
- variant 变体校验逻辑（如 MilvusIVF 的 FLAT/SQ8/PQ/RABITQ）
- Str-or-Instance 便利包装（由 Indexer/Store 层自行处理）
- Milvus 特有的 `efSearchFactor` 动态计算（`ef = round(top_k * efSearchFactor)`）

## 7. 测试策略

### 7.1 测试用子类

```go
// testVectorField 测试用子类，模拟未来子类的字段标记方式
type testVectorField struct {
    VectorField
    ConstructParam  int            `vf:"construct"`
    SearchParam     float64        `vf:"search"`
    KeepZeroParam   int            `vf:"search,keepzero"`
    IgnoredField    string         // 无 vf 标签，应被过滤
    ExtraConstruct  map[string]any `vf:"construct"`
    ExtraSearch     map[string]any `vf:"search"`
}
```

### 7.2 测试场景

| 类别 | 测试用例 |
|------|---------|
| DatabaseType 枚举 | 所有值的 String()、未知值处理 |
| IndexType 枚举 | 所有值的 String()、未知值处理 |
| VectorField 构造 | NewVectorField 基本创建、字段赋值 |
| Validate 钩子 | 基类默认返回 nil |
| parseVFTag | `"construct"`、`"search"`、`"-"`、`"construct,keepzero"`、`"search,keepzero"`、空字符串、`"unknown"` |
| ToDict("construct") | 只输出 construct 阶段字段 |
| ToDict("search") | 只输出 search 阶段字段 |
| 零值过滤 | 默认行为下零值不输出 |
| keepzero | 零值也输出 |
| vf:"-" 过滤 | 内部字段不输出 |
| 无标签过滤 | 无 vf 标签字段不输出 |
| Extra 合并 | ExtraConstruct/ExtraSearch 展开合并 |
| Extra nil/空 | 不影响输出 |
| Extra 覆盖 | Extra 字段 key 覆盖普通字段 key |
| 嵌入子类 | 子类字段正确读取 |

### 7.3 覆盖率目标

≥ 85%
