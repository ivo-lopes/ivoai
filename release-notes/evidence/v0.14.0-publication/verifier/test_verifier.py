"""Synthetic utility tests; never count these as real release verification."""
import base64
import copy
import hashlib
import gzip
import importlib.util
import io
import json
from pathlib import Path
import re
import stat
import struct
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

HERE = Path(__file__).parent
spec = importlib.util.spec_from_file_location('verifier', HERE / 'verify_release014.py')
v = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v)
SOURCE = Path('/home/ivo/Downloads/PROJETOS/MINDSITE/ivoai-claude-readonly-workers')


class VerifierTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='synthetic-', dir=HERE)
        self.root = Path(self.temp.name)
        self.addCleanup(self.temp.cleanup)

    def assets(self):
        root = self.root / 'assets'
        root.mkdir()
        for name in v.EXPECTED:
            (root / name).write_bytes(name.encode())
        checksums = ''.join(v.digest(root / name) + '  ' + name + '\n'
                            for name in sorted(v.EXPECTED - {'checksums.txt'}))
        (root / 'checksums.txt').write_text(checksums)
        return root

    def tar(self, members):
        archive = self.root / 'core.tar.gz'
        with tarfile.open(archive, 'w:gz') as stream:
            for name, kind, payload in members:
                info = tarfile.TarInfo(name)
                info.type, info.mode = kind, 0o755
                if kind == tarfile.REGTYPE:
                    info.size = len(payload)
                    stream.addfile(info, io.BytesIO(payload))
                else:
                    info.linkname = 'outside'
                    stream.addfile(info)
        return archive

    def elf(self, arch='amd64'):
        data = bytearray(64)
        data[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<HH', data, 16, 2, {'amd64': 62, 'arm64': 183}[arch])
        return data

    def contract_info(self):
        requires = dict(re.findall(r'^\s+(\S+) (v\S+)', (SOURCE / 'go.mod').read_text(), re.M))
        sums = {}
        for line in (SOURCE / 'go.sum').read_text().splitlines():
            path, version, value = line.split()
            if not version.endswith('/go.mod'):
                sums[(path, version)] = value
        contract = {'module': 'github.com/ivo-lopes/ivoai', 'go_version': 'go1.27.3',
                    'source_commit': 'a' * 40, 'requires': requires, 'sums': sums}
        values = {'-buildmode': 'exe', '-compiler': 'gc', '-trimpath': 'true', 'CGO_ENABLED': '0',
                  'GOOS': 'linux', 'GOARCH': 'amd64', 'GOAMD64': 'v1', 'vcs': 'git',
                  'vcs.revision': 'a' * 40, 'vcs.modified': 'false'}
        info = {'GoVersion': 'go1.27.3', 'Path': 'github.com/ivo-lopes/ivoai/cmd/ivoai',
                'Main': {'Path': contract['module'], 'Version': 'v0.14.0', 'Replace': None},
                'Settings': [{'Key': key, 'Value': value} for key, value in values.items()],
                'Deps': [{'Path': path, 'Version': version, 'Sum': sums[(path, version)], 'Replace': None}
                         for path, version in requires.items()]}
        return contract, info

    def test_manifest_positive(self):
        self.assertEqual(len(v.manifest_hashes(self.assets())), 12)

    def test_manifest_duplicate(self):
        root = self.assets()
        with open(root / 'checksums.txt', 'a') as stream:
            stream.write((root / 'checksums.txt').read_text().splitlines()[0] + '\n')
        with self.assertRaises(ValueError): v.manifest_hashes(root)

    def test_manifest_traversal(self):
        root = self.assets()
        with open(root / 'checksums.txt', 'a') as stream: stream.write('a' * 64 + '  ../private\n')
        with self.assertRaises(ValueError): v.manifest_hashes(root)

    def test_manifest_symlink(self):
        root = self.assets()
        name = 'ivoai-memory-context.zip'
        (root / name).unlink()
        (root / name).symlink_to(root / 'checksums.txt')
        with self.assertRaises(ValueError): v.manifest_hashes(root)

    def test_safe_core_positive(self):
        archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf())])
        v.safe_core(archive, self.root / 'extracted', 'amd64')

    def test_safe_core_traversal(self):
        archive = self.tar([('../ivoai', tarfile.REGTYPE, self.elf())])
        with self.assertRaises(ValueError): v.safe_core(archive, self.root / 'extracted', 'amd64')

    def test_safe_core_link(self):
        archive = self.tar([('ivoai', tarfile.SYMTYPE, b'')])
        with self.assertRaises(ValueError): v.safe_core(archive, self.root / 'extracted', 'amd64')

    def test_safe_core_extra(self):
        archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf()), ('extra', tarfile.REGTYPE, b'x')])
        with self.assertRaises(ValueError): v.safe_core(archive, self.root / 'extracted', 'amd64')

    def test_safe_core_wrong_arch(self):
        archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf('arm64'))])
        with self.assertRaises(ValueError): v.safe_core(archive, self.root / 'extracted', 'amd64')

    def test_module_positive(self):
        contract, info = self.contract_info()
        self.assertEqual(len(v.verify_info(info, contract, 'amd64')['dependencies']), 12)

    def test_module_mutations(self):
        contract, info = self.contract_info()
        for mutation in ('dirty', 'source', 'version', 'sum', 'dependency', 'replace', 'cpu', 'duplicate'):
            with self.subTest(mutation=mutation):
                changed = copy.deepcopy(info)
                if mutation == 'dirty': changed['Settings'][-1]['Value'] = 'true'
                elif mutation == 'source': changed['Settings'][-2]['Value'] = 'b' * 40
                elif mutation == 'version': changed['Main']['Version'] = 'v0.13.0'
                elif mutation == 'sum': changed['Deps'][0]['Sum'] = 'h1:wrong'
                elif mutation == 'dependency': changed['Deps'][0]['Version'] = 'v0.0.0'
                elif mutation == 'replace': changed['Deps'][0]['Replace'] = {'Path': '/tmp/local'}
                elif mutation == 'cpu': changed['Settings'][6]['Value'] = 'v3'
                else: changed['Settings'].append(changed['Settings'][-1])
                with self.assertRaises(ValueError): v.verify_info(changed, contract, 'amd64')

    def test_api_all_digests(self):
        root = self.assets()
        hashes = v.manifest_hashes(root)
        api = {'tag_name': v.TAG, 'draft': False, 'assets': [{'name': name, 'size': (root / name).stat().st_size,
                'digest': 'sha256:' + hashes[name]} for name in sorted(v.EXPECTED)]}
        path = self.root / 'api.json'
        path.write_text(json.dumps(api))
        self.assertEqual(len(v.api_hashes(path, hashes, root)['verified_sha256']), 12)

    def test_api_missing_digest_unknown(self):
        root = self.assets()
        hashes = v.manifest_hashes(root)
        api = {'tag_name': v.TAG, 'draft': False, 'assets': [{'name': name, 'size': (root / name).stat().st_size,
                'digest': None} for name in sorted(v.EXPECTED)]}
        path = self.root / 'api.json'
        path.write_text(json.dumps(api))
        self.assertEqual(len(v.api_hashes(path, hashes, root)['missing_sha256']), 12)

    def test_component_and_embedded_association(self):
        assets = self.root / 'components'
        assets.mkdir()
        hashes = {}
        patches = v.expected_patches(SOURCE)
        metadata = {}
        for component in ('opencode', 'graft'):
            name = 'ivoai-' + component + '_linux_amd64.tar.gz'
            (assets / name).write_bytes(b'synthetic component only')
            hashes[name] = v.digest(assets / name)
            doc = {'archive': name, 'platform': 'linux/amd64', 'sha256': hashes[name],
                   'patch_sha256': patches[component]}
            if component == 'opencode': doc.update(version='1.19.0-ivoai.3', upstream_revision='733b2c627e5aec39cb33cc22373bdce33053ef2c', source_sha256='ab433f7a5fc9e54c90da18bd3457298009f426424805f043aa32c03543b4a192', bun_version='1.3.14', bun_sha256=v.BUN_SHA['amd64'])
            else: doc.update(version='0.21.1-ivoai.1', upstream_revision='fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad', source_sha256='d39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1', node_version='26.11.1', node_sha256=v.NODE_SHA['amd64'])
            doc['revision'] = hashlib.sha256((doc['source_sha256'] + '\n' + doc['patch_sha256'] + '\n' + doc['sha256'] + '\n').encode()).hexdigest()
            path = assets / ('managed-' + component + '-amd64.json')
            path.write_text(json.dumps(doc))
            metadata[component] = v.component_metadata(assets, component, 'amd64', hashes, patches)
        blob = b''
        for doc in metadata.values():
            embedded = {**doc, 'url': 'https://github.com/ivo-lopes/ivoai/releases/download/' + v.TAG + '/' + doc['archive']}
            blob += base64.b64encode((json.dumps(embedded, separators=(',', ':')) + '\n').encode()) + b'\0'
        binary = self.root / 'synthetic-bytes-not-an-executable'
        binary.write_bytes(blob)
        self.assertEqual(set(v.embedded_association(binary, metadata)), {'opencode', 'graft'})
        binary.write_bytes(b'no metadata')
        with self.assertRaises(ValueError): v.embedded_association(binary, metadata)
        metadata['opencode']['sha256'] = '0' * 64
        (assets / 'managed-opencode-amd64.json').write_text(json.dumps(metadata['opencode']))
        with self.assertRaises(ValueError): v.component_metadata(assets, 'opencode', 'amd64', hashes, patches)

    def test_duplicate_json_rejected(self):
        with self.assertRaises(ValueError): v.unique_json('{"x":1,"x":2}')


    def test_tar_pax_refused_before_body_read(self):
        archive = HERE / 'old-pax-4MiB-fixture.tar.gz'
        calls = []
        original = gzip.GzipFile.read
        def spy(stream, size=-1):
            data = original(stream, size)
            calls.append(len(data))
            return data
        with mock.patch.object(v.gzip.GzipFile, 'read', spy):
            with self.assertRaisesRegex(ValueError, 'extension'):
                v.safe_core(archive, self.root / 'core', 'amd64')
        self.assertEqual(sum(calls), 512)

    def test_tar_gnu_extension_refused(self):
        archive = self.tar([('long-name', tarfile.GNUTYPE_LONGNAME, b'')])
        with self.assertRaisesRegex(ValueError, 'extension'):
            v.safe_core(archive, self.root / 'core', 'amd64')

    def test_tar_oversized_header_refused(self):
        archive = self.root / 'oversized.tar.gz'
        info = tarfile.TarInfo('ivoai')
        info.mode, info.size = 0o755, v.MAX_CORE_BYTES + 1
        with gzip.open(archive, 'wb') as stream:
            stream.write(info.tobuf(format=tarfile.GNU_FORMAT))
            stream.write(b'\0' * 1024)
        with self.assertRaisesRegex(ValueError, 'unsafe'):
            v.safe_core(archive, self.root / 'core', 'amd64')

    def test_bounded_reader_prevents_overread(self):
        stream = io.BytesIO(b'abcdef')
        reader = v.BoundedGzipReader(stream, 2)
        with self.assertRaisesRegex(ValueError, 'budget'): reader.read(3)
        self.assertEqual(stream.tell(), 0)
        with self.assertRaisesRegex(ValueError, 'oversized'): reader.read(v.MiB + 1)
        self.assertEqual(stream.tell(), 0)

    def test_compressed_core_cap_before_parser(self):
        path = self.root / 'oversized.gz'
        with open(path, 'wb') as stream: stream.truncate(128 * v.MiB + 1)
        with mock.patch.object(v.gzip, 'GzipFile', side_effect=AssertionError('parser called')):
            with self.assertRaisesRegex(ValueError, 'size'):
                v.safe_core(path, self.root / 'core', 'amd64')

    def test_small_asset_cap_before_hashing(self):
        root = self.assets()
        path = root / 'managed-opencode-amd64.json'
        with open(path, 'wb') as stream: stream.truncate(65537)
        with mock.patch.object(v, 'digest', side_effect=AssertionError('hashing called')):
            with self.assertRaisesRegex(ValueError, 'size'): v.manifest_hashes(root)

    def skill(self, compression=zipfile.ZIP_STORED):
        path = self.root / 'skill.zip'
        with zipfile.ZipFile(path, 'w') as archive:
            directory = zipfile.ZipInfo('ivoai-memory-context/', (2020, 1, 1, 0, 0, 0))
            directory.external_attr = 0o40755 << 16
            member = zipfile.ZipInfo('ivoai-memory-context/SKILL.md', (2020, 1, 1, 0, 0, 0))
            member.external_attr = 0o100644 << 16
            member.compress_type = compression
            archive.writestr(directory, b'')
            archive.writestr(member, b'synthetic skill')
        return path

    def test_skill_positive(self):
        v.skill_contents(self.skill(), b'synthetic skill')

    def test_zip_size_before_parser(self):
        path = self.root / 'huge.zip'
        with open(path, 'wb') as stream: stream.truncate(v.MAX_ZIP_BYTES + 1)
        with mock.patch.object(v.zipfile, 'ZipFile', side_effect=AssertionError('parser called')):
            with self.assertRaisesRegex(ValueError, 'size'): v.skill_contents(path, b'x')

    def test_zip_count_before_parser(self):
        path = self.root / 'many.zip'
        with zipfile.ZipFile(path, 'w') as archive:
            for i in range(1000): archive.writestr(str(i), b'')
        with mock.patch.object(v.zipfile, 'ZipFile', side_effect=AssertionError('parser called')):
            with self.assertRaisesRegex(ValueError, 'count'): v.skill_contents(path, b'x')

    def test_zip_directory_budget_before_parser(self):
        path = self.root / 'bad-directory.zip'
        path.write_bytes(struct.pack('<4s4H2LH', b'PK\x05\x06', 0, 0, 2, 2, 4097, 0, 0))
        with mock.patch.object(v.zipfile, 'ZipFile', side_effect=AssertionError('parser called')):
            with self.assertRaisesRegex(ValueError, 'budget'): v.skill_contents(path, b'x')

    def test_zip_deflate_refused_before_member_read(self):
        path = self.skill(zipfile.ZIP_DEFLATED)
        with mock.patch.object(v.zipfile.ZipFile, 'read', side_effect=AssertionError('decompression called')):
            with self.assertRaisesRegex(ValueError, 'compression'): v.skill_contents(path, b'synthetic skill')


if __name__ == '__main__':
    unittest.main()
