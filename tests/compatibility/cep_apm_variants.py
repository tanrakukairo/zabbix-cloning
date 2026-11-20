"""Additional update/delete variants for the disposable CEP/APM environment."""
import json
import cep_apm as t


def main():
    t.RESULTS[:] = [r for r in json.loads((t.OUT/'results.json').read_text()) if r['status']=='PASS']
    source, target = t.compat.API('80','source'), t.compat.API('80','target')
    source.call('proxy.update', {'proxyid':t.proxy(source)['proxyid'], 'apm':{
        'data_collection_status':1, 'max_messages_per_second':0, 'additional_resource_attributes':[]}})
    variants = [
        ('https', dict(status=1,url='https://clickhouse:8443',authentication_type=0,username='zc',password=t.SOURCE_SECRET,db='otel',ssl_verify_peer=1,ssl_verify_host=1)),
        ('https-no-verify', dict(ssl_verify_peer=0)),
        ('http-no-auth', dict(url=t.DB_URL,authentication_type=1)),
    ]
    for name, value in variants:
        source.call('settings.update', {'apm_global_db':value})
        t.cli('master', phase=name)
        t.cli('replica', phase=name)
        t.assert_same(source,target)
        assert t.proxy(target)['apm']['additional_resource_attributes'] == []
        t.record('APM update '+name+' and empty resource attributes')
    stale = target.call('ceprule.create', {'name':'ZC obsolete CEP', 'sortorder':100,
        'window_type':2, 'window':{'group_by_host':1}, 'operations':[], 'filter':{'evaltype':0,'conditions':[]}})['cep_ruleids'][0]
    before_ids = {r['name']:r['cep_ruleid'] for r in t.rules(target) if r['cep_ruleid'] != stale}
    t.cli('replica', phase='cep-delete', extra=['--delete.api'])
    assert all(r['cep_ruleid'] != stale for r in t.rules(target))
    assert before_ids == {r['name']:r['cep_ruleid'] for r in t.rules(target)}
    t.record('8.0 delete.api removes obsolete CEP and preserves desired IDs')
    t.check_store()


if __name__ == '__main__':
    main()
