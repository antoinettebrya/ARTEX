package domain

import (
	"encoding/json"
	"math"
	"strings"
)

func (in ContentJobInput) Normalize() (ContentJobInput, error) {
	if !ValidID(in.SiteID) || !IsContentKind(in.Kind) {
		return in, invalid("kind", "use a valid site and a supported content task")
	}
	siteTask := in.Kind == ContentInventory || in.Kind == ContentExtract
	if siteTask && in.ProjectID != "" || !siteTask && !ValidID(in.ProjectID) {
		return in, invalid("project_id", "is required only for project tasks")
	}
	if UsesContentModel(in.Kind) && !ValidID(in.ProviderID) || !UsesContentModel(in.Kind) && in.ProviderID != "" {
		return in, invalid("provider_id", "is required only for model stages")
	}
	in.URL = strings.TrimSpace(in.URL)
	if in.Kind == ContentExtract || in.Kind == ContentVerify {
		if _, err := EvidenceURL(in.URL); err != nil {
			return in, err
		}
	} else if in.URL != "" {
		return in, invalid("url", "allowed only for extraction or publication verification")
	}
	if len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 120 || !identifier(in.IdempotencyKey) || in.Priority < 0 || in.Priority > 100 {
		return in, invalid("task", "provide an idempotency key and priority between 0 and 100")
	}
	return in, nil
}

func (p ContentModelProfile) Validate() error {
	if !ValidID(p.ProviderID) || p.Role != "writer" && p.Role != "reviewer" && p.Role != "any" {
		return invalid("role", "use writer, reviewer or any for a valid provider")
	}
	if hasControl(strings.ReplaceAll(p.SystemPrompt, "\n", "")) || len(p.SystemPrompt) > 3000 ||
		math.IsNaN(p.Temperature) || p.Temperature < 0 || p.Temperature > 1 ||
		p.MaxInputTokens < 1024 || p.MaxInputTokens > 48000 || p.DailyTokens < 1 || p.DailyTokens > 1000000000 ||
		p.DailyCostMicros < 1 || p.DailyCostMicros > 100000000000 ||
		p.InputMicrosPerMillion < 1 || p.InputMicrosPerMillion > 10000000000 ||
		p.CachedInputMicrosPerMillion != nil && (*p.CachedInputMicrosPerMillion < 0 || *p.CachedInputMicrosPerMillion > p.InputMicrosPerMillion) ||
		p.OutputMicrosPerMillion < 1 || p.OutputMicrosPerMillion > 10000000000 ||
		p.MaxRetries < 0 || p.MaxRetries > 2 || p.RetrySeconds < 1 || p.RetrySeconds > 120 {
		return invalid("profile", "configure bounded token limits, USD rates, temperature and retries")
	}
	return nil
}

func (a ContentArticle) Validate(market Market) error {
	if a.Language != market.LanguageCode || strings.TrimSpace(a.Title) == "" ||
		strings.TrimSpace(a.H1) == "" || len(a.Title) > 400 || len(a.H1) > 400 || len(a.Description) > 1000 ||
		len(a.Author) > 200 || len(a.Sections) < 1 || len(a.Sections) > 16 {
		return invalid("article", "the article needs a matching locale, title, H1 and bounded sections")
	}
	level := 1
	for _, s := range a.Sections {
		if s.Level < 2 || s.Level > 6 || s.Level > level+1 || strings.TrimSpace(s.Heading) == "" ||
			len(s.Heading) > 400 || len(s.Body) > 6000 || len(s.Claims) > 40 {
			return invalid("sections", "use ordered H2-H6 sections and bounded plain text")
		}
		level = s.Level
	}
	data, err := json.Marshal(a)
	if err != nil || len(data) > 53000 || strings.Contains(string(data), `\u0000`) {
		return invalid("article", "the article exceeds its storage limit")
	}
	return nil
}

func (b ContentBrief) Validate(project ContentProject) error {
	if KeywordKey(b.PrimaryKeyword) != KeywordKey(project.Keyword) || b.Country != project.Market.CountryCode ||
		b.Language != project.Market.LanguageCode || strings.TrimSpace(b.Title) == "" || strings.TrimSpace(b.H1) == "" ||
		len(b.Headings) > 16 || len(b.Headings) == 0 {
		return invalid("brief", "use the project's keyword, market and bounded heading outline")
	}
	level := 1
	for _, heading := range b.Headings {
		if heading.Level < 2 || heading.Level > 6 || heading.Level > level+1 || strings.TrimSpace(heading.Heading) == "" {
			return invalid("headings", "use ordered H2-H6 headings")
		}
		level = heading.Level
	}
	data, err := json.Marshal(b)
	if err != nil || len(data) > 32000 || strings.Contains(string(data), `\u0000`) {
		return invalid("brief", "the brief exceeds its storage limit")
	}
	return nil
}
