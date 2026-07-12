#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaAlertMixin:
    def setAlertStopInUpdate(self):
        '''
        アップデート中アラートが発生しないようにメンテナンスを設定する
        メンテナンス期間: 10分
        '''

        # 開始時刻の設定
        now = UNIXTIME()
        # 期間
        period = 600

        # グループID
        gIds = 'groupids'
        # 6.0対応
        if self.VERSION.major >= 6.0:
            gIds = 'groups' 

        targets = [item['ZABBIX_ID'] for item in self.LOCAL['hostgroup'].values()]
        # 6.0対応
        if self.VERSION.major >= 6.0:
            idName = self.getKeynameInMethod('hostgroup', 'id')
            targets = [{idName: id} for id in targets]

        inUpdate = {
            'name': ZC_MAINTE_NAME,
            'active_since': now,
            'active_till': now + period,
            'maintenance_type': 0,
            'timeperiods' :[
                {
                    'timeperiod_type': 0,
                    'start_date': now,
                    'period': period
                }
            ],
            gIds: targets
        }
        API = getattr(self.ZAPI, 'maintenance')

        # 既存のアップデート中アラート停止の有無を確認、あれば削除
        process = 'Set AlartStop in Update'
        exists = [item['ZABBIX_ID'] for item in self.LOCAL['maintenance'].values() if item['NAME'] == ZC_MAINTE_NAME]
        if exists:
            try:
                API.delete(*exists)
            except Exception as e:
                self.LOGGER.debug(e)
                return (False, 'Failed Delete Exist AlertStop.')
        PRINT_TAB(2, self.CONFIG.quiet)
        self.LOGGER.info(f'{process}: Delete Exists.')
        try:
            result = API.create(**inUpdate)
            if not result.get('maintenanceids'):
                return (False, 'No Set AlertStop.')
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, f'Failed Set AlertStop.')
        PRINT_TAB(2, self.CONFIG.quiet)
        self.LOGGER.info(f'{process}: Success.')
        
        # Zabbixからのデータ再取得
        self.getDataFromZabbix()

        PRINT_TAB(2, self.CONFIG.quiet)
        self.LOGGER.info(f'{process}: Start from NOW to {period}s after.')

        return ZC_COMPLETE
    def setAlertMedia(self):
        '''
        アラート情報をユーザーに設定する
        '''
        if self.CONFIG.role in ZC_NO_NOTICE_ROLE:
            # 通知設定しないノードは終了
            skipMessage = 'Nodes Not to be Notified.'
        elif not self.LOCAL.get('mediatype'):
            # 有効なメディアタイプがなければ終了
            skipMessage = 'No Exist Enabled MediaType.'
        else:
            skipMessage = ''
        if skipMessage:
            PRINT_TAB(2, self.CONFIG.quiet)
            self.LOGGER.info(f'SKIP, {skipMessage}.')
            return ZC_COMPLETE
        # ZCに渡されたメディア設定
        mediaSettings = self.CONFIG.mediaSettings
        # {メディア：対象ユーザーデータ}になっているのを{ユーザー：[メディア]}に変換
        # 6.2対応
        if self.VERSION.major >= 6.2:
            userMedias = 'medias'
        else:
            userMedias = 'user_medias'
        userMediasData = {}
        for media, values in mediaSettings.items():
            # ZABBIX_ID取得
            id = self.replaceIdName('mediatype', media)
            # なければスキップ
            if not id:
                continue
            for user, value in values.items():
                user = self.replaceIdName('user', user)
                if not user:
                    # ワーカーノードにユーザーがいないものは排除
                    continue
                idName = self.getKeynameInMethod('user', 'id')
                if not value.get('to'):
                    # 宛先設定がないものは排除
                    continue
                if not value.get('severity'):
                    # 対応重要度レベルがないものは排除
                    continue
                if not value.get('work_time'):
                    # 通知可能時間がないものは排除
                    continue
                # 宛先はリストかタプル
                if isinstance(value['to'], (list, tuple)):
                    address = value['to']
                elif isinstance(value['to'], str):
                    address = [value['to']]
                else:
                    continue
                # severity生成
                # 重要度レベルが「543210」（0はUnknown）の並びのビット立てを数字にしたもの
                severity = 0
                for lv in range(0, 6):
                    enabled = value['severity'].get(str(lv), False)
                    if enabled is True or str(enabled).upper() == 'YES' or str(enabled).lower() == 'true':
                        severity += 2 ** lv
                # period生成
                # 曜日ごとに「isoweekday,HH:MM-HH:MM」の記述で「;」区切り
                # 曜日は1-7って範囲で書けるけど、ワークタイムが同じ場合のみ範囲で書けるので
                # そこの判定はめんどうなので曜日ごとにする
                period = []
                for wd, time in value['work_time'].items():
                    if not time:
                        continue
                    if not re.match(r'[0-9].\:[0-9].\-[0-9].\:[0-9].', time):
                        continue
                    period.append(f'{ZABBIX_WEEKDAY[wd.upper()]},{time}')
                media = {
                    'mediatypeid': id,
                    'sendto': address,
                    'active':0,
                    'severity': severity,
                    'period': ';'.join(period)
                }
                if not userMediasData.get(user):
                    # userMediasDataにuserがない場合は作成
                    userMediasData[user] = {
                        idName: user,
                        userMedias: [media]
                    }
                else:
                    # ある場合はmediaだけ追加
                    userMediasData[user][userMedias].append(media)
            PRINT_TAB(2, self.CONFIG.quiet)
            self.LOGGER.info('Setting Import From ConfigFile: Done.')

        # 適用
        if not userMediasData:
            return (True, 'No Data.')
        for user, data in userMediasData.items():
            process = 'API Execute[user.mediatype]'
            PRINT_TAB(2, self.CONFIG.quiet)
            try:
                self.ZAPI.user.update(**data)
                self.LOGGER.info(f'{process}: Success.')
            except Exception as e:
                self.LOGGER.debug(e)
                self.LOGGER.error(f'{process}: Failed.')
                return (False, f'Failed Set AlertMedia for {self.replaceIdName("user", user)}.')
        return ZC_COMPLETE
