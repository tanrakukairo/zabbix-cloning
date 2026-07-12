#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from concurrent import futures
from zc.common import *

class ReplicaHostApplyMixin:
    def applyHosts(self, hosts):
        '''
        host.create/updateを並列実行し、失敗したホスト名リストを返す
        '''
        hostResult = {'total': len(hosts), 'create': 0, 'update': 0, 'failed': 0}
        process = 'Host Import'

        # host.createの並列実行、実行数はphp-fpmのフォーク数以下にする
        # ZabbixのAPI応答ベースの処理なのでProcess*じゃなくてThread*を使ってる
        future_list = []
        with futures.ThreadPoolExecutor(max_workers=self.CONFIG.phpWorkerNum) as executor:
            for host in hosts:
                future = executor.submit(self.applyHost, host, hostResult, process)
                future_list.append(future)
        futures.as_completed(fs=future_list)

        sum = hostResult['create'] + hostResult['update'] + hostResult['failed']
        res = f'{sum}/{hostResult["total"]} (create:{hostResult["create"]}/update:{hostResult["update"]}/failed:{hostResult["failed"]})'
        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
        self.LOGGER.info(f'{process}: {res}')

        failedHost = [item._result for item in future_list if not item._result[0]]
        if failedHost:
            PRINT_PROG(f'{TAB*2}Failed Hosts:\n', self.CONFIG.quiet)
            for item in failedHost:
                PRINT_TAB(3, self.CONFIG.quiet)
                self.LOGGER.error(f'Failed {item[1]} {item[2]}')
            failedHost = [item[2] for item in failedHost]

        return failedHost

    def applyHost(self, host, hostResult, process):
        function = host['function']
        name = host['name']
        data = host['data']
        try:
            getattr(self.ZAPI.host, function)(**data)
            hostResult[function] += 1
            result = (True, function)
        except Exception as e:
            self.LOGGER.debug(e)
            # hostはインポート失敗しても止めずに進める
            hostResult['failed'] += 1
            result = (False, function, name)
        sum = hostResult['create'] + hostResult['update'] + hostResult['failed']
        res = f'{sum}/{hostResult["total"]} (create:{hostResult["create"]}/update:{hostResult["update"]}/failed:{hostResult["failed"]})'
        PRINT_PROG(f'\r{TAB*2}{process}: {res}', self.CONFIG.quiet)
        return result
