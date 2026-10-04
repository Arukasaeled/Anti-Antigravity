package supervisor

import (
	"fmt"
	"path/filepath"

	"github.com/2ag/2ag/internal/activity"
)

type ActivityStep struct {
	StepIndex  int64            `json:"stepIndex"`
	Native     map[string]any   `json:"native"`
	TokenUsage *GenerationUsage `json:"tokenUsage"`
}

type ActivityHistory struct {
	SessionID  string         `json:"sessionId"`
	Source     SessionSource  `json:"source"`
	Steps      []ActivityStep `json:"steps"`
	TotalSteps int64          `json:"totalSteps"`
	NextAfter  int64          `json:"nextAfter"`
	Complete   bool           `json:"complete"`
	Partial    bool           `json:"partial"`
	Note       string         `json:"note,omitempty"`
}

// ReadAntigravityActivity uses the same discovered source as Preview/Usage.
// Paging bounds response size; callers keep loading until Complete, not 50 rows.
func ReadAntigravityActivity(id, store string, after int64) (ActivityHistory, error) {
	r := ActivityHistory{SessionID: id, Steps: []ActivityStep{}, NextAfter: after}
	if after < -1 {
		return r, fmt.Errorf("无效 Activity 游标")
	}
	source, err := resolveSessionSource(id, store)
	if err != nil {
		return r, err
	}
	r.Source = source
	if filepath.Ext(source.ConversationPath) != ".db" {
		return r, fmt.Errorf("当前 Activity 仅支持原生 conversation SQLite")
	}
	path := source.ConversationPath
	if err := readNativeUsageRows(path, "SELECT count(*), NULL FROM steps", func(n int64, _ []byte) error { r.TotalSteps = n; return nil }); err != nil {
		return r, err
	}
	const pageSize = 128
	query := fmt.Sprintf("SELECT idx, step_payload FROM steps WHERE idx > %d ORDER BY idx LIMIT %d", after, pageSize+1)
	seen := 0
	err = readNativeUsageRows(path, query, func(idx int64, data []byte) error {
		seen++
		if seen > pageSize {
			return nil
		}
		r.NextAfter = idx
		native, err := activity.Decode(data, "gemini_coder.Step")
		if err != nil {
			r.Partial = true
			r.Note = "部分原生步骤无法解析；历史未完整还原。"
			return nil
		}
		r.Steps = append(r.Steps, ActivityStep{StepIndex: idx, Native: native})
		if native["projectionPartial"] == true {
			r.Partial = true
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	r.Complete = seen <= pageSize
	byIndex := make(map[int64]int)
	for i, step := range r.Steps {
		byIndex[step.StepIndex] = i
	}
	// error_details can be stored separately from the full Step payload.
	if len(r.Steps) > 0 {
		query = fmt.Sprintf("SELECT idx, error_details FROM steps WHERE idx > %d AND idx <= %d AND length(error_details)>0 ORDER BY idx", after, r.NextAfter)
		if err := readNativeUsageRows(path, query, func(idx int64, data []byte) error {
			if i, ok := byIndex[idx]; ok {
				e, err := activity.Decode(data, "exa.cortex_pb.CortexErrorDetails")
				if err != nil {
					r.Partial = true
				} else {
					r.Steps[i].Native["error"] = e
					if e["projectionPartial"] == true {
						r.Steps[i].Native["projectionPartial"] = true
						r.Partial = true
					}
				}
			}
			return nil
		}); err != nil {
			r.Partial = true
			r.Note = "部分错误详情暂未读取。"
		}
	}
	// Consume existing request telemetry and normalization, not a second token reader.
	usage, usageErr := readGenerationUsage(path, id)
	byResponse := make(map[string]GenerationUsage)
	for _, g := range usage.generations {
		if g.ResponseID != "" {
			byResponse[g.ResponseID] = g
		}
	}
	for i := range r.Steps {
		metadata, _ := r.Steps[i].Native["metadata"].(map[string]any)
		modelUsage, _ := metadata["modelUsage"].(map[string]any)
		response, _ := modelUsage["responseId"].(string)
		if g, ok := byResponse[response]; ok {
			copy := g
			r.Steps[i].TokenUsage = &copy
		}
	}
	if usageErr != nil {
		r.Note = "步骤已读取；部分 Token telemetry 暂不可用。"
	}
	return r, nil
}
