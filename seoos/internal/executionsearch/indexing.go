package executionsearch

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

var indexNowKey = regexp.MustCompile("^[A-Za-z0-9-]{8,128}$")

func (c *Client) Inspect(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, target string) (domain.ExecutionIndexRecord, error) {
	base, err := siteURL(config.BaseURL)
	if err != nil {
		return domain.ExecutionIndexRecord{}, err
	}
	if !config.Enabled || !targetWithin(base, target) {
		return domain.ExecutionIndexRecord{}, domain.ErrExecutionAuthority
	}
	switch config.Kind {
	case domain.ExecutionGSC:
		return c.inspectGoogle(ctx, config, secret, target)
	case domain.ExecutionBing:
		return c.inspectBing(ctx, config, secret, target)
	default:
		return domain.ExecutionIndexRecord{}, domain.ErrExecutionUnsupported
	}
}

func (c *Client) Submit(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, urls []string) error {
	_, err := c.SubmitStatus(ctx, config, secret, urls)
	return err
}

// SubmitStatus returns the receipt status without implying search indexing.
// IndexNow 202 means the URLs were received and key verification is pending.
func (c *Client) SubmitStatus(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, urls []string) (int, error) {
	if !config.Enabled || !config.AllowWrites {
		return 0, domain.ErrExecutionAuthority
	}
	if len(urls) == 0 || len(urls) > 10000 {
		return 0, failure("execution_submission_limit")
	}
	base, err := siteURL(config.BaseURL)
	if err != nil {
		return 0, err
	}
	for _, target := range urls {
		if !targetWithin(base, target) {
			return 0, domain.ErrExecutionAuthority
		}
	}
	switch config.Kind {
	case domain.ExecutionIndexNow:
		return c.submitIndexNow(ctx, config, secret, base, urls)
	case domain.ExecutionBing:
		return c.submitBing(ctx, config, secret, base, urls)
	default:
		return 0, domain.ErrExecutionUnsupported
	}
}

func (c *Client) submitIndexNow(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, base *url.URL, urls []string) (int, error) {
	if !indexNowKey.MatchString(secret.Secret) {
		return 0, failure("execution_invalid_indexnow_key")
	}
	input := struct {
		Host        string   `json:"host"`
		Key         string   `json:"key"`
		KeyLocation string   `json:"keyLocation,omitempty"`
		URLs        []string `json:"urlList"`
	}{
		Host: strings.TrimSuffix(strings.ToLower(base.Hostname()), "."), Key: secret.Secret,
		URLs: urls,
	}
	if config.KeyLocation != "" {
		key, err := siteURL(config.KeyLocation)
		if err != nil || key.Scheme != base.Scheme || urlHost(key) != urlHost(base) || strings.HasSuffix(key.Path, "/") || key.Path == "" {
			return 0, failure("execution_invalid_indexnow_key_location")
		}
		scope := *key
		scope.Path, scope.RawPath = path.Dir(key.Path), ""
		for _, target := range urls {
			if !targetWithin(&scope, target) {
				return 0, failure("execution_indexnow_key_scope_mismatch")
			}
		}
		input.KeyLocation = config.KeyLocation
	}
	status, err := c.requestJSON(ctx, config, http.MethodPost, indexNowURL, "", input, nil)
	if err != nil {
		return status, err
	}
	if status != http.StatusOK && status != http.StatusAccepted {
		return status, failure("execution_invalid_indexnow_submission_receipt")
	}
	return status, nil
}
