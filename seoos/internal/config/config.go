package config

import (
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type Config struct {
	Address          string
	DatabaseURL      string
	OrganizationID   string
	OrganizationName string
	APIToken         string
	ProviderKey      []byte
}

// Load reads only SEO-specific settings. Legacy ARTEX credentials and database
// configuration are deliberately not inherited.
func Load(getenv func(string) string) (Config, error) {
	c, err := LoadStorage(getenv)
	if err != nil {
		return Config{}, err
	}
	c.Address = getenv("SEOOS_ADDR")
	c.OrganizationID = getenv("SEOOS_ORGANIZATION_ID")
	c.OrganizationName = strings.TrimSpace(getenv("SEOOS_ORGANIZATION_NAME"))
	c.APIToken = getenv("SEOOS_API_TOKEN")
	if c.Address == "" {
		c.Address = "127.0.0.1:8789"
	}
	if c.OrganizationName == "" {
		c.OrganizationName = "SEO OS"
	}
	_, port, err := net.SplitHostPort(c.Address)
	p, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || p < 1 || p > 65535 {
		return Config{}, errors.New("SEOOS_ADDR must be a host:port address with a port between 1 and 65535")
	}
	if !domain.ValidID(c.OrganizationID) {
		return Config{}, errors.New("SEOOS_ORGANIZATION_ID must be a nonzero canonical UUID")
	}
	if len(c.APIToken) < 32 || len(c.APIToken) > 512 || strings.ContainsFunc(c.APIToken, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r > unicode.MaxASCII }) {
		return Config{}, errors.New("SEOOS_API_TOKEN must be 32 to 512 ASCII characters without whitespace")
	}
	return c, nil
}

// LoadStorage reads the settings needed by a standalone worker, which does
// not authenticate HTTP requests or bootstrap an organization.
func LoadStorage(getenv func(string) string) (Config, error) {
	c := Config{DatabaseURL: getenv("SEOOS_DATABASE_URL")}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return Config{}, errors.New("SEOOS_DATABASE_URL is required")
	}
	if raw := getenv("SEOOS_PROVIDER_KEY"); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			return Config{}, errors.New("SEOOS_PROVIDER_KEY must be 32 bytes encoded as standard base64")
		}
		c.ProviderKey = key
	}
	return c, nil
}
