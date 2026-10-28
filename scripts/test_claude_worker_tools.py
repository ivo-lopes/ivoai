#!/usr/bin/env python3
"""Pinned official Claude protocol proof; synthetic provider, isolated network.

Invoke through TestClaudeWorkerOfficialDelegationBoundary to test actual factory
arguments. No authentication, provider inference, or personal config is used.
"""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time

BINARY_SHA256 = '24972e3bc859fab2b46ed4c1e51f7d6130f06d3bd550811a114640de3370d0de'
ARCHIVE_SHA256 = '45fe2b33f5cbdd114309618970bcd90b8b163e34252d66cf100d7e068d9b831d'
VERSION = '2.1.296 (Claude Code)'
CHILD_PROMPT = 'Return IVOAI_SYNTHETIC_CHILD; do not call tools.'


def digest(path):
    value = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def launch(args):
    binary = Path(args.binary).resolve(strict=True)
    factory = Path(args.factory_args).resolve(strict=True)
    output = Path(args.output).absolute()
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    if output.is_symlink() or output.stat().st_uid != os.getuid() or output.stat().st_mode & 0o077:
        raise RuntimeError('proof output must be an owned private directory')
    if digest(binary) != BINARY_SHA256:
        raise RuntimeError('official Claude 2.1.296 Linux amd64 binary digest mismatch')
    cmd = ['bwrap', '--unshare-all', '--die-with-parent', '--new-session', '--clearenv',
           '--ro-bind', '/usr', '/usr', '--symlink', 'usr/bin', '/bin',
           '--symlink', 'usr/lib', '/lib', '--symlink', 'usr/lib64', '/lib64',
           '--proc', '/proc', '--dev', '/dev', '--tmpfs', '/tmp',
           '--ro-bind', str(binary), '/fixture/claude',
           '--ro-bind', str(factory), '/fixture/args.json',
           '--ro-bind', str(Path(__file__).resolve()), '/fixture/probe.py',
           '--bind', str(output), '/audit', '--setenv', 'PATH', '/usr/bin:/bin',
           '--setenv', 'HOME', '/tmp/home', '--chdir', '/audit',
           '/usr/bin/python3', '/fixture/probe.py', '--inside']
    return subprocess.run(cmd, check=False, timeout=210).returncode


def inside():
    version = subprocess.check_output(['/fixture/claude', '--version'], text=True).strip()
    assert version == VERSION, version
    factory = json.loads(Path('/fixture/args.json').read_text())
    assert factory[factory.index('--tools') + 1] == 'Read,Glob,Grep'
    denied = factory[factory.index('--disallowedTools') + 1].split(',')
    assert 'Agent' in denied and 'Task' in denied
    assert '--strict-mcp-config' in factory and '--permission-mode' in factory
    assert json.loads(factory[factory.index('--mcp-config') + 1]) == {'mcpServers': {}}
    Path('/audit/factory-args.json').write_text(json.dumps(factory, indent=2) + '\n')
    records = []
    state = {'issued': False, 'alias': 'Agent'}

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or '{}')
            tools = [tool.get('name') for tool in body.get('tools', [])]
            messages = body.get('messages', [])
            classifier = not tools and '<block>' in json.dumps(messages)
            child = any(isinstance(m.get('content'), list) and any(
                isinstance(x, dict) and x.get('text') == CHILD_PROMPT
                for x in m['content']) for m in messages)
            records.append({'path': self.path, 'tool_names': tools,
                            'classifier': classifier, 'child': child, 'messages': messages})
            if 'count_tokens' in self.path:
                response, content_type = json.dumps({'input_tokens': 10}).encode(), 'application/json'
            else:
                is_tool = bool(tools) and not state['issued']
                if is_tool:
                    state['issued'] = True
                    content = {'type': 'tool_use', 'id': 'toolu_synthetic_agent', 'name': state['alias'],
                               'input': {'description': 'Synthetic native delegation',
                                         'prompt': CHILD_PROMPT, 'subagent_type': 'Explore'}}
                else:
                    content = {'type': 'text', 'text': '<block>no</block>' if classifier
                               else 'IVOAI_SYNTHETIC_CHILD' if child else 'IVOAI_SYNTHETIC_PARENT'}
                message = {'id': 'msg_synthetic_' + str(len(records)), 'type': 'message',
                           'role': 'assistant', 'model': 'claude-sonnet-4-6', 'content': [content],
                           'stop_reason': 'tool_use' if is_tool else 'end_turn', 'stop_sequence': None,
                           'usage': {'input_tokens': 10, 'output_tokens': 5}}
                if body.get('stream'):
                    events = [
                        ('message_start', {'type': 'message_start', 'message': {
                            **message, 'content': [], 'stop_reason': None,
                            'usage': {'input_tokens': 10, 'output_tokens': 0}}}),
                        ('content_block_start', {'type': 'content_block_start', 'index': 0,
                                                'content_block': {**content, **({'input': {}} if is_tool else {'text': ''})}}),
                        ('content_block_delta', {'type': 'content_block_delta', 'index': 0,
                                                'delta': {'type': 'input_json_delta', 'partial_json': json.dumps(content['input'])}
                                                if is_tool else {'type': 'text_delta', 'text': content['text']}}),
                        ('content_block_stop', {'type': 'content_block_stop', 'index': 0}),
                        ('message_delta', {'type': 'message_delta', 'delta': {
                            'stop_reason': message['stop_reason'], 'stop_sequence': None}, 'usage': {'output_tokens': 5}}),
                        ('message_stop', {'type': 'message_stop'})]
                    response = ''.join('event: ' + event + '\ndata: ' + json.dumps(value) + '\n\n'
                                       for event, value in events).encode()
                    content_type = 'text/event-stream'
                else:
                    response, content_type = json.dumps(message).encode(), 'application/json'
            self.send_response(200)
            self.send_header('Content-Type', content_type)
            self.send_header('Content-Length', str(len(response)))
            self.end_headers()
            self.wfile.write(response)

    def before_fix(values):
        values = list(values)
        i = values.index('--tools')
        del values[i:i + 2]
        i = values.index('--disallowedTools') + 1
        values[i] = ','.join(x for x in values[i].split(',') if x not in ('Agent', 'Task'))
        return values

    cases = [('positive_control', 'Agent', before_fix(factory) + ['--allowedTools', 'Agent,Task'], True),
             ('before_fix_agent', 'Agent', before_fix(factory), True),
             ('before_fix_task_alias', 'Task', before_fix(factory), True),
             ('fixed_agent', 'Agent', factory, False), ('fixed_task_alias', 'Task', factory, False)]
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    summary = {'version': version, 'binary_sha256': digest('/fixture/claude'),
               'official_archive_sha256': ARCHIVE_SHA256,
               'factory_args_sha256': digest('/fixture/args.json'),
               'real_inference': False, 'new_authentication': False,
               'external_network': False, 'personal_config_mounted': False,
               'synthetic_usage_not_economic_evidence': True, 'cases': []}
    failures = []
    try:
        for name, alias, cli_args, expected_spawn in cases:
            records.clear()
            state.update({'issued': False, 'alias': alias})
            home = Path('/audit') / name
            home.mkdir(mode=0o700, exist_ok=True)
            work = home / 'work'
            work.mkdir(exist_ok=True)
            env = {'PATH': '/usr/bin:/bin', 'HOME': str(home), 'CLAUDE_CONFIG_DIR': str(home / 'config'),
                   'ANTHROPIC_API_KEY': 'ivoai-synthetic-loopback-no-real-auth',
                   'ANTHROPIC_BASE_URL': 'http://127.0.0.1:' + str(server.server_port),
                   'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC': '1', 'DISABLE_AUTOUPDATER': '1',
                   'CLAUDE_CODE_DISABLE_TERMINAL_TITLE': '1', 'TERM': 'dumb'}
            started = time.monotonic()
            proc = subprocess.Popen(['/fixture/claude', *cli_args, '--setting-sources', '',
                                     '--max-turns', '4', '--model', 'claude-sonnet-4-6', '--', 'Synthetic audit only.'],
                                    env=env, cwd=work, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
            timeout = False
            try:
                stdout, stderr = proc.communicate(timeout=35)
            except subprocess.TimeoutExpired:
                timeout = True
                os.killpg(proc.pid, signal.SIGKILL)
                stdout, stderr = proc.communicate()
            (home / 'requests.json').write_text(json.dumps(records, indent=2) + '\n')
            (home / 'stdout.json').write_bytes(stdout)
            (home / 'stderr.txt').write_bytes(stderr)
            result = json.loads(stdout) if stdout.strip() else {}
            first = next((q for q in records if q['path'].startswith('/v1/messages')), {})
            children = sum(q['child'] and not q['classifier'] and bool(q['tool_names']) for q in records)
            disabled = any('disabled for this session' in json.dumps(q['messages']) for q in records)
            spawned = result.get('subagent_stats', {}).get('spawned')
            passed = proc.returncode == 0 and not timeout and spawned == int(expected_spawn)
            passed = passed and children == int(expected_spawn)
            passed = passed and (('Agent' in first.get('tool_names', [])) if expected_spawn else
                                 (set(first.get('tool_names', [])) == {'Read', 'Glob', 'Grep'} and disabled))
            item = {'case': name, 'alias': alias, 'exit_code': proc.returncode, 'timeout': timeout,
                    'advertised_tools': first.get('tool_names'), 'requests': len(records),
                    'child_api_requests': children, 'subagent_stats': result.get('subagent_stats'),
                    'forged_tool_disabled': disabled, 'elapsed_seconds': round(time.monotonic() - started, 3),
                    'pass': passed}
            summary['cases'].append(item)
            if not passed:
                failures.append(name)
            print(json.dumps(item), flush=True)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        summary['pass'] = not failures and len(summary['cases']) == len(cases)
        Path('/audit/summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    if failures:
        raise RuntimeError('official client boundary failed: ' + ', '.join(failures))
    assert summary['pass']
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary')
    parser.add_argument('--factory-args')
    parser.add_argument('--output')
    parser.add_argument('--inside', action='store_true', help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.inside:
        return inside()
    if not all((args.binary, args.factory_args, args.output)):
        parser.error('--binary, --factory-args and --output are required')
    return launch(args)


if __name__ == '__main__':
    sys.exit(main())
