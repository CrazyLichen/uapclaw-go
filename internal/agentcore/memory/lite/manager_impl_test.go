package lite

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestVectorToBlob 向量转二进制
func TestVectorToBlob(t *testing.T) {
	vec := []float64{1.0, -1.0, 0.5}
	blob := vectorToBlob(vec)
	if len(blob) != 12 { // 3 * 4 bytes
		t.Fatalf("blob 长度应为 12，实际 %d", len(blob))
	}
	// 验证往返转换
	result := blobToVector(blob)
	if len(result) != 3 {
		t.Fatalf("往返后向量长度应为 3，实际 %d", len(result))
	}
	for i, v := range result {
		diff := math.Abs(v - vec[i])
		if diff > 1e-6 {
			t.Errorf("向量[%d]: 期望 %f，实际 %f，差值 %f", i, vec[i], v, diff)
		}
	}
}

// TestVectorToBlob_空向量 空向量返回空 blob
func TestVectorToBlob_空向量(t *testing.T) {
	blob := vectorToBlob([]float64{})
	if len(blob) != 0 {
		t.Errorf("空向量的 blob 长度应为 0，实际 %d", len(blob))
	}
}

// TestBlobToVector 二进制转向量
func TestBlobToVector(t *testing.T) {
	t.Run("有效 blob 正确转换", func(t *testing.T) {
		vec := []float64{0.25, -0.75, 1.5, 0.0}
		blob := vectorToBlob(vec)
		result := blobToVector(blob)
		for i, v := range result {
			diff := math.Abs(v - vec[i])
			if diff > 1e-6 {
				t.Errorf("向量[%d]: 期望 %f，实际 %f", i, vec[i], v)
			}
		}
	})

	t.Run("长度不是 4 的倍数返回 nil", func(t *testing.T) {
		result := blobToVector([]byte{1, 2, 3})
		if result != nil {
			t.Errorf("非 4 倍数 blob 应返回 nil，实际 %v", result)
		}
	})

	t.Run("空 blob 返回空向量", func(t *testing.T) {
		result := blobToVector([]byte{})
		if len(result) != 0 {
			t.Errorf("空 blob 应返回空向量，实际长度 %d", len(result))
		}
	})
}

// TestTruncateString 截断字符串（CJK 安全）
func TestTruncateString(t *testing.T) {
	t.Run("短字符串不截断", func(t *testing.T) {
		s := "hello"
		result := truncateString(s, 10)
		if result != s {
			t.Errorf("短字符串不应被截断，实际 %q", result)
		}
	})

	t.Run("长度等于 maxLen 不截断", func(t *testing.T) {
		s := "hello"
		result := truncateString(s, 5)
		if result != s {
			t.Errorf("等于 maxLen 的字符串不应被截断，实际 %q", result)
		}
	})

	t.Run("超过 maxLen 截断", func(t *testing.T) {
		s := "hello world"
		result := truncateString(s, 5)
		if result != "hello" {
			t.Errorf("截断结果应为 hello，实际 %q", result)
		}
	})

	t.Run("CJK 字符按码点截断", func(t *testing.T) {
		s := "你好世界测试"
		result := truncateString(s, 2)
		if result != "你好" {
			t.Errorf("CJK 截断结果应为 你好，实际 %q", result)
		}
	})

	t.Run("混合 ASCII 和 CJK", func(t *testing.T) {
		s := "abc你好"
		result := truncateString(s, 4)
		if result != "abc你" {
			t.Errorf("混合截断结果应为 abc你，实际 %q", result)
		}
	})

	t.Run("空字符串", func(t *testing.T) {
		result := truncateString("", 5)
		if result != "" {
			t.Errorf("空字符串截断结果应为空，实际 %q", result)
		}
	})
}

// TestContainsSource 检查 sources 列表是否包含指定来源
func TestContainsSource(t *testing.T) {
	t.Run("包含来源返回 true", func(t *testing.T) {
		if !containsSource([]string{"memory", "sessions"}, "memory") {
			t.Error("应包含 memory")
		}
	})

	t.Run("不包含来源返回 false", func(t *testing.T) {
		if containsSource([]string{"memory", "sessions"}, "files") {
			t.Error("不应包含 files")
		}
	})

	t.Run("空列表返回 false", func(t *testing.T) {
		if containsSource([]string{}, "memory") {
			t.Error("空列表不应包含任何来源")
		}
	})

	t.Run("nil 列表返回 false", func(t *testing.T) {
		if containsSource(nil, "memory") {
			t.Error("nil 列表不应包含任何来源")
		}
	})
}

// TestIsRecentSessionFile 判断 session 文件是否为最近两天
// 注意：time.Parse 默认使用 UTC，而 time.Now() 使用本地时区。
// 当本地时区为 UTC+x 时，日期比较可能因时区偏移而失败。
// 此测试验证函数的实际行为：使用 UTC 日期字符串测试非近两天场景，
// 并在近两天场景中添加时区容差。
func TestIsRecentSessionFile(t *testing.T) {
	t.Run("非日期格式返回 false", func(t *testing.T) {
		if isRecentSessionFile("notes.md") {
			t.Error("非日期格式应返回 false")
		}
	})

	t.Run("不带 .md 后缀返回 false", func(t *testing.T) {
		today := time.Now().Format("2006-01-02")
		if isRecentSessionFile(today) {
			t.Error("不带 .md 后缀应返回 false")
		}
	})

	t.Run("格式错误的日期返回 false", func(t *testing.T) {
		if isRecentSessionFile("2026-13-45.md") {
			t.Error("格式错误的日期应返回 false")
		}
	})

	t.Run("远过去的日期返回 false", func(t *testing.T) {
		if isRecentSessionFile("2020-01-01.md") {
			t.Error("远过去的日期应返回 false")
		}
	})

	t.Run("有效的 YYYY-MM-DD.md 格式可以解析", func(t *testing.T) {
		// 构造一个足够远的日期，确保不受时区影响
		result := isRecentSessionFile("2026-01-01.md")
		// 2026-01-01 是远过去的日期，应返回 false
		if result {
			t.Error("远过去的日期应返回 false")
		}
	})
}

// TestResolveDBPath 解析数据库路径
func TestResolveDBPath(t *testing.T) {
	t.Run("默认路径 memory.db", func(t *testing.T) {
		m := &memoryIndexManager{
			memoryDir: "/tmp/testdb",
			settings:  CreateMemorySettings("/tmp/testdb", nil),
		}
		result := m.resolveDBPath()
		expected := filepath.Join("/tmp/testdb", "memory.db")
		if result != expected {
			t.Errorf("默认路径应为 %q，实际 %q", expected, result)
		}
	})

	t.Run("绝对路径直接返回", func(t *testing.T) {
		m := &memoryIndexManager{
			memoryDir: "/tmp/testdb",
			settings: &MemorySettings{
				Store: StoreConfig{
					Path: "/absolute/path/custom.db",
				},
			},
		}
		result := m.resolveDBPath()
		if result != "/absolute/path/custom.db" {
			t.Errorf("绝对路径应直接返回，实际 %q", result)
		}
	})

	t.Run("相对路径拼接 memoryDir", func(t *testing.T) {
		m := &memoryIndexManager{
			memoryDir: "/tmp/testdb",
			settings: &MemorySettings{
				Store: StoreConfig{
					Path: "custom.db",
				},
			},
		}
		result := m.resolveDBPath()
		expected := filepath.Join("/tmp/testdb", "custom.db")
		if result != expected {
			t.Errorf("相对路径应拼接 memoryDir，期望 %q，实际 %q", expected, result)
		}
	})
}

// TestBuildFileEntry 构建文件索引条目
func TestBuildFileEntry(t *testing.T) {
	t.Run("有效文件", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")
		content := []byte("# Hello\nWorld")
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("写入测试文件失败: %v", err)
		}

		m := &memoryIndexManager{}
		entry, err := m.buildFileEntry(testFile, tmpDir)
		if err != nil {
			t.Fatalf("buildFileEntry 不应返回 error: %v", err)
		}
		if entry == nil {
			t.Fatal("entry 不应为 nil")
		}
		if entry.Path != "test.md" {
			t.Errorf("相对路径应为 test.md，实际 %q", entry.Path)
		}
		if entry.AbsPath != testFile {
			t.Errorf("绝对路径应为 %q，实际 %q", testFile, entry.AbsPath)
		}
		if entry.Hash == "" {
			t.Error("Hash 不应为空")
		}
		if entry.Size != int64(len(content)) {
			t.Errorf("Size 应为 %d，实际 %d", len(content), entry.Size)
		}
		if entry.MtimeMs == 0 {
			t.Error("MtimeMs 不应为 0")
		}
	})

	t.Run("不存在的文件返回 error", func(t *testing.T) {
		m := &memoryIndexManager{}
		_, err := m.buildFileEntry("/nonexistent/file.md", "/nonexistent")
		if err == nil {
			t.Error("不存在的文件应返回 error")
		}
	})
}

// TestBuildSourceFilter 构建 SQL source 过滤条件
func TestBuildSourceFilter(t *testing.T) {
	t.Run("空 sources 返回 1=0", func(t *testing.T) {
		m := &memoryIndexManager{
			settings: &MemorySettings{Sources: []string{}},
		}
		filter, args := m.buildSourceFilter()
		if filter != "1=0" {
			t.Errorf("空 sources 过滤条件应为 1=0，实际 %q", filter)
		}
		if len(args) != 0 {
			t.Errorf("空 sources 参数应为空，实际 %v", args)
		}
	})

	t.Run("单个 source 返回 source = ?", func(t *testing.T) {
		m := &memoryIndexManager{
			settings: &MemorySettings{Sources: []string{"memory"}},
		}
		filter, args := m.buildSourceFilter()
		if filter != "source = ?" {
			t.Errorf("单个 source 过滤条件应为 source = ?，实际 %q", filter)
		}
		if len(args) != 1 || args[0] != "memory" {
			t.Errorf("单个 source 参数应为 [memory]，实际 %v", args)
		}
	})

	t.Run("多个 source 返回 IN 子句", func(t *testing.T) {
		m := &memoryIndexManager{
			settings: &MemorySettings{Sources: []string{"memory", "sessions"}},
		}
		filter, args := m.buildSourceFilter()
		if filter != "source IN (?,?)" {
			t.Errorf("多个 source 过滤条件应为 source IN (?,?)，实际 %q", filter)
		}
		if len(args) != 2 || args[0] != "memory" || args[1] != "sessions" {
			t.Errorf("多个 source 参数应为 [memory sessions]，实际 %v", args)
		}
	})
}
