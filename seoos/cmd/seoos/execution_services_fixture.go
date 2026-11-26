//go:build seoos_testfixture

package main

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/Autumn-27/artex/seoos/internal/worker"
)

type executionLoopback struct {
	origin    string
	transport *http.Transport
}

func (t executionLoopback) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Hostname() {
	case "phase3.example.com", "www.googleapis.com", "oauth2.googleapis.com", "searchconsole.googleapis.com",
		"analyticsdata.googleapis.com", "analyticsadmin.googleapis.com", "ssl.bing.com", "api.indexnow.org":
	default:
		return nil, errors.New("execution fixture does not handle this host")
	}
	if t.origin == "" {
		return nil, errors.New("execution fixture origin is unavailable")
	}
	next := req.Clone(req.Context())
	next.Header.Set("X-SEOOS-Fixture-Host", req.URL.Hostname())
	next.URL.Scheme, next.URL.Host, next.Host = "http", t.origin, t.origin
	return t.transport.RoundTrip(next)
}

func newExecutionServices() (*worker.ExecutionServices, error) {
	raw := os.Getenv("SEOOS_EXECUTION_TEST_URL")
	origin := ""
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "http" || u.User != nil || u.Port() == "" || u.Path != "" || u.RawQuery != "" ||
			u.ForceQuery || u.Fragment != "" || !net.ParseIP(u.Hostname()).IsLoopback() {
			return nil, errors.New("execution fixture requires a loopback HTTP origin")
		}
		origin = u.Host
	}
	client := &http.Client{Transport: executionLoopback{origin, &http.Transport{}}}
	return &worker.ExecutionServices{Fixture: client}, nil
}
