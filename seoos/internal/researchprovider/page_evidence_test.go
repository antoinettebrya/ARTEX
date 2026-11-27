package researchprovider

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	evidencePageURL      = "https://competitor.example.com/guide"
	crawlTaskID          = "01161532-1535-0216-0000-9b351e1ad02c"
	privateEvidenceToken = "Private raw fixture sentence"
)

func TestDataForSEOContentParsingHTTPContract(t *testing.T) {
	const path = "/v3/on_page/content_parsing/live"
	payload := fixtureBytes(t, "onpage_content")
	provider := fixtureProvider(t, path, map[string]any{
		"url": evidencePageURL, "accept_language": "en", "markdown_view": false,
	}, payload)
	request := domain.DataRequest{Operation: domain.DataPageContent, Market: usaEN, TargetURL: evidencePageURL}
	plan, err := provider.Plan(request)
	if err != nil || plan.ReserveMicros != 1000 || plan.TTL != 3*24*time.Hour {
		t.Fatalf("content parsing plan = %#v, err = %v", plan, err)
	}
	got, err := provider.Fetch(context.Background(), request, testAccess())
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, got, payload, path, 150)
	at := time.Date(2024, time.December, 7, 10, 30, 0, 0, time.UTC)
	if !got.CollectedAt.Equal(at) {
		t.Errorf("collected at %s, want %s", got.CollectedAt, at)
	}
	want := &domain.PageEvidence{
		URL: evidencePageURL, Source: domain.ProviderDataForSEO + path,
		Headings: []domain.HeadingEvidence{
			{Level: 2, Text: "Battery life"}, {Level: 2, Text: "Frequently Asked Questions"},
			{Level: 3, Text: "How long does it last?"}, {Level: 2, Text: "Which model should I buy?"},
			{Level: 3, Text: "Other features"},
		},
		Topics: []string{"Battery life", "How long does it last?", "Which model should I buy?", "Other features"},
		FAQ:    []string{"How long does it last?"}, Entities: []string{"Sample Author"},
		Sections: []domain.PageSectionEvidence{
			{Level: 2, Heading: "Battery life", PrimaryBlocks: 2, Tables: 1},
			{Level: 2, Heading: "Frequently Asked Questions", SecondaryBlocks: 1},
			{Level: 3, Heading: "How long does it last?", PrimaryBlocks: 1},
			{Level: 2, Heading: "Which model should I buy?", PrimaryBlocks: 1},
			{Level: 3, Heading: "Other features", PrimaryBlocks: 1},
		},
	}
	assertPageEvidence(t, got.Page, want)
}

func TestDataForSEOMicrodataHTTPContract(t *testing.T) {
	const path = "/v3/on_page/microdata"
	payload := fixtureBytes(t, "onpage_microdata")
	provider := fixtureProvider(t, path, map[string]any{"url": evidencePageURL, "id": crawlTaskID}, payload)
	request := domain.DataRequest{
		Operation: domain.DataPageMicrodata, Market: usaEN, TargetURL: evidencePageURL, TargetTaskID: crawlTaskID,
	}
	plan, err := provider.Plan(request)
	if err != nil || plan.ReserveMicros != 1 || plan.TTL != 3*24*time.Hour {
		t.Fatalf("microdata plan = %#v, err = %v", plan, err)
	}
	got, err := provider.Fetch(context.Background(), request, testAccess())
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, got, payload, path, 0)
	want := &domain.PageEvidence{
		URL: evidencePageURL, Source: domain.ProviderDataForSEO + path, MicrodataObserved: true,
		Schema: []string{"FAQPage", "Question", "Answer", "Organization", "Brand", "Article"},
		FAQ:    []string{"How long does it last?"}, Entities: []string{"Competitor Tools", "Guide Brand", "A topic mentioned in schema"},
	}
	assertPageEvidence(t, got.Page, want)
}

func TestDataForSEORejectsWrongPageEcho(t *testing.T) {
	const path = "/v3/on_page/content_parsing/live"
	fixture := fixtureBytes(t, "onpage_content")
	other := strings.ReplaceAll(string(fixture), evidencePageURL, "https://other.example.com/guide")
	provider := fixtureProvider(t, path, map[string]any{
		"url": evidencePageURL, "accept_language": "en", "markdown_view": false,
	}, []byte(other))
	request := domain.DataRequest{Operation: domain.DataPageContent, Market: usaEN, TargetURL: evidencePageURL}
	got, err := provider.Fetch(context.Background(), request, testAccess())
	requireDataError(t, err, "provider_page_unavailable", false, false, 0)
	assertSnapshot(t, got, []byte(other), path, 150)
	if got.Page != nil {
		t.Fatal("returned evidence for the wrong page")
	}
}

func TestDataForSEOMicrodataPendingAndInvalidID(t *testing.T) {
	const path = "/v3/on_page/microdata"
	payload := []byte(`{"status_code":20000,"cost":0,"tasks":[{"id":"fixture-1","status_code":20000,"data":{"url":"https://competitor.example.com/guide"},"result":[{"crawl_progress":"in_progress","items":null}]}]}`)
	provider := fixtureProvider(t, path, map[string]any{"url": evidencePageURL, "id": crawlTaskID}, payload)
	request := domain.DataRequest{
		Operation: domain.DataPageMicrodata, Market: usaEN, TargetURL: evidencePageURL, TargetTaskID: "bad-id",
	}
	_, err := provider.Plan(request)
	requireDataError(t, err, "invalid_provider_task_id", false, true, 0)
	request.TargetTaskID = crawlTaskID
	got, err := provider.Fetch(context.Background(), request, testAccess())
	requireDataError(t, err, "provider_crawl_pending", true, true, 15*time.Second)
	assertSnapshot(t, got, payload, path, 0)
}

func assertPageEvidence(t *testing.T, got, want *domain.PageEvidence) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("page = %+v, want %+v", got, want)
	}
	normalized, err := json.Marshal(got)
	if err != nil || strings.Contains(string(normalized), privateEvidenceToken) {
		t.Fatal("normalized evidence contains page body, answer, or contacts")
	}
}
