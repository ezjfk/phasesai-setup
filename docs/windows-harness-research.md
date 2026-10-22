# Windows harness setup research

Checked 2026-10-22. Sources below are first-party documentation, release assets, or release source. Version numbers and hashes are observations, not installer pins.

## Implementation decisions

- Download the native Windows runtime for each tool. The setup EXE itself and these native runtimes avoid a preinstalled Node.js/npm requirement.
- Write the normal per-user config files and add the command directory to User PATH once. Users should not need to set an API-key environment variable in each terminal.
- Resolve the latest release at setup time, then download a version-specific artifact and verify its SHA256 before executing it. Retain every runtime companion file from an upstream bundle.
- Merge existing config and preserve unrelated entries. Back up changed files without logging credentials.

These are implementation recommendations derived from the sources, rather than upstream promises about this installer.

## OpenCode

The official documentation permits downloading the CLI binary directly from the official GitHub releases. Its global configuration is `~/.config/opencode/opencode.json`; JSONC is also supported. Config layers merge, with later conflicting values overriding earlier ones. A normal global config therefore provides persistence without `OPENCODE_CONFIG`. [Installation](https://opencode.ai/docs/), [configuration](https://opencode.ai/docs/config)

Latest stable observed: `v1.19.4`. The immutable release publishes these CLI assets, distinct from desktop installers:

| Architecture | Asset | Observed SHA256 |
| --- | --- | --- |
| x64 | `opencode-windows-x64.zip` | `93b899277bd6f68803009e32bf71772ce43c34b18adf1e93b9b4d8780fdf62e5` |
| x64 baseline | `opencode-windows-x64-baseline.zip` | `21cac96abb146b0f6e770680ea7855fc4f354c7966aab39f5be9859b0df1deac` |
| ARM64 | `opencode-windows-arm64.zip` | `36e696fd1d275b469ab02a1ccc00d161bfdcc0a4ffa93c2b7a4e57399f3ff347` |

Source: [official v1.19.4 assets](https://github.com/anomalyco/opencode/releases/expanded_assets/v1.19.4). The [release](https://github.com/anomalyco/opencode/releases/tag/v1.19.4) says its bundled Bun runtime is now 1.4.4. The [tagged build script](https://raw.githubusercontent.com/anomalyco/opencode/v1.19.4/packages/opencode/script/build.ts) creates Windows ZIPs from `dist/<platform>/bin/*`; the executable is at the archive root. Extract all files safely instead of assuming the executable will always be the sole file.

Runtime metadata endpoint: `https://api.github.com/repos/anomalyco/opencode/releases/latest`. GitHub release asset records use `name`, `browser_download_url`, and `digest` (`sha256:<hex>` when supplied). Exact current JSON could not be retrieved from this environment; verify this metadata path in the installer's live check. The official release page and expanded assets above were successfully read.

The live [configuration schema](https://opencode.ai/config.json) accepts the user's `skills: ["~/.claude/skills", "~/.agents/skills"]` form, and accepts the older `skills: {"paths": [...]}` form as deprecated compatibility. Stable v1.19.4 internally lowers V2 config before validating its V1 shape, so the paths-object form is the conservative compatibility choice until a downloaded CLI smoke test establishes the array shape. [Tagged config loader](https://raw.githubusercontent.com/anomalyco/opencode/v1.19.4/packages/opencode/src/config/config.ts), [V1 skills type](https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/core/src/v1/config/skills.ts)

The same tagged loader reads global `config.json`, then `opencode.json`, then `opencode.jsonc`. Updating an existing `opencode.jsonc` avoids leaving a higher-precedence old credential in place. The supplied `provider` entries remain the stable V1 provider format; the [custom provider docs](https://opencode.ai/docs/providers) document `npm`, `options.baseURL`, and model definitions.

OpenCode 1 and the V2 preview both use `opencode`; avoid silently choosing a preview release. The current [intro docs](https://opencode.ai/docs/) distinguish the two.

## Claude Code

Native Windows requires Windows 10 1809+/Server 2019+, x64/ARM64, and internet access. Git for Windows is **optional**: without it Claude uses its PowerShell shell tool. A fresh Windows setup consequently does not need a separate Git/Bash installer. Native installs require no administrator and update in the background. These statements are current docs, and supersede older guidance that Git Bash is mandatory. [Advanced setup](https://code.claude.com/docs/en/setup)

The official release CDN base is `https://downloads.claude.ai/claude-code-releases`. The version channel is `/latest`; version manifests are `/<version>/manifest.json`. The documented checksum is `platforms.<platform>.checksum`. Supported Windows platform identifiers are `win32-x64` and `win32-arm64`. A direct binary follows `/<version>/<platform>/claude.exe`. The manifest structure relevant to a downloader is therefore:

```json
{
  "platforms": {
    "win32-x64": { "checksum": "<64 hexadecimal SHA256 characters>" },
    "win32-arm64": { "checksum": "<64 hexadecimal SHA256 characters>" }
  }
}
```

This is a structural example, not a live manifest response. The docs establish the CDN, checksum field, and supported platforms; the direct binary path is additionally reported in a first-party issue. [Setup integrity verification](https://code.claude.com/docs/en/setup#binary-integrity-and-code-signing), [installation troubleshooting](https://code.claude.com/docs/en/troubleshoot-install), [upstream issue showing the Windows binary path](https://github.com/anthropics/claude-code/issues/79337?timeline_page=1)

Releases from 2.1.89 onward also provide `manifest.json.sig`. The documented signing-key fingerprint is `31DD DE24 DDFA B679 F42D 7BD2 BAA9 29FF 1A7E CACE`; Windows binaries carry the publisher signature `Anthropic, PBC`. Native Windows command location is `%USERPROFILE%\.local\bin\claude.exe`. [Binary integrity and code signing](https://code.claude.com/docs/en/setup#binary-integrity-and-code-signing), [PATH troubleshooting](https://code.claude.com/docs/en/troubleshoot-install#verify-your-path)

Persistent user settings belong in `%USERPROFILE%\.claude\settings.json`. The `env` object is a normal settings key, so saving `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` there implements the user's requested persistence. Strict JSON is required. Merge rather than replace permissions, plugins, and unrelated preferences. [Settings files and precedence](https://code.claude.com/docs/en/settings)

Implementation verification subsequently fetched the live channel and manifest: Claude `2.1.308`, with `win32-x64` and `win32-arm64` entries containing `binary`, `checksum`, and `size`. The compiled setup's dry run resolved this channel successfully. The version and checksums are read at runtime, never pinned in the installer.

## Codex

The filename is **`config.toml`**, not `settings.toml`, under `%USERPROFILE%\.codex`. A custom provider needs a selected `model_provider`, its `[model_providers.<id>]` table, the gateway's `/v1` base URL, and `wire_api = "responses"`. Root TOML keys must precede the first table. [Gateway configuration](https://learn.chatgpt.com/docs/enterprise/connect-to-a-gateway)

For cached API-key authentication through an OpenAI-compatible proxy, official docs explicitly support `requires_openai_auth = true`. That selects cached OpenAI authentication and ignores `env_key`; false without another authentication mechanism means no authentication. `cli_auth_credentials_store = "file"` stores credentials in `auth.json`; the keyring and automatic modes are separate alternatives. [Authentication](https://learn.chatgpt.com/docs/auth)

Suggested persistent config, with the user-requested model preserved:

```toml
model = "gpt-6.1-sol"
model_provider = "phasesai"
cli_auth_credentials_store = "file"

[model_providers.phasesai]
name = "PhasesAI"
base_url = "https://api.phasesai.cc/v1"
wire_api = "responses"
requires_openai_auth = true
```

The documented key-login interface is `codex login --with-api-key` with the key on **stdin**. Running this after writing the config lets the downloaded version create its own correct `auth.json` without an exported environment variable. The allowed official documentation does not publish the complete `auth.json` JSON schema; `auth_mode = "apikey"` / `OPENAI_API_KEY` should not be treated as independently verified from these docs. A setup that writes this JSON directly should verify it using that downloaded version's `codex login status`. [API key login](https://learn.chatgpt.com/docs/auth#sign-in-with-an-api-key)

OpenAI's Windows standalone installer defaults to latest and installs into the current user's profile, with command location `%LOCALAPPDATA%\Programs\OpenAI\Codex\bin` and User PATH. Critically, official deployment guidance says to keep **all standalone runtime files together**: copying only `codex.exe` is incomplete. [Windows CLI deployment](https://learn.chatgpt.com/docs/enterprise/windows-deployment#deploy-codex-cli-on-windows)

The same documentation points to `https://chatgpt.com/codex/install.ps1` and a release selector. Implementation verification downloaded and checksum-verified the official `0.163.0` Windows x64 runtime asset, `codex-package-x86_64-pc-windows-msvc.tar.gz`; ARM64 is `codex-package-aarch64-pc-windows-msvc.tar.gz`. The inspected bundle contains `bin/codex.exe`, `bin/codex-code-mode-host.exe`, `codex-package.json`, `codex-path/rg.exe`, sandbox helpers under `codex-resources`, and voice DLLs/licenses. Setup retains this directory layout. Its installed PATH entry is the bundle's `bin` directory. The vendor executable generates `auth.json` in an isolated directory before the config transaction applies it.

The compiled setup dry run resolved latest Claude `2.1.308`, Codex `0.163.0`, and OpenCode `1.19.4`. A shared-network GitHub API quota was encountered during verification; setup now falls back to GitHub's official latest-release redirect and expanded asset page. It requires the SHA-256 attached to the exact asset row and fails closed when that digest is absent. This fallback also has an offline regression test.

## Checks required before release

1. Resolve actual latest metadata with the Windows EXE and download the proper native architecture.
2. Verify artifact checksum before extraction or execution, retain full bundles, and validate version output.
3. Exercise a clean profile with no Node/npm/Git on PATH.
4. Restart the shell or log in again; confirm normal commands find persistent configuration and key authentication.
5. Verify an existing commented OpenCode config, Claude preferences, and Codex TOML survive a rerun.

The research establishes the flows and official constraints; it does not claim a clean Windows VM or a paid model inference was run.
