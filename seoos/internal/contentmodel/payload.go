package contentmodel

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func endpoint(kind string) string {
	switch kind {
	case domain.ProviderOpenAI:
		return "https://api.openai.com/v1/responses"
	case domain.ProviderAnthropic:
		return "https://api.anthropic.com/v1/chat/completions"
	case domain.ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case domain.ProviderDeepSeek:
		return "https://api.deepseek.com/chat/completions"
	}
	return ""
}

type modelAdapter struct {
	endpoint string
	protocol string
	decode   func([]byte) (parsedOutput, error)
}

func adapterFor(provider domain.Provider) modelAdapter {
	if provider.Kind == domain.ProviderCompatible {
		options := provider.Options()
		if options.Protocol == domain.ModelResponses {
			return modelAdapter{options.BaseURL + "/responses", domain.ModelResponses, parseResponses}
		}
		return modelAdapter{options.BaseURL + "/chat/completions", domain.ModelChat, parseChat}
	}
	if provider.Kind == domain.ProviderOpenAI {
		return modelAdapter{endpoint(provider.Kind), domain.ModelResponses, parseResponses}
	}
	if provider.Kind == domain.ProviderAnthropic && provider.Options().Protocol == domain.ModelAnthropic {
		return modelAdapter{"https://api.anthropic.com/v1/messages", domain.ModelAnthropic, parseAnthropic}
	}
	return modelAdapter{endpoint(provider.Kind), domain.ModelChat, parseChat}
}

type modelMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type modelInput struct {
	Stage    string          `json:"stage"`
	Language string          `json:"language"`
	Data     json.RawMessage `json:"data"`
}

func buildPayload(req domain.ContentModelRequest, call domain.ContentModelReservation) ([]byte, error) {
	p := call.Provider
	options := p.Options()
	protocol := adapterFor(p).protocol
	input, err := json.Marshal(modelInput{Stage: req.Stage, Language: req.Language, Data: req.Input})
	if err != nil {
		return nil, err
	}
	system := req.System
	if call.Profile.SystemPrompt != "" {
		system += "\nEditorial preferences for this provider:\n" + call.Profile.SystemPrompt
	}
	format := outputFormat(p, req.Stage)
	if schemaFor(req.Stage) != nil {
		system += "\nReturn exactly one JSON object."
	}
	params := map[string]any{"model": p.Model}
	if protocol == domain.ModelResponses {
		params["store"] = false
		params["max_output_tokens"] = p.MaxOutputTokens
		params["instructions"] = system
		params["input"] = string(input)
	} else if protocol == domain.ModelAnthropic {
		params["max_tokens"] = p.MaxOutputTokens
		params["system"] = system
		params["messages"] = []modelMessage{{Role: "user", Content: string(input)}}
	} else {
		tokenField := options.TokenField
		if tokenField == "" {
			tokenField = "max_tokens"
		}
		params[tokenField] = p.MaxOutputTokens
		params["messages"] = []modelMessage{{Role: "system", Content: system}, {Role: "user", Content: string(input)}}
	}
	if includeTemperature(p) && (p.Kind != domain.ProviderAnthropic || call.Profile.Temperature <= 1) {
		params["temperature"] = call.Profile.Temperature
	}
	if effort := reasoningEffort(p); effort != "" {
		switch {
		case protocol == domain.ModelResponses:
			params["reasoning"] = map[string]string{"effort": effort}
		case p.Kind == domain.ProviderDeepSeek:
			if effort == "none" {
				params["thinking"] = map[string]string{"type": "disabled"}
			} else {
				params["thinking"] = map[string]string{"type": "enabled"}
				params["reasoning_effort"] = effort
			}
		default:
			params["reasoning_effort"] = effort
		}
	}
	if format != domain.ModelText {
		applyFormat(params, protocol, format, req.Stage)
	}
	return json.Marshal(params)
}

func baseModel(model string) string {
	name := strings.ToLower(model)
	if original, ok := strings.CutPrefix(name, "ft:"); ok {
		name, _, _ = strings.Cut(original, ":")
	}
	return name
}

func reasoningEffort(p domain.Provider) string {
	effort := p.Options().Reasoning
	name := baseModel(p.Model)
	// Gemini Pro and newer generations always reason. Existing saved "none"
	// configurations use the upstream default instead of producing HTTP 400.
	if p.Kind == domain.ProviderGemini && effort == "none" &&
		!strings.HasPrefix(name, "gemini-2.5-flash") {
		return ""
	}
	return effort
}

func includeTemperature(p domain.Provider) bool {
	options := p.Options()
	if options.Temperature == "off" || options.Reasoning != "" && options.Reasoning != "none" {
		return false
	}
	model := baseModel(p.Model)
	switch p.Kind {
	case domain.ProviderOpenAI:
		for _, prefix := range []string{"o1", "o3", "o4"} {
			if strings.HasPrefix(model, prefix) {
				return false
			}
		}
		if strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6") {
			var minor int
			_, _ = fmt.Sscanf(model, "gpt-5.%d", &minor)
			return minor >= 1 && options.Reasoning == "none" && options.Temperature == "on"
		}
		return options.Temperature == "on" || strings.HasPrefix(model, "gpt-4")
	case domain.ProviderDeepSeek:
		return options.Temperature == "on" && (options.Reasoning == "none" || model == "deepseek-chat")
	case domain.ProviderAnthropic:
		if strings.Contains(model, "mythos") {
			return false
		}
		for _, family := range []string{"claude-opus-", "claude-sonnet-", "claude-haiku-"} {
			if version, ok := strings.CutPrefix(model, family); ok {
				var major, minor int
				_, _ = fmt.Sscanf(version, "%d-%d", &major, &minor)
				if major > 4 || major == 4 && minor >= 7 {
					return false
				}
			}
		}
		return options.Temperature == "on"
	default:
		// Only send a sampling override when the owner explicitly enables it.
		return options.Temperature == "on"
	}
}

func outputFormat(p domain.Provider, stage string) string {
	if schemaFor(stage) == nil {
		return domain.ModelText
	}
	if output := p.Options().Output; output != "" {
		return output
	}
	switch p.Kind {
	case domain.ProviderDeepSeek, domain.ProviderGemini:
		return domain.ModelJSON
	case domain.ProviderOpenAI:
		for _, prefix := range []string{"gpt-4.1", "gpt-5", "gpt-6", "o3", "o4"} {
			if strings.HasPrefix(baseModel(p.Model), prefix) {
				return domain.ModelSchema
			}
		}
	}
	return domain.ModelText
}

func applyFormat(params map[string]any, protocol, format, stage string) {
	if protocol == domain.ModelAnthropic {
		if format == domain.ModelSchema {
			params["output_config"] = map[string]any{"format": map[string]any{"type": format, "schema": schemaFor(stage)}}
		}
		return
	}
	wire := map[string]any{"type": format}
	if format == domain.ModelSchema {
		schema := map[string]any{"name": "artex_" + stage, "strict": true, "schema": schemaFor(stage)}
		if protocol == domain.ModelResponses {
			for key, value := range schema {
				wire[key] = value
			}
		} else {
			wire["json_schema"] = schema
		}
	}
	if protocol == domain.ModelResponses {
		params["text"] = map[string]any{"format": wire}
	} else {
		params["response_format"] = wire
	}
}

func schemaFor(stage string) map[string]any {
	field := func(kind string) map[string]any { return map[string]any{"type": kind} }
	list := func(items any) map[string]any { return map[string]any{"type": "array", "items": items} }
	object := func(properties map[string]any) map[string]any {
		required := make([]string, 0, len(properties))
		for property := range properties {
			required = append(required, property)
		}
		slices.Sort(required)
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	switch stage {
	case "outline":
		return object(map[string]any{"headings": list(object(map[string]any{
			"level": field("integer"), "heading": field("string"), "purpose": field("string"),
		}))})
	case "section":
		return object(map[string]any{"body": field("string"), "claims": list(object(map[string]any{
			"text": field("string"), "fact_ids": list(field("string")),
		}))})
	case "review":
		return object(map[string]any{"passed": field("boolean"), "intent": field("boolean"), "language": field("boolean"),
			"original_value": field("boolean"), "issues": list(field("string"))})
	case "intent":
		return object(map[string]any{"primary": field("string"), "secondary": field("string"), "need": field("string"),
			"page_type": field("string"), "confidence": field("number"), "reasons": list(field("string"))})
	}
	return nil
}

func matchesSchema(schema map[string]any, value any) bool {
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		fields := schema["properties"].(map[string]any)
		if !ok || len(object) != len(fields) {
			return false
		}
		for key, field := range fields {
			if child, ok := object[key]; !ok || !matchesSchema(field.(map[string]any), child) {
				return false
			}
		}
		return true
	case "array":
		array, ok := value.([]any)
		if !ok {
			return false
		}
		for _, child := range array {
			if !matchesSchema(schema["items"].(map[string]any), child) {
				return false
			}
		}
		return true
	case "integer":
		number, ok := value.(float64)
		return ok && math.Trunc(number) == number
	case "number":
		_, ok := value.(float64)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

// EstimateInputTokens uses the actual serialized request and a framing margin
// as a conservative operational estimate, independent of vendor tokenizers.
func EstimateInputTokens(req domain.ContentModelRequest, p domain.Provider, profile domain.ContentModelProfile) (int64, error) {
	body, err := buildPayload(req, domain.ContentModelReservation{Provider: p, Profile: profile})
	return int64(len(body) + 128), err
}
