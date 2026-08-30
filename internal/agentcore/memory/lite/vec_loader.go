package lite

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mattn/go-sqlite3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// vec0InitFuncName vec0 扩展初始化函数名
const vec0InitFuncName = "sqlite3_vec_init"

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveVec0Path 解析 vec0.so 的路径。
// Linux 下: third_party/sqlite-vec/linux-amd64/vec0.so
// macOS 下: third_party/sqlite-vec/darwin-arm64/vec0.dylib
func ResolveVec0Path() string {
	ext := ".so"
	if runtime.GOOS == "darwin" {
		ext = ".dylib"
	}
	return filepath.Join("third_party", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0"+ext)
}

// IsVec0Available 检查 vec0.so 文件是否存在
func IsVec0Available() bool {
	_, err := os.Stat(ResolveVec0Path())
	return err == nil
}

// LoadVec0Extension 加载 vec0.so 扩展到 SQLite 连接。
// 对齐 Python sqlite_vec.load(self.db)
func LoadVec0Extension(conn *sqlite3.SQLiteConn, vecPath string) error {
	if conn == nil {
		return fmt.Errorf("conn 为 nil")
	}
	return conn.LoadExtension(vecPath, vec0InitFuncName)
}
