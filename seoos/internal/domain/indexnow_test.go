package domain

import (
	"fmt"
	"strings"
	"testing"
)

func TestIndexNowKeyAndFileRules(t *testing.T) {
	all := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-"
	for _, key := range []string{all, "GZgz12-9", "--------", "01234567", strings.Repeat("Z", 128)} {
		if !ValidIndexNowKey(key) {
			t.Fatalf("legal key of length %d rejected", len(key))
		}
	}
	for _, key := range []string{"", "short-7", "GZgz 129", " GZgz129", "GZgz129 ", "GZgz/129", "GZgz\n129", "保GZgz129", strings.Repeat("G", 129)} {
		if ValidIndexNowKey(key) {
			t.Fatalf("invalid key of length %d accepted", len(key))
		}
	}
	const key = "GZgz-01290"
	for _, directory := range []string{"/", "/catalog/"} {
		location, err := IndexNowLocation("https://example.test", directory, key)
		if err != nil || location != "https://example.test"+directory+key+".txt" {
			t.Fatalf("verification file path for %s: %s %v", directory, location, err)
		}
		input := ExecutionConnectionInput{Kind: ExecutionIndexNow, KeyLocation: location}
		if err := (ExecutionCredential{Secret: key}).Validate(input); err != nil {
			t.Fatal(err)
		}
		input.KeyLocation = strings.ToLower(location)
		if err := (ExecutionCredential{Secret: key}).Validate(input); err == nil {
			t.Fatal("a filename with different capitalization was accepted")
		}
	}
}

func TestIndexNowAPIJobBatchLimit(t *testing.T) {
	in := ExecutionJobInput{Kind: ExecIndexNow, SiteID: "45311f7b-4b64-4098-a069-47cca9130efd",
		IdempotencyKey: "indexnow-1000-test"}
	for range IndexNowMaxBatchURLs {
		in.URLs = append(in.URLs, fmt.Sprintf("https://example.test/catalog/%d", len(in.URLs)))
	}
	if err := in.Validate(); err != nil {
		t.Fatalf("a 1,000 URL job was rejected: %v", err)
	}
	in.URLs = append(in.URLs, "https://example.test/catalog/extra")
	if err := in.Validate(); err == nil {
		t.Fatal("a 1,001 URL job was accepted")
	}
}
