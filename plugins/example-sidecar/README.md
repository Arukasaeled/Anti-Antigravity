# Example sidecar plugin

This directory documents the 2Ag community plugin shape. Put the Windows
sidecar at `bin/example-sidecar.exe` and optional Hub code at `ui/panel.js`.
The launcher supplies `2AG_CDP_PORT`, `2AG_APP_DIR`, and `2AG_PLUGIN_ID` to the
sidecar process, together with `2AG_PLUGIN_DIR`, `2AG_CONTROL_URL`,
`2AG_PLUGIN_UI_URL`, and `2AG_PORT`. Manifest argument placeholders such as
`${2AG_PORT}` are passed through for the sidecar to resolve from its
environment. The example UI registers a tab through
`window.__2AG__.registerTab` and requests core state through
`window.__2AG__.ipc`.
