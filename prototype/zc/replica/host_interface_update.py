#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaHostInterfaceMixin:
    def updateHostInterfaces(self, ifUpdateHosts):
        '''
        host.updateとは別に、hostinterface.create/update/deleteを実行する
        '''
        if not ifUpdateHosts:
            return

        process = 'Host Interface Update'
        interfaceResult = {'total': 0, 'create': 0, 'update': 0, 'delete': 0, 'failed': 0, 'skip': 0}
        PRINT_PROG(f'{TAB*2}{process}:', self.CONFIG.quiet)

        for host in self.sortInterfaceUpdateHosts(ifUpdateHosts):
            self.updateHostInterface(host, interfaceResult, process)

        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
        self.LOGGER.info(f'{process}: {self.formatInterfaceProgress(interfaceResult)}')

    def sortInterfaceUpdateHosts(self, ifUpdateHosts):
        return sorted(
            ifUpdateHosts,
            key=lambda host: (str(host.get('host', '')), str(host.get('id', '')))
        )

    def updateHostInterface(self, host, interfaceResult, process):
        hostId = host['id']
        updateIfs = self.prepareUpdateInterfaces(host.get('data', []))
        self.selectMainInterface(updateIfs)

        try:
            hostIfs = self.ZAPI.hostinterface.get(
                **{
                    'output': 'extend',
                    'hostids': hostId
                }
            )
        except Exception as e:
            self.LOGGER.debug(e)
            interfaceResult['total'] += len(updateIfs)
            interfaceResult['failed'] += len(updateIfs)
            self.printInterfaceProgress(interfaceResult, process)
            return

        plans, deleteTargets = self.buildInterfaceUpdatePlan(hostIfs, updateIfs)
        interfaceResult['total'] += len(plans) + len(deleteTargets)

        for plan in plans:
            if plan['function'] == 'skip':
                interfaceResult['skip'] += 1
            elif plan['function'] == 'create':
                self.createHostInterface(hostId, plan['interface'], hostIfs, interfaceResult)
            elif plan['function'] == 'update':
                self.updateMatchedInterface(plan['interface'], plan['target'], hostIfs, interfaceResult)
            self.printInterfaceProgress(interfaceResult, process)

        self.deleteUnusedInterfaces(deleteTargets, interfaceResult, process)

    def prepareUpdateInterfaces(self, updateIfs):
        interfaces = []
        for updateIf in updateIfs:
            data = updateIf.copy()
            if data.get('details'):
                data['details'] = data['details'].copy()
            interfaces.append(data)
        return interfaces

    def selectMainInterface(self, updateIfs):
        # 新しいデータ側にmain指定がない場合だけ、優先タイプ順でmainを選出する
        if [hostIf for hostIf in updateIfs if int(hostIf.get('main', 0)) == 1]:
            return

        typePriority = [
            ZABBIX_IFTYPE['AGENT'],
            ZABBIX_IFTYPE['SNMP'],
            ZABBIX_IFTYPE['JMX'],
            ZABBIX_IFTYPE['IPMI'],
        ]
        for ifType in typePriority:
            for hostIf in updateIfs:
                if int(hostIf.get('type', 0)) == ifType:
                    hostIf['main'] = 1
                    return

    def buildInterfaceUpdatePlan(self, hostIfs, updateIfs):
        plans = []
        matchedIds = set()
        for updateIf in updateIfs:
            targetIf = self.findTargetInterface(hostIfs, updateIf, matchedIds)
            if targetIf:
                matchedIds.add(targetIf['interfaceid'])
                function = 'update' if self.hasInterfaceChanged(updateIf, targetIf) else 'skip'
            else:
                function = 'create'
            plans.append(
                {
                    'function': function,
                    'interface': updateIf,
                    'target': targetIf,
                }
            )

        updateKeys = [self.interfaceKey(updateIf) for updateIf in updateIfs]
        deleteTargets = [
            hostIf for hostIf in hostIfs
            if hostIf['interfaceid'] not in matchedIds
            and self.interfaceKey(hostIf) not in updateKeys
        ]
        return plans, deleteTargets

    def findTargetInterface(self, hostIfs, updateIf, matchedIds):
        candidates = [
            item for item in hostIfs
            if item['interfaceid'] not in matchedIds
            and self.interfaceKey(item) == self.interfaceKey(updateIf)
        ]
        if len(candidates) == 1:
            return candidates[0]
        if len(candidates) > 1:
            sameMain = [
                item for item in candidates
                if int(item.get('main', 0)) == int(updateIf.get('main', 0))
            ]
            if len(sameMain) == 1:
                return sameMain[0]
            return candidates[0]

        sameType = [
            item for item in hostIfs
            if item['interfaceid'] not in matchedIds
            and int(item.get('type', 0)) == int(updateIf.get('type', 0))
        ]
        if len(sameType) == 1:
            return sameType[0]

        sameMain = [
            item for item in sameType
            if int(item.get('main', 0)) == int(updateIf.get('main', 0))
        ]
        if len(sameMain) == 1:
            return sameMain[0]

        return None

    def interfaceKey(self, hostIf):
        details = hostIf.get('details') or {}
        detailsKey = tuple(
            sorted([(str(key), str(value)) for key, value in details.items()])
        )
        return (
            int(hostIf.get('type', 0)),
            int(hostIf.get('useip', 0)),
            str(hostIf.get('ip', '')),
            str(hostIf.get('dns', '')),
            str(hostIf.get('port', '')),
            detailsKey,
        )

    def hasInterfaceChanged(self, updateIf, targetIf):
        for param, value in updateIf.items():
            if param == 'details':
                targetDetails = targetIf.get('details') or {}
                for detail, dVal in value.items():
                    if targetDetails.get(detail) != str(dVal):
                        return True
            elif targetIf.get(param) != str(value):
                return True
        return False

    def createHostInterface(self, hostId, updateIf, hostIfs, interfaceResult):
        createIf = updateIf.copy()
        createIf['hostid'] = hostId
        oldMainIf = self.findCurrentMainInterface(hostIfs, updateIf)
        try:
            self.ZAPI.hostinterface.create(**createIf)
            self.unsetOldMainInterface(oldMainIf)
            interfaceResult['create'] += 1
        except Exception as e:
            self.LOGGER.debug(e)
            if not oldMainIf:
                interfaceResult['failed'] += 1
                return
            try:
                self.unsetOldMainInterface(oldMainIf, force=True)
                self.ZAPI.hostinterface.create(**createIf)
                interfaceResult['create'] += 1
            except Exception as retryError:
                self.LOGGER.debug(retryError)
                interfaceResult['failed'] += 1

    def updateMatchedInterface(self, updateIf, targetIf, hostIfs, interfaceResult):
        updateData = updateIf.copy()
        updateData['interfaceid'] = targetIf['interfaceid']
        oldMainIf = self.findCurrentMainInterface(hostIfs, updateIf, targetIf)
        try:
            self.ZAPI.hostinterface.update(**updateData)
            self.applyLocalInterfaceUpdate(targetIf, updateData)
            self.unsetOldMainInterface(oldMainIf)
            interfaceResult['update'] += 1
        except Exception as e:
            self.LOGGER.debug(e)
            if not oldMainIf:
                interfaceResult['failed'] += 1
                return
            try:
                self.unsetOldMainInterface(oldMainIf, force=True)
                self.ZAPI.hostinterface.update(**updateData)
                self.applyLocalInterfaceUpdate(targetIf, updateData)
                interfaceResult['update'] += 1
            except Exception as retryError:
                self.LOGGER.debug(retryError)
                interfaceResult['failed'] += 1

    def applyLocalInterfaceUpdate(self, targetIf, updateData):
        for key, value in updateData.items():
            if key == 'interfaceid':
                continue
            if key == 'details':
                targetIf[key] = value.copy()
            else:
                targetIf[key] = str(value)

    def findCurrentMainInterface(self, hostIfs, updateIf, targetIf=None):
        if int(updateIf.get('main', 0)) != 1:
            return None
        if targetIf and int(targetIf.get('main', 0)) == 1:
            return None
        currentMain = [
            hostIf for hostIf in hostIfs
            if int(hostIf.get('type', 0)) == int(updateIf.get('type', 0))
            and int(hostIf.get('main', 0)) == 1
        ]
        if len(currentMain) != 1:
            return None
        if targetIf and currentMain[0]['interfaceid'] == targetIf['interfaceid']:
            return None
        return currentMain[0]

    def unsetOldMainInterface(self, oldMainIf, force=False):
        if not oldMainIf:
            return
        if not force and int(oldMainIf.get('main', 0)) == 0:
            return
        self.ZAPI.hostinterface.update(
            interfaceid=oldMainIf['interfaceid'],
            main=0
        )
        oldMainIf['main'] = '0'

    def deleteUnusedInterfaces(self, deleteTargets, interfaceResult, process):
        for hostIf in deleteTargets:
            try:
                self.ZAPI.hostinterface.delete(hostIf['interfaceid'])
                interfaceResult['delete'] += 1
            except Exception as e:
                self.LOGGER.debug(e)
                interfaceResult['failed'] += 1
            self.printInterfaceProgress(interfaceResult, process)

    def printInterfaceProgress(self, interfaceResult, process):
        res = self.formatInterfaceProgress(interfaceResult)
        PRINT_PROG(f'\r{TAB*2}{process}: {res}', self.CONFIG.quiet)

    def formatInterfaceProgress(self, interfaceResult):
        sum = interfaceResult['create'] + interfaceResult['update'] + interfaceResult['delete'] + interfaceResult['skip'] + interfaceResult['failed']
        return f'{sum}/{interfaceResult["total"]} (create:{interfaceResult["create"]}/update:{interfaceResult["update"]}/delete:{interfaceResult["delete"]}/skip:{interfaceResult["skip"]}/failed:{interfaceResult["failed"]})'
