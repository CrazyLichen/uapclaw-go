package web

import (
	"context"
	"fmt"
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

// defaultCollectMemory 用 gopsutil 采集真实数据（psutil 等价物）。
//
// Python: process = psutil.Process(); process.memory_info().rss; psutil.virtual_memory()
func defaultCollectMemory() (rssBytes, totalBytes, availableBytes uint64, usedPercent float64, err error) {
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("获取当前进程信息失败: %w", err)
	}
	procMem, err := proc.MemoryInfo()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("获取进程内存信息失败: %w", err)
	}
	vm, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("获取系统内存信息失败: %w", err)
	}
	return procMem.RSS, vm.Total, vm.Available, vm.UsedPercent, nil
}

// bytesToMB 字节转 MB，对齐 Python bytes / (1024 * 1024)。
func bytesToMB(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}
