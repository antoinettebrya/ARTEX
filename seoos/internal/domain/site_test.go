package domain

import (
	"errors"
	"strings"
	"testing"
)

func validInput() SiteInput {
	return SiteInput{Name: "Example", BaseURL: "https://example.com", CountryCode: "US", LanguageTag: "en-US"}
}

func TestSiteCanonicalization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"https://EXAMPLE.com:443/", "https://example.com"},
		{"http://example.com.:80/blog/", "http://example.com/blog"},
		{"https://example.com:8443/WordPress/", "https://example.com:8443/WordPress"},
		{"http://[2001:0db8::1]:80/", "http://[2001:db8::1]"},
		{"http://localhost:8789/site/", "http://localhost:8789/site"},
		{"https://xn--fsqu00a.xn--0zwm56d/", "https://xn--fsqu00a.xn--0zwm56d"},
		{"https://example.com/%77p/%7euser/", "https://example.com/wp/~user"},
		{"https://example.com/wp%2f", "https://example.com/wp%2F"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			in := validInput()
			in.BaseURL = tc.input
			in.Name = "  示例站点  "
			in.CountryCode = "cn"
			in.LanguageTag = "zh-hans"
			got, err := NormalizeSiteInput(in)
			if err != nil {
				t.Fatal(err)
			}
			if got.BaseURL != tc.want || got.Name != "示例站点" || got.CountryCode != "CN" || got.LanguageTag != "zh-Hans" || got.Status != "active" || got.CMS != "wordpress" {
				t.Fatalf("unexpected normalization: %+v", got)
			}
			again, err := NormalizeSiteInput(got)
			if err != nil || again != got {
				t.Fatalf("normalization must be idempotent: %+v, %v", again, err)
			}
		})
	}
}

func TestInvalidSiteAddresses(t *testing.T) {
	for _, address := range []string{
		"", "example.com", "ftp://example.com", "https://user:password@example.com",
		"https://example.com?token=secret", "https://example.com?", "https://example.com/#", "https://example.com/#fragment",
		"https://example.com:0", "https://example.com:65536", "https://example.com:bad", "https://example.com/../admin", "https://example.com/%2e%2e/admin",
		"https://example.com/path%00", "https://bad_host.com", "https://-bad.example", "https://example..com", "https://例子.测试", "https://example.com/path%5cadmin",
		"https://example.com/" + strings.Repeat("中", 400),
	} {
		t.Run(address, func(t *testing.T) {
			in := validInput()
			in.BaseURL = address
			_, err := NormalizeSiteInput(in)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != "base_url" {
				t.Fatalf("wanted address validation error, got %v", err)
			}
		})
	}
}

func TestSiteSettingsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*SiteInput)
	}{
		{"name", func(in *SiteInput) { in.Name = " \n " }},
		{"name", func(in *SiteInput) { in.Name = "Hello\x00World" }},
		{"cms", func(in *SiteInput) { in.CMS = "unsupported" }},
		{"status", func(in *SiteInput) { in.Status = "deleted" }},
		{"country_code", func(in *SiteInput) { in.CountryCode = "ZZ" }},
		{"country_code", func(in *SiteInput) { in.CountryCode = "USA" }},
		{"language_tag", func(in *SiteInput) { in.LanguageTag = "" }},
		{"language_tag", func(in *SiteInput) { in.LanguageTag = "not-a-language-123456789" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutate(&in)
			_, err := NormalizeSiteInput(in)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != tc.name {
				t.Fatalf("wanted %s error, got %v", tc.name, err)
			}
		})
	}
}

func TestIdentifiersAndPagination(t *testing.T) {
	if !ValidID("28fe744e-a79c-4a33-a647-1858bd2d9090") {
		t.Fatal("canonical ID rejected")
	}
	for _, id := range []string{"", "28FE744E-A79C-4A33-A647-1858BD2D9090", "00000000-0000-0000-0000-000000000000", "28fe744ea79c4a33a6471858bd2d9090"} {
		if ValidID(id) {
			t.Fatalf("invalid/noncanonical ID accepted: %s", id)
		}
	}
	for _, pair := range [][2]int{{0, 0}, {201, 0}, {50, -1}, {50, 100001}} {
		if ValidatePage(pair[0], pair[1]) == nil {
			t.Fatalf("invalid page accepted: %v", pair)
		}
	}
	if err := ValidatePage(200, 100000); err != nil {
		t.Fatal(err)
	}
}
