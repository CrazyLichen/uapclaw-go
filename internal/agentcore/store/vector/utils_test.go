package vector

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestConvertL2Squared(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		maxDist  float64
		want     float64
	}{
		{"零距离返回1", 0, 4.0, 1.0},
		{"最大距离返回0", 4.0, 4.0, 0.0},
		{"中间值", 2.0, 4.0, 0.5},
		{"超过最大距离截断为0", 5.0, 4.0, 0.0},
		{"负值距离超过1", -1.0, 4.0, 1.25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertL2Squared(tt.rawScore, tt.maxDist); got != tt.want {
				t.Errorf("ConvertL2Squared(%v, %v) = %v, want %v", tt.rawScore, tt.maxDist, got, tt.want)
			}
		})
	}
}

func TestConvertL2Squared_默认最大距离(t *testing.T) {
	// 默认 maxDist=4.0
	if got := ConvertL2Squared(0, 4.0); got != 1.0 {
		t.Errorf("ConvertL2Squared(0, 4.0) = %v, want 1.0", got)
	}
}

func TestConvertCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"完全相似", 1.0, 1.0},
		{"完全相反", -1.0, 0.0},
		{"正交", 0.0, 0.5},
		{"0.6映射", 0.6, 0.8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertCosineSimilarity(tt.rawScore); got != tt.want {
				t.Errorf("ConvertCosineSimilarity(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}

func TestConvertCosineDistance(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"距离0完全相似", 0.0, 1.0},
		{"距离2完全相反", 2.0, 0.0},
		{"距离1正交", 1.0, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertCosineDistance(tt.rawScore); got != tt.want {
				t.Errorf("ConvertCosineDistance(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}

func TestConvertIPSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"高内积1", 1.0, 1.0},
		{"内积-1", -1.0, 0.0},
		{"内积0", 0.0, 0.5},
		{"超高内积截断1", 3.0, 1.0},
		{"超低内积截断0", -3.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertIPSimilarity(tt.rawScore); got != tt.want {
				t.Errorf("ConvertIPSimilarity(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}

func TestConvertIPDistance(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"距离0完全相似", 0.0, 1.0},
		{"距离2完全相反", 2.0, 0.0},
		{"距离1", 1.0, 0.5},
		{"负距离超出范围", -1.0, 1.0},
		{"超高距离截断0", 3.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertIPDistance(tt.rawScore); got != tt.want {
				t.Errorf("ConvertIPDistance(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}
