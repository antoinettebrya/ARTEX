//go:build !seoos_testfixture

package main

import (
	"net/http"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchprovider"
)

func newResearchProvider() (domain.SEODataProvider, error) {
	return researchprovider.NewDataForSEO("", nil), nil
}

func newProviderTestFixture() (*http.Client, error) { return nil, nil }
