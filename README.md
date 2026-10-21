# phasesai-setup

Open source, MIT-licensed configuration installer for [PhasesAI](https://phasesai.cc). Public source: [ezjfk/phasesai-setup](https://github.com/ezjfk/phasesai-setup). Never publish the parent gateway project.

The installer configures Claude Code **2.1.306**, Codex **0.163.0**, and OpenCode **1.19.3**. These are source-reviewed recipes, not a certified PhasesAI harness/model matrix. Other versions, Pi, Grok Build, Aider and OpenCode 2 use the [manual guides](https://docs.phasesai.cc/harnesses/overview).

**Release status:** the original `v0.0.0-preview.20261021163405` binaries used Go 1.27.0 and are withdrawn from the download host. Use source or a replacement release whose CI, checksums and build provenance verify. No stable release is promoted. Source availability and signatures support inspection and origin verification; they do not prove software harmless.

## Build and run

Go **1.27.3** or newer is required. Keep Go checksum verification enabled.

```sh
go mod verify
go test ./...
go vet ./...
go build -trimpath -o phasesai-setup .
./phasesai-setup --harness codex --model MODEL_ID --dry-run
./phasesai-setup --harness codex --model MODEL_ID --smoke-test --save-key
./phasesai-setup --launch codex
./phasesai-setup --launch codex -- --help
```

Choose an exact ID from `https://api.phasesai.cc/v1/models`. Enter a key at the hidden prompt or inject `PHASESAI_API_KEY` privately. Keys never enter JSON/TOML, command arguments or diagnostic logs. Public discovery cannot validate credentials or balance. `--smoke-test` asks separately before one potentially billable API request with a 32-output-token ceiling; it does not test harness tool execution.

`--save-key` uses Windows Credential Manager, macOS Keychain, or Linux Secret Service. The service must be available and unlocked; there is no plaintext fallback. Real OS-store integration remains an acceptance check; unit tests use a mock. `--launch` reads the environment key first, then the saved key, and passes it only to the child process. Claude receives `ANTHROPIC_AUTH_TOKEN` with competing Anthropic credentials removed from that child's environment. Shell profiles and vendor sign-ins are retained. Without a saved key, inject the variable in each launch environment.

## Configuration and recovery

Run from a neutral directory. Setup detects the exact executable version, honors config-root overrides and refuses known project/profile/credential/header conflicts. Managed policy, remote settings, plugins and every shell override are not exhaustively inspected; check effective settings inside the harness.

The preview displays the config path, changed field names, endpoint and before/after SHA-256. Existing values are hidden. `--dry-run` makes no config, credential or log writes; installation, key saving and logging cannot be combined with it. Version checks and discovery still run.

`--install-missing` separately offers an exact vendor npm package on the public registry. Node/npm must exist. Vendor lifecycle scripts run only after confirmation, without requested elevation. Installation is outside config rollback. Existing unreviewed versions are refused rather than replaced automatically.

Claude uses strict JSON; OpenCode JSON/JSONC preserves comments/trailing commas; Codex TOML scalar edits preserve unrelated comments/text. Unsupported layouts, malformed files, duplicate JSON keys, linked/reparse paths, hard-linked files and other-owner files are refused. Configs must be inside the user's home and at most 2 MiB.

Transactions use native exclusive locks, home/parent handles, exclusive private backups/staging, a flushed journal, compare-before-replace and post-write validation. Backups can contain old secrets. Private POSIX files use 0600; Windows files grant the current user and SYSTEM access. Existing config permission bits or Windows DACL are preserved. Timestamps, extended attributes and alternate streams are not preserved. File rollback does not undo npm installation or credential-store changes.

Verification failure restores original bytes/permissions only if the file still matches this installation. Recover an interrupted transaction explicitly:

```sh
./phasesai-setup --recover --config /absolute/path/to/config.toml
```

Recovery asks first, retains backups and refuses to overwrite later edits. To restore a successful older install, close the harness, review subsequent changes and manually restore the adjacent `.phasesai-<random>.bak`. Unix directory syncing and Windows file flushing/journaling do not promise power-loss durability on every filesystem. Locks coordinate setup processes, not arbitrary editors.

`--log-file /path/inside/home/setup.jsonl` enables private JSONL with 1 MiB rotation. Only time, build version, harness and fixed event/result names are logged. HTTP diagnostics include status, validated request ID and bounded numeric Retry-After, never response bodies. HTTPS requests to `api.phasesai.cc` reject redirects, cap bodies at 2 MiB and have a 30-second deadline. Catalog GETs retry up to three attempts; billable POSTs never retry automatically. There is no telemetry or self-update.

## Download verification

Read release notes and metadata. Verify SHA256SUMS and provenance before execution:

```sh
gh attestation verify FILE --repo ezjfk/phasesai-setup --signer-workflow ezjfk/phasesai-setup/.github/workflows/preview.yml --source-ref refs/tags/EXACT_RELEASE_TAG
```

Replace placeholders with a published artifact/tag. Review `scripts/install.sh` or `scripts/install.ps1` first; both require an explicit version and enforce checksum plus repository/workflow/tag identity before execution. Missing provenance and withdrawn downloads fail closed. Binaries are not Apple-notarized or Windows Authenticode-signed. Never bypass a failed verification.

R2 paths: `https://downloads.phasesai.cc/setup/<tag>/<macos|linux|windows>-<amd64|arm64>/<filename>`. The private bucket is served by the hash-verifying Worker in `distribution/`. Only allowlisted GitHub assets are served; immutable keys are not overwritten. Withdrawn executables return 410. `/setup/stable.json` is unavailable until explicit promotion.

## Publish through GitHub CI

From the parent project in Git Bash:

```bash
bash phasesai-setup/scripts/publish-ci.sh --check
bash phasesai-setup/scripts/publish-ci.sh v0.1.0-preview.1
```

`publish.sh` is an alias. The script verifies the public source allowlist, clean standalone main branch and approved remote, runs checks, displays the commit/tag, then asks for `PUBLISH`. Git's normal credential helper authenticates an atomic main/tag push; nothing is force-pushed. CI runs native Windows/macOS/Linux tests and vulnerability checks, builds six targets once, emits source/SBOM/licenses/checksums, attests those exact bytes and publishes a prerelease. R2 requires separate hash-verified promotion. No cloud credentials are needed for GitHub releases.

If Git reports dubious ownership, review the installer folder and run the exact-folder `safe.directory` command printed by the script in your own terminal. Never trust every directory. Press Ctrl+C to clear an incomplete pasted command before typing at a normal prompt.

## Acceptance

`python scripts/verify-api.py --output acceptance.json` optionally checks completed/streamed API responses and harmless tool selection using a dedicated spend-limited key. It asks before billing and saves redacted results; no tools execute. Actual harness tool execution, usage records, model-specific behavior and real credential-store operation require separate acceptance. A public model list or passing unit tests do not establish those behaviors.
