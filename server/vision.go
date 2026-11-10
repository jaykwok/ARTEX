package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/llmpool"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

const visionProfileSetting = "vision_profile_id"

type visionScopedTool struct{ actool.CoreTool }

func (*visionScopedTool) AllowedAgent(agent string) bool {
	return agent == "worker" || agent == "retester" || agent == "mainagent"
}

func (s *Server) visionProfileID() int64 {
	v, _, _ := s.m.pg.GetSetting(visionProfileSetting)
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}
func (s *Server) getVision(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"profile_id": s.visionProfileID()})
}
func (s *Server) setVision(w http.ResponseWriter, r *http.Request) {
	var a struct {
		ProfileID int64 `json:"profile_id"`
	}
	if decode(r, &a) != nil || a.ProfileID < 0 {
		writeErr(w, 400, "invalid profile_id")
		return
	}
	if a.ProfileID > 0 {
		if p, e := s.m.pg.ProfileByID(a.ProfileID); e != nil || p == nil {
			writeErr(w, 400, "profile not found")
			return
		}
	}
	if err := s.m.pg.SetSetting(visionProfileSetting, strconv.FormatInt(a.ProfileID, 10)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.getVision(w, r)
}
func (s *Server) visionTool() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: "analyze_image", Description: "使用独立视觉模型读取当前运行目录中的 PNG/JPEG（<=5MiB）。只返回文字摘要；不上传到主模型历史。需在 LLM 配置中绑定视觉模型。",
		Schema: objSchema(map[string]any{"image_path": strParam("当前运行目录内的图片路径"), "question": strParam("需要识别的页面或图片信息")}, "image_path", "question"),
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.AskUser("send this image to the configured vision model?")
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			var a struct {
				Path     string `json:"image_path"`
				Question string `json:"question"`
			}
			if json.Unmarshal(in, &a) != nil || tc == nil || tc.WorkingDir == "" {
				return actool.Errorf("invalid image arguments or working directory"), nil
			}
			id := s.visionProfileID()
			if id <= 0 {
				return actool.Errorf("未绑定视觉模型，请在系统 → LLM 配置中选择"), nil
			}
			cfg, ok := s.loadProfileConfig(id)
			if !ok {
				return actool.Errorf("视觉模型配置不可用"), nil
			}
			path := a.Path
			if filepath.IsAbs(path) {
				var err error
				path, err = filepath.Rel(tc.WorkingDir, path)
				if err != nil {
					return actool.Errorf(err.Error()), nil
				}
			}
			if !filepath.IsLocal(path) || strings.Contains(path, ":") {
				return actool.Errorf("image must be inside this run directory"), nil
			}
			root, err := os.OpenRoot(tc.WorkingDir)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			defer root.Close()
			f, err := root.Open(path)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil || !st.Mode().IsRegular() || st.Size() > 5<<20 {
				return actool.Errorf("image must be a regular file <=5MiB"), nil
			}
			raw, err := io.ReadAll(io.LimitReader(f, (5<<20)+1))
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			jpegData, err := llmpool.PrepareImage(raw)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			started := time.Now()
			res, err := llmpool.AnalyzeImage(ctx, llmpool.VisionConfig{Format: string(cfg.Format), BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, Proxy: cfg.Proxy, MaxTokensField: cfg.MaxTokensField, SessionHeaderKey: cfg.SessionHeaderKey}, jpegData, a.Question)
			info := agent.RunInfoFrom(ctx)
			status := "ok"
			if err != nil {
				status = "error"
			}
			profileName := ""
			if p, e := s.m.pg.ProfileByID(id); e == nil && p != nil {
				profileName = p.Name
			}
			if e := s.m.pg.InsertLLMUsage(&db.LLMUsage{TaskID: strconv.FormatInt(info.TaskID, 10), ExplorationID: info.ExplorationID, Worker: "vision", Model: cfg.Model, ProfileName: profileName, LatencyMs: int(time.Since(started).Milliseconds()), InputTokens: res.InputTokens, OutputTokens: res.OutputTokens, Status: status}); e != nil {
				log.Printf("[vision] usage: %v", e)
			}
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text(res.Text), nil
		},
	})
}
