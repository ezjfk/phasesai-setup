#!/usr/bin/env bash
# Publish this standalone source and six UNSIGNED PREVIEW binaries to GitHub.
# Uses Git's credential helper in your own terminal; GitHub CLI is not required.
set -euo pipefail

usage() {
  cat <<'HELP'
Usage: bash phasesai-setup/scripts/publish.sh [v0.0.0-preview.NUMBER]
       bash phasesai-setup/scripts/publish.sh --check

Requires Git, Go (see go.mod), and Python 3. Uses your normal GitHub Git login.
Creates/reuses YOUR_GITHUB_ACCOUNT/phasesai-setup as a PUBLIC repository.
Runs checks, builds six binaries, pushes main/tag, then publishes a prerelease
with binaries, SHA256SUMS, source archive, license, and build metadata.
Asks you to type PUBLISH before changing anything on GitHub.

The installer is a core skeleton. Windows supports dry-run only. These local
builds are unsigned and do not have GitHub build attestations or notarization.
This script does not deploy Mintlify, change website links, retire /docs, or
upload to R2. --check makes no changes and does not authenticate.
HELP
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  --check) check_only=true; tag='' ;;
  *) check_only=false; tag="${1:-v0.0.0-preview.$(date -u +%Y%m%d%H%M%S)}" ;;
esac
[[ $# -le 1 ]] || { usage >&2; exit 2; }
if [[ "$check_only" == false && ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-preview\.[0-9][A-Za-z0-9.-]*$ ]]; then
  printf 'Use a preview tag such as v0.0.0-preview.1\n' >&2; exit 2
fi

source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$source_dir"
for tool in git go; do
  command -v "$tool" >/dev/null || { printf 'Missing required tool: %s\n' "$tool" >&2; exit 1; }
done
python_cmd=()
for candidate in python3 python; do
  if command -v "$candidate" >/dev/null && "$candidate" -c 'import sys; sys.exit(sys.version_info < (3, 9))' 2>/dev/null; then
    python_cmd=("$candidate"); break
  fi
done
if [[ ${#python_cmd[@]} -eq 0 ]] && command -v py >/dev/null && py -3 -c 'import sys; sys.exit(sys.version_info < (3, 9))' 2>/dev/null; then
  python_cmd=(py -3)
fi
[[ ${#python_cmd[@]} -gt 0 ]] || { printf 'Python 3.9+ is required.\n' >&2; exit 1; }

# Exact source allowlist: never stage or publish the private parent gateway.
files=(.gitattributes .github/workflows/preview.yml .gitignore LICENSE README.md go.mod go.sum main.go main_test.go scripts/publish.sh)
for file in "${files[@]}"; do
  [[ -f "$file" && ! -L "$file" ]] || { printf 'Missing or linked source file: %s\n' "$file" >&2; exit 1; }
done
[[ ! -e server && ! -e public && ! -e data.sqlite && ! -e .env ]] || {
  printf 'Refusing to publish: this directory contains private gateway/state files.\n' >&2; exit 1;
}
if [[ -e .git ]]; then
  if ! top="$(git rev-parse --show-toplevel 2>&1)"; then
    printf '%s\n' "$top" >&2
    if [[ "$top" == *'detected dubious ownership'* ]]; then
      trust_path="$source_dir"
      case "$(uname -s)" in
        MINGW*|MSYS*) trust_path="$(pwd -W)" ;;
      esac
      printf '\nRepository ownership differs from your current OS account.\n' >&2
      printf 'This can happen when a coding sandbox created the checkout.\n' >&2
      printf 'After reviewing this installer folder, run in your own terminal:\n\n' >&2
      printf '  git config --global --add safe.directory %q\n\n' "$trust_path" >&2
      printf 'Then rerun this script. No trust settings or GitHub resources were changed.\n' >&2
    fi
    exit 1
  fi
  [[ "$(cd -- "$top" && pwd -P)" == "$source_dir" ]] || { printf 'Installer must be its own Git repository.\n' >&2; exit 1; }
  [[ "$(git branch --show-current)" == main ]] || { printf 'Switch the standalone installer repository to main first.\n' >&2; exit 1; }
  while IFS= read -r -d '' file; do
    case "$file" in
      .gitattributes|.github/workflows/preview.yml|.gitignore|LICENSE|README.md|go.mod|go.sum|main.go|main_test.go|scripts/publish.sh) ;;
      *) printf 'Unexpected tracked file; refusing publication: %s\n' "$file" >&2; exit 1 ;;
    esac
  done < <(git ls-files -z)
  [[ -z "$(git status --porcelain --untracked-files=all)" ]] || {
    printf 'Commit or resolve installer source changes first. Nothing was published.\n' >&2; exit 1;
  }
fi

printf 'Standalone source: %s\n' "$source_dir"
printf 'Source allowlist (%s files):\n' "${#files[@]}"
printf '  %s\n' "${files[@]}"
git --version
go version
"${python_cmd[@]}" --version
if [[ "$check_only" == true ]]; then
  printf 'Preflight passed. No files changed; no GitHub requests made.\n'; exit 0
fi
if [[ ! -e .git ]]; then
  git init -b main
  git add -- "${files[@]}"
  git commit -m 'Add open source PhasesAI setup core skeleton'
fi

printf '\nVerifying source and running tests...\n'
go mod verify
go test ./...
go vet ./...
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || { printf 'Checks changed source; commit and retry.\n' >&2; exit 1; }
commit="$(git rev-parse HEAD)"
release_dir="$source_dir/dist/$tag"
mkdir -p -- "$release_dir"
printf '\nBuilding preview %s from %s...\n' "$tag" "$commit"
for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  target_os="${target%/*}"; target_arch="${target#*/}"; suffix=''
  [[ "$target_os" != windows ]] || suffix='.exe'
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -buildvcs=true \
    -ldflags "-X main.version=${tag#v}" \
    -o "$release_dir/phasesai-setup-$target_os-$target_arch$suffix" .
  printf '  Built %s\n' "$target"
done
git archive --format=tar.gz --output="$release_dir/phasesai-setup-source.tar.gz" HEAD
cp -- LICENSE "$release_dir/LICENSE"

# Only this temporary helper is removed; release files stay available to inspect.
# No credential is written into the helper or passed in its arguments.
helper="$(mktemp "${TMPDIR:-/tmp}/phasesai-publish.XXXXXXXX")"
trap 'rm -f -- "$helper"' EXIT
cat > "$helper" <<'PYTHON'
import hashlib
import json
import pathlib
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

API = 'https://api.github.com'
API_VERSION = '2026-03-10'
NAME = 'phasesai-setup'

def stop(message):
    raise SystemExit(message)

def git(*args, capture=False, check=True):
    return subprocess.run(['git', *args], text=True, capture_output=capture, check=check)

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None

def main():
    release_dir = pathlib.Path(sys.argv[1])
    tag, commit = sys.argv[2:4]
    go_version = subprocess.check_output(['go', 'version'], text=True).strip()
    credential = subprocess.run(
        ['git', 'credential', 'fill'], input='protocol=https\nhost=github.com\n\n',
        text=True, capture_output=True)
    fields = dict(line.split('=', 1) for line in credential.stdout.splitlines() if '=' in line)
    token = fields.get('password', '')
    if credential.returncode or not token or '\n' in token or '\r' in token:
        stop('GitHub credentials unavailable. Run git credential-manager github login in your normal terminal, then retry. No GitHub changes made.')
    # Never print credential stdout/stderr, raw API bodies, or the token.
    opener = urllib.request.build_opener(NoRedirect())

    def request(method, path, payload=None, missing=False, binary=None):
        url = path if path.startswith('https://') else API + path
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme != 'https' or parsed.hostname not in ('api.github.com', 'uploads.github.com'):
            stop('Unexpected API destination; refusing to transmit credentials.')
        headers = {'Accept': 'application/vnd.github+json', 'Authorization': 'Bearer ' + token,
                   'X-GitHub-Api-Version': API_VERSION, 'User-Agent': 'phasesai-setup-publisher'}
        data = None
        if binary is not None:
            data = binary.read_bytes()
            headers['Content-Type'] = 'application/octet-stream'
        elif payload is not None:
            data = json.dumps(payload).encode()
            headers['Content-Type'] = 'application/json'
        try:
            with opener.open(urllib.request.Request(url, data=data, headers=headers, method=method), timeout=120) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            if missing and exc.code == 404:
                return None
            stop('GitHub HTTP %s during %s. Check repository/workflow permissions or rate limits. Any incomplete release remains a draft; no assets are deleted.' % (exc.code, method))
        except (urllib.error.URLError, TimeoutError):
            stop('GitHub network/TLS request failed. Inspect your repository before retrying; incomplete releases remain drafts.')

    user = request('GET', '/user')
    owner = user.get('login', '')
    if not re.fullmatch(r'[A-Za-z0-9-]+', owner):
        stop('Unexpected GitHub account name.')
    repo_path = '/repos/' + owner + '/' + NAME
    repo_url = 'https://github.com/' + owner + '/' + NAME
    remote_url = repo_url + '.git'
    remote = git('remote', 'get-url', 'origin', capture=True, check=False)
    allowed_remotes = (remote_url.lower(), repo_url.lower(), ('git@github.com:' + owner + '/' + NAME + '.git').lower())
    if remote.returncode == 0:
        push_urls = git('remote', 'get-url', '--push', '--all', 'origin', capture=True).stdout.splitlines()
        if remote.stdout.strip().lower() not in allowed_remotes or len(push_urls) != 1 or push_urls[0].lower() not in allowed_remotes:
            stop('Existing origin/push URL points elsewhere. Refusing to change it or publish to another repository.')
    repo = request('GET', repo_path, missing=True)
    if repo and (repo.get('private') or repo.get('archived') or not repo.get('permissions', {}).get('push')):
        stop('Existing repository must be public, unarchived, and writable. No permissions were changed.')
    if repo and repo.get('size', 0) and repo.get('default_branch') != 'main':
        stop('Existing repository uses a different default branch; inspect it manually first.')
    tag_path = repo_path + '/git/ref/tags/' + urllib.parse.quote(tag, safe='')
    remote_tag = request('GET', tag_path, missing=True) if repo else None
    if remote_tag and remote_tag.get('object', {}).get('sha') != commit:
        stop('Preview tag already points at a different commit. Choose a new tag; nothing was overwritten.')
    release = request('GET', repo_path + '/releases/tags/' + urllib.parse.quote(tag, safe=''), missing=True) if repo else None
    if release and (not release.get('draft') or release.get('target_commitish') != commit):
        stop('Release already published or belongs to different source. Choose another preview tag.')
    local_tag = git('rev-parse', '--verify', 'refs/tags/' + tag + '^{commit}', capture=True, check=False)
    if local_tag.returncode == 0 and local_tag.stdout.strip() != commit:
        stop('Local tag points elsewhere. Choose a new preview tag.')

    print('\nAccount:', owner)
    print('PUBLIC repository:', repo_url)
    print('Source commit:', commit)
    print('Preview tag:', tag)
    print('Will push installer source, create a preview release, and upload six unsigned binaries.')
    print('Windows writes remain disabled. No Mintlify, gateway, redirects, or R2 deployment is performed.')
    if input('Type PUBLISH to proceed: ').strip() != 'PUBLISH':
        stop('Cancelled. No GitHub changes made; local builds remain in ' + str(release_dir))

    if not repo:
        repo = request('POST', '/user/repos', {
            'name': NAME, 'private': False, 'auto_init': False,
            'description': 'Open source PhasesAI harness setup core skeleton (preview)',
            'has_issues': True, 'has_wiki': False})
    if remote.returncode:
        git('remote', 'add', 'origin', remote_url)
    git('push', '--set-upstream', 'origin', 'main')
    if local_tag.returncode:
        git('tag', tag, commit)
    git('push', 'origin', 'refs/tags/' + tag)

    metadata = {'version': tag, 'source_repository': repo_url, 'source_commit': commit,
                'toolchain': go_version, 'unsigned_preview': True,
                'windows_write_support': False, 'github_build_attestation': False}
    (release_dir / 'build-info.json').write_text(json.dumps(metadata, indent=2) + '\n', encoding='utf-8')
    names = [
        'phasesai-setup-darwin-amd64', 'phasesai-setup-darwin-arm64',
        'phasesai-setup-linux-amd64', 'phasesai-setup-linux-arm64',
        'phasesai-setup-windows-amd64.exe', 'phasesai-setup-windows-arm64.exe',
        'phasesai-setup-source.tar.gz', 'LICENSE', 'build-info.json']
    assets = [release_dir / name for name in names]
    hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in assets}
    checksum = release_dir / 'SHA256SUMS'
    checksum.write_text(''.join(hashes[p.name] + '  ' + p.name + '\n' for p in assets), encoding='utf-8')
    assets.append(checksum)
    hashes[checksum.name] = hashlib.sha256(checksum.read_bytes()).hexdigest()
    if not release:
        release = request('POST', repo_path + '/releases', {
            'tag_name': tag, 'target_commitish': commit,
            'name': 'PhasesAI Setup ' + tag + ' (core skeleton preview)',
            'draft': True, 'prerelease': True,
            'body': 'MIT-licensed source and locally built preview binaries.\n\n'
                    '**Core skeleton; not a stable installer. Windows supports dry-run only.** '
                    'No harness auto-installation, persistent keychain integration, or crash recovery. '
                    'Binaries are unsigned and have no GitHub build attestation or platform notarization. '
                    'See README for limitations. SHA256SUMS detects byte changes; it does not authenticate the publisher.\n\n'
                    'Source commit: `' + commit + '`. Source archive, license and build details are attached.'})
    existing = {a['name']: a for a in release.get('assets', [])}
    upload_url = release['upload_url'].split('{', 1)[0]
    for asset in assets:
        if asset.name in existing:
            previous = existing[asset.name]
            if previous.get('digest') != 'sha256:' + hashes[asset.name]:
                stop('Draft already contains an unverified/different asset named ' + asset.name + '; inspect it or choose a new tag. Nothing was deleted.')
            print('Verified existing asset:', asset.name)
            continue
        uploaded = request('POST', upload_url + '?' + urllib.parse.urlencode({'name': asset.name}), binary=asset)
        if uploaded.get('size') != asset.stat().st_size or uploaded.get('state') != 'uploaded':
            stop('Upload verification failed for ' + asset.name + '; release remains a draft.')
        if uploaded.get('digest') and uploaded['digest'] != 'sha256:' + hashes[asset.name]:
            stop('Upload hash mismatch; release remains a draft.')
        print('Uploaded:', asset.name)
    published = request('PATCH', repo_path + '/releases/' + str(release['id']), {
        'draft': False, 'prerelease': True, 'make_latest': 'false'})
    confirmed = request('GET', repo_path + '/releases/tags/' + urllib.parse.quote(tag, safe=''))
    if confirmed.get('draft') or not confirmed.get('prerelease'):
        stop('Release visibility did not verify. Inspect the repository.')
    print('\nPublished source:', repo_url)
    print('Preview release:', published['html_url'])
    print('Keep the preview label until the installer release TODOs are resolved.')

if __name__ == '__main__':
    try:
        main()
    except KeyboardInterrupt:
        stop('\nInterrupted. No automatic remote rollback was attempted; inspect the repository/draft release before retrying.')
    except subprocess.CalledProcessError:
        stop('A Git command failed. Nothing was force-pushed or deleted. Check GitHub access (including workflow write permission), then retry.')
PYTHON

"${python_cmd[@]}" "$helper" "$release_dir" "$tag" "$commit"
