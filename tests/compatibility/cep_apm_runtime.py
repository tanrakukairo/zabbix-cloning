"""Runtime extension of cep_apm.py; requires its disposable addons and forwards."""
import json
import socket
import struct
import sys
import time
import uuid

import grpc
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest
from opentelemetry.proto.collector.trace.v1.trace_service_pb2_grpc import TraceServiceStub
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2 import ExportMetricsServiceRequest
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2_grpc import MetricsServiceStub
from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import ExportLogsServiceRequest
from opentelemetry.proto.collector.logs.v1.logs_service_pb2_grpc import LogsServiceStub
import cep_apm as t


def wait_until(check, timeout=90):
    deadline = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except Exception as exc:
            last_error = exc
        time.sleep(2)
    raise AssertionError('Timed out: ' + str(last_error))


def sql(query):
    return t.kubectl('exec', 'clickhouse', '--', 'clickhouse-client', '--user', 'zc',
                     '--password', t.TARGET_SECRET, '--query', query).strip()


def sender(value):
    now = time.time_ns()
    body = json.dumps({'request':'sender data', 'data':[
        {'host':'zc-cep-live', 'key':'zc.cep.event', 'value':str(value), 'clock':now//1_000_000_000, 'ns':now%1_000_000_000}]}).encode()
    with socket.create_connection(('127.0.0.1',18851), timeout=15) as s:
        s.sendall(b'ZBXD\x01' + struct.pack('<Q',len(body)) + body)
        header = b''
        while len(header) < 13:
            chunk = s.recv(13-len(header))
            if not chunk:
                raise ConnectionError('sender response closed before header')
            header += chunk
        length = struct.unpack('<Q',header[5:])[0]
        response = b''
        while len(response) < length:
            chunk = s.recv(length-len(response))
            if not chunk:
                raise ConnectionError('sender response closed before body')
            response += chunk
        data = json.loads(response)
        return data.get('response') == 'success' and 'failed: 0' in data.get('info','')


def main():
    if (t.OUT/'results.json').exists():
        t.RESULTS[:] = [r for r in json.loads((t.OUT/'results.json').read_text()) if r['status']=='PASS']
    source, target = t.compat.API('80','source'), t.compat.API('80','target')
    source.call('settings.update', {'apm_global_db':{'status':1, 'url':t.DB_URL, 'authentication_type':0,
        'username':'zc', 'password':t.SOURCE_SECRET, 'db':'otel'}})
    source.call('proxy.update', {'proxyid':t.proxy(source)['proxyid'], 'apm':{'data_collection_status':1,
        'max_messages_per_second':1000, 'additional_resource_attributes':[
            {'key':'zc.fixture', 'value':'collected', 'signal_type':i} for i in range(3)]}})
    rule0 = source.one('ceprule','ZC CEP 0')
    source.call('ceprule.update', {'cep_ruleid':rule0['cep_ruleid'], 'status':0, 'stop':1})
    group = source.one('hostgroup','ZC Test 80')['groupid']
    old_hosts = source.call('host.get', {'output':['hostid'], 'filter':{'host':'zc-cep-live'}})
    if old_hosts:
        source.call('host.delete', [h['hostid'] for h in old_hosts])
    template_name = 'ZC CEP Runtime Template'
    old_templates = source.call('template.get', {'output':['templateid'], 'filter':{'host':template_name}})
    if old_templates:
        source.call('template.delete', [r['templateid'] for r in old_templates])
    template_group = source.one('templategroup','ZC Test 80')['groupid']
    template = source.call('template.create', {'host':template_name,'groups':[{'groupid':template_group}]})['templateids'][0]
    source.call('item.create', {'hostid':template, 'name':'CEP event input', 'key_':'zc.cep.event', 'type':2, 'value_type':3})
    source.call('trigger.create', {'description':'ZC CEP original', 'expression':f'last(/{template_name}/zc.cep.event)=1',
        'tags':[{'tag':'zc.fixture','value':'yes'},{'tag':'service.name','value':'zc-fixture'}]})
    source.call('host.create', {'host':'zc-cep-live', 'groups':[{'groupid':group}], 'status':0, 'templates':[{'templateid':template}]})
    t.cli('master', phase='runtime')
    before_ids = {r['cep_ruleid'] for r in t.rules(target)}
    t.cli('replica', phase='runtime-full', extra=['--initialize.full'], confirmation='y\ny\n')
    t.assert_same(source,target)
    assert before_ids.isdisjoint({r['cep_ruleid'] for r in t.rules(target)})
    t.record('8.0 initialize.full deletes and recreates CEP; restores APM')
    t.kubectl('exec','z80-target','-c','server','--','zabbix_server','-R','config_cache_reload')
    wait_until(lambda: sender(0))
    assert sender(1)
    live_host = target.one('host','zc-cep-live','host')['hostid']
    problems = wait_until(lambda: target.call('problem.get', {'output':'extend','hostids':[live_host]}))
    assert any(p['name']=='ZC CEP processed' for p in problems), problems
    t.record('CEP transforms a real trapper-trigger event name')
    telemetry(target)
    print('RUNTIME COMPLETE', flush=True)


def telemetry(target):
    cpuinfo = t.kubectl('exec', 'apm-proxy', '--', 'cat', '/proc/cpuinfo')
    if 'avx2' not in cpuinfo.split():
        t.record('APM collection end to end', status='BLOCKED',
                 reason='t2dev does not expose AVX2; the supported ClickHouse 26.9 image exits with SIGILL. ClickHouse 26.3 cannot parse APM fractional timestamps.')
        return
    trace_id = uuid.uuid4().bytes
    span_id = uuid.uuid4().bytes[:8]
    now = time.time_ns()
    traces = ExportTraceServiceRequest()
    rs = traces.resource_spans.add()
    attr = rs.resource.attributes.add(key='service.name')
    attr.value.string_value = 'zc-fixture'
    rs.scope_spans.add().spans.add(trace_id=trace_id, span_id=span_id, name='zc-live-span', kind=2,
                                 start_time_unix_nano=now, end_time_unix_nano=now+1_000_000)
    metrics = ExportMetricsServiceRequest()
    rm = metrics.resource_metrics.add()
    rm.resource.CopyFrom(rs.resource)
    metric = rm.scope_metrics.add().metrics.add(name='zc.live.gauge')
    metric.gauge.data_points.add(time_unix_nano=now, as_double=42)
    logs = ExportLogsServiceRequest()
    rl = logs.resource_logs.add()
    rl.resource.CopyFrom(rs.resource)
    log = rl.scope_logs.add().log_records.add(time_unix_nano=now, observed_time_unix_nano=now,
                                            severity_number=9, severity_text='INFO', trace_id=trace_id, span_id=span_id)
    log.body.string_value = 'ZC live log'
    with grpc.insecure_channel('127.0.0.1:14317') as channel:
        wait_until(lambda: (TraceServiceStub(channel).Export(traces,timeout=10), True)[1])
        MetricsServiceStub(channel).Export(metrics,timeout=10)
        LogsServiceStub(channel).Export(logs,timeout=10)
    counts = {}
    for name, table in [('traces','otel_traces'),('metrics','otel_metrics_gauge'),('logs','otel_logs')]:
        counts[name] = wait_until(lambda table=table: int(sql(f"SELECT count() FROM otel.{table} WHERE ResourceAttributes['zc.fixture']='collected'")))
    t.record('OTLP traces metrics logs reach ClickHouse with cloned resource attributes', counts=counts)
    params = {'time_from':int(now//1_000_000_000)-60, 'time_till':int(time.time())+60, 'countOutput':True}
    api_counts = {}
    for method in ['apmtrace.get','apmspan.get','apmmetric.get','apmlog.get']:
        api_counts[method] = int(target.call(method, params))
        assert api_counts[method] > 0, method
    t.record('Zabbix APM APIs read ClickHouse using target-only secret', counts=api_counts)
    t.check_store()


if __name__ == '__main__':
    if '--telemetry-only' in sys.argv:
        t.RESULTS[:] = [r for r in json.loads((t.OUT/'results.json').read_text()) if r['status']=='PASS']
        telemetry(t.compat.API('80','target'))
    else:
        main()
