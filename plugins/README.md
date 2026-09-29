# 2Ag sidecar plugins

Place optional Windows sidecars in this directory and declare them in
`2ag.json`:

```json
{
  "plugins": [
    {
      "name": "eyes-control",
      "executable": "plugins/eyes-control.exe",
      "args": [],
      "enabled": false
    }
  ]
}
```

Enabled processes are started by `2ag.exe` and assigned to the same Windows
Job Object as Antigravity. They are terminated when the launcher closes.

## Manifest and Hub UI

Each plugin directory may contain a `manifest.json`:

```json
{
  "id": "example-sidecar",
  "name": "Example Sidecar",
  "version": "1.0.0",
  "sidecar": { "executable": "bin/example-sidecar.exe", "args": ["--port", "${2AG_PORT}"] },
  "ui": { "tabTitle": "Example", "entry": "ui/panel.js" }
}
```

The optional UI entry is fetched from the loopback control server and can
register a Hub tab:

```javascript
window.__2AG__.registerTab({
  id: 'example',
  title: 'Example',
  render(panel) { panel.textContent = 'Hello from 2Ag'; }
});
```

The shared IPC methods are `core.dialog.openFile`, `core.config.get`,
`core.config.set`, `core.plugins.list`, and `core.plugins.toggle`.
