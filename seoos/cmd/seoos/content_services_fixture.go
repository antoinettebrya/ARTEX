//go:build seoos_testfixture

package main

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/contentmodel"
	"github.com/Autumn-27/artex/seoos/internal/worker"
)

type contentLoopback struct {
	origin    string
	transport *http.Transport
}

func (m contentLoopback) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Hostname() {
	case "phase3.example.com", "official.example.com",
		"api.openai.com", "api.anthropic.com", "generativelanguage.googleapis.com", "api.deepseek.com":
	default:
		return nil, errors.New("fixture does not handle this host")
	}
	next := req.Clone(req.Context())
	next.Header.Set("X-SEOOS-Fixture-Host", req.URL.Hostname())
	next.URL.Scheme, next.URL.Host, next.Host = "http", m.origin, m.origin
	return m.transport.RoundTrip(next)
}

func newContentServices() (*worker.ContentServices, error) {
	raw := os.Getenv("SEOOS_CONTENT_TEST_URL")
	if raw == "" {
		return nil, nil
	}
	origin, err := url.Parse(raw)
	if err != nil || origin.Scheme != "http" || origin.User != nil || origin.Port() == "" ||
		origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || !net.ParseIP(origin.Hostname()).IsLoopback() {
		return nil, errors.New("the content test fixture requires a loopback HTTP origin")
	}
	client := &http.Client{Transport: contentLoopback{origin.Host, &http.Transport{}}}
	return &worker.ContentServices{Crawler: contentinventory.New(client), Models: contentmodel.New(client)}, nil
}
