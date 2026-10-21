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
for target in ['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64', 'windows-amd64', 'windows-arm64']:
    artifact = dist / ('phasesai-setup-' + target + ('.exe' if target.startswith('windows-') else ''))
    build = subprocess.check_output(['go', 'version', '-m', str(artifact)], text=True)
    if ('vcs.revision=' + commit) not in build or 'vcs.modified=false' not in build or ('-X main.version=' + version) not in build:
        raise SystemExit('Binary/source/version mismatch: ' + artifact.name)
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
            notices.append(name + '@' + revision + ' — ' + file.name + '\n' + file.read_text(encoding='utf-8', errors='replace'))
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
        'prerelease': True, 'native_signed': False, 'windows_write_support': True,
        'github_build_attestation': os.environ.get('GITHUB_ACTIONS') == 'true', 'harness_live_certification': False}
(dist / 'build-info.json').write_text(json.dumps(info, indent=2) + '\n', encoding='utf-8', newline='')
checks = []
for file in sorted(dist.iterdir()):
    if file.is_file() and file.name != 'SHA256SUMS':
        checks.append(hashlib.sha256(file.read_bytes()).hexdigest() + '  ' + file.name)
(dist / 'SHA256SUMS').write_text('\n'.join(checks) + '\n', encoding='utf-8', newline='')
print('Release inventory:', len(checks), 'assets; source:', commit)
