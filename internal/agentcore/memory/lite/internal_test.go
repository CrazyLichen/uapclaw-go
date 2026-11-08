package lite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// equalTokens 比较两个 token 切片是否完全一致（nil 与空切片视为相等）。
func equalTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBuildFTSQuery 测试 FTS5 查询构建
func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantEmpty bool
	}{
		{"空字符串", "", true},
		{"空格", "   ", true},
		{"简单词", "hello", false},
		{"多个词", "hello world", false},
		{"中文", "你好世界", false}, // Python: \w+ Unicode 模式匹配中文字符
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFTSQuery(tt.query)
			if (got == "") != tt.wantEmpty {
				t.Errorf("BuildFTSQuery(%q) = %q, wantEmpty=%v", tt.query, got, tt.wantEmpty)
			}
		})
	}
}

// TestBuildFTSQuery_格式验证 测试 FTS5 查询格式
//
// 已对齐上游：正则切分 + OR 连接（多 token 用 AND 会让长查询几乎必然落空）。
func TestBuildFTSQuery_格式验证(t *testing.T) {
	got := BuildFTSQuery("hello world")
	if got != `"hello" OR "world"` {
		t.Errorf("BuildFTSQuery('hello world') = %q, 期望 %q", got, `"hello" OR "world"`)
	}
}

// TestBuildFTSQuery_OR与截断 验证 OR 连接与前 maxQueryTokens 截断
func TestBuildFTSQuery_OR与截断(t *testing.T) {
	got := BuildFTSQuery("VDS15800 组网")
	// "组网" 只有 2 个字符 < trigram 阈值，属短 token，不参与 FTS（由 LIKE 兜底）
	if want := `"VDS15800"`; got != want {
		t.Errorf("BuildFTSQuery = %q, want %q", got, want)
	}

	// 超过 maxQueryTokens 个 token 截断（全用 3 字以上词元，确保都是长 token）
	got = BuildFTSQuery("aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk lll")
	if n := strings.Count(got, " OR "); n != maxQueryTokens-1 {
		t.Errorf("截断后应有 %d 个 OR，实际 %d（query=%q）", maxQueryTokens-1, n, got)
	}
}

// TestBuildFTSQuery_空查询 验证空串与纯标点返回空（不做关键词检索）
func TestBuildFTSQuery_空查询(t *testing.T) {
	if got := BuildFTSQuery("   "); got != "" {
		t.Errorf("空查询应返回空串，实际 %q", got)
	}
	if got := BuildFTSQuery("，。！"); got != "" {
		t.Errorf("纯标点应返回空串，实际 %q", got)
	}
}

// TestBuildFTSQuery_只有短token时返回空 短 token 全部交给 LIKE 兜底，FTS 部分为空。
func TestBuildFTSQuery_只有短token时返回空(t *testing.T) {
	// "组网" 2 字 → 短 token；FTS 侧无可用 token
	if got := BuildFTSQuery("组网"); got != "" {
		t.Errorf("全为短 token 时应返回空串，实际 %q", got)
	}
}

// TestSplitQueryTokens 验证按 3 字符阈值分流长短 token。
// 对齐 Python split_query_tokens（internal.py:179）。
func TestSplitQueryTokens(t *testing.T) {
	cases := []struct {
		// name 用例名
		name string
		// query 输入查询
		query string
		// wantLong 期望长 token
		wantLong []string
		// wantShort 期望短 token
		wantShort []string
	}{
		{"中英混合", "VDS15800 组网", []string{"VDS15800"}, []string{"组网"}},
		{"全短", "组网 日志", nil, []string{"组网", "日志"}},
		{"全长", "configuration setting", []string{"configuration", "setting"}, nil},
		{"三字中文算长", "组网模式", []string{"组网模式"}, nil},
		{"空串", "", nil, nil},
		{"纯标点", "，。", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			long, short := SplitQueryTokens(tc.query)
			if !equalTokens(long, tc.wantLong) {
				t.Errorf("long = %v, want %v", long, tc.wantLong)
			}
			if !equalTokens(short, tc.wantShort) {
				t.Errorf("short = %v, want %v", short, tc.wantShort)
			}
		})
	}
}

// TestEscapeLikePattern 验证 LIKE 元字符转义。
// 不转义的话用户查询里的 `%` 会变成通配符，把整张表都捞出来。
func TestEscapeLikePattern(t *testing.T) {
	cases := []struct {
		// name 用例名
		name string
		// in 输入 token
		in string
		// want 期望模式
		want string
	}{
		{"普通词", "组网", `%组网%`},
		{"百分号", "100%", `%100\%%`},
		{"下划线", "a_b", `%a\_b%`},
		{"反斜杠", `a\b`, `%a\\b%`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EscapeLikePattern(tc.in); got != tc.want {
				t.Errorf("EscapeLikePattern(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBM25RankToScore 测试 BM25 排名转分数
func TestBM25RankToScore(t *testing.T) {
	if score := BM25RankToScore(0); score != 1.0 {
		t.Errorf("BM25RankToScore(0) = %f, want 1.0", score)
	}
	if score := BM25RankToScore(-1); score <= 0 {
		t.Errorf("BM25RankToScore(-1) = %f, want > 0", score)
	}
	if score := BM25RankToScore(-5); score <= 0 || score >= 1 {
		t.Errorf("BM25RankToScore(-5) = %f, want between 0 and 1", score)
	}
	// BM25 rank 通常为负数，越小越不相关
	score1 := BM25RankToScore(-1)
	score2 := BM25RankToScore(-10)
	if score1 <= score2 {
		t.Errorf("BM25RankToScore(-1)=%f 应大于 BM25RankToScore(-10)=%f", score1, score2)
	}
}

// TestIsMemoryPath 测试记忆路径判断
func TestIsMemoryPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"notes.md", true},
		{"MEMORY.md", true},
		{"data.json", false},
		{"readme.txt", false},
		{"dir/file.md", true},
		{"dir\\file.md", true}, // Windows 路径
		{"file.md.bak", false},
	}
	for _, tt := range tests {
		if got := IsMemoryPath(tt.path); got != tt.want {
			t.Errorf("IsMemoryPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestNormalizeExtraMemoryPaths 测试额外记忆路径归一化
func TestNormalizeExtraMemoryPaths(t *testing.T) {
	// 绝对路径保持不变
	result := NormalizeExtraMemoryPaths([]string{"/tmp/mem"}, "/workspace")
	if len(result) != 1 || result[0] != "/tmp/mem" {
		t.Errorf("绝对路径应保持不变，实际为 %v", result)
	}
	// 相对路径拼接 workspaceDir
	result = NormalizeExtraMemoryPaths([]string{"extra"}, "/workspace")
	if len(result) != 1 || result[0] != "/workspace/extra" {
		t.Errorf("相对路径应拼接 workspaceDir，实际为 %v", result)
	}
	// nil 返回 nil
	result = NormalizeExtraMemoryPaths(nil, "/workspace")
	if result != nil {
		t.Errorf("nil 应返回 nil，实际为 %v", result)
	}
	// 空切片返回 nil
	result = NormalizeExtraMemoryPaths([]string{}, "/workspace")
	if result != nil {
		t.Errorf("空切片应返回 nil，实际为 %v", result)
	}
}

// TestEstimateTokens 测试 token 估算
func TestEstimateTokens(t *testing.T) {
	if tokens := EstimateTokens("hello world"); tokens != 2 {
		t.Errorf("EstimateTokens('hello world') = %d, want 2", tokens)
	}
	if tokens := EstimateTokens(""); tokens != 0 {
		t.Errorf("EstimateTokens('') = %d, want 0", tokens)
	}
}

// TestHashText 测试哈希函数
func TestHashText(t *testing.T) {
	hash1 := HashText("hello")
	hash2 := HashText("hello")
	if hash1 != hash2 {
		t.Error("相同输入应产生相同哈希")
	}
	if len(hash1) != 16 {
		t.Errorf("哈希长度应为 16，实际为 %d", len(hash1))
	}
	hash3 := HashText("world")
	if hash1 == hash3 {
		t.Error("不同输入应产生不同哈希")
	}
}

// TestCosineSimilarity 测试余弦相似度
func TestCosineSimilarity(t *testing.T) {
	// 相同向量
	vec := []float64{1.0, 0.0, 0.0}
	if sim := CosineSimilarity(vec, vec); sim != 1.0 {
		t.Errorf("相同向量相似度应为 1.0，实际为 %f", sim)
	}
	// 正交向量
	vec1 := []float64{1.0, 0.0}
	vec2 := []float64{0.0, 1.0}
	if sim := CosineSimilarity(vec1, vec2); sim != 0.0 {
		t.Errorf("正交向量相似度应为 0.0，实际为 %f", sim)
	}
	// 不同长度
	if sim := CosineSimilarity([]float64{1.0}, []float64{1.0, 2.0}); sim != 0 {
		t.Errorf("不同长度向量应返回 0，实际为 %f", sim)
	}
}

// TestChunkMarkdown 测试 Markdown 分块
func TestChunkMarkdown(t *testing.T) {
	// 短文本不分块
	chunks := ChunkMarkdown("hello", 256, 32)
	if len(chunks) != 1 {
		t.Errorf("短文本应产生 1 个 chunk，实际为 %d", len(chunks))
	}
	// 长文本分块
	longText := ""
	for i := 0; i < 100; i++ {
		longText += "This is a line of text for testing chunking.\n"
	}
	chunks = ChunkMarkdown(longText, 50, 10)
	if len(chunks) <= 1 {
		t.Errorf("长文本应产生多个 chunk，实际为 %d", len(chunks))
	}
	// 零参数使用默认值
	chunks = ChunkMarkdown("test", 0, 0)
	if len(chunks) != 1 {
		t.Errorf("零参数应使用默认值，实际为 %d chunks", len(chunks))
	}
}

// TestEnsureDir 测试目录创建
func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := tmpDir + "/test/nested"
	if err := EnsureDir(testDir); err != nil {
		t.Fatalf("EnsureDir 失败: %v", err)
	}
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("EnsureDir 后目录应存在")
	}
}

// TestListMemoryFiles 测试列出记忆文件。对齐 Python list_memory_files
func TestListMemoryFiles(t *testing.T) {
	// 准备测试目录结构
	tmpDir := t.TempDir()
	memoryDir := filepath.Join(tmpDir, "memory")
	dailyDir := filepath.Join(memoryDir, "daily_memory")
	os.MkdirAll(dailyDir, 0o755)

	// 创建测试文件
	os.WriteFile(filepath.Join(memoryDir, "notes.md"), []byte("notes"), 0o644)
	os.WriteFile(filepath.Join(memoryDir, "data.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(memoryDir, "plan.md"), []byte("plan"), 0o644)
	os.WriteFile(filepath.Join(dailyDir, "2024-01.md"), []byte("daily"), 0o644)

	// 创建 USER.md
	userMD := filepath.Join(tmpDir, "USER.md")
	os.WriteFile(userMD, []byte("user"), 0o644)

	// 创建 workspace
	ws := &workspace.Workspace{
		RootPath: tmpDir,
		Directories: []workspace.DirectoryNode{
			{"name": "memory", "path": "memory"},
			{"name": "USER.md", "path": "USER.md"},
			{"name": "daily_memory", "path": "daily_memory"},
		},
	}

	// 测试基本调用
	files := ListMemoryFiles(ws, nil, "memory")
	if len(files) < 3 {
		t.Errorf("期望至少 3 个 .md 文件，实际为 %d: %v", len(files), files)
	}

	// 验证不含 .json 文件
	for _, f := range files {
		if filepath.Ext(f) != ".md" {
			t.Errorf("不应包含非 .md 文件: %s", f)
		}
	}

	// 测试 nil workspace
	nilFiles := ListMemoryFiles(nil, nil, "memory")
	if len(nilFiles) != 0 {
		t.Errorf("nil workspace 应返回空切片，实际为 %d", len(nilFiles))
	}
}

// TestListMemoryFiles_去重测试 验证文件去重排序
func TestListMemoryFiles_去重测试(t *testing.T) {
	tmpDir := t.TempDir()
	memoryDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(memoryDir, 0o755)
	os.WriteFile(filepath.Join(memoryDir, "notes.md"), []byte("notes"), 0o644)

	ws := &workspace.Workspace{
		RootPath: tmpDir,
		Directories: []workspace.DirectoryNode{
			{"name": "memory", "path": "memory"},
		},
	}

	// 多次调用应返回一致结果（去重后）
	files1 := ListMemoryFiles(ws, nil, "memory")
	files2 := ListMemoryFiles(ws, nil, "memory")
	if len(files1) != len(files2) {
		t.Errorf("多次调用结果不一致: %v vs %v", files1, files2)
	}
}

// TestDedupStrings 测试字符串去重
func TestDedupStrings(t *testing.T) {
	// 空切片
	result := dedupStrings(nil)
	if result != nil {
		t.Errorf("nil 应返回 nil，实际为 %v", result)
	}
	// 单元素
	result = dedupStrings([]string{"a"})
	if len(result) != 1 || result[0] != "a" {
		t.Errorf("单元素应原样返回，实际为 %v", result)
	}
	// 有重复
	result = dedupStrings([]string{"a", "a", "b", "c", "c"})
	if len(result) != 3 {
		t.Errorf("去重后应 3 个元素，实际为 %d: %v", len(result), result)
	}
	// 无重复
	result = dedupStrings([]string{"a", "b", "c"})
	if len(result) != 3 {
		t.Errorf("无重复应保持 3 个，实际为 %d", len(result))
	}
}
