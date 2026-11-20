// Package interrupt 的产品版本判定。
//
// 本文件对齐 Python jiuwenswarm/enterprise/edition.py（零依赖版本探测）。
//
// # 为什么单独一个文件
//
// Python 那边刻意做成「只依赖标准库 os」的独立模块，任何启动阶段（包括
// dotenv_early 等早期路径）都能安全导入。Go 侧同理：不 import 任何项目内包，
// 避免早期初始化时被依赖链拖住。
package interrupt

import (
	"os"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// envEdition 产品形态环境变量。
	//
	// Python: JIUWENSWARM_EDITION（品牌前缀按 jiuwenswarm→uapclaw 映射）
	envEdition = "UAPCLAW_EDITION"
	// editionEnterprise 企业版标识
	editionEnterprise = "enterprise"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsEnterprise 判断当前是否企业版。
//
// Python: is_enterprise() (edition.py L18-20)
//
// 口径：UAPCLAW_EDITION 去空白转小写后等于 "enterprise"。
// 环境变量未设置或为其它值一律按个人版处理。
func IsEnterprise() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv(envEdition))) == editionEnterprise
}
