# FileKVStore (ShelveStore 等价) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现基于 bbolt 的 FileKVStore 文件持久化键值存储，满足 BaseKVStore + KVPipeline 接口，严格复刻 Python ShelveStore 语义（包括值解包不一致和过期语义不一致）。

**Architecture:** 使用 bbolt 嵌入式 KV 数据库作为底层存储，单 DB 实例复用 + Close() 优雅关闭。所有值统一编码为 `fileEntry` JSON 结构体（`{exclusive_value: base64, exclusive_expiry: timestamp}`）。依赖 bbolt 事务模型保证并发安全和 ExclusiveSet 的 read-then-write 原子性，不额外加 Go 层面锁。

**Tech Stack:** Go 1.25, bbolt (`go.etcd.io/bbolt`), encoding/json, encoding/base64

**Design Spec:** `docs/superpowers/specs/2025-07-28-file-kvstore-design.md`

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/store/kv/file.go` | 新建 | FileKVStore + filePipeline + fileEntry + 辅助函数 |
| `internal/agentcore/store/kv/file_test.go` | 新建 | FileKVStore 全量单元测试 |
| `internal/agentcore/store/kv/doc.go` | 修改 | 添加 file.go 到文件目录 |
| `go.mod` / `go.sum` | 修改 | 添加 bbolt 依赖 |

---

### Task 1: 添加 bbolt 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 bbolt 依赖**

```bash
cd /home/opensource/uap-claw-go && go get go.etcd.io/bbolt@latest
```

- [ ] **Step 2: 验证依赖添加成功**

```bash
cd /home/opensource/uap-claw-go && go mod tidy && grep bbolt go.mod
```

预期：输出包含 `go.etcd.io/bbolt`

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum && git commit -m "chore: add bbolt dependency for FileKVStore"
```

---

### Task 2: 创建 file.go 骨架 — 结构体 + fileEntry + 构造函数 + Close

**Files:**
- Create: `internal/agentcore/store/kv/file.go`

- [ ] **Step 1: 编写 fileEntry、FileKVStore、filePipeline 结构体定义、NewFileKVStore 构造函数和 Close 方法**

```go
package kv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fileEntry 文件存储条目，统一 JSON 序列化格式。
// 对齐 Python：Set 存原始值，ExclusiveSet 存 {EXCLUSIVE_VALUE_KEY, EXCLUSIVE_EXPIRY_KEY} dict。
// Go 统一用 JSON 结构体，ExpiryAt=0 表示不过期（等同于 Python 的普通 Set）。
type fileEntry struct {
	// Value 实际存储的值（Base64 编码）
	Value string `json:"exclusive_value"`
	// ExpiryAt 过期时间戳（Unix 秒），0 表示不过期
	ExpiryAt int64 `json:"exclusive_expiry"`
}

// FileKVStore 基于 bbolt 的文件持久化键值存储。
// 对应 Python ShelveStore，严格复刻其语义（包括已知的值解包不一致）。
type FileKVStore struct {
	// db bbolt 数据库实例，构造时打开，Close() 关闭
	db *bolt.DB
	// bucketName 默认 bucket 名称
	bucketName string
}

// filePipeline 文件存储 Pipeline 实现。
type filePipeline struct {
	// ops 待执行的操作列表
	ops []operation
	// store 关联的 FileKVStore 实例
	store *FileKVStore
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultBucketName 默认 bucket 名称
	defaultBucketName = "default"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFileKVStore 创建基于 bbolt 的文件 KV 存储。
// dbPath: 数据库文件路径（自动创建父目录）。
// 对齐 Python: Path(db_path).parent.mkdir(parents=True, exist_ok=True)。
func NewFileKVStore(dbPath string) (*FileKVStore, error) {
	// 创建父目录
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// 打开 bbolt 数据库
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, err
	}

	// 创建默认 bucket
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(defaultBucketName)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, err
	}

	return &FileKVStore{
		db:         db,
		bucketName: defaultBucketName,
	}, nil
}

// Close 关闭数据库连接。
func (s *FileKVStore) Close() error {
	return s.db.Close()
}

// Set 存储或覆盖一个键值对。
func (s *FileKVStore) Set(_ context.Context, key string, value []byte) error {
	entry := fileEntry{
		Value:    base64.StdEncoding.EncodeToString(value),
		ExpiryAt: 0,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		return b.Put([]byte(key), data)
	})
}

// ExclusiveSet 原子性地设置键值对，仅当 key 不存在时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在且未过期。
// 对齐 Python ShelveStore：Get 返回已过期值、Exists 对过期 key 返回 true、仅 ExclusiveSet 检查过期。
func (s *FileKVStore) ExclusiveSet(_ context.Context, key string, value []byte, expiry int) (bool, error) {
	now := time.Now().Unix()
	var result bool

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		existing := b.Get([]byte(key))

		if existing != nil {
			// 反序列化已有值
			var entry fileEntry
			if err := json.Unmarshal(existing, &entry); err != nil {
				return err
			}
			// ExpiryAt == 0 视为未过期（等同 Python 普通 Set 存的原始值）
			// ExpiryAt > 0 且 > now 视为未过期
			if entry.ExpiryAt == 0 || entry.ExpiryAt > now {
				result = false
				return nil
			}
			// ExpiryAt > 0 且 <= now：已过期，允许覆盖
		}

		// 计算过期时间戳
		var expireAt int64
		if expiry > 0 {
			expireAt = now + int64(expiry)
		}

		entry := fileEntry{
			Value:    base64.StdEncoding.EncodeToString(value),
			ExpiryAt: expireAt,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}

		if err := b.Put([]byte(key), data); err != nil {
			return err
		}
		result = true
		return nil
	})

	return result, err
}

// Get 根据 key 获取值，key 不存在时返回 nil, nil。
// 对齐 Python ShelveStore：解包 exclusive 值返回实际 []byte，不过期检查。
func (s *FileKVStore) Get(_ context.Context, key string) ([]byte, error) {
	var result []byte

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		raw := b.Get([]byte(key))
		if raw == nil {
			result = nil
			return nil
		}

		// 反序列化并解包（对齐 Python：get() 解包 exclusive dict）
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		decoded, err := base64.StdEncoding.DecodeString(entry.Value)
		if err != nil {
			return err
		}
		result = decoded
		return nil
	})

	return result, err
}

// Exists 检查 key 是否存在。
// 对齐 Python ShelveStore：不过期检查，过期 key 仍返回 true。
func (s *FileKVStore) Exists(_ context.Context, key string) (bool, error) {
	var result bool

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		raw := b.Get([]byte(key))
		result = raw != nil
		return nil
	})

	return result, err
}

// Delete 删除指定 key，key 不存在时不执行操作。
func (s *FileKVStore) Delete(_ context.Context, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		return b.Delete([]byte(key))
	})
}

// GetByPrefix 获取所有以 prefix 开头的键值对。
// 对齐 Python ShelveStore：返回原始 JSON 字节，不解包。
func (s *FileKVStore) GetByPrefix(_ context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		c := b.Cursor()

		for k, v := c.Seek([]byte(prefix)); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == prefix; k, v = c.Next() {
			result[string(k)] = v // 不解包，返回原始 JSON 字节
		}
		return nil
	})

	return result, err
}

// DeleteByPrefix 删除所有以 prefix 开头的键值对。
// batchSize 为每批删除的数量，0 表示一次性删除。
func (s *FileKVStore) DeleteByPrefix(_ context.Context, prefix string, batchSize int) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		c := b.Cursor()

		// 收集所有匹配前缀的 key
		var toDel [][]byte
		for k, _ := c.Seek([]byte(prefix)); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == prefix; k, _ = c.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			toDel = append(toDel, keyCopy)
		}

		if batchSize <= 0 {
			for _, k := range toDel {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			return nil
		}

		for i := 0; i < len(toDel); i += batchSize {
			end := i + batchSize
			if end > len(toDel) {
				end = len(toDel)
			}
			for _, k := range toDel[i:end] {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// MGet 批量获取多个 key 的值。
// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
// 对齐 Python ShelveStore：返回原始 JSON 字节，不解包。
func (s *FileKVStore) MGet(_ context.Context, keys []string) ([][]byte, error) {
	result := make([][]byte, len(keys))

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		for i, key := range keys {
			raw := b.Get([]byte(key))
			if raw != nil {
				// 不解包，返回原始 JSON 字节的拷贝
				valCopy := make([]byte, len(raw))
				copy(valCopy, raw)
				result[i] = valCopy
			}
		}
		return nil
	})

	return result, err
}

// BatchDelete 批量删除多个 key，返回成功删除的数量。
// batchSize 为每批删除的数量，0 表示一次性删除。
func (s *FileKVStore) BatchDelete(_ context.Context, keys []string, batchSize int) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	deleted := 0

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))

		if batchSize <= 0 {
			for _, key := range keys {
				k := []byte(key)
				if b.Get(k) != nil {
					if err := b.Delete(k); err != nil {
						return err
					}
					deleted++
				}
			}
			return nil
		}

		for i := 0; i < len(keys); i += batchSize {
			end := i + batchSize
			if end > len(keys) {
				end = len(keys)
			}
			for _, key := range keys[i:end] {
				k := []byte(key)
				if b.Get(k) != nil {
					if err := b.Delete(k); err != nil {
						return err
					}
					deleted++
				}
			}
		}
		return nil
	})

	return deleted, err
}

// Pipeline 创建批量操作管道。
func (s *FileKVStore) Pipeline(_ context.Context) KVPipeline {
	return &filePipeline{
		ops:   make([]operation, 0),
		store: s,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// encodeFileEntry 将 value 和 expiryAt 编码为 fileEntry JSON 字节。
func encodeFileEntry(value []byte, expiryAt int64) ([]byte, error) {
	entry := fileEntry{
		Value:    base64.StdEncoding.EncodeToString(value),
		ExpiryAt: expiryAt,
	}
	return json.Marshal(entry)
}

// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
func (p *filePipeline) Set(_ context.Context, key string, value []byte) error {
	p.ops = append(p.ops, operation{op: "set", key: key, value: value})
	return nil
}

// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
func (p *filePipeline) Get(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "get", key: key})
	return nil
}

// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
func (p *filePipeline) Exists(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "exists", key: key})
	return nil
}

// Execute 提交并执行管道中的所有操作，返回各操作的结果。
// 对齐 Python ShelveStore：pipeline get 返回原始 JSON 字节（不解包），pipeline set 是普通 set（非 exclusive）。
// 执行后管道被清空，可复用。
func (p *filePipeline) Execute(_ context.Context) ([]PipelineResult, error) {
	if len(p.ops) == 0 {
		p.ops = nil
		return []PipelineResult{}, nil
	}

	// 拷贝操作列表并清空，允许 Pipeline 复用
	ops := p.ops
	p.ops = nil

	results := make([]PipelineResult, 0, len(ops))

	err := p.store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(p.store.bucketName))

		for _, op := range ops {
			switch op.op {
			case "set":
				data, err := encodeFileEntry(op.value, 0)
				if err != nil {
					results = append(results, PipelineResult{Op: "set", Key: op.key, Err: err})
					continue
				}
				if err := b.Put([]byte(op.key), data); err != nil {
					results = append(results, PipelineResult{Op: "set", Key: op.key, Err: err})
					continue
				}
				results = append(results, PipelineResult{Op: "set", Key: op.key})

			case "get":
				raw := b.Get([]byte(op.key))
				if raw != nil {
					// 不解包，返回原始 JSON 字节的拷贝
					valCopy := make([]byte, len(raw))
					copy(valCopy, raw)
					results = append(results, PipelineResult{Op: "get", Key: op.key, Value: valCopy})
				} else {
					results = append(results, PipelineResult{Op: "get", Key: op.key, Value: nil})
				}

			case "exists":
				raw := b.Get([]byte(op.key))
				results = append(results, PipelineResult{Op: "exists", Key: op.key, Exists: raw != nil})
			}
		}
		return nil
	})

	return results, err
}
```

- [ ] **Step 2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...
```

预期：无错误输出

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/file.go && git commit -m "feat: add FileKVStore implementation based on bbolt"
```

---

### Task 3: 创建 file_test.go — 构造函数 + Set/Get/Delete/Exists 测试

**Files:**
- Create: `internal/agentcore/store/kv/file_test.go`

- [ ] **Step 1: 编写构造函数、Set/Get/Delete/Exists 基础测试**

```go
package kv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newTestFileKVStore 创建用于测试的 FileKVStore 实例，使用 t.TempDir() 提供临时目录。
func newTestFileKVStore(t *testing.T) *FileKVStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// ──── 构造函数测试 ────

// TestNewFileKVStore 验证构造函数创建非 nil 实例。
func TestNewFileKVStore(t *testing.T) {
	store := newTestFileKVStore(t)
	if store == nil {
		t.Fatal("NewFileKVStore 返回 nil")
	}
	if store.db == nil {
		t.Fatal("内部 db 未初始化")
	}
}

// TestNewFileKVStore_自动创建父目录 验证构造函数自动创建父目录。
func TestNewFileKVStore_自动创建父目录(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "dir", "test.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}
	defer store.Close()

	// 验证数据库文件存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("数据库文件未创建")
	}
}

// TestNewFileKVStore_路径无效 验证路径无效时返回错误。
func TestNewFileKVStore_路径无效(t *testing.T) {
	// 使用一个空字符串作为路径（无法创建目录）
	_, err := NewFileKVStore("")
	if err == nil {
		t.Error("空路径应返回错误")
	}
}

// ──── Set / Get 测试 ────

// TestFileKVStore_SetGet 基本 Set + Get 往返。
func TestFileKVStore_SetGet(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	err := store.Set(ctx, "key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestFileKVStore_Get_不存在 验证获取不存在的 key 返回 nil。
func TestFileKVStore_Get_不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if val != nil {
		t.Errorf("Get 返回 %v, 期望 nil", val)
	}
}

// TestFileKVStore_Set_覆盖 验证 Set 覆盖已有值。
func TestFileKVStore_Set_覆盖(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// ──── Exists 测试 ────

// TestFileKVStore_Exists_存在 验证 key 存在时 Exists 返回 true。
func TestFileKVStore_Exists_存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))

	exists, err := store.Exists(ctx, "key1")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if !exists {
		t.Error("Exists 返回 false, 期望 true")
	}
}

// TestFileKVStore_Exists_不存在 验证 key 不存在时 Exists 返回 false。
func TestFileKVStore_Exists_不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	exists, err := store.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if exists {
		t.Error("Exists 返回 true, 期望 false")
	}
}

// ──── Delete 测试 ────

// TestFileKVStore_Delete 验证删除后 Get 返回 nil。
func TestFileKVStore_Delete(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("删除后 Get 返回 %v, 期望 nil", val)
	}
}

// TestFileKVStore_Delete_不存在 验证删除不存在的 key 不报错。
func TestFileKVStore_Delete_不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 返回错误: %v", err)
	}
}

// ──── Get 解包 exclusive 值测试 ────

// TestFileKVStore_Get_解包Exclusive值 验证 Get 解包 exclusive-set 的值返回实际 []byte。
func TestFileKVStore_Get_解包Exclusive值(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("exclusive_value"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Fatal("ExclusiveSet 返回 false, 期望 true")
	}

	// Get 应解包，返回实际值而非 JSON 包装
	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "exclusive_value" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "exclusive_value")
	}

	// 验证返回的不是 JSON 字节
	var entry fileEntry
	if err := json.Unmarshal(val, &entry); err == nil && entry.Value != "" {
		t.Error("Get 返回了 JSON 包装结构，应返回解包后的实际值")
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test -v -run "TestNewFileKVStore|TestFileKVStore_SetGet|TestFileKVStore_Get_|TestFileKVStore_Set_|TestFileKVStore_Exists|TestFileKVStore_Delete" ./internal/agentcore/store/kv/...
```

预期：所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/file_test.go && git commit -m "test: add FileKVStore constructor, Set/Get/Delete/Exists tests"
```

---

### Task 4: 添加 ExclusiveSet 测试 + 过期语义测试

**Files:**
- Modify: `internal/agentcore/store/kv/file_test.go`

- [ ] **Step 1: 追加 ExclusiveSet 和过期语义测试**

```go
// ──── ExclusiveSet 测试 ────

// TestFileKVStore_ExclusiveSet_新key 验证新 key 设置成功。
func TestFileKVStore_ExclusiveSet_新key(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("ExclusiveSet 后 Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestFileKVStore_ExclusiveSet_已存在拒绝 验证 key 已存在且未过期时返回 false。
func TestFileKVStore_ExclusiveSet_已存在拒绝(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if ok {
		t.Error("ExclusiveSet 返回 true, 期望 false")
	}

	// 原值不变
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("拒绝后 Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestFileKVStore_ExclusiveSet_已过期允许覆盖 验证 key 已过期时允许覆盖。
func TestFileKVStore_ExclusiveSet_已过期允许覆盖(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 设置 1 秒后过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次 ExclusiveSet 应返回 true")
	}

	// 手动修改过期时间为过去，避免 sleep
	// 直接操作 bbolt 修改 ExpiryAt 为已过期时间戳
	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		entry.ExpiryAt = 1 // 1970-01-01，已过期
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte("key1"), data)
	})
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}

	// 已过期的 key 允许覆盖
	ok, err = store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("已过期 key 的 ExclusiveSet 返回 false, 期望 true")
	}

	val, _ := store.Get(ctx, "key1")
	if string(val) != "value2" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "value2")
	}
}

// TestFileKVStore_ExclusiveSet_带expiry 验证带过期时间的设置。
func TestFileKVStore_ExclusiveSet_带expiry(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 60)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 验证 key 存在
	exists, _ := store.Exists(ctx, "key1")
	if !exists {
		t.Error("设置后 Exists 返回 false, 期望 true")
	}

	// 验证内部过期时间戳已设置（通过直接读取 bbolt）
	err := store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if entry.ExpiryAt == 0 {
			t.Error("ExpiryAt 为 0, 期望非零")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("读取内部数据失败: %v", err)
	}
}

// ──── 过期语义测试（对齐 Python ShelveStore） ────

// TestFileKVStore_Get_过期key仍返回值 验证 Get 不过期检查（对齐 Python ShelveStore）。
func TestFileKVStore_Get_过期key仍返回值(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 设置带过期的 key，然后手动修改为已过期
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 10)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		entry.ExpiryAt = 1 // 1970-01-01，已过期
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte("key1"), data)
	})
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}

	// Get 应返回值，不过期检查（对齐 Python）
	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("过期 key Get 返回 %q, 期望 %q（对齐 Python 不过期检查）", string(val), "value1")
	}
}

// TestFileKVStore_Exists_过期key仍返回true 验证 Exists 不过期检查（对齐 Python ShelveStore）。
func TestFileKVStore_Exists_过期key仍返回true(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 设置带过期的 key，然后手动修改为已过期
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 10)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		entry.ExpiryAt = 1 // 1970-01-01，已过期
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte("key1"), data)
	})
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}

	// Exists 应返回 true，不过期检查（对齐 Python）
	exists, err := store.Exists(ctx, "key1")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if !exists {
		t.Error("过期 key Exists 返回 false, 期望 true（对齐 Python 不过期检查）")
	}
}
```

注意：`file_test.go` 需要在文件头部添加 `import "go.etcd.io/bbolt"`，因为过期语义测试直接操作 bbolt。但由于 `fileEntry` 是同包非导出类型，可以直接访问。不过测试中用到了 `bolt` 包，需在 import 中添加：

```go
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)
```

- [ ] **Step 2: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test -v -run "TestFileKVStore_ExclusiveSet|TestFileKVStore_Get_过期|TestFileKVStore_Exists_过期" ./internal/agentcore/store/kv/...
```

预期：所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/file_test.go && git commit -m "test: add FileKVStore ExclusiveSet and expiry semantics tests"
```

---

### Task 5: 添加 GetByPrefix / DeleteByPrefix / MGet / BatchDelete 测试

**Files:**
- Modify: `internal/agentcore/store/kv/file_test.go`

- [ ] **Step 1: 追加前缀操作和批量操作测试**

```go
// ──── GetByPrefix 测试 ────

// TestFileKVStore_GetByPrefix_正常 验证按前缀获取键值对。
func TestFileKVStore_GetByPrefix_正常(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	result, err := store.GetByPrefix(ctx, "user:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 2 条", len(result))
	}
	// 注意：GetByPrefix 不解包，返回原始 JSON 字节
	// 验证 key 存在即可
	if _, ok := result["user:1"]; !ok {
		t.Error("结果中缺少 user:1")
	}
	if _, ok := result["user:2"]; !ok {
		t.Error("结果中缺少 user:2")
	}
}

// TestFileKVStore_GetByPrefix_无匹配 验证无匹配前缀返回空 map。
func TestFileKVStore_GetByPrefix_无匹配(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	result, err := store.GetByPrefix(ctx, "item:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 0 条", len(result))
	}
}

// TestFileKVStore_GetByPrefix_不解包 验证 GetByPrefix 返回原始 JSON 字节（不解包）。
func TestFileKVStore_GetByPrefix_不解包(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	result, _ := store.GetByPrefix(ctx, "user:")
	raw := result["user:1"]

	// 应为有效 JSON（fileEntry 格式）
	var entry fileEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("GetByPrefix 返回值不是有效 JSON: %v", err)
	}
	if entry.Value == "" {
		t.Error("fileEntry.Value 为空")
	}
	// 不应等于原始值 "alice"（应是 base64 编码的 JSON 包装）
	if entry.Value == "alice" {
		t.Error("GetByPrefix 应返回原始 JSON 字节（不解包），但看起来解包了")
	}
}

// ──── DeleteByPrefix 测试 ────

// TestFileKVStore_DeleteByPrefix_一次性 验证 batchSize=0 时一次性删除。
func TestFileKVStore_DeleteByPrefix_一次性(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	err := store.DeleteByPrefix(ctx, "user:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	exists, _ := store.Exists(ctx, "user:1")
	if exists {
		t.Error("删除后 user:1 仍存在")
	}
	exists, _ = store.Exists(ctx, "item:1")
	if !exists {
		t.Error("item:1 不应被删除")
	}
}

// TestFileKVStore_DeleteByPrefix_分批 验证 batchSize>0 时分批删除。
func TestFileKVStore_DeleteByPrefix_分批(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "user:3", []byte("charlie"))

	err := store.DeleteByPrefix(ctx, "user:", 2)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	// 所有 user: 前缀的 key 都应被删除
	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 user: 前缀的 key", len(result))
	}
}

// ──── MGet 测试 ────

// TestFileKVStore_MGet_正常 验证批量获取多个 key。
func TestFileKVStore_MGet_正常(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))

	values, err := store.MGet(ctx, []string{"k1", "k2"})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	// MGet 不解包，返回原始 JSON 字节
	if values[0] == nil {
		t.Error("values[0] 为 nil")
	}
	if values[1] == nil {
		t.Error("values[1] 为 nil")
	}
}

// TestFileKVStore_MGet_部分不存在 验证部分 key 不存在时对应位置为 nil。
func TestFileKVStore_MGet_部分不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	values, _ := store.MGet(ctx, []string{"k1", "k2"})
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	if values[0] == nil {
		t.Error("k1 应存在")
	}
	if values[1] != nil {
		t.Errorf("k2 不存在，应为 nil, 实际为 %v", values[1])
	}
}

// TestFileKVStore_MGet_不解包 验证 MGet 返回原始 JSON 字节（不解包）。
func TestFileKVStore_MGet_不解包(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	values, _ := store.MGet(ctx, []string{"k1"})
	raw := values[0]

	// 应为有效 JSON（fileEntry 格式）
	var entry fileEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("MGet 返回值不是有效 JSON: %v", err)
	}
	if entry.Value == "" {
		t.Error("fileEntry.Value 为空")
	}
}

// ──── BatchDelete 测试 ────

// TestFileKVStore_BatchDelete_正常 验证批量删除并返回删除数量。
func TestFileKVStore_BatchDelete_正常(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))
	_ = store.Set(ctx, "k3", []byte("v3"))

	deleted, err := store.BatchDelete(ctx, []string{"k1", "k3"}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 2 {
		t.Errorf("BatchDelete 返回 %d, 期望 2", deleted)
	}

	exists, _ := store.Exists(ctx, "k1")
	if exists {
		t.Error("k1 删除后仍存在")
	}
	exists, _ = store.Exists(ctx, "k2")
	if !exists {
		t.Error("k2 不应被删除")
	}
}

// TestFileKVStore_BatchDelete_空列表 验证空列表返回 0。
func TestFileKVStore_BatchDelete_空列表(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 0 {
		t.Errorf("BatchDelete 返回 %d, 期望 0", deleted)
	}
}

// TestFileKVStore_BatchDelete_分批 验证分批删除。
func TestFileKVStore_BatchDelete_分批(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))
	_ = store.Set(ctx, "k3", []byte("v3"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2", "k3"}, 2)
	if deleted != 3 {
		t.Errorf("BatchDelete 返回 %d, 期望 3", deleted)
	}

	result, _ := store.GetByPrefix(ctx, "k")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 key", len(result))
	}
}

// TestFileKVStore_BatchDelete_部分不存在 验证只删除存在的 key。
func TestFileKVStore_BatchDelete_部分不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2"}, 0)
	if deleted != 1 {
		t.Errorf("BatchDelete 返回 %d, 期望 1", deleted)
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test -v -run "TestFileKVStore_GetByPrefix|TestFileKVStore_DeleteByPrefix|TestFileKVStore_MGet|TestFileKVStore_BatchDelete" ./internal/agentcore/store/kv/...
```

预期：所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/file_test.go && git commit -m "test: add FileKVStore prefix/batch operation tests"
```

---

### Task 6: 添加 Pipeline 测试 + 并发安全测试 + 持久化测试 + 接口满足验证

**Files:**
- Modify: `internal/agentcore/store/kv/file_test.go`

- [ ] **Step 1: 追加 Pipeline、并发安全、持久化、接口满足测试**

```go
// ──── Pipeline 测试 ────

// TestFileKVStore_Pipeline_混合操作 验证 Pipeline 混合 set+get+exists 操作。
func TestFileKVStore_Pipeline_混合操作(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 先设置一个 key
	_ = store.Set(ctx, "existing", []byte("old_value"))

	// 创建 Pipeline 并执行混合操作
	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "new_key", []byte("new_value"))
	_ = pipe.Get(ctx, "existing")
	_ = pipe.Exists(ctx, "nonexistent")

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Pipeline 返回 %d 条结果, 期望 3 条", len(results))
	}

	// 验证 Set 结果
	if results[0].Op != "set" || results[0].Key != "new_key" {
		t.Errorf("results[0] = {Op: %q, Key: %q}, 期望 {Op: %q, Key: %q}", results[0].Op, results[0].Key, "set", "new_key")
	}

	// 验证 Get 结果（Pipeline Get 不解包，返回原始 JSON 字节）
	if results[1].Op != "get" {
		t.Errorf("results[1].Op = %q, 期望 %q", results[1].Op, "get")
	}
	if results[1].Value == nil {
		t.Error("Get existing key 返回 nil")
	} else {
		// 验证是 JSON 格式（不解包）
		var entry fileEntry
		if err := json.Unmarshal(results[1].Value, &entry); err != nil {
			t.Errorf("Pipeline Get 返回值不是有效 JSON: %v", err)
		}
	}

	// 验证 Exists 结果
	if results[2].Op != "exists" || results[2].Exists {
		t.Errorf("results[2] = {Op: %q, Exists: %v}, 期望 {Op: %q, Exists: false}", results[2].Op, results[2].Exists, "exists")
	}

	// 验证 Pipeline Set 实际写入了 store
	val, _ := store.Get(ctx, "new_key")
	if string(val) != "new_value" {
		t.Errorf("Pipeline Set 后 Get 返回 %q, 期望 %q", string(val), "new_value")
	}
}

// TestFileKVStore_Pipeline_复用 验证 Execute 后 Pipeline 可复用。
func TestFileKVStore_Pipeline_复用(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	pipe := store.Pipeline(ctx)

	// 第一次使用
	_ = pipe.Set(ctx, "k1", []byte("v1"))
	results1, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第一次 Execute 返回错误: %v", err)
	}
	if len(results1) != 1 {
		t.Errorf("第一次 Execute 返回 %d 条结果, 期望 1 条", len(results1))
	}

	// 第二次使用（复用同一个 Pipeline）
	_ = pipe.Set(ctx, "k2", []byte("v2"))
	_ = pipe.Get(ctx, "k1")
	results2, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第二次 Execute 返回错误: %v", err)
	}
	if len(results2) != 2 {
		t.Errorf("第二次 Execute 返回 %d 条结果, 期望 2 条", len(results2))
	}

	// 验证第二次的 Get 结果（不解包）
	if results2[1].Op != "get" {
		t.Errorf("复用 Pipeline Get Op = %q, 期望 %q", results2[1].Op, "get")
	}
	if results2[1].Value == nil {
		t.Error("Get k1 返回 nil")
	}
}

// TestFileKVStore_Pipeline_空操作 验证空 Pipeline Execute 返回空切片。
func TestFileKVStore_Pipeline_空操作(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("空 Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("空 Pipeline Execute 返回 %d 条结果, 期望 0 条", len(results))
	}
}

// ──── 并发安全测试 ────

// TestFileKVStore_并发安全 验证多 goroutine 并发读写无 race。
func TestFileKVStore_并发安全(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "concurrent.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	const goroutines = 20
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 4) // 4 组 goroutine: Set, Get, Delete, ExclusiveSet

	// 并发 Set
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_ = store.Set(ctx, key, []byte("value"))
			}
		}(i)
	}

	// 并发 Get
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_, _ = store.Get(ctx, key)
			}
		}(i)
	}

	// 并发 Delete
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_ = store.Delete(ctx, key)
			}
		}(i)
	}

	// 并发 ExclusiveSet
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("exclusive:%d:%d", id, j)
				_, _ = store.ExclusiveSet(ctx, key, []byte("value"), 0)
			}
		}(i)
	}

	wg.Wait()
}

// ──── 持久化测试 ────

// TestFileKVStore_数据持久化 验证写入后关闭再打开，数据仍在。
func TestFileKVStore_数据持久化(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")
	ctx := context.Background()

	// 第一次打开，写入数据
	store1, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("第一次 NewFileKVStore 返回错误: %v", err)
	}
	_ = store1.Set(ctx, "key1", []byte("value1"))
	_ = store1.Set(ctx, "key2", []byte("value2"))
	if err := store1.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}

	// 第二次打开，验证数据仍在
	store2, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("第二次 NewFileKVStore 返回错误: %v", err)
	}
	defer store2.Close()

	val, err := store2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("持久化后 Get 返回 %q, 期望 %q", string(val), "value1")
	}

	val, err = store2.Get(ctx, "key2")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value2" {
		t.Errorf("持久化后 Get 返回 %q, 期望 %q", string(val), "value2")
	}
}

// ──── Close 后操作测试 ────

// TestFileKVStore_Close后操作 验证 Close 后操作返回 error。
func TestFileKVStore_Close后操作(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "closed.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}

	// 写入数据后关闭
	_ = store.Set(context.Background(), "key1", []byte("value1"))
	if err := store.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}

	// Close 后 Get 应返回 error
	_, err = store.Get(context.Background(), "key1")
	if err == nil {
		t.Error("Close 后 Get 应返回 error")
	}
}

// ──── 接口满足验证 ────

// TestFileKVStore_接口满足 验证 FileKVStore 满足 BaseKVStore 接口。
func TestFileKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*FileKVStore)(nil)
}
```

注意：需要在 file_test.go 的 import 中添加 `"fmt"` 和 `"sync"`（并发安全测试使用）。

- [ ] **Step 2: 运行全部 FileKVStore 测试**

```bash
cd /home/opensource/uap-claw-go && go test -v -run "TestFileKVStore" ./internal/agentcore/store/kv/...
```

预期：所有测试 PASS

- [ ] **Step 3: 运行 race 检测**

```bash
cd /home/opensource/uap-claw-go && go test -race -run "TestFileKVStore_并发安全" ./internal/agentcore/store/kv/...
```

预期：PASS，无 race detected

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/file_test.go && git commit -m "test: add FileKVStore Pipeline, concurrency, persistence, and close tests"
```

---

### Task 7: 更新 doc.go 包文档

**Files:**
- Modify: `internal/agentcore/store/kv/doc.go`

- [ ] **Step 1: 更新 doc.go，添加 file.go 到文件目录和包概述**

```go
// Package kv 提供键值存储的抽象接口定义和多种后端实现。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// InMemoryKVStore 提供基于内存的并发安全实现，支持惰性过期检查。
// FileKVStore 提供基于 bbolt 的文件持久化实现，对应 Python ShelveStore，
// 严格复刻其语义（包括已知的值解包不一致和过期语义不一致）。
// 其他后端实现（数据库、Redis 等）将在后续版本中提供。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	├── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//	└── file.go          # FileKVStore 文件持久化实现 + filePipeline
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
//
//	InMemoryKVStore 对应: openjiuwen/core/foundation/store/kv/in_memory_kv_store.py
//	FileKVStore 对应:     openjiuwen/core/foundation/store/kv/shelve_store.py
package kv
```

- [ ] **Step 2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...
```

预期：无错误输出

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/doc.go && git commit -m "docs: update kv/doc.go with FileKVStore entry"
```

---

### Task 8: 运行全量测试 + 覆盖率检查

**Files:**
- 无修改

- [ ] **Step 1: 运行全量 kv 包测试**

```bash
cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/kv/...
```

预期：所有测试 PASS

- [ ] **Step 2: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/store/kv/... && go tool cover -func=coverage.out | grep file.go
```

预期：file.go 覆盖率 ≥ 85%

- [ ] **Step 3: 运行 race 检测**

```bash
cd /home/opensource/uap-claw-go && go test -race ./internal/agentcore/store/kv/...
```

预期：PASS，无 race detected

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将步骤 4.3 状态从 ☐ 改为 ✅**

找到 IMPLEMENTATION_PLAN.md 中 4.3 对应的行，将 `☐` 改为 `✅`。

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: mark step 4.3 FileKVStore as completed"
```
