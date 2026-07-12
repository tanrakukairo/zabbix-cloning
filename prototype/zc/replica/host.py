#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *
from zc.replica.host_data import ReplicaHostDataMixin
from zc.replica.host_plan import ReplicaHostPlanMixin
from zc.replica.host_apply import ReplicaHostApplyMixin
from zc.replica.host_interface_update import ReplicaHostInterfaceMixin
from zc.replica.host_delete import ReplicaHostDeleteMixin

class ReplicaHostMixin(
    ReplicaHostDataMixin,
    ReplicaHostPlanMixin,
    ReplicaHostApplyMixin,
    ReplicaHostInterfaceMixin,
    ReplicaHostDeleteMixin,
):
    def setHostToZabbix(self):
        '''
        STOREデータを加工し、Zabbixへhostを適用する
        hostsは数が多いのでAPIを並列処理する
        あとバージョン上がってデータ形式が変更すると下位バージョンのインポートファイルで
        エラーになる場合が多いのでデータ形式をここで変換する（キー名変わる可能性もあるしね）
        '''
        hosts = self.prepareHostData()
        if isinstance(hosts, tuple):
            return hosts

        hosts, ifUpdateHosts = self.buildHostApplyPlan(hosts)
        failedHost = self.applyHosts(hosts)
        self.updateHostInterfaces(ifUpdateHosts)

        # Zabbixからのデータ再取得
        self.getDataFromZabbix()

        self.deleteMissingHosts(hosts, failedHost)

        # 監視する対象がないので終了
        if not hosts:
            if self.CONFIG.hostUpdate:
                return (True, 'No Exist Monitoring Hosts with %s.' % self.CONFIG.node)
            else:
                return (True, 'Not Allowed Host Update.')

        return ZC_COMPLETE
