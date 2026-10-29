"""Opt-in bounded Linux procfs observations; no argv/env, daemon, or raw PID output."""
import os
from pathlib import Path
import stat
import time


class ProcessSampler:
    INTERVAL = 0.1
    MAX_PROCESSES = 128
    MAX_THREADS = 128
    MAX_READS = 512
    MAX_SAMPLES = 3000
    MAX_IDENTITIES = 4096
    WINDOW_SECONDS = 0.025

    def __init__(self, pid, proc_root=Path('/proc'), clock=time.monotonic):
        self.pid, self.proc, self.clock = pid, proc_root, clock
        self.next_tick = 0.0
        self.root_identity = None
        self.identities = set()
        self.samples = self.partial_samples = self.read_failures = 0
        self.first_tick = self.last_tick = None
        self.limit_reached = False
        self.peak_processes = self.peak_rss = None
        self.wall_seconds = self.cpu_seconds = self.max_window = 0.0
        self.page_size = os.sysconf('SC_PAGE_SIZE')

    def _read(self, path):
        self.reads += 1
        if self.reads > self.MAX_READS or self.clock() - self.started > self.WINDOW_SECONDS:
            self.limit_reached = True
            raise OSError('bounded observation exhausted')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
        with os.fdopen(fd, 'rb') as stream:
            if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
                raise OSError('nonregular procfs input')
            body = stream.read(8193)
        if len(body) > 8192:
            raise OSError('oversized procfs input')
        return body

    def _process(self, pid):
        body = self._read(self.proc / str(pid) / 'stat')
        prefix, separator, rest = body.rpartition(b')')
        if not separator or prefix.split(b'(', 1)[0].strip() != str(pid).encode():
            raise ValueError('invalid procfs identity')
        fields = rest.split()
        if len(fields) < 22:
            raise ValueError('incomplete procfs observation')
        ppid, start, rss = (int(fields[index]) for index in (1, 19, 21))
        if min(ppid, start, rss) < 0 or rss > ((1 << 53) - 1) // self.page_size:
            raise ValueError('invalid procfs numeric observation')
        return ppid, start, rss * self.page_size

    def _children(self, pid):
        children, threads = set(), 0
        with os.scandir(self.proc / str(pid) / 'task') as tasks:
            for task in tasks:
                if not task.name.isdecimal():
                    continue
                threads += 1
                if threads > self.MAX_THREADS:
                    self.limit_reached = True
                    break
                # Linux children belong to individual tasks, not just the
                # process leader. Inspect bounded task children for Go/Rust.
                body = self._read(Path(task.path) / 'children')
                for field in body.split():
                    child = int(field)
                    if child <= 0:
                        raise ValueError('invalid child identity')
                    children.add(child)
                    if len(children) > self.MAX_PROCESSES:
                        self.limit_reached = True
                        return children
        return children

    def tick(self):
        now = self.clock()
        if now < self.next_tick or self.samples >= self.MAX_SAMPLES:
            if self.samples >= self.MAX_SAMPLES:
                self.limit_reached = True
            return
        self.next_tick = now + self.INTERVAL
        if self.first_tick is None:
            self.first_tick = now
        self.last_tick = now
        cpu_started = time.process_time()
        self.started, self.reads = now, 0
        count, rss, partial = 0, 0, False
        pending, seen = [(self.pid, None)], set()
        while pending:
            if len(seen) >= self.MAX_PROCESSES:
                self.limit_reached, partial = True, True
                break
            pid, parent = pending.pop()
            if pid in seen:
                continue
            seen.add(pid)
            try:
                ppid, start, memory = self._process(pid)
                if parent is None:
                    if self.root_identity is None:
                        self.root_identity = start
                    if self.root_identity != start:
                        raise ValueError('root identity changed')
                elif ppid != parent:
                    raise ValueError('descendant reparented or identity raced')
                # Re-read after children traversal to avoid accepting PID reuse
                # or reparenting during this non-atomic observation window.
                descendants = self._children(pid)
                if self._process(pid)[:2] != (ppid, start):
                    raise ValueError('process changed during observation')
                if rss + memory > (1 << 53) - 1:
                    raise ValueError('aggregate RSS exceeds exact numeric range')
                count, rss = count + 1, rss + memory
                if len(self.identities) < self.MAX_IDENTITIES:
                    self.identities.add((pid, start))
                else:
                    self.limit_reached, partial = True, True
                pending.extend((child, pid) for child in descendants)
            except (OSError, ValueError):
                self.read_failures += 1
                partial = True
        duration = self.clock() - now
        self.samples += 1
        self.partial_samples += int(partial or self.limit_reached or duration > self.WINDOW_SECONDS)
        self.wall_seconds += duration
        self.cpu_seconds += time.process_time() - cpu_started
        self.max_window = max(self.max_window, duration)
        if count:
            self.peak_processes = max(self.peak_processes or 0, count)
            self.peak_rss = max(self.peak_rss or 0, rss)

    def result(self):
        observed = self.peak_processes is not None
        return {'schema_version': 1, 'source': 'linux_procfs_bounded_task_children_stat',
                'status': 'partial' if observed else 'unavailable',
                'coverage': 'sampled_root_and_descendants_not_complete_or_atomic',
                'read_loss_or_cap_detected': bool(self.partial_samples or self.limit_reached),
                'sample_count': self.samples, 'partial_samples': self.partial_samples,
                'read_failures': self.read_failures, 'limit_reached': self.limit_reached,
                'interval_ms': 100, 'window_budget_ms': 25,
                'observed_span_ms': round((self.last_tick - self.first_tick) * 1000, 3) if self.first_tick is not None else None,
                'max_window_ms': round(self.max_window * 1000, 3),
                'observer_wall_ms': round(self.wall_seconds * 1000, 3),
                'observer_cpu_ms': round(self.cpu_seconds * 1000, 3),
                'sampled_process_count_peak': self.peak_processes,
                'sampled_rss_sum_peak_bytes': self.peak_rss,
                'observed_identity_count': len(self.identities) if observed else None,
                'rss_semantics': 'kernel_approximate_sampled_rss_sum_includes_shared_pages_not_unique_physical_memory',
                'actual_process_total': None, 'exact_peak_rss': None}
