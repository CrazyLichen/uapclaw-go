package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// wikiManifestEntry manifest 条目：name/destination/ingested_at。
// Python: Dict[str, str] (wiki_tools.py L121-127)
type wikiManifestEntry map[string]string

// sourceManifest 按 SHA-256 内容哈希追踪已摄取的源文件。
// Python: _SourceManifest (wiki_tools.py L89-158)
type sourceManifest struct {
	// path manifest.json 完整路径
	path string
	// sysOp 系统操作（nil → 本地 os 分支）
	sysOp sys_operation.SysOperation
	// data 源文件 sha256 → manifest 条目 的映射
	data map[string]wikiManifestEntry
}

// llmWiki LLM Wiki 实例：内嵌 DeepAgent 编译知识库。
// Python: LLMWiki (wiki_tools.py L161-350)
type llmWiki struct {
	// workspace 工作区根（.llm_wiki）
	workspace string
	// sourcesDir 原始文档目录
	sourcesDir string
	// wikiDir 编译后 markdown 页面目录
	wikiDir string
	// schemaDir 架构规则目录
	schemaDir string
	// manifest 源文件哈希清单
	manifest *sourceManifest
	// agent 内嵌 DeepAgent
	agent *harness.DeepAgent
	// sessionID 会话 ID（workspace 哈希前 16 位）
	sessionID string
	// sess 会话实例
	sess *session.Session
}

// wikiIngestInput wiki_ingest 工具入参。
// Python: wiki_ingest(source, workspace, force, sys_operation) (wiki_tools.py L404-409)
// 注意：Python 的 sys_operation 参数运行时恒为 None（LocalFunction.invoke 只展开 inputs），
// Go 工具入参 struct 不含该字段（spec 决策 5），内部恒传 nil。
type wikiIngestInput struct {
	// Source 源文件或目录路径
	Source string `json:"source" jsonschema:"description=The source file or directory to ingest,required"`
	// Workspace wiki 工作区
	Workspace string `json:"workspace" jsonschema:"description=The root folder where the .llm_wiki will be created"`
	// Force 强制重新摄取
	Force bool `json:"force" jsonschema:"description=Force re-ingestion even if the file hash is known"`
}

// wikiQueryInput wiki_query 工具入参。
// Python: wiki_query(query, workspace) (wiki_tools.py L460-461)
type wikiQueryInput struct {
	// Query 自然语言查询
	Query string `json:"query" jsonschema:"description=The natural language question,required"`
	// Workspace wiki 工作区
	Workspace string `json:"workspace" jsonschema:"description=The folder containing the .llm_wiki"`
}

// wikiLintInput wiki_lint 工具入参。
// Python: wiki_lint(workspace) (wiki_tools.py L493)
type wikiLintInput struct {
	// Workspace wiki 工作区
	Workspace string `json:"workspace" jsonschema:"description=The root folder containing the .llm_wiki"`
}

// ──────────────────────────── 常量 ────────────────────────────

// defaultWikiDir wiki 工作区目录名。
// Python: DEFAULT_WIKI_DIR (wiki_tools.py L32)
const defaultWikiDir = ".llm_wiki"

// wikiAgentSystemPromptEN wiki agent 英文系统提示词（逐字复制 Python）。
// Python: DEFAULT_WIKI_AGENT_SYSTEM_PROMPT_EN (wiki_tools.py L34-52)
const wikiAgentSystemPromptEN = "You are an LLM Wiki Maintainer. You manage a workspace consisting of three primary directories:\n" +
	"1. `sources/`: where immutable raw text and PDF files are placed.\n" +
	"2. `wiki/`: the destination for compiled, structured markdown entity pages.\n" +
	"3. `schema/`: contains an `AGENT.md` document defining the architecture and rules.\n\n" +
	"CRITICAL RULES:\n" +
	"- Always read `schema/AGENT.md` first to understand operational rules.\n" +
	"- Always read `wiki/index.md` before making modifications to ensure you append new items properly.\n" +
	"- You MUST update `wiki/log.md` with an entry describing every major action or ingestion you perform!\n" +
	"- To ingest PDFs, use the `read_pdf` tool. For large documents, start by reading the FIRST page to" +
	" understand the structure and total page count. Then, read subsequent pages in chunks as needed to" +
	" ensure complete synthesis without exceeding your context limit.\n" +
	"- Prioritize ingesting new, unprocessed files from `sources/` before performing secondary cross-linking.\n" +
	"- When calling tools (especially `edit_file`), you MUST use the exact parameter names defined in the" +
	" tool schema (e.g., `old_string`, `new_string`). Do not append symbols like `=` to keys, and do not provide lists" +
	" or indices as string values." +
	" Only use `replace_all: true` if you are providing the COMPLETE new content for a section.\n" +
	"- DO NOT create subdirectories within `wiki/`. Always save pages directly in the `wiki/` root folder."

// wikiAgentSystemPromptCN wiki agent 中文系统提示词（逐字复制 Python）。
// Python: DEFAULT_WIKI_AGENT_SYSTEM_PROMPT_CN (wiki_tools.py L54-67)
const wikiAgentSystemPromptCN = "你是 LLM Wiki 维护者。你负责管理一个包含三个主要目录的工作区：\n" +
	"1. `sources/`：存放不可变的原始文档和 PDF。\n" +
	"2. `wiki/`：存放编译后结构化的 markdown 主题页面。\n" +
	"3. `schema/`：包含一个 `AGENT.md` 文档，定义了架构和操作规则。\n\n" +
	"关键规则：\n" +
	"- 务必首先读取 `schema/AGENT.md`，以了解操作规范。\n" +
	"- 在进行任何修改之前，务必先读取 `wiki/index.md` 和 `wiki/log.md`，以确保正确追加新项并维护一致的交叉引用。\n" +
	"- 对于 PDF 摄取，请使用 `read_pdf` 工具。对于大型文档，请先阅读第一页以了解结构和总页数，然后根据需要分块阅读后续页面，以确保在不超出上下文限制的情况下完成综合。\n" +
	"- 在对现有 `wiki/` 页面进行二次交叉引用或 lint 之前，优先从 `sources/` 摄取新的、未经处理的文件。\n" +
	"- 调用工具（特别是 `edit_file`）时，必须使用工具架构中确化的准确参数名称（例如 `old_string`、`new_string`）。不要在键名后添加 `=` 等符号，也不要提供列表或索引作为字符串值。" +
	"仅当你为某个部分提供完整的全新内容时，才使用 `replace_all: true`。\n" +
	"- 不要在 `wiki/` 中创建子目录。始终将页面直接保存在 `wiki/` 根文件夹中。"

// wikiAgentDescriptionEN wiki agent 英文描述（逐字复制 Python）。
// Python: DEFAULT_WIKI_AGENT_DESCRIPTION_EN (wiki_tools.py L74-77)
const wikiAgentDescriptionEN = "You are a Wiki Maintainer agent." +
	" You ingest raw documents and continuously compile them into a structured markdown wiki knowledge base."

// wikiAgentDescriptionCN wiki agent 中文描述（逐字复制 Python）。
// Python: DEFAULT_WIKI_AGENT_DESCRIPTION_CN (wiki_tools.py L79-81)
const wikiAgentDescriptionCN = "你是 Wiki 维护代理。负责摄取原始文档，并不断将它们编译成结构化的 markdown Wiki 知识库。"

// wikiSchemaContent schema/AGENT.md 模板（7 条规则，逐字复制 Python）。
// Python: ensure_initialized 内 schema_content (wiki_tools.py L234-243)
const wikiSchemaContent = "# Wiki Maintainer Rules\n" +
	"1. Never modify files inside `sources/`.\n" +
	"2. All pages you generate MUST be saved directly inside the `wiki/` directory root.\n" +
	"3. You must maintain a `wiki/index.md` listing all topics.\n" +
	"4. You must maintain a `wiki/log.md` with an append-only timeline of ingestions.\n" +
	"5. Break concepts down into modular topic pages.\n" +
	"6. Make heavy use of markdown links to interconnect pages within `wiki/`.\n" +
	"7. DO NOT create subdirectories (like `wiki/entity/`). Save all files in the `wiki/` root.\n"

// wikiIndexContent wiki/index.md 模板（四段骨架，逐字复制 Python）。
// Python: ensure_initialized 内 index_content (wiki_tools.py L255-264)
const wikiIndexContent = "# Wiki Index\n\n" +
	"This index lists all topics covered in the wiki.\n\n" +
	"## Entities\n\n" +
	"<!-- entity pages go here, one bullet per page -->\n\n" +
	"## Concepts\n\n" +
	"<!-- concept pages go here -->\n\n" +
	"## Sources\n\n" +
	"<!-- one bullet per ingested source, link to its summary page -->\n"

// wikiLogContent wiki/log.md 模板（grep 友好头，逐字复制 Python）。
// Python: ensure_initialized 内 log_content (wiki_tools.py L276-282)
const wikiLogContent = "# Wiki Log\n\n" +
	"Append-only timeline of all wiki operations.\n" +
	"Each entry starts with `## [YYYY-MM-DD] <operation> | <title>`\n" +
	"so it is grep-parseable:\n" +
	"<!-- append new entries below this line -->\n"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// WikiIngest wiki_ingest 工具包级单例（对齐 Python 模块级 @tool 实例）。
	// Python: wiki_ingest (wiki_tools.py L396)
	WikiIngest = newWikiIngestTool()

	// WikiQuery wiki_query 工具包级单例。
	// Python: wiki_query (wiki_tools.py L454)
	WikiQuery = newWikiQueryTool()

	// WikiLint wiki_lint 工具包级单例。
	// Python: wiki_lint (wiki_tools.py L487)
	WikiLint = newWikiLintTool()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSourceManifest 构造 sourceManifest。
// Python: _SourceManifest.__init__ (wiki_tools.py L94-97)
func newSourceManifest(sourcesDir string, sysOp sys_operation.SysOperation) *sourceManifest {
	return &sourceManifest{
		path:  filepath.Join(sourcesDir, "manifest.json"),
		sysOp: sysOp,
		data:  map[string]wikiManifestEntry{},
	}
}

// ensureInitialized 加载 manifest。
// Python: _SourceManifest.ensure_initialized (wiki_tools.py L99-100)
func (m *sourceManifest) ensureInitialized(ctx context.Context) {
	m.data = m.load(ctx)
}

// sha256Of 计算文件 SHA-256（64KB 流式）。
// Python: _SourceManifest.sha256_of (wiki_tools.py L102-110)
func (m *sourceManifest) sha256Of(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("Path is not a valid file: %s", filePath)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("Path is not a valid file: %s", filePath)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	buf := make([]byte, 65536)
	for {
		n, rErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			return "", rErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isKnown 哈希是否已知。
// Python: _SourceManifest.is_known (wiki_tools.py L112-113)
func (m *sourceManifest) isKnown(sha string) bool {
	_, ok := m.data[sha]
	return ok
}

// get 获取条目。
// Python: _SourceManifest.get (wiki_tools.py L115-116)
func (m *sourceManifest) get(sha string) *wikiManifestEntry {
	if e, ok := m.data[sha]; ok {
		return &e
	}
	return nil
}

// record 记录条目（已存在跳过）。
// Python: _SourceManifest.record (wiki_tools.py L118-128)
func (m *sourceManifest) record(ctx context.Context, sha, name, destination string) error {
	if _, ok := m.data[sha]; ok {
		return nil
	}
	m.data[sha] = wikiManifestEntry{
		"name":        name,
		"destination": destination,
		// Python: datetime.now(timezone.utc).isoformat(timespec="seconds")
		"ingested_at": time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	return m.save(ctx)
}

// allEntries 返回全部条目副本。
// Python: _SourceManifest.all_entries (wiki_tools.py L130-131)
func (m *sourceManifest) allEntries() map[string]wikiManifestEntry {
	out := make(map[string]wikiManifestEntry, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

// load 加载 manifest（sysOp 双分支；损坏/OS 错误吞错为空 map）。
// Python: _SourceManifest._load (wiki_tools.py L133-148)
func (m *sourceManifest) load(ctx context.Context) map[string]wikiManifestEntry {
	if m.sysOp != nil {
		res, err := m.sysOp.Fs().ReadFile(ctx, m.path)
		if err == nil && res != nil && res.Code == 0 && res.Data != nil {
			var parsed map[string]wikiManifestEntry
			if jErr := json.Unmarshal([]byte(res.Data.Content), &parsed); jErr == nil {
				return parsed
			}
			return map[string]wikiManifestEntry{}
		}
		return map[string]wikiManifestEntry{}
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return map[string]wikiManifestEntry{}
	}
	var parsed map[string]wikiManifestEntry
	if jErr := json.Unmarshal(raw, &parsed); jErr != nil {
		return map[string]wikiManifestEntry{}
	}
	return parsed
}

// save 保存 manifest（parent MkdirAll；sysOp 双分支）。
// Python: _SourceManifest._save (wiki_tools.py L150-158)
func (m *sourceManifest) save(ctx context.Context) error {
	parent := filepath.Dir(m.path)
	if _, statErr := os.Stat(parent); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
			return mkErr
		}
	}
	// Python: json.dumps(self._data, indent=2, ensure_ascii=False)
	content, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	if m.sysOp != nil {
		_, wErr := m.sysOp.Fs().WriteFile(ctx, m.path, string(content))
		return wErr
	}
	return os.WriteFile(m.path, content, 0o644)
}

// newLLMWiki 构造 LLMWiki。
// Python: LLMWiki.__init__ (wiki_tools.py L162-225)
func newLLMWiki(ctx context.Context, workspaceDir string, model *llm.Model, sysOp sys_operation.SysOperation) (*llmWiki, error) {
	w := &llmWiki{
		workspace:  workspaceDir,
		sourcesDir: filepath.Join(workspaceDir, "sources"),
		wikiDir:    filepath.Join(workspaceDir, "wiki"),
		schemaDir:  filepath.Join(workspaceDir, "schema"),
		manifest:   newSourceManifest(filepath.Join(workspaceDir, "sources"), sysOp),
	}

	// Python: resolved_language = resolve_language(None)
	language := prompts.ResolveLanguage("")

	// Python: final_card = card or AgentCard(name="wiki_agent", description=...)
	description := wikiAgentDescriptionCN
	if language == "en" {
		description = wikiAgentDescriptionEN
	}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("wiki_agent"),
		agentschema.WithAgentDescription(description),
	)

	// Python: final_prompt = system_prompt or DEFAULT_..._SYSTEM_PROMPT.get(lang, ["cn"])
	systemPrompt := wikiAgentSystemPromptCN
	if language == "en" {
		systemPrompt = wikiAgentSystemPromptEN
	}

	// Python: final_rails = rails if rails is not None else [SysOperationRail()]
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:               model,
		Card:                card,
		SystemPrompt:        systemPrompt,
		Rails:               []agentinterfaces.AgentRail{rails.NewSysOperationRail()},
		EnableTaskLoop:      true,
		MaxIterations:       15,
		Workspace:           hworkspace.NewWorkspace(workspaceDir, language),
		Language:            language,
		SysOperation:        sysOp, // nil → CreateDeepAgent 内部默认
		AutoCreateWorkspace: true,
	})
	if err != nil {
		return nil, err
	}
	w.agent = agent

	// Python: _sid = session_id or sha256(workspace.resolve())[:16]
	// session_id 参数恒空——Python 调用点未传。
	resolved := resolvePathStringWiki(workspaceDir)
	sidHash := sha256.Sum256([]byte(resolved))
	w.sessionID = hex.EncodeToString(sidHash[:])[:16]
	w.sess = session.NewSession(session.WithSessionID(w.sessionID))
	return w, nil
}

// ensureInitialized 初始化三目录 + manifest + 三模板 + agent。
// Python: LLMWiki.ensure_initialized (wiki_tools.py L227-293)
func (w *llmWiki) ensureInitialized(ctx context.Context) error {
	for _, d := range []string{w.sourcesDir, w.wikiDir, w.schemaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	w.manifest.ensureInitialized(ctx)

	// 三模板"不存在则写"（sysOp 双分支）
	if err := w.ensureTemplateFile(ctx, filepath.Join(w.schemaDir, "AGENT.md"), wikiSchemaContent); err != nil {
		return err
	}
	if err := w.ensureTemplateFile(ctx, filepath.Join(w.wikiDir, "index.md"), wikiIndexContent); err != nil {
		return err
	}
	if err := w.ensureTemplateFile(ctx, filepath.Join(w.wikiDir, "log.md"), wikiLogContent); err != nil {
		return err
	}

	_, err := w.agent.EnsureInitialized(ctx)
	return err
}

// ensureTemplateFile 模板"不存在则写"。
// Python: ensure_initialized 内三段 if self._sys_op / else 分支 (wiki_tools.py L244-291)
func (w *llmWiki) ensureTemplateFile(ctx context.Context, filePath, content string) error {
	if w.manifest.sysOp != nil {
		res, err := w.manifest.sysOp.Fs().ReadFile(ctx, filePath)
		if err == nil && res != nil && res.Code == 0 {
			return nil
		}
		// Python: write_file(..., create_if_not_exist=True)
		_, wErr := w.manifest.sysOp.Fs().WriteFile(ctx, filePath, content,
			sys_operation.WithFsCreateIfNotExist(true))
		return wErr
	}
	if _, statErr := os.Stat(filePath); statErr == nil {
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return os.WriteFile(filePath, []byte(content), 0o644)
}

// listSources 列出全部已摄取源。
// Python: LLMWiki.list_sources (wiki_tools.py L295-296)
func (w *llmWiki) listSources() map[string]wikiManifestEntry {
	return w.manifest.allEntries()
}

// ingest 摄取单个源文件。
// Python: LLMWiki.ingest (wiki_tools.py L298-332)
func (w *llmWiki) ingest(ctx context.Context, sourcePath string, force bool) (map[string]any, error) {
	if _, statErr := os.Stat(sourcePath); statErr != nil {
		// Python: raise FileNotFoundError(f"Source file not found: {source_path}")
		return nil, fmt.Errorf("Source file not found: %s", sourcePath)
	}

	sha256sum, err := w.manifest.sha256Of(sourcePath)
	if err != nil {
		return nil, err
	}
	if !force && w.manifest.isKnown(sha256sum) {
		var entry map[string]string
		if e := w.manifest.get(sha256sum); e != nil {
			entry = *e
		}
		logger.Warn(logger.ComponentCommon).
			Str("source_name", filepath.Base(sourcePath)).
			Msg("Skipping already-ingested source")
		return map[string]any{
			"output":      "Skipped duplicate source",
			"skipped":     true,
			"sha256":      sha256sum,
			"ingested_at": entry["ingested_at"],
		}, nil
	}

	// Python: destination = sources_dir / f"{sha256[:8]}_{source_path.name}"
	destination := filepath.Join(w.sourcesDir, fmt.Sprintf("%s_%s", sha256sum[:8], filepath.Base(sourcePath)))
	// Python: if source_path.resolve() != destination.resolve(): shutil.copy2(...)
	if resolvePathStringWiki(sourcePath) != resolvePathStringWiki(destination) {
		if cErr := copyFilePreserveMeta(sourcePath, destination); cErr != nil {
			return nil, cErr
		}
	}

	// Python: 固定 query 模板 (wiki_tools.py L318-324 逐字)
	query := fmt.Sprintf(
		"Read the rules in your `schema/AGENT.md`."+
			" Process the new raw source document '%s' inside `sources/` into the wiki."+
			" CRITICAL: You MUST read `wiki/index.md` and other relevant `.md` files to discover existing topics."+
			" Actively interconnect them by adding deep Markdown cross-links."+
			" FINALLY, you MUST append a detailed summary of what knowledge you extracted into `wiki/log.md`!",
		filepath.Base(destination),
	)
	result, err := w.agent.Invoke(ctx, map[string]any{"query": query}, agentinterfaces.WithSession(w.sess))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	// Python: if "error" in result or str(result.get("output", "")).startswith("[ERROR"):
	// 不记录哈希如果 agent 显式失败
	if _, hasErr := result["error"]; hasErr {
		return result, nil
	}
	if outputStr, ok := result["output"].(string); ok && strings.HasPrefix(outputStr, "[ERROR") {
		return result, nil
	}

	if rErr := w.manifest.record(ctx, sha256sum, filepath.Base(sourcePath), destination); rErr != nil {
		return nil, rErr
	}
	return result, nil
}

// query 查询知识库。
// Python: LLMWiki.query (wiki_tools.py L334-339)
func (w *llmWiki) query(ctx context.Context, question string) (map[string]any, error) {
	// Python: 固定 query 模板 (wiki_tools.py L335-338 逐字)
	query := fmt.Sprintf(
		"Answer this strictly based on `wiki/`: '%s'."+
			" If your answer forms a valuable new insight, write it back into the wiki.",
		question,
	)
	return w.agent.Invoke(ctx, map[string]any{"query": query}, agentinterfaces.WithSession(w.sess))
}

// lint 健康检查并修复。
// Python: LLMWiki.lint (wiki_tools.py L341-350)
func (w *llmWiki) lint(ctx context.Context) (map[string]any, error) {
	// Python: 固定 query 模板 (wiki_tools.py L342-348 逐字)
	query := "Health-check the `wiki/` directory." +
		" Actively evaluate all topic pages to discover orphaned documents or related, un-linked concepts." +
		" You MUST proactively use your file editing tools to forge new Markdown cross-links between related" +
		" pages, transforming isolated files into a highly connected knowledge graph." +
		" Report the links you established." +
		" FINALLY, you MUST append a detailed summary of the repairs and link established into `wiki/log.md`!"
	return w.agent.Invoke(ctx, map[string]any{"query": query}, agentinterfaces.WithSession(w.sess))
}

// getDefaultWikiModel 获取默认 wiki 模型。
// Python: _get_default_model (wiki_tools.py L353-373)
func getDefaultWikiModel() (*llm.Model, error) {
	defaults := config.GetDefaultModels(nil)
	var modelConf map[string]any
	for _, m := range defaults {
		if isTruthyWiki(m["is_default"]) {
			modelConf = m
			break
		}
	}
	if modelConf == nil && len(defaults) > 0 {
		modelConf = defaults[0]
	}
	if modelConf == nil {
		modelConf = map[string]any{}
	}

	// Python: client_config = model_conf.get("model_client_config", {})
	clientConfig, _ := modelConf["model_client_config"].(map[string]any)
	if clientConfig == nil {
		clientConfig = map[string]any{}
	}
	// Python: req_config = model_conf.get("model_config_obj", {})
	reqConfig, _ := modelConf["model_config_obj"].(map[string]any)
	if reqConfig == nil {
		reqConfig = map[string]any{}
	}

	// Python: if client_config.get("custom_headers") == "": del client_config["custom_headers"]
	if s, ok := clientConfig["custom_headers"].(string); ok && s == "" {
		delete(clientConfig, "custom_headers")
	}
	// Python: 四 setdefault 防 ValidationError
	if _, ok := clientConfig["api_key"]; !ok {
		clientConfig["api_key"] = ""
	}
	if _, ok := clientConfig["api_base"]; !ok {
		clientConfig["api_base"] = ""
	}
	if _, ok := clientConfig["client_provider"]; !ok {
		clientConfig["client_provider"] = "OpenAI"
	}
	if _, ok := clientConfig["model_name"]; !ok {
		clientConfig["model_name"] = "default"
	}
	if _, ok := reqConfig["model"]; !ok {
		if mn, ok := clientConfig["model_name"].(string); ok {
			reqConfig["model"] = mn
		}
	}

	// 复用 NewModelClientConfig + NewModel（参照 adapter buildModelFromEntry 模式，
	// 不 import adapter 的方法——其为 adapter 私有）
	provider, _ := clientConfig["client_provider"].(string)
	apiKey, _ := clientConfig["api_key"].(string)
	apiBase, _ := clientConfig["api_base"].(string)
	modelName, _ := clientConfig["model_name"].(string)

	clientConfigOpts := []llmschema.ModelClientConfigOption{
		llmschema.WithClientID("wiki_default_model"),
	}
	// 数值字段双类型兼容：本仓 YAML 库整数解出为 int（JSON 恒 float64），
	// 两种来源都要能透传（对齐 buildModelFromEntry 的 switch 模式）。
	if v, ok := clientConfig["timeout"]; ok {
		switch n := v.(type) {
		case float64:
			clientConfigOpts = append(clientConfigOpts, llmschema.WithTimeout(n))
		case int:
			clientConfigOpts = append(clientConfigOpts, llmschema.WithTimeout(float64(n)))
		}
	}
	if v, ok := clientConfig["verify_ssl"].(bool); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithVerifySSL(v))
	}
	// 对齐 Python ModelClientConfig(**client_config) 全字段透传——
	// max_retries / ssl_cert / custom_headers 此前漏映射（2026-10-28 审查 M-01），
	// 配置了内部网关鉴权头或自签 CA 证书的部署会以错误参数调用模型。
	if v, ok := clientConfig["max_retries"]; ok {
		switch n := v.(type) {
		case float64:
			clientConfigOpts = append(clientConfigOpts, llmschema.WithMaxRetries(int(n)))
		case int:
			clientConfigOpts = append(clientConfigOpts, llmschema.WithMaxRetries(n))
		}
	}
	if v, ok := clientConfig["ssl_cert"].(string); ok && v != "" {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithSSLCert(v))
	}
	if headers := wikiCustomHeadersToStringMap(clientConfig["custom_headers"]); headers != nil {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithCustomHeaders(headers))
	}
	mcc, err := llmschema.NewModelClientConfig(provider, apiKey, apiBase, clientConfigOpts...)
	if err != nil {
		return nil, err
	}

	// Python: req_config.setdefault("model", client_config["model_name"])
	reqModel, _ := reqConfig["model"].(string)
	if reqModel == "" {
		reqModel = modelName
	}
	// 对齐 Python ModelRequestConfig(**req_config) 全字段透传——
	// temperature / top_p / max_tokens / stop 此前漏映射（M-01）。
	reqOpts := []llmschema.ModelRequestConfigOption{llmschema.WithModelName(reqModel)}
	if v, ok := reqConfig["temperature"]; ok {
		switch n := v.(type) {
		case float64:
			reqOpts = append(reqOpts, llmschema.WithTemperature(n))
		case int:
			reqOpts = append(reqOpts, llmschema.WithTemperature(float64(n)))
		}
	}
	if v, ok := reqConfig["top_p"]; ok {
		switch n := v.(type) {
		case float64:
			reqOpts = append(reqOpts, llmschema.WithTopP(n))
		case int:
			reqOpts = append(reqOpts, llmschema.WithTopP(float64(n)))
		}
	}
	if v, ok := reqConfig["max_tokens"]; ok {
		switch n := v.(type) {
		case float64:
			reqOpts = append(reqOpts, llmschema.WithMaxTokens(int(n)))
		case int:
			reqOpts = append(reqOpts, llmschema.WithMaxTokens(n))
		}
	}
	if v, ok := reqConfig["stop"].(string); ok && v != "" {
		reqOpts = append(reqOpts, llmschema.WithStop(v))
	}
	mco := llmschema.NewModelRequestConfig(reqOpts...)
	return llm.NewModel(mcc, mco)
}

// wikiCustomHeadersToStringMap 将 wiki 默认模型的 custom_headers 配置转为 map[string]string。
// YAML 解出为 map[string]any，llmschema.WithCustomHeaders 签名收 map[string]string；
// 非字符串值跳过（对齐 runtime_inheritance.customHeadersToStringMap 的宽松语义，
// 该函数为 team 包私有无法导入，此处内联同款逻辑）。
func wikiCustomHeadersToStringMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			result[k] = s
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// resolveWikiWorkspace 解析 workspace 为绝对路径（四分支）。
// Python: _resolve_workspace (wiki_tools.py L376-393)
func resolveWikiWorkspace(workspaceDir string) string {
	cleanedWorkspace := strings.TrimSpace(workspaceDir)
	baseDir := workspace.AgentWorkspaceDir()

	if cleanedWorkspace == "" || cleanedWorkspace == defaultWikiDir {
		return resolvePathStringWiki(filepath.Join(baseDir, defaultWikiDir))
	}

	workspacePath := expandUserHomeWiki(cleanedWorkspace)
	if !filepath.IsAbs(workspacePath) {
		workspacePath = filepath.Join(baseDir, workspacePath)
	}

	if filepath.Base(workspacePath) != defaultWikiDir {
		workspacePath = filepath.Join(workspacePath, defaultWikiDir)
	}

	return resolvePathStringWiki(workspacePath)
}

// newWikiIngestTool 构造 wiki_ingest 工具。
// Python: wiki_ingest (wiki_tools.py L396-451)
func newWikiIngestTool() tool.Tool {
	card := tool.NewToolCard(
		"wiki_ingest",
		"Ingest a source file (PDF, TXT, MD) or a directory into the LLM Wiki."+
			" By default, identical files (by SHA-256) are skipped perfectly (deduplication)."+
			" Set `force=True` if the user explicitly asks to re-ingest, rebuild, or force the ingestion."+
			" The `workspace` parameter is the root folder where the `.llm_wiki` will be created."+
			" If the user specifies a target directory like 'wiki_lib' or 'bibliography', pass that exact path as `workspace`.",
		nil, // InputParams 由 NewTool 从 input struct 反射生成
		nil,
	)

	fn := func(ctx context.Context, input wikiIngestInput, opts ...tool.ToolOption) (map[string]any, error) {
		// Python: try/except → f"Wiki Ingest Error: {str(e)}"
		output, _ := withWikiRecover(func() (string, error) {
			return invokeWikiIngest(ctx, input)
		}, "Wiki Ingest Error")
		return map[string]any{"result": output}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// invokeWikiIngest wiki_ingest 主流程（不含 recover）。
// Python: wiki_ingest 函数体 (wiki_tools.py L411-451)
func invokeWikiIngest(ctx context.Context, input wikiIngestInput) (string, error) {
	model, err := getDefaultWikiModel()
	if err != nil {
		return "", err
	}
	finalWorkspace := resolveWikiWorkspace(input.Workspace)
	// Python: sys_operation 参数恒 None（spec 决策 5）
	wiki, err := newLLMWiki(ctx, finalWorkspace, model, nil)
	if err != nil {
		return "", err
	}
	if err := wiki.ensureInitialized(ctx); err != nil {
		return "", err
	}

	// Python: src_path = Path(source).expanduser(); 相对 → AgentWorkspaceDir 基
	srcPath := expandUserHomeWiki(input.Source)
	if !filepath.IsAbs(srcPath) {
		srcPath = filepath.Join(workspace.AgentWorkspaceDir(), srcPath)
	}

	if _, statErr := os.Stat(srcPath); statErr != nil {
		return fmt.Sprintf("Error: Source %s not found.", input.Source), nil
	}

	// Python: protected_root = str(final_workspace).lower()
	protectedRoot := strings.ToLower(finalWorkspace)

	var targets []string
	info, _ := os.Stat(srcPath)
	if info != nil && info.IsDir() {
		// Python: for ext in (".pdf", ".md", ".txt"): glob(f"**/*{ext}")
		_ = filepath.WalkDir(srcPath, func(p string, d os.DirEntry, wErr error) error {
			if wErr != nil || d == nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".pdf" && ext != ".md" && ext != ".txt" {
				return nil
			}
			// Python: posix_path.lower().startswith(protected_root) 跳过 .llm_wiki 内部文件
			posixPath := strings.ToLower(filepath.ToSlash(p))
			if strings.HasPrefix(posixPath, protectedRoot) {
				return nil
			}
			targets = append(targets, p)
			return nil
		})
	} else {
		targets = append(targets, srcPath)
	}

	allResults := map[string]string{}
	for _, filePath := range targets {
		res, iErr := wiki.ingest(ctx, filePath, input.Force)
		if iErr != nil {
			allResults[filePath] = fmt.Sprintf("[Failed]: %s", iErr.Error())
			continue
		}
		if errVal, hasErr := res["error"]; hasErr {
			allResults[filePath] = fmt.Sprintf("[Failed]: %v", errVal)
		} else if skipped, ok := res["skipped"].(bool); ok && skipped {
			allResults[filePath] = "[Skipped]: Deduplicated"
		} else {
			allResults[filePath] = "[Success]"
		}
	}

	// Python: "Wiki Ingestion Results:\n" + json.dumps(all_results, indent=2)
	data, _ := json.MarshalIndent(allResults, "", "  ")
	return "Wiki Ingestion Results:\n" + string(data), nil
}

// newWikiQueryTool 构造 wiki_query 工具。
// Python: wiki_query (wiki_tools.py L454-484)
func newWikiQueryTool() tool.Tool {
	card := tool.NewToolCard(
		"wiki_query",
		"Query the LLM Wiki's compiled knowledge base directly via Natural Language."+
			" The `workspace` parameter is the folder containing the `.llm_wiki`."+
			" If the user specifies a target directory like 'wiki_lib' or 'bibliography', pass that exact path as `workspace`.",
		nil,
		nil,
	)

	fn := func(ctx context.Context, input wikiQueryInput, opts ...tool.ToolOption) (map[string]any, error) {
		// Python: 空 query 检查在 try 之外
		if strings.TrimSpace(input.Query) == "" {
			return map[string]any{"result": "Error: Query cannot be empty."}, nil
		}
		output, err := withWikiRecover(func() (string, error) {
			return invokeWikiQuery(ctx, input)
		}, "Wiki Query Error")
		_ = err
		return map[string]any{"result": output}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// invokeWikiQuery wiki_query 主流程。
// Python: wiki_query 函数体 (wiki_tools.py L466-484)
func invokeWikiQuery(ctx context.Context, input wikiQueryInput) (string, error) {
	model, err := getDefaultWikiModel()
	if err != nil {
		return "", err
	}
	finalWorkspace := resolveWikiWorkspace(input.Workspace)
	if _, statErr := os.Stat(finalWorkspace); statErr != nil {
		// Python: 引导文案逐字 (wiki_tools.py L470-473)
		return fmt.Sprintf(
			"Error: The workspace '%s' does not have an initialized LLM Wiki."+
				" You must use 'wiki_ingest' first to create the knowledge base.",
			input.Workspace,
		), nil
	}
	wiki, err := newLLMWiki(ctx, finalWorkspace, model, nil)
	if err != nil {
		return "", err
	}
	if err := wiki.ensureInitialized(ctx); err != nil {
		return "", err
	}

	result, err := wiki.query(ctx, input.Query)
	if err != nil {
		return "", err
	}
	return wikiResultToText(result, "Error querying wiki"), nil
}

// newWikiLintTool 构造 wiki_lint 工具。
// Python: wiki_lint (wiki_tools.py L487-513)
func newWikiLintTool() tool.Tool {
	card := tool.NewToolCard(
		"wiki_lint",
		"Health-check and trigger automatic repairs of broken links, orphans, or anomalies in the LLM Wiki."+
			" The `workspace` parameter is the root folder containing the `.llm_wiki`."+
			" If the user specifies a target directory like 'wiki_lib' or 'bibliography', pass that exact path as `workspace`.",
		nil,
		nil,
	)

	fn := func(ctx context.Context, input wikiLintInput, opts ...tool.ToolOption) (map[string]any, error) {
		output, err := withWikiRecover(func() (string, error) {
			return invokeWikiLint(ctx, input)
		}, "Wiki Lint Error")
		_ = err
		return map[string]any{"result": output}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// invokeWikiLint wiki_lint 主流程。
// Python: wiki_lint 函数体 (wiki_tools.py L495-513)
func invokeWikiLint(ctx context.Context, input wikiLintInput) (string, error) {
	model, err := getDefaultWikiModel()
	if err != nil {
		return "", err
	}
	finalWorkspace := resolveWikiWorkspace(input.Workspace)
	if _, statErr := os.Stat(finalWorkspace); statErr != nil {
		return fmt.Sprintf(
			"Error: The workspace '%s' does not have an initialized LLM Wiki."+
				" You must use 'wiki_ingest' first to create the knowledge base.",
			input.Workspace,
		), nil
	}
	wiki, err := newLLMWiki(ctx, finalWorkspace, model, nil)
	if err != nil {
		return "", err
	}
	if err := wiki.ensureInitialized(ctx); err != nil {
		return "", err
	}

	result, err := wiki.lint(ctx)
	if err != nil {
		return "", err
	}
	return wikiResultToText(result, "Error linting wiki"), nil
}

// wikiResultToText agent 结果转文案。
// Python: if "output" in result → str; elif "error" → f"Error ...: {err}"; else MarshalIndent
func wikiResultToText(result map[string]any, errorPrefix string) string {
	if output, ok := result["output"]; ok {
		return fmt.Sprintf("%v", output)
	}
	if errVal, ok := result["error"]; ok {
		return fmt.Sprintf("%s: %v", errorPrefix, errVal)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data)
}

// withWikiRecover 顶层 recover → "{prefix}: {err}"。
// Python: except Exception as e → f"Wiki Xxx Error: {str(e)}"
func withWikiRecover(fn func() (string, error), prefix string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("%s: %v", prefix, r)
			err = nil
		}
	}()
	out, err = fn()
	if err != nil {
		out = fmt.Sprintf("%s: %s", prefix, err.Error())
		err = nil
	}
	return out, nil
}

// resolvePathStringWiki 路径 resolve（对齐 Python Path.resolve 的 strict=False 语义）。
func resolvePathStringWiki(p string) string {
	return upath.ResolveSymlinksLenient(p)
}

// expandUserHomeWiki ~/ 展开。
// Python: Path.expanduser()
func expandUserHomeWiki(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// copyFilePreserveMeta 复制文件并保留 mtime（对齐 shutil.copy2）。
// Python: shutil.copy2(source_path, destination)
func copyFilePreserveMeta(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	// 保留修改时间（copy2 语义）
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// isTruthyWiki is_default 字段真值判断。
func isTruthyWiki(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		return s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
	}
	return false
}
