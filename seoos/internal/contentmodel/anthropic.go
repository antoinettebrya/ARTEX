package contentmodel

import (
	"encoding/json"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func parseAnthropic(raw []byte) (parsedOutput, error) {
	var reply struct {
		ID, Model  string
		StopReason string `json:"stop_reason"`
		Content    []struct{ Type, Text string }
		Usage      struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
			Read   *int64 `json:"cache_read_input_tokens"`
			Write  *int64 `json:"cache_creation_input_tokens"`
		}
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return parsedOutput{}, err
	}
	out := parsedOutput{response: domain.ContentModelResponse{Model: reply.Model, ResponseID: reply.ID}}
	if reply.Usage.Input != nil {
		input := *reply.Usage.Input
		if input < 0 || input > 2_000_000 {
			out.badUsage = true
			return out, nil
		}
		for _, count := range []*int64{reply.Usage.Read, reply.Usage.Write} {
			if count != nil {
				if *count < 0 || *count > 2_000_000 {
					out.badUsage = true
					return out, nil
				}
				input += *count
			}
		}
		out.usage(&input, reply.Usage.Output, reply.Usage.Read)
	}
	switch reply.StopReason {
	case "end_turn", "stop_sequence":
		var content strings.Builder
		for _, block := range reply.Content {
			if block.Type == "text" {
				content.WriteString(block.Text)
			}
		}
		out.response.Text = content.String()
	case "refusal":
		out.code, out.category, out.upstream = "content_model_refused", "refusal", reply.StopReason
	default:
		out.code, out.category, out.upstream = "content_model_incomplete", "response", reply.StopReason
	}
	return out, nil
}
