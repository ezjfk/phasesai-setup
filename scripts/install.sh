#!/bin/sh
# Download an explicit release, verify GitHub build provenance, then run it.
set -eu
umask 077
version=${1:-}
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) printf '%s\n' 'Usage: sh install.sh vVERSION [installer arguments...]' >&2; exit 2 ;; esac
case "$version" in *[!A-Za-z0-9.-]*) printf '%s\n' 'Invalid release tag.' >&2; exit 2 ;; esac
shift
command -v gh >/dev/null 2>&1 || { printf '%s\n' 'Install the official GitHub CLI first; it verifies the build signature before execution.' >&2; exit 1; }
case "$(uname -s)" in Darwin) platform=macos; os=darwin ;; Linux) platform=linux; os=linux ;; *) printf '%s\n' 'Use install.ps1 on Windows.' >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) printf '%s\n' 'Unsupported CPU architecture.' >&2; exit 1 ;; esac
asset="phasesai-setup-$os-$arch"
workdir=$(mktemp -d)
trap 'rm -f "$workdir/artifact" "$workdir/SHA256SUMS"; rmdir "$workdir"' EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "https://downloads.phasesai.cc/setup/$version/$platform-$arch/$asset" -o "$workdir/artifact"
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "https://github.com/ezjfk/phasesai-setup/releases/download/$version/SHA256SUMS" -o "$workdir/SHA256SUMS"
expected=$(tr -d '\r' < "$workdir/SHA256SUMS" | awk -v name="$asset" '$2==name {print $1}')
case "$expected" in *[!a-f0-9]*|'') printf '%s\n' 'Missing or invalid checksum.' >&2; exit 1 ;; esac
[ "${#expected}" -eq 64 ] || { printf '%s\n' 'Invalid checksum length.' >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$workdir/artifact" | awk '{print $1}'); else actual=$(shasum -a 256 "$workdir/artifact" | awk '{print $1}'); fi
[ "$actual" = "$expected" ] || { printf '%s\n' 'Checksum mismatch; nothing executed.' >&2; exit 1; }
gh attestation verify "$workdir/artifact" --repo ezjfk/phasesai-setup --signer-workflow ezjfk/phasesai-setup/.github/workflows/preview.yml --source-ref "refs/tags/$version"
chmod 700 "$workdir/artifact"
printf '%s\n' 'Verified checksum and GitHub build provenance. Starting interactive setup.'
"$workdir/artifact" "$@" < /dev/tty
