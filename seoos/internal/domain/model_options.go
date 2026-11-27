package domain

import (
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

const (
	ModelResponses = "responses"
	ModelAnthropic = "anthropic_messages"
	ModelChat      = "chat"
	ModelText      = "text"
	ModelJSON      = "json_object"
	ModelSchema    = "json_schema"
)

// ModelOptions contain routing and capabilities. The credential stays in the
// encrypted credential column and is never stored as an option.
type ModelOptions struct {
	BaseURL     string `json:"base_url,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	AuthHeader  string `json:"auth_header,omitempty"`
	AuthMode    string `json:"auth_mode,omitempty"`
	TokenField  string `json:"token_field,omitempty"`
	Temperature string `json:"temperature,omitempty"`
	Reasoning   string `json:"reasoning_effort,omitempty"`
	Output      string `json:"output_format,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

func (in ModelOptions) normalize(kind string) (ModelOptions, error) {
	if kind == ProviderCompatible {
		base, err := normalizeURL(in.BaseURL)
		if err != nil || !strings.HasPrefix(base, "https://") {
			return in, invalid("base_url", "use an absolute HTTPS API base URL without credentials, query, or fragment")
		}
		u, _ := url.Parse(base)
		host := u.Hostname()
		literal, literalErr := netip.ParseAddr(host)
		if !strings.Contains(host, ".") && !strings.Contains(host, ":") ||
			u.Port() != "" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
			literalErr == nil && (!literal.IsGlobalUnicast() || literal.IsPrivate() || literal.IsLoopback() || literal.IsLinkLocalUnicast()) {
			return in, invalid("base_url", "use a public API hostname")
		}
		if strings.HasSuffix(u.Path, "/chat/completions") || strings.HasSuffix(u.Path, "/responses") || strings.HasSuffix(u.Path, "/models") {
			return in, invalid("base_url", "use an API base URL, for example https://api.example.com/v1")
		}
		in.BaseURL = base
		in.AuthHeader = http.CanonicalHeaderKey(strings.TrimSpace(in.AuthHeader))
		switch {
		case in.AuthHeader == "" || in.AuthHeader == "Authorization":
			in.AuthHeader = "Authorization"
			if in.AuthMode == "" {
				in.AuthMode = "bearer"
			}
		case in.AuthHeader == "Api-Key" || strings.HasPrefix(in.AuthHeader, "X-") && compatibleHeader(in.AuthHeader):
			if in.AuthMode == "" {
				in.AuthMode = "raw"
			}
		default:
			return in, invalid("auth_header", "choose Authorization, Api-Key, or a dedicated X-* credential header")
		}
		if in.AuthMode != "bearer" && in.AuthMode != "raw" {
			return in, invalid("auth_mode", "choose Bearer or raw key authentication")
		}
		if in.Protocol == "" {
			in.Protocol = ModelChat
		}
		if in.Protocol != ModelResponses && in.Protocol != ModelChat {
			return in, invalid("protocol", "choose Responses API or Chat Completions")
		}
		if in.TokenField != "" && (in.Protocol != ModelChat || in.TokenField != "max_completion_tokens" && in.TokenField != "max_tokens") {
			return in, invalid("token_field", "choose a Chat Completions output token parameter")
		}
	} else if in.BaseURL != "" || in.Protocol != "" && !(kind == ProviderAnthropic && in.Protocol == ModelAnthropic) ||
		in.AuthHeader != "" || in.AuthMode != "" || in.TokenField != "" {
		return in, invalid("base_url", "custom routing and authentication require an OpenAI-compatible provider")
	}
	if in.WorkspaceID != "" && (kind != ProviderAnthropic || !strings.HasPrefix(in.WorkspaceID, "wrkspc_") ||
		len(in.WorkspaceID) > 120 || !identifier(in.WorkspaceID)) {
		return in, invalid("workspace_id", "enter a Claude workspace ID")
	}
	switch in.Temperature {
	case "", "on", "off":
	default:
		return in, invalid("temperature", "choose automatic, enabled or disabled")
	}
	switch in.Output {
	case "", ModelText, ModelSchema:
		if in.Output == ModelSchema && kind == ProviderDeepSeek {
			return in, invalid("output_format", "DeepSeek Chat Completions supports JSON Object")
		}
	case ModelJSON:
		if kind == ProviderAnthropic {
			return in, invalid("output_format", "use strict JSON Schema or plain text for Claude")
		}
	default:
		return in, invalid("output_format", "choose a supported JSON format")
	}
	switch in.Reasoning {
	case "":
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		if kind == ProviderAnthropic || kind == ProviderDeepSeek && in.Reasoning != "none" && in.Reasoning != "high" && in.Reasoning != "max" ||
			kind == ProviderGemini && (in.Reasoning == "xhigh" || in.Reasoning == "max") || kind == ProviderOpenAI && in.Reasoning == "max" {
			return in, invalid("reasoning_effort", "choose an effort supported by this provider")
		}
	default:
		return in, invalid("reasoning_effort", "choose a supported reasoning effort")
	}
	return in, nil
}

func compatibleHeader(header string) bool {
	if len(header) <= 2 || len(header) > 64 {
		return false
	}
	for _, prefix := range []string{"X-Forwarded-", "X-Original-", "X-Rewrite-", "X-Accel-", "X-Http-", "X-Method-", "X-Real-", "X-Seoos-"} {
		if strings.HasPrefix(header, prefix) {
			return false
		}
	}
	for _, c := range header {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func (p Provider) Options() ModelOptions {
	if p.ModelOptions == nil {
		return ModelOptions{}
	}
	return *p.ModelOptions
}

func (p Provider) ModelAuthorization(header http.Header, key string) {
	settings := p.Options()
	if p.Kind == ProviderAnthropic && settings.WorkspaceID != "" {
		header.Set("Anthropic-Workspace-Id", settings.WorkspaceID)
	}
	if p.Kind == ProviderAnthropic && settings.Protocol == ModelAnthropic {
		header.Set("X-Api-Key", key)
		header.Set("Anthropic-Version", "2023-06-01")
		return
	}
	if p.Kind == ProviderCompatible && settings.AuthHeader != "" {
		if settings.AuthMode == "bearer" {
			key = "Bearer " + key
		}
		header.Set(settings.AuthHeader, key)
		return
	}
	header.Set("Authorization", "Bearer "+key)
}
