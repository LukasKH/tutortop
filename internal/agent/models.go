package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"
)

// Model is one model a provider offers, with the reasoning efforts it accepts.
type Model struct {
	ID          string   `json:"id"`   // passed to the CLI; "" means the CLI's own default
	Name        string   `json:"name"` // shown to people
	Description string   `json:"description,omitempty"`
	Efforts     []Effort `json:"efforts"`
	// DefaultEffort is used when none is chosen; "" means the CLI decides.
	DefaultEffort string `json:"defaultEffort,omitempty"`
}

// Effort is one reasoning level.
type Effort struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

func claudeModels(context.Context) ([]Model, error) { return claudeModelList, nil }

var claudeModelList = []Model{
	{ID: "", Name: "Default", Description: "Whatever Claude Code is set to use", Efforts: claudeEfforts},
	{ID: "fable", Name: "Fable", Description: "Most capable", Efforts: claudeEfforts, DefaultEffort: "medium"},
	{ID: "opus", Name: "Opus", Description: "Very capable, for complex work", Efforts: claudeEfforts, DefaultEffort: "medium"},
	{ID: "sonnet", Name: "Sonnet", Description: "Fast and capable for everyday work", Efforts: claudeEfforts, DefaultEffort: "medium"},
	{ID: "haiku", Name: "Haiku", Description: "Fastest and lightest", Efforts: claudeEfforts, DefaultEffort: "medium"},
}

// Claude Code takes model aliases that always point at the latest model of a
// family, so this list doesn't go stale with each release. Every current model
// takes the same efforts (as listed by `claude --help`), defaulting to medium.
var claudeEfforts = []Effort{
	{"low", "Fastest, light reasoning"},
	{"medium", "Balanced"},
	{"high", "Deeper reasoning"},
	{"xhigh", "Extra deep reasoning"},
	{"max", "Deepest reasoning, slowest"},
}

// codexModels asks a short-lived `codex app-server` for its model list, so
// the list follows the learner's account and CLI version.
func codexModels(ctx context.Context) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	proc, rpc, err := startCodexServer(ctx, os.TempDir(), func(string, json.RawMessage) {},
		func(string, json.RawMessage) (any, error) { return nil, errors.New("not supported") })
	if err != nil {
		return nil, err
	}
	defer proc.Close()

	var out []Model
	cursor := ""
	for range 10 { // pages; a handful of models fits in one
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		type modelPage struct {
			Data []struct {
				ID                        string `json:"id"`
				Model                     string `json:"model"`
				DisplayName               string `json:"displayName"`
				Description               string `json:"description"`
				Hidden                    bool   `json:"hidden"`
				IsDefault                 bool   `json:"isDefault"`
				DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
					Description     string `json:"description"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		res, err := rpc.Call[modelPage](ctx, "model/list", params)
		if err != nil {
			return nil, err
		}
		for _, m := range res.Data {
			if m.Hidden {
				continue
			}
			model := Model{ID: m.Model, Name: cmp.Or(m.DisplayName, m.Model), Description: m.Description, DefaultEffort: m.DefaultReasoningEffort}
			for _, e := range m.SupportedReasoningEfforts {
				model.Efforts = append(model.Efforts, Effort{ID: e.ReasoningEffort, Description: e.Description})
			}
			if m.IsDefault {
				out = append([]Model{model}, out...)
			} else {
				out = append(out, model)
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}
