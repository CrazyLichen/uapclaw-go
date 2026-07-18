package stages

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识。
var logComponent = logger.ComponentAgentServer

// init 注册所有阶段处理器到 skilldev 包的全局映射。
// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	skilldev.RegisterStageHandler(skilldev.SkillDevStageInit, &InitStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStagePlan, &PlanStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageGenerate, &GenerateStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageValidate, &ValidateStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageTestDesign, &TestDesignStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageTestRun, &TestRunStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageEvaluate, &EvaluateStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageImprove, &ImproveStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStagePackage, &PackageStageHandler{})
	skilldev.RegisterStageHandler(skilldev.SkillDevStageDescOptimize, &DescOptimizeStageHandler{})
}
