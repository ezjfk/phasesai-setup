# PhasesAI Windows setup

Native Windows installers for **Claude Code, OpenAI Codex, and OpenCode**. Download an EXE, open it, and paste your PhasesAI API key at the hidden prompt. No Node.js, npm, Git, GitHub CLI, or administrator privileges are required. Internet access is required.

Public source: [ezjfk/phasesai-setup](https://github.com/ezjfk/phasesai-setup). MIT licensed. This standalone repository contains only the installer; never publish the parent gateway project.

## Download and use

Download from [the Windows release](https://github.com/ezjfk/phasesai-setup/releases/tag/v0.2.0-preview.1). Choose `amd64` for an Intel/AMD PC or `arm64` for Windows on ARM.

| EXE | Interaction |
| --- | --- |
| `phasesai-claude-setup-windows-amd64.exe` | Paste your API key; installs Claude Code |
| `phasesai-codex-setup-windows-amd64.exe` | Paste your API key; installs Codex |
| `phasesai-opencode-setup-windows-amd64.exe` | Paste your API key; installs OpenCode |
| `phasesai-setup-windows-amd64.exe` | Choose an app or all three, then paste one key |

Every EXE also has an `arm64` variant. These are console applications with colored stages, download progress, and clear completion/error messages. Double-clicked windows stay open until you press Enter.

Setup resolves each vendor's **latest stable release at runtime**, downloads its native Windows binary/bundle, verifies the vendor SHA-256 checksum, and checks its executable version. Codex's complete runtime bundle, including helper executables and DLLs, stays together. OpenCode uses the x64 baseline build for wider CPU compatibility. Claude Code can use Windows PowerShell without Git Bash.

Setup merges your global config, privately backs up changed files, adds the installed app to your **User PATH**, and creates a shortcut under **Start → PhasesAI**. Open the shortcut, or open a new terminal and run `claude`, `codex`, or `opencode`. No API-key environment variable or special launcher is needed.

Rerun setup to download the latest releases. An existing PhasesAI key is reused; `--replace-key` prompts for a new one. Unrelated providers, settings, comments, model entries, and installed plugins are preserved. Optional Claude plugins are not automatically installed or enabled.

## Persistent configuration

| App | Default files | Default model |
| --- | --- | --- |
| Claude Code | `%USERPROFILE%\.claude\settings.json` | `claude-opus-5-5` |
| Codex | `%USERPROFILE%\.codex\config.toml` and `auth.json` | `gpt-6.1-sol` |
| OpenCode | `%USERPROFILE%\.config\opencode\opencode.jsonc` | `phasesai-anthropic/claude-opus-5-5` |

Existing OpenCode `opencode.json` is reused when there is no JSONC file. Standard config-root overrides are honored when the target is inside your home. App-specific project configs and enterprise policy can override global settings.

Claude's settings contain `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, an empty `ANTHROPIC_API_KEY`, the selected model, and the supplied default Opus/Sonnet/Haiku model aliases. Existing theme/plugin choices remain; a fresh config defaults to `dark-daltonized`.

OpenCode gets separate `phasesai-anthropic` (`@ai-sdk/anthropic`) and `phasesai-gpt` (`@ai-sdk/openai-compatible`) providers, each with a saved key and `https://api.phasesai.cc/v1`. Its supplied Claude/GPT model definitions and limits are included, alongside the default Claude/Agents skill paths. OpenCode handles provider package downloads using its bundled runtime.

Codex uses the Responses API provider at `https://api.phasesai.cc/v1`, `requires_openai_auth = true`, `forced_login_method = "api"`, and `cli_auth_credentials_store = "file"`. Setup passes your key over stdin to the downloaded `codex login --with-api-key` in a disposable config directory, then installs the generated `auth.json` alongside your merged `config.toml`. The key never enters command arguments.

Keys are stored as plaintext in these native app files, protected by a Windows DACL granting your account and SYSTEM access. Backups may contain previous credentials and are also private. Setup does not print keys or config contents. Binaries live under `%LOCALAPPDATA%\PhasesAI\harnesses`; previous installations are retained when updating.

Normal setup sends **no billable inference request**. Version/auth/config checks do not establish model access, balance, tool execution, or API compatibility. `--smoke-test` separately asks before a potentially billable 32-output-token check.

## CLI options

```powershell
.\phasesai-setup-windows-amd64.exe --harness all
.\phasesai-setup-windows-amd64.exe --harness codex --dry-run
.\phasesai-setup-windows-amd64.exe --harness claude --replace-key
.\phasesai-setup-windows-amd64.exe --harness codex --model gpt-6-sol
.\phasesai-setup-windows-amd64.exe --launch codex -- --help
```

`--dry-run` resolves releases and previews paths without a key, software download, config write, log, shortcut, or PATH mutation. `--config` selects a single app's config inside your home. `--log-file` enables a private, rotating JSONL log of fixed events; paths, credentials, and HTTP bodies are excluded. The legacy `--install-missing` and `--save-key` flags remain accepted; installation and persistence are now the default.

Configuration writes use exclusive locks, private backups/staging, journals, compare-before-replace, and verification. A later file failure rolls earlier writes back without overwriting concurrent edits. Interrupted individual file transactions can be recovered with:

```powershell
.\phasesai-setup-windows-amd64.exe --recover --config C:\Users\YOU\.codex\config.toml
```

Recover every config/auth file reporting an interrupted transaction before rerunning setup. Backups remain adjacent as `.phasesai-<random>.bak`. Downloads, PATH registration, and shortcuts are outside config rollback; rerun setup to repair those. Linked/reparse paths, other-owner files, hardlinks, malformed/duplicate config, and configs above 2 MiB are refused.

## Build, test, publish

Maintainers need Go **1.27.3+**; end users only need the EXE.

```powershell
go mod verify
go test ./...
go vet ./...
go build -trimpath -o phasesai-setup-windows-amd64.exe .
$env:PHASESAI_WINDOWS_ACCEPTANCE = '1'
go test -run '^TestNativeWindowsCleanProfile$' -v -timeout=20m .
```

The opt-in acceptance test uses actual latest downloads and dummy credentials in an isolated profile with Node/npm/Git removed from PATH. It checks native versions, bundle companions, persisted Codex login, and OpenCode's effective config. It sends no model inference and does not change the real user's PATH or Start menu. A fresh Windows VM and paid end-to-end harness/tool acceptance remain separate checks.

GitHub CI tests Windows, builds only x64/ARM64 EXEs (universal chooser plus three app presets), emits source/SBOM/licenses/checksums, attests the exact artifacts, and publishes preview tags. Setup EXEs are not Authenticode signed. Inspect the source and release metadata; maintainers can verify provenance with:

```sh
gh attestation verify FILE --repo ezjfk/phasesai-setup --signer-workflow ezjfk/phasesai-setup/.github/workflows/preview.yml --source-ref refs/tags/EXACT_TAG
```

The optional `scripts/install.ps1` bootstrap uses only built-in Windows PowerShell and verifies release checksums. Prefer direct EXE downloads for the simplest experience. Historical macOS/Linux releases are archival; new builds are Windows only. The R2 download host is managed separately and is not promoted by a GitHub source push.

See [implementation research and primary sources](docs/windows-harness-research.md).
