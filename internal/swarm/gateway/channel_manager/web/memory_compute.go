package web

import (
	"context"
	"os"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// collectMemory 采集本进程与系统内存，返回字节数及系统内存使用百分比。
//
// 以包级变量暴露供测试注入：gopsutil 的错误分支无法由真实环境稳定触发。
var collectMemory = defaultCollectMemory

// processRSSBytes 采集本进程 RSS 字节数（测试注入点）。
var processRSSBytes = defaultProcessRSSBytes

// currentPID 当前进程 PID（测试注入点）。
var currentPID = os.Getpid

// systemVirtualMemory 采集系统内存（测试注入点）。
var systemVirtualMemory = mem.VirtualMemory

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleMemoryCompute 处理 memory.compute 请求，返回进程与系统内存占用。
//
// Python: _memory_compute (app_web_handlers.py L1414-1427)
// 偏差：Python 计算了 mem.percent 却未放进 payload，此处补 used_percent 为超集
// （Go 前端 ToolPanel 读取该键，见 spec D1）。
func handleMemoryCompute() RPCHandlerFunc {
	return func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		rssBytes, totalBytes, availableBytes, usedPercent, err := collectMemory()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("method", "memory.compute").
				Msg("memory.compute collect failed")
			return nil, err
		}
		return map[string]any{
			"rss_mb":       bytesToMB(rssBytes),
			"total_mb":     bytesToMB(totalBytes),
			"available_mb": bytesToMB(availableBytes),
			"used_percent": usedPercent,
		}, nil
	}
}

// defaultCollectMemory 组合进程 RSS 与系统内存采集（psutil 等价物）。
//
// Python: process = psutil.Process(); process.memory_info().rss; psutil.virtual_memory()
// 错误原样透传，对齐 Python「psutil 异常直接冒到 register_method 包装层」语义。
func defaultCollectMemory() (rssBytes, totalBytes, availableBytes uint64, usedPercent float64, err error) {
	rssBytes, err = processRSSBytes()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	vm, err := systemVirtualMemory()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return rssBytes, vm.Total, vm.Available, vm.UsedPercent, nil
}

// defaultProcessRSSBytes 读取本进程物理内存（RSS）。
func defaultProcessRSSBytes() (uint64, error) {
	proc, err := process.NewProcess(int32(currentPID()))
	if err != nil {
		return 0, err
	}
	procMem, err := proc.MemoryInfo()
	if err != nil {
		return 0, err
	}
	return procMem.RSS, nil
}

// bytesToMB 字节转 MB，对齐 Python bytes / (1024 * 1024)。
func bytesToMB(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}
