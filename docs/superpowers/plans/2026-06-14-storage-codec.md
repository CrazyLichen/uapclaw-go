# StorageCodec 存储编解码协议实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 统一 StorageCodec 接口与 AesStorageCodec 的签名，使 AesStorageCodec 直接满足 StorageCodec 接口。

**Architecture:** 修改 StorageCodec 接口的 Encode/Decode 方法签名，从 `string` 改为 `(string, error)`，然后在 SimpleMemoryIndex 的 4 处调用点处理 error。fakeCodec 测试 mock 同步更新。在 AesStorageCodec 测试中新增接口约束验证。

**Tech Stack:** Go 1.x，无新依赖

**设计文档:** `docs/superpowers/specs/2026-06-14-storage-codec-design.md`

---

### Task 1: 修改 StorageCodec 接口签名

**Files:**
- Modify: `internal/agentcore/foundation/store/index/base.go:59-63`

- [ ] **Step 1: 修改 StorageCodec 接口的 Encode 和 Decode 方法签名**

将 `base.go` 第 59-63 行：

```go
type StorageCodec interface {
	// Encode 对文本进行编码（如加密）
	Encode(text string) string
	// Decode 对数据进行解码（如解密）
	Decode(data string) string
}
```

改为：

```go
type StorageCodec interface {
	// Encode 对文本进行编码（如加密）
	Encode(text string) (string, error)
	// Decode 对数据进行解码（如解密）
	Decode(data string) (string, error)
}
```

- [ ] **Step 2: 验证编译失败**

Run: `go build ./internal/agentcore/foundation/store/index/...`
Expected: 编译失败，因为 fakeCodec 和 SimpleMemoryIndex 中的调用点签名不匹配

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/foundation/store/index/base.go
git commit -m "refactor: StorageCodec 接口 Encode/Decode 签名加 error (4.29)"
```

---

### Task 2: 更新 fakeCodec 测试 mock 签名

**Files:**
- Modify: `internal/agentcore/foundation/store/index/base_test.go:139-165`

- [ ] **Step 1: 修改 fakeCodec 的 Encode 方法签名**

将 `base_test.go` 第 139-143 行：

```go
func (f *fakeCodec) Encode(text string) string {
	encoded := "enc:" + text
	f.encoded[encoded] = text
	return encoded
}
```

改为：

```go
func (f *fakeCodec) Encode(text string) (string, error) {
	encoded := "enc:" + text
	f.encoded[encoded] = text
	return encoded, nil
}
```

- [ ] **Step 2: 修改 fakeCodec 的 Decode 方法签名**

将 `base_test.go` 第 145-149 行：

```go
func (f *fakeCodec) Decode(data string) string {
	decoded := strings.TrimPrefix(data, "enc:")
	f.decoded[data] = decoded
	return decoded
}
```

改为：

```go
func (f *fakeCodec) Decode(data string) (string, error) {
	decoded := strings.TrimPrefix(data, "enc:")
	f.decoded[data] = decoded
	return decoded, nil
}
```

- [ ] **Step 3: 修改 TestStorageCodec_EncodeDecode 调整返回值接收**

将 `base_test.go` 第 156-165 行：

```go
func TestStorageCodec_EncodeDecode(t *testing.T) {
	codec := newFakeCodec()
	original := "敏感数据"
	encoded := codec.Encode(original)
	decoded := codec.Decode(encoded)

	if decoded != original {
		t.Errorf("编解码往返失败: 期望 %q, 实际 %q", original, decoded)
	}
}
```

改为：

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

- [ ] **Step 4: 验证 base_test.go 编译通过**

Run: `go build ./internal/agentcore/foundation/store/index/...`
Expected: 仍然失败（simple.go 调用点未改），但 base_test.go 本身无编译错误

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/foundation/store/index/base_test.go
git commit -m "refactor: fakeCodec 签名加 error，对齐 StorageCodec 接口 (4.29)"
```

---

### Task 3: 更新 SimpleMemoryIndex 的 4 处 codec 调用

**Files:**
- Modify: `internal/agentcore/foundation/store/index/simple.go:161-164,288-292,490-494,536-540`

- [ ] **Step 1: 修改 AddMemories 中的 codec.Encode 调用（第 161-164 行）**

将：

```go
		if s.codec != nil {
			if mem, ok := kvData["mem"].(string); ok {
				kvData["mem"] = s.codec.Encode(mem)
			}
		}
```

改为：

```go
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

- [ ] **Step 2: 修改 Search 中的 codec.Decode 调用（第 288-292 行）**

将：

```go
			if s.codec != nil {
				if mem, ok := data["mem"].(string); ok {
					data["mem"] = s.codec.Decode(mem)
				}
			}
```

改为：

```go
			if s.codec != nil {
				if mem, ok := data["mem"].(string); ok {
					decoded, err := s.codec.Decode(mem)
					if err != nil {
						continue
					}
					data["mem"] = decoded
				}
			}
```

- [ ] **Step 3: 修改 GetByID 中的 codec.Decode 调用（第 490-494 行）**

将：

```go
	if s.codec != nil {
		if mem, ok := data["mem"].(string); ok {
			data["mem"] = s.codec.Decode(mem)
		}
	}
```

改为：

```go
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

- [ ] **Step 4: 修改 ListMemories 中的 codec.Decode 调用（第 536-540 行）**

将：

```go
		if s.codec != nil {
			if mem, ok := data["mem"].(string); ok {
				data["mem"] = s.codec.Decode(mem)
			}
		}
```

改为：

```go
		if s.codec != nil {
			if mem, ok := data["mem"].(string); ok {
				decoded, err := s.codec.Decode(mem)
				if err != nil {
					continue
				}
				data["mem"] = decoded
			}
		}
```

- [ ] **Step 5: 验证编译通过**

Run: `go build ./internal/agentcore/foundation/store/index/...`
Expected: 编译成功

- [ ] **Step 6: 运行 index 包测试**

Run: `go test ./internal/agentcore/foundation/store/index/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/foundation/store/index/simple.go
git commit -m "refactor: SimpleMemoryIndex 4 处 codec 调用处理 error (4.29)"
```

---

### Task 4: 新增 AesStorageCodec 接口约束测试

**Files:**
- Modify: `internal/agentcore/memory/codec/aes_storage_codec_test.go`

- [ ] **Step 1: 在 aes_storage_codec_test.go 末尾添加接口约束测试**

在文件末尾添加：

```go
// TestAesStorageCodec_满足StorageCodec接口 验证 AesStorageCodec 满足 StorageCodec 接口
func TestAesStorageCodec_满足StorageCodec接口(t *testing.T) {
	// 验证 AesStorageCodec 满足 StorageCodec 接口
	var _ index.StorageCodec = (*AesStorageCodec)(nil)
}
```

同时添加 import：

```go
import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
)
```

- [ ] **Step 2: 验证编译通过（确认签名兼容）**

Run: `go build ./internal/agentcore/memory/codec/...`
Expected: 编译成功（AesStorageCodec 的 Encode/Decode 签名已满足 StorageCodec 接口）

- [ ] **Step 3: 运行 codec 包测试**

Run: `go test ./internal/agentcore/memory/codec/... -v -count=1`
Expected: 所有测试通过，包括新增的接口约束测试

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/codec/aes_storage_codec_test.go
git commit -m "test: 添加 AesStorageCodec 满足 StorageCodec 接口的约束测试 (4.29)"
```

---

### Task 5: 更新 doc.go 文档

**Files:**
- Modify: `internal/agentcore/foundation/store/index/doc.go:25`
- Modify: `internal/agentcore/memory/codec/doc.go:21`

- [ ] **Step 1: 更新 index/doc.go 的 StorageCodec 描述**

将第 25 行：

```
	StorageCodec        — 存储编解码器接口，用于对记忆文本加解密
```

改为：

```
	StorageCodec        — 存储编解码器接口，Encode/Decode 返回 (string, error)，AesStorageCodec 满足此接口
```

- [ ] **Step 2: 更新 codec/doc.go 的 AesStorageCodec 描述**

将第 21 行：

```
	AesStorageCodec — AES-256-GCM 存储编解码器，key 为空时 passthrough，key 非空时严格模式
```

改为：

```
	AesStorageCodec — AES-256-GCM 存储编解码器，实现 index.StorageCodec 接口，key 为空时 passthrough
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/foundation/store/index/doc.go internal/agentcore/memory/codec/doc.go
git commit -m "docs: 更新 StorageCodec 和 AesStorageCodec 的 doc.go 描述 (4.29)"
```

---

### Task 6: 全量回归测试 + 更新实现计划状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` (4.29 状态)

- [ ] **Step 1: 运行全量测试**

Run: `go test ./internal/agentcore/... -count=1`
Expected: 所有测试通过

- [ ] **Step 2: 运行覆盖率检查**

Run: `go test -cover ./internal/agentcore/foundation/store/index/... ./internal/agentcore/memory/codec/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 4.29 状态为 ✅**

将：

```
| 4.29 | ☐ | StorageCodec | 存储编解码协议 | `openjiuwen/core/foundation/store/` (StorageCodec) |
```

改为：

```
| 4.29 | ✅ | StorageCodec | 存储编解码协议 | `openjiuwen/core/foundation/store/` (StorageCodec) |
```

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 4.29 StorageCodec 状态为已完成 ✅"
```
