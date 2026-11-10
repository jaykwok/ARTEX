package server

import (
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
	"net/http"
	"strconv"
)

func (s *Server) browserImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var a struct {
		TaskID       string                  `json:"task_id"`
		Exchange     traffic.BrowserExchange `json:"exchange"`
		CreateIntent bool                    `json:"create_intent"`
	}
	if err := decode(r, &a); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t, ok := s.m.Task(a.TaskID)
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "task is being deleted")
		return
	}
	defer s.engine.decInflight(t.ID)
	tr := s.m.Traffic()
	if tr == nil {
		writeErr(w, 409, "traffic store unavailable; start ARTEX with the recording proxy enabled")
		return
	}
	if a.CreateIntent && !s.m.TrafficEnabled() {
		writeErr(w, 409, "启用流量录制后才能指派探索，worker 需要 traffic_get 读取导入请求")
		return
	}
	id, err := tr.ImportBrowser(a.Exchange)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	assetID, err := s.m.Assets().UpsertEndpoint(db.UpsertEndpointReq{URL: a.Exchange.URL, Method: a.Exchange.Method, TaskID: taskID})
	if err != nil {
		writeErr(w, 500, "traffic saved as "+id+"; asset import failed: "+err.Error())
		return
	}
	hintID, intentID, err := t.Store.BrowserImport(id, a.Exchange.URL, assetID, a.CreateIntent)
	if err != nil {
		writeErr(w, 500, "traffic saved as "+id+"; graph import failed: "+err.Error())
		return
	}
	t.NotifyHint([]string{"浏览器导入：" + a.Exchange.Method + " " + a.Exchange.URL})
	if a.CreateIntent {
		s.reviveTask(t)
	}
	writeJSON(w, 200, map[string]any{"traffic_id": id, "asset_id": assetID, "hint_id": hintID, "intent_id": intentID})
}
