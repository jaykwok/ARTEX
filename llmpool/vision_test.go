package llmpool

import (
	"bytes"
	"encoding/base64"
	"github.com/Autumn-27/norma/transcript"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrepareImageBoundsAndAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3000, 10))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var raw bytes.Buffer
	if err := png.Encode(&raw, src); err != nil {
		t.Fatal(err)
	}
	out, err := PrepareImage(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width > 2048 || base64.StdEncoding.EncodedLen(len(out)) > 512<<10 {
		t.Fatal(cfg, err)
	}
	if _, err := PrepareImage(make([]byte, (5<<20)+1)); err == nil {
		t.Fatal("oversized image accepted")
	}
	if _, err := PrepareImage([]byte("not an image")); err == nil {
		t.Fatal("invalid image accepted")
	}
}
func TestVisionProviderFormats(t *testing.T) {
	for _, format := range []string{"openai", "openai-responses", "anthropic"} {
		t.Run(format, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "fixture question") || !strings.Contains(string(body), "aW1hZ2U=") {
					t.Error("missing multimodal payload")
				}
				if format == "anthropic" && r.Header.Get("x-api-key") != "fixture" {
					t.Error("missing key")
				}
				if r.Header.Get("X-Fixture-Session") != "vision-fixture" {
					t.Error("profile session header missing")
				}
				if format == "openai" && !strings.Contains(string(body), `"max_completion_tokens":2048`) {
					t.Error("profile token field ignored")
				}
				w.Header().Set("Content-Type", "application/json")
				switch format {
				case "openai":
					io.WriteString(w, `{"choices":[{"message":{"content":"screen summary"}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`)
				case "openai-responses":
					io.WriteString(w, `{"output":[{"content":[{"type":"output_text","text":"screen summary"}]}],"usage":{"input_tokens":11,"output_tokens":3}}`)
				default:
					io.WriteString(w, `{"content":[{"type":"text","text":"screen summary"}],"usage":{"input_tokens":11,"output_tokens":3}}`)
				}
			}))
			defer srv.Close()
			res, err := AnalyzeImage(transcript.WithSessionID(t.Context(), "vision-fixture"), VisionConfig{Format: format, BaseURL: srv.URL, APIKey: "fixture", Model: "vision", SessionHeaderKey: "X-Fixture-Session", MaxTokensField: "max_completion_tokens"}, []byte("image"), "fixture question")
			if err != nil || res.Text != "screen summary" || res.InputTokens != 11 || res.OutputTokens != 3 {
				t.Fatal(res, err)
			}
		})
	}
}
