package executionsearch

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

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
	if err := c.ValidateSubmission(config, secret, urls); err != nil {
		return 0, err
	}
	base, _ := siteURL(config.BaseURL)
	switch config.Kind {
	case domain.ExecutionIndexNow:
		return c.submitIndexNow(ctx, config, secret, base, urls)
	case domain.ExecutionBing:
		return c.submitBing(ctx, config, secret, urls)
	default:
		return 0, domain.ErrExecutionUnsupported
	}
}

// ValidateSubmission bounds writes before recording their dispatch intent.
func (c *Client) ValidateSubmission(config domain.ExecutionConnection, secret domain.ExecutionCredential, urls []string) error {
	if !config.Enabled || !config.AllowWrites {
		return domain.ErrExecutionAuthority
	}
	if len(urls) == 0 || len(urls) > domain.IndexingBatchLimit(config.Kind) {
		return failure("execution_submission_limit")
	}
	base, err := siteURL(config.BaseURL)
	if err != nil {
		return err
	}
	for _, target := range urls {
		if !targetWithin(base, target) {
			return domain.ErrExecutionAuthority
		}
	}
	switch config.Kind {
	case domain.ExecutionBing:
		property, err := siteURL(config.Property)
		if err != nil || !targetWithin(base, config.Property) || !hasBingCredential(secret) {
			return domain.ErrExecutionAuthority
		}
		for _, target := range urls {
			if !targetWithin(property, target) {
				return failure("execution_property_scope_mismatch")
			}
		}
	case domain.ExecutionIndexNow:
		if !domain.ValidIndexNowKey(secret.Secret) {
			return failure("execution_invalid_indexnow_key")
		}
		if config.KeyLocation != "" {
			key, err := siteURL(config.KeyLocation)
			if err != nil || key.Scheme != base.Scheme || urlHost(key) != urlHost(base) || path.Base(key.Path) != secret.Secret+".txt" {
				return failure("execution_invalid_indexnow_key_location")
			}
			key.Path, key.RawPath = path.Dir(key.Path), ""
			for _, target := range urls {
				if !targetWithin(key, target) {
					return failure("execution_indexnow_key_scope_mismatch")
				}
			}
		}
	default:
		return domain.ErrExecutionUnsupported
	}
	return nil
}

func (c *Client) submitIndexNow(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, base *url.URL, urls []string) (int, error) {
	input := struct {
		Host        string   `json:"host"`
		Key         string   `json:"key"`
		KeyLocation string   `json:"keyLocation,omitempty"`
		URLs        []string `json:"urlList"`
	}{
		Host: strings.TrimSuffix(strings.ToLower(base.Hostname()), "."), Key: secret.Secret,
		KeyLocation: config.KeyLocation, URLs: urls,
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
