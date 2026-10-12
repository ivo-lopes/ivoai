#!/usr/bin/env python3
"""ABI compatibility regression tests, without loading native code."""
import importlib.util
import pathlib
import unittest
ROOT = pathlib.Path(__file__).resolve().parents[1]

class ABITests(unittest.TestCase):
    def setUp(self):
        helper = ROOT/'patches/graft/check-abi.py'
        self.assertTrue(helper.is_file(), 'compatibility gate is missing')
        spec = importlib.util.spec_from_file_location('graft_abi', helper)
        self.abi = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.abi)

    def test_compatible_requirements_are_recorded(self):
        self.assertEqual(self.abi.check_versions('Name: GLIBC_2.17\nName: GLIBC_2.35\nName: GLIBCXX_3.4.30\nName: CXXABI_1.3.13'), {'GLIBC': '2.35', 'GLIBCXX': '3.4.30', 'CXXABI': '1.3.13'})

    def test_host_gcc14_requirements_are_rejected(self):
        for version in ('GLIBCXX_3.4.31', 'CXXABI_1.3.15', 'GLIBC_2.36', 'GLIBC_PRIVATE', 'GLIBC_ABI_DT_RELR'):
            with self.subTest(version=version), self.assertRaises(ValueError):
                self.abi.check_versions('Name: '+version)

    def test_numeric_versions_are_not_compared_lexicographically(self):
        self.assertEqual(self.abi.check_versions('Name: GLIBC_2.9\nName: GLIBC_2.17'), {'GLIBC': '2.17'})

if __name__ == '__main__': unittest.main()
