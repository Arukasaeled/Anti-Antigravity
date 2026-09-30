package supervisor

import (
	"testing"
)

func TestIsRealWorkbenchTarget(t *testing.T) {
	testCases := []struct {
		name       string
		target     cdpTarget
		wantOk     bool
		minScore   int
	}{
		{
			name: "Transition splash screen data URI",
			target: cdpTarget{
				ID:                   "splash-1",
				Title:                "Loading Antigravity",
				Type:                 "page",
				URL:                  "data:text/html;charset=utf-8,<html>Loading Antigravity...</html>",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/splash-1",
			},
			wantOk: false,
		},
		{
			name: "Transition splash screen with loading in title",
			target: cdpTarget{
				ID:                   "splash-2",
				Title:                "Loading Antigravity...",
				Type:                 "page",
				URL:                  "file:///tmp/loading.html",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/splash-2",
			},
			wantOk: false,
		},
		{
			name: "DevTools window",
			target: cdpTarget{
				ID:                   "devtools-1",
				Title:                "Developer Tools",
				Type:                 "page",
				URL:                  "devtools://devtools/bundled/inspector.html",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/devtools-1",
			},
			wantOk: false,
		},
		{
			name: "Blank page",
			target: cdpTarget{
				ID:                   "blank-1",
				Title:                "",
				Type:                 "page",
				URL:                  "about:blank",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/blank-1",
			},
			wantOk: false,
		},
		{
			name: "Real Antigravity workbench with empty Title (Core Electron Bug Case)",
			target: cdpTarget{
				ID:                   "main-workbench-empty-title",
				Title:                "",
				Type:                 "page",
				URL:                  "https://127.0.0.1:52202/",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/main-workbench-empty-title",
			},
			wantOk:   true,
			minScore: 90,
		},
		{
			name: "Real Antigravity conversation window",
			target: cdpTarget{
				ID:                   "81C7F9456C84A3CDED39C35BEA98C150",
				Title:                "Numeric Test Input - Anti-antigravity - Antigravity",
				Type:                 "page",
				URL:                  "https://127.0.0.1:52202/c/0be6f4da-646f-4651-8554-0108e59281a4?section=681bec65-2871-4f98-8e85-2e6c32754fa0",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/81C7F9456C84A3CDED39C35BEA98C150",
			},
			wantOk:   true,
			minScore: 120,
		},
		{
			name: "Real VSCode/Electron workbench target",
			target: cdpTarget{
				ID:                   "workbench-main",
				Title:                "Antigravity",
				Type:                 "page",
				URL:                  "vscode-file://vscode-app/out/vs/code/electron-sandbox/workbench/workbench.html",
				WebSocketDebuggerURL: "ws://127.0.0.1:28472/devtools/page/workbench-main",
			},
			wantOk:   true,
			minScore: 110,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotOk := IsRealWorkbenchTarget(tc.target)
			if gotOk != tc.wantOk {
				t.Errorf("IsRealWorkbenchTarget() gotOk = %v, want %v", gotOk, tc.wantOk)
			}
			score := targetScore(tc.target)
			if tc.wantOk && score < tc.minScore {
				t.Errorf("targetScore() score = %d, want >= %d", score, tc.minScore)
			}
		})
	}
}
