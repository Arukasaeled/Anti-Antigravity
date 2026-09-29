# Anti-Antigravity (2Ag)

2Ag is a Windows launcher that keeps Antigravity's `app.asar` intact and
adds runtime features through CDP and local loopback services.

`run` starts a per-process CDP port, an optional local HTTP forward proxy, the
configured sidecars, and the Antigravity host under one Windows Job Object.
The injected Hub provides Dream Skin controls, proxy status, global rules and
sidecar switches.

Plugins are discovered from `plugins/*/manifest.json`. A manifest can declare
one sidecar executable and one UI entry. Sidecars receive the CDP port, app
directory, plugin directory and loopback control URL, and are assigned to the
same Job Object as the host. UI entries can register modular Hub tabs through
`window.__2AG__.registerTab`; the core IPC methods are documented in
`plugins/README.md`.

The proxy blocks configured telemetry hosts and supports endpoint overrides,
custom headers and global rules for explicitly configured plain HTTP JSON
requests. HTTPS `CONNECT` is forwarded as an opaque tunnel. 2Ag does not
install a CA certificate or decrypt TLS, so HTTPS request-body rewriting is
not enabled.

Build a console binary for local diagnostics:

```powershell
go build -o 2ag.exe .\cmd\2ag
```

Build a packaged GUI binary and staging directory:

```powershell
.\scripts\pack.ps1 -Version 0.1.0
```
