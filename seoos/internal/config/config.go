package config

import (
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
}

// Load reads only SEO-specific settings. Legacy ARTEX credentials and database
// configuration are deliberately not inherited.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Address:          getenv("SEOOS_ADDR"),
		DatabaseURL:      getenv("SEOOS_DATABASE_URL"),
		OrganizationID:   getenv("SEOOS_ORGANIZATION_ID"),
		OrganizationName: strings.TrimSpace(getenv("SEOOS_ORGANIZATION_NAME")),
		APIToken:         getenv("SEOOS_API_TOKEN"),
	}
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
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return Config{}, errors.New("SEOOS_DATABASE_URL is required")
	}
	if !domain.ValidID(c.OrganizationID) {
		return Config{}, errors.New("SEOOS_ORGANIZATION_ID must be a nonzero canonical UUID")
	}
	if len(c.APIToken) < 32 || len(c.APIToken) > 512 || strings.ContainsFunc(c.APIToken, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r > unicode.MaxASCII }) {
		return Config{}, errors.New("SEOOS_API_TOKEN must be 32 to 512 ASCII characters without whitespace")
	}
	return c, nil
}
