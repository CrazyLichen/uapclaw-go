package reme

import (
	"encoding/json"
	"math"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// jsonBlockPattern 匹配 ```json ... ``` 代码块
var jsonBlockPattern = regexp.MustCompile("```json\\s*([\\s\\S]*?)\\s*```")

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSONExperienceResponse 从 LLM 响应中解析经验数据。
// 优先提取 ```json ... ``` 代码块中的 JSON，若不存在则尝试直接解析整个响应。
func ParseJSONExperienceResponse(response string) []map[string]any {
	// 尝试从 ```json 代码块中提取
	jsonBlocks := jsonBlockPattern.FindAllStringSubmatch(response, -1)
	if len(jsonBlocks) > 0 {
		var parsed any
		if err := json.Unmarshal([]byte(jsonBlocks[0][1]), &parsed); err == nil {
			switch v := parsed.(type) {
			case []any:
				experiences := make([]map[string]any, 0, len(v))
				for _, expData := range v {
					if m, ok := expData.(map[string]any); ok && isValidExperience(m) {
						experiences = append(experiences, m)
					}
				}
				return experiences
			case map[string]any:
				if isValidExperience(v) {
					return []map[string]any{v}
				}
			}
		}
	}

	// 无代码块，尝试直接解析整个响应
	var parsed any
	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		logger.Warn(logComponent).Str("error", err.Error()).Msg("解析 JSON 经验响应失败")
		return nil
	}
	switch v := parsed.(type) {
	case []any:
		result := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	case map[string]any:
		return []map[string]any{v}
	}
	return nil
}

// CalculateCosineSimilarity 计算两个向量的余弦相似度。
// 对齐 Python numpy: dot(a,b) / (norm(a) * norm(b))，零范数向量返回 0.0。
func CalculateCosineSimilarity(a, b []float64) float64 {
	dot := 0.0
	normA := 0.0
	normB := 0.0
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dot / (normA * normB)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isValidExperience 检查数据是否为有效的经验条目。
// 必须包含 experience 字段且包含 when_to_use 或 condition 字段。
func isValidExperience(data map[string]any) bool {
	_, hasExperience := data["experience"]
	_, hasTrigger := data["when_to_use"]
	if !hasTrigger {
		_, hasTrigger = data["condition"]
	}
	return hasExperience && hasTrigger
}
