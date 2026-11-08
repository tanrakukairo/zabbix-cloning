"""Live ZC compatibility checks against disposable Kubernetes instances only.

Requires localhost forwards: 18700/1, 18720/1, 18740/1, 18800/1.
Deletes default hosts/templates on SOURCE instances during seed. Never point
these endpoints at an existing installation. Artifacts go to .tools/compat80.
"""
from http.client import HTTPConnection
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor

from zabbix_utils import ZabbixAPI
from zabbix_utils.exceptions import APIRequestError

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(os.environ.get('ZC_COMPAT_OUT', ROOT / '.tools' / 'compat80'))
EXPECTED_VERSIONS = json.loads(Path(__file__).with_name('versions.json').read_text(encoding='utf-8'))
VERSIONS = sorted(EXPECTED_VERSIONS)


class API:
    def __init__(self, version, role):
        self.node = f'z{version}-{role}'
        port = 18000 + int(version) * 10 + (role == 'target')
        self.url = f'http://127.0.0.1:{port}'
        connection = HTTPConnection('127.0.0.1', port, timeout=30)
        try:
            connection.request('GET', '/index.php?form=default')
            with connection.getresponse() as response:
                if response.status != 200:
                    raise RuntimeError(f'{self.node}: frontend returned HTTP {response.status}')
                html = response.read(4 << 20).decode('utf-8')
        finally:
            connection.close()
        match = re.search(r'<div class="server-name">\s*([^<]+?)\s*</div>', html)
        if not match or match.group(1) != self.node:
            raise RuntimeError('Refusing non-test endpoint: ' + self.url)

        # The SDK would otherwise log in from environment credentials at construction.
        if any(os.environ.get(key) for key in ('ZABBIX_TOKEN', 'ZABBIX_USER', 'ZABBIX_PASSWORD')):
            raise RuntimeError('Unset ZABBIX_TOKEN, ZABBIX_USER and ZABBIX_PASSWORD before running the fixture')
        expected = EXPECTED_VERSIONS[version]
        # zabbix-utils 2.0.4 caps its tested versions at 7.4. Keep our exact check.
        self.client = ZabbixAPI(url=self.url, timeout=120, skip_version_check=version == '80')
        self.version = str(self.client.version)
        if self.version != expected:
            raise RuntimeError(f'{self.node}: expected Zabbix {expected}, got {self.version}')
        self.client.login(user='Admin', password='zabbix')

    def call(self, method, params):
        resource, operation = method.split('.', 1)
        try:
            return getattr(getattr(self.client, resource), operation)(params)
        except APIRequestError as exc:
            raise RuntimeError(f'{self.node} {method}: {exc}') from exc

    def one(self, method, name, key='name', **params):
        rows = self.call(method + '.get', dict(output='extend', filter={key: name}, **params))
        assert len(rows) == 1, (method, name, rows)
        return rows[0]


def seed(version):
    api = API(version, 'source')
    # Source fixtures are deliberately small; default media/account/settings
    # remain to exercise real exports, defaults and version conversions.
    for method, idkey in [('maintenance', 'maintenanceid'), ('action', 'actionid'), ('host', 'hostid'), ('template', 'templateid')]:
        ids = [r[idkey] for r in api.call(method + '.get', {'output': [idkey]})]
        if ids:
            api.call(method + '.delete', ids)
    suffix = version
    for method, idkey, name in [('proxy', 'proxyid', 'ZC Proxy '), ('proxygroup', 'proxy_groupid', 'ZC Proxy Group '), ('regexp', 'regexpid', 'ZC Regex '), ('script', 'scriptid', 'ZC Script '), ('role', 'roleid', 'ZC Role '), ('usergroup', 'usrgrpid', 'ZC Users '), ('hostgroup', 'groupid', 'ZC Test '), ('templategroup', 'groupid', 'ZC Test ')]:
        ids = [r[idkey] for r in api.call(method + '.get', {'output': [idkey], 'filter': {'name': name + suffix}})]
        if ids:
            api.call(method + '.delete', ids)
    macros = api.call('usermacro.get', {'globalmacro': True, 'filter': {'macro': '{$ZC_MATRIX_' + suffix + '}'}})
    if macros:
        api.call('usermacro.deleteglobal', [m['globalmacroid'] for m in macros])
    group = api.call('hostgroup.create', {'name': 'ZC Test ' + suffix})['groupids'][0]
    tg = api.call('templategroup.create', {'name': 'ZC Test ' + suffix})['groupids'][0]
    template = api.call('template.create', {'host': 'ZC Template ' + suffix, 'groups': [{'groupid': tg}]})['templateids'][0]
    api.call('item.create', {'hostid': template, 'name': 'Agent fixture', 'key_': 'agent.ping', 'type': 0, 'value_type': 3, 'delay': '1m'})
    api.call('trigger.create', {'description': 'ZC Trigger ' + suffix, 'expression': f'last(/ZC Template {suffix}/agent.ping)=0', 'priority': 2})
    tags = [{'tag': 'ZC_WORKER', 'value': f'z{v}-target'} for v in VERSIONS]
    host = api.call('host.create', {
        'host': 'zc-test-' + suffix, 'name': 'ZC Test Host ' + suffix,
        'groups': [{'groupid': group}], 'templates': [{'templateid': template}],
        'status': 1, 'tags': tags, 'macros': [{'macro': '{$ZC_TEST}', 'value': suffix}],
        'interfaces': [
            {'type': 1, 'main': 1, 'useip': 1, 'ip': '192.0.2.10', 'dns': '', 'port': '10050'},
            {'type': 2, 'main': 1, 'useip': 1, 'ip': '192.0.2.11', 'dns': '', 'port': '161', 'details': {'version': 2, 'community': '{$SNMP_COMMUNITY}', 'bulk': 1}},
        ],
    })['hostids'][0]
    api.call('usermacro.createglobal', {'macro': '{$ZC_MATRIX_' + suffix + '}', 'value': 'fixture'})
    regex = {'name': 'ZC Regex ' + suffix, 'expressions': [{'expression': '^fixture$', 'expression_type': 3}]}
    if version == '80':
        regex['description'] = '8.0 description fixture'
    api.call('regexp.create', regex)
    pg = api.call('proxygroup.create', {'name': 'ZC Proxy Group ' + suffix, 'failover_delay': '1m', 'min_online': '1'})['proxy_groupids'][0]
    api.call('proxy.create', {'name': 'ZC Proxy ' + suffix, 'operating_mode': 0, 'proxy_groupid': pg, 'local_address': '192.0.2.20'})
    api.call('maintenance.create', {'name': 'ZC Maintenance ' + suffix, 'active_since': int(time.time()), 'active_till': int(time.time()) + 86400 * 30, 'hosts': [{'hostid': host}], 'timeperiods': [{'timeperiod_type': 0, 'start_date': int(time.time()) + 86400, 'period': 3600}]})
    api.call('script.create', {'name': 'ZC Script ' + suffix, 'type': 6, 'scope': 2, 'url': 'https://example.com/'})
    api.call('role.create', {'name': 'ZC Role ' + suffix, 'type': 1, 'rules': {'api.access': 1, 'api.mode': 1, 'api': []}})
    api.call('usergroup.create', {'name': 'ZC Users ' + suffix, 'hostgroup_rights': [{'id': group, 'permission': 2}]})
    api.call('action.create', {'name': 'ZC Action ' + suffix, 'eventsource': 0, 'status': 1, 'filter': {'evaltype': 0, 'conditions': [{'conditiontype': 1, 'operator': 0, 'value': host}]}, 'operations': [{'operationtype': 0, 'opmessage': {'default_msg': 1}, 'opmessage_usr': [{'userid': '1'}]}]})
    if version == '80':
        api.call('action.create', {'name': 'ZC Proxy Action ' + suffix, 'eventsource': 2, 'status': 1, 'filter': {'evaltype': 0, 'conditions': [{'conditiontype': 29, 'operator': 0, 'value': pg}]}, 'operations': [{'operationtype': 2}]})
    print('SEEDED', api.node, api.version, flush=True)


def run_cli(role, source, target=None, phase='initial'):
    version = source if role == 'master' else target
    node = f'z{version}-' + ('source' if role == 'master' else 'target')
    port = 18000 + int(version) * 10 + (role != 'master')
    store = OUT / ('store-' + source)
    store.mkdir(exist_ok=True)
    name = f'{source}-{target or source}-{role}-{phase}'
    log = OUT / (name + '.log')
    log.write_text('', encoding='utf-8')
    args = [str(OUT / 'zc.exe'), role, '--no.config.files', '--node', node,
            '--endpoint', f'http://127.0.0.1:{port}', '--user', 'Admin',
            '--store.type', 'file', '--file.store.path', str(store),
            '--yes', '--quiet', '--online', '--host.update',
            '--skip.template=false', '--log.file', str(OUT / (name + '.log'))]
    env = {k: v for k, v in os.environ.items() if not k.startswith('ZC_')}
    env['ZC_PASSWORD'] = 'zabbix'
    p = subprocess.run(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, encoding='utf-8', errors='replace', timeout=600)
    (OUT / (name + '.stdout')).write_text(p.stdout, encoding='utf-8')
    output = p.stdout + log.read_text(encoding='utf-8')
    errors = [line for line in output.splitlines() if '[ERROR]' in line or '[CRITICAL]' in line]
    if p.returncode or errors:
        raise RuntimeError(f'{name}: exit={p.returncode}\n' + '\n'.join(errors[-6:] or output.splitlines()[-6:]))


def verify(source, target, role, phase='initial'):
    api = API(target, 'target')
    host = api.one('host', 'zc-test-' + source, 'host', selectInterfaces='extend', selectTags='extend', selectParentTemplates='extend', selectMacros='extend')
    assert host['status'] == ('0' if role == 'worker' else '1'), host
    expected_ips = {'192.0.2.10', '192.0.2.11'} if phase == 'initial' else {'192.0.2.30', '192.0.2.31', '192.0.2.32'}
    assert len(host['interfaces']) == len(expected_ips), host['interfaces']
    assert {i['ip'] for i in host['interfaces']} == expected_ips, host['interfaces']
    if phase != 'initial':
        assert next(i for i in host['interfaces'] if i['ip'] == '192.0.2.31')['main'] == '1'
        assert next(i for i in host['interfaces'] if i['ip'] == '192.0.2.30')['main'] == '0'
    assert any(t['tag'] == 'ZC_UUID' for t in host['tags'])
    assert any(t['host'] == 'ZC Template ' + source for t in host['parentTemplates'])
    assert any(m['macro'] == '{$ZC_TEST}' and m['value'] == source for m in host['macros'])
    items = api.call('item.get', {'hostids': host['hostid'], 'output': ['key_', 'interfaceid']})
    item_ip = '192.0.2.10' if phase == 'initial' else '192.0.2.30'
    item_interface_id = next(i['interfaceid'] for i in host['interfaces'] if i['ip'] == item_ip)
    assert any(i['key_'] == 'agent.ping' and i['interfaceid'] == item_interface_id for i in items), items
    triggers = api.call('trigger.get', {'hostids': host['hostid'], 'output': ['description']})
    assert any(t['description'] == 'ZC Trigger ' + source for t in triggers), triggers
    macros = api.call('usermacro.get', {'globalmacro': True, 'filter': {'macro': '{$ZC_MATRIX_' + source + '}'}})
    assert len(macros) == 1 and macros[0]['value'] == 'fixture', macros
    for method, prefix in [('regexp','ZC Regex '), ('proxygroup','ZC Proxy Group '), ('proxy','ZC Proxy '), ('maintenance','ZC Maintenance '), ('script','ZC Script '), ('role','ZC Role '), ('usergroup','ZC Users '), ('action','ZC Action ')]:
        api.one(method, prefix + source)
    action = api.one('action', 'ZC Action ' + source, selectFilter='extend')
    assert action['filter']['conditions'][0]['value'] == host['hostid'], action
    maintenance = api.one('maintenance', 'ZC Maintenance ' + source, selectHosts=['hostid'])
    assert [h['hostid'] for h in maintenance['hosts']] == [host['hostid']], maintenance
    proxygroup = api.one('proxygroup', 'ZC Proxy Group ' + source)
    assert api.one('proxy', 'ZC Proxy ' + source)['proxy_groupid'] == proxygroup['proxy_groupid']
    rights = api.one('usergroup', 'ZC Users ' + source, selectHostGroupRights='extend')['hostgroup_rights']
    group = api.one('hostgroup', 'ZC Test ' + source)
    assert any(r['id'] == group['groupid'] and r['permission'] == '2' for r in rights), rights
    rules = api.one('role', 'ZC Role ' + source, selectRules='extend')['rules']
    assert rules['api.access'] == '1', rules
    assert rules['api.mode'] == ('0' if source != '80' and target == '80' else '1'), rules
    if source == '80':
        assert api.one('regexp', 'ZC Regex 80')['description'] == '8.0 description fixture'
        pg = api.one('proxygroup', 'ZC Proxy Group 80')['proxy_groupid']
        assert api.one('action', 'ZC Proxy Action 80', selectFilter='extend')['filter']['conditions'][0]['value'] == pg
    return api.version


def test_pair(source, target, phase):
    rows = []
    for role in ['replica', 'worker']:
        row = dict(source=source, target=target, role=role, phase=phase)
        try:
            before_server_id = API(target, 'target').call('settings.get', {'output': ['serverid']})['serverid'] if target == '80' else None
            previous_id = None
            if phase != 'initial':
                api = API(target, 'target')
                previous = api.one('host', 'zc-test-' + source, 'host', selectInterfaces='extend')
                previous_id = next(i['interfaceid'] for i in previous['interfaces'] if i['ip'] in ['192.0.2.10','192.0.2.30'])
            run_cli(role, source, target, phase)
            row['actual_target'] = verify(source, target, role, phase)
            if before_server_id is not None:
                assert API(target, 'target').call('settings.get', {'output': ['serverid']})['serverid'] == before_server_id, 'Target server identity changed'
            if previous_id:
                updated = api.one('host', 'zc-test-' + source, 'host', selectInterfaces='extend')
                assert next(i['interfaceid'] for i in updated['interfaces'] if i['ip'] == '192.0.2.30') == previous_id, 'Interface ID changed'
            row['status'] = 'PASS'
        except Exception as exc:
            row['status'] = 'FAIL'
            row['error'] = str(exc)
        rows.append(row)
        print(json.dumps(row), flush=True)
    return rows


def matrix(phase='initial'):
    results = []
    for source in sys.argv[3:] or VERSIONS:
        try:
            run_cli('master', source, phase=phase)
        except Exception as exc:
            print('MASTER FAILED', source, str(exc), flush=True)
            results.append(dict(source=source, phase='master', error=str(exc)))
            continue
        with ThreadPoolExecutor(max_workers=4) as pool:
            futures = [pool.submit(test_pair, source, target, phase) for target in VERSIONS[VERSIONS.index(source):]]
            for future in futures:
                results.extend(future.result())
                (OUT / f'results-{phase}.json').write_text(json.dumps(results, indent=2), encoding='utf-8')
    (OUT / f'results-{phase}.json').write_text(json.dumps(results, indent=2), encoding='utf-8')
    if any(row.get('status') != 'PASS' for row in results):
        raise SystemExit(1)


def mutate():
    for version in VERSIONS:
        api = API(version, 'source')
        host = api.one('host', 'zc-test-' + version, 'host', selectInterfaces='extend')
        agent = next(i for i in host['interfaces'] if i['type'] == '1')
        api.call('host.update', {'hostid': host['hostid'], 'interfaces': [
            {'interfaceid': agent['interfaceid'], 'type': 1, 'main': 0, 'useip': 1, 'ip': '192.0.2.30', 'dns': '', 'port': '10050'},
            {'type': 1, 'main': 1, 'useip': 1, 'ip': '192.0.2.31', 'dns': '', 'port': '10050'},
            {'type': 4, 'main': 1, 'useip': 1, 'ip': '192.0.2.32', 'dns': '', 'port': '12345'},
        ]})
        print('MUTATED', version, flush=True)


if __name__ == '__main__':
    if sys.argv[1] == 'probe':
        for version in VERSIONS:
            for role in ['source', 'target']:
                api = API(version, role)
                print(api.node, api.version, flush=True)
    elif sys.argv[1] == 'seed':
        for version in sys.argv[2:] or VERSIONS:
            seed(version)
    elif sys.argv[1] == 'matrix':
        matrix(sys.argv[2] if len(sys.argv) > 2 else 'initial')
    elif sys.argv[1] == 'mutate':
        mutate()
