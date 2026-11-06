#!/usr/bin/env python3
"""Official CLI protocol only. Synthetic loopback endpoint, isolated network.
No subscription inference, personal configuration, or authentication is used.
"""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time

SHA = '708c77b204db30086ff6f1ed34577bda03e1be74ced80557cbf7b54249ebf0bd'


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def mcp():
    for line in sys.stdin:
        request = json.loads(line)
        if 'id' not in request:
            continue
        method = request.get('method')
        if method == 'initialize':
            result = {'protocolVersion': '2024-11-05', 'capabilities': {'tools': {}},
                      'serverInfo': {'name': 'ivoai-offline-protocol-fixture', 'version': '1'}}
        elif method == 'tools/list':
            result = {'tools': [{'name': 'core_echo', 'description': 'Offline protocol evidence fixture',
                                'inputSchema': {'type': 'object', 'properties': {'text': {'type': 'string'}}}}]}
        elif method == 'tools/call':
            result = {'content': [{'type': 'text', 'text': 'CORE_EVIDENCE_MARKER'}]}
        else:
            result = {}
        print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}), flush=True)


def inside():
    factory = json.loads(Path('/fixture/args.json').read_text())
    assert factory[factory.index('--tools') + 1] == ''
    assert factory[factory.index('--setting-sources') + 1] == ''
    assert factory[factory.index('--permission-mode') + 1] == 'dontAsk'
    assert json.loads(factory[factory.index('--settings') + 1])['disableAllHooks'] is True
    version = subprocess.check_output(['/fixture/claude', '--version'], text=True).strip()
    assert version == '2.1.318 (Claude Code)', version
    records, state = [], {'tool': 'mcp__ivoai-orchestrator__core_echo', 'issued': False}

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or '{}')
            tools = [tool.get('name') for tool in body.get('tools', [])]
            messages = body.get('messages', [])
            records.append({'path': self.path, 'tools': tools, 'messages': messages})
            if 'count_tokens' in self.path:
                payload, kind = json.dumps({'input_tokens': 10}).encode(), 'application/json'
            else:
                is_tool = not state['issued'] and bool(tools)
                state['issued'] = state['issued'] or is_tool
                content = ({'type': 'tool_use', 'id': 'toolu_offline', 'name': state['tool'],
                            'input': {'text': 'fixture', 'description': 'delegation forbidden',
                                      'prompt': 'NEVER_SPAWN', 'subagent_type': 'Explore'}} if is_tool else
                           {'type': 'text', 'text': 'IVOAI_OFFLINE_OWNED_ANSWER'})
                stop = 'tool_use' if is_tool else 'end_turn'
                message = {'id': 'msg_offline_' + str(len(records)), 'type': 'message', 'role': 'assistant',
                           'model': 'claude-sonnet-4-6', 'content': [content], 'stop_reason': stop,
                           'stop_sequence': None, 'usage': {'input_tokens': 10, 'output_tokens': 5}}
                if body.get('stream'):
                    events = [('message_start', {'type': 'message_start', 'message': {**message, 'content': [], 'stop_reason': None}}),
                              ('content_block_start', {'type': 'content_block_start', 'index': 0, 'content_block': {**content, **({'input': {}} if is_tool else {'text': ''})}}),
                              ('content_block_delta', {'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'input_json_delta', 'partial_json': json.dumps(content['input'])} if is_tool else {'type': 'text_delta', 'text': content['text']}}),
                              ('content_block_stop', {'type': 'content_block_stop', 'index': 0}),
                              ('message_delta', {'type': 'message_delta', 'delta': {'stop_reason': stop, 'stop_sequence': None}, 'usage': {'output_tokens': 5}}),
                              ('message_stop', {'type': 'message_stop'})]
                    payload = ''.join('event: ' + event + '\ndata: ' + json.dumps(value) + '\n\n' for event, value in events).encode()
                    kind = 'text/event-stream'
                else:
                    payload, kind = json.dumps(message).encode(), 'application/json'
            self.send_response(200)
            self.send_header('Content-Type', kind)
            self.send_header('Content-Length', str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    home, work = Path('/audit/home'), Path('/audit/work')
    home.mkdir(mode=0o700, exist_ok=True)
    work.mkdir(mode=0o700, exist_ok=True)
    # Malicious ambient project configuration must have no execution authority.
    (work / '.claude').mkdir(exist_ok=True)
    (work / '.claude/settings.json').write_text(json.dumps({'hooks': {'SessionStart': [{'hooks': [{'type': 'command', 'command': 'touch /audit/HOOK_EXECUTED'}]}]}}))
    env = {'PATH': '/usr/bin:/bin', 'HOME': str(home), 'CLAUDE_CONFIG_DIR': str(home / 'config'),
           'ANTHROPIC_API_KEY': 'SYNTHETIC_LOOPBACK_ONLY_NOT_AUTH',
           'ANTHROPIC_BASE_URL': 'http://127.0.0.1:' + str(server.server_port),
           'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC': '1', 'DISABLE_AUTOUPDATER': '1',
           'CLAUDE_CODE_DISABLE_AUTO_MEMORY': '1', 'TERM': 'dumb'}
    summary = {'official_cli': version, 'binary_sha256': digest('/fixture/claude'),
               'factory_args_sha256': digest('/fixture/args.json'), 'real_inference': False,
               'external_network': False, 'personal_configuration_mounted': False,
               'synthetic_usage_not_economic_evidence': True, 'cases': []}
    native_id = None
    for name, tool in [('core_mcp', 'mcp__ivoai-orchestrator__core_echo'), ('owned_resume', 'mcp__ivoai-orchestrator__core_echo'), ('deny_native_agent', 'Agent')]:
        records.clear()
        state.update({'tool': tool, 'issued': False})
        command = ['/fixture/claude', *factory, '--print', '--verbose', '--output-format', 'stream-json', '--include-partial-messages', '--model', 'claude-sonnet-4-6', '--max-turns', '4']
        if name == 'owned_resume':
            command += ['--resume', native_id]
        command += ['-', ]
        started = time.monotonic()
        result = subprocess.run(command, input='Offline fixture only.', env=env, cwd=work, capture_output=True, text=True, timeout=50)
        lines = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
        init = next((line for line in lines if line.get('type') == 'system' and line.get('subtype') == 'init'), {})
        terminal = next((line for line in lines if line.get('type') == 'result'), {})
        session_id = init.get('session_id')
        evidence = any('CORE_EVIDENCE_MARKER' in json.dumps(record['messages']) for record in records)
        tools = set(tool for record in records for tool in record['tools'])
        passed = result.returncode == 0 and terminal.get('is_error') is False and bool(session_id)
        passed = passed and tools == {'mcp__ivoai-orchestrator__core_echo'} and not Path('/audit/HOOK_EXECUTED').exists()
        if name == 'owned_resume':
            passed = passed and session_id == native_id
        if name == 'deny_native_agent':
            passed = passed and 'Agent' not in tools and terminal.get('subagent_stats', {}).get('spawned') == 0
        else:
            passed = passed and evidence
        native_id = session_id
        (Path('/audit') / (name + '-stdout.jsonl')).write_text(result.stdout)
        (Path('/audit') / (name + '-stderr.txt')).write_text(result.stderr)
        summary['cases'].append({'name': name, 'pass': bool(passed), 'exit_code': result.returncode,
                                 'seconds': time.monotonic() - started, 'core_evidence_observed': evidence,
                                 'ambient_hook_executed': Path('/audit/HOOK_EXECUTED').exists(), 'native_subagents_spawned': terminal.get('subagent_stats', {}).get('spawned'), 'tool_catalog': sorted(tools)})
    server.shutdown()
    summary['pass'] = all(case['pass'] for case in summary['cases'])
    Path('/audit/summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    print(json.dumps(summary, indent=2))
    return 0 if summary['pass'] else 1


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary')
    parser.add_argument('--factory-args')
    parser.add_argument('--runtime')
    parser.add_argument('--output')
    parser.add_argument('--inside', action='store_true')
    parser.add_argument('--mcp', action='store_true')
    args = parser.parse_args()
    if args.mcp:
        mcp()
        return 0
    if args.inside:
        return inside()
    binary, runtime, output = Path(args.binary).resolve(), Path(args.runtime).resolve(), Path(args.output).resolve()
    assert digest(binary) == SHA
    assert output.stat().st_uid == os.getuid() and output.stat().st_mode & 0o077 == 0
    cmd = ['bwrap', '--unshare-all', '--die-with-parent', '--new-session', '--clearenv',
           '--ro-bind', '/usr', '/usr', '--symlink', 'usr/bin', '/bin', '--symlink', 'usr/lib', '/lib', '--symlink', 'usr/lib64', '/lib64',
           '--proc', '/proc', '--dev', '/dev', '--tmpfs', '/tmp', '--ro-bind', str(binary), '/fixture/claude',
           '--ro-bind', str(Path(args.factory_args).resolve()), '/fixture/args.json',
           '--ro-bind', str(Path(__file__).resolve()), '/fixture/probe.py', '--ro-bind', str(runtime), str(runtime),
           '--bind', str(output), '/audit', '--setenv', 'PATH', '/usr/bin:/bin', '--setenv', 'HOME', '/tmp/home',
           '--chdir', '/audit', '/usr/bin/python3', '/fixture/probe.py', '--inside']
    return subprocess.run(cmd, timeout=180, check=False).returncode

if __name__ == '__main__':
    raise SystemExit(main())
