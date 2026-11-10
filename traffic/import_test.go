package traffic

import (
	"strings"
	"testing"
)

func TestBrowserImportNormalizesAndDeduplicates(t *testing.T) {
	tr, _ := openTraffic(t)
	ex := BrowserExchange{URL: "https://example.test/a?q=x", Method: "POST", Headers: []ImportHeader{{Name: ":authority", Value: "example.test"}, {Name: "Content-Type", Value: "text/plain"}}, Body: "fixture request", Status: 200, ResponseBody: "fixture response"}
	id, err := tr.ImportBrowser(ex)
	if err != nil {
		t.Fatal(err)
	}
	other, err := tr.ImportBrowser(ex)
	if err != nil || other != id {
		t.Fatal(other, err)
	}
	req, resp, err := tr.Get(id)
	if err != nil || !strings.Contains(req, "POST /a?q=x HTTP/1.1") || !strings.Contains(req, "Host: example.test") || strings.Contains(req, ":authority") || !strings.Contains(resp, "fixture response") {
		t.Fatal(req, resp, err)
	}
	ex.Headers = []ImportHeader{{Name: "X-Test", Value: "x\r\ninjected: true"}}
	if _, err := tr.ImportBrowser(ex); err == nil {
		t.Fatal("accepted header injection")
	}
	ex.Headers = nil
	ex.URL = "file:///tmp/fixture"
	if _, err := tr.ImportBrowser(ex); err == nil {
		t.Fatal("accepted non-HTTP URL")
	}
}
