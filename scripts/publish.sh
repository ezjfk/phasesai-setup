#!/usr/bin/env bash
# Compatibility alias: CI is the only release-binary publisher.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
exec bash "$script_dir/publish-ci.sh" "$@"
