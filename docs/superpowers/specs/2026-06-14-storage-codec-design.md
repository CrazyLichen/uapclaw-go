# StorageCodec 存储编解码协议设计

## 1. 背景与问题

### 1.1 实现计划

IMPLEMENTATION_PLAN.md 步骤 4.29：StorageCodec 存储编解码协议，对应 Python `openjiuwen/core/foundation/store/base_memory_index.py` 中的 `StorageCodec` Protocol。

### 1.2 现状

当前 Go 项目中存在两套互不兼容的编解码类型系统：

| 位置 | 类型 | Encode/Decode 签名 |
|------|------|-------------------|
| `foundation/store/index/base.go` | `StorageCodec` 接口 | `(string)` 无 error |
| `foundation/store/index/base_test.go` | `fakeCodec` 测试 mock | `(string)` 无 error |
| `memory/codec/aes_storage_codec.go` | `AesStorageCodec` 结构体 | `(string, error)` 有 error |

**核心矛盾**：`AesStorageCodec` 的方法签名返回 `(string, error)`，与 `StorageCodec` 接口要求的 `string` 不兼容。因此 `AesStorageCodec` 没有实现 `StorageCodec` 接口，两者各自独立使用：

- `StorageCodec` + `fakeCodec`：在 `foundation/store/index/` 包中供 `SimpleMemoryIndex` 使用
- `AesStorageCodec`：在 `memory/codec/` 包中供 `SqlMessageStore` 直接使用

### 1.3 Python 对比

Python 的 `StorageCodec` 是 Protocol（鸭子类型），签名 `encode(text: str) -> str`，不抛异常。`AesStorageCodec` 实现了 `encode`/`decode`，在 Python 中自动满足 `StorageCodec` Protocol，无需显式声明。

Python 中 `StorageCodec` 有两种使用模式：

1. **通过接口注入**：`LongTermMemory.set_config()` 创建 `AesStorageCodec` 实例，通过 `SimpleMemoryIndex.set_storage_codec(codec: StorageCodec)` 注入
2. **直接构造使用**：`SqlMessageStore` 和 `VariableManager` 在 `__init__` 中直接 `AesStorageCodec(crypto_key)` 创建并内部持有，字段类型未标注为 `StorageCodec`

### 1.4 设计决策

选择**方案 A：接口签名加 error**，将 `StorageCodec` 接口改为返回 `(string, error)`。

理由：

1. Go 惯用做法，错误显式返回
2. `AesStorageCodec` 直接满足 `StorageCodec` 接口，无需适配器
3. AesStorageCodec 已有容错逻辑（失败返回原文 + Warn 日志），error 只在极端情况非 nil
4. 之前 review 已确认 Go 比 Python 更严格是可接受的（如 key 长度校验）

## 2. 接口签名变更

### 2.1 StorageCodec 接口

```go
// 变更前
type StorageCodec interface {
    Encode(text string) string
    Decode(data string) string
}

// 变更后
type StorageCodec interface {
    Encode(text string) (string, error)
    Decode(data string) (string, error)
}
```

### 2.2 影响范围

#### foundation/store/index/ 包（4 处调用）

**simple.go — AddMemories**（第 161-164 行）：

```go
// 变更前
if s.codec != nil {
    if mem, ok := kvData["mem"].(string); ok {
        kvData["mem"] = s.codec.Encode(mem)
    }
}

// 变更后
if s.codec != nil {
    if mem, ok := kvData["mem"].(string); ok {
        encoded, err := s.codec.Encode(mem)
        if err != nil {
            return fmt.Errorf("编码记忆文本失败: %w", err)
        }
        kvData["mem"] = encoded
    }
}
```

**simple.go — Search**（第 288-292 行）：

```go
// 变更后
if s.codec != nil {
    if mem, ok := data["mem"].(string); ok {
        decoded, err := s.codec.Decode(mem)
        if err != nil {
            continue // 解码失败跳过此条，不影响其他结果
        }
        data["mem"] = decoded
    }
}
```

**simple.go — GetByID**（第 490-494 行）：

```go
// 变更后
if s.codec != nil {
    if mem, ok := data["mem"].(string); ok {
        decoded, err := s.codec.Decode(mem)
        if err != nil {
            return nil, fmt.Errorf("解码记忆文本失败: %w", err)
        }
        data["mem"] = decoded
    }
}
```

**simple.go — ListMemories**（第 536-540 行）：

```go
// 变更后
if s.codec != nil {
    if mem, ok := data["mem"].(string); ok {
        decoded, err := s.codec.Decode(mem)
        if err != nil {
            continue // 解码失败跳过此条
        }
        data["mem"] = decoded
    }
}
```

**错误处理策略**：

| 方法 | 编码失败 | 解码失败 |
|------|---------|---------|
| AddMemories | 返回 error（写入脏数据不如拒绝写入） | — |
| Search | — | continue（跳过此条，不影响其他结果） |
| GetByID | — | 返回 error（单条查询，无法忽略） |
| ListMemories | — | continue（跳过此条，不影响其他结果） |

注：AesStorageCodec 的容错行为（失败返回原文 + Warn 日志）意味着在正常使用场景下 error 几乎不会出现。但接口签名给调用方选择权，未来其他实现（如 RSA 编解码器）可能真正返回 error。

#### foundation/store/index/ 测试（base_test.go）

**fakeCodec** 签名变更：

```go
// 变更前
func (f *fakeCodec) Encode(text string) string {
    encoded := "enc:" + text
    f.encoded[encoded] = text
    return encoded
}
func (f *fakeCodec) Decode(data string) string {
    decoded := strings.TrimPrefix(data, "enc:")
    f.decoded[data] = decoded
    return decoded
}

// 变更后
func (f *fakeCodec) Encode(text string) (string, error) {
    encoded := "enc:" + text
    f.encoded[encoded] = text
    return encoded, nil
}
func (f *fakeCodec) Decode(data string) (string, error) {
    decoded := strings.TrimPrefix(data, "enc:")
    f.decoded[data] = decoded
    return decoded, nil
}
```

**TestStorageCodec_EncodeDecode** 调整断言：

```go
func TestStorageCodec_EncodeDecode(t *testing.T) {
    codec := newFakeCodec()
    original := "敏感数据"
    encoded, _ := codec.Encode(original)
    decoded, _ := codec.Decode(encoded)
    if decoded != original {
        t.Errorf("编解码往返失败: 期望 %q, 实际 %q", original, decoded)
    }
}
```

### 2.3 不变更的部分

以下文件**不需要修改**：

- `memory/codec/aes_storage_codec.go`：签名已经是 `(string, error)`，变更后自然满足 `StorageCodec` 接口
- `memory/codec/aes_storage_codec_test.go`：测试不涉及 `StorageCodec` 接口
- `memory/manage/model/sql_message_store.go`：直接使用 `*codec.AesStorageCodec`，不通过 `StorageCodec` 接口

## 3. 接口约束验证

在 `memory/codec/aes_storage_codec_test.go` 中补充 `AesStorageCodec` 对 `StorageCodec` 接口的约束测试：

```go
func TestAesStorageCodec_满足StorageCodec接口(t *testing.T) {
    // 验证 AesStorageCodec 满足 StorageCodec 接口
    var _ index.StorageCodec = (*AesStorageCodec)(nil)
}
```

注：此测试放在 `memory/codec` 包而非 `foundation/store/index` 包，因为验证自身满足外部接口是 `AesStorageCodec` 的责任。依赖方向 `memory/codec` → `foundation/store/index`，`memory/codec` 不依赖 `foundation/store/index`（仅测试文件导入），无循环依赖。

## 4. doc.go 更新

### 4.1 index/doc.go

更新 `StorageCodec` 描述，反映签名变更：

```
StorageCodec        — 存储编解码器接口，Encode/Decode 返回 (string, error)，AesStorageCodec 满足此接口
```

### 4.2 codec/doc.go

更新核心类型索引，标注 AesStorageCodec 满足 StorageCodec 接口：

```
AesStorageCodec — AES-256-GCM 存储编解码器，实现 foundation/store/index.StorageCodec 接口
```

## 5. 变更文件清单

| 文件 | 变更内容 |
|------|---------|
| `foundation/store/index/base.go` | `StorageCodec` 接口 Encode/Decode 加 error |
| `foundation/store/index/base_test.go` | `fakeCodec` 签名加 error + `TestStorageCodec_EncodeDecode` 调整 |
| `foundation/store/index/simple.go` | 4 处 codec.Encode/Decode 调用处理 error |
| `foundation/store/index/simple_test.go` | 所有通过 fakeCodec 间接调用 StorageCodec 的断言适配 |
| `foundation/store/index/doc.go` | StorageCodec 描述更新 |
| `memory/codec/aes_storage_codec_test.go` | 新增接口约束测试 |
| `memory/codec/doc.go` | AesStorageCodec 描述更新 |

## 6. 不在范围内

- `SqlMessageStore` 从 `*codec.AesStorageCodec` 改为 `StorageCodec` 接口：属于后续步骤（7.x 记忆系统）的优化，不在 4.29 范围内
- `VariableManager` 相关实现：同上，属于记忆系统步骤
- 新增编解码器实现（如 RSA）：YAGNI，未来按需添加
