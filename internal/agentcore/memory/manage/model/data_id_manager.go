package model

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DataIdManager 数据 ID 管理器，生成唯一 ID。
//
// ID 生成算法：6字节时间戳 + 3字节随机数 + 3字节用户哈希 = 12字节 = 24字符hex串。
// 与 Python 实现对齐，确保相同用户在相近时间生成的 ID 不冲突。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/data_id_manager.py (DataIdManager)
type DataIdManager struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDataIdManager 创建 DataIdManager 实例。
func NewDataIdManager() *DataIdManager {
	return &DataIdManager{}
}

// GenerateNextID 生成下一个唯一 ID。
// 算法：6字节毫秒时间戳 + 3字节加密安全随机数 + 3字节用户ID哈希。
//
// 对应 Python: DataIdManager.generate_next_id(user_id)
func (m *DataIdManager) GenerateNextID(userID string) string {
	// 6字节时间戳：毫秒级，取低48位
	t := uint64(time.Now().UnixMilli()) & 0xFFFFFFFFFFFF
	var tBytes [8]byte
	binary.BigEndian.PutUint64(tBytes[:], t)
	// 取后6字节
	timePart := tBytes[2:]

	// 3字节随机数
	var randBytes [3]byte
	_, _ = rand.Read(randBytes[:])

	// 3字节用户哈希
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID))
	hashVal := h.Sum32() & 0xFFFFFF
	var hBytes [4]byte
	binary.BigEndian.PutUint32(hBytes[:], hashVal)
	// 取后3字节
	hashPart := hBytes[1:]

	// 拼接：6+3+3 = 12字节
	var raw [12]byte
	copy(raw[0:6], timePart)
	copy(raw[6:9], randBytes[:])
	copy(raw[9:12], hashPart)

	return fmt.Sprintf("%x", raw)
}
