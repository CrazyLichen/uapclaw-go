package agent_mode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 对齐 Python L26-48: 26 个形容词
var adjectives = []string{
	"ancient", "blazing", "calm", "daring", "eager",
	"fierce", "gleaming", "happy", "icy", "jolly",
	"keen", "lively", "mighty", "noble", "open",
	"proud", "quiet", "rapid", "silent", "tall",
	"unique", "vivid", "warm", "xenial", "young", "zealous",
}

// 对齐 Python L34-40: 23 个动词
var verbs = []string{
	"brewing", "crafting", "designing", "exploring", "forging",
	"gathering", "hunting", "inspiring", "joining", "keeping",
	"learning", "making", "noting", "opening", "planning",
	"questing", "reading", "seeking", "testing", "using",
	"viewing", "writing", "yielding",
}

// 对齐 Python L43-48: 26 个名词
var nouns = []string{
	"anchor", "bridge", "cloud", "delta", "ember",
	"falcon", "galaxy", "harbor", "island", "jungle",
	"kernel", "lantern", "meadow", "nexus", "orbit",
	"phoenix", "quartz", "river", "summit", "tower",
	"union", "valley", "wave", "xenon", "yacht", "zenith",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateWordSlug 生成随机的 adjective-verb-noun 格式 slug。
//
// 使用 crypto/rand 作为随机源，对齐 Python generate_word_slug() L51-68。
func GenerateWordSlug() string {
	adj := adjectives[cryptoRandInt(len(adjectives))]
	verb := verbs[cryptoRandInt(len(verbs))]
	noun := nouns[cryptoRandInt(len(nouns))]
	return fmt.Sprintf("%s-%s-%s", adj, verb, noun)
}

// ResolvePlanFilePath 根据工作区根路径和 slug 推导 plan 文件绝对路径。
//
// 若 .plans 目录不存在则创建。对齐 Python resolve_plan_file_path() L71-92。
func ResolvePlanFilePath(workspaceRoot, slug string) string {
	plansDir := filepath.Join(workspaceRoot, ".plans")
	_ = os.MkdirAll(plansDir, 0o755)
	return filepath.Join(plansDir, slug+".md")
}

// GetOrCreatePlanSlug 生成不与已有 plan 文件冲突的 slug。
//
// 最多尝试 20 次。对齐 Python get_or_create_plan_slug() L95-111。
func GetOrCreatePlanSlug(workspaceRoot string) string {
	plansDir := filepath.Join(workspaceRoot, ".plans")
	_ = os.MkdirAll(plansDir, 0o755)
	for i := 0; i < 20; i++ {
		slug := GenerateWordSlug()
		path := filepath.Join(plansDir, slug+".md")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return slug
		}
	}
	return GenerateWordSlug()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cryptoRandInt 返回 [0, n) 范围内的安全随机整数。
func cryptoRandInt(n int) int {
	if n <= 0 {
		return 0
	}
	result, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// 降级到确定值，避免运行时崩溃
		return 0
	}
	return int(result.Int64())
}

// normalizeLanguage 规范化语言代码为 "cn" 或 "en"。
func normalizeLanguage(lang string) string {
	if lang == "en" {
		return "en"
	}
	return "cn"
}

// formatPlanPath 格式化 plan 路径用于消息输出。
func formatPlanPath(planPath string) string {
	// 统一使用正斜杠，与 Python Path 行为对齐
	return strings.ReplaceAll(planPath, `\`, "/")
}

// extractSession 从 ToolOption 中提取 SessionFacade。
func extractSession(opts []tool.ToolOption) sessioninterfaces.SessionFacade {
	callOpts := tool.NewToolCallOptions(opts...)
	session := callOpts.Session
	if session == nil {
		return nil
	}
	if sess, ok := session.(sessioninterfaces.SessionFacade); ok {
		return sess
	}
	return nil
}

// getWorkspaceRoot 从 DeepAgentInterface 获取工作区根路径。
func getWorkspaceRoot(agent hinterfaces.DeepAgentInterface) string {
	cfg := agent.DeepConfig()
	if cfg != nil && cfg.Workspace != nil {
		return cfg.Workspace.RootPath
	}
	return ""
}
