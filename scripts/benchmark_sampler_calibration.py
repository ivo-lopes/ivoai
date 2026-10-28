"""Bounded offline OFF/ON procfs calibration; no client, network or credentials."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import resource
import select
import signal
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
from benchmark_process_sampler import ProcessSampler

CASES = ('wait', 'cpu', 'touched_memory_children', 'churn')
REPETITIONS = 4
ATTEMPT_SECONDS = 3.0
TOTAL_SECONDS = 110.0
HOLD_SECONDS = 0.25
CPU_ITERATIONS = 2_000_000
MEMORY_CHILDREN = 2
MEMORY_BYTES = 4 * 1024 * 1024
CHURN_CHILDREN = 12
CHURN_HOLD_SECONDS = 0.012
MAX_MESSAGE = 2048


def plan():
    return [{'case': case, 'repetition': repetition, 'arm': arm,
             'order': 'AB' if repetition % 2 == 0 else 'BA', 'ordinal': ordinal}
            for ordinal, (case, repetition, arm) in enumerate(
                (case, repetition, arm) for case in CASES
                for repetition in range(REPETITIONS)
                for arm in (('OFF', 'ON') if repetition % 2 == 0 else ('ON', 'OFF')))]


def expected(case):
    children = {'wait': 0, 'cpu': 0, 'touched_memory_children': MEMORY_CHILDREN,
                'churn': CHURN_CHILDREN}[case]
    simultaneous = 1 + (MEMORY_CHILDREN if case == 'touched_memory_children' else int(case == 'churn'))
    pages = MEMORY_CHILDREN * ((MEMORY_BYTES + os.sysconf('SC_PAGE_SIZE') - 1) // os.sysconf('SC_PAGE_SIZE')) if case == 'touched_memory_children' else 0
    checksum = ((CPU_ITERATIONS - 1) * CPU_ITERATIONS * (2 * CPU_ITERATIONS - 1) // 6) % 104729 if case == 'cpu' else pages * 7
    return {'started_children': children, 'completed_children': children,
            'child_failures': 0, 'checksum': checksum, 'touched_pages': pages,
            'known_total_processes': 1 + children, 'known_simultaneous_processes': simultaneous,
            'ready_children': MEMORY_CHILDREN if case == 'touched_memory_children' else int(case == 'churn')}


def emit(value):
    print(json.dumps(value, separators=(',', ':')), flush=True)


class ProtocolFailure(ValueError):
    """Fixed diagnostic labels only; never retain input or exception text."""
    REASONS = ('eof', 'oversize', 'invalid_json', 'shape', 'ready_mismatch', 'oracle_mismatch')

    def __init__(self, reason):
        if reason not in self.REASONS:
            raise ValueError('unknown diagnostic reason')
        self.reason = reason
        super().__init__(reason)


class ProtocolDeadline(TimeoutError):
    def __init__(self):
        super().__init__('deadline')


def failure_details(error, exit_code):
    # wait4's owned-group deadline sentinel is an observed outcome, not an
    # errno inferred from a signal. Keep protocol details for zero/unknown exit.
    if exit_code == 124:
        reason = 'deadline'
    elif exit_code is not None and exit_code != 0:
        reason = 'nonzero_exit'
    elif isinstance(error, ProtocolFailure):
        reason = error.reason
    elif isinstance(error, (TimeoutError, subprocess.TimeoutExpired)):
        reason = 'deadline'
    elif isinstance(error, OSError):
        reason = 'os_error'
    else:
        reason = 'value_error'
    number = error.errno if isinstance(error, OSError) else None
    number = number if type(number) is int and 0 <= number <= 4095 else None
    return reason, number


def read_message(fd, deadline):
    body = bytearray()
    while time.monotonic() < deadline:
        if not select.select([fd], [], [], min(0.01, max(0, deadline - time.monotonic())))[0]:
            continue
        byte = os.read(fd, 1)
        if not byte:
            raise ProtocolFailure('eof')
        body.extend(byte)
        if len(body) > MAX_MESSAGE:
            raise ProtocolFailure('oversize')
        if byte == b'\n':
            try:
                value = json.loads(body)
            except ValueError:
                raise ProtocolFailure('invalid_json') from None
            if type(value) is not dict:
                raise ProtocolFailure('shape')
            return value
    raise ProtocolDeadline()


def private_env(home):
    return {'PATH': '/usr/bin:/bin', 'HOME': str(home), 'TMPDIR': str(home),
            'LC_ALL': 'C', 'TZ': 'UTC'}


def worker(kind, case):
    resource.setrlimit(resource.RLIMIT_AS, (128 << 20, 128 << 20))
    resource.setrlimit(resource.RLIMIT_CPU, (8, 8))
    resource.setrlimit(resource.RLIMIT_NOFILE, (32, 32))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    if kind != 'coordinator':
        data = bytearray(MEMORY_BYTES) if kind == 'memory_child' else None
        pages = 0
        if data is not None:
            for offset in range(0, len(data), os.sysconf('SC_PAGE_SIZE')):
                data[offset] = 7
                pages += 1
        emit({'kind': 'ready', 'pages': pages})
        if sys.stdin.buffer.read(1) != b'G':
            return 2
        time.sleep(HOLD_SECONDS if kind == 'memory_child' else CHURN_HOLD_SECONDS)
        emit({'kind': 'done', 'pages': pages, 'checksum': sum(data[::os.sysconf('SC_PAGE_SIZE')]) if data is not None else 0})
        return 0
    started_cpu = time.process_time_ns()
    children = []
    owned_children = []
    count = completed = failures = touched = checksum = 0

    def spawn(child_kind):
        nonlocal count
        child = subprocess.Popen([sys.executable, '-I', str(Path(__file__).resolve()),
                                  '--worker', child_kind], stdin=subprocess.PIPE,
                                 stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                 env=private_env(Path(os.environ['HOME'])))
        owned_children.append(child)
        count += 1
        ready = read_message(child.stdout.fileno(), time.monotonic() + ATTEMPT_SECONDS)
        if ready.get('kind') != 'ready':
            raise ValueError('protocol')
        return child

    def finish(child):
        nonlocal completed, failures, touched, checksum
        child.stdin.write(b'G'); child.stdin.close()
        done = read_message(child.stdout.fileno(), time.monotonic() + ATTEMPT_SECONDS)
        code = child.wait(timeout=ATTEMPT_SECONDS)
        child.stdout.close()
        completed += 1
        failures += int(code != 0 or done.get('kind') != 'done')
        touched += done.get('pages', -1)
        checksum += done.get('checksum', -1)

    try:
        if case == 'touched_memory_children':
            children = [spawn('memory_child') for _ in range(MEMORY_CHILDREN)]
        elif case == 'churn':
            children = [spawn('churn_child')]
        emit({'kind': 'ready', 'ready_children': len(children)})
        if sys.stdin.buffer.read(1) != b'G':
            return 2
        if case == 'wait':
            time.sleep(HOLD_SECONDS)
        elif case == 'cpu':
            for i in range(CPU_ITERATIONS):
                checksum = (checksum + i * i) % 104729
        elif case == 'touched_memory_children':
            # Release all children together; all were held and touched at READY.
            for child in children:
                child.stdin.write(b'G'); child.stdin.close()
            for child in children:
                done = read_message(child.stdout.fileno(), time.monotonic() + ATTEMPT_SECONDS)
                failures += int(child.wait(timeout=ATTEMPT_SECONDS) != 0 or done.get('kind') != 'done')
                child.stdout.close(); completed += 1
                touched += done.get('pages', -1); checksum += done.get('checksum', -1)
        else:
            finish(children[0])
            for _ in range(CHURN_CHILDREN - 1):
                finish(spawn('churn_child'))
        child_usage = resource.getrusage(resource.RUSAGE_CHILDREN)
        emit({'kind': 'done', 'started_children': count, 'completed_children': completed,
              'child_failures': failures, 'checksum': checksum, 'touched_pages': touched,
              'reported_coordinator_cpu_ms': (time.process_time_ns() - started_cpu) / 1e6,
              'reported_waited_children_cpu_ms': (child_usage.ru_utime + child_usage.ru_stime) * 1000})
        return 0
    finally:
        for child in owned_children:
            if child.poll() is None:
                child.kill()
            child.wait()
            for pipe in (child.stdin, child.stdout):
                if pipe is not None and not pipe.closed:
                    pipe.close()



def waiter():
    spec = importlib.util.spec_from_file_location('calibration_common_driver', ROOT / 'benchmark-economic-runtime.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.wait4_process


def run_one(item, home, wait4):
    row = {**item, 'status': 'FAILED', 'correct': False, 'error': None,
           'failure_phase': 'launch', 'failure_reason': None, 'failure_errno': None,
           'fixture_known': expected(item['case']), 'actual_fixture': None,
           'process_observation': None, 'barrier_observed_processes': None,
           'known_identities_not_observed': None, 'exit_code': None,
           'workload_wait4_user_ms': None, 'workload_wait4_system_ms': None,
           'external_process_elapsed_ms': None, 'external_active_elapsed_ms': None,
           'barrier_hold_ms': None, 'controller_cpu_ms': None}
    process = sampler = usage = None
    started, cpu_started = time.monotonic_ns(), time.process_time_ns()
    active_started = None
    try:
        process = subprocess.Popen([sys.executable, '-I', str(Path(__file__).resolve()),
                                    '--worker', 'coordinator', '--case', item['case']],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, env=private_env(home),
                                   start_new_session=True)
        deadline = started / 1e9 + ATTEMPT_SECONDS
        row['failure_phase'] = 'ready'
        ready = read_message(process.stdout.fileno(), deadline)
        if ready != {'kind': 'ready', 'ready_children': expected(item['case'])['ready_children']}:
            raise ProtocolFailure('ready_mismatch')
        barrier_started = time.monotonic_ns()
        if item['arm'] == 'ON':
            row['failure_phase'] = 'sample'
            sampler = ProcessSampler(process.pid)
            sampler.tick()
            row['barrier_observed_processes'] = sampler.result()['sampled_process_count_peak']
        row['failure_phase'] = 'release'
        active_started = time.monotonic_ns()
        process.stdin.write(b'G'); process.stdin.close()
        row['barrier_hold_ms'] = (active_started - barrier_started) / 1e6
        row['failure_phase'] = 'wait4'
        code, usage = wait4(process, max(0, deadline - time.monotonic()), sampler)
        row['exit_code'] = code
        row['failure_phase'] = 'done' if code == 0 else 'wait4'
        done = read_message(process.stdout.fileno(), time.monotonic() + 0.5)
        known = expected(item['case'])
        keys = ('started_children', 'completed_children', 'child_failures', 'checksum', 'touched_pages')
        row['failure_phase'] = 'oracle' if code == 0 else 'wait4'
        if code != 0 or done.get('kind') != 'done' or any(type(done.get(k)) is not int or done[k] != known[k] for k in keys):
            raise ProtocolFailure('oracle_mismatch')
        row['actual_fixture'] = {k: done[k] for k in keys + ('reported_coordinator_cpu_ms', 'reported_waited_children_cpu_ms')}
        row['status'], row['correct'], row['failure_phase'] = 'PASS', True, None
    except (OSError, ValueError, TimeoutError, subprocess.TimeoutExpired) as error:
        row['error'] = 'fixture_or_protocol_failed'
        row['failure_reason'], row['failure_errno'] = failure_details(error, row['exit_code'])
    finally:
        if process is not None and process.returncode is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            _, status, usage = os.wait4(process.pid, 0)
            process.returncode = os.waitstatus_to_exitcode(status)
            row['exit_code'] = process.returncode
        if process is not None:
            for pipe in (process.stdin, process.stdout):
                if pipe is not None and not pipe.closed:
                    pipe.close()
        now = time.monotonic_ns()
        row['external_process_elapsed_ms'] = (now - started) / 1e6
        row['external_active_elapsed_ms'] = (now - active_started) / 1e6 if active_started is not None else None
        row['controller_cpu_ms'] = (time.process_time_ns() - cpu_started) / 1e6
        if usage is not None:
            row['workload_wait4_user_ms'] = usage.ru_utime * 1000
            row['workload_wait4_system_ms'] = usage.ru_stime * 1000
        if sampler is not None:
            row['process_observation'] = sampler.result()
            observed = row['process_observation']['observed_identity_count']
            if observed is not None and row['correct']:
                row['known_identities_not_observed'] = row['fixture_known']['known_total_processes'] - observed
    return row


def write_new(path, value):
    body = (json.dumps(value, indent=2, allow_nan=False) + '\n').encode()
    if len(body) > 32768:
        raise ValueError('bounded receipt')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(body)


def sources():
    names = ('benchmark_sampler_calibration.py', 'benchmark_process_sampler.py',
             'benchmark-economic-runtime.py', 'benchmark_catalog.py',
             'test_benchmark_sampler_calibration.py')
    return {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in names}


def main(output):
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    frozen = sources()
    workload_plan = plan()
    manifest = {'schema': 1, 'condition': 'offline-process-sampler-calibration-v1',
                'sources_sha256': frozen, 'python_binary_sha256': hashlib.sha256(Path(sys.executable).read_bytes()).hexdigest(),
                'python_version': sys.version.split()[0], 'plan': workload_plan,
                'limits': {'attempt_seconds': ATTEMPT_SECONDS, 'total_seconds': TOTAL_SECONDS,
                           'per_workload_process_address_space_bytes': 128 << 20, 'child_max_cpu_seconds': 8,
                           'touched_bytes_per_child': MEMORY_BYTES, 'memory_children': MEMORY_CHILDREN},
                'sampler_caps': {k: getattr(ProcessSampler, k) for k in ('INTERVAL', 'WINDOW_SECONDS', 'MAX_PROCESSES', 'MAX_THREADS', 'MAX_READS', 'MAX_SAMPLES', 'MAX_IDENTITIES')},
                'scope': 'offline known subprocess trees; initial sample after READY barrier, then common driver10ms wait4 polling',
                'provenance': 'wait4 owned coordinator plus reaped descendants; controller process CPU; sampler tick CPU/wall subset; no whole-host accounting',
                'rss': 'sampled kernel approximate sum includes shared pages; no exact or unique physical peak',
                'model_calls': 0, 'economy': 'NOT_EVALUATED', 'mechanical_subtraction': False}
    write_new(output / 'freeze.json', manifest)
    started, rows, wait4 = time.monotonic(), [], waiter()
    for item in workload_plan:
        if sources() != frozen or time.monotonic() - started > TOTAL_SECONDS - ATTEMPT_SECONDS - 1:
            row = {**item, 'status': 'NOT_RUN', 'correct': False, 'error': 'source_changed_or_total_budget', 'failure_phase': None, 'failure_reason': None, 'failure_errno': None}
        else:
            with tempfile.TemporaryDirectory(prefix='sampler-offline-', dir=output) as home:
                row = run_one(item, Path(home), wait4)
        rows.append(row)
        write_new(output / ('row-%02d.json' % item['ordinal']), row)
    available = sum(r.get('process_observation', {}).get('status') == 'partial' for r in rows if r.get('process_observation') is not None)
    summary = {'schema': 1, 'condition': manifest['condition'], 'planned_rows': len(workload_plan),
               'actual_rows': len(rows), 'correct_rows': sum(r['correct'] for r in rows),
               'status': 'PASS_OFFLINE_FIXTURES' if all(r['correct'] for r in rows) and available == len(workload_plan) // 2 else 'INCOMPLETE',
               'observation_rows_available': available,
               'source_freeze_verified_after': sources() == frozen,
               'elapsed_seconds': time.monotonic() - started,
               'model_calls': 0, 'economy': 'NOT_EVALUATED', 'actual_native_workers': None,
               'limits': 'single shared host; sample coverage always partial/non-atomic; no LLM benefit or subtractable correction factor'}
    write_new(output / 'summary.json', summary)
    print(json.dumps(summary))
    return 0 if summary['status'] == 'PASS_OFFLINE_FIXTURES' and summary['source_freeze_verified_after'] else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--worker', choices=('coordinator', 'memory_child', 'churn_child'))
    parser.add_argument('--case', choices=CASES)
    args = parser.parse_args()
    if args.worker:
        if args.worker == 'coordinator' and args.case is None:
            parser.error('coordinator case required')
        sys.exit(worker(args.worker, args.case))
    if args.output is None or not args.output.is_absolute() or not sys.platform.startswith('linux') or not hasattr(os, 'wait4'):
        parser.error('new absolute output and Linux wait4/procfs required')
    sys.exit(main(args.output))
