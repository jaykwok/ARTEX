package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// BrowserImport records provenance as a hint, not a tested fact or confirmed finding.
// Optional intent creation and its lineage are committed together and idempotent.
func (s *ExplorationStore) BrowserImport(trafficID, url string, assetID int64, createIntent bool) (hintID, intentID int64, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	// Acquire SQLite's writer reservation before reading the idempotency key.
	// Two concurrent imports must not both observe an absent node.
	var locked sql.Result
	locked, err = tx.Exec(`UPDATE explorations SET round_no=round_no WHERE id=$1`, s.expID)
	if err != nil {
		return
	}
	if n, _ := locked.RowsAffected(); n != 1 {
		err = fmt.Errorf("exploration not found")
		return
	}
	err = tx.QueryRow(`SELECT id FROM exploration_nodes WHERE exploration_id=$1 AND kind='hint' AND origin='browser' AND json_extract(payload,'$.traffic_id')=$2`, s.expID, trafficID).Scan(&hintID)
	if err != nil && err != sql.ErrNoRows {
		return
	}
	if hintID == 0 {
		p, _ := json.Marshal(map[string]any{"text": "浏览器导入请求（未由 ARTEX 复测，身份头或响应正文可能省略，不能视为完整抓包）：" + url, "traffic_id": trafficID, "traffic_refs": []TrafficRef{{TrafficID: trafficID, Role: "supporting"}}})
		err = tx.QueryRow(`INSERT INTO exploration_nodes(exploration_id,kind,payload,priority,state,origin) VALUES($1,'hint',$2,0,'active','browser') RETURNING id`, s.expID, string(p)).Scan(&hintID)
		if err != nil {
			return
		}
		if _, err = tx.Exec(`INSERT INTO exploration_anchors(node_id,asset_id) VALUES($1,$2)`, hintID, assetID); err != nil {
			return
		}
	}
	if createIntent {
		err = tx.QueryRow(`SELECT id FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent' AND origin='browser' AND json_extract(payload,'$.traffic_id')=$2`, s.expID, trafficID).Scan(&intentID)
		if err != nil && err != sql.ErrNoRows {
			return
		}
		if intentID == 0 {
			p, _ := json.Marshal(map[string]any{"summary": fmt.Sprintf("检查浏览器导入的接口 %s。先用 traffic_get 读取真实请求/响应（traffic_id=%s），遵守任务约束。导入内容仅为线索，不是已验证漏洞。", url, trafficID), "traffic_id": trafficID, "asset_ids": []int64{assetID}})
			err = tx.QueryRow(`INSERT INTO exploration_nodes(exploration_id,kind,payload,priority,state,origin) VALUES($1,'intent',$2,10,'open','browser') RETURNING id`, s.expID, string(p)).Scan(&intentID)
			if err != nil {
				return
			}
			if _, err = tx.Exec(`INSERT INTO exploration_anchors(node_id,asset_id) VALUES($1,$2)`, intentID, assetID); err != nil {
				return
			}
			// The root fact owns new directions; the imported hint is retained separately.
			if _, err = tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) SELECT $1,id,'derived_from',$2 FROM exploration_nodes WHERE exploration_id=$1 AND kind='fact' AND state='origin' LIMIT 1`, s.expID, intentID); err != nil {
				return
			}
		}
	}
	err = tx.Commit()
	return
}
