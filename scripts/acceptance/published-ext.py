#!/usr/bin/env python3
"""Verify a Git revision against published dependencies in a fresh temp export."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tarfile
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--revision', default='HEAD')
parser.add_argument('--node', help='absolute Node.js executable if node is not on PATH')
parser.add_argument('--working-tree', action='store_true', help='include tracked changes and nonignored untracked files; record their hashes')
args = parser.parse_args()
repo = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], text=True).strip())
node = shutil.which(args.node or 'node')
if not node:
    parser.error('Node.js is required; supply --node /absolute/path/to/node')
revision = subprocess.check_output(['git', 'rev-parse', args.revision + '^{commit}'], cwd=repo, text=True).strip()
run = Path(tempfile.mkdtemp(prefix='cymonkey-published-ext-', dir='/private/tmp' if Path('/private/tmp').exists() else '/tmp'))
source = run / 'source'
source.mkdir()
logs = run / 'logs'
logs.mkdir()
print(f'Evidence: {run}', flush=True)
archive = run / 'source.tar'
with archive.open('wb') as out:
    subprocess.run(['git', 'archive', revision], cwd=repo, stdout=out, check=True)
with tarfile.open(archive) as contents:
    contents.extractall(source, filter='data')
if args.working_tree:
    if revision != subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo, text=True).strip():
        parser.error('--working-tree requires the HEAD revision')
    patch = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=repo)
    (run / 'working-tree.patch').write_bytes(patch)
    if patch:
        subprocess.run(['git', 'apply', '--binary', '-'], input=patch, cwd=source, check=True)
    untracked = subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', '-z'], cwd=repo).decode().split('\0')
    for name in filter(None, untracked):
        target = source / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(repo / name, target, follow_symlinks=False)
    (run / 'untracked.json').write_text(json.dumps(list(filter(None, untracked)), indent=2))
# Never allow an exported symlink to reach another checkout.
for path in source.rglob('*'):
    if path.is_symlink() and not path.resolve().is_relative_to(source):
        raise SystemExit(f'External symlink forbidden: {path}')
if list(source.rglob('vendor')):
    raise SystemExit('Vendored sources forbidden in this acceptance run')
inventory = {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(source.rglob('*')) if p.is_file()}
(run / 'source-sha256.json').write_text(json.dumps(inventory, indent=2) + '\n')
modules = sorted(p.parent for p in source.rglob('go.mod'))
(run / 'modules.json').write_text(json.dumps([str(p.relative_to(source)) for p in modules], indent=2))
for dirname in ('home', 'gopath', 'modcache', 'buildcache', 'tmp'):
    (run / dirname).mkdir()
env = {
    'PATH': str(Path(node).resolve().parent) + os.pathsep + os.environ['PATH'], 'HOME': str(run / 'home'), 'TMPDIR': str(run / 'tmp'),
    'GOWORK': 'off', 'GOENV': 'off', 'GOFLAGS': '-mod=readonly',
    'GOPATH': str(run / 'gopath'), 'GOMODCACHE': str(run / 'modcache'),
    'GOCACHE': str(run / 'buildcache'), 'GOPROXY': 'https://proxy.golang.org,direct',
    'GOSUMDB': 'sum.golang.org', 'GOPRIVATE': '', 'GONOPROXY': '', 'GONOSUMDB': '',
    'GOTOOLCHAIN': 'auto', 'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': '/dev/null',
    'GIT_CONFIG_COUNT': '0', 'GIT_TERMINAL_PROMPT': '0', 'LC_ALL': 'C',
}
(run / 'environment.json').write_text(json.dumps(env, indent=2))
results = []
def command(label, argv, cwd=source):
    print(f'{label}: {" ".join(argv)}', flush=True)
    with (logs / (label + '.log')).open('w') as output, (logs / (label + '.stderr')).open('w') as errors:
        process = subprocess.Popen(argv, cwd=cwd, env=env, stdout=output,
                                   stderr=errors if label.endswith('-graph') else subprocess.STDOUT,
                                   start_new_session=True)
        try:
            code = process.wait(timeout=1200)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            code = 124
    results.append({'check': label, 'exit_code': code})
    print(f'{label}: {"PASS" if code == 0 else "FAIL"}', flush=True)
    return code == 0
command('toolchain', ['go', 'version'])
command('node-version', ['node', '--version'])
command('go-env', ['go', 'env', '-json'])
# Protect dependency files and verify their bytes after all subprocesses finish.
deps = {p: p.read_bytes() for p in source.rglob('go.*') if p.name in ('go.mod', 'go.sum')}
for path in deps:
    path.chmod(0o444)
for module in modules:
    label = str(module.relative_to(source)).replace('/', '-')
    label = 'root' if label == '.' else label
    if not command(label + '-graph', ['go', 'list', '-m', '-json', 'all'], module):
        continue
    # Decode the JSON stream, validate all replacement paths, and prove ext is remote.
    raw = (logs / (label + '-graph.log')).read_text()
    decoder = json.JSONDecoder()
    graph = []
    while raw.strip():
        entry, end = decoder.raw_decode(raw.lstrip())
        graph.append(entry)
        raw = raw.lstrip()[end:]
    for entry in graph:
        replacement = entry.get('Replace')
        if replacement:
            if entry['Path'].startswith('github.com/webong/ext') or replacement.get('Version'):
                raise SystemExit(f'External replacement forbidden: {entry}')
            if not Path(replacement['Dir']).resolve().is_relative_to(source):
                raise SystemExit(f'Replacement escapes export: {entry}')
        if entry['Path'].startswith('github.com/webong/ext'):
            if not entry.get('Version'):
                raise SystemExit(f'ext published version missing: {entry}')
            if entry.get('Dir') and not Path(entry['Dir']).resolve().is_relative_to(run / 'modcache'):
                raise SystemExit(f'ext source is not in the fresh module cache: {entry}')
    command(label + '-build', ['go', 'build', './...'], module)
    command(label + '-test', ['go', 'test', '-count=1', '-v', '-timeout=15m', './...'], module)
    # Go 1.27.1's verify attempts to hash local short-name replacements.
    # Verify the exact remote graph in its own module instead; local source is
    # covered by source-sha256.json and the unchanged dependency-file check.
    verify = run / ('verify-' + label)
    verify.mkdir()
    remote = [e for e in graph if e.get('Version') and not e.get('Replace') and e['Path'] not in ('go', 'toolchain')]
    (verify / 'go.mod').write_text('module acceptance\n\ngo 1.26.5\n\nrequire (\n' + ''.join('\t' + e['Path'] + ' ' + e['Version'] + '\n' for e in remote) + ')\n')
    sums = sorted({line for p in deps if p.name == 'go.sum' for line in p.read_text().splitlines()})
    (verify / 'go.sum').write_text('\n'.join(sums) + '\n')
    command(label + '-verify', ['go', 'mod', 'verify'], verify)
command('cymy-build', ['go', 'build', '-o', str(run / 'cymy'), './src'])
if (run / 'cymy').exists():
    command('cymy-help', [str(run / 'cymy'), '--help'])
required = {
    'root-test.log': ['TestContentCommandsRunInTheInstalledContentService',
                      'TestContentServeStreamsItsLogsAndStopsWhenCanceled',
                      'TestStandaloneBoardProviderServesTheInventoryAndKeyboard',
                      'TestDisplayEngineProgramServesACooperativeSession',
                      'TestDoRunsATrustedAdapterAndReportsProgress',
                      'TestDoRefusesUntrustedAdaptersAndUndeclaredOperations',
                      'TestDoSurfacesAdapterFailuresAndCancellation'],
}
missing = [name for log, names in required.items() for name in names
           if not (logs / log).exists() or ('--- PASS: ' + name + ' (') not in (logs / log).read_text()]
results.append({'check': 'required-runtime-tests', 'exit_code': int(bool(missing)), 'missing': missing})
changed = [str(p.relative_to(source)) for p, data in deps.items() if not p.exists() or p.read_bytes() != data]
changed += [str(p.relative_to(source)) for p in source.rglob('go.*') if p.name in ('go.mod', 'go.sum') and p not in deps]
results.append({'check': 'dependency-files-unchanged', 'exit_code': int(bool(changed)), 'changed': changed})
skips = {p.name: [line for line in p.read_text().splitlines() if '--- SKIP:' in line] for p in logs.glob('*-test.log')}
(run / 'skipped-tests.json').write_text(json.dumps(skips, indent=2) + '\n')
summary = {'revision': revision, 'working_tree_overlay': args.working_tree, 'results': results,
           'pass': all(r['exit_code'] == 0 for r in results)}
(run / 'result.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary, indent=2), flush=True)
print(f'Evidence retained: {run}', flush=True)
raise SystemExit(0 if summary['pass'] else 1)
