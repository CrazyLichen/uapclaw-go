package processcli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// 本文件是 channels/process_cli/diagnostics.py 的 Go 对应物。
//
// Python: channels/process_cli/diagnostics.py（143 行）
//
// 职责：把一次 CLI 调用的诊断（库日志、panic 栈、子进程输出）与用户可见
// 输出分开。做法是 dup 出真实句柄给渲染器，再把 fd1/fd2 指向日志文件。

// ──────────────────────────── 结构体 ────────────────────────────

// VisibleStreams 一次 CLI 调用中用户可见的两个流与诊断日志路径。
// Python: diagnostics.py:23-27 VisibleStreams
type VisibleStreams struct {
	// Stdout 用户可见的标准输出（dup 出的真实 fd1）
	Stdout *os.File
	// Stderr 用户可见的标准错误（dup 出的真实 fd2）
	Stderr *os.File
	// LogPath 诊断日志路径；空表示未启用文件记录
	LogPath string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// processCliLogsDirName 诊断日志子目录名（相对数据目录的 agent/ 下）
	// Python: diagnostics.py:41 "agent" / "process_cli_logs"
	processCliLogsDirName = "process_cli_logs"
	// processDebugEnv 强制开启诊断透传的环境变量
	// Python: diagnostics.py:74 JIUWENSWARM_PROCESS_DEBUG
	processDebugEnv = "UAPCLAW_PROCESS_DEBUG"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CaptureDiagnostics 为一次 CLI 调用捕获 Go 与继承的原生诊断。
// Python: diagnostics.py:64-169 capture_diagnostics（contextmanager）
//
// 渲染器通过 dup 出的真实句柄写出用户可见内容；库日志、panic 栈与子进程
// 继承被重定向的标准句柄。debug 或 UAPCLAW_PROCESS_DEBUG=1 时直接透传。
//
// 返回的 restore 必须调用一次，恢复原 fd（不可重复调用）。
func CaptureDiagnostics(captureStdout, debug bool) (*VisibleStreams, func(), error) {
	// Python: :74-76 debug 或环境变量 → 直接透传
	if debug || os.Getenv(processDebugEnv) == "1" {
		return passthroughStreams(), func() {}, nil
	}

	logPath, err := diagnosticPath()
	if err != nil {
		// Python: :88-91 日志准备失败不阻断 CLI，退回透传
		fmt.Fprintf(os.Stderr, "uapclaw-process: 无法写入诊断日志：%v\n", err)
		return passthroughStreams(), func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 无法创建日志目录：%v\n", err)
		return passthroughStreams(), func() {}, nil
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 无法创建日志文件：%v\n", err)
		return passthroughStreams(), func() {}, nil
	}

	// Python: :100-118 Windows 控制台特例：Win32 句柄超出 CRT fd 范围，
	// 替换 fd1/fd2 会破坏控制台句柄。只记路径、不动 fd。
	if runtime.GOOS == "windows" && (isTerminal(os.Stdout) || isTerminal(os.Stderr)) {
		return &VisibleStreams{
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
			LogPath: logPath,
		}, func() { _ = logFile.Close() }, nil
	}

	// Python: :124-139 dup 出可见句柄
	visibleStdout := os.Stdout
	visibleStderr := os.Stderr
	if captureStdout {
		if dup, dupErr := dupFile(os.Stdout); dupErr == nil {
			visibleStdout = dup
		}
	}
	if dup, dupErr := dupFile(os.Stderr); dupErr == nil {
		visibleStderr = dup
	}

	restoreStdout, restoreStderr, redirectErr := redirectStdio(logFile, captureStdout)
	if redirectErr != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 重定向失败：%v\n", redirectErr)
		_ = logFile.Close()
		return passthroughStreams(), func() {}, nil
	}

	restore := func() {
		// Python: :148-165 先 flush 两流，再按 stdout 后 stderr 恢复（与重定向相反）
		_ = visibleStdout.Sync()
		_ = visibleStderr.Sync()
		restoreStdout()
		restoreStderr()
		if visibleStdout != os.Stdout {
			_ = visibleStdout.Close()
		}
		if visibleStderr != os.Stderr {
			_ = visibleStderr.Close()
		}
		_ = logFile.Close()
	}
	return &VisibleStreams{Stdout: visibleStdout, Stderr: visibleStderr, LogPath: logPath}, restore, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// passthroughStreams 透传形态（不做任何重定向）。
func passthroughStreams() *VisibleStreams {
	return &VisibleStreams{Stdout: os.Stdout, Stderr: os.Stderr}
}

// diagnosticPath 构造诊断日志路径。
// Python: diagnostics.py:30-42 _diagnostic_path
//
// 数据目录下 agent/process_cli_logs/<UTC 时间戳>-<pid>-<rand>.log。
func diagnosticPath() (string, error) {
	root := os.Getenv("UAPCLAW_DATA_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".uapclaw")
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d-%s.log", stamp, os.Getpid(), suffix)
	return filepath.Join(root, "agent", processCliLogsDirName, name), nil
}

// randomHex 生成 n 字节的十六进制串。
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
