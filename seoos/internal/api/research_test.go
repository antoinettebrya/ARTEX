package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type researchRepository struct {
	stubRepository
	domain.ResearchRepository
	query   domain.ResearchQuery
	org     string
	section string
	request domain.ResearchInput
}

func (s *researchRepository) ListResearch(_ context.Context, org, section string, q domain.ResearchQuery) ([]json.RawMessage, error) {
	s.query, s.org, s.section = q, org, section
	return []json.RawMessage{}, nil
}

func (s *researchRepository) EnqueueResearch(_ context.Context, org string, input domain.ResearchInput) (domain.Job, bool, error) {
	s.org, s.request = org, input
	return domain.Job{ID: testSite}, true, nil
}

func (s *researchRepository) ResearchSnapshot(_ context.Context, org, id string) (json.RawMessage, error) {
	s.org = org
	if id != testSite {
		return nil, domain.ErrNotFound
	}
	return json.RawMessage(`{ "fixture" : "exact bytes" }`), nil
}

func TestResearchHTTPRoutingAndMarketContract(t *testing.T) {
	store := &researchRepository{}
	h := newTestHandler(t, store, testOrgA)
	base := "/api/v1/research/keywords?site_id=" + testSite
	for _, suffix := range []string{"&country_code=JP", "&location_code=garbage", "&unknown=1", "&device=mobile&device=desktop"} {
		requireStatus(t, request(h, http.MethodGet, base+suffix, ""), http.StatusBadRequest)
	}
	read := base + "&country_code=jp&location_code=2392&language_code=ja&search_engine=google&device=mobile&q=%E6%90%BA%E5%B8%AF&limit=12"
	requireStatus(t, request(h, http.MethodGet, read, ""), http.StatusOK)
	if store.org != testOrgA || store.section != "keywords" || store.query.SiteID != testSite || store.query.Limit != 12 ||
		store.query.Query != "携帯" || store.query.Market == nil || store.query.Market.CountryCode != "JP" || store.query.Market.LanguageCode != "ja" {
		t.Fatalf("wrong market parameters: %#v", store.query)
	}
	job := `{"site_id":"` + testSite + `","kind":"keyword_intent","keyword":"携帯","priority":22,
		"idempotency_key":"fixture-request","market":{"country_code":"JP","location_code":2392,"language_code":"ja","search_engine":"google","device":"mobile"}}`
	newJob := request(h, http.MethodPost, "/api/v1/research/jobs", job)
	requireStatus(t, newJob, http.StatusCreated)
	if newJob.Header().Get("Location") != "/api/v1/jobs/"+testSite || store.request.Priority != 22 || store.request.Keyword != "携帯" {
		t.Fatalf("unexpected research submission: %s", newJob.Body.String())
	}
	snapshot := request(h, http.MethodGet, "/api/v1/research/snapshots/"+testSite, "")
	requireStatus(t, snapshot, http.StatusOK)
	if snapshot.Body.String() != `{ "fixture" : "exact bytes" }` || !strings.HasPrefix(snapshot.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("snapshot changed: %s", snapshot.Body.String())
	}
	requireStatus(t, request(h, http.MethodGet, "/api/v1/research/snapshots/"+testOrgB, ""), http.StatusNotFound)
}
