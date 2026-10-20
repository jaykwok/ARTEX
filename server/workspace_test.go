package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkspaceProtectsInternalStores(t *testing.T) {
	dir := t.TempDir()
	s := &Server{m: &Manager{dir: dir}}
	protected := []string{"artex.sqlite", "artex.sqlite-wal", "artex.sqlite-shm", "traffic/store.db", "evidence/blobs/body.bin", "archives/tasks/task-1-1.tar.zst"}
	for _, name := range protected {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("internal"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range protected {
		data, _ := json.Marshal(map[string]string{"path": name, "content": "replacement"})
		s.wsWrite(httptest.NewRecorder(), httptest.NewRequest("POST", "/", bytes.NewReader(data)))
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		f, _ := mw.CreateFormFile("file", filepath.Base(name))
		f.Write([]byte("upload"))
		mw.Close()
		req := httptest.NewRequest("POST", "/?path="+filepath.ToSlash(filepath.Dir(name)), &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		s.wsUpload(httptest.NewRecorder(), req)
	}
	for _, name := range []string{"", ".", "..", "../archives", "archives", "traffic", "evidence", "artex.sqlite", "C:/outside", `..\artex.sqlite`} {
		s.wsDelete(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/?path="+name, nil))
	}
	for _, name := range protected {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != "internal" {
			t.Fatalf("internal %s changed: %q, %v", name, got, err)
		}
	}
	w := httptest.NewRecorder()
	s.wsList(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestWorkspaceLinksCannotEscape(t *testing.T) {
	dir := t.TempDir()
	s := &Server{m: &Manager{dir: dir}}
	root, err := s.wsRoot()
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	outside := filepath.Join(dir, "evidence")
	os.MkdirAll(outside, 0700)
	target := filepath.Join(outside, "body")
	os.WriteFile(target, []byte("internal"), 0600)
	// Hard links require no administrator rights on Windows.
	hard := filepath.Join(dir, "workspace", "hard")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.wsWrite(w, httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"path":"hard","content":"new"}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if b, _ := os.ReadFile(target); string(b) != "internal" {
		t.Fatal("hardlink target overwritten")
	}
	uploadLink := filepath.Join(dir, "workspace", "upload-hard")
	if err := os.Link(target, uploadLink); err != nil {
		t.Fatal(err)
	}
	var upload bytes.Buffer
	mw := multipart.NewWriter(&upload)
	f, err := mw.CreateFormFile("file", "upload-hard")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("uploaded"))
	mw.Close()
	req := httptest.NewRequest("POST", "/", &upload)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	s.wsUpload(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if b, _ := os.ReadFile(target); string(b) != "internal" {
		t.Fatal("hardlink upload overwrote target")
	}
	link := filepath.Join(dir, "workspace", "escape")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		// Directory junctions exercise the Windows boundary without symlink privilege.
		if output, linkErr := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); linkErr != nil {
			t.Fatalf("could not create junction: %v %s", linkErr, output)
		}
	}
	for _, handler := range []http.HandlerFunc{s.wsRead, s.wsDownload, s.wsDelete} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/?path=escape/body", nil))
		if w.Code == 200 {
			t.Fatalf("link escaped: %s", w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	s.wsWrite(w, httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"path":"escape/body","content":"bad"}`)))
	if w.Code == 200 {
		t.Fatal("link write escaped")
	}
	if b, _ := os.ReadFile(target); string(b) != "internal" {
		t.Fatal("target changed")
	}
	// Recursive removal of a link removes only the link, never its target.
	s.wsDelete(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/?path=escape", nil))
	if b, _ := os.ReadFile(target); string(b) != "internal" {
		t.Fatal("parent deletion followed link")
	}
}

func TestWorkspaceRejectsCustomBusinessDatabase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARTEX_DB_PATH", filepath.Join(dir, "workspace", "custom.sqlite"))
	m, err := NewManager(dir, "")
	if err == nil {
		m.Close()
		t.Fatal("business database accepted inside workspace")
	}
}
