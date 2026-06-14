package graph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetUUID 生成32位十六进制UUID（无连字符）
func GetUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GetCurrentUTCTimestamp 获取当前UTC时间戳（秒级整数）
func GetCurrentUTCTimestamp() int64 {
	return time.Now().UTC().Unix()
}

// Batched 将切片分批，每批n个元素
func Batched[T any](items []T, n int) [][]T {
	if n <= 0 {
		return nil
	}
	var batches [][]T
	for i := 0; i < len(items); i += n {
		end := i + n
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

// FormatTimestamp 将时间戳格式化为可读字符串
func FormatTimestamp(t int64, tz *time.Location, layout string) string {
	if tz == nil {
		tz = time.UTC
	}
	return time.Unix(t, 0).In(tz).Format(layout)
}

// FormatTimestampISO 将时间戳格式化为ISO 8601字符串
func FormatTimestampISO(t int64, tz *time.Location) string {
	return FormatTimestamp(t, tz, time.RFC3339)
}

// ISO2Timestamp 将ISO 8601字符串转换为时间戳和时区偏移
func ISO2Timestamp(isoStr string) (timestamp int64, offset int8, err error) {
	t, err := time.Parse(time.RFC3339, isoStr)
	if err != nil {
		return 0, 0, fmt.Errorf("解析ISO时间字符串失败: %w", err)
	}
	_, tzOffset := t.Zone()
	return t.Unix(), storeTZOffset(tzOffset), nil
}

// LoadStoredTimeFromDB 从数据库存储的时间戳和偏移重建时间
func LoadStoredTimeFromDB(timestamp int64, offset int8) (*time.Time, error) {
	tz := loadTZOffset(offset)
	t := time.Unix(timestamp, 0).In(tz)
	return &t, nil
}

// EnsureUniqueUUIDs 去重UUID：查询集合中已存在的UUID，对重复的UUID重新生成，循环直到全部唯一。
// 对齐 Python 行为：检测到重复时重新生成 UUID 而非仅过滤。
func EnsureUniqueUUIDs(ctx context.Context, store BaseGraphStore, ids []string, collection string, skip bool) ([]string, error) {
	if skip || len(ids) == 0 {
		return ids, nil
	}

	// 循环去重：查询已存在的 UUID，对重复的重新生成，直到全部唯一
	result := make([]string, len(ids))
	copy(result, ids)

	for {
		existing, err := store.Query(ctx, collection, WithIDs(stringsToAny(result)...), WithOutputFields("uuid"))
		if err != nil {
			return nil, err
		}
		existingSet := make(map[string]struct{}, len(existing))
		for _, row := range existing {
			if uuid, ok := row["uuid"].(string); ok {
				existingSet[uuid] = struct{}{}
			}
		}

		// 检查是否有重复，如有则重新生成
		hasDuplicate := false
		for i, id := range result {
			if _, found := existingSet[id]; found {
				result[i] = GetUUID()
				hasDuplicate = true
			}
		}

		if !hasDuplicate {
			break
		}
	}

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// storeTZOffset 解析时区秒偏移为15分钟单位偏移
func storeTZOffset(tzOffsetSeconds int) int8 {
	return int8(tzOffsetSeconds / (15 * 60))
}

// loadTZOffset 从15分钟单位偏移重建时区
func loadTZOffset(offset int8) *time.Location {
	seconds := int(offset) * 15 * 60
	return time.FixedZone("", seconds)
}

// stringsToAny 将 []string 转为 []any
func stringsToAny(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}
