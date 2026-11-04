"""Offline release-gate adversarial fixtures; no artifact is executed."""
import base64
import copy
import gzip
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest
import zipfile

SPEC = importlib.util.spec_from_file_location('release_artifact_gate', Path(__file__).with_name('verify-release-artifacts.py'))
v = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(v)


class ReleaseArtifactsTests(unittest.TestCase):
    def setUp(self):
        self.private = tempfile.TemporaryDirectory(prefix='ivoai-release-fixtures-')
        self.addCleanup(self.private.cleanup)
        self.root = Path(self.private.name)

    def assets(self):
        path = self.root/'assets'
        path.mkdir()
        for name in v.EXPECTED:
            (path/name).write_bytes(name.encode())
        self.checksums(path)
        return path

    def checksums(self, path):
        (path/'checksums.txt').write_text(''.join(v.digest(path/name)+'  '+name+'\n'
                                               for name in sorted(v.EXPECTED-{'checksums.txt'})))

    def tar(self, entries, format=tarfile.USTAR_FORMAT):
        path = self.root/'fixture.tar.gz'
        with tarfile.open(path, 'w:gz', format=format) as archive:
            for name, kind, data in entries:
                item = tarfile.TarInfo(name)
                item.type, item.mode = kind, 0o755
                if kind in (tarfile.REGTYPE, tarfile.AREGTYPE):
                    item.size = len(data)
                    archive.addfile(item, io.BytesIO(data))
                else:
                    if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                        item.linkname = '../escape'
                    archive.addfile(item)
        return path

    def elf(self, arch='amd64'):
        header = bytearray(64)
        header[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<HH', header, 16, 2, {'amd64': 62, 'arm64': 183}[arch])
        return bytes(header)

    def skill(self, content=b'reviewed skill', compression=zipfile.ZIP_STORED):
        path = self.root/'skill.zip'
        with zipfile.ZipFile(path, 'w') as archive:
            directory = zipfile.ZipInfo('ivoai-memory-context/')
            directory.external_attr = 0o40755 << 16
            archive.writestr(directory, b'')
            member = zipfile.ZipInfo('ivoai-memory-context/SKILL.md')
            member.external_attr = 0o100644 << 16
            member.compress_type = compression
            archive.writestr(member, content)
        return path

    def document(self, component='opencode', arch='amd64'):
        archive = 'ivoai-'+component+'_linux_'+arch+'.tar.gz'
        metadata = {'version': '1.19.0-ivoai.3', 'platform': 'linux/'+arch,
                    'sha256': 'b'*64, 'archive': archive,
                    'upstream_revision': '733b2c627e5aec39cb33cc22373bdce33053ef2c',
                    'source_sha256': 'ab433f7a5fc9e54c90da18bd3457298009f426424805f043aa32c03543b4a192',
                    'patch_sha256': 'a'*64, 'bun_version': '1.3.14', 'bun_sha256': v.BUN_SHA[arch]}
        if component == 'graft':
            metadata.update(version='0.21.1-ivoai.1',
                            upstream_revision='fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad',
                            source_sha256='d39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1',
                            node_version='26.11.1', node_sha256=v.NODE_SHA[arch])
            del metadata['bun_version'], metadata['bun_sha256']
        metadata['revision'] = hashlib.sha256((metadata['source_sha256']+'\n'+metadata['patch_sha256']+'\n'+metadata['sha256']+'\n').encode()).hexdigest()
        return metadata

    def golden_info(self, arch='amd64'):
        contract = {'module': 'github.com/ivo-lopes/ivoai', 'go_version': 'go1.27.3',
                    'source_commit': 'c'*40, 'requires': {'example.invalid/lib': 'v1.0.0'},
                    'sums': {('example.invalid/lib', 'v1.0.0'): 'h1:reviewed'}}
        metadata = {name: self.document(name, arch) for name in ('opencode', 'graft')}
        flags = '-s -w -X main.version=0.16.0'
        for name, doc in metadata.items():
            body = {**doc, 'url': 'https://github.com/ivo-lopes/ivoai/releases/download/v0.16.0/'+doc['archive']}
            encoded = base64.b64encode((json.dumps(body, separators=(',', ':'))+'\n').encode()).decode()
            flags += ' -X '+contract['module']+'/internal/components.managed'+('OpenCode' if name == 'opencode' else 'Graft')+'Build='+encoded
        settings = {'-ldflags': flags, '-buildmode': 'exe', '-compiler': 'gc', '-trimpath': 'true',
                    'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': arch, 'vcs': 'git',
                    'vcs.revision': contract['source_commit'], 'vcs.modified': 'false'}
        if arch == 'amd64':
            settings['GOAMD64'] = 'v1'
        info = {'GoVersion': contract['go_version'], 'Path': contract['module']+'/cmd/ivoai',
                'Main': {'Path': contract['module'], 'Version': 'v0.16.0'},
                'Settings': [{'Key': k, 'Value': value} for k, value in settings.items()],
                'Deps': [{'Path': 'example.invalid/lib', 'Version': 'v1.0.0', 'Sum': 'h1:reviewed'}]}
        return info, contract, metadata

    def test_manifest_exact_positive(self):
        self.assertEqual(len(v.manifest_hashes(self.assets())), 12)

    def test_manifest_missing_and_extra_assets(self):
        path = self.assets()
        (path/'extra').write_text('unexpected')
        with self.assertRaisesRegex(ValueError, 'exactly'): v.manifest_hashes(path)
        (path/'extra').unlink()
        (path/'ivoai-memory-context.zip').unlink()
        with self.assertRaisesRegex(ValueError, 'exactly'): v.manifest_hashes(path)

    def test_manifest_duplicate_or_traversal_checksums(self):
        path = self.assets()
        valid = (path/'checksums.txt').read_text()
        for extra in (valid.splitlines()[0]+'\n', 'a'*64+'  ../escape\n'):
            with self.subTest(extra=extra[:10]):
                (path/'checksums.txt').write_text(valid+extra)
                with self.assertRaises(ValueError): v.manifest_hashes(path)

    def test_manifest_checksum_tamper(self):
        path = self.assets()
        (path/'ivoai-memory-context.zip').write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'): v.manifest_hashes(path)

    def test_manifest_symlink_or_hardlink_refused(self):
        path = self.assets()
        asset = path/'ivoai-memory-context.zip'
        asset.unlink()
        asset.symlink_to(path/'checksums.txt')
        with self.assertRaises(ValueError): v.manifest_hashes(path)
        asset.unlink()
        asset.hardlink_to(path/'checksums.txt')
        with self.assertRaises(ValueError): v.manifest_hashes(path)

    def test_manifest_size_budget_before_hash(self):
        path = self.assets()
        with (path/'checksums.txt').open('wb') as stream: stream.truncate(65537)
        with self.assertRaisesRegex(ValueError, 'size'): v.manifest_hashes(path)

    def test_core_elf_both_architectures(self):
        for arch in ('amd64', 'arm64'):
            with self.subTest(arch=arch):
                archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf(arch))])
                self.assertEqual(v.scan_tar(archive, 'core', arch, self.root/arch)['entries'], 1)

    def test_core_wrong_architecture(self):
        archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf('arm64'))])
        with self.assertRaisesRegex(ValueError, 'architecture'): v.scan_tar(archive, 'core', 'amd64', self.root/'core')

    def test_core_unsafe_names(self):
        for name in ('../ivoai', '/ivoai', 'a/../ivoai', 'a//ivoai', 'a\\ivoai', './ivoai'):
            with self.subTest(name=name):
                archive = self.tar([(name, tarfile.REGTYPE, self.elf())])
                with self.assertRaises(ValueError): v.scan_tar(archive, 'core', 'amd64')

    def test_core_links_and_extensions(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.DIRTYPE, tarfile.GNUTYPE_SPARSE,
                     tarfile.GNUTYPE_LONGNAME, tarfile.XGLTYPE):
            with self.subTest(kind=kind):
                archive = self.tar([('ivoai', kind, b'')])
                with self.assertRaises(ValueError): v.scan_tar(archive, 'core', 'amd64')

    def test_core_duplicates_and_extra_files(self):
        for name in ('ivoai', 'extra'):
            archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf()), (name, tarfile.REGTYPE, b'x')])
            with self.assertRaises(ValueError): v.scan_tar(archive, 'core', 'amd64')

    def test_core_payload_limit_before_read(self):
        item = tarfile.TarInfo('ivoai')
        item.size, item.mode = v.MAX_CORE_BYTES+1, 0o755
        archive = self.root/'oversize.tar.gz'
        archive.write_bytes(gzip.compress(item.tobuf()))
        with self.assertRaisesRegex(ValueError, 'size exceeds'): v.scan_tar(archive, 'core', 'amd64')

    def test_no_hidden_concatenated_tar(self):
        archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf())])
        data = gzip.decompress(archive.read_bytes())
        archive.write_bytes(gzip.compress(data+data))
        with self.assertRaisesRegex(ValueError, 'trailing'): v.scan_tar(archive, 'core', 'amd64')

    def test_truncated_or_bad_gzip(self):
        archive = self.tar([('ivoai', tarfile.REGTYPE, self.elf())])
        archive.write_bytes(archive.read_bytes()[:-4])
        with self.assertRaises((ValueError, EOFError, OSError)): v.scan_tar(archive, 'core', 'amd64')

    def test_component_archives_positive(self):
        archive = self.tar([('opencode', tarfile.REGTYPE, b'fixture'), ('LICENSE', tarfile.REGTYPE, b'MIT')])
        self.assertEqual(v.scan_tar(archive, 'opencode', 'amd64')['entries'], 2)
        required = ['graft-runtime', 'provenance.json', 'files.json', 'LICENSE']
        archive = self.tar([(name, tarfile.REGTYPE, b'fixture') for name in required] +
                           [('a'*120, tarfile.REGTYPE, b'long reviewed path')], tarfile.PAX_FORMAT)
        self.assertEqual(v.scan_tar(archive, 'graft', 'amd64')['entries'], 5)

    def test_component_file_parent_collision(self):
        for entries in ([('a', tarfile.REGTYPE, b'x'), ('a/b', tarfile.REGTYPE, b'x')],
                        [('a/b', tarfile.REGTYPE, b'x'), ('a', tarfile.REGTYPE, b'x')]):
            archive = self.tar(entries)
            with self.assertRaises(ValueError): v.scan_tar(archive, 'graft', 'amd64')

    def test_component_missing_required_entries(self):
        archive = self.tar([('LICENSE', tarfile.REGTYPE, b'MIT')])
        with self.assertRaisesRegex(ValueError, 'missing'): v.scan_tar(archive, 'graft', 'amd64')

    def test_component_header_count_limit(self):
        archive = self.tar([('file'+str(n), tarfile.REGTYPE, b'') for n in range(4097)])
        with self.assertRaisesRegex(ValueError, 'excessive'): v.scan_tar(archive, 'graft', 'amd64')

    def test_pax_overlarge_header_rejected_before_payload(self):
        item = tarfile.TarInfo('pax')
        item.type, item.size = tarfile.XHDTYPE, 4*v.MiB
        archive = self.root/'pax.tar.gz'
        archive.write_bytes(gzip.compress(item.tobuf()))
        with self.assertRaisesRegex(ValueError, 'PAX header budget'): v.scan_tar(archive, 'graft', 'amd64')

    def test_pax_overrides_nested_records_and_traversal_refused(self):
        for record in (b'12 size=123\n', b'15 path=../bad\n', b'10 path=a\n10 path=b\n'):
            with self.subTest(record=record):
                try:
                    path = v.pax_path(record)
                    v.safe_name(path)
                except ValueError:
                    continue
                self.fail('unsafe PAX accepted')

    def test_zip_exact_positive(self):
        v.skill_contents(self.skill(), b'reviewed skill')

    def test_zip_content_tamper(self):
        with self.assertRaisesRegex(ValueError, 'differs'): v.skill_contents(self.skill(b'changed bytes!'), b'reviewed skill')

    def test_zip_compression_not_canonical(self):
        with self.assertRaisesRegex(ValueError, 'noncanonical'): v.skill_contents(self.skill(compression=zipfile.ZIP_DEFLATED), b'reviewed skill')

    def test_zip_extra_and_duplicate_members(self):
        for name in ('../escape', 'ivoai-memory-context/SKILL.md'):
            archive = self.skill()
            with zipfile.ZipFile(archive, 'a') as stream:
                # zipfile's duplicate-name warning is expected for this attack.
                import warnings
                with warnings.catch_warnings():
                    warnings.simplefilter('ignore', UserWarning)
                    stream.writestr(name, b'x')
            with self.assertRaises(ValueError): v.skill_contents(archive, b'reviewed skill')

    def test_source_identity_must_be_full_commit(self):
        for commit in ('HEAD', 'v0.16.0', '67cee56', 'a'*40+':go.mod', 'A'*40):
            with self.subTest(commit=commit):
                with self.assertRaisesRegex(ValueError, 'full lowercase'):
                    v.Source(self.root, commit)

    def test_zip_symlink_refused(self):
        path = self.root/'symlink.zip'
        with zipfile.ZipFile(path, 'w') as archive:
            directory = zipfile.ZipInfo('ivoai-memory-context/')
            directory.external_attr = 0o40755 << 16
            archive.writestr(directory, b'')
            item = zipfile.ZipInfo('ivoai-memory-context/SKILL.md')
            item.external_attr = 0o120777 << 16
            archive.writestr(item, b'reviewed skill')
        with self.assertRaisesRegex(ValueError, 'unsafe skill'): v.skill_contents(path, b'reviewed skill')

    def test_zip_preamble_and_oversize_directory(self):
        path = self.skill()
        path.write_bytes(b'hidden'+path.read_bytes())
        with self.assertRaises(ValueError): v.skill_contents(path, b'reviewed skill')

    def test_zip_local_header_cannot_override_central_sizes(self):
        for offset in (6, 14, 18, 22):
            path = self.skill()
            blob = bytearray(path.read_bytes())
            blob[offset] ^= 1
            path.write_bytes(blob)
            with self.assertRaisesRegex(ValueError, 'headers disagree'):
                v.skill_contents(path, b'reviewed skill')

    def test_unique_json_rejects_duplicate_keys(self):
        with self.assertRaisesRegex(ValueError, 'duplicate'): v.unique_json('{"key":1,"key":2}')

    def test_component_metadata_positive_both_architectures(self):
        for component in ('graft', 'opencode'):
            for arch in ('amd64', 'arm64'):
                with self.subTest(component=component, arch=arch):
                    doc = self.document(component, arch)
                    (self.root/('managed-'+component+'-'+arch+'.json')).write_text(json.dumps(doc))
                    self.assertEqual(v.component_metadata(self.root, component, arch, {doc['archive']: 'b'*64}, {component: 'a'*64}), doc)

    def test_component_metadata_association_tampering(self):
        original = self.document()
        for field in ('version', 'platform', 'archive', 'revision', 'sha256', 'source_sha256',
                      'patch_sha256', 'upstream_revision', 'bun_version', 'bun_sha256'):
            with self.subTest(field=field):
                doc = {**original, field: 'tampered'}
                (self.root/'managed-opencode-amd64.json').write_text(json.dumps(doc))
                with self.assertRaises(ValueError):
                    v.component_metadata(self.root, 'opencode', 'amd64', {original['archive']: 'b'*64}, {'opencode': 'a'*64})

    def test_buildinfo_golden_both_architectures(self):
        for arch in ('amd64', 'arm64'):
            info, contract, metadata = self.golden_info(arch)
            v.verify_info(info, contract, arch, 'v0.16.0', metadata)

    def unrecorded_info(self, wrong_url=False, missing=False):
        info, contract, metadata = self.golden_info()
        info['Settings'] = [item for item in info['Settings'] if item['Key'] != '-ldflags']
        blob = self.elf()
        for document in metadata.values():
            body = {**document, 'url': 'https://github.com/ivo-lopes/ivoai/releases/download/' +
                    ('v0.15.0' if wrong_url else 'v0.16.0') + '/' + document['archive']}
            if not missing:
                blob += base64.b64encode((json.dumps(body, separators=(',', ':'))+'\n').encode())
        binary = self.root/'core-fixture'
        binary.write_bytes(blob)
        return info, contract, metadata, binary

    def test_unrecorded_ldflags_are_explicit_not_verified_bindings(self):
        info, contract, metadata, binary = self.unrecorded_info()
        result = v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata, binary)
        self.assertEqual(result['ldflags_status'], 'NOT_RECORDED_BY_TOOLCHAIN')
        self.assertNotIn('-ldflags', result['required_build_settings_verified'])
        self.assertNotIn('-ldflags', result['observed_build_settings'])
        self.assertEqual(len(result['embedded_byte_associations']), 2)
        self.assertIn('REQUIRED_SEPARATELY', result['native_version_help_gate'])

    def test_unrecorded_ldflags_require_actual_expected_payloads(self):
        for wrong_url, missing in ((True, False), (False, True)):
            info, contract, metadata, binary = self.unrecorded_info(wrong_url, missing)
            with self.assertRaisesRegex(ValueError, 'same-tag embedded'):
                v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata, binary)

    def test_unrecorded_ldflags_do_not_relax_version_or_source(self):
        info, contract, metadata, binary = self.unrecorded_info()
        info['Main']['Version'] = 'v0.15.0'
        with self.assertRaisesRegex(ValueError, 'module/version'):
            v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata, binary)
        info, contract, metadata, binary = self.unrecorded_info()
        next(item for item in info['Settings'] if item['Key'] == 'vcs.modified')['Value'] = 'true'
        with self.assertRaisesRegex(ValueError, 'build setting'):
            v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata, binary)

    def test_buildinfo_source_dirty_cgo_and_cpu_rejected(self):
        for key, value in [('vcs.revision', 'd'*40), ('vcs.modified', 'true'), ('CGO_ENABLED', '1'),
                           ('GOAMD64', 'v3'), ('GOARCH', 'arm64'), ('-trimpath', 'false')]:
            with self.subTest(key=key):
                info, contract, metadata = self.golden_info()
                next(item for item in info['Settings'] if item['Key'] == key)['Value'] = value
                with self.assertRaisesRegex(ValueError, 'build setting'): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)

    def test_buildinfo_version_and_entrypoint_rejected(self):
        for location in ('GoVersion', 'Path', 'Version'):
            info, contract, metadata = self.golden_info()
            if location == 'Version': info['Main']['Version'] = 'v0.15.0'
            else: info[location] = 'unexpected'
            with self.assertRaises(ValueError): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)

    def test_buildinfo_dependency_replaced_or_modified(self):
        for field, value in [('Replace', {'Path': '/unreviewed'}), ('Sum', 'h1:changed'), ('Version', 'v2.0.0')]:
            info, contract, metadata = self.golden_info()
            info['Deps'][0][field] = value
            with self.assertRaises(ValueError): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)

    def test_buildinfo_wrong_ldflag_version(self):
        info, contract, metadata = self.golden_info()
        setting = next(item for item in info['Settings'] if item['Key'] == '-ldflags')
        setting['Value'] = setting['Value'].replace('main.version=0.16.0', 'main.version=0.15.0')
        with self.assertRaisesRegex(ValueError, 'version linker'): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)

    def test_buildinfo_duplicate_linker_and_build_setting(self):
        info, contract, metadata = self.golden_info()
        next(item for item in info['Settings'] if item['Key'] == '-ldflags')['Value'] += ' -X main.version=0.16.0'
        with self.assertRaisesRegex(ValueError, 'duplicate'): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)
        info, contract, metadata = self.golden_info()
        info['Settings'].append(copy.deepcopy(info['Settings'][0]))
        with self.assertRaisesRegex(ValueError, 'duplicate'): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)

    def test_buildinfo_same_tag_url_required(self):
        info, contract, metadata = self.golden_info()
        flags = next(item for item in info['Settings'] if item['Key'] == '-ldflags')
        bindings = v.ldflags({'-ldflags': flags['Value']})
        key = contract['module']+'/internal/components.managedGraftBuild'
        body = json.loads(base64.b64decode(bindings[key]))
        body['url'] = body['url'].replace('v0.16.0', 'v0.15.0')
        flags['Value'] = flags['Value'].replace(bindings[key], base64.b64encode(json.dumps(body).encode()).decode())
        with self.assertRaisesRegex(ValueError, 'same-tag'): v.verify_info(info, contract, 'amd64', 'v0.16.0', metadata)

    def test_optional_api_unknown_digests_remain_unknown(self):
        path = self.assets()
        hashes = v.manifest_hashes(path)
        doc = {'tagName': 'v0.16.0', 'isDraft': True, 'isPrerelease': False,
               'assets': [{'name': name, 'size': (path/name).stat().st_size} for name in sorted(v.EXPECTED)]}
        api = self.root/'api.json'
        api.write_text(json.dumps(doc))
        checked = v.api_metadata(api, hashes, path, 'v0.16.0')
        self.assertEqual(checked['verified_digests'], 0)
        self.assertEqual(len(checked['unknown_digests']), 12)

    def test_release_version_preserves_workflow_prerelease_grammar(self):
        for version in ('0.16.0', 'v0.16.0', '0.17.0-rc.1', 'v0.17.0-beta-2', '1.0.0-123'):
            with self.subTest(version=version):
                self.assertEqual(v.release_tag(version), 'v'+version.removeprefix('v'))
        for version in ('0.16', '0.16.0-', '0.16.0-rc..1', '0.16.0+build', '0.16.0/rc',
                        '../0.16.0', '0.16.0-rc_1', '0.16.0\n', 'vv0.16.0'):
            with self.subTest(version=version):
                with self.assertRaisesRegex(ValueError, 'tag grammar'):
                    v.release_tag(version)

    def test_api_prerelease_must_match_tag(self):
        path = self.assets()
        hashes = v.manifest_hashes(path)
        for tag, prerelease, accepted in (
                ('v0.16.0', False, True), ('v0.16.0', True, False),
                ('v0.17.0-rc.1', True, True), ('v0.17.0-rc.1', False, False),
                ('v0.17.0-rc.1', None, False), ('v0.16.0', 0, False)):
            with self.subTest(tag=tag, prerelease=prerelease):
                doc = {'tag_name': tag, 'draft': True, 'prerelease': prerelease,
                       'assets': [{'name': name, 'size': (path/name).stat().st_size,
                                   'digest': 'sha256:'+hashes[name]} for name in sorted(v.EXPECTED)]}
                api = self.root/'api.json'
                api.write_text(json.dumps(doc))
                if accepted:
                    checked = v.api_metadata(api, hashes, path, tag)
                    self.assertIs(checked['prerelease'], prerelease)
                    self.assertEqual(checked['verified_digests'], 12)
                else:
                    with self.assertRaisesRegex(ValueError, 'prerelease state'):
                        v.api_metadata(api, hashes, path, tag)

    def test_optional_api_digest_size_tag_and_duplicates_rejected(self):
        path = self.assets()
        hashes = v.manifest_hashes(path)
        golden = {'tag_name': 'v0.16.0', 'draft': True, 'prerelease': False,
                  'assets': [{'name': name, 'size': (path/name).stat().st_size, 'digest': 'sha256:'+hashes[name]} for name in sorted(v.EXPECTED)]}
        for case in ('digest', 'size', 'tag', 'duplicate', 'url'):
            doc = copy.deepcopy(golden)
            if case == 'digest': doc['assets'][0]['digest'] = 'sha256:'+'e'*64
            elif case == 'size': doc['assets'][0]['size'] += 1
            elif case == 'tag': doc['tag_name'] = 'v0.15.0'
            elif case == 'url': doc['assets'][0]['browser_download_url'] = 'https://example.invalid/wrong'
            else: doc['assets'][1] = doc['assets'][0]
            api = self.root/'api.json'
            api.write_text(json.dumps(doc))
            with self.assertRaises(ValueError): v.api_metadata(api, hashes, path, 'v0.16.0')


if __name__ == '__main__':
    unittest.main()
