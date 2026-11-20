"""Positive/negative credential check against the disposable ClickHouse."""
import json
import sys
import time
import cep_apm as t


def main():
    t.RESULTS[:] = [r for r in json.loads((t.OUT/'results.json').read_text()) if r['status']=='PASS']
    source, target = t.compat.API('80','source'), t.compat.API('80','target')
    if '--verify-only' not in sys.argv:
        source.call('settings.update', {'apm_global_db':{'status':1,'url':t.DB_URL,'authentication_type':0,
            'username':'zc','password':t.SOURCE_SECRET,'db':'otel'}})
        t.cli('master',phase='secret-connection')
        t.cli('replica',phase='secret-connection')
    params = {'time_from':int(time.time())-300,'time_till':int(time.time())+60,'countOutput':True}
    try:
        source.call('apmtrace.get',params)
    except RuntimeError as exc:
        assert 'database error' in str(exc).lower(), str(exc)
    else:
        raise AssertionError('source-only password unexpectedly authenticated to target ClickHouse')
    code = t.kubectl('exec','schema-bash','--','curl','-sS','-o','/dev/null','-w','%{http_code}',
                     '-u','zc:'+t.SOURCE_SECRET,'--data-binary','SELECT 1','http://clickhouse:8123').strip()
    assert code in ('401','403'), code
    for method in ['apmtrace.get','apmspan.get','apmmetric.get','apmlog.get']:
        assert int(target.call(method,params)) >= 0
    t.record('APM API connection accepts target secret and rejects source secret')
    t.check_store()


if __name__ == '__main__':
    main()
