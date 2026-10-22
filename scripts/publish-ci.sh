#!/usr/bin/env bash
# Push this isolated source and a preview tag; GitHub CI owns release binaries.
set -euo pipefail
source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$source_dir"
tag="${1:-v0.1.0-preview.$(date -u +%Y%m%d%H%M%S)}"
case "$tag" in --help|-h) printf '%s\n' 'Usage: bash phasesai-setup/scripts/publish-ci.sh [v0.1.0-preview.NUMBER|--check]'; exit 0 ;; esac
[[ $# -le 1 ]] || { printf '%s\n' 'Too many arguments.' >&2; exit 2; }
check_only=false
if [[ "$tag" == --check ]]; then check_only=true; elif [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-preview\.[0-9][A-Za-z0-9.-]*$ ]]; then printf '%s\n' 'Use an explicit preview version.' >&2; exit 2; fi
for command in git go; do command -v "$command" >/dev/null || { printf 'Missing %s\n' "$command" >&2; exit 1; }; done
if ! top="$(git rev-parse --show-toplevel 2>&1)"; then
  printf '%s\n' "$top" >&2
  if [[ "$top" == *'dubious ownership'* ]]; then
    trust_path="$source_dir"; case "$(uname -s)" in MINGW*|MSYS*) trust_path="$(pwd -W)" ;; esac
    printf '\nReview this installer folder, then run in your own terminal:\n  git config --global --add safe.directory %q\n' "$trust_path" >&2
  fi
  exit 1
fi
[[ "$(cd -- "$top" && pwd -P)" == "$source_dir" && "$(git branch --show-current)" == main ]] || { printf '%s\n' 'Use the standalone installer main branch.' >&2; exit 1; }
case "$(git remote get-url origin)" in https://github.com/ezjfk/phasesai-setup.git|https://github.com/ezjfk/phasesai-setup|git@github.com:ezjfk/phasesai-setup.git) ;; *) printf '%s\n' 'Origin must be the approved ezjfk/phasesai-setup repository.' >&2; exit 1 ;; esac
[[ ! -e server && ! -e public && ! -e data.sqlite && ! -e .env ]] || { printf '%s\n' 'Private gateway/state files detected; refusing publication.' >&2; exit 1; }
for directory in scripts distribution docs .github .github/workflows; do [[ ! -L "$directory" ]] || { printf '%s\n' 'Linked source directories are refused.' >&2; exit 1; }; done
declare -A allowed=()
while IFS= read -r file; do
  file="${file%$'\r'}"; [[ -n "$file" ]] || continue
  case "$file" in *..*|/*|*\\*) printf '%s\n' 'Unsafe source allowlist entry.' >&2; exit 1 ;; esac
  [[ -f "$file" && ! -L "$file" ]] || { printf 'Missing or linked source: %s\n' "$file" >&2; exit 1; }
  allowed["$file"]=1
done < source-files.txt
while IFS= read -r -d '' file; do [[ -n "${allowed[$file]:-}" ]] || { printf 'Unexpected tracked file: %s\n' "$file" >&2; exit 1; }; done < <(git ls-files -z)
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || { printf '%s\n' 'Source changes must be committed and reviewed first.' >&2; exit 1; }
go mod verify
go test ./...
go vet ./...
[[ "$check_only" == false ]] || { printf '%s\n' 'Preflight passed. No authentication or remote mutation attempted.'; exit 0; }
printf 'Repository: https://github.com/ezjfk/phasesai-setup\nCommit: %s\nPreview tag: %s\n' "$(git rev-parse HEAD)" "$tag"
printf '%s\n' 'This pushes reviewed source. CI tests Windows, builds once, attests artifacts and publishes a prerelease.'
read -r -p 'Type PUBLISH to push source and tag: ' confirmation
[[ "$confirmation" == PUBLISH ]] || { printf '%s\n' 'Cancelled.'; exit 1; }
git fetch origin main
git merge-base --is-ancestor origin/main HEAD || { printf '%s\n' 'Remote main has diverged; reconcile before publishing.' >&2; exit 1; }
if [[ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]]; then printf '%s\n' 'Remote tag exists; choose a new version. Nothing overwritten.' >&2; exit 1; fi
if git show-ref --verify --quiet "refs/tags/$tag"; then
  [[ "$(git rev-parse "$tag^{}")" == "$(git rev-parse HEAD)" ]] || { printf '%s\n' 'Local tag points elsewhere.' >&2; exit 1; }
else git tag -a "$tag" -m "PhasesAI setup verified preview $tag"; fi
git push --atomic origin HEAD:refs/heads/main "refs/tags/$tag"
printf '\nFollow CI: https://github.com/ezjfk/phasesai-setup/actions\nRelease after CI passes: https://github.com/ezjfk/phasesai-setup/releases/tag/%s\n' "$tag"
printf '%s\n' 'R2 promotion is a separate hash-verified operation. No stable channel was promoted.'
