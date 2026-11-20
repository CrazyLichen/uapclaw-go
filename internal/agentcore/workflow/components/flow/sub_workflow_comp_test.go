package flow

import "testing"

// TestBuildFinalResult_空累积 返回空 map（Python workflow_comp.py:47-48）
func TestBuildFinalResult_空累积(t *testing.T) {
	s := &SubWorkflowStreamState{}
	got, err := s.BuildFinalResult()
	if err != nil {
		t.Fatalf("期望无错误, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("期望空 map, got %v", got)
	}
}

// TestBuildFinalResult_嵌套output展开 复刻 Python :50-55
func TestBuildFinalResult_嵌套output展开(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"output": map[string]any{"out": "x"}})
	got, err := s.BuildFinalResult()
	if err != nil {
		t.Fatalf("期望无错误, got %v", err)
	}
	if v, ok := got["out"]; !ok || v != "x" {
		t.Fatalf("期望 out=x, got %v", got)
	}
}

// TestBuildFinalResult_同key字符串拼接 复刻 Python merged[key]+value
func TestBuildFinalResult_同key字符串拼接(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"k": "a"})
	s.Accumulate(map[string]any{"k": "b"})
	got, _ := s.BuildFinalResult()
	if got["k"] != "ab" {
		t.Fatalf("期望 k=ab, got %v", got)
	}
}

// TestBuildFinalResult_同key数字求和 int/int、float/float、int/float
func TestBuildFinalResult_同key数字求和(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"a": 1, "b": 1.5, "c": 2})
	s.Accumulate(map[string]any{"a": 2, "b": 2.5, "c": 0.5})
	got, _ := s.BuildFinalResult()
	if got["a"] != 3.0 || got["b"] != 4.0 || got["c"] != 2.5 {
		t.Fatalf("期望 a=3 b=4.0 c=2.5, got %v", got)
	}
}

// TestBuildFinalResult_异类型报错 对齐 Python TypeError（dict+dict / str+int）
func TestBuildFinalResult_异类型报错(t *testing.T) {
	cases := []struct {
		name   string
		frames []map[string]any
	}{
		{"dict加dict", []map[string]any{{"k": map[string]any{"x": 1}}, {"k": map[string]any{"y": 2}}}},
		{"str加int", []map[string]any{{"k": "a"}, {"k": 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &SubWorkflowStreamState{}
			for _, f := range c.frames {
				s.Accumulate(f)
			}
			if _, err := s.BuildFinalResult(); err == nil {
				t.Fatalf("期望报错（对齐 Python TypeError）, got nil")
			}
		})
	}
}

// TestBuildFinalResult_非dict帧跳过 复刻 Python isinstance(output, dict) 过滤
func TestBuildFinalResult_非dict帧跳过(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate("not a dict")
	s.Accumulate(42)
	s.Accumulate(map[string]any{"k": "v"})
	got, _ := s.BuildFinalResult()
	if got["k"] != "v" || len(got) != 1 {
		t.Fatalf("期望仅 k=v, got %v", got)
	}
}

// TestClear 清空后重新累积
func TestClear(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"k": "v"})
	s.Clear()
	got, _ := s.BuildFinalResult()
	if len(got) != 0 {
		t.Fatalf("Clear 后期望空, got %v", got)
	}
	s.Accumulate(map[string]any{"k2": "v2"})
	got, _ = s.BuildFinalResult()
	if got["k2"] != "v2" {
		t.Fatalf("Clear 后应可重新累积, got %v", got)
	}
}
