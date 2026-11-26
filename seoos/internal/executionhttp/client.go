package executionhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
)

type countedTransport struct {
	transport http.RoundTripper
	admit     func(context.Context) error
}

func (c countedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := c.admit(req.Context()); err != nil {
		return nil, err
	}
	return c.transport.RoundTrip(req)
}

// Client applies the same request accounting to provider and authorized
// page requests. A supplied transport is only used by loopback test builds.
func Client(fixture *http.Client, timeout time.Duration, admit func(context.Context) error) *http.Client {
	c := contentinventory.NewPublicHTTPClient(timeout)
	if fixture != nil && fixture.Transport != nil {
		c.Transport = fixture.Transport
	}
	c.Transport = countedTransport{transport: c.Transport, admit: admit}
	return c
}
