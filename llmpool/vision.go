package llmpool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/norma/transcript"
)

// PrepareImage bounds compressed input, decoded pixels, dimensions and API bytes.
func PrepareImage(raw []byte) ([]byte, error) {
	if len(raw) > 5<<20 {
		return nil, fmt.Errorf("image exceeds 5 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if format != "png" && format != "jpeg" {
		return nil, fmt.Errorf("only PNG and JPEG are supported")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 24_000_000 {
		return nil, fmt.Errorf("image exceeds 24 million pixels")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	w, h := cfg.Width, cfg.Height
	if w > 2048 || h > 2048 {
		if w >= h {
			h = h * 2048 / w
			w = 2048
		} else {
			w = w * 2048 / h
			h = 2048
		}
	}
	for {
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		b := src.Bounds()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bb, a := src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h).RGBA()
				dst.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - a) >> 8), uint8((g + 65535 - a) >> 8), uint8((bb + 65535 - a) >> 8), 255})
			}
		}
		var out bytes.Buffer
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
			return nil, err
		}
		if base64.StdEncoding.EncodedLen(out.Len()) <= 512<<10 {
			return out.Bytes(), nil
		}
		w = w * 3 / 4
		h = h * 3 / 4
	}
}

type VisionConfig struct{ Format, BaseURL, APIKey, Model, Proxy, MaxTokensField, SessionHeaderKey string }

var visionSlots = make(chan struct{}, 4)

type VisionResult struct {
	Text                      string
	InputTokens, OutputTokens int
}

// AnalyzeImage is a one-shot lane. Image data is never returned to the main agent
// or passed through the raw LLM recorder. It never falls back to a text-only model.
func AnalyzeImage(ctx context.Context, c VisionConfig, jpegData []byte, question string) (VisionResult, error) {
	var result VisionResult
	if strings.TrimSpace(c.Model) == "" || strings.TrimSpace(question) == "" || len(question) > 8000 {
		return result, fmt.Errorf("model and a question (<=8000 bytes) are required")
	}
	b64 := base64.StdEncoding.EncodeToString(jpegData)
	select {
	case visionSlots <- struct{}{}:
		defer func() { <-visionSlots }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		if c.Format == "anthropic" {
			base = "https://api.anthropic.com"
		} else {
			base = "https://api.openai.com/v1"
		}
	}
	var endpoint string
	var body any
	switch c.Format {
	case "anthropic":
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		endpoint = base + "/messages"
		body = map[string]any{"model": c.Model, "max_tokens": 2048, "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": b64}}, map[string]any{"type": "text", "text": question}}}}}
	case "openai-responses":
		endpoint = base + "/responses"
		body = map[string]any{"model": c.Model, "max_output_tokens": 2048, "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": question}, map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + b64}}}}}
	case "openai":
		endpoint = base + "/chat/completions"
		field := "max_tokens"
		if c.MaxTokensField == "max_completion_tokens" {
			field = c.MaxTokensField
		}
		body = map[string]any{"model": c.Model, field: 2048, "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": question}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + b64}}}}}}
	default:
		return result, fmt.Errorf("unsupported vision format %q", c.Format)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Format == "anthropic" {
		req.Header.Set("x-api-key", c.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if c.SessionHeaderKey != "" {
		if id := transcript.SessionIDFrom(ctx); id != "" {
			req.Header.Set(c.SessionHeaderKey, id)
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	if c.Proxy != "" {
		p, err := url.Parse(c.Proxy)
		if err != nil {
			return result, err
		}
		transport.Proxy = http.ProxyURL(p)
	}
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("vision provider HTTP %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return result, err
	}
	for _, c := range out.Choices {
		result.Text += c.Message.Content
	}
	for _, c := range out.Content {
		result.Text += c.Text
	}
	for _, o := range out.Output {
		for _, c := range o.Content {
			result.Text += c.Text
		}
	}
	if strings.TrimSpace(result.Text) == "" {
		return result, fmt.Errorf("vision provider returned no text")
	}
	if len(result.Text) > 24000 {
		result.Text = string([]rune(result.Text)[:min(6000, len([]rune(result.Text)))])
	}
	result.InputTokens = out.Usage.Input + out.Usage.Prompt
	result.OutputTokens = out.Usage.Output + out.Usage.Completion
	return result, nil
}
