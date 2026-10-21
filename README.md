# phasesai-setup

MIT-licensed **core skeleton**, prepared for a public GitHub repository named `phasesai-setup` under the maintainer's account. The public owner/URL is not established yet. This folder is isolated from the private gateway: do not publish the parent project.

The source configures Claude Code, Codex, and OpenCode 1. It is not a production installer release. Windows builds support dry-run only until ACL/reparse-point/replace semantics are implemented and tested. Pi, Grok Build, Aider, OpenCode 2, keychain integration, and harness auto-installation are design follow-ups.

## Build and inspect

Use the Go version recorded in `go.mod` or newer. Dependencies are pinned in `go.mod` and authenticated by `go.sum`; Go's standard checksum verification stays enabled.

```sh
go mod verify
go test ./...
go vet ./...
go build -trimpath -o phasesai-setup .
./phasesai-setup --version
./phasesai-setup --harness codex --model MODEL_ID --dry-run
```

Set `PHASESAI_API_KEY` privately in the same terminal, or enter it at the hidden prompt. A prompted key is used only for optional verification; it is not persisted. Codex/OpenCode launches need that variable in their own environment. Claude also needs `ANTHROPIC_AUTH_TOKEN="$PHASESAI_API_KEY"`. Existing stored credentials/managed policy can override routing; verify effective settings manually.

The skeleton does not automatically install harnesses. It detects an executable and directs missing installations to vendor instructions. It contacts only `https://api.phasesai.cc` for discovery and optional inference, refuses redirects, and has no telemetry or self-update. A catalog GET is public and cannot validate credentials. `--smoke-test` asks separately before a potentially billable request. The skeleton's authenticated check is API-level, not full harness/tool-call verification.

## Config changes and recovery

The tool previews harness, path, endpoint, and hashes without dumping config content. It parses strict JSON or TOML, retains unrelated semantic values, and refuses malformed or unsupported JSONC files. Comments and formatting may change. It checks the file hasn't changed since preview, makes a timestamped adjacent backup, stages a replacement in the same directory, reparses it, and restores previous bytes if verification fails. It leaves the backup for manual recovery and does not overwrite intervening edits during rollback. It does not provide a crash-safe multi-file transaction or a cross-process lock.

Close editors/harnesses that might write the same file. Review existing auth/header/profile overrides manually. No key is persisted by this skeleton. Backups can contain old secrets from a prior config, so protect them. New files use POSIX mode 0600/directories 0700; Windows ACL support is a release blocker. This skeleton may tighten existing POSIX file permissions to 0600; restoring bytes does not restore original permission metadata.

Diagnostics go to the terminal and never include key/config/HTTP-response bodies. Persistent structured logging, request-ID diagnostics, durable recovery journals, native OS file locks, precise owned-field diffs, TOML comment preservation, JSONC merging, and installed-version allowlists remain TODO before a stable release.

## Publish the standalone source

Once authenticated GitHub access is available, use the account's actual login in the remote URL. Inspect the staged file list first. `dist/`, local caches, logs, and `.env` are ignored.

```sh
git add .
git diff --cached --stat
git commit -m "Add open source PhasesAI setup core skeleton"
# GitHub CLI alternative: gh repo create phasesai-setup --public --source=. --remote=origin --push
git remote add origin https://github.com/YOUR_ACCOUNT/phasesai-setup.git
git push -u origin main
```

Create the empty **public** repository on GitHub first if using plain Git; `git push` alone does not create it. Do not include the private reseller code, deployment archives, customer databases, credentials, or scratch files. The included MIT license applies to this new installer source.

## Release integrity

The workflow builds reviewable preview artifacts for six OS/architecture targets and emits checksums/provenance. It deliberately does not publish a stable release or upload to R2 while release blockers remain. Pin the toolchain and action SHAs; promote exact tested bytes to GitHub Releases and R2, never rebuild separately for each host. Publish source tag/commit, license, checksums, SBOM, provenance, and release notes alongside binaries. A checksum downloaded from the same compromised host is not proof of authenticity; verify provenance against the actual public repository and expected workflow identity.

See `../docs-phasesai/maintenance/SETUP-DESIGN.md` and `R2-HOSTING.md` in the handoff package for the full product design. Those private handoff paths are not dependencies of this standalone source repository.
