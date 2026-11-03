package lite

import "testing"

// TestParseVecTableDims 验证从 vec0 建表语句解析维度。
//
// 为什么要解析 sql：vec0 的维度写在 CREATE 语句里，没有系统表可查；
// 新进程的 vectorDims 是空的，读到旧维度表时无法发现不匹配。
func TestParseVecTableDims(t *testing.T) {
	cases := []struct {
		// name 用例名
		name string
		// sql 建表语句
		sql string
		// want 期望维度
		want int
		// ok 期望是否解析成功
		ok bool
	}{
		{"标准建表", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[128])", 128, true},
		{"带附加参数", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[1024], distance_metric=cosine)", 1024, true},
		{"空格容错", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[ 768 ])", 768, true},
		{"非 vec 表", "CREATE TABLE chunks (id TEXT)", 0, false},
		{"空 SQL", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseVecTableDims(tc.sql)
			if got != tc.want || ok != tc.ok {
				t.Errorf("parseVecTableDims(%q) = (%d, %v), want (%d, %v)", tc.sql, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestNeedVecTableRebuild 验证重建判定
func TestNeedVecTableRebuild(t *testing.T) {
	cases := []struct {
		// name 用例名
		name string
		// sql 现有建表语句（空串表示表不存在）
		sql string
		// want 目标维度
		want int
		// reb 期望是否重建
		reb bool
	}{
		{"表不存在 → 需要建", "", 1024, true},
		{"维度一致 → 复用", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[1024])", 1024, false},
		{"维度变化 → 重建", "CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[128])", 1024, true},
		{"解析不出 → 重建", "CREATE VIRTUAL TABLE chunks_vec USING vec0(x)", 1024, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needVecTableRebuild(tc.sql, tc.want); got != tc.reb {
				t.Errorf("needVecTableRebuild(%q, %d) = %v, want %v", tc.sql, tc.want, got, tc.reb)
			}
		})
	}
}
