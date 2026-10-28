// 三元组 beam search（GraphRetriever 的图扩展核心算法）。
//
// Python: graph_retriever.py (TripleBeamSearch L24-225)
//
// 文件目录：
//
//	（见 retriever/doc.go）
package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent retriever 包日志组件常量（13.23 首次引入 logger）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// TripleBeamSearch 三元组 beam search。
//
// 构造默认：numBeams=10 / numCandidatesPerBeam=100 / maxLength=2 /
// encoderBatchSize=256（死参数存而不读，spec §8-5）/ retrieveMode=""
// （= Python None）。embedModel 从 retriever 经 EmbedModelProvider 断言提取
// （Python hasattr L48）。
type TripleBeamSearch struct {
	// retriever 候选检索器
	retriever Retriever
	// numBeams 束宽
	numBeams int
	// numCandidatesPerBeam 每束候选数
	numCandidatesPerBeam int
	// maxLength 最大路径长度（图跳数）
	maxLength int
	// encoderBatchSize 编码批大小（Python 死参数，存而不读）
	encoderBatchSize int
	// retrieveMode 构造传入的模式（"" = Python None）
	retrieveMode string
	// embedModel 嵌入模型（构造时从 retriever 提取）
	embedModel embedding.BaseEmbedding
}

// TripleBeamSearchOption 构造选项。
type TripleBeamSearchOption func(*TripleBeamSearch)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTripleNumBeams 默认束宽
	defaultTripleNumBeams = 10
	// defaultTripleNumCandidates 默认每束候选数
	defaultTripleNumCandidates = 100
	// defaultTripleMaxLength 默认最大路径长度
	defaultTripleMaxLength = 2
	// defaultTripleEncoderBatchSize 默认编码批大小（Python 死参数）
	defaultTripleEncoderBatchSize = 256
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTripleNumBeams 设置束宽。
func WithTripleNumBeams(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.numBeams = n }
}

// WithTripleNumCandidatesPerBeam 设置每束候选数。
func WithTripleNumCandidatesPerBeam(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.numCandidatesPerBeam = n }
}

// WithTripleMaxLength 设置最大路径长度。
func WithTripleMaxLength(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.maxLength = n }
}

// WithTripleEncoderBatchSize 设置编码批大小（Python 死参数，仅存储）。
func WithTripleEncoderBatchSize(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.encoderBatchSize = n }
}

// WithTripleRetrieveMode 设置构造模式（"" = Python None）。
func WithTripleRetrieveMode(mode string) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.retrieveMode = mode }
}

// NewTripleBeamSearch 创建 beam search。
//
// 对齐 Python __init__（L27-50）：max_length<1 报 155204（文案对齐
// Python f"expect max_length >= 1; got {max_length=}" 展开形态）；
// embedModel 经 EmbedModelProvider 断言提取（hasattr 同构）。
func NewTripleBeamSearch(retriever Retriever, opts ...TripleBeamSearchOption) (*TripleBeamSearch, error) {
	s := &TripleBeamSearch{
		retriever:            retriever,
		numBeams:             defaultTripleNumBeams,
		numCandidatesPerBeam: defaultTripleNumCandidates,
		maxLength:            defaultTripleMaxLength,
		encoderBatchSize:     defaultTripleEncoderBatchSize,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.maxLength < 1 {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("expect max_length >= 1; got max_length=%d", s.maxLength)),
		)
	}
	if p, ok := retriever.(EmbedModelProvider); ok {
		s.embedModel = p.EmbedModel()
	}
	return s, nil
}

// BeamSearch 执行 beam search。
//
// 对齐 Python beam_search（L78-108）：空输入 warn 返回空；无 embed 155202；
// triples+query 合批嵌入（批大小经 MaxBatchSizeProvider 断言，断言失败
// 显式 155202——拍板 c）；余弦 topk 初始 beams；maxLength-1 轮扩展。
func (s *TripleBeamSearch) BeamSearch(ctx context.Context, query string, triples []common.RetrievalResult) ([]*common.TripleBeam, error) {
	if len(triples) == 0 {
		logger.Warn(logComponent).
			Str("query", query).
			Msg("beam search got empty input triples")
		return nil, nil
	}
	if s.embedModel == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverEmbedModelNotFound,
			exception.WithParam("error_msg", "embed_model is required for beam search"),
		)
	}

	texts := make([]string, 0, len(triples)+1)
	for _, x := range triples {
		texts = append(texts, x.Text)
	}
	texts = append(texts, query)

	embeddings, err := s.embedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	queryEmbedding := embeddings[len(embeddings)-1]
	candEmbeddings := embeddings[:len(embeddings)-1]

	scores := cosineScores(queryEmbedding, candEmbeddings)
	topkIdx, topkScores := topkIndices(scores, s.numBeams)
	beams := make([]*common.TripleBeam, 0, len(topkIdx))
	for i, idx := range topkIdx {
		beams = append(beams, common.NewTripleBeam([]common.RetrievalResult{triples[idx]}, topkScores[i]))
	}

	for range s.maxLength - 1 {
		results := make([][]common.RetrievalResult, len(beams))
		g, gctx := errgroup.WithContext(ctx)
		for i := range beams {
			i, beam := i, beams[i]
			g.Go(func() error {
				ret, err := s.searchCandidates(gctx, beam)
				if err != nil {
					return err
				}
				results[i] = ret
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}

		beams, err = s.expandBeams(ctx, beams, results, queryEmbedding)
		if err != nil {
			return nil, err
		}
	}
	return beams, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// embedBatch 批量嵌入（Python embed_documents(batch_size=max_batch_size)）。
//
// max_batch_size 经 MaxBatchSizeProvider 断言：断言失败显式报 155202
// （Python AttributeError 炸点的业务化形态，spec §8-7——炸点保留使
// graph_expansion 降级路径可达）。
func (s *TripleBeamSearch) embedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	mp, ok := s.embedModel.(MaxBatchSizeProvider)
	if !ok {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverEmbedModelNotFound,
			exception.WithParam("error_msg", "embed_model does not support max_batch_size"),
		)
	}
	return s.embedModel.EmbedDocuments(ctx, texts, embedding.WithBatchSize(mp.MaxBatchSize()))
}

// expandBeams 扩展 beams。
//
// 对齐 Python _expand_beams（L110-170）：existTriples 全 beam 文本判重；
// 候选路径 (beam, triple|nil)（nil=保原 beam）；路径文本重嵌入打分 topk；
// seenBeamTexts 按 "; ".join 格式去重；texts 空→原 beams。
func (s *TripleBeamSearch) expandBeams(ctx context.Context, beams []*common.TripleBeam, candidatesPerBeam [][]common.RetrievalResult, queryEmbedding []float64) ([]*common.TripleBeam, error) {
	var texts []string
	type candidatePath struct {
		beam   *common.TripleBeam
		triple *common.RetrievalResult // nil = 保原 beam
	}
	var candidatePaths []candidatePath

	existTriples := make(map[string]struct{})
	for _, beam := range beams {
		for _, x := range beam.Triples() {
			existTriples[x.Text] = struct{}{}
		}
	}

	for i, beam := range beams {
		cands := candidatesPerBeam[i]
		if len(cands) == 0 {
			// 无候选：保原 beam（Python L121-124）
			candidatePaths = append(candidatePaths, candidatePath{beam: beam, triple: nil})
			texts = append(texts, formatTriples(beam.Triples()))
			continue
		}
		for j := range cands {
			triple := cands[j]
			if _, ok := existTriples[triple.Text]; ok {
				continue
			}
			candidatePaths = append(candidatePaths, candidatePath{beam: beam, triple: &cands[j]})
			// Python beam.triples + [triple]——Triples() 浅拷贝密封 append（13.22 C2）
			texts = append(texts, formatTriples(append(beam.Triples(), triple)))
		}
	}

	if len(texts) == 0 {
		return beams, nil
	}
	if s.embedModel == nil {
		return beams, nil
	}

	embeddings, err := s.embedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	nextScores := cosineScores(queryEmbedding, embeddings)

	topkIdx, _ := topkIndices(nextScores, s.numBeams)

	newBeams := make([]*common.TripleBeam, 0, len(topkIdx))
	seenBeamTexts := make(map[string]struct{})
	for _, idx := range topkIdx {
		path := candidatePaths[idx]
		if path.triple == nil {
			beamText := formatTriples(path.beam.Triples())
			if _, ok := seenBeamTexts[beamText]; !ok {
				newBeams = append(newBeams, path.beam)
				seenBeamTexts[beamText] = struct{}{}
			}
		} else {
			newTriples := append(path.beam.Triples(), *path.triple)
			beamText := formatTriples(newTriples)
			if _, ok := seenBeamTexts[beamText]; !ok {
				newBeams = append(newBeams, common.NewTripleBeam(newTriples, nextScores[idx]))
				seenBeamTexts[beamText] = struct{}{}
			}
		}
	}
	return newBeams, nil
}

// searchCandidates 检索单 beam 的候选三元组。
//
// 对齐 Python _search_candidates（L172-225）：beam 尾 triple 元数据防御
// 解析（缺失/非字符串/解析失败 warn 返回空，Python L176-185）；entities=
// {首,尾}；mode 三级解析 IndexTypeCarrier > retrieveMode > "hybrid"
// （bm25→sparse）；retrieve(top_k=numCandidates)；三重过滤——已在 beam
// （Contains）/候选解析**裸调用无防御**（Python L213，错误冒泡由上层
// graph_expansion 降级）/首尾都不在 entities。空结果 warn。
func (s *TripleBeamSearch) searchCandidates(ctx context.Context, beam *common.TripleBeam) ([]common.RetrievalResult, error) {
	tail := beam.Get(-1)
	var tripleData any
	if tail.Metadata != nil {
		tripleData = tail.Metadata["triple"]
	}
	tripleStr, ok := tripleData.(string)
	if !ok || tripleStr == "" {
		// Python L176-179：metadata.get("triple") None → warn 返回空
		logger.Warn(logComponent).Msg("beam has no triple metadata")
		return nil, nil
	}
	var parsed []any
	if err := json.Unmarshal([]byte(tripleStr), &parsed); err != nil {
		// Python L182-185：JSONDecodeError → warn 返回空
		logger.Warn(logComponent).Err(err).Msg("[graph] Failed to parse triple metadata")
		return nil, nil
	}
	if len(parsed) < 2 {
		// Python L187-188：not triple or len(triple) < 2
		return nil, nil
	}

	entities := make(map[string]struct{}, 2)
	entities[pyAnyStr(parsed[0])] = struct{}{}
	entities[pyAnyStr(parsed[len(parsed)-1])] = struct{}{}
	queryStr := strings.Join(mapKeys(entities), " ")

	mode := s.resolveCandidateMode()

	nodes, err := s.retriever.Retrieve(ctx, queryStr, s.numCandidatesPerBeam, nil, mode)
	if err != nil {
		return nil, err
	}

	var ret []common.RetrievalResult
	for _, x := range nodes {
		if beam.Contains(x) {
			continue
		}
		// 候选解析裸调用（Python L213 无 try——json.loads(None) TypeError
		// 冒泡炸掉 beam search，由 graph_expansion except 降级）：
		// Go 侧 Unmarshal 错误/类型不符直接冒泡，字面保留。
		xTriple, err := parseCandidateTriple(x)
		if err != nil {
			return nil, err
		}
		if len(xTriple) < 2 {
			continue
		}
		if _, okFirst := entities[pyAnyStr(xTriple[0])]; !okFirst {
			if _, okLast := entities[pyAnyStr(xTriple[len(xTriple)-1])]; !okLast {
				continue
			}
		}
		ret = append(ret, x)
	}

	if len(ret) == 0 {
		logger.Warn(logComponent).
			Str("beam", formatTriples(beam.Triples())).
			Msg("empty candidates for beam")
	}
	return ret, nil
}

// resolveCandidateMode 候选检索 mode 三级解析。
//
// 对齐 Python L193-199：retriever.index_type > retrieveMode > "hybrid"；
// bm25 → sparse 映射。
func (s *TripleBeamSearch) resolveCandidateMode() RetrievalMode {
	mode := ""
	if ic, ok := s.retriever.(IndexTypeCarrier); ok {
		mode = ic.IndexType()
	}
	if mode == "" {
		mode = s.retrieveMode
	}
	if mode == "" {
		mode = "hybrid"
	}
	if mode == "bm25" {
		mode = "sparse"
	}
	return RetrievalMode(mode)
}

// parseCandidateTriple 候选 triple 元数据裸解析（无防御，错误冒泡）。
func parseCandidateTriple(x common.RetrievalResult) ([]any, error) {
	if x.Metadata == nil {
		return nil, fmt.Errorf("json: cannot unmarshal nil triple metadata")
	}
	raw, ok := x.Metadata["triple"].(string)
	if !ok {
		return nil, fmt.Errorf("json: cannot unmarshal non-string triple metadata")
	}
	var parsed []any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// formatTriples 路径文本渲染（Python "; ".join L75-76）。
func formatTriples(triples []common.RetrievalResult) string {
	parts := make([]string, len(triples))
	for i, x := range triples {
		parts[i] = x.Text
	}
	return strings.Join(parts, "; ")
}

// pyAnyStr Python str() 的宽松字符串化（triple 元素可能非字符串）。
func pyAnyStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// mapKeys 取 map 键集（entities join 顺序与 Python set 迭代同样无序，
// 语义同构）。
func mapKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
