package rails

import "sync"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registrationMu 保护 registrationDone
	registrationMu sync.Mutex
	// registrationDone 是否已完成注册（对齐上游 _REGISTERED 守卫）
	registrationDone bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResetRegistrationForTest 重置注册守卫（仅测试用）。
//
// 生产不得调用：会让下次 EnsureHarnessElementsRegistered 重跑注册。
func ResetRegistrationForTest() {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	registrationDone = false
}
