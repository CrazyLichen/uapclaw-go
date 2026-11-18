// Package detectors 频繁上下文压缩推断（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/detectors/compaction.py
package detectors

import (
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FrequentCompactionDetector 从消息数骤降推断上下文压缩。
//
// Python: compaction.py FrequentCompactionDetector。无专用压缩钩子，本 detector
// 观察连续 BEFORE_MODEL_CALL 信号上的上下文消息数。降幅 ≥ drop_ratio 视为一次
// 压缩事件；窗口内压缩复发 frequency_threshold 次时抛 MEDIUM 异常。严重度封顶
// MEDIUM——推断是模糊的（正常多轮裁剪也可能像压缩），证据携带降幅供 leader
// 判断，且此 detector 永不进入自动补救。
type FrequentCompactionDetector struct {
	// frequencyThreshold 窗口内压缩次数阈值
	frequencyThreshold int
	// dropRatio 判定压缩所需的消息数降幅（0-1）
	dropRatio float64
	// nowFn 当前时间戳（秒），显式注入
	nowFn func() float64
	// window 压缩频次滑动窗口
	window *core.SlidingWindowCounter
	// lastCount 上一次消息数基线
	lastCount *int
	// fired 本轮是否已发射
	fired bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFrequentCompactionDetector 构造频繁压缩检测器。
//
// Python: compaction.py FrequentCompactionDetector.__init__（默认 300s/3/0.3）。
func NewFrequentCompactionDetector(windowSeconds float64, frequencyThreshold int, dropRatio float64, nowFn func() float64) *FrequentCompactionDetector {
	return &FrequentCompactionDetector{
		frequencyThreshold: frequencyThreshold,
		dropRatio:          dropRatio,
		nowFn:              nowFn,
		window:             core.NewSlidingWindowCounter(windowSeconds),
	}
}

// Name 返回 detector 稳定标识。
//
// Python: compaction.py FrequentCompactionDetector.name。
func (d *FrequentCompactionDetector) Name() string { return "frequent_compaction" }

// Observe 检测消息数骤降并标记频繁压缩。
//
// Python: compaction.py FrequentCompactionDetector.observe。
func (d *FrequentCompactionDetector) Observe(signal *core.Signal) *core.Anomaly {
	if signal.Kind != core.SignalKindBeforeModelCall || signal.MessageCount == nil {
		return nil
	}
	current := *signal.MessageCount
	previous := d.lastCount
	d.lastCount = &current
	if previous == nil || *previous <= 0 {
		return nil
	}
	// Python: current >= previous * (1.0 - drop_ratio) → 未骤降
	if float64(current) >= float64(*previous)*(1.0-d.dropRatio) {
		return nil
	}
	// 消息数骤降：推断一次压缩事件
	compactions := d.window.Add(d.nowFn())
	if compactions < d.frequencyThreshold || d.fired {
		return nil
	}
	d.fired = true
	return &core.Anomaly{
		Detector:   "frequent_compaction",
		Kind:       core.AnomalyKindFrequentCompaction,
		Severity:   core.SeverityMedium,
		MemberName: signal.MemberName,
		Summary:    strconv.Itoa(compactions) + " inferred compactions; last drop " + strconv.Itoa(*previous) + "->" + strconv.Itoa(current),
		Evidence: map[string]any{
			"compactions": compactions,
			"prev_count":  *previous,
			"cur_count":   current,
			"drop":        *previous - current,
		},
	}
}

// Reset 重置窗口、基线与已发射标记。
//
// Python: compaction.py FrequentCompactionDetector.reset。
func (d *FrequentCompactionDetector) Reset() {
	d.window.Reset()
	d.lastCount = nil
	d.fired = false
}
