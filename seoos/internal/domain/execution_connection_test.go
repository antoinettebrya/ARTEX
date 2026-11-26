package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestWordPressConnectionUsesCanonicalRegisteredInstall(t *testing.T) {
	site := Site{ID: "installed-site", BaseURL: "https://wordpress.example.test/blog"}
	for _, address := range []string{"", "https://WORDPRESS.example.test:443/blog/"} {
		connection, err := (ExecutionConnectionInput{
			SiteID: site.ID, Name: strings.Repeat("名", 120), Kind: ExecutionWordPress, BaseURL: address,
		}).Normalize(site)
		if err != nil || connection.BaseURL != site.BaseURL {
			t.Fatalf("registered WordPress install rejected: %v", err)
		}
	}
	for _, address := range []string{
		"https://wordpress.example.test/blog/wp-json",
		"https://wordpress.example.test/blog/another-site",
		"https://untrusted.example.test/blog",
		"http://wordpress.example.test/blog",
	} {
		_, err := (ExecutionConnectionInput{
			SiteID: site.ID, Name: "WordPress", Kind: ExecutionWordPress, BaseURL: address,
		}).Normalize(site)
		var field *ValidationError
		if !errors.As(err, &field) || field.Field != "base_url" {
			t.Errorf("%s should be rejected as an installation URL: %v", address, err)
		}
	}
}

func TestWordPressCredentialIdentifiesMissingUsername(t *testing.T) {
	input := ExecutionConnectionInput{Kind: ExecutionWordPress}
	credentials := ExecutionCredential{Username: "editor", Secret: "abcd efgh ijkl mnop qrst uvwx"}
	if err := credentials.Validate(input); err != nil {
		t.Fatalf("the grouped application password should be accepted: %v", err)
	}
	credentials.Username = "invalid:user"
	var field *ValidationError
	if err := credentials.Validate(input); !errors.As(err, &field) || field.Field != "username" {
		t.Fatalf("expected a username validation error: %v", err)
	}
}
