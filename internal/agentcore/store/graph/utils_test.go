package graph

import (
	"testing"
	"time"
)

// TestGetUUID_格式 测试 UUID 格式
func TestGetUUID_格式(t *testing.T) {
	uuid := GetUUID()
	if len(uuid) != 32 {
		t.Errorf("UUID 长度应为 32，实际为 %d", len(uuid))
	}
	for _, c := range uuid {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("UUID 应为十六进制，发现字符 %c", c)
			break
		}
	}
}

// TestGetUUID_唯一性 测试 UUID 唯一性
func TestGetUUID_唯一性(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		uuid := GetUUID()
		if _, ok := seen[uuid]; ok {
			t.Fatalf("生成重复 UUID: %s", uuid)
		}
		seen[uuid] = struct{}{}
	}
}

// TestGetCurrentUTCTimestamp 测试时间戳
func TestGetCurrentUTCTimestamp(t *testing.T) {
	ts := GetCurrentUTCTimestamp()
	if ts <= 0 {
		t.Error("时间戳应大于 0")
	}
	// 应为秒级（10位数字左右）
	now := time.Now().UTC().Unix()
	if ts < now-1 || ts > now+1 {
		t.Errorf("时间戳与当前时间相差过大: got %d, expect around %d", ts, now)
	}
}

// TestBatched_正常分批 测试 Batched 正常分批
func TestBatched_正常分批(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	batches := Batched(items, 2)
	if len(batches) != 3 {
		t.Fatalf("应有 3 批，实际为 %d", len(batches))
	}
	if len(batches[0]) != 2 || len(batches[1]) != 2 || len(batches[2]) != 1 {
		t.Errorf("批次大小不正确: %v", batches)
	}
}

// TestBatched_空切片 测试 Batched 空切片
func TestBatched_空切片(t *testing.T) {
	batches := Batched([]int{}, 3)
	if len(batches) != 0 {
		t.Errorf("空切片应返回空批次，实际为 %d", len(batches))
	}
}

// TestBatched_无效批次大小 测试 Batched 无效批次大小
func TestBatched_无效批次大小(t *testing.T) {
	batches := Batched([]int{1, 2, 3}, 0)
	if batches != nil {
		t.Errorf("n<=0 应返回 nil，实际为 %v", batches)
	}
}

// TestFormatTimestampISO 测试 ISO 时间格式化
func TestFormatTimestampISO(t *testing.T) {
	ts := int64(1700000000)
	result := FormatTimestampISO(ts, time.UTC)
	if len(result) == 0 {
		t.Error("ISO 格式化结果不应为空")
	}
	// 应包含 T 分隔符
	found := false
	for _, c := range result {
		if c == 'T' {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ISO 格式应包含 T，实际为 %s", result)
	}
}

// TestISO2Timestamp_正常 测试 ISO 时间戳解析
func TestISO2Timestamp_正常(t *testing.T) {
	ts, offset, err := ISO2Timestamp("2023-11-14T22:13:20+08:00")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ts <= 0 {
		t.Error("时间戳应大于 0")
	}
	// +08:00 = 8*4=32 个15分钟单位
	if offset != 32 {
		t.Errorf("偏移量应为 32，实际为 %d", offset)
	}
}

// TestISO2Timestamp_无效格式 测试无效格式
func TestISO2Timestamp_无效格式(t *testing.T) {
	_, _, err := ISO2Timestamp("not-a-date")
	if err == nil {
		t.Error("无效格式应返回错误")
	}
}

// TestLoadStoredTimeFromDB 测试从数据库存储重建时间
func TestLoadStoredTimeFromDB(t *testing.T) {
	ts := int64(1700000000)
	offset := int8(32) // +08:00
	tm, err := LoadStoredTimeFromDB(ts, offset)
	if err != nil {
		t.Fatalf("重建时间失败: %v", err)
	}
	if tm == nil {
		t.Fatal("重建时间不应为 nil")
	}
}

// TestStoreTZOffset 测试时区偏移转换
func TestStoreTZOffset(t *testing.T) {
	// +08:00 = 28800秒 = 32 * 15 * 60
	result := storeTZOffset(28800)
	if result != 32 {
		t.Errorf("+08:00 偏移应为 32，实际为 %d", result)
	}
	// UTC = 0
	result = storeTZOffset(0)
	if result != 0 {
		t.Errorf("UTC 偏移应为 0，实际为 %d", result)
	}
}

// TestLoadTZOffset 测试从偏移重建时区
func TestLoadTZOffset(t *testing.T) {
	loc := loadTZOffset(32)
	if loc == nil {
		t.Fatal("时区不应为 nil")
	}
	_, offset := time.Now().In(loc).Zone()
	if offset != 28800 {
		t.Errorf("时区偏移应为 28800 秒，实际为 %d", offset)
	}
}

// TestStringsToAny 测试字符串切片转any切片
func TestStringsToAny(t *testing.T) {
	ss := []string{"a", "b", "c"}
	result := stringsToAny(ss)
	if len(result) != 3 {
		t.Fatalf("长度应为 3，实际为 %d", len(result))
	}
	for i, v := range result {
		if v.(string) != ss[i] {
			t.Errorf("第 %d 个元素不匹配: got %v, want %s", i, v, ss[i])
		}
	}
}

// TestStringsToAny_空切片 测试空切片
func TestStringsToAny_空切片(t *testing.T) {
	result := stringsToAny(nil)
	if len(result) != 0 {
		t.Errorf("空切片应返回空，实际为 %v", result)
	}
}
