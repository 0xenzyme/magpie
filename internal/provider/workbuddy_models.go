package provider

// A WorkBuddy plan's models are the ones WorkBuddy's product config gives
// its CLI agent: GET /v3/config, signed as a chat is, answers with the
// product's agents (the "cli" one's models are WorkBuddy's picker) and each
// model's details. Which product answers goes by the User-Agent: WorkBuddy
// sends "CLI/<version> WorkBuddy/<version>", and a bare WorkBuddy/<version>
// gets CodeBuddy IDE's config, with no cli agent.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/yetone/magpie/internal/catalog"
)

type wbProductConfig struct {
	Agents []struct {
		Name   string   `json:"name"`
		Models []string `json:"models"`
	} `json:"agents"`
	Models []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		MaxInputTokens  int    `json:"maxInputTokens"`
		MaxOutputTokens int    `json:"maxOutputTokens"`
		SupportsImages  *bool  `json:"supportsImages"`
		OnlyReasoning   bool   `json:"onlyReasoning"`
		Reasoning       struct {
			SupportedEfforts   []string `json:"supportedEfforts"`
			CanDisableThinking *bool    `json:"canDisableThinking"`
		} `json:"reasoning"`
	} `json:"models"`
}

// wbFetchModels asks WorkBuddy's product config for the plan's models,
// with sign signing the request as the account's chats are.
func wbFetchModels(ctx context.Context, w *wbSite, sign func(context.Context, *http.Request, []byte) error) ([]catalog.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.api()+"/v3/config", nil)
	if err != nil {
		return nil, err
	}
	if err := sign(ctx, req, nil); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CLI/"+wbAppVersion+" WorkBuddy/"+wbUAVersion)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data wbProductConfig `json:"data"`
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("WorkBuddy's config: %s", APIError(b, res.Status))
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("WorkBuddy's config: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("WorkBuddy's config: %s (%d)", env.Msg, env.Code)
	}
	ms := env.Data.cliModels()
	if len(ms) == 0 {
		return nil, fmt.Errorf("WorkBuddy's config lists no models for its CLI agent")
	}
	return ms, nil
}

func (c wbProductConfig) cliModels() []catalog.Model {
	var out []catalog.Model
	for _, a := range c.Agents {
		if a.Name != "cli" {
			continue
		}
		for _, id := range a.Models {
			m := catalog.Model{ID: id, Name: id}
			for _, d := range c.Models {
				if d.ID != id {
					continue
				}
				if d.Name != "" {
					m.Name = d.Name
				}
				m.Context, m.Output = d.MaxInputTokens, d.MaxOutputTokens
				if d.SupportsImages != nil {
					m.Images, m.ImageInput = *d.SupportsImages, d.SupportsImages
				}
				// the levels WorkBuddy offers the model, and off when it
				// offers that; a model it gives no levels takes any
				if es := d.Reasoning.SupportedEfforts; len(es) > 0 {
					if !d.OnlyReasoning && (d.Reasoning.CanDisableThinking == nil || *d.Reasoning.CanDisableThinking) {
						m.Efforts = append(m.Efforts, "none")
					}
					m.Efforts = append(m.Efforts, es...)
				}
				break
			}
			out = append(out, m)
		}
	}
	return out
}
