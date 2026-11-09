// PermissionEngine 的内部状态访问器与重建入口。
//
// 背景（9.90d-1 Task 13 前置基础设施第二批）：
// Python 的 JiuwenSwarmPermissionInterruptRail 直接读写父类引擎的私有成员
// `self._engine._file_guard` / `_workspace_root` 并调用 `_rebuild_file_guard()`，
// 用于在宿主工作区变化时把 NativePathGuardProjection 重新包在最新 checker 外。
//
// Go 的 PermissionEngine 在 security 包内，permissions 包的子类无法访问私有
// 字段，故本文件提供最小访问器面（不改动原有读写语义）。

package security

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// FileGuard 返回当前路径防护判定器（Python engine._file_guard）。
func (e *PermissionEngine) FileGuard() FileGuardEvaluator {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.fileGuard
}

// SetFileGuard 替换路径防护判定器（Python engine._file_guard 赋值）。
//
// 用于把既有 checker 包进 NativePathGuardProjection 后再装回。
func (e *PermissionEngine) SetFileGuard(fileGuard FileGuardEvaluator) {
	e.mu.Lock()
	e.fileGuard = fileGuard
	e.mu.Unlock()
}

// WorkspaceRoot 返回工作空间根（Python engine._workspace_root）。
func (e *PermissionEngine) WorkspaceRoot() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.workspaceRoot
}

// SetWorkspaceRoot 设置工作空间根（Python engine._workspace_root 赋值）。
func (e *PermissionEngine) SetWorkspaceRoot(workspaceRoot string) {
	e.mu.Lock()
	e.workspaceRoot = workspaceRoot
	e.mu.Unlock()
}

// RebuildFileGuard 按当前 config / workspaceRoot / trustedDirs 重建路径防护。
//
// Python: engine._rebuild_file_guard()
func (e *PermissionEngine) RebuildFileGuard() {
	e.mu.Lock()
	config := e.config
	workspace := e.workspaceRoot
	trusted := e.trustedDirs
	e.mu.Unlock()

	fileGuard := newFileGuardEvaluator(config, workspace, trusted)

	e.mu.Lock()
	e.fileGuard = fileGuard
	e.mu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
