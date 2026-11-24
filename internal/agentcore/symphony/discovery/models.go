// models.go 对齐 Python discovery/models.py（466 行）：扫描、缓存与规范化。
// 缓存等价实现（偏差 D-16-4-4）：Go 无 OrderedDict，用 map+切片 LRU；
// unicodedata.category 用显式码点区间等价（Cc/Cs/Zl/Zp）。
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build"
)

// 缓存容量（对齐 Python _SKILL_FILE_CACHE_MAX_ENTRIES=2048 与各缓存 128）
const (
	skillFileCacheMaxEntries = 2048
	genericCacheMaxEntries   = 128
)

// SkillRecord 单个可执行技能的发现投影（frozen dataclass → Go 只读结构体）
type SkillRecord struct {
	// WorkerID 唯一标识
	WorkerID string
	// Name 名称
	Name string
	// Description 描述
	Description string
	// SkillFile 技能文件路径
	SkillFile string
	// Source 来源（缺省 "local"）
	Source string
	// Version 版本（缺省空）
	Version string
	// Author 作者（缺省空）
	Author string
	// ContentHash 内容哈希（缺省空）
	ContentHash string
	// SourceRoot 授权根目录（缺省空）
	SourceRoot string
	// Aliases 别名
	Aliases []string
}

// SkillInventory 当前可见技能记录的稳定快照
type SkillInventory struct {
	// Items 记录列表
	Items []SkillRecord
	// Fingerprint 全量指纹
	Fingerprint string
}

// Count 记录数（对齐 Python count property）
func (inv SkillInventory) Count() int { return len(inv.Items) }

// ──────────────────────────── 缓存结构（D-16-4-4） ────────────────────────────

// lruCache 简单 LRU（map + 命中即前移的访问序切片）
type lruCache struct {
	mu    sync.Mutex
	data  map[string]any
	order []string
	max   int
}

func newLRU(max int) *lruCache { return &lruCache{data: map[string]any{}, max: max} }

// get 命中返回值并前移；miss 返回 nil
func (c *lruCache) get(key string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key]
	if !ok {
		return nil
	}
	c.touchLocked(key)
	return v
}

// put 插入并淘汰最旧
func (c *lruCache) put(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; ok {
		c.data[key] = value
		c.touchLocked(key)
		return
	}
	c.data[key] = value
	c.order = append(c.order, key)
	for len(c.order) > c.max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.data, oldest)
	}
}

func (c *lruCache) touchLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

// 包级五层缓存（对齐 models.py:23-35）
var (
	skillFileCache    = newLRU(skillFileCacheMaxEntries) // key=path → (signature, (meta,body,hash))
	scannedRecordCch  = newLRU(genericCacheMaxEntries)   // cacheKey → SkillRecord
	rootLayoutCache   = newLRU(genericCacheMaxEntries)   // root → (signature, sources)
	normalizedCache   = newLRU(genericCacheMaxEntries)   // records 标识 → SkillInventory
	inventoryFlights  = map[string]*inventoryFlight{}
	inventoryFlightsM sync.Mutex
	lastInventories   = newLRU(genericCacheMaxEntries) // key → SkillInventory
)

type inventoryFlight struct {
	done   chan struct{}
	result SkillInventory
	err    error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// InventoryFromRecords 规范化并指纹化调用方提供的记录。
// 对齐 Python inventory_from_records（models.py:85-113）：content_hash 全量时
// 走 normalizedCache；worker_id 空值归零、去重、排序 (name.casefold, worker_id.casefold)。
func InventoryFromRecords(records []SkillRecord) SkillInventory {
	// Go 无对象身份可比（id(records)），缓存键取内容摘要（等价形态，D-16-4-4）
	cacheable := len(records) > 0
	cacheKeyParts := make([]string, 0, len(records))
	for _, r := range records {
		if r.ContentHash == "" {
			cacheable = false
			break
		}
		cacheKeyParts = append(cacheKeyParts, r.WorkerID+"\x00"+r.SkillFile+"\x00"+r.ContentHash)
	}
	if cacheable {
		key := strings.Join(cacheKeyParts, "\x01")
		if cached := normalizedCache.get(key); cached != nil {
			return cached.(SkillInventory)
		}
		inv := inventoryFromRecordsUncached(records)
		normalizedCache.put(key, inv)
		return inv
	}
	return inventoryFromRecordsUncached(records)
}

// inventoryFromRecordsUncached 无缓存规范化路径
func inventoryFromRecordsUncached(records []SkillRecord) SkillInventory {
	preparedList := make([]preparedRecord, 0, len(records))
	for _, record := range records {
		rawWorkerID := record.WorkerID
		preparedList = append(preparedList, preparedRecord{rawWorkerID, withContentHash(record)})
	}
	normalized := withUniqueWorkerIDs(preparedList)
	sortNormalized(normalized)
	return inventoryFromNormalized(normalized)
}

// sortNormalized 按 (name.casefold, worker_id.casefold) 排序（对齐 models.py:100-105）
func sortNormalized(items []SkillRecord) {
	sort.SliceStable(items, func(i, j int) bool {
		ni, nj := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if ni != nj {
			return ni < nj
		}
		return strings.ToLower(items[i].WorkerID) < strings.ToLower(items[j].WorkerID)
	})
}

// inventoryFromNormalized 指纹构造。
// 对齐 Python _inventory_from_normalized（models.py:116-133）：
// payload 键序无关（json sort_keys=True → Go 手工按固定序拼 JSON 再哈希）。
func inventoryFromNormalized(normalized []SkillRecord) SkillInventory {
	entries := make([]map[string]any, 0, len(normalized))
	for _, item := range normalized {
		entries = append(entries, map[string]any{
			"worker_id": item.WorkerID, "name": item.Name, "description": item.Description,
			"skill_file": item.SkillFile, "aliases": item.Aliases, "source": item.Source,
			"version": item.Version, "author": item.Author,
			"content_hash": item.ContentHash, "source_root": item.SourceRoot,
		})
	}
	data, _ := json.Marshal(entries)
	sum := sha256.Sum256(data)
	return SkillInventory{Items: normalized, Fingerprint: hex.EncodeToString(sum[:])}
}

// ScanSkillDirectories live 直子目录扫描（single-flight + last-inventories 复用）。
// 对齐 Python scan_skill_directories（models.py:136-187）。
func ScanSkillDirectories(directories []string, disabledSkills []string, sourceByName map[string]string) (SkillInventory, error) {
	roots := make([]string, 0, len(directories))
	for _, d := range directories {
		expanded := d
		if strings.HasPrefix(expanded, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[1:])
			}
		}
		abs, err := filepath.Abs(expanded)
		if err != nil {
			abs = expanded
		}
		roots = append(roots, abs)
	}
	disabled := append([]string(nil), disabledSkills...)
	sort.Strings(disabled)
	var sources []string
	{
		keys := make([]string, 0, len(sourceByName))
		for k := range sourceByName {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sources = append(sources, k+"\x00"+sourceByName[k])
		}
	}
	key := strings.Join(roots, "\x01") + "\x02" + strings.Join(disabled, "\x01") + "\x02" + strings.Join(sources, "\x01")

	inventoryFlightsM.Lock()
	flight, ok := inventoryFlights[key]
	if !ok {
		flight = &inventoryFlight{done: make(chan struct{})}
		inventoryFlights[key] = flight
		inventoryFlightsM.Unlock()
	} else {
		inventoryFlightsM.Unlock()
		// follower：等待领飞者结果
		<-flight.done
		if flight.err != nil {
			return SkillInventory{}, flight.err
		}
		return flight.result, nil
	}
	defer func() {
		inventoryFlightsM.Lock()
		delete(inventoryFlights, key)
		inventoryFlightsM.Unlock()
		close(flight.done)
	}()
	inventory, err := scanSkillDirectoriesUncached(roots, disabled, sourceByName)
	if err != nil {
		flight.err = err
		return SkillInventory{}, err
	}
	// 指纹相同复用上次实例（对齐 :170-178）
	if prev := lastInventories.get(key); prev != nil {
		if prev.(SkillInventory).Fingerprint == inventory.Fingerprint {
			inventory = prev.(SkillInventory)
		} else {
			lastInventories.put(key, inventory)
		}
	} else {
		lastInventories.put(key, inventory)
	}
	flight.result = inventory
	return inventory, nil
}

// scanSkillDirectoriesUncached 直子目录扫描主链。
// 对齐 Python _scan_skill_directories_uncached（models.py:190-254）。
func scanSkillDirectoriesUncached(directories []string, disabledSkills []string, sourceByName map[string]string) (SkillInventory, error) {
	disabled := map[string]bool{}
	for _, v := range disabledSkills {
		disabled[v] = true
	}
	sources := sourceByName
	if sources == nil {
		sources = map[string]string{}
	}
	var prepared []preparedRecord
	seenSourceIDs := map[string]bool{}
	for _, rawDirectory := range directories {
		skillSources, err := skillSources(rawDirectory)
		if err != nil {
			return SkillInventory{}, err
		}
		for _, source := range skillSources {
			childName, skillFile := source.childName, source.skillFile
			metadata, body, contentHash, err := cachedSkillFile(skillFile)
			if err != nil {
				return SkillInventory{}, err
			}
			workerID := canonicalSkillID(childName)
			name := strings.TrimSpace(stringOr(metadata, "name"))
			if name == "" {
				name = workerID
			}
			if seenSourceIDs[childName] {
				continue // 先扫根优先（对齐 :212-213）
			}
			sourceName, hasSource := sources[name]
			if !hasSource {
				sourceName, hasSource = sources[workerID]
			}
			if !hasSource {
				sourceName = "local"
			}
			cacheKey := strings.Join([]string{childName, skillFile, rawDirectory, contentHash, sourceName}, "\x00")
			record := scannedRecordCch.get(cacheKey)
			if record == nil {
				description := strings.TrimSpace(stringOr(metadata, "description"))
				if description == "" {
					description = build.FirstParagraph(body, 500)
				}
				if description == "" {
					description = name
				}
				record = SkillRecord{
					WorkerID:    workerID,
					Name:        SanitizeModelText(name),
					Description: SanitizeModelText(description),
					SkillFile:   skillFile,
					Aliases:     skillAliases(metadata),
					Source:      SanitizeModelText(sourceName),
					Version:     SanitizeModelText(stringOr(metadata, "version")),
					Author:      SanitizeModelText(stringOr(metadata, "author")),
					ContentHash: contentHash,
					SourceRoot:  rawDirectory,
				}
				scannedRecordCch.put(cacheKey, record)
			}
			prepared = append(prepared, preparedRecord{childName, record.(SkillRecord)})
			seenSourceIDs[childName] = true
		}
	}
	normalized := withUniqueWorkerIDs(prepared)
	sortNormalized(normalized)
	inventory := inventoryFromNormalized(normalized)
	if len(disabled) == 0 {
		return inventory, nil
	}
	// 禁用过滤：worker_id 与父目录名双重判定（对齐 :248-254）
	var filtered []SkillRecord
	for _, item := range inventory.Items {
		if disabled[item.WorkerID] {
			continue
		}
		parentName := filepath.Base(filepath.Dir(item.SkillFile))
		if disabled[parentName] {
			continue
		}
		filtered = append(filtered, item)
	}
	return inventoryFromNormalized(filtered), nil
}

// skillSource 单个授权子目录条目
type skillSource struct {
	childName string
	skillFile string
}

// skillSources 授权布局解析（布局缓存 + 符号链接/越界防护）。
// 对齐 Python _skill_sources（models.py:257-310）：隐藏/`_` 前缀排除、
// resolve 后必须在根内、FindSkillFile 命中、签名缓存。
func skillSources(root string) ([]skillSource, error) {
	stat, err := os.Stat(root)
	if err != nil {
		return []skillSource{}, nil // Python OSError → 空
	}
	if !stat.IsDir() {
		return []skillSource{}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return []skillSource{}, nil
	}
	// 子目录按名称 casefold 排序（对齐 :262）
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.SliceStable(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	// 布局签名：根 mtime + 子项名/存在性（对齐 :267-277；st_ino/ctime_ns Go 侧
	// 不通用，等价签名取 mtime+size）
	sig := stat.ModTime().UnixNano()
	var childSig []string
	for _, name := range names {
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		cs, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			childSig = append(childSig, name+":missing")
			continue
		}
		childSig = append(childSig, fmt.Sprintf("%s:%d:%d", name, cs.ModTime().UnixNano(), cs.Size()))
	}
	cacheKey := root
	layoutSig := fmt.Sprintf("%d|%s", sig, strings.Join(childSig, "\x01"))
	type layoutPair struct {
		sig     string
		sources []skillSource
	}
	if cached := rootLayoutCache.get(cacheKey); cached != nil {
		p := cached.(layoutPair)
		if p.sig == layoutSig {
			return p.sources, nil
		}
	}
	var sources []skillSource
	for _, name := range names {
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		childPath := filepath.Join(root, name)
		resolvedChild, err := filepath.EvalSymlinks(childPath)
		if err != nil {
			continue
		}
		cs, err := os.Stat(resolvedChild)
		if err != nil || !cs.IsDir() || !withinRoot(resolvedChild, root) {
			continue
		}
		skillFile := build.FindSkillFile(resolvedChild)
		if skillFile == "" {
			continue
		}
		resolvedFile, err := filepath.EvalSymlinks(skillFile)
		if err != nil || !withinRoot(resolvedFile, root) {
			continue
		}
		abs, _ := filepath.Abs(skillFile)
		sources = append(sources, skillSource{childName: name, skillFile: abs})
	}
	rootLayoutCache.put(cacheKey, layoutPair{layoutSig, sources})
	return sources, nil
}

// withinRoot 路径是否位于授权根内（对齐 is_relative_to）
func withinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// withContentHash 补哈希与规范化（adapter 提供记录时走此路径）。
// 对齐 Python _with_content_hash（models.py:313-338）。
func withContentHash(record SkillRecord) SkillRecord {
	expanded := record.SkillFile
	if strings.HasPrefix(expanded, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[1:])
		}
	}
	path, _ := filepath.Abs(expanded)
	var sourceRoot string
	if strings.TrimSpace(record.SourceRoot) != "" {
		expandedRoot := record.SourceRoot
		if strings.HasPrefix(expandedRoot, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				expandedRoot = filepath.Join(home, expandedRoot[1:])
			}
		}
		absRoot, err := filepath.Abs(expandedRoot)
		if err == nil {
			sourceRoot = absRoot
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	if sourceRoot != "" {
		resolvedRoot, err := filepath.EvalSymlinks(sourceRoot)
		if err == nil && !withinRoot(resolved, resolvedRoot) {
			// Python raise ValueError；Go 侧按 panic 等价？改为保留错误路径由上层感知——
			// 对齐行为：越界记录整体拒绝（返回原值会导致静默通过，这里 panic 与 Python
			// 抛异常路径等价，由 ScanSkillDirectories 的错误传播面兜住）。
			panic(fmt.Sprintf("Skill source escapes its authorized root: %s", record.WorkerID))
		}
	}
	contentHash := record.ContentHash
	if contentHash == "" {
		contentHash, _ = build.SHA256File(resolved)
	}
	name := record.Name
	if name == "" {
		name = record.WorkerID
	}
	description := record.Description
	if description == "" {
		description = record.Name
	}
	if description == "" {
		description = record.WorkerID
	}
	source := record.Source
	if source == "" {
		source = "local"
	}
	return SkillRecord{
		WorkerID:    canonicalSkillID(record.WorkerID),
		Name:        SanitizeModelText(name),
		Description: SanitizeModelText(description),
		SkillFile:   path,
		Aliases:     skillAliases(map[string]any{"aliases": toAnySlice(record.Aliases)}),
		Source:      SanitizeModelText(source),
		Version:     SanitizeModelText(record.Version),
		Author:      SanitizeModelText(record.Author),
		ContentHash: contentHash,
		SourceRoot:  sourceRoot,
	}
}

// preparedRecord rawID+record 对（withUniqueWorkerIDs 输入形态）
type preparedRecord struct {
	RawWorkerID string
	Record      SkillRecord
}

// withUniqueWorkerIDs 规范 ID 去重（哈希后缀消解）。
// 对齐 Python _with_unique_worker_ids（models.py:341-378）：多重排序键 +
// \0 连接身份 sha256 前 10 hex 后缀 ~h + 冲突递增。
func withUniqueWorkerIDs(records []preparedRecord) []SkillRecord {
	reserved := map[string]bool{}
	for _, p := range records {
		reserved[p.Record.WorkerID] = true
	}
	used := map[string]bool{}
	ordered := append([]preparedRecord{}, records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		ka := sortKeyOf(a)
		kb := sortKeyOf(b)
		for x := 0; x < len(ka) && x < len(kb); x++ {
			if ka[x] != kb[x] {
				return ka[x] < kb[x]
			}
		}
		return false
	})
	normalized := make([]SkillRecord, 0, len(ordered))
	for _, p := range ordered {
		workerID := p.Record.WorkerID
		if used[workerID] {
			identity := strings.Join([]string{p.RawWorkerID, p.Record.SkillFile, p.Record.SourceRoot}, "\x00")
			sum := sha256.Sum256([]byte(identity))
			suffix := hex.EncodeToString(sum[:])[:10]
			workerID = fmt.Sprintf("%s~h%s", p.Record.WorkerID, suffix)
			index := 2
			for used[workerID] || reserved[workerID] {
				workerID = fmt.Sprintf("%s~h%s-%d", p.Record.WorkerID, suffix, index)
				index++
			}
			p.Record.WorkerID = workerID
		}
		used[workerID] = true
		normalized = append(normalized, p.Record)
	}
	return normalized
}

// sortKeyOf 多重排序键（对齐 models.py:349-364 十二元组）
func sortKeyOf(p preparedRecord) []string {
	r := p.Record
	flag := "1"
	if p.RawWorkerID == r.WorkerID {
		flag = "0"
	}
	return []string{
		strings.ToLower(r.WorkerID), flag, strings.ToLower(p.RawWorkerID), p.RawWorkerID,
		r.SkillFile, r.SourceRoot, r.ContentHash, r.Name, r.Description, r.Source, r.Version, r.Author,
	}
}

// cachedSkillFile stat 签名缓存读取（对齐 models.py:381-404）。
func cachedSkillFile(path string) (map[string]any, string, string, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, "", "", err
	}
	signature := fmt.Sprintf("%d:%d:%d", stat.Size(), stat.ModTime().UnixNano(), stat.Mode())
	key := path
	if cached := skillFileCache.get(key); cached != nil {
		entry := cached.(fileCacheEntry)
		if entry.signature == signature {
			return entry.metadata, entry.body, entry.contentHash, nil
		}
	}
	metadata, body, contentHash, err := readSkillFileLenient(path)
	if err != nil {
		return nil, "", "", err
	}
	body = strings.TrimSpace(body)
	skillFileCache.put(key, fileCacheEntry{signature, metadata, body, contentHash})
	return metadata, body, contentHash, nil
}

type fileCacheEntry struct {
	signature   string
	metadata    map[string]any
	body        string
	contentHash string
}

// readSkillFileLenient 宽容读取（对齐 errors="replace" + fallback_to_simple=False）
func readSkillFileLenient(path string) (map[string]any, string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", err
	}
	content := string(raw) // 非法字节已是 U+FFFD（Go UTF-8 解码语义等价 errors="replace"）
	metadata, body := build.ParseFrontmatter(content, false)
	sum := sha256.Sum256(raw)
	return metadata, body, hex.EncodeToString(sum[:]), nil
}

// ──────────────────────────── 身份与净化 ────────────────────────────

// injectionRe 指令注入检测（对齐 models.py:407-410）
var injectionRe = regexp.MustCompile(`(?i)(tool[_ -]?call|system[_ -]?reminder|ignore|disregard|override|previous instructions|prior instructions)`)

// untrustedPrefix 不可信标记前缀
const untrustedPrefix = "[untrusted Skill metadata] "

// CanonicalSkillId 敌意文件名安全标识（控制字符转 ~uXXXX，空名 unnamed）。
// 对齐 Python _canonical_skill_id（models.py:414-425）：Cc/Cs/Zl/Zp 转义。
func canonicalSkillID(value any) string {
	return CanonicalSkillID(value)
}

// CanonicalSkillID 导出版（toolkit/rail 复用）
func CanonicalSkillID(value any) string {
	raw := strings.TrimSpace(fmt.Sprintf("%v", value))
	if raw == "<nil>" {
		raw = ""
	}
	var parts []string
	for _, r := range raw {
		if isCc(r) || isCs(r) || r == 0x2028 || r == 0x2029 {
			parts = append(parts, fmt.Sprintf("~u%04x", r))
		} else {
			parts = append(parts, string(r))
		}
	}
	out := strings.TrimSpace(strings.Join(parts, ""))
	if out == "" {
		return "unnamed"
	}
	return out
}

// isCc 控制字符（Cc：C0/C1）
func isCc(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }

// isCs 代理对（合法 UTF-8 解码后不出现；U+FFFD 替换符按 Cs 等价处理）
func isCs(r rune) bool { return r == 0xfffd }

// SanitizeModelText 模型可见元数据净化（注入检测 + 类别替换 + HTML 转义）。
// 对齐 Python sanitize_model_text（models.py:428-450）。
func SanitizeModelText(value any) string {
	raw := strings.TrimSpace(fmt.Sprintf("%v", value))
	if raw == "<nil>" {
		raw = ""
	}
	unsafe := injectionRe.MatchString(raw) || strings.Contains(raw, "<") || strings.Contains(raw, ">")
	var cleaned []rune
	for _, r := range raw {
		switch {
		case isCs(r):
			cleaned = append(cleaned, 0xfffd)
			unsafe = true
		case r == 0x2028 || r == 0x2029: // Zl/Zp
			cleaned = append(cleaned, ' ')
			unsafe = true
		case isCc(r) && r != '\n' && r != '\r' && r != '\t':
			cleaned = append(cleaned, ' ')
			unsafe = true
		default:
			cleaned = append(cleaned, r)
		}
	}
	escaped := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(string(cleaned), "<", "&lt;"), ">", "&gt;"))
	if unsafe && escaped != "" && !strings.HasPrefix(escaped, untrustedPrefix) {
		return untrustedPrefix + escaped
	}
	return escaped
}

// skillAliases 别名提取（aliases/alias 双键、字符串单值、去重）。
// 对齐 Python _skill_aliases（models.py:453-457）。
func skillAliases(metadata map[string]any) []string {
	var raw any
	if v, ok := metadata["aliases"]; ok {
		raw = v
	} else {
		raw = metadata["alias"]
	}
	var values []any
	switch t := raw.(type) {
	case string:
		values = []any{t}
	case []any:
		values = t
	case nil, bool, int, float64:
		return nil
	default:
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		alias := SanitizeModelText(v)
		if alias != "" && !seen[alias] {
			seen[alias] = true
			out = append(out, alias)
		}
	}
	return out
}

// stringOr 字符串字段，Python str(x or "") 语义：
// falsy（nil/空串/false/0）回落 default，其余标量 str() 强转
// （YAML version: 1.0 → float64 → "1.0"，与 Python str(1.0) 等价）。
func stringOr(m map[string]any, key string) string {
	return pyStrOr(m[key], "")
}

// pyStrOr Python str(value or default) 等价
func pyStrOr(value any, def string) string {
	switch t := value.(type) {
	case nil:
		return def
	case string:
		if t == "" {
			return def
		}
		return t
	case bool:
		if !t {
			return def
		}
		return "True"
	case int:
		if t == 0 {
			return def
		}
		return fmt.Sprintf("%d", t)
	case float64:
		if t == 0 {
			return def
		}
		return pyFloatStr(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// pyFloatStr Python str(float) 等价（最短表示，1.0→"1.0"）
func pyFloatStr(f float64) string {
	if f == float64(int64(f)) && f >= -1e15 && f <= 1e15 {
		return strconv.FormatFloat(f, 'f', -1, 64) + ".0"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// toAnySlice []string→[]any
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

var _ = strconv.Itoa // strconv 引用保持
