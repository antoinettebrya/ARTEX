//go:build seoos_testfixture

package main

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchprovider"
)

func newResearchProvider() (domain.SEODataProvider, error) {
	endpoint, err := dataForSEOFixtureOrigin()
	if err != nil {
		return nil, err
	}
	return researchprovider.NewDataForSEO(endpoint.String(), nil), nil
}

func dataForSEOFixtureOrigin() (*url.URL, error) {
	endpoint, err := url.Parse(os.Getenv("SEOOS_DATAFORSEO_TEST_URL"))
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.Port() == "" ||
		endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		!net.ParseIP(endpoint.Hostname()).IsLoopback() {
		return nil, errors.New("the DataForSEO test fixture requires a loopback HTTP origin")
	}
	return endpoint, nil
}

type providerProbeLoopback struct {
	origin    string
	transport *http.Transport
}

func (t providerProbeLoopback) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Hostname() {
	case "api.dataforseo.com", "api.openai.com", "api.anthropic.com", "api.deepseek.com", "generativelanguage.googleapis.com":
	default:
		return nil, errors.New("provider test fixture does not handle this host")
	}
	next := req.Clone(req.Context())
	next.Header.Set("X-SEOOS-Fixture-Host", req.URL.Hostname())
	next.URL.Scheme, next.URL.Host, next.Host = "http", t.origin, t.origin
	return t.transport.RoundTrip(next)
}

func newProviderTestFixture() (*http.Client, error) {
	origin, err := dataForSEOFixtureOrigin()
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: providerProbeLoopback{origin.Host, &http.Transport{}}}, nil
}
