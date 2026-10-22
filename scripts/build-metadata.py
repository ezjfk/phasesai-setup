"""Generate release metadata and SPDX inventory from the exact checked-out Go tree."""
import argparse, datetime, hashlib, json, os, pathlib, subprocess, sys

root = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('version')
parser.add_argument('--output', type=pathlib.Path, default=root / 'dist')
args = parser.parse_args()
dist = args.output.resolve()
version = args.version
commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
if subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=no'], cwd=root, text=True).strip():
    raise SystemExit('Refusing metadata for modified tracked source; commit reviewed source first.')
expected = []
for arch in ['amd64', 'arm64']:
    for harness in ['all', 'claude', 'codex', 'opencode']:
        name = f'phasesai-setup-windows-{arch}.exe' if harness == 'all' else f'phasesai-{harness}-setup-windows-{arch}.exe'
        expected.append(name)
        artifact = dist / name
        build = subprocess.check_output(['go', 'version', '-m', str(artifact)], text=True)
        if ('vcs.revision=' + commit) not in build or 'vcs.modified=false' not in build or 'GOOS=windows' not in build or ('GOARCH=' + arch) not in build:
            raise SystemExit('Binary/source/target mismatch: ' + artifact.name)
for artifact in dist.glob('phasesai-setup-*'):
    if artifact.name not in expected and artifact.name != 'phasesai-setup-source.tar.gz':
        raise SystemExit('Non-Windows or unexpected setup artifact: ' + artifact.name)
# Go omits linker arguments from build info with -trimpath. Check VCS/target
# identity for every binary, then execute only the native target's --version.
host_os = subprocess.check_output(['go', 'env', 'GOHOSTOS'], text=True).strip()
host_arch = subprocess.check_output(['go', 'env', 'GOHOSTARCH'], text=True).strip()
native = dist / ('phasesai-setup-' + host_os + '-' + host_arch + ('.exe' if host_os == 'windows' else ''))
if subprocess.check_output([str(native), '--version'], text=True, timeout=5).strip() != version:
    raise SystemExit('Native binary version mismatch.')
raw = subprocess.check_output(['go', 'list', '-m', '-json', 'all'], cwd=root, text=True)
decoder = json.JSONDecoder(); modules = []
while raw.strip():
    value, end = decoder.raw_decode(raw.lstrip()); modules.append(value); raw = raw.lstrip()[end:]
packages = []; notices = []
for i, module in enumerate(modules):
    name = module['Path']; revision = module.get('Version', commit)
    packages.append({'SPDXID': 'SPDXRef-Package-' + str(i), 'name': name, 'versionInfo': revision,
                     'downloadLocation': 'https://github.com/ezjfk/phasesai-setup' if module.get('Main') else 'https://proxy.golang.org/' + name + '/@v/' + revision + '.zip',
                     'filesAnalyzed': False, 'licenseConcluded': 'NOASSERTION', 'licenseDeclared': 'NOASSERTION', 'copyrightText': 'NOASSERTION'})
    directory = pathlib.Path(module.get('Dir', root))
    for file in sorted(directory.iterdir()):
        if file.is_file() and (file.name.upper().startswith(('LICENSE', 'COPYING', 'NOTICE'))):
            notices.append(name + '@' + revision + ' - ' + file.name + '\n' + file.read_text(encoding='utf-8', errors='replace'))
goroot = pathlib.Path(subprocess.check_output(['go', 'env', 'GOROOT'], text=True).strip())
notices.append('Go runtime\n' + (goroot / 'LICENSE').read_text())
spdx = {'spdxVersion': 'SPDX-2.3', 'dataLicense': 'CC0-1.0', 'SPDXID': 'SPDXRef-DOCUMENT',
        'name': 'phasesai-setup-' + version,
        'documentNamespace': 'https://github.com/ezjfk/phasesai-setup/releases/' + version + '/sbom/' + commit,
        'creationInfo': {'created': datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'), 'creators': ['Tool: phasesai-setup-build-metadata']},
        'packages': packages, 'relationships': [{'spdxElementId': 'SPDXRef-DOCUMENT', 'relationshipType': 'DESCRIBES', 'relatedSpdxElement': package['SPDXID']} for package in packages]}
dist.mkdir(parents=True, exist_ok=True)
(dist / 'sbom.spdx.json').write_text(json.dumps(spdx, indent=2) + '\n', encoding='utf-8', newline='')
(dist / 'THIRD_PARTY_NOTICES.txt').write_text('\n\n'.join(notices), encoding='utf-8', newline='')
info = {'version': version, 'source_repository': 'https://github.com/ezjfk/phasesai-setup', 'source_commit': commit,
        'toolchain': subprocess.check_output(['go', 'version'], text=True).strip(),
        'prerelease': '-' in version.split('+')[0], 'native_signed': False, 'windows_write_support': True, 'windows_only': True, 'native_harness_downloads': True, 'credential_storage': 'harness_config',
        'github_build_attestation': os.environ.get('GITHUB_ACTIONS') == 'true', 'harness_live_certification': False}
(dist / 'build-info.json').write_text(json.dumps(info, indent=2) + '\n', encoding='utf-8', newline='')
checks = []
for file in sorted(dist.iterdir()):
    if file.is_file() and file.name != 'SHA256SUMS':
        checks.append(hashlib.sha256(file.read_bytes()).hexdigest() + '  ' + file.name)
(dist / 'SHA256SUMS').write_text('\n'.join(checks) + '\n', encoding='utf-8', newline='')
print('Release inventory:', len(checks), 'assets; source:', commit)
