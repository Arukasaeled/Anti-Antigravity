# Interaction Layer checkpoint audit

Baseline: `100f53f`. The working tree contained 22 tracked modifications and 24 untracked files. Account switching, relay, embedded UI and Trace registration share dependencies; the checkpoint keeps them together in one runnable commit.

| Class | Files / scope | Treatment |
| --- | --- | --- |
| A · Source | cmd/2ag/accounts.go; internal/api/account_relay.go, account_switch.go; internal/patcher/live_trace.js; supervisor/auth_callback_windows.go, credential_checkpoint_windows.go, native_auth.go, native_auth_windows.go, native_rpc_windows.go, relay_context.go; tracked source/UI changes | Commit |
| B · Documentation | ACCOUNT-LIFECYCLE-LIVETRACE.md, ACCOUNT-RELAY.md, EVOLUTION-CODEXPP.md, V012-READINESS.md; README, release notes, plugin documentation | Commit; historical reports retain their dates and limits |
| C · Tests | account_relay_test.go, live_trace_test.cjs, credential_diagnostic_test.go, credential_lifecycle_test.go; tracked tests; verify-manager-paint.ps1, verify-v012.ps1 | Commit; live credential diagnostics require explicit opt-in, values are not logged |
| D · Raw research | Root Antigravity LiveTrace investigation report; local_recovery_probe_test.go | Preserve locally, ignore; portable findings are documented separately |
| E · Build outputs | dist/, executables, installer staging | Ignore |
| F · Task launchers | _codex task markdown and _run-codex scripts | Preserve locally, ignore |
| G · Private runtime | Local configuration, logs, vault, credential snapshots, profiles, caches | Never stage; existing runtime data remains outside source scope |

The ignore file now covers local research/task inputs and performance profile outputs. No broad staging of the working directory is used. Checkpoint staging is an explicit source/document/test list. Release packaging separately checks owned source and staged resources for personal paths, personal emails and credential material.

No account eligibility, OAuth, low-quota routing or other unverified runtime result is promoted to a success by this checkpoint.
