#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaHostDataMixin:
    def prepareHostData(self):
        '''
        STOREのhostデータを確認し、Zabbix APIに渡せる形へ加工する
        '''
        hosts = []
        if not self.STORE.get('host'):
            return (True, 'No Exist Hosts in Store Data.')

        for host in self.STORE['host']:
            name = host['NAME']
            data = host['DATA']
            hostUuid = self.getHostUuid(data)
            if not self.isHostApplyTarget(data):
                continue

            self.normalizeHostData(data)
            hosts.append(
                {
                    'name': name,
                    'data': data,
                    'uuid': hostUuid,
                }
            )

        return hosts

    def getHostUuid(self, data):
        hostUuid = [tag.get('value') for tag in data.get('tags', []) if tag.get('tag') == ZC_UNIQUE_TAG]
        return hostUuid[0] if len(hostUuid) == 1 else None

    def isHostApplyTarget(self, data):
        # 適用可能ホストの判定:ZC_WORKERタグのバリューを利用する
        monitorNode = [tag.get('value') for tag in data.get('tags', []) if tag.get('tag') == ZC_MONITOR_TAG]
        if self.isReplica:
            # replicaはそのまま適用
            data.update({'status': 1 if data.get('status') == 'DISABLED' else 0})
        elif self.CONFIG.node in monitorNode:
            # 監視有効で適用するホスト
            data.update({'status': ZABBIX_ENABLE})
        else:
            # このノードの適用対象ではないのでスキップ
            return False
        if self.CONFIG.disableMonitoring:
            # 監視無効
            data.update({'status': ZABBIX_DISABLE})
        return True

    def normalizeHostData(self, data):
        # ホスト直設定のアイテム、トリガー、LLDは除外
        [data.pop(section, None) for section in self.discardParameter['host']]
        # バリューなしのキーを削除:5.x系であったcreateの空データ無視がなくなった時の対応（だったかな）
        [data.pop(key, None) for key, value in data.copy().items() if not value]
        # インベントリモードの変換:MANUALの場合キーが存在しない
        data['inventory_mode'] = ZABBIX_INVENTORY_MODE.get(data.get('inventory_mode'), ZABBIX_INVENTORY_MODE['MANUAL'])
        # 4.2のテンプレート出力対応
        if data.get('inventory'):
            data['inventory'].pop('inventory_mode', None)

        self.normalizeHostInterfaces(data)
        self.convertHostProxy(data)
        self.convertHostRelations(data)

    def normalizeHostInterfaces(self, data):
        # インターフェイスの処理
        if len(data.get('interfaces', [])) == 0:
            # 6.4未満はインターフェイスがない場合はエラーになるので、デフォルトのインターフェイスを入れる
            if self.VERSION.major < 6.4:
                data['interfaces'] = [{'default': 'YES'}]
            else:
                data.pop('interfaces', None)
                return
        for hostIf in data.get('interfaces', []):
            self.normalizeHostInterface(hostIf)

    def normalizeHostInterface(self, hostIf):
        # Yes/Noの値変換
        Y_N = {'NO': 0, 'YES': 1}

        # create時に不要なので削除
        hostIf.pop('interface_ref', None)
        ifType = hostIf.get('type', 'AGENT')
        hostIf.update(
            {
                'ip': hostIf.get('ip', '127.0.0.1'),
                'main': Y_N[hostIf.pop('default', 'YES')],
                'port': hostIf.get('port', '10050'),
                'type': ZABBIX_IFTYPE[ifType] if not ifType.isdigit() else ifType,
                'useip': 0 if hostIf.get('useip', 'YES') != 'YES' else 1,
                'dns': hostIf.get('dns', ''),
            }
        )
        # 強制DNS->IP変換処理
        if int(hostIf['useip']) == 0 and self.CONFIG.useip:
            try:
                new_ip = socket.gethostbyname(hostIf['dns'])
            except:
                new_ip = '0.0.0.0'
            if new_ip != '0.0.0.0':
                hostIf['ip'] = new_ip
                hostIf['useip'] = 1
                hostIf.pop('dns', None)
        # 5.0対応
        if self.VERSION.major >= 5.0:
            # bulkがdetailsの中に移動なので削除
            hostIf.pop('bulk', None)
            # SNMPは接続設定detailsが追加、他のインターフェイスはあっても無視される
            if ifType == 'SNMP':
                useVersion = hostIf['details'].get('version', 'SNMPV2').upper() if hostIf.get('details') else 'SNMPV2'
                snmpCommunity = hostIf['details'].get('community', ZABBIX_SNMP_COMMUNITY)
                hostIf.update(
                    {
                        'details': {
                            'version': ZABBIX_SNMP_VERSION[useVersion],
                            'community': snmpCommunity
                        }
                    }
                )
        else:
            bulk = hostIf.get('bulk', 'YES')
            hostIf['bulk'] = Y_N[bulk] if not bulk.isdigit() else bulk

    def convertHostProxy(self, data):
        # Proxy変換
        if self.VERSION.major >= 7.0:
            if self.getLatestVersion('MASTER_VERSION') >= 7.0:
                # 7.0対応 プロキシグループとの区別が追加
                # 各所で表記ブレブレなのどうにかしてよ……
                proxyType = data.pop('monitored_by', 'direct').lower()
            else:
                proxyType = 'proxy'
            monitor = ZABBIX_PROXY_MODE.get(proxyType, 0)
            proxy = data.pop(proxyType, None)
            if monitor > 0 and proxy:
                # プロキシ情報を追加
                data.update(
                    {
                        'monitored_by': monitor,
                        proxyType + 'id': self.replaceIdName(proxyType.replace('_', ''), proxy['name'])
                    }
                )
        else:
            proxy = data.pop('proxy', None)
            if proxy:
                data['proxy_hostid'] = self.replaceIdName('proxy', proxy['name'])

    def convertHostRelations(self, data):
        # テンプレートとホストグループのID変換
        for method in ['template', 'hostgroup']:
            section = method + 's'
            section = section.replace('host', '')
            id = self.getKeynameInMethod(method, 'id')
            data[section] = [
                {
                    id: self.replaceIdName(method, item['name'])
                } for item in data.get(section, []) if item['name'] in self.LOCAL[method].keys()
            ]
