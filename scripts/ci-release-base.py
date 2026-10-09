#!/usr/bin/env python3
"""Print the parent only for an exact, generated release bump; otherwise print nothing.

Use content verification, not filenames: package.json and Go version files can
also contain executable changes. Never execute scripts from the candidate head.
"""
import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


def git(*args):
    return subprocess.check_output(['git', *args], stderr=subprocess.DEVNULL)


def release_base(head):
    parents = git('rev-list', '--parents', '-n', '1', head).decode().split()
    if len(parents) != 2:
        return ''
    head, parent = parents
    changed = git('diff', '--name-only', '--no-renames', parent, head).decode().splitlines()
    # Generator changes are substantive commits, including the commit that first
    # introduced version tooling; do not try to execute its parent's tooling.
    if any(name.startswith('scripts/') for name in changed):
        return ''
    scripts = ['scripts/' + name for name in (
        'version-managed-files.sh', 'set-version.sh', 'read-version.sh', 'sync-version.sh')]
    with tempfile.TemporaryDirectory(prefix='anx-release-base-') as scratch:
        root = Path(scratch)
        for name in scripts:
            dest = root / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(git('show', f'{parent}:{name}'))
            dest.chmod(0o755)
        files = subprocess.check_output(['bash', str(root / scripts[0])]).decode().splitlines()
        if not changed or set(changed) != set(files):
            return ''
        version = git('show', f'{head}:VERSION').decode().strip()
        if not re.fullmatch(r'v\d+\.\d+\.\d+', version):
            return ''
        for name in files:
            # Reject deletion, mode changes and symlink substitution as well.
            old_mode = git('ls-tree', parent, '--', name).split()[0]
            new_mode = git('ls-tree', head, '--', name).split()[0]
            if old_mode != new_mode or old_mode != b'100644':
                return ''
            dest = root / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(git('show', f'{parent}:{name}'))
        subprocess.run(['bash', str(root / 'scripts/set-version.sh'), version],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                       env={**os.environ, 'PATH': os.environ['PATH']})
        if any((root / name).read_bytes() != git('show', f'{head}:{name}') for name in files):
            return ''
    return parent


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('revision', nargs='?', default='HEAD')
    parser.add_argument('--strict', action='store_true',
                        help='Fail on detector errors when resolving release gates')
    args = parser.parse_args()
    try:
        print(release_base(args.revision))
    except (subprocess.CalledProcessError, IndexError, UnicodeError, OSError) as error:
        if args.strict:
            print(f'could not verify release ancestry: {error}', file=sys.stderr)
            sys.exit(1)
        # CI detection failure must fail closed: run full checks on this head.
        print('')
