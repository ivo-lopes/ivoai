import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

parser = argparse.ArgumentParser(description='Run the explicit real Memory HTTP test in an isolated network namespace')
parser.add_argument('--memory-binary', type=Path, required=True)
parser.add_argument('--test-binary', type=Path, required=True)
parser.add_argument('--output', type=Path, required=True)
parser.add_argument('--label', required=True)
options = parser.parse_args()
root = options.output.resolve()
root.mkdir(mode=0o700, parents=True, exist_ok=True)
binary = options.memory_binary.resolve()
assert hashlib.sha256(binary.read_bytes()).hexdigest() == '5a8e2c51cb09dadfc157601772b32c79ee63a6cbf9f05caf59c67da385bae6c1'
test_binary = options.test_binary.resolve()
label = options.label
if Path(label).name != label or label in ['.', '..']:
    raise SystemExit('label must be one filename component')
if not test_binary.is_file() or not os.access(test_binary, os.X_OK):
    parser.error('the compiled executable must exist before starting the runtime fixture')
test_sha256 = hashlib.sha256(test_binary.read_bytes()).hexdigest()
parent_namespace = os.readlink('/proc/self/ns/net')
with tempfile.TemporaryDirectory(prefix='runtime-', dir=root) as runtime:
    runtime_path = Path(runtime)
    for name in ['tmp', 'home']:
        (runtime_path / name).mkdir(mode=0o700)
    args = ['/usr/bin/bwrap', '--die-with-parent', '--new-session',
            '--unshare-user', '--uid', '1000', '--gid', '1000',
            '--unshare-net', '--unshare-pid', '--unshare-ipc', '--unshare-uts',
            '--ro-bind', '/usr', '/usr', '--ro-bind', '/lib', '/lib',
            '--ro-bind', '/lib64', '/lib64', '--proc', '/proc', '--dev', '/dev',
            '--dir', '/etc', '--ro-bind', '/etc/ld.so.cache', '/etc/ld.so.cache',
            '--dir', '/fixture', '--ro-bind', str(test_binary), '/fixture/admin.test',
            '--ro-bind', str(binary), '/fixture/ai-memory',
            '--bind', runtime, '/private', '--chdir', '/private', '--clearenv',
            '--setenv', 'PATH', '/usr/bin:/bin', '--setenv', 'HOME', '/private/home',
            '--setenv', 'TMPDIR', '/private/tmp', '--setenv', 'GOMAXPROCS', '2',
            '--setenv', 'GOMEMLIMIT', '768MiB',
            '--setenv', 'IVOAI_ADMIN_REAL_MEMORY_BINARY', '/fixture/ai-memory',
            '--setenv', 'IVOAI_ADMIN_MEMORY_PARENT_NETNS', parent_namespace,
            '/fixture/admin.test', '-test.v', '-test.count=1', '-test.timeout=75s',
            '-test.run=^TestAdministrativePositiveMemoryStatusOfficialServer$']
    started = time.monotonic()
    result = subprocess.run(args, capture_output=True, timeout=90,
                            env={'PATH': '/usr/bin:/bin', 'HOME': runtime})
    wall = time.monotonic() - started
    (root / (label + '.log')).write_bytes(result.stdout + result.stderr)
    receipt = {'epoch': label, 'exit_code': result.returncode, 'wall_seconds': wall,
               'official_memory_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
               'test_binary_sha256': test_sha256,
               'network': 'private bubblewrap namespace, no external route',
               'personal_home_mounted': False, 'private_runtime_cleanup': 'TemporaryDirectory',
               'systemd_or_production': False, 'provider_inference': False}
    (root / (label + '.json')).write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt))
    print(result.stdout.decode(errors='replace'))
    print(result.stderr.decode(errors='replace'))
    raise SystemExit(result.returncode)
