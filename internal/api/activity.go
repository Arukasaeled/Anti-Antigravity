package api

import (
	"net/http"
	"strconv"

	"github.com/2ag/2ag/internal/supervisor"
)

func (s *Server) handleSessionActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	after := int64(-1)
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < -1 {
			http.Error(w, "无效 Activity 游标", http.StatusBadRequest)
			return
		}
	}
	history, err := supervisor.ReadAntigravityActivity(r.URL.Query().Get("id"), r.URL.Query().Get("store"), after)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	getJSON(w, r, history)
}
