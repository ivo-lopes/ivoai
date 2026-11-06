"""Run the byte-preserved v0.16 bundle adversarial tests as a separate contract."""
import hashlib
import importlib.util
from pathlib import Path
import unittest

BASELINE = Path(__file__).parent / 'release-baselines' / 'v0.16.0'
PINS = {'verify-release-artifacts.py':'d26f0ff631ad02f30f47da42ffcd451a29d8c6433899e64d483bb3e3269bdcca',
        'test_release_artifacts.py':'d38f3094f17687f2199284979044d12f04b51b63351cd46c194ceb4d6adde36f'}

def load_tests(loader, tests, pattern):
    for name,digest in PINS.items():
        if hashlib.sha256((BASELINE/name).read_bytes()).hexdigest()!=digest:
            raise RuntimeError('historical v0.16 verifier/test bytes changed')
    spec=importlib.util.spec_from_file_location('legacy_v016_release_tests',BASELINE/'test_release_artifacts.py')
    module=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return loader.loadTestsFromModule(module)
