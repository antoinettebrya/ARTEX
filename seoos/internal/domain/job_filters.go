package domain

import (
	"regexp"
	"time"
)

type SiteScope struct {
	SiteID   string
	Country  string
	Language string
}

var scopeCountry = regexp.MustCompile(`^[A-Z]{2}$`)
var scopeLanguage = regexp.MustCompile(`^[A-Za-z0-9-]{2,100}$`)

func (f SiteScope) Validate() error {
	if f.SiteID != "" && !ValidID(f.SiteID) {
		return invalid("site_id", "choose a valid site ID")
	}
	if f.Country != "" && !scopeCountry.MatchString(f.Country) {
		return invalid("site_country", "choose a two-letter uppercase country code")
	}
	if f.Language != "" && !scopeLanguage.MatchString(f.Language) {
		return invalid("site_language", "choose a valid language tag")
	}
	return nil
}

type JobFilter struct {
	SiteScope
	State string
	Kind  string
	Day   string
}

func (f JobFilter) Validate() error {
	if err := f.SiteScope.Validate(); err != nil {
		return err
	}
	switch f.State {
	case "", "queued", "running", "succeeded", "failed", "cancelled":
	default:
		return invalid("state", "choose a job state")
	}
	if f.Kind != "" && f.Kind != JobSiteBrief && f.Kind != JobSiteInventory &&
		!IsContentKind(f.Kind) && !IsExecutionKind(f.Kind) && !IsResearchKind(f.Kind) {
		return invalid("kind", "choose a job type")
	}
	if f.Day != "" {
		if _, err := time.Parse(time.DateOnly, f.Day); err != nil {
			return invalid("day", "choose a valid UTC date")
		}
	}
	return nil
}
