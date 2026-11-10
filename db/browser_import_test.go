package db

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestBrowserImportIsIdempotentAndDoesNotCountAsTested(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTask("fixture", "fixture", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := d.Exploration(task.ExplorationID)
	asset, err := d.Assets().UpsertEndpoint(UpsertEndpointReq{URL: "https://browser.example.test/api", Method: "GET", TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	hint, intent, err := s.BrowserImport("fixture-id", "https://browser.example.test/api", asset, true)
	if err != nil || hint == 0 || intent == 0 {
		t.Fatal(hint, intent, err)
	}
	h2, i2, err := s.BrowserImport("fixture-id", "https://browser.example.test/api", asset, true)
	if err != nil || h2 != hint || i2 != intent {
		t.Fatal("duplicate import", h2, i2, err)
	}
	var tested int
	if err = d.QueryRow(`SELECT count(*) FROM exploration_anchors a JOIN exploration_nodes n ON n.id=a.node_id WHERE a.asset_id=$1 AND n.kind='fact'`, asset).Scan(&tested); err != nil || tested != 0 {
		t.Fatal("import inflated coverage", tested, err)
	}
	n, _ := s.GetNode(intent)
	if n.Priority != 10 || n.State != "open" {
		t.Fatal(n)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, i, e := s.BrowserImport("concurrent-fixture", "https://browser.example.test/api", asset, true)
			if e != nil || h == 0 || i == 0 {
				t.Errorf("concurrent import: %d %d %v", h, i, e)
			}
		}()
	}
	wg.Wait()
	var copies int
	if err = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND origin='browser' AND json_extract(payload,'$.traffic_id')='concurrent-fixture'`, task.ExplorationID).Scan(&copies); err != nil || copies != 2 {
		t.Fatalf("concurrent import copies=%d err=%v", copies, err)
	}
}
