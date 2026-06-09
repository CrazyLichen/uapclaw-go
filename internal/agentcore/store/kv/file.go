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
		_ = db.Close()
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
// expiry 为过期秒数，0 表示不过期。
func (p *filePipeline) Set(_ context.Context, key string, value []byte, expiry int) error {
	p.ops = append(p.ops, operation{op: "set", key: key, value: value, expiry: expiry})
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
				var expiryAt int64
				if op.expiry > 0 {
					expiryAt = time.Now().Unix() + int64(op.expiry)
				}
				data, err := encodeFileEntry(op.value, expiryAt)
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
