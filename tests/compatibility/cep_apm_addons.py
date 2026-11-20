"""Deploy disposable ClickHouse and APM proxy into the feature-test namespace."""
import json
import subprocess

NS = 'zc-cep-apm-20261120'


def pod(name, app, image, env, memory, volume=False):
    container = dict(name=app, image=image, env=[dict(name=k, value=v) for k,v in env.items()],
                     resources=dict(requests=dict(cpu='100m', memory='128Mi'), limits=dict(memory=memory)))
    spec = dict(containers=[container])
    if volume:
        container['volumeMounts'] = [dict(name='data', mountPath='/var/lib/clickhouse')]
        spec['volumes'] = [dict(name='data', emptyDir={})]
    return dict(apiVersion='v1', kind='Pod', metadata=dict(name=name, namespace=NS, labels=dict(app=app)), spec=spec)


def service(name, selector, port):
    return dict(apiVersion='v1', kind='Service', metadata=dict(name=name, namespace=NS),
                spec=dict(selector=dict(app=selector), ports=[dict(port=port, targetPort=port)]))


items = [
    pod('clickhouse', 'clickhouse', 'clickhouse/clickhouse-server:26.9',
        dict(CLICKHOUSE_DB='otel', CLICKHOUSE_USER='zc', CLICKHOUSE_PASSWORD='zc-target-fixture-only',
             CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT='1'), '2Gi', True),
    service('clickhouse', 'clickhouse', 8123),
    service('z80-target', 'z80-target', 10051),
    pod('apm-proxy', 'proxy', 'zabbix/zabbix-proxy-sqlite3:alpine-8.0.0',
        dict(ZBX_HOSTNAME='ZC APM Proxy', ZBX_SERVER_HOST='z80-target', ZBX_STARTAPMCOLLECTOR='1',
             ZBX_APMLISTENIP='0.0.0.0', ZBX_CONFIGFREQUENCY='10'), '512Mi'),
]
helper = pod('schema-bash', 'schema-bash', 'alpine:3.22', {}, '128Mi')
helper['spec']['containers'][0]['command'] = ['sh', '-c', 'apk add --no-cache bash curl && sleep 3600']
items.append(helper)
subprocess.run(['kubectl', '--context', 't2dev', 'apply', '-f', '-'],
               input=json.dumps(dict(apiVersion='v1', kind='List', items=items)), text=True, check=True)
