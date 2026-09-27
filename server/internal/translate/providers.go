package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var client = &http.Client{Timeout: 90 * time.Second}

// FromConfig picks a provider by which key is set. Gemini first: its free tier
// is the more generous of the two for occasional use.
func FromConfig(geminiKey, geminiModel, groqKey, groqModel string) Provider {
	switch {
	case geminiKey != "":
		if geminiModel == "" {
			// The "-latest" alias follows Google's current Flash model, so a
			// model retirement does not silently break translation.
			geminiModel = "gemini-flash-latest"
		}
		return &Gemini{Key: geminiKey, Model: geminiModel, Fallback: "gemini-flash-lite-latest"}
	case groqKey != "":
		if groqModel == "" {
			groqModel = "llama-3.3-70b-versatile"
		}
		return &Groq{Key: groqKey, Model: groqModel}
	}
	return nil
}

type Gemini struct {
	Key, Model string
	// Fallback is tried when Model stays overloaded: the free tier answers
	// "high demand" (503) or rate-limits (429) in bursts.
	Fallback string
}

func (g *Gemini) Name() string { return "gemini/" + g.Model }

func (g *Gemini) Complete(ctx context.Context, system, user string) (string, error) {
	models := []string{g.Model}
	if g.Fallback != "" && g.Fallback != g.Model {
		models = append(models, g.Fallback)
	}
	var err error
	for _, m := range models {
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(time.Duration(attempt*2) * time.Second):
				}
			}
			var out string
			if out, err = g.complete(ctx, m, system, user); err == nil {
				return out, nil
			}
			if !transient(err) {
				return "", err
			}
		}
	}
	return "", err
}

func (g *Gemini) complete(ctx context.Context, model, system, user string) (string, error) {
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]string{"text": system}}},
		"contents":          []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": user}}}},
		"generationConfig":  map[string]any{"responseMimeType": "application/json", "temperature": 0.2},
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent"
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := postJSON(ctx, url, map[string]string{"x-goog-api-key": g.Key}, body, &resp); err != nil {
		return "", fmt.Errorf("gemini/%s: %w", model, err)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini/%s: empty response", model)
	}
	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// httpError keeps the status so callers can tell "busy, retry" from "wrong key".
type httpError struct {
	Status int
	Msg    string
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg) }

func transient(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status == 429 || he.Status >= 500
	}
	return false
}

type Groq struct{ Key, Model string }

func (g *Groq) Name() string { return "groq/" + g.Model }

func (g *Groq) Complete(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": g.Model,
		"messages": []any{
			map[string]string{"role": "system", "content": system},
			map[string]string{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0.2,
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := postJSON(ctx, "https://api.groq.com/openai/v1/chat/completions",
		map[string]string{"Authorization": "Bearer " + g.Key}, body, &resp)
	if err != nil {
		return "", fmt.Errorf("groq: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("groq: empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

func postJSON(ctx context.Context, url string, headers map[string]string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		// Surface the provider's message (quota, bad key) but never echo
		// request headers, which hold the key.
		msg := string(data)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return &httpError{Status: resp.StatusCode, Msg: msg}
	}
	return json.Unmarshal(data, out)
}
