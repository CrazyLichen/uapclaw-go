package callback

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestCallbackMetrics_Update(t *testing.T) {
	m := &CallbackMetrics{}
	m.Update(0.1, false)
	m.Update(0.3, true)
	m.Update(0.2, false)
	if m.CallCount != 3 {
		t.Errorf("CallCount = %d, want 3", m.CallCount)
	}
	if m.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", m.ErrorCount)
	}
	if m.MinTime != 0.1 {
		t.Errorf("MinTime = %f, want 0.1", m.MinTime)
	}
	if m.MaxTime != 0.3 {
		t.Errorf("MaxTime = %f, want 0.3", m.MaxTime)
	}
}

func TestCallbackMetrics_AvgTime(t *testing.T) {
	m := &CallbackMetrics{}
	if m.AvgTime() != 0 {
		t.Errorf("空指标 AvgTime = %f, want 0", m.AvgTime())
	}
	m.Update(0.2, false)
	m.Update(0.4, false)
	if math.Abs(m.AvgTime()-0.3) > 1e-9 {
		t.Errorf("AvgTime = %f, want 0.3", m.AvgTime())
	}
}

func TestCallbackMetrics_ToDict(t *testing.T) {
	m := &CallbackMetrics{}
	m.Update(0.5, false)
	d := m.ToDict()
	if d["call_count"] != 1 {
		t.Errorf("call_count = %v, want 1", d["call_count"])
	}
}

func TestCallbackMetrics_并发安全(t *testing.T) {
	m := &CallbackMetrics{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Update(0.01, false)
		}()
	}
	wg.Wait()
	if m.CallCount != 100 {
		t.Errorf("CallCount = %d, want 100", m.CallCount)
	}
}

func TestFilterResult_构造(t *testing.T) {
	fr := FilterResult{Action: FilterActionSkip, Reason: "rate limited"}
	if fr.Action != FilterActionSkip {
		t.Errorf("Action = %v, want %v", fr.Action, FilterActionSkip)
	}
}

func TestChainContext_GetLastResult(t *testing.T) {
	c := &ChainContext{Results: []any{"a", "b", "c"}}
	if c.GetLastResult() != "c" {
		t.Errorf("GetLastResult = %v, want c", c.GetLastResult())
	}
	empty := &ChainContext{}
	if empty.GetLastResult() != nil {
		t.Errorf("空 Results GetLastResult = %v, want nil", empty.GetLastResult())
	}
}

func TestChainContext_ElapsedTime(t *testing.T) {
	c := &ChainContext{StartTime: time.Now()}
	d := c.ElapsedTime()
	if d < 0 {
		t.Errorf("ElapsedTime = %v, want >= 0", d)
	}
}

func TestChainContext_Metadata(t *testing.T) {
	c := &ChainContext{}
	c.SetMetadata("key", "value")
	v, ok := c.GetMetadata("key")
	if !ok || v != "value" {
		t.Errorf("GetMetadata = %v, %v; want value, true", v, ok)
	}
	_, ok = c.GetMetadata("missing")
	if ok {
		t.Errorf("GetMetadata(missing) = true, want false")
	}
}

func TestChainResult_构造(t *testing.T) {
	cr := &ChainResult{Action: ChainActionBreak, Result: "done"}
	if cr.Action != ChainActionBreak {
		t.Errorf("Action = %v, want %v", cr.Action, ChainActionBreak)
	}
}
