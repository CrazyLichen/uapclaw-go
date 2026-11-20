package trajectory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// store.go 规范 Trajectory 快照的同步持久化。
// 对齐 Python: agent_evolving/trajectory/store.py（全文，2026-12-04 快照）。

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryStore 同步轨迹归档协议。
// Python: store.py TrajectoryStore(Protocol)。
type TrajectoryStore interface {
	// Save 持久化一个规范轨迹快照。version 用于实验隔离（可空）。
	Save(trajectory *Trajectory, version string) error
	// Load 加载最旧的匹配轨迹（按 trajectory_id），不存在返回 nil。
	Load(trajectoryID string, version string) (*Trajectory, error)
	// Query 返回匹配稳定资源元数据的快照。
	Query(version, sessionID, teamID, memberID, caseID, source string) ([]*Trajectory, error)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// versionName 校验并归一化版本目录名。
// Python: store.py _version_name。
func versionName(version string) (string, error) {
	if version == "" {
		return "default", nil
	}
	if version == "." || version == ".." || strings.ContainsAny(version, "/\\") {
		return "", fmt.Errorf("version must be a simple path component")
	}
	return version, nil
}

// queryFilters 收集非空过滤条件。
// Python: store.py _query_filters。
func queryFilters(sessionID, teamID, memberID, caseID, source string) map[string]string {
	filters := map[string]string{}
	for _, pair := range []struct{ key, value string }{
		{"session_id", sessionID}, {"team_id", teamID},
		{"member_id", memberID}, {"case_id", caseID}, {"source", source},
	} {
		if pair.value != "" {
			filters[pair.key] = pair.value
		}
	}
	return filters
}

// metadataValue 读轨迹的稳定元数据值（按查询键名）。
// Python: store.py _metadata_value。
func metadataValue(trajectory *Trajectory, key string) string {
	attributes := trajectory.ResourceAttributes()
	switch key {
	case "trajectory_id":
		return trajectory.TrajectoryID()
	case "session_id":
		return optionalAttrString(attributes[SessionID])
	case "team_id":
		return optionalAttrString(attributes[TeamID])
	case "member_id":
		return optionalAttrString(attributes[MemberID])
	case "case_id":
		return optionalAttrString(attributes[CaseID])
	case "source":
		value := optionalAttrString(attributes[TrajectorySource])
		if value == "" {
			return "offline"
		}
		return value
	default:
		return ""
	}
}

// optionalAttrString 属性值字符串化（nil 返空串）。
func optionalAttrString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryTrajectoryStore 进程内规范轨迹归档。
// Python: store.py InMemoryTrajectoryStore。
type InMemoryTrajectoryStore struct {
	// data version -> trajectory_id -> Trajectory。order 记录插入序（对齐
	// Python dict 插入序语义，Load 返回最旧匹配）。
	data  map[string]map[string]*Trajectory
	order map[string][]string
}

// NewInMemoryTrajectoryStore 创建内存轨迹存储。
// Python: store.py InMemoryTrajectoryStore.__init__。
func NewInMemoryTrajectoryStore() *InMemoryTrajectoryStore {
	return &InMemoryTrajectoryStore{
		data:  map[string]map[string]*Trajectory{},
		order: map[string][]string{},
	}
}

// Save 保存规范轨迹（深拷贝封存）。
// Python: store.py InMemoryTrajectoryStore.save。
func (s *InMemoryTrajectoryStore) Save(trajectory *Trajectory, version string) error {
	name, err := versionName(version)
	if err != nil {
		return err
	}
	if s.data[name] == nil {
		s.data[name] = map[string]*Trajectory{}
	}
	copied, err := FromOTLP(trajectory.ToOTLP())
	if err != nil {
		return err
	}
	id := copied.TrajectoryID()
	if _, exists := s.data[name][id]; !exists {
		s.order[name] = append(s.order[name], id)
	}
	s.data[name][id] = copied
	return nil
}

// Load 按 trajectory_id 加载最旧匹配。
// Python: store.py InMemoryTrajectoryStore.load。
func (s *InMemoryTrajectoryStore) Load(trajectoryID string, version string) (*Trajectory, error) {
	name, err := versionName(version)
	if err != nil {
		return nil, err
	}
	bucket := s.data[name]
	if bucket == nil {
		return nil, nil
	}
	return bucket[trajectoryID], nil
}

// Query 返回匹配稳定资源元数据的快照（保存序）。
// Python: store.py InMemoryTrajectoryStore.query。
func (s *InMemoryTrajectoryStore) Query(version, sessionID, teamID, memberID, caseID, source string) ([]*Trajectory, error) {
	name, err := versionName(version)
	if err != nil {
		return nil, err
	}
	filters := queryFilters(sessionID, teamID, memberID, caseID, source)
	var result []*Trajectory
	for _, id := range s.order[name] {
		trajectory := s.data[name][id]
		matched := true
		for key, value := range filters {
			if metadataValue(trajectory, key) != value {
				matched = false
				break
			}
		}
		if matched {
			result = append(result, trajectory)
		}
	}
	return result, nil
}

// FileTrajectoryStore 规范轨迹的追加式 JSONL 归档。
// Python: store.py FileTrajectoryStore。
type FileTrajectoryStore struct {
	baseDir string
}

// NewFileTrajectoryStore 创建文件轨迹存储（目录惰性创建）。
// Python: store.py FileTrajectoryStore.__init__。
func NewFileTrajectoryStore(baseDir string) *FileTrajectoryStore {
	return &FileTrajectoryStore{baseDir: baseDir}
}

// filePath 版本对应的 JSONL 文件路径。
// Python: store.py FileTrajectoryStore._get_file_path。
func (s *FileTrajectoryStore) filePath(version string) (string, error) {
	name, err := versionName(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.baseDir, fmt.Sprintf("trajectories_%s.jsonl", name)), nil
}

// Save 追加一条规范轨迹快照（ensure_ascii=False 紧凑 JSON + 换行）。
// Python: store.py FileTrajectoryStore.save。
func (s *FileTrajectoryStore) Save(trajectory *Trajectory, version string) error {
	path, err := s.filePath(version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return err
	}
	resourceSpans, ok := trajectory.ToOTLP()["resourceSpans"].([]any)
	if !ok || len(resourceSpans) == 0 {
		return fmt.Errorf("trajectory payload must contain non-empty resourceSpans")
	}
	record := map[string]any{"resourceSpans": toJsonCompatible(resourceSpans)}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// records 逐行读取 JSONL 记录（坏行跳过）。
// Python: store.py FileTrajectoryStore._records。
func (s *FileTrajectoryStore) records(path string) []map[string]any {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var result []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil || record == nil {
			continue
		}
		result = append(result, record)
	}
	return result
}

// decodeRecord 记录 → Trajectory（非法载荷返回 nil）。
// Python: store.py FileTrajectoryStore._decode。
func decodeRecord(record map[string]any) *Trajectory {
	trajectory, err := FromOTLP(record)
	if err != nil {
		return nil
	}
	return trajectory
}

// Load 按 trajectory_id 加载（文件序即保存序，首个匹配返回）。
// Python: store.py FileTrajectoryStore.load。
func (s *FileTrajectoryStore) Load(trajectoryID string, version string) (*Trajectory, error) {
	path, err := s.filePath(version)
	if err != nil {
		return nil, err
	}
	for _, record := range s.records(path) {
		trajectory := decodeRecord(record)
		if trajectory != nil && trajectory.TrajectoryID() == trajectoryID {
			return trajectory, nil
		}
	}
	return nil, nil
}

// Query 返回匹配稳定资源元数据的快照。
// Python: store.py FileTrajectoryStore.query。
func (s *FileTrajectoryStore) Query(version, sessionID, teamID, memberID, caseID, source string) ([]*Trajectory, error) {
	path, err := s.filePath(version)
	if err != nil {
		return nil, err
	}
	filters := queryFilters(sessionID, teamID, memberID, caseID, source)
	var result []*Trajectory
	for _, record := range s.records(path) {
		trajectory := decodeRecord(record)
		if trajectory == nil {
			continue
		}
		matched := true
		for key, value := range filters {
			if metadataValue(trajectory, key) != value {
				matched = false
				break
			}
		}
		if matched {
			result = append(result, trajectory)
		}
	}
	return result, nil
}
