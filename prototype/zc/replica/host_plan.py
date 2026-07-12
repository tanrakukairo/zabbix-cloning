#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaHostPlanMixin:
    def buildHostUuidMap(self):
        '''
        ローカルのホスト確認用UUIDテーブルを生成する
        {'ZC_UUIDの中身': 'ローカルホストのhostid'}
        '''
        hostUuids = {}
        for item in self.LOCAL['host'].values():
            tags = [tag['value'] for tag in item['DATA'].get('tags', []) if tag.get('tag') == ZC_UNIQUE_TAG]
            if not tags:
                continue
            hostUuids.update(
                {
                    tags[0]: item['ZABBIX_ID']
                }
            )
        return hostUuids

    def buildHostApplyPlan(self, hosts):
        '''
        create/updateの処理内容を決定し、update時のインターフェイス更新データを取り出す
        '''
        hostUuids = self.buildHostUuidMap()
        ifUpdateHosts = []
        for item in hosts.copy():
            localHost = self.LOCAL['host'].get(item['name'])
            idName = self.getKeynameInMethod('host', 'id')
            data = item['data']
            hostUuid = item['uuid']
            hostId = None
            update = False
            if localHost:
                # 同じホスト名がある
                if self.CONFIG.hostUpdate:
                    # アップデートする場合
                    if hostUuid in hostUuids.keys():
                        # ZC_UUIDも同じ
                        function = 'update'
                        # 更新対象のローカルのIDを入れる
                        hostId = localHost['ZABBIX_ID']
                        update = True
                else:
                    # アップデートしない場合
                    hosts.remove(item)
                    continue
            else:
                # 同じホスト名はない
                if hostUuid in hostUuids.keys():
                    # ZC_UUIDは同じ
                    if self.CONFIG.forceHostUpdate:
                        # 強制アップデートの場合
                        function = 'update'
                        # ストアの情報から名前を抜く
                        data.pop('host', None)
                        data.pop('name', None)
                        # ローカルにあるホストのIDを使う
                        hostId = hostUuids[hostUuid]
                        update = True
                    else:
                        # アップデートしない場合
                        hosts.remove(item)
                        continue
                else:
                    # 新規ホスト
                    function = 'create'
            if update:
                # インターフェイスの更新は別でやらないといけない
                hostIfs = data.pop('interfaces', None)
                if hostIfs:
                    ifUpdateHosts.append(
                        {
                            'host': item['name'],
                            'id': hostId,
                            'data': hostIfs
                        }
                    )
            item['function'] = function
            data[idName] = hostId

        return hosts, ifUpdateHosts
