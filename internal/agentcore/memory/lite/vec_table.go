package lite

import (
	"regexp"
	"strconv"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// vecTableDimsRe 从 vec0 建表语句里抓 float[N] 的维度（容忍括号内空格）。
var vecTableDimsRe = regexp.MustCompile(`float\[\s*(\d+)\s*\]`)

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseVecTableDims 解析 vec0 建表语句里的向量维度。
//
// 为什么要解析 sql：vec0 的维度写在 CREATE 语句里，没有系统表可查；而 manager
// 进程内的 vectorDims 在新进程里是空的，读到旧维度表时无法发现维度不匹配。
func parseVecTableDims(sql string) (int, bool) {
	m := vecTableDimsRe.FindStringSubmatch(sql)
	if len(m) != 2 {
		return 0, false
	}
	dims, err := strconv.Atoi(m[1])
	if err != nil || dims <= 0 {
		return 0, false
	}
	return dims, true
}

// needVecTableRebuild 判定是否必须丢弃现有 vec 表重建。
//
// 表不存在 → 需要建；维度一致 → 复用；维度不一致或解析不出 → 重建。
func needVecTableRebuild(existingSQL string, want int) bool {
	dims, ok := parseVecTableDims(existingSQL)
	if !ok {
		return true
	}
	return dims != want
}
