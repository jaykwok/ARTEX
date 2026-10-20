package server

import (
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// GetSetting 对"键不存在"和"读取出错"的返回值只差一个 error：两种情况 value 都是
// 空串。密码相关的 handler 一旦把 error 当成"还没设置密码"，就会在数据库抖动期间
// 敞开初始化入口——authInit 会放行一个未认证请求去覆盖已有的管理员密码，
// authStatus 则会把前端直接送到 /setup 去照着做这件事。
//
// 这两个测试把 handler 的连接池关掉来制造读取失败，断言两处都 fail closed。
func TestAuthStatusFailsClosedWhenDataSourceUnavailable(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("SQLite unavailable (%v)", err)
	}
	t.Cleanup(func() { m.Close() })
	// 关掉池子，让后续 GetSetting 返回 error 而不是 sql.ErrNoRows。
	if err := m.pg.Close(); err != nil {
		t.Fatal(err)
	}

	s := &Server{m: m}
	w := httptest.NewRecorder()
	s.authStatus(w, httptest.NewRequest("GET", "/api/auth/status", nil))

	if w.Code != 503 {
		t.Fatalf("status=%d want 503 (读失败被当成未初始化会把用户送去 /setup 覆盖密码); body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Initialized *bool `json:"initialized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err == nil && payload.Initialized != nil {
		t.Fatalf("读失败时不应回答 initialized，得到 %v", *payload.Initialized)
	}
}

func TestAuthInitFailsClosedWhenDataSourceUnavailable(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("SQLite unavailable (%v)", err)
	}
	t.Cleanup(func() { m.Close() })
	if err := m.pg.Close(); err != nil {
		t.Fatal(err)
	}

	s := &Server{m: m}
	w := httptest.NewRecorder()
	body := strings.NewReader(`{"password":"correct horse battery"}`)
	s.authInit(w, httptest.NewRequest("POST", "/api/auth/init", body))

	if w.Code != 503 {
		t.Fatalf("status=%d want 503 (读失败时放行会让未认证请求覆盖已有密码); body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "token") {
		t.Fatalf("读失败时不应签发 token: %s", w.Body.String())
	}
}

func TestValidatePassword(t *testing.T) {
	for _, tc := range []struct {
		name, pw string
		wantErr  bool
	}{
		{"空", "", true},
		{"七位", "1234567", true},
		{"八位", "12345678", false},
		{"八个汉字按字符数而非字节数计", "密码密码密码密码", false},
		{"三个汉字够 9 字节但只有 3 个字符", "密码强", true},
		{"72 字节", strings.Repeat("a", 72), false},
		{"73 字节超出 bcrypt 上限", strings.Repeat("a", 73), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validatePassword(tc.pw); (got != "") != tc.wantErr {
				t.Fatalf("validatePassword(%q)=%q, wantErr=%v", tc.pw, got, tc.wantErr)
			}
		})
	}
}

func signTestJWT(t *testing.T, s *Server) (string, error) {
	t.Helper()
	hash, _, err := s.m.pg.GetSetting(authPassKey)
	if err != nil {
		return "", err
	}
	if hash == "" {
		hash = "test-session-generation"
		if err := s.m.pg.SetSetting(authPassKey, hash); err != nil {
			return "", err
		}
	}
	return signJWT(s.sessionKey(hash))
}

func TestPasswordChangeRevokesCopiedSessions(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	s := &Server{m: m, jwtKey: []byte("test-key-32-bytes-long-for-session")}
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.pg.SetSetting(authPassKey, string(hash)); err != nil {
		t.Fatal(err)
	}
	old, err := signJWT(s.sessionKey(string(hash)))
	if err != nil {
		t.Fatal(err)
	}
	protected := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	check := func(token string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/tasks", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("status=%d want=%d: %s", w.Code, want, w.Body.String())
		}
	}
	check(old, 204)
	req := httptest.NewRequest("POST", "/api/auth/change-password", strings.NewReader(`{"old_password":"old-password","new_password":"new-password"}`))
	req.Header.Set("Authorization", "Bearer "+old)
	w := httptest.NewRecorder()
	s.authChangePassword(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	check(old, 401)
	current, _, err := m.pg.GetSetting(authPassKey)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := signJWT(s.sessionKey(current))
	if err != nil {
		t.Fatal(err)
	}
	check(fresh, 204)
	// An out-of-process administrator reset uses the same persisted password hash.
	reset, err := bcrypt.GenerateFromPassword([]byte("reset-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.pg.SetSetting(authPassKey, string(reset)); err != nil {
		t.Fatal(err)
	}
	check(fresh, 401)
}

func TestCORSAuthorizationAndPatchPreflight(t *testing.T) {
	h := cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("preflight reached API") }))
	r := httptest.NewRequest("OPTIONS", "/api/tasks/1", nil)
	r.Header.Set("Origin", "https://frontend.example")
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	r.Header.Set("Access-Control-Request-Headers", "Authorization,Content-Type")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("bad preflight: %d %v", w.Code, w.Header())
	}
}
