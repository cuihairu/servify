package application

import (
	"context"
	"regexp"
	"sort"
	"strings"

	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var tokenRe = regexp.MustCompile(`[\p{Han}]|[A-Za-z0-9_]+`)

func (s *Service) Suggest(ctx context.Context, req *suggestioncontract.SuggestionRequest) (*suggestioncontract.SuggestionResponse, error) {
	query := ""
	ticketLimit := 5
	docLimit := 5
	candidateMax := 200
	if req != nil {
		query = strings.TrimSpace(req.Query)
		if req.TicketLimit > 0 {
			ticketLimit = req.TicketLimit
		}
		if req.KnowledgeDocLimit > 0 {
			docLimit = req.KnowledgeDocLimit
		}
		if req.CandidateTicketMax > 0 {
			candidateMax = req.CandidateTicketMax
		}
	}
	if ticketLimit > 20 {
		ticketLimit = 20
	}
	if docLimit > 20 {
		docLimit = 20
	}
	if candidateMax > 1000 {
		candidateMax = 1000
	}

	intent := ClassifyIntent(query)
	tokens := ExtractTokens(query)

	tickets, ticketCandidates, err := s.suggestTickets(ctx, query, tokens, ticketLimit, candidateMax)
	if err != nil {
		return nil, err
	}
	docs, docCandidates, err := s.suggestKnowledgeDocs(ctx, query, tokens, docLimit)
	if err != nil {
		return nil, err
	}

	return &suggestioncontract.SuggestionResponse{
		Query:          query,
		Intent:         intent,
		SimilarTickets: tickets,
		KnowledgeDocs:  docs,
		Meta: map[string]interface{}{
			"tokens":            tokens,
			"ticket_candidates": ticketCandidates,
			"doc_candidates":    docCandidates,
		},
	}, nil
}

func (s *Service) suggestTickets(ctx context.Context, query string, tokens []string, limit int, candidateMax int) ([]suggestioncontract.TicketSuggestion, int, error) {
	rows, err := s.repo.FindTicketCandidates(ctx, tokens, candidateMax)
	if err != nil {
		return nil, 0, err
	}

	sugs := make([]suggestioncontract.TicketSuggestion, 0, len(rows))
	for _, r := range rows {
		score := ScoreText(query, r.Title+" "+r.Description)
		if score <= 0 {
			continue
		}
		sugs = append(sugs, suggestioncontract.TicketSuggestion{
			ID:        r.ID,
			Title:     r.Title,
			Status:    r.Status,
			Category:  r.Category,
			Priority:  r.Priority,
			CreatedAt: r.CreatedAt,
			Score:     score,
		})
	}

	sort.Slice(sugs, func(i, j int) bool {
		if sugs[i].Score == sugs[j].Score {
			return sugs[i].CreatedAt.After(sugs[j].CreatedAt)
		}
		return sugs[i].Score > sugs[j].Score
	})
	if len(sugs) > limit {
		sugs = sugs[:limit]
	}
	return sugs, len(rows), nil
}

func (s *Service) suggestKnowledgeDocs(ctx context.Context, query string, tokens []string, limit int) ([]suggestioncontract.KnowledgeDocSuggestion, int, error) {
	rows, err := s.repo.FindKnowledgeDocCandidates(ctx, tokens)
	if err != nil {
		return nil, 0, err
	}

	sugs := make([]suggestioncontract.KnowledgeDocSuggestion, 0, len(rows))
	for _, r := range rows {
		score := ScoreText(query, r.Title+" "+r.Content+" "+r.Tags)
		if score <= 0 {
			continue
		}
		sugs = append(sugs, suggestioncontract.KnowledgeDocSuggestion{
			ID:       r.ID,
			Title:    r.Title,
			Category: r.Category,
			Tags:     r.Tags,
			Score:    score,
		})
	}

	sort.Slice(sugs, func(i, j int) bool {
		if sugs[i].Score == sugs[j].Score {
			return sugs[i].ID < sugs[j].ID
		}
		return sugs[i].Score > sugs[j].Score
	})
	if len(sugs) > limit {
		sugs = sugs[:limit]
	}
	return sugs, len(rows), nil
}

func ExtractTokens(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	matches := tokenRe.FindAllString(query, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		// tokenRe 只匹配单个汉字或 [A-Za-z0-9_]+，TrimSpace 后恒非空，
		// 无需空串守卫。
		m = strings.ToLower(strings.TrimSpace(m))
		if len([]rune(m)) >= 32 {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func BuildLikeWhereTokens(fields []string, tokens []string, maxTokens int) (string, []interface{}) {
	if len(fields) == 0 || len(tokens) == 0 || maxTokens <= 0 {
		return "", nil
	}
	var parts []string
	var args []interface{}
	used := 0
	for _, t := range tokens {
		if used >= maxTokens {
			break
		}
		if t == "" {
			continue
		}
		like := "%" + t + "%"
		var sub []string
		for _, f := range fields {
			sub = append(sub, f+" LIKE ?")
			args = append(args, like)
		}
		parts = append(parts, "("+strings.Join(sub, " OR ")+")")
		used++
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, " OR "), args
}

func ScoreText(query string, text string) float64 {
	qTokens := ExtractTokens(query)
	if len(qTokens) == 0 {
		return 0
	}
	tTokens := ExtractTokens(text)
	if len(tTokens) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(tTokens))
	for _, t := range tTokens {
		set[t] = struct{}{}
	}
	hits := 0
	for _, qt := range qTokens {
		if _, ok := set[qt]; ok {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	return float64(hits) / float64(len(qTokens))
}

func ClassifyIntent(query string) suggestioncontract.IntentSuggestion {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return suggestioncontract.IntentSuggestion{Label: "general", Confidence: 0.2}
	}
	type bucket struct {
		label    string
		keywords []string
	}
	buckets := []bucket{
		{label: "complaint", keywords: []string{"投诉", "差评", "不满", "生气", "愤怒", "骗子", "垃圾", "坑", "投诉", "complaint", "angry"}},
		{label: "billing", keywords: []string{"发票", "付款", "支付", "收费", "价格", "账单", "退款", "billing", "invoice", "payment", "refund"}},
		{label: "technical", keywords: []string{"报错", "错误", "bug", "无法", "失败", "异常", "error", "crash", "连接", "登录", "超时", "timeout"}},
	}
	bestLabel := "general"
	bestHits := 0
	var matches []string
	for _, b := range buckets {
		hits := 0
		var m []string
		for _, kw := range b.keywords {
			// buckets 关键词均为非空字面量，无需空串守卫。
			if strings.Contains(q, strings.ToLower(kw)) {
				hits++
				m = append(m, kw)
			}
		}
		if hits > bestHits {
			bestHits = hits
			bestLabel = b.label
			matches = m
		}
	}
	conf := 0.35
	if bestLabel != "general" {
		conf = 0.6 + 0.1*float64(bestHits-1)
		if conf > 0.95 {
			conf = 0.95
		}
	}
	return suggestioncontract.IntentSuggestion{Label: bestLabel, Confidence: conf, Matches: matches}
}
