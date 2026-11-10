package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
	actool "github.com/Autumn-27/norma/tool"
)

func TestBrowserImportAuthenticatedRoute(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	m.traffic, err = traffic.Open(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	td := t.TempDir()
	s := newTestServer(t, t.Context(), m, td, td, td)
	h := s.Handler()
	token, err := signTestJWT(t, s)
	if err != nil {
		t.Fatal(err)
	}
	task, err := m.CreateTask("browser fixture", "fixture", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.pg.DeleteTask(mustTaskID(t, task.ID)) })
	input := map[string]any{"task_id": task.ID, "exchange": map[string]any{"url": "https://browser.example.test/api", "method": "GET", "status": 200, "response_body": "fixture"}}
	send := func(auth bool, path string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if r := send(false, "/api/browser/import"); r.Code != 401 {
		t.Fatalf("unauthenticated import: %d", r.Code)
	}
	first := send(true, "/api/browser/import")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	second := send(true, "/api/browser/import")
	if second.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatalf("not idempotent: %s %s", first.Body.String(), second.Body.String())
	}
	input["create_intent"] = true
	if r := send(true, "/api/browser/import"); r.Code != 409 {
		t.Fatalf("exploration without traffic tools: %d %s", r.Code, r.Body.String())
	}
}

func TestVisionRouteBindingPathBoundaryAndSummary(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	td := t.TempDir()
	s := newTestServer(t, t.Context(), m, td, td, td)
	h := s.Handler()
	token, err := signTestJWT(t, s)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"fixture screen summary"}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`))
	}))
	defer provider.Close()
	id, err := m.pg.SaveProfile(&db.LLMProfile{Name: t.Name(), Format: "openai", Model: "fixture", APIKey: "fixture", BaseURL: provider.URL})
	if err != nil {
		t.Fatal(err)
	}
	old, _, _ := m.pg.GetSetting(visionProfileSetting)
	t.Cleanup(func() { m.pg.SetSetting(visionProfileSetting, old); m.pg.DeleteProfile(id) })
	put := func(value int64, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "/api/llm/vision", strings.NewReader(`{"profile_id":`+strconv.FormatInt(value, 10)+`}`))
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if r := put(id, false); r.Code != 401 {
		t.Fatal("unauthenticated vision setting", r.Code)
	}
	if r := put(-1, true); r.Code != 400 {
		t.Fatal("negative profile accepted")
	}
	if r := put(id, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	dir := t.TempDir()
	var raw bytes.Buffer
	png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	os.WriteFile(filepath.Join(dir, "screen.png"), raw.Bytes(), 0600)
	call := func(path string) actool.Result {
		in, _ := json.Marshal(map[string]string{"image_path": path, "question": "fixture"})
		r, e := s.visionTool().Call(t.Context(), in, &actool.ToolContext{WorkingDir: dir})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	if r := call("../screen.png"); !r.IsError || calls.Load() != 0 {
		t.Fatal("image path escaped run directory")
	}
	if r := call("screen.png"); r.IsError || r.Flatten() != "fixture screen summary" || calls.Load() != 1 {
		t.Fatal(r.Flatten())
	}
	if r := put(0, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call("screen.png"); !r.IsError || calls.Load() != 1 {
		t.Fatal("disabled vision still invoked provider")
	}
}
