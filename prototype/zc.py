#!/usr/bin/env python3
# -*- coding: utf-8 -*-
'''
Zabbix Cloning: Zabbix monitoring settings cloning tool.
'''
import sys
import json

from zc.common import (
    DEFAULT_LOG, DEFAULT_LOG_LEVEL, DEFAULT_LOG_FILE, DEFAULT_LOG_STREAM,
    __LOGGER__, ZABBIX_TIME, PRINT_PROG, PRINT_TAB, TAB, ZC_COMPLETE,
    ZabbixCloneConfig, inputParameters
)
from zc.master.main import ZabbixMaster
from zc.replica.main import ZabbixReplica

def main():
    params = inputParameters(mode='clone')
    if not params:
        sys.exit('wrong parameters')

    logConfig = DEFAULT_LOG
    logConfig['logLevel'] = params.get('log_level', DEFAULT_LOG_LEVEL)
    logConfig['logName'] = params.get('log_name', 'ZabbixClone')
    logFileConfig = DEFAULT_LOG_FILE
    if params.get('log_file'):
        logFileConfig = DEFAULT_LOG_FILE.copy()
        logFileConfig['option'] = DEFAULT_LOG_FILE.get('option', {}).copy()
        logFileConfig['option']['filename'] = params['log_file']
    if not params.get('quiet'):
        logConfig['logHandlers'] = [logFileConfig, DEFAULT_LOG_STREAM]
    else:
        logConfig['logHandlers'] = [logFileConfig]
    LOGGER = __LOGGER__(**logConfig)
    params['LOGGER'] = LOGGER

    params.pop('command', 'clone')
    quiet = params.get('quiet', False)

    config = ZabbixCloneConfig(**params)
    if config.role == 'master':
        node = ZabbixMaster(config)
    else:
        node = ZabbixReplica(config)

    if config.storeType == 'direct':
        logConfig['logName'] = 'DirectMaster'
        params['LOGGER'] = __LOGGER__(**logConfig)
        directConfig = ZabbixCloneConfig(**params)
        directConfig.changeDirectMaster()
        master = ZabbixMaster(directConfig)

    config.showParameters()
    if not config.yes:
        if not config.quiet:
            inputKey = input('\nContinue? [y/N]: ')
            if inputKey.upper() in ['Y', 'YES']:
                pass
            else:
                LOGGER.info('[USER ABORT]')
                sys.exit()
        else:
            LOGGER.info('[DO NOT START]')
            sys.exit()

    PRINT_PROG('\n', config.quiet)
    LOGGER.info(f'[START] {ZABBIX_TIME()}')

    functions = [
        ['firstProcess', None]
    ]

    if node.isMaster:
        functions += [
            ['createNewData',         None],
            ['setVersionDataToStore', None]
        ]
    else:
        if config.updatePassword is True:
            functions += [
                ['changePassword', None]
            ]

        if config.storeType == 'direct':
            functions += ['getDataFromStore', {'master': master}],
        else:
            functions += ['getDataFromStore', None],

        functions += [
            ['setGlobalsettingsToZabbix', None],
            ['setApiToZabbix',            {'section': 'PRE'}],
            ['setConfigurationToZabbix',  None],
            ['setAlertStopInUpdate',      None],
            ['setApiToZabbix',            {'section': 'MID'}],
        ]
        if not config.hostSkip:
            functions += [
                ['setHostToZabbix',       None],
            ]
        functions += [
            ['execCheckNow',              None],
            ['setApiToZabbix',            {'section': 'POST'}],
            ['setApiToZabbix',            {'section': 'ACCOUNT'}],
            ['setApiToZabbix',            {'section': 'EXTEND'}],
            ['setAuthenticationToZabbix', None],
            ['setAlertMedia',             None],
        ]

    functions += [
        ['setVersionCode', None],
    ]

    for function in functions:
        func = function[0]
        option = function[1]
        if not quiet:
            execute = f'{config.role}({config.node}).{func}'
            PRINT_PROG(f'{TAB}{execute}:\n', config.quiet)
        try:
            if option:
                result = getattr(node, func)(**option)
            else:
                result = getattr(node, func)()
        except Exception as e:
            PRINT_PROG('\n', config.quiet)
            LOGGER.debug(e)
            if option:
                LOGGER.error(f'[ABORT] {func} option:{option}')
            else:
                LOGGER.error(f'[ABORT] {func}')
            sys.exit(254)
        if isinstance(result[1], (dict, list, tuple)):
            output = json.dumps(result[1], indent=TAB)
            output = f'Output:\n{TAB*2}' + output.replace('\n', f'\n{TAB*2}')
            end = TAB*2 + ZC_COMPLETE[1]
        else:
            output = result[1]
            end = None
        if not result[0]:
            PRINT_PROG('\n', config.quiet)
            LOGGER.error(f'[ABORT] {func}:{output}')
            sys.exit(255)
        if end:
            PRINT_TAB(2, config.quiet)
            LOGGER.info(output)
            PRINT_PROG(f'{end.upper()}\n', config.quiet)
        else:
            PRINT_PROG(f'{TAB*2}{output.upper()}\n', config.quiet)
    PRINT_PROG('\n', config.quiet)
    LOGGER.info(f'[FINISH] {ZABBIX_TIME()}')

    sys.exit(0)

if __name__ == '__main__':
    main()
