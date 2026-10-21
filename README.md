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

From the project folder in **Git Bash**, run:

```bash
bash phasesai-setup/scripts/publish.sh
```

Or, from this standalone repository, run `bash scripts/publish.sh`. The script requires Git, Go, and Python 3.9+ (not the GitHub CLI). It uses your normal Git credential helper, identifies the authenticated GitHub account, and asks you to type `PUBLISH` before creating or changing anything on GitHub.

It creates/reuses a public `phasesai-setup` repository, verifies the source allowlist, runs module verification/tests/vet, builds six binaries, pushes source and a preview tag, then uploads the binaries, checksums, source archive, license and build metadata. Assets are staged in a draft before publishing a **prerelease**. The local builds remain unsigned; this does not create a stable installer or provide GitHub build attestations. Installer limitations still apply.

Use `bash scripts/publish.sh --check` for a preflight without authentication or changes. Supply a version explicitly with `bash scripts/publish.sh v0.0.0-preview.1`. If Git Credential Manager needs a sign-in, run `git credential-manager github login` in your normal terminal first. The credential must allow creating the repository and pushing its workflow; tokens are never printed, placed in arguments, or written by this script. Existing remote URLs, public visibility, and release tags are checked; nothing is force-pushed, deleted, or converted from private to public. Inspect any partial repository/draft after a failed publish and use a new tag if assets cannot be verified.

The script does not publish Mintlify, change the gateway website, deploy redirects, or upload to R2. [Git's credential interface](https://git-scm.com/docs/git-credential), [GitHub repository creation](https://docs.github.com/en/rest/repos/repos#create-a-repository-for-the-authenticated-user), and [release uploads](https://docs.github.com/en/rest/releases/assets#upload-a-release-asset) describe the interfaces it uses.

For manual publishing instead, use the account's actual login in the remote URL. Inspect the staged file list first. `dist/`, local caches, logs, and `.env` are ignored.

```sh
git add .gitattributes .github .gitignore LICENSE README.md go.mod go.sum main.go main_test.go scripts/publish.sh
git diff --cached --stat
git commit -m "Add open source PhasesAI setup core skeleton"
# GitHub CLI alternative: gh repo create phasesai-setup --public --source=. --remote=origin --push
git remote add origin https://github.com/YOUR_ACCOUNT/phasesai-setup.git
git push -u origin main
```

Create the empty **public** repository on GitHub first if using plain Git; `git push` alone does not create it. Do not include the private reseller code, deployment archives, customer databases, credentials, or scratch files. The included MIT license applies to this new installer source.

## Release integrity

The workflow builds reviewable preview artifacts for six OS/architecture targets and emits checksums/provenance. It deliberately does not publish a stable release or upload to R2 while release blockers remain. The Bash script publishes a separately identified local-build prerelease; do not confuse those bytes with attested Actions artifacts. Pin the toolchain and action SHAs; promote exact tested bytes to GitHub Releases and R2, never rebuild separately for each host. Publish source tag/commit, license, checksums, SBOM, provenance, and release notes alongside binaries. A checksum downloaded from the same compromised host is not proof of authenticity; verify provenance against the actual public repository and expected workflow identity.

See `../docs-phasesai/maintenance/SETUP-DESIGN.md` and `R2-HOSTING.md` in the handoff package for the full product design. Those private handoff paths are not dependencies of this standalone source repository.
