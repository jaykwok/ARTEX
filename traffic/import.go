package traffic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
)

type ImportHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type BrowserExchange struct {
	URL                 string         `json:"url"`
	Method              string         `json:"method"`
	Headers             []ImportHeader `json:"headers"`
	Body                string         `json:"body"`
	Status              int            `json:"status"`
	ResponseHeaders     []ImportHeader `json:"response_headers"`
	ResponseBody        string         `json:"response_body"`
	ResponseBodyOmitted bool           `json:"response_body_omitted,omitempty"`
}

var httpToken = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9a-zA-Z-]+$")

// ImportBrowser only records supplied bytes. It never sends the request.
func (t *Traffic) ImportBrowser(ex BrowserExchange) (string, error) {
	u, err := url.Parse(ex.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || len(ex.URL) > 8192 || strings.ContainsAny(ex.URL, "\r\n") {
		return "", fmt.Errorf("invalid HTTP URL")
	}
	if !httpToken.MatchString(ex.Method) || ex.Status < 0 || ex.Status > 599 || len(ex.Body) > 512<<10 || len(ex.ResponseBody) > 512<<10 {
		return "", fmt.Errorf("invalid method/status or body exceeds 512 KiB")
	}
	encodeHeaders := func(headers []ImportHeader) (string, error) {
		if len(headers) > 200 {
			return "", fmt.Errorf("too many headers")
		}
		var out strings.Builder
		for _, h := range headers {
			if strings.HasPrefix(h.Name, ":") {
				continue
			}
			if !httpToken.MatchString(h.Name) || strings.ContainsAny(h.Value, "\r\n\x00") {
				return "", fmt.Errorf("invalid header")
			}
			out.WriteString(h.Name + ": " + h.Value + "\r\n")
			if out.Len() > 64<<10 {
				return "", fmt.Errorf("headers exceed 64 KiB")
			}
		}
		return out.String(), nil
	}
	reqHead, err := encodeHeaders(ex.Headers)
	if err != nil {
		return "", err
	}
	respHead, err := encodeHeaders(ex.ResponseHeaders)
	if err != nil {
		return "", err
	}
	if !strings.Contains(strings.ToLower("\r\n"+reqHead), "\r\nhost:") {
		reqHead = "Host: " + u.Host + "\r\n" + reqHead
	}
	reqHead = ex.Method + " " + u.RequestURI() + " HTTP/1.1\r\n" + reqHead
	respHead = fmt.Sprintf("HTTP/1.1 %d\r\n%s", ex.Status, respHead)
	raw, _ := json.Marshal(ex)
	id := fmt.Sprintf("browser-%x", sha256.Sum256(raw))
	t.wmu.Lock()
	defer t.wmu.Unlock()
	tx, err := t.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	ct := ""
	for _, h := range ex.ResponseHeaders {
		if strings.EqualFold(h.Name, "Content-Type") {
			ct = h.Value
		}
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO exchanges(id,ts,host,method,url_template,url,status,content_type,req_len,resp_len,path) VALUES(?,?,?,?,?,?,?,?,?,?,'')`, id, time.Now().Unix(), u.Hostname(), ex.Method, db.TemplatePath(u.EscapedPath()), ex.URL, ex.Status, ct, len(ex.Body), len(ex.ResponseBody))
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return id, nil
	}
	rowid, _ := res.LastInsertId()
	if _, err = tx.Exec(`INSERT INTO exchange_bodies(id,req_head,req_body,resp_head,resp_body) VALUES(?,?,?,?,?)`, id, reqHead, []byte(ex.Body), respHead, []byte(ex.ResponseBody)); err != nil {
		return "", err
	}
	if t.fts {
		if _, err = tx.Exec(`INSERT INTO ex_fts(rowid,content) VALUES(?,?)`, rowid, strings.Join([]string{ex.URL, reqHead, ex.Body, respHead, ex.ResponseBody}, "\n")); err != nil {
			return "", err
		}
	}
	return id, tx.Commit()
}
