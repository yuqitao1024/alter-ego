package ability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Domain string

const (
	DomainThinkingModels      Domain = "thinking-models"
	DomainWorkMethodology     Domain = "work-methodology"
	DomainCommunicationSkills Domain = "communication-skills"
)

type Options struct {
	DBPath    string
	MaxChunks int
}

type Builder struct {
	dbPath    string
	maxChunks int
}

type retrievedChunk struct {
	Category Domain
	Text     string
	Summary  string
}

func NewBuilder(opts Options) *Builder {
	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = strings.TrimSpace(os.Getenv("ALTER_EGO_ABILITY_RAG_DB_PATH"))
	}
	if dbPath == "" {
		dbPath = filepath.Join(".agents", "ability-rag", "rag-db", "rag.sqlite")
	}

	maxChunks := opts.MaxChunks
	if maxChunks <= 0 {
		maxChunks = 6
	}

	return &Builder{
		dbPath:    dbPath,
		maxChunks: maxChunks,
	}
}

func (b *Builder) BuildContext(ctx context.Context, userText string) (string, error) {
	domains := ClassifyDomains(userText)
	if len(domains) == 0 {
		return "", nil
	}

	chunks, err := b.search(ctx, strings.TrimSpace(userText), domains)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	var out strings.Builder
	out.WriteString("你是用户的高能力分身。像用户本人一样处理工作问题，但思考更清晰、判断更稳、表达更准、推进更有章法。\n")
	out.WriteString("回答普通聊天时，融合结构化思考、工作方法论和沟通技巧，但不要说自己使用了 skill、RAG、资料库或检索结果。\n")
	out.WriteString("默认使用简体中文，OKR、PR、RAG、skill、workflow 这类关键名词可以保留英文。\n")
	out.WriteString("输出要求：先给判断；短句短段；少用大段列表；避免套话、夸张、口号和 AI 腔；不要输出资料标题、文件名、路径、引用说明或“根据资料”。\n")
	out.WriteString("去 AI 味道：不要用“当然”“总结一下”“希望有帮助”“我们来深入探讨”这类模板化表达；少用机械小标题、粗体标签和三段式排比。\n")
	out.WriteString("处理方式：先判断用户真正要解决的工作问题，再给可执行建议；涉及沟通时，优先给能直接发送或当面说的话术。\n")
	out.WriteString("\n可用能力侧重点：\n")
	for _, domain := range domains {
		out.WriteString("- ")
		out.WriteString(domainGuidance(domain))
		out.WriteByte('\n')
	}

	if len(chunks) > 0 {
		out.WriteString("\n内部参考片段，只用于形成答案，不得在回答中暴露来源信息：\n")
		for i, chunk := range chunks {
			out.WriteString(fmt.Sprintf("%d. %s\n", i+1, sanitizeContextText(firstNonEmpty(chunk.Summary, chunk.Text), 700)))
		}
	}

	return strings.TrimSpace(out.String()), nil
}

func ClassifyDomains(text string) []Domain {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return nil
	}

	var domains []Domain
	if containsAny(normalized, []string{
		"思考", "分析", "判断", "决策", "取舍", "权衡", "拆解", "诊断", "问题", "原因", "策略", "swot", "pest", "5w2h", "模型",
	}) {
		domains = append(domains, DomainThinkingModels)
	}
	if containsAny(normalized, []string{
		"计划", "规划", "目标", "执行", "推进", "落地", "复盘", "okr", "kpi", "优先级", "项目", "任务", "效率", "管理", "方法论", "工作流",
	}) {
		domains = append(domains, DomainWorkMethodology)
	}
	if containsAny(normalized, []string{
		"沟通", "汇报", "表达", "说服", "谈判", "协商", "反馈", "绩效", "面谈", "老板", "领导", "同事", "客户", "话术", "发消息", "邮件", "会议",
	}) {
		domains = append(domains, DomainCommunicationSkills)
	}
	if len(domains) == 0 && containsAny(normalized, []string{"工作", "职场", "团队", "老板", "领导", "同事", "业务"}) {
		return []Domain{DomainThinkingModels, DomainWorkMethodology, DomainCommunicationSkills}
	}
	return domains
}

func (b *Builder) search(ctx context.Context, query string, domains []Domain) ([]retrievedChunk, error) {
	if _, err := os.Stat(b.dbPath); err != nil {
		return nil, err
	}
	if query == "" {
		return nil, nil
	}

	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(b.dbPath))
	if err != nil {
		return nil, fmt.Errorf("open ability rag db: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	limitPerDomain := b.maxChunks / len(domains)
	if limitPerDomain <= 0 {
		limitPerDomain = 1
	}

	chunks := make([]retrievedChunk, 0, b.maxChunks)
	for _, domain := range domains {
		rows, err := searchDomain(ctx, db, query, domain, limitPerDomain)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, rows...)
		if len(chunks) >= b.maxChunks {
			return chunks[:b.maxChunks], nil
		}
	}
	return chunks, nil
}

func searchDomain(ctx context.Context, db *sql.DB, query string, domain Domain, limit int) ([]retrievedChunk, error) {
	for _, ftsQuery := range ftsQueryVariants(query) {
		chunks, err := queryFTS(ctx, db, ftsQuery, domain, limit)
		if err == nil && len(chunks) > 0 {
			return chunks, nil
		}
	}

	likeQuery := bestLikeQuery(query)
	rows, err := db.QueryContext(ctx, `
		select category, text, summary
		from chunk_fts
		where category = ? and (text like ? or summary like ? or tags like ?)
		limit ?`, string(domain), "%"+likeQuery+"%", "%"+likeQuery+"%", "%"+likeQuery+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search ability rag db: %w", err)
	}
	defer rows.Close()

	var chunks []retrievedChunk
	for rows.Next() {
		var chunk retrievedChunk
		var category string
		if err := rows.Scan(&category, &chunk.Text, &chunk.Summary); err != nil {
			return nil, err
		}
		chunk.Category = Domain(category)
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

func queryFTS(ctx context.Context, db *sql.DB, query string, domain Domain, limit int) ([]retrievedChunk, error) {
	rows, err := db.QueryContext(ctx, `
		select category, text, summary
		from chunk_fts
		where chunk_fts match ? and category = ?
		limit ?`, query, string(domain), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []retrievedChunk
	for rows.Next() {
		var chunk retrievedChunk
		var category string
		if err := rows.Scan(&category, &chunk.Text, &chunk.Summary); err != nil {
			return nil, err
		}
		chunk.Category = Domain(category)
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

func sqliteReadOnlyDSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		return path
	}
	u := &url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("immutable", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

func quoteFTSQuery(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, " ") + `"`
}

func ftsQueryVariants(query string) []string {
	var variants []string
	query = strings.TrimSpace(query)
	if query != "" {
		variants = append(variants, query)
	}
	if terms := queryTerms(query); len(terms) > 0 {
		variants = append(variants, strings.Join(terms, " OR "))
	}
	if query != "" {
		variants = append(variants, quoteFTSQuery(query))
	}
	return variants
}

func queryTerms(query string) []string {
	known := []string{
		"OKR", "KPI", "SWOT", "PEST", "5W2H",
		"复盘", "沟通", "绩效", "面谈", "汇报", "表达", "谈判", "反馈",
		"目标", "计划", "优先级", "项目", "执行", "推进", "决策", "分析", "拆解",
	}
	lowerQuery := strings.ToLower(query)
	seen := map[string]bool{}
	var terms []string
	for _, term := range known {
		if strings.Contains(lowerQuery, strings.ToLower(term)) && !seen[term] {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	return terms
}

func bestLikeQuery(query string) string {
	if terms := queryTerms(query); len(terms) > 0 {
		return terms[0]
	}
	return query
}

func domainGuidance(domain Domain) string {
	switch domain {
	case DomainThinkingModels:
		return "思维能力：澄清目标、拆解问题、识别约束和关键假设，给出判断依据。"
	case DomainWorkMethodology:
		return "工作方法论：把目标拆成路径、检查点、风险和下一步动作。"
	case DomainCommunicationSkills:
		return "沟通能力：区分对象和场景，给出简洁、克制、可直接使用的表达。"
	default:
		return string(domain)
	}
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func sanitizeContextText(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
