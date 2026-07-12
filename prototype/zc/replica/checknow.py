#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from time import sleep
from zc.common import *

class ReplicaCheckNowMixin:
    def execCheckNow(self):
        '''
        LLDとLONGTIMEインターバルアイテムを初回実行する
        '''
        # CheckNowを実行するか確認
        if not self.CONFIG.checknowExec:
            return (True, 'SKIP.')

        def checknow(targets):
            '''
            ファンクション内CheckNow実行ファンクション
            '''
            # 5.0.5対応
            if self.VERSION.major > 5 or (self.VERSION.major == 5 and self.VERSION.minor >= 5):
                option = [
                    {
                        'type': '6',
                        'request': {'itemid': target}
                    } for target in targets
                ]
            else:
                option = {
                    'type': '6',
                    'itemids': targets
                }
            try:
                # DB上のデータがZabbixサーバーに適用されるのを待つ
                sleep(self.CONFIG.checknowWait)
                if isinstance(option, list):
                    self.ZAPI.task.create(*option)
                else:
                    self.ZAPI.task.create(**option)
                return 'Success.'
            except Exception as e:
                self.LOGGER.debug(e)
                return 'Failed.'

        def filterCheckNowTargets(items):
            '''
            Execute now対象外のitemを除外して、task.createへ渡すitemidに変換する
            '''
            unsupportedTypes = {'7'}  # Zabbix agent active
            masterIds = [item['master_itemid'] for item in items if int(item.get('master_itemid', 0))]
            masterTypes = {}
            if masterIds:
                try:
                    masters = self.ZAPI.item.get(
                        output=['itemid', 'type'],
                        itemids=list(set(masterIds))
                    )
                    masterTypes = {item['itemid']: item['type'] for item in masters}
                except Exception as e:
                    self.LOGGER.debug(e)
            targets = []
            skipped = 0
            for item in items:
                masterId = item.get('master_itemid', '0')
                if int(masterId):
                    target = masterId
                    itemType = masterTypes.get(masterId)
                else:
                    target = item['itemid']
                    itemType = item.get('type')
                if itemType in unsupportedTypes:
                    skipped += 1
                    continue
                if target not in targets:
                    targets.append(target)
            return targets, skipped

        # 更新間隔サーチワード
        interval = []
        for item in self.CONFIG.checknowInterval:
            time = item[:-1]
            suffix = item[-1]
            if time.isdigit() and not suffix.isdigit():
                time = int(time)
                if suffix == 'm':
                    time *= 60
                elif suffix == 'h':
                    time *= 3600
                elif suffix == 'd':
                    time *= 86400
                else:
                    try:
                        time = int(item)
                    except:
                        continue
            interval.append(str(time))
        interval = set(sorted(interval))

        # Zabbixに適用されているホスト
        hosts = [item['ZABBIX_ID'] for item in self.LOCAL['host'].values()]

        # LLDを検索
        output = ['itemid', 'type']
        # 4.2対応
        if self.VERSION.major >= 4.2:
            output.append('master_itemid')
        try:
            items = self.ZAPI.discoveryrule.get(
                output=output,
                hostids=hosts
            )
            targets, skipped = filterCheckNowTargets(items)
        except:
            targets = []
            skipped = 0

        process = f'LLDs {len(targets)} items'
        if skipped:
            process += f' (skip:{skipped})'
        process += f' (wait {self.CONFIG.checknowWait}s)'
        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
        if not targets:
            self.LOGGER.info(f'{process}: No Exist LLDs items.')
        else:
            # LLDへのCheckNow実行
            res = checknow(targets)
            self.LOGGER.info(f'{process}: {res}')

        # 更新間隔にユーザーマクロで部分一致文字列を適用しているものを抽出
        try:
            items = self.ZAPI.item.get(
                output=output,
                hostids=hosts,
                filter={'delay': interval}
            )
            targets, skipped = filterCheckNowTargets(items)
        except:
            targets = []
            skipped = 0
        if targets:
            interval = '/'.join(interval)
            process = f'TargetInterval[{interval}] {len(targets)} items'
            if skipped:
                process += f' (skip:{skipped})'
            process += f' (wait {self.CONFIG.checknowWait}s)'
            # LLDへのCheckNow実行
            res = checknow(targets)
            PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
            self.LOGGER.info(f'{process}: {res}')

        return ZC_COMPLETE
