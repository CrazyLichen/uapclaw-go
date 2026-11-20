// Python 源：openjiuwen/agent_teams/experts/catalog.py（113 行 1:1）
//
// leader 专家工具的包定位协议，由 host 实现。本机装了哪些专家包、装在哪是
// host 知识（安装状态、目录布局）；包内容不是——manifest 由 agent-core 解析
// （ReadAgentTemplateCard/ReadAgentGroupCard/包加载器）。协议只回答「哪些包、
// 在哪个目录」，永不打开 manifest。
//
// host 经进程级注入（SetProcessExpertCatalog）注入一个 catalog；leader 的
// view_experts / view_expert_groups / spawn_expert 工具只在 catalog 存在时注册。

package experts

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// ExpertRef 一个专家模板的身份。
// 单专家按包名标识；AgentGroup 成员只按 (group, member name)——成员名仅组内唯一。
// Python: ExpertRef (catalog.py:33-52)
type ExpertRef struct {
	// Name 单专家包名，或 group 内成员名
	Name string
	// Group AgentGroup 名；单专家为空
	Group string
}

// ToDict 成员行持久化的 JSON 形态。Python: to_dict (:44-46)
func (r ExpertRef) ToDict() map[string]string {
	return map[string]string{"name": r.Name, "group": r.Group}
}

// Label 群组成员得 group/name，单专家得 name（日志、结果用）。
// Python: label (:48-50)
func (r ExpertRef) Label() string {
	if r.Group != "" {
		return r.Group + "/" + r.Name
	}
	return r.Name
}

// PackageLocation 一个已安装包的位置。
// Python: PackageLocation (catalog.py:55-66)
type PackageLocation struct {
	// Name 包名（单专家）或组名（AgentGroup）；等于目录名
	Name string
	// Path 包根目录（绝对）
	Path string
	// Source host 安装来源，如 builtin / local / hub
	Source string
}

// ExpertCatalog 定位已安装专家包；只读。实现只返回目录真实存在的已安装包，
// 永不读或校验 manifest 内容——坏包由读它的 core 解析器报告。
// Python: ExpertCatalog Protocol (catalog.py:69-88)；async 沿 D-15b 同步化。
type ExpertCatalog interface {
	// ListExpertPackages 每个已安装的单专家（agent_template）包。
	ListExpertPackages() []PackageLocation
	// LocateExpert 一个已安装单专家包；未安装得 error（Python ValueError）。
	LocateExpert(name string) (PackageLocation, error)
	// ListGroupPackages 每个已安装 AgentGroup 包。
	ListGroupPackages() []PackageLocation
	// LocateGroup 一个已安装 AgentGroup 包；未安装得 error。
	LocateGroup(name string) (PackageLocation, error)
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// processCatalogMu 保护 processCatalog（Python 单线程全局；Go 加锁）。
	processCatalogMu sync.RWMutex
	// processCatalog 本进程的专家 catalog。Python: _process_catalog (:91)
	processCatalog ExpertCatalog
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetProcessExpertCatalog 安装（或传 nil 清除）本进程的专家 catalog。
// 经 TeamRuntimeManager.set_expert_catalog 调用。catalog 与共享 DB、messager
// 一样是进程级：leader 的 spec 在 host 进程内装配，任何 runtime manager 钩子
// 都来不及触达。
// Python: set_process_expert_catalog (catalog.py:94-102)
func SetProcessExpertCatalog(catalog ExpertCatalog) {
	processCatalogMu.Lock()
	defer processCatalogMu.Unlock()
	processCatalog = catalog
}

// GetProcessExpertCatalog 本进程的专家 catalog；host 未注入得 nil。
// Python: get_process_expert_catalog (catalog.py:105-108)
func GetProcessExpertCatalog() ExpertCatalog {
	processCatalogMu.RLock()
	defer processCatalogMu.RUnlock()
	return processCatalog
}
