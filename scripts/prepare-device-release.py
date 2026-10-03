#!/usr/bin/env python3
"""Build a local Cymonkey device release candidate; never publish or tag Git."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]

def run(*args, env=None):
    return subprocess.check_output(args, cwd=ROOT, env=env)

def digest_file(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

def source_identity():
    revision = run('git', 'rev-parse', 'HEAD').decode().strip()
    status = run('git', 'status', '--porcelain=v1', '-z')
    digest = hashlib.sha256(revision.encode())
    digest.update(run('git', 'diff', '--binary', 'HEAD', '--'))
    for name in sorted(run('git', 'ls-files', '--others', '--exclude-standard', '-z').split(b'\0')):
        if not name:
            continue
        path = ROOT / os.fsdecode(name)
        digest.update(name + b'\0')
        if path.is_symlink():
            digest.update(os.readlink(path).encode())
        elif path.is_file():
            digest.update(digest_file(path).encode())
    return revision, bool(status), digest.hexdigest()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path, help='New directory outside the checkout')
    parser.add_argument('--require-clean', action='store_true', help='Refuse a candidate with uncommitted source changes')
    args = parser.parse_args()
    output = args.output.expanduser().resolve()
    if output == ROOT or ROOT in output.parents:
        parser.error('output must be outside the source checkout')
    if output.exists():
        parser.error('output already exists')
    before = source_identity()
    if args.require_clean and before[1]:
        parser.error('source checkout has uncommitted changes')
    output.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix='.cymonkey-candidate-', dir=output.parent))
    try:
        env = os.environ.copy()
        env['GOWORK'] = 'off'
        # Resolve dependencies separately; candidate generation must work offline.
        env['GOPROXY'] = 'off'
        target = json.loads(run('go', 'env', '-json', 'GOOS', 'GOARCH', 'GOVERSION', env=env))
        binary = stage / ('cymy.exe' if target['GOOS'] == 'windows' else 'cymy')
        subprocess.run(['go', 'build', '-trimpath', '-buildvcs=true', '-o', str(binary), './src'], cwd=ROOT, env=env, check=True)
        build_info = run('go', 'version', '-m', str(binary)).replace(str(binary).encode(), binary.name.encode(), 1)
        (stage / 'go-build-info.txt').write_bytes(build_info)
        (stage / 'docs').mkdir()
        for name in ('device-sessions.md', 'device-sdk-extraction.md', 'device-validation.md'):
            shutil.copyfile(ROOT / 'docs' / name, stage / 'docs' / name)
        schema = Path('src/protocol/device/v1alpha1/device.schema.json')
        (stage / schema).parent.mkdir(parents=True)
        shutil.copyfile(ROOT / schema, stage / schema)
        if before != source_identity():
            raise RuntimeError('source changed during build; retry once concurrent edits finish')
        manifest = {
            'kind': 'cymonkey.device.release-candidate',
            'apiVersion': 'cymonkey.device/v1alpha1',
            'sourceRevision': before[0],
            'sourceModified': before[1],
            'sourceFingerprintSha256': before[2],
            'target': target,
            'hostPlatform': platform.system(),
            'published': False,
            'validation': 'Not implied by this build. Attach validation evidence separately.',
            'artifacts': {str(p.relative_to(stage)): {'sha256': digest_file(p), 'bytes': p.stat().st_size} for p in sorted(stage.rglob('*')) if p.is_file()},
        }
        (stage / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        stage.rename(output)
        print(output / 'manifest.json')
    finally:
        if stage.exists():
            shutil.rmtree(stage)

if __name__ == '__main__':
    main()
