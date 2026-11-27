package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestWordPressConnectionUsesCanonicalRegisteredInstall(t *testing.T) {
	site := Site{ID: "installed-site", BaseURL: "https://wordpress.example.test/blog"}
	for _, tc := range []struct{ address, want string }{
		{"", site.BaseURL},
		{"https://WORDPRESS.example.test:443/blog/", site.BaseURL},
		{"https://wordpress.example.test/blog/cms/", site.BaseURL + "/cms"},
	} {
		connection, err := (ExecutionConnectionInput{
			SiteID: site.ID, Name: strings.Repeat("名", 120), Kind: ExecutionWordPress, BaseURL: tc.address,
		}).Normalize(site)
		if err != nil || connection.BaseURL != tc.want {
			t.Fatalf("registered WordPress install rejected: %v", err)
		}
	}
	for _, address := range []string{
		"https://wordpress.example.test/blog/wp-json",
		"https://wordpress.example.test/blog-other",
		"https://wordpress.example.test/outside",
		"https://wordpress.example.test/blog%2fcms",
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

func TestGoogleConnectionRedirects(t *testing.T) {
	site := Site{ID: "search-site", BaseURL: "https://search.example.test"}
	const console = "https://console.example.test"
	for _, path := range []string{GoogleOAuthCallbackPath, "/seo/execution", "/seo/execution/"} {
		for _, kind := range []struct{ service, property string }{
			{ExecutionGSC, "sc-domain:search.example.test"},
			{ExecutionGA4, "properties/12345"},
		} {
			input := ExecutionConnectionInput{SiteID: site.ID, Name: "Google", Kind: kind.service,
				Property: kind.property, RedirectURI: console + path}
			got, err := input.Normalize(site)
			if err != nil || got.RedirectURI != input.RedirectURI {
				t.Errorf("%s with redirect %s: %v", kind.service, path, err)
			}
		}
	}
	for _, redirect := range []string{
		console + GoogleOAuthCallbackPath + "/",
		console + GoogleOAuthCallbackPath + "?",
		console + GoogleOAuthCallbackPath + "?state=external",
		console + GoogleOAuthCallbackPath + "#unexpected",
		console + "/seo/api/integrations/oauth/callback/%67oogle",
		console + "/seo/api/integrations/oauth/callback/bing",
		"http://console.example.test" + GoogleOAuthCallbackPath,
	} {
		input := ExecutionConnectionInput{SiteID: site.ID, Name: "Google", Kind: ExecutionGSC,
			Property: "sc-domain:search.example.test", RedirectURI: redirect}
		_, err := input.Normalize(site)
		var field *ValidationError
		if !errors.As(err, &field) || field.Field != "redirect_uri" {
			t.Errorf("unexpected result for callback %s: %v", redirect, err)
		}
	}
}
