package infra

import (
	"regexp"
	"strings"

	autoharness "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/auto_harness"
)

// competitorSignalKeywords 扩展演进信号关键词（对齐 _COMPETITOR_SIGNAL_KEYWORDS）
var competitorSignalKeywords = []string{
	"竞品",
	"competitor",
	"吸收",
	"absorb",
	"hermes",
	"evolver",
	"devin",
	"cursor",
	"扩展能力",
	"extend capability",
}

// competitorSignalPatterns 扩展演进信号正则（对齐 _COMPETITOR_SIGNAL_PATTERNS）
var competitorSignalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`把.+的.+能力.+加入`),
	regexp.MustCompile(`学习.+的做法`),
}

// DetectPipelineSignal 判断会话是否应路由到 extended evolve（对齐 detect_pipeline_signal）。
// 命中返回 ExtendedEvolvePipeline，未命中返回空串。
func DetectPipelineSignal(tasks []autoharness.OptimizationTask, config *autoharness.AutoHarnessConfig) string {
	var textParts []string
	for _, task := range tasks {
		if task.Topic != "" {
			textParts = append(textParts, task.Topic)
		}
		if task.Description != "" {
			textParts = append(textParts, task.Description)
		}
		if task.ExpectedEffect != "" {
			textParts = append(textParts, task.ExpectedEffect)
		}
	}
	if config.OptimizationGoal != "" {
		textParts = append(textParts, config.OptimizationGoal)
	}
	text := strings.ToLower(strings.Join(textParts, " "))
	if text == "" {
		return ""
	}
	for _, keyword := range competitorSignalKeywords {
		if strings.Contains(text, keyword) {
			return autoharness.ExtendedEvolvePipeline
		}
	}
	for _, pattern := range competitorSignalPatterns {
		if pattern.MatchString(text) {
			return autoharness.ExtendedEvolvePipeline
		}
	}
	return ""
}

// ChooseSessionPipeline 从配置偏好与信号中选择会话流水线
// （对齐 choose_session_pipeline）。
func ChooseSessionPipeline(
	tasks []autoharness.OptimizationTask,
	config *autoharness.AutoHarnessConfig,
	availablePipelines []string,
) autoharness.PipelineSelectionArtifact {
	preference := autoharness.NormalizePipelinePreference(config.PipelinePreference)
	if preference != autoharness.PipelinePreferenceAuto && containsStr(availablePipelines, preference) {
		var alternatives []string
		for _, name := range availablePipelines {
			if name != preference {
				alternatives = append(alternatives, name)
			}
		}
		return autoharness.PipelineSelectionArtifact{
			PipelineName:     preference,
			Reason:           "config pipeline preference",
			Alternatives:     alternatives,
			Confidence:       1.0,
			FallbackPipeline: preference,
		}
	}

	signal := DetectPipelineSignal(tasks, config)
	if signal != "" && containsStr(availablePipelines, signal) {
		var alternatives []string
		for _, name := range availablePipelines {
			if name != signal {
				alternatives = append(alternatives, name)
			}
		}
		return autoharness.PipelineSelectionArtifact{
			PipelineName:     signal,
			Reason:           "auto signal matched extended evolve pipeline",
			Alternatives:     alternatives,
			Confidence:       0.85,
			FallbackPipeline: autoharness.MetaEvolvePipeline,
		}
	}

	pipelineName := autoharness.MetaEvolvePipeline
	if !containsStr(availablePipelines, pipelineName) {
		pipelineName = availablePipelines[0]
	}
	var alternatives []string
	for _, name := range availablePipelines {
		if name != pipelineName {
			alternatives = append(alternatives, name)
		}
	}
	return autoharness.PipelineSelectionArtifact{
		PipelineName:     pipelineName,
		Reason:           "auto default meta evolve pipeline",
		Alternatives:     alternatives,
		Confidence:       0.7,
		FallbackPipeline: pipelineName,
	}
}

// containsStr 列表包含判断
func containsStr(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}
