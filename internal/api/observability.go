package api

import (
	"encoding/json"
	"net/http"

	"github.com/2ag/2ag/internal/patcher"
	"github.com/2ag/2ag/internal/supervisor"
)

func getJSON(w http.ResponseWriter, r *http.Request, value any) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	supervisor.ObserveContextReadOnly(r.Context())
	targets, counters := supervisor.RuntimeManager.Snapshot()
	getJSON(w, r, map[string]any{"targets": targets, "counters": counters})
}

func (s *Server) handleRuntime(w http.ResponseWriter, r *http.Request) {
	targets, counters := supervisor.RuntimeManager.Snapshot()
	getJSON(w, r, map[string]any{"targets": targets, "counters": counters})
}

func (s *Server) handleContextView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(patcher.ContextViewScript()))
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Query().Get("export") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="2ag-diagnostic-report.json"`)
	}
	getJSON(w, r, supervisor.ReadDoctorReport())
}

func (s *Server) handleEnvironment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	getJSON(w, r, supervisor.DetectAntigravityEnvironment())
}

func (s *Server) handleQuotaHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	accounts := supervisor.ScanLocalAccounts()
	getJSON(w, r, map[string]any{"trends": supervisor.ReadQuotaTrends(accounts), "accounts": accounts})
}

func (s *Server) handleSessionPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	preview, err := supervisor.PreviewAISession(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	getJSON(w, r, preview)
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	getJSON(w, r, map[string]any{"skills": supervisor.ScanInstalledSkills(), "management": "read-only", "note": "原生 Enable / Disable 机制尚未确认；不改写 Skill 目录"})
}

func (s *Server) handleSkillRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	content, err := supervisor.ReadInstalledSkill(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	getJSON(w, r, map[string]any{"content": content})
}
