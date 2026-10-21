#!/usr/bin/env bash
# Offline checks: corrupt bytes and failed provenance must never execute.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
temp="$(mktemp -d)"
trap 'rm -f "$temp/curl" "$temp/gh" "$temp/uname" "$temp/payload" "$temp/executed" "$temp/checksum" "$temp/attestation-attempted"; rmdir "$temp"' EXIT
export BOOTSTRAP_TEST_DIR="$temp"
printf '#!/bin/sh\ntouch "$BOOTSTRAP_TEST_DIR/executed"\n' > "$temp/payload"
sha256sum "$temp/payload" | awk '{print $1}' > "$temp/checksum"
cat > "$temp/curl" <<'SH'
#!/bin/sh
set -eu
destination=; url=
while [ "$#" -gt 0 ]; do
 case "$1" in -o) shift; destination=$1 ;; https://*) url=$1 ;; esac
 shift
done
case "$url" in
 */SHA256SUMS)
  hash=$(cat "$BOOTSTRAP_TEST_DIR/checksum")
  [ "${BAD_CHECKSUM:-}" != yes ] || hash=0000000000000000000000000000000000000000000000000000000000000000
  printf '%s  phasesai-setup-linux-amd64\n' "$hash" > "$destination" ;;
 *) cp "$BOOTSTRAP_TEST_DIR/payload" "$destination" ;;
esac
SH
cat > "$temp/gh" <<'SH'
#!/bin/sh
touch "$BOOTSTRAP_TEST_DIR/attestation-attempted"
exit 33
SH
cat > "$temp/uname" <<'SH'
#!/bin/sh
case "$1" in -s) printf 'Linux\n' ;; -m) printf 'x86_64\n' ;; *) exit 1 ;; esac
SH
chmod 700 "$temp/curl" "$temp/gh" "$temp/uname"
if PATH="$temp:$PATH" BAD_CHECKSUM=yes sh "$root/scripts/install.sh" v0.1.0-preview.1 > /dev/null 2>&1; then
 printf '%s\n' 'Corrupt checksum accepted.' >&2; exit 1
fi
[[ ! -e "$temp/executed" ]] || { printf '%s\n' 'Untrusted binary executed.' >&2; exit 1; }
if PATH="$temp:$PATH" sh "$root/scripts/install.sh" v0.1.0-preview.1 > /dev/null 2>&1; then
 printf '%s\n' 'Missing provenance accepted.' >&2; exit 1
fi
[[ ! -e "$temp/executed" ]] || { printf '%s\n' 'Unattested binary executed.' >&2; exit 1; }
[[ -e "$temp/attestation-attempted" ]] || { printf '%s\n' 'Provenance rejection path was not reached.' >&2; exit 1; }
printf '%s\n' 'Bootstrap refused corrupt bytes and failed provenance without execution.'
