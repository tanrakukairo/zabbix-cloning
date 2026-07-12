#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import sys
import json

from zc.common import *
from zc.common import __LOGGER__, inputParameters
from zc.master.main import ZabbixMaster

def show_versions(node, id_only):
    title = 'In Store Versions:'
    print(f'{title}{B_CHAR*(WIDE_COUNT-len(title))}')
    for ver in node.VERSIONS:
        if id_only:
            vId = ver['VERSION_ID']
            unixtime = ver['UNIXTIME']
            print(f'{TAB}{vId}: {unixtime}')
        else:
            output = json.dumps(ver, indent=TAB)
            print(f'{TAB}' + output.replace('\n', f'\n{TAB}'))
            print(f'{TAB}{BD}')

def show_data(store, target_method, target_name, id_only):
    for method, items in store.items():
        if not target_method or method in target_method:
            print(f'{method}:{B_CHAR*(WIDE_COUNT-len(method)-1)}')
            items = sorted(items, key=lambda x:x['NAME'])
            for item in items:
                if not target_name or item['NAME'] in target_name:
                    if id_only:
                        name = item['NAME']
                        dId = item.get('DATA_ID', 'DIRECT-MODE')
                        print(f'{TAB}{dId}: {name}')
                    else:
                        output = json.dumps(item, indent=TAB)
                        print(f'{TAB}' + output.replace('\n', f'\n{TAB}'))
                        print(f'{TAB}{BD}')

def load_store(config, version):
    node = ZabbixCloneDatastore(config)
    result = node.getVersionFromStore(version)
    if not result[0]:
        sys.exit(result[1])
    if not node.VERSIONS:
        sys.exit(f'No Exist Version: {version}')

    target = node.VERSIONS[0]
    result = node.getDataFromStore(target)
    if not result[0]:
        sys.exit(result[1])
    if isinstance(result[1], list):
        store = {}
        for item in result[1]:
            method = item['METHOD']
            if not store.get(method):
                store[method] = []
            store[method].append(item)
        return store
    return node.STORE

def load_direct(config):
    config.changeDirectMaster()
    node = ZabbixMaster(config)
    node.VERSIONS = [
        {
            'VERSION_ID': 'DIRECT-MODE',
            'UNIXTIME': UNIXTIME(),
            'MASTER_VERSION': 0,
            'DESCRIPTION': 'DIRECT-MODE'
        }
    ]
    result = node.firstProcess()
    if not result[0]:
        sys.exit(result[1])
    result = node.getDataFromZabbix()
    if not result[0]:
        sys.exit(result[1])
    result = node.createNewData()
    if not result[0]:
        sys.exit(result[1])
    return node.STORE

def main():
    params = inputParameters(mode='view')
    if not params:
        sys.exit('wrong parameters')

    logConfig = DEFAULT_LOG
    logConfig['logLevel'] = params.get('log_level', DEFAULT_LOG_LEVEL)
    logConfig['logName'] = params.get('log_name', 'ZabbixCloneView')
    logFileConfig = DEFAULT_LOG_FILE
    if params.get('log_file'):
        logFileConfig = DEFAULT_LOG_FILE.copy()
        logFileConfig['option'] = DEFAULT_LOG_FILE.get('option', {}).copy()
        logFileConfig['option']['filename'] = params['log_file']
    logConfig['logHandlers'] = [logFileConfig]
    LOGGER = __LOGGER__(**logConfig)
    params['LOGGER'] = LOGGER
    params['quiet'] = True
    params['role'] = 'worker'

    command = params.pop('command')
    targetMethod = params.pop('method', None)
    targetName = params.pop('name', None)
    idOnly = params.pop('id_only', None)

    config = ZabbixCloneConfig(**params)
    print(f'STORE TYPE:[ {config.storeType} ] / COMMAND: {command}')

    if command == 'showversions':
        if config.storeType == 'direct':
            print('DirectMode Connot Execute showversions.')
            sys.exit(0)
        node = ZabbixCloneDatastore(config)
        result = node.getVersionFromStore()
        if not result[0]:
            sys.exit(result[1])
        show_versions(node, idOnly)
    elif command == 'showdata':
        if config.storeType == 'direct':
            store = load_direct(config)
        else:
            version = params.get('version')
            if not version:
                sys.exit(f'{command} Required --version.')
            store = load_store(config, version)
        show_data(store, targetMethod, targetName, idOnly)

    sys.exit(0)

if __name__ == '__main__':
    main()
