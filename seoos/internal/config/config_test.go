package config

import (
	"strings"
	"testing"
)

func TestConfigurationIsExplicitAndDoesNotLeakCredentials(t *testing.T) {
	env := map[string]string{
		"SEOOS_DATABASE_URL":    "postgres://user:secret@example.invalid/seo",
		"SEOOS_ORGANIZATION_ID": "28fe744e-a79c-4a33-a647-1858bd2d9090",
		"SEOOS_API_TOKEN":       strings.Repeat("a", 32),
	}
	get := func(key string) string { return env[key] }
	cfg, err := Load(get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "127.0.0.1:8789" {
		t.Fatalf("unsafe default bind: %s", cfg.Address)
	}
	for _, key := range []string{"SEOOS_DATABASE_URL", "SEOOS_ORGANIZATION_ID", "SEOOS_API_TOKEN"} {
		t.Run(key, func(t *testing.T) {
			value := env[key]
			delete(env, key)
			defer func() { env[key] = value }()
			_, err := Load(get)
			if err == nil {
				t.Fatal("missing setting accepted")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), strings.Repeat("a", 32)) {
				t.Fatalf("error exposed credential: %v", err)
			}
		})
	}
	// A legacy DSN is not a fallback for a missing SEO DSN.
	delete(env, "SEOOS_DATABASE_URL")
	env["ARTEX_PG_DSN"] = "postgres://legacy"
	if _, err := Load(get); err == nil {
		t.Fatal("legacy configuration inherited")
	}
}

func TestRejectUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"SEOOS_API_TOKEN", strings.Repeat("a", 31)},
		{"SEOOS_API_TOKEN", strings.Repeat("a", 32) + "\n"},
		{"SEOOS_API_TOKEN", strings.Repeat("a", 32) + "中"},
		{"SEOOS_API_TOKEN", strings.Repeat("a", 513)},
		{"SEOOS_ORGANIZATION_ID", "00000000-0000-0000-0000-000000000000"},
		{"SEOOS_ADDR", "localhost"},
		{"SEOOS_ADDR", "127.0.0.1:70000"},
	} {
		t.Run(tc.key+"/"+tc.value[:min(12, len(tc.value))], func(t *testing.T) {
			env := map[string]string{"SEOOS_DATABASE_URL": "postgres://example.invalid/seo", "SEOOS_ORGANIZATION_ID": "28fe744e-a79c-4a33-a647-1858bd2d9090", "SEOOS_API_TOKEN": strings.Repeat("a", 32)}
			env[tc.key] = tc.value
			if _, err := Load(func(key string) string { return env[key] }); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
