//go:build seoos_testfixture

package main

import (
	"errors"
	"net"
	"net/url"
	"os"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchprovider"
)

func newResearchProvider() (domain.SEODataProvider, error) {
	endpoint, err := url.Parse(os.Getenv("SEOOS_DATAFORSEO_TEST_URL"))
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.Port() == "" ||
		endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		!net.ParseIP(endpoint.Hostname()).IsLoopback() {
		return nil, errors.New("the DataForSEO test fixture requires a loopback HTTP origin")
	}
	return researchprovider.NewDataForSEO(endpoint.String(), nil), nil
}
