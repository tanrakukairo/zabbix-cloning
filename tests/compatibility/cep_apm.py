"""Destructive feature tests: disposable zc-cep-apm-20261120 namespace only.

Requires the forwards and deployment described in README.md. No production URLs
are accepted. Test credentials are deliberately unrelated to real credentials.
"""
import bz2
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import time

os.environ.setdefault('ZC_COMPAT_OUT', str(Path(__file__).resolve().parents[2] / '.tools' / 'cep-apm-live'))
import matrix as compat

OUT = compat.OUT
NS = 'zc-cep-apm-20261120'
SOURCE_SECRET = 'zc-source-fixture-only'
TARGET_SECRET = 'zc-target-fixture-only'
DB_URL = 'http://clickhouse:8123'
RESULTS = []


def record(name, status='PASS', **details):
    RESULTS.append(dict(test=name, status=status, **details))
    (OUT / 'results.json').write_text(json.dumps(RESULTS, indent=2), encoding='utf-8')
    print(status, name, flush=True)


def kubectl(*args, input=None):
    p = subprocess.run(['kubectl', '--context', 't2dev', '-n', NS, *args],
                       input=input, text=True, capture_output=True, timeout=60)
    if p.returncode:
        raise RuntimeError(p.stderr)
    return p.stdout


def cli(role, source='80', phase='initial', secret=True, extra=(), confirmation=None):
    node = f'z{source}-source' if role == 'master' else 'z80-target'
    port = 18000 + int(source) * 10 if role == 'master' else 18801
    store = OUT / ('store-' + source)
    store.mkdir(exist_ok=True)
    label = f'{source}-{role}-{phase}'
    log = OUT / (label + '.log')
    log.write_text('', encoding='utf-8')
    args = [str(OUT / 'zc.exe'), role, '--no.config.files', '--node', node,
            '--endpoint', f'http://127.0.0.1:{port}', '--user', 'Admin',
            '--store.type', 'file', '--file.store.path', str(store),
            '--online', '--host.update', '--skip.template=false', '--log.file', str(log)]
    if confirmation is None:
        args += ['--yes', '--quiet']
    if role != 'master' and secret:
        args += ['--secret.file', str(OUT / 'zc.secret')]
    args += list(extra)
    env = {k:v for k,v in os.environ.items() if not k.startswith(('ZC_', 'ZABBIX_'))}
    env['ZC_PASSWORD'] = 'zabbix'
    p = subprocess.run(args, env=env, input=confirmation, text=True, encoding='utf-8',
                       errors='replace', stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=600)
    (OUT / (label + '.stdout')).write_text(p.stdout, encoding='utf-8')
    output = p.stdout + log.read_text(encoding='utf-8')
    errors = [line for line in output.splitlines() if '[ERROR]' in line or '[CRITICAL]' in line]
    if p.returncode or errors:
        raise RuntimeError(label + ': ' + '\n'.join(errors[-6:] or output.splitlines()[-6:]))
    return output


def rules(api):
    return api.call('ceprule.get', dict(output='extend', selectFilter='extend',
                                      selectWindow='extend', selectOperations='extend'))


def data_source(api):
    data = api.call('settings.get', {'output':['apm_global_db']})['apm_global_db']
    data.pop('password', None)
    return data


def proxy(api):
    return api.one('proxy', 'ZC APM Proxy', selectApm='extend')


def clean_rule(rule):
    rule = copy.deepcopy(rule)
    rule.pop('cep_ruleid', None)
    rule.pop('error', None)
    return rule


def assert_same(source, target):
    left = {r['name']: clean_rule(r) for r in rules(source)}
    right = {r['name']: clean_rule(r) for r in rules(target)}
    assert left == right, ('CEP differs', left, right)
    assert data_source(source) == data_source(target), 'APM data source differs'
    assert proxy(source)['apm'] == proxy(target)['apm'], 'proxy APM differs'
    source_group = source.one('usergroup', 'ZC APM Users', selectProxies='extend', selectProxyGroups='extend', selectApmFilters='extend')
    target_group = target.one('usergroup', 'ZC APM Users', selectProxies='extend', selectProxyGroups='extend', selectApmFilters='extend')
    assert target_group['apm_filters'] == source_group['apm_filters']
    assert [p['proxyid'] for p in target_group['proxies']] == [proxy(target)['proxyid']]
    assert proxy(source)['proxyid'] != proxy(target)['proxyid'], 'fixture must use different proxy IDs'


def seed_features(source, target):
    # Defaults would make template export slow and obscure feature-only failures.
    compat.seed('80')
    compat.seed('74')
    for api in (source, target):
        existing = rules(api)
        if existing:
            api.call('ceprule.delete', [r['cep_ruleid'] for r in existing])
        groups = api.call('usergroup.get', {'output':['usrgrpid'], 'filter':{'name':'ZC APM Users'}})
        if groups:
            api.call('usergroup.delete', [g['usrgrpid'] for g in groups])
        old_proxies = api.call('proxy.get', {'output':['proxyid'], 'filter':{'name':['ZC APM Proxy','ID spacer']}})
        if old_proxies:
            api.call('proxy.delete', [p['proxyid'] for p in old_proxies])
    source.call('proxy.create', {'name':'ZC APM Proxy', 'operating_mode':0, 'apm':{
        'data_collection_status':1, 'max_messages_per_second':123,
        'additional_resource_attributes':[{'key':'zc.fixture', 'value':'initial', 'signal_type':i} for i in range(3)]}})
    target.call('proxy.create', {'name':'ID spacer', 'operating_mode':0})
    target.call('proxy.create', {'name':'ZC APM Proxy', 'operating_mode':0})
    source.call('usergroup.create', {'name':'ZC APM Users', 'proxies':[{'proxyid':proxy(source)['proxyid']}],
        'apm_filters':[{'key':'service.name', 'value':'zc-fixture', 'signal_type':i} for i in range(3)]})
    source.call('settings.update', {'apm_global_db':{
        'status':1, 'url':DB_URL, 'authentication_type':0, 'username':'zc', 'password':SOURCE_SECRET, 'db':'otel'}})
    for window_type in range(5):
        rule = {'name':f'ZC CEP {window_type}', 'sortorder':window_type + 10, 'status':0,
                'window_type':window_type, 'filter':{'evaltype':3, 'formula':'A and B', 'conditions':[
                    {'type':26, 'operator':0, 'tag':'zc.fixture', 'tag_value':'yes', 'formulaid':'A'},
                    {'type':36, 'operator':2, 'host_name':'zc', 'formulaid':'B'}]}}
        if window_type:
            rule['window'] = {'duration':'1m', 'capacity':10, 'group_by_host':1, 'group_by_tags':1, 'tags':['service.name']}
        if window_type == 2:
            rule['window']['event_count_tag'] = 'zc_count'
            rule['operations'] = []
        else:
            execute_when = 4 if window_type == 4 else 0
            rule['operations'] = [{'sortorder':1, 'execute_when':execute_when, 'type':1, 'event_name':'ZC CEP processed',
                'scope':2 if window_type == 4 else 0,
                'filter':{'evaltype':0, 'conditions':[]},
                'scope_filter':{'evaltype':3, 'formula':'A', 'conditions':[{'type':26,'operator':0,'tag':'service.name','tag_value':'zc-fixture','formulaid':'A'}]} if window_type == 4 else {'evaltype':0,'conditions':[]}}]
        if window_type == 4:
            rule['window']['script'] = 'return true;'
        source.call('ceprule.create', rule)


def check_store():
    latest = max((OUT / 'store-80').glob('*.bz2'), key=lambda p:p.stat().st_mtime_ns)
    raw = bz2.decompress(latest.read_bytes()).decode('utf-8')
    assert SOURCE_SECRET not in raw and TARGET_SECRET not in raw, 'password leaked into store'
    assert 'ZC CEP 4' in raw and 'apm_global_db' in raw
    record('store contains CEP/APM configuration and excludes passwords')


def main():
    OUT.mkdir(exist_ok=True, parents=True)
    (OUT / 'zc.secret').write_text(json.dumps({'settings':{'apm_global_db':{'password':TARGET_SECRET}}}), encoding='utf-8')
    source, target = compat.API('80','source'), compat.API('80','target')
    compat.API('74','source')
    seed_features(source, target)
    record('seed all five CEP window types and APM fixtures')
    cli('master')
    check_store()
    cli('replica')
    assert_same(source,target)
    record('8.0 master to replica creates CEP and APM with target IDs')
    cli('worker', phase='reapply')
    assert_same(source,target)
    record('8.0 worker reapplies CEP and APM')
    before = (rules(target), data_source(target), proxy(target)['apm'])
    cli('replica', phase='dry-run', extra=['--dry.run'])
    assert before == (rules(target), data_source(target), proxy(target)['apm'])
    record('dry-run makes no CEP/APM changes')
    ids = {r['name']:r['cep_ruleid'] for r in rules(target)}
    for rule in rules(source):
        source.call('ceprule.update', {'cep_ruleid':rule['cep_ruleid'], 'description':'updated', 'status':1, 'sortorder':int(rule['sortorder'])+10})
    source.call('proxy.update', {'proxyid':proxy(source)['proxyid'], 'apm':{'data_collection_status':0}})
    source.call('settings.update', {'apm_global_db':{'status':0}})
    cli('master', phase='updated')
    cli('replica', phase='updated')
    assert_same(source,target)
    assert ids == {r['name']:r['cep_ruleid'] for r in rules(target)}, 'update recreated CEP rules'
    record('CEP update preserves IDs; APM source and collection disable')
    cli('master', source='74', phase='legacy')
    target.call('settings.update', {'apm_global_db':{'status':1, 'url':DB_URL, 'authentication_type':0,
                                                   'username':'zc', 'password':TARGET_SECRET, 'db':'otel'}})
    before = (rules(target), data_source(target))
    cli('replica', source='74', phase='legacy-full', secret=False, extra=['--initialize.full'], confirmation='y\ny\n')
    assert (rules(target), data_source(target)) == before
    record('7.4 store with initialize.full preserves CEP and APM data source')
    legacy_proxy = target.one('proxy', 'ZC Proxy 74', selectApm='extend')
    target.call('proxy.update', {'proxyid':legacy_proxy['proxyid'], 'apm':{'data_collection_status':1}})
    cli('replica', source='74', phase='legacy-delete', secret=False, extra=['--delete.api'])
    assert (rules(target), data_source(target)) == before
    assert target.one('proxy', 'ZC Proxy 74', selectApm='extend')['apm']['data_collection_status'] == '1'
    record('7.4 store with delete.api preserves CEP and existing APM fields')
    print('COMPLETE', len(RESULTS), 'checks', flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        RESULTS.append({'test':'run', 'status':'FAIL', 'error':str(exc)})
        OUT.mkdir(exist_ok=True, parents=True)
        (OUT / 'results.json').write_text(json.dumps(RESULTS, indent=2), encoding='utf-8')
        raise
