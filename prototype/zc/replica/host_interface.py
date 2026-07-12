#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaHostInterfaceMixin:
    def updateHostInterfaces(self, ifUpdateHosts):
        '''
        host.updateとは別に、hostinterface.update/deleteを実行する
        '''
        deleteInterfaces = []
        if ifUpdateHosts:

            # 表示（仮）
            process = 'Host Interface Update'
            interfaceResult = {'total':0, 'update':0, 'delete': 0, 'failed': 0, 'skip': 0}
            PRINT_PROG(f'{TAB*2}{process}:', self.CONFIG.quiet)

            for host in ifUpdateHosts:
                deleteInterfaces.extend(
                    self.updateHostInterface(host, interfaceResult)
                )
        else:
            return

        if deleteInterfaces:
            self.deleteUnusedInterfaces(deleteInterfaces, interfaceResult, process)

    def updateHostInterface(self, host, interfaceResult):
        deleteInterfaces = []
        hostId = host['id']
        hostName = host['host']

        try:
            # インターフェイスの取得
            hostIfs = self.ZAPI.hostinterface.get(
                **{
                    'output': 'extend',
                    'hostids': hostId
                }
            )
            interfaceResult['total'] += len(hostIfs)
        except Exception as e:
            self.LOGGER.debug(e)
            # 現状のホストのインターフェイス情報取得失敗
            interfaceResult['total'] += 1
            interfaceResult['failed'] += 1
            return deleteInterfaces

        # インターフェイスの確認
        types = [item['type'] for item in hostIfs]
        if len(hostIfs) != len(list(set(types))):
            # 同じ種類が複数ある（ので重複排除で少なくなる）
            if len(hostIfs) == 2 and len(list(set(types))) == 1:
                # インターフェイスが２つ、どちらも同じtypeなのでアップデート可
                pass
            else:
                # 対応できないインターフェイス設定なのでアップデート不可、スキップ
                interfaceResult['skip'] += 1
                return deleteInterfaces
        else:
            # 全部違う種類（typeで判断できる）で１つずつだけなのでアップデート可
            pass
        for updateIf in host['data']:
            targetIf = self.findTargetInterface(hostIfs, updateIf)
            if not targetIf:
                interfaceResult['skip'] += 1
                continue
            # アップデートするインターフェイスはリストから消す、残ったインターフェイスは削除対象
            hostIfs.remove(targetIf)
            # ターゲットのdetailsが空の時は[]になっているというクソ仕様（中身があるとdict、型が違う）
            # 空の時はdetailsを消す
            if not targetIf.get('details'):
                targetIf.pop('details')

            if not self.hasInterfaceChanged(updateIf, targetIf):
                # 変更がないのでスキップ
                interfaceResult['skip'] += 1
                continue
            updateIf['interfaceid'] = targetIf['interfaceid']
            try:
                self.ZAPI.hostinterface.update(**updateIf)
                interfaceResult['update'] += 1
            except Exception as e:
                self.LOGGER.debug(e)
                interfaceResult['failed'] += 1

            self.printInterfaceProgress(interfaceResult, 'Host Interface Update')

        for hostIf in hostIfs:
            # 削除対象の処理
            deleteInterfaces.append(
                {
                    'name': '%s(%s)' % (hostName, ZABBIX_IFTYPE[int(hostIf['type'])]),
                    'id': hostIf['interfaceid']
                }
            )
        return deleteInterfaces

    def findTargetInterface(self, hostIfs, updateIf):
        # typeとmainが同じインターフェイスを選択
        targetIf = [
            item for item in hostIfs if int(item['type']) == updateIf['type'] and int(item['main']) == updateIf['main']
        ]
        if not targetIf or len(targetIf) > 1:
            # このパターンはないはずだけど一応
            return None
        return targetIf[0]

    def hasInterfaceChanged(self, updateIf, targetIf):
        # 変更箇所の確認
        for param, value in updateIf.items():
            # 変更が一つでもあれば更新
            if param == 'details':
                for detail, dVal in updateIf['details'].items():
                    if targetIf['details'].get(detail) != str(dVal):
                        return True
            else:
                if targetIf.get(param) != str(value):
                    return True
        return False

    def deleteUnusedInterfaces(self, deleteInterfaces, interfaceResult, process):
        for delIf in deleteInterfaces:
            try:
                self.ZAPI.hostinterface.delete(delIf['id'])
                interfaceResult['delete'] += 1
            except Exception as e:
                self.LOGGER.debug(e)
                interfaceResult['failed'] += 1

            self.printInterfaceProgress(interfaceResult, process)

        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
        res = self.formatInterfaceProgress(interfaceResult)
        self.LOGGER.info(f'{process}: {res}')

    def printInterfaceProgress(self, interfaceResult, process):
        res = self.formatInterfaceProgress(interfaceResult)
        PRINT_PROG(f'\r{TAB*2}{process}: {res}', self.CONFIG.quiet)

    def formatInterfaceProgress(self, interfaceResult):
        sum = interfaceResult['update'] + interfaceResult['delete'] + interfaceResult['skip'] + interfaceResult['failed']
        return f'{sum}/{interfaceResult["total"]} (update:{interfaceResult["update"]}/delete:{interfaceResult["delete"]}/skip:{interfaceResult["skip"]}/failed:{interfaceResult["failed"]})'
