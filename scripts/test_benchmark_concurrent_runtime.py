"""SYNTHETIC owned /bin/sleep clients; no provider, auth or remote concurrency."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import threading
import time
import sys
from types import SimpleNamespace
import unittest
from unittest import mock

import benchmark_catalog as catalog
import benchmark_concurrent_contract as contract
import benchmark_concurrent_runtime as runtime
from test_benchmark_economic_runtime import valid_row

SPEC = importlib.util.spec_from_file_location("concurrent_fixture_driver", Path(__file__).with_name("benchmark-economic-runtime.py"))
DRIVER = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = DRIVER
SPEC.loader.exec_module(DRIVER)


class SyntheticConcurrentRuntimeTests(unittest.TestCase):
    def fixture(self, mode="valid"):
        temporary = tempfile.TemporaryDirectory(prefix="ivoai-synthetic-client-pair-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        harness = root / "synthetic-harness"
        capabilities = catalog.expected_capabilities(catalog.ROOT / "internal/app/primary_prefetch_live_test.go")
        capabilities.update(schema_version=4, client_lifecycle_sha256=contract.client_lifecycle_digest())
        harness.write_text("#!/usr/bin/python3\nimport os,sys,time,json,hashlib,subprocess\nfrom pathlib import Path\n"
            f"mode={mode!r}\ncapabilities={capabilities!r}\nrow={valid_row()!r}\n"
            "if any('TestPrimaryPrefetchBenchmarkCatalog' in a for a in sys.argv):\n"
            " if mode=='old_discovery': capabilities['schema_version']=3;capabilities.pop('client_lifecycle_sha256')\n"
            " p=Path(os.environ['IVOAI_BENCHMARK_CATALOG_OUTPUT']); p.write_text(json.dumps(capabilities));sys.exit(0)\n"
            "scenario=next(a for a in sys.argv if a.startswith('-test.run=')).split('/')[1]\n"
            "arm=os.environ['IVOAI_ECONOMIC_RUNTIME_ARM'];start=time.monotonic_ns()\n"
            "if mode=='sequential' and scenario=='dependencies':time.sleep(0.8)\n"
            "child=subprocess.Popen(['/bin/sleep','30' if mode=='cancel' else '0.6'],env={'PATH':'/usr/bin:/bin'})\n"
            "raw=Path(f'/proc/{child.pid}/stat').read_bytes();fields=raw[raw.rindex(b')')+1:].split()\n"
            "identity={'attempt_id':os.environ['IVOAI_CONCURRENT_ATTEMPT_ID'],'scenario':scenario,'arm':arm,\n"
            " 'fixture_sha256':capabilities['fixtures']['go-core' if scenario=='implementation' else 'python-deps'],\n"
            " 'harness_sha256':capabilities['harness_sha256'],'composition_sha256':" + repr(contract.contract_digest()) + ",\n"
            " 'client_sha256':os.environ['IVOAI_CONCURRENT_CLIENT_SHA256'],\n"
            " 'isolation_sha256':hashlib.sha256(os.environ['TMPDIR'].encode()).hexdigest()}\n"
            "value={'schema_version':1,'clock':'linux_clock_monotonic','identity':identity,'pid':child.pid,\n"
            " 'start_ticks':int(fields[19]),'started_ns':time.monotonic_ns(),'reaped_ns':0,'exit_code':-1}\n"
            "if mode=='stale':value['start_ticks']+=1\n"
            "if mode=='stale_clock':value['started_ns']=1\n"
            "if mode=='wrapper':value['pid']=os.getpid()\n"
            "if mode=='substitute':identity['attempt_id']='f'*32\n"
            "if mode=='same_scope':identity['isolation_sha256']='f'*64\n"
            "def save(path,data):\n"
            " fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)\n"
            " with os.fdopen(fd,'w') as f:json.dump(data,f)\n"
            "if mode!='missing':save(os.environ['IVOAI_CONCURRENT_CLIENT_START'],value)\n"
            "child.wait();value['reaped_ns']=time.monotonic_ns();value['exit_code']=child.returncode\n"
            "if mode=='zero':value['reaped_ns']=value['started_ns']\n"
            "row.update(Scenario=scenario,Arm=arm,CatalogSHA256=capabilities['catalog_sha256'],\n"
            " HarnessSHA256=capabilities['harness_sha256'],FixtureSHA256=identity['fixture_sha256'],ClientLifecycle=value,\n"
            " TotalMS=(time.monotonic_ns()-start)//1000000,ProviderMS=(time.monotonic_ns()-start)//1000000-1,PrefetchMS=0,ValidationMS=0)\n"
            "row['DistributionInventorySHA256']=hashlib.sha256((Path(os.environ['IVOAI_GRAFT_TEST_DIST'])/'files.json').read_bytes()).hexdigest()\n"
            "if mode=='one_fail' and scenario=='implementation':row['Correct']=False\n"
            "save(os.environ['IVOAI_GRAFT_PREFETCH_OUTPUT'],[5] if mode=='invalid_row' and scenario=='implementation' else [row])\n"
            "sys.exit(1 if mode=='one_fail' and scenario=='implementation' else 0)\n")
        harness.chmod(0o700)
        distribution = root / "distribution"; distribution.mkdir(); (distribution / "files.json").write_text("{}")
        args = SimpleNamespace(baseline=harness, candidate=harness, codex=Path("/bin/sleep"), distribution=distribution, sample_processes=False)
        private = root / "private"; private.mkdir(mode=0o700)
        env = {"PATH": "/usr/bin:/bin", "IVOAI_GRAFT_TEST_DIST": str(distribution)}
        return args, env, capabilities, private

    def test_synthetic_actual_client_overlap_and_public_rows_hide_pids(self):
        args, env, capabilities, private = self.fixture()
        row = runtime.run_pair(DRIVER, args, "A", 0, env, capabilities, private)
        self.assertTrue(row["correct"], row)
        self.assertEqual(row["client_overlap"], "OBSERVED_CLIENT_LIFECYCLE")
        self.assertEqual(row["overlap_receipt"]["kind"], "owned_client_pidfd_A_B_A_live_bracket")
        self.assertIsNone(row["batch_peak_rss_bytes"])
        self.assertEqual(row["scenario_status"], "UNSUPPORTED")
        self.assertEqual(row["remote_inference_overlap"], "NOT_ATTESTED")
        self.assertEqual(len(row["children"]), 2)
        serialized = json.dumps(row)
        self.assertNotIn('"pid"', serialized); self.assertNotIn('"start_ticks"', serialized)
        self.assertFalse(list(private.rglob("client-start.json")))
        self.assertFalse(list(private.rglob("row.json")))

    def test_synthetic_one_failed_child_keeps_both_results(self):
        args, env, capabilities, private = self.fixture("one_fail")
        row = runtime.run_pair(DRIVER, args, "B", 1, env, capabilities, private)
        self.assertFalse(row["correct"])
        self.assertEqual([child["Correct"] for child in row["children"]], [False, True])
        self.assertEqual(row["client_overlap"], "OBSERVED_CLIENT_LIFECYCLE")

    def test_synthetic_malformed_row_preserves_other_child_and_cleans_private_pid_files(self):
        args,env,capabilities,private=self.fixture('invalid_row')
        row=runtime.run_pair(DRIVER,args,'A',0,env,capabilities,private)
        self.assertFalse(row['correct'])
        self.assertEqual([child['Correct'] for child in row['children']],[False,True])
        self.assertIsNone(row['child_total_ms_sum'])
        self.assertFalse(list(private.rglob('row.json')))

    def test_synthetic_missing_zero_stale_wrapper_substitution_and_same_scope_fail(self):
        for mode in ("missing", "zero", "stale", "stale_clock", "wrapper", "substitute", "same_scope", "sequential"):
            with self.subTest(mode=mode):
                args, env, capabilities, private = self.fixture(mode)
                row = runtime.run_pair(DRIVER, args, "A", 0, env, capabilities, private)
                self.assertFalse(row["correct"])
                self.assertEqual(len(row["children"]), 2)
                if mode != "zero":
                    self.assertEqual(row["client_overlap"], "UNVERIFIED")

    def test_synthetic_cancel_reaps_two_owned_groups_without_touching_unrelated_process(self):
        import subprocess
        unrelated = subprocess.Popen(["/bin/sleep", "5"])
        self.addCleanup(lambda: (unrelated.terminate(), unrelated.wait()) if unrelated.poll() is None else None)
        args, env, capabilities, private = self.fixture("cancel")
        cancel = threading.Event(); timer = threading.Timer(0.2, cancel.set);timer.start()
        try:
            started=time.monotonic()
            row = runtime.run_pair(DRIVER, args, "A", 0, env, capabilities, private, cancel)
        finally:
            timer.cancel()
        self.assertLess(time.monotonic()-started, 3)
        self.assertTrue(row["cancelled"]); self.assertFalse(row["correct"])
        self.assertEqual([c["TestExitCode"] for c in row["children"]], [130,130])
        self.assertIsNone(unrelated.poll())

    def test_A_B_A_bracket_rejects_exit_between_samples(self):
        starts=[{"pid":1,"started_ns":1,"identity":{"isolation_sha256":"a"*64}},
                {"pid":2,"started_ns":1,"identity":{"isolation_sha256":"b"*64}}]
        reader,writer=os.pipe()
        try:
            with mock.patch.object(os,"pidfd_open",side_effect=lambda *_:os.dup(reader)),mock.patch.object(runtime,"live_client",side_effect=[True,True,False]) as live:
                self.assertIsNone(runtime.observe_bracket(starts,[10,20],(1,2)))
                self.assertEqual(live.call_count,3)
        finally:os.close(reader);os.close(writer)

    def test_pidfd_exit_poll_rejects_lifetime_end_despite_live_metadata(self):
        starts=[{"pid":1,"started_ns":1,"identity":{"isolation_sha256":"a"*64}},
                {"pid":2,"started_ns":1,"identity":{"isolation_sha256":"b"*64}}]
        reader,writer=os.pipe()
        calls=[]
        def live(*_):
            calls.append(1)
            if len(calls)==2:os.write(writer,b'x')
            return True
        try:
            with mock.patch.object(os,"pidfd_open",side_effect=lambda *_:os.dup(reader)),mock.patch.object(runtime,"live_client",side_effect=live):
                self.assertIsNone(runtime.observe_bracket(starts,[10,20],(1,2)))
        finally:os.close(reader);os.close(writer)

    def test_pidfd_unavailable_never_launches_clients(self):
        args,env,capabilities,private=self.fixture()
        with mock.patch.object(os,'pidfd_open',side_effect=OSError('synthetic unavailable')),mock.patch.object(DRIVER,'run_attempt') as launch:
            row=runtime.run_pair(DRIVER,args,'A',0,env,capabilities,private)
            launch.assert_not_called()
        self.assertEqual(row['client_execution'],'NOT_RUN')
        self.assertEqual(row['scenario_status'],'UNSUPPORTED')
        self.assertEqual(len(row['children']),2)

    def test_cancelled_before_launch_keeps_both_not_run(self):
        args,env,capabilities,private=self.fixture();cancel=threading.Event();cancel.set()
        with mock.patch.object(DRIVER,'run_attempt') as launch:
            row=runtime.run_pair(DRIVER,args,'A',0,env,capabilities,private,cancel)
            launch.assert_not_called()
        self.assertTrue(row['cancelled'])
        self.assertEqual(row['client_execution'],'NOT_RUN')
        self.assertEqual(len(row['children']),2)

    def test_missing_source_records_blocked_without_client(self):
        args,_,_,private=self.fixture();args.harness=private/'missing.go';args.output=private.parent/'blocked'
        with mock.patch.object(DRIVER,'run_attempt') as launch:
            self.assertEqual(runtime.run(args,DRIVER),1)
            launch.assert_not_called()
        report=json.loads((args.output/'preflight.json').read_bytes())
        self.assertEqual(report['execution_admission'],'BLOCKED')
        self.assertEqual(report['client_execution'],'NOT_RUN')

    def run_main(self, mode="valid", repetitions=1, preflight=False):
        args, _, _, private = self.fixture(mode)
        output=private.parent/'public-evidence'
        argv=['--concurrent-projects','--baseline',str(args.baseline),'--candidate',str(args.candidate),
              '--codex',str(args.codex),'--node','/bin/true','--sandbox-binary','/bin/true',
              '--distribution',str(args.distribution),'--output',str(output),'--repetitions',str(repetitions)]
        if preflight: argv.append('--preflight-only')
        # Only oracle admission is stubbed here; separate tests execute the real
        # Go/Python oracles. All process launch/observation/rows are real fixtures.
        with mock.patch.object(contract,'preflight',return_value={'preflight_status':'OFFLINE_ORACLES_VERIFIED','scope':'synthetic_test_admission_stub','provider_attempts':0}):
            code=DRIVER.main(argv)
        return code,output

    def test_synthetic_end_to_end_driver_preserves_AB_BA_and_two_children(self):
        code,output=self.run_main(repetitions=2)
        self.assertEqual(code,0)
        rows=json.loads((output/'runtime-comparison.json').read_bytes())
        self.assertEqual([row['arm'] for row in rows],['A','B','B','A'])
        self.assertTrue(all(row['correct'] and len(row['children'])==2 for row in rows))
        manifest=json.loads((output/'manifest.json').read_bytes())
        self.assertEqual(manifest['scenario_status'],'UNSUPPORTED')
        self.assertEqual(manifest['global_economic_gate'],'INCOMPLETE')
        self.assertNotIn('"pid"',json.dumps(rows))

    def test_old_discovery_and_preflight_only_never_start_clients(self):
        code,output=self.run_main(mode='old_discovery')
        self.assertEqual(code,1)
        self.assertEqual(json.loads((output/'runtime-comparison.json').read_bytes()),[])
        with mock.patch.object(runtime,'run_pair') as pair:
            code,output=self.run_main(preflight=True)
            pair.assert_not_called()
        self.assertEqual(code,0)
        report=json.loads((output/'preflight.json').read_bytes())
        self.assertEqual(report['client_execution'],'NOT_RUN')
        self.assertEqual(report['scenario_status'],'UNSUPPORTED')

    def test_observed_wait4_fallback_cancellation_reaps_own_group(self):
        cancel=threading.Event();timer=threading.Timer(0.1,cancel.set);timer.start()
        original=Path.is_file
        with tempfile.TemporaryDirectory() as temporary, mock.patch.object(Path,'is_file',lambda path: False if path==Path('/usr/bin/time') else original(path)):
            try:
                code,_=DRIVER.run_attempt(['/bin/sleep','30'],{'PATH':'/usr/bin:/bin'},Path(temporary)/'resources.json',cancel_event=cancel)
            finally:timer.cancel()
        self.assertEqual(code,130)

    def test_on_start_exception_reaps_owned_process(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaises(RuntimeError):
                DRIVER.run_attempt(['/bin/sleep','30'],{'PATH':'/usr/bin:/bin'},Path(temporary)/'resources.json',on_start=lambda _: (_ for _ in ()).throw(RuntimeError('synthetic start callback failure')))

    def complete_args(self,args,private,repetitions=1):
        args.node=Path('/bin/true');args.sandbox_binary=Path('/bin/true');args.oracle_go=None
        args.harness=catalog.ROOT/'internal/app/primary_prefetch_live_test.go'
        args.output=private.parent/'cohort-output';args.repetitions=repetitions;args.preflight_only=False
        return args

    def test_cohort_executable_change_preserves_completed_children_and_blocks_remaining_without_retry(self):
        args,_,_,private=self.fixture();self.complete_args(args,private,2)
        original=runtime.run_pair;calls=[];initial=DRIVER.sha256(args.baseline)
        def replace_after_pair(*pos,**kw):
            row=original(*pos,**kw);calls.append(1)
            with args.baseline.open('a') as stream:stream.write('\n# benign synthetic harness replacement\n')
            return row
        with mock.patch.object(contract,'preflight',return_value={'preflight_status':'OFFLINE_ORACLES_VERIFIED','scope':'synthetic_test_admission_stub'}),mock.patch.object(runtime,'run_pair',side_effect=replace_after_pair):
            code=runtime.run(args,DRIVER)
        rows=json.loads((args.output/'runtime-comparison.json').read_bytes())
        self.assertEqual(code,1);self.assertEqual(len(calls),1)
        self.assertEqual([child['Correct'] for child in rows[0]['children']],[True,True])
        self.assertTrue(all(row['client_execution']=='NOT_RUN' and len(row['children'])==2 for row in rows[1:]))
        self.assertEqual([row['arm'] for row in rows],['A','B','B','A'])
        manifest=json.loads((args.output/'manifest.json').read_bytes())
        self.assertEqual(manifest['baseline_sha256'],initial)
        self.assertNotEqual(initial,DRIVER.sha256(args.baseline))
        self.assertFalse(json.loads((args.output/'cohort-final.json').read_bytes())['cohort_pins_stable'])

    def test_cohort_final_check_preserves_completed_rows_but_fails_changed_source(self):
        args,_,_,private=self.fixture();self.complete_args(args,private)
        original=DRIVER.write_json
        def replace_at_finish(path,value):
            original(path,value)
            if path.name=='runtime-comparison.json' and len(value)==2:
                with (args.distribution/'files.json').open('a') as stream:stream.write(' ')
        with mock.patch.object(contract,'preflight',return_value={'preflight_status':'OFFLINE_ORACLES_VERIFIED','scope':'synthetic_test_admission_stub'}),mock.patch.object(DRIVER,'write_json',side_effect=replace_at_finish):
            self.assertEqual(runtime.run(args,DRIVER),1)
        rows=json.loads((args.output/'runtime-comparison.json').read_bytes())
        self.assertTrue(all(row['correct'] for row in rows))
        final=json.loads((args.output/'cohort-final.json').read_bytes())
        self.assertFalse(final['cohort_pins_stable']);self.assertFalse(final['correct'])

    def test_detected_pair_pin_change_is_sticky_even_if_current_files_match_again(self):
        args,_,_,private=self.fixture();self.complete_args(args,private)
        original=runtime.run_pair;calls=[]
        def detected_change(*pos,**kw):
            row=original(*pos,**kw);calls.append(1);row['cohort_pins_stable']=False
            return row
        with mock.patch.object(contract,'preflight',return_value={'preflight_status':'OFFLINE_ORACLES_VERIFIED','scope':'synthetic_test_admission_stub'}),mock.patch.object(runtime,'run_pair',side_effect=detected_change):
            self.assertEqual(runtime.run(args,DRIVER),1)
        rows=json.loads((args.output/'runtime-comparison.json').read_bytes())
        self.assertEqual(len(calls),1)
        self.assertEqual(rows[1]['client_execution'],'NOT_RUN')
        self.assertFalse(json.loads((args.output/'cohort-final.json').read_bytes())['cohort_pins_stable'])

    def test_cohort_pins_bind_executables_source_contract_oracles_and_distribution(self):
        args,_,_,private=self.fixture();self.complete_args(args,private)
        initial=runtime.capture_cohort(args,DRIVER)
        required={'baseline','candidate','client','node','sandbox','oracle_go','oracle_python','resource_timer',
                  'driver','composite_driver','catalog_loader','composition_loader','harness','distribution_inventory',
                  'catalog','composition','go_mod','go_sum','concurrent.go','fixtures.go','edit_oracle.go','exec_observation.go',
                  'client_lifecycle.go','client_lifecycle_linux.go','client_lifecycle_other.go'}
        self.assertEqual(set(initial['files']),required)
        for name in required:
            changed=dict(initial);changed['files']=dict(initial['files']);changed['files'][name]='f'*64
            with self.subTest(pin=name),mock.patch.object(runtime,'capture_cohort',return_value=changed):
                self.assertFalse(runtime.cohort_unchanged(initial,args,DRIVER))


if __name__ == "__main__":
    unittest.main()
