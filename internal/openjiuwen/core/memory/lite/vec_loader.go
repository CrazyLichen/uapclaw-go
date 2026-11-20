package lite

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mattn/go-sqlite3"
)

// ──────────────────────────── 常量 ────────────────────────────

// vec0InitFuncName vec0 扩展初始化函数名
const vec0InitFuncName = "sqlite3_vec_init"

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveVec0Path 解析 vec0 扩展文件路径。
// 目录布局：third_party/sqlite-vec/<GOOS>-<GOARCH>/vec0.<ext>
// Linux: vec0.so；macOS: vec0.dylib；Windows: vec0.dll。
// 查找顺序（与 path.ResourcesDir 一致，保证解压即用、可从任意目录启动）：
//  1. 当前工作目录
//  2. 可执行文件所在目录
//  3. 可执行文件上一级目录（适配 zip 内 uapclaw/ 子目录布局）
//
// 全部不存在时返回 CWD 相对路径（调用方 os.Stat 后按降级处理）。
func ResolveVec0Path() string {
	ext := ".so"
	switch runtime.GOOS {
	case "darwin":
		ext = ".dylib"
	case "windows":
		ext = ".dll"
	}
	rel := filepath.Join("third_party", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0"+ext)
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	if execPath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(execPath)
		for _, base := range []string{exeDir, filepath.Dir(exeDir)} {
			candidate := filepath.Join(base, rel)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
		}
	}
	return rel
}

// IsVec0Available 检查 vec0.so 文件是否存在
func IsVec0Available() bool {
	_, err := os.Stat(ResolveVec0Path())
	return err == nil
}

// LoadVec0Extension 加载 vec0.so 扩展到 SQLite 连接。
// Python: sqlite_vec.load(self.db)
func LoadVec0Extension(conn *sqlite3.SQLiteConn, vecPath string) error {
	if conn == nil {
		return fmt.Errorf("conn 为 nil")
	}
	return conn.LoadExtension(vecPath, vec0InitFuncName)
}
