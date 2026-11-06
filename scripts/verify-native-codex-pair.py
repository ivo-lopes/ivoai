#!/usr/bin/env python3
"""Verify and start the exact official native pair, without login or inference."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import stat
import subprocess
import tempfile
import time


def require(value, message):
    if not value:
        raise ValueError(message)


def fingerprint(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= 512 << 20,
                'native runtime must be a bounded regular file')
        h = hashlib.sha256()
        for block in iter(lambda: stream.read(1 << 20), b''):
            h.update(block)
        after = os.fstat(stream.fileno())
        require((before.st_size, before.st_mtime_ns, before.st_ino) ==
                (after.st_size, after.st_mtime_ns, after.st_ino), 'runtime changed during verification')
        return before.st_size, h.hexdigest()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--arch', choices=('amd64', 'arm64'), required=True)
    parser.add_argument('--emulator', type=Path)
    parser.add_argument('--cpu')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(argv)
    require(platform.system() == 'Linux', 'Linux runtime required')
    require(args.directory.is_absolute() and args.directory.is_dir() and not args.directory.is_symlink(),
            'explicit regular runtime directory required')
    require(args.output.is_absolute() and not args.output.exists(), 'fresh absolute receipt required')
    native = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(args.emulator is not None or args.arch == native, 'cross-architecture startup needs an emulator')
    require((args.emulator is None) == (args.cpu is None), 'emulator and explicit CPU must be paired')
    if args.emulator:
        require(args.emulator.is_absolute() and args.emulator.is_file(), 'explicit emulator required')
        require(args.cpu == {'amd64': 'Nehalem,-avx,-avx2', 'arm64': 'cortex-a53'}[args.arch],
                'unsupported baseline CPU')
    script=Path(__file__).resolve()
    pinfile=script.parents[1]/'manifest/native-codex-runtime.json'
    source_identity={'script_sha256':fingerprint(script)[1],'pins_sha256':fingerprint(pinfile)[1]}
    pins = json.loads(pinfile.read_text())
    selected = {x['binary']: x for x in pins if x['arch'] == args.arch}
    require(set(selected) == {'codex', 'codex-code-mode-host'}, 'exact reviewed pair required')
    row = {'scope': 'official_native_pair_startup_without_auth_or_inference', 'outcome': 'FAIL',
           'version': '0.167.0', 'architecture': args.arch, 'emulated': args.emulator is not None,
           'cpu': args.cpu if args.emulator else 'native_host', 'provider_inference': False,
           'qualification_source':source_identity, 'emulator_sha256':fingerprint(args.emulator)[1] if args.emulator else None,
           'credentials_inherited': False, 'paired_child_execution_certified': False, 'executables': {}}
    started = time.monotonic()
    try:
        with tempfile.TemporaryDirectory(prefix='ivoai-native-startup-', dir='/var/tmp') as private:
            root = Path(private)
            env = {'PATH': '/usr/bin:/bin', 'HOME': private, 'CODEX_HOME': str(root/'codex-home'),
                   'XDG_CONFIG_HOME': str(root/'config'), 'XDG_DATA_HOME': str(root/'data'),
                   'XDG_CACHE_HOME': str(root/'cache'), 'XDG_STATE_HOME': str(root/'state'),
                   'LANG': 'C.UTF-8', 'NO_COLOR': '1'}
            for key in ('CODEX_HOME','XDG_CONFIG_HOME','XDG_DATA_HOME','XDG_CACHE_HOME','XDG_STATE_HOME'):
                Path(env[key]).mkdir(mode=0o700)
            for name, pin in selected.items():
                binary = args.directory/name
                size, digest = fingerprint(binary)
                require(size == pin['binary_bytes'] and digest == pin['binary_sha256'], 'official executable identity mismatch')
                command = [str(binary)]
                if args.emulator:
                    command = [str(args.emulator), '-cpu', args.cpu] + command
                diagnostic = False
                for arguments in ([['--version'], ['--help']] if name == 'codex' else [['--help']]):
                    result = subprocess.run(command+arguments, env=env, cwd=private, stdin=subprocess.DEVNULL,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
                    require(result.returncode == 0 and 0 < len(result.stdout) <= 1 << 20 and len(result.stderr) <= 1 << 20,
                            'official native startup failed')
                    diagnostic = diagnostic or bool(result.stderr)
                    if arguments == ['--version']:
                        require(result.stdout.strip() == b'codex-cli 0.167.0', 'official version mismatch')
                require(fingerprint(binary) == (size, digest), 'official executable changed during startup')
                row['executables'][name] = {'sha256': digest, 'bytes': size, 'startup_exit_zero': True, 'startup_diagnostic_output_observed': diagnostic}
        require(fingerprint(script)[1]==source_identity['script_sha256'] and fingerprint(pinfile)[1]==source_identity['pins_sha256'], 'qualification source changed during startup')
        row['outcome'] = 'PASS'
    finally:
        row['wall_ms'] = round((time.monotonic()-started)*1000)
        descriptor = os.open(args.output, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, 'w') as stream:
            json.dump(row, stream, indent=2)
            stream.write('\n')


if __name__ == '__main__':
    main()
