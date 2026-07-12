#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaHostDeleteMixin:
    def deleteMissingHosts(self, hosts, failedHost):
        '''
        マスターに存在しないホストを削除する
        '''
        if not self.CONFIG.deleteHost:
            return

        # 対象IDリスト
        deleteTarget = []
        # update/createの両方処理済み(失敗を除く)のホストは削除対象から除外する
        appliedHosts = set([host['name'] for host in hosts if host['name'] not in failedHost])
        # 現在ワーカーに存在するホストのうち、適用対象に存在しないものを削除する
        targetHosts = [name for name in self.LOCAL['host'].keys() if name not in appliedHosts]
        for name in targetHosts:
            deleteTarget.append(self.LOCAL['host'][name]['ZABBIX_ID'])
        if deleteTarget:
            process = 'Host Delete'
            PRINT_TAB(2, self.CONFIG.quiet)
            try:
                self.ZAPI.host.delete(*deleteTarget)
                self.LOGGER.info(f'{process}: Success.\n{"/".join(targetHosts)}')
                # Zabbixからのデータ再取得
                self.getDataFromZabbix()
            except Exception as e:
                self.LOGGER.debug(e)
                self.LOGGER.error(f'{process}: Failed.')
