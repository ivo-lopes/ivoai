"""Local procfs fixtures/processes only; no clients, credentials or inference."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

from benchmark_process_sampler import ProcessSampler


class SamplerTests(unittest.TestCase):
    def fixture(self):
        temporary = tempfile.TemporaryDirectory(); self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        def process(pid, ppid, start, rss, children=(), tid=None):
            directory=root/str(pid);directory.mkdir(exist_ok=True)
            fields=['S',str(ppid)]+['0']*17+[str(start),'100',str(rss)]
            (directory/'stat').write_text(f'{pid} (PRIVATE_COMMAND_WITH_)_SECRET) '+ ' '.join(fields))
            task=directory/'task'/str(tid or pid);task.mkdir(parents=True,exist_ok=True)
            (task/'children').write_text(' '.join(map(str,children)))
        return root,process

    def test_nonleader_children_identity_and_payload_privacy(self):
        root,add=self.fixture();add(100,1,10,3,(200,),tid=101);add(200,100,20,5)
        sampler=ProcessSampler(100,root);sampler.tick();value=sampler.result()
        self.assertEqual(value['sampled_process_count_peak'],2)
        self.assertEqual(value['sampled_rss_sum_peak_bytes'],8*os.sysconf('SC_PAGE_SIZE'))
        self.assertEqual(value['observed_identity_count'],2)
        self.assertIsNone(value['actual_process_total']);self.assertIsNone(value['exact_peak_rss'])
        for secret in ('PRIVATE','SECRET','100','200'):
            # Fixed interval100 is public; identifiers/command text are absent as keys/values.
            if secret.isdecimal(): continue
            self.assertNotIn(secret,json.dumps(value))
        add(100,1,999,999);sampler.next_tick=0;sampler.tick()
        self.assertEqual(sampler.result()['sampled_rss_sum_peak_bytes'],8*os.sysconf('SC_PAGE_SIZE'))
        self.assertEqual(sampler.result()['status'],'partial')

    def test_limits_missing_reparented_and_nonregular_are_not_full_coverage(self):
        root,add=self.fixture();add(100,1,10,3,(200,));add(200,99,20,500)
        sampler=ProcessSampler(100,root);sampler.tick()
        self.assertEqual(sampler.result()['sampled_process_count_peak'],1)
        self.assertEqual(sampler.result()['status'],'partial')
        missing=ProcessSampler(999,root);missing.tick()
        self.assertEqual(missing.result()['status'],'unavailable')
        self.assertIsNone(missing.result()['sampled_process_count_peak'])
        (root/'100/stat').unlink();os.mkfifo(root/'100/stat')
        before=time.monotonic();fifo=ProcessSampler(100,root);fifo.tick()
        self.assertLess(time.monotonic()-before,1)
        self.assertEqual(fifo.result()['status'],'unavailable')
        (root/'100/stat').unlink();add(100,1,10,3,(200,));add(200,100,20,5)
        capped=ProcessSampler(100,root);capped.MAX_PROCESSES=1;capped.tick()
        self.assertTrue(capped.result()['limit_reached']);self.assertEqual(capped.result()['status'],'partial')
        bounded=ProcessSampler(100,root);bounded.MAX_READS=1;bounded.tick()
        self.assertTrue(bounded.result()['limit_reached']);self.assertIsNone(bounded.result()['sampled_rss_sum_peak_bytes'])

    @unittest.skipUnless(sys.platform.startswith('linux'),'Linux procfs observation')
    def test_real_parent_and_child_memory_are_only_sampled_rss_sum(self):
        child='import time; data=bytearray(8*1024*1024); print("READY",flush=True); time.sleep(1)'
        parent='import subprocess,sys; data=bytearray(8*1024*1024); child=subprocess.Popen([sys.executable,"-c",'+repr(child)+'],stdout=subprocess.PIPE); child.stdout.readline(); print("READY",flush=True); child.wait()'
        proc=subprocess.Popen([sys.executable,'-c',parent],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
        self.addCleanup(lambda: proc.kill() if proc.poll() is None else None)
        self.assertEqual(proc.stdout.readline(),b'READY\n')
        sampler=ProcessSampler(proc.pid);sampler.tick();value=sampler.result()
        self.assertGreaterEqual(value['sampled_process_count_peak'],2)
        self.assertGreaterEqual(value['sampled_rss_sum_peak_bytes'],16*1024*1024)
        self.assertEqual(value['coverage'],'sampled_root_and_descendants_not_complete_or_atomic')
        proc.wait(timeout=5);proc.stdout.close()


if __name__=='__main__':unittest.main()
