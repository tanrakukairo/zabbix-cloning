#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.utils import *

class CloneVersionMixin:
    '''
    Version metadata and ID/name replacement helpers.
    '''

    def createNewVersion(self):
        '''
        新しいバージョンデータを取得、存在しなければマスターノードでのみ生成
        '''
        description = f'MasterNode: {self.CONFIG.node} ({self.CONFIG.endpoint}), CreateDate: {ZABBIX_TIME()}'
        if self.CONFIG.description:
            description += f' : {self.CONFIG.description}'
        if not self.NEW and self.isMaster:
            self.NEW = {
                'VERSION_ID': str(uuid.uuid4()),
                'UNIXTIME': UNIXTIME(),
                'MASTER_VERSION': self.VERSION.major,
                'DESCRIPTION': description
            }
        return self.NEW
    def getLatestVersion(self, target=None):
        '''
        最新バージョンデータを返す
        target: 指定キーの内容を返す
        '''
        latest = self.VERSIONS[0] if len(self.VERSIONS) > 0 else {}
        return latest if not target else latest.get(target, latest)
    def replaceIdName(self, method=None, target=None):
        '''
        method.targetの変換
        targetがidならname、nameならidを返す
        '''
        if not method or not target:
            # パラメータなし
            return None
        if self.IDREPLACE.get(method, None) is None:
            # メソッドが存在しない
            return None
        try:
            # 数字が文字列で入ってきた場合の処理
            target = int(target)
        except:
            pass
        if method == 'mediatype':
            # メディアタイプの特別処理
            if target == 0:
                return '__ALL_MEDIA__'
            elif target == '__ALL_MEDIA__':
                return 0
            else:
                pass
        elif method == 'host':
            # ホストの特別処理
            if target == 0:
                return '__CURRENT_HOST__'
            elif target == '__CURRENT_HOST__':
                return 0
            else:
                pass
        elif method == 'proxy':
            if target == 0:
                return '__SERVER_DIRECT__'
            elif target == '__SERVER_DIRECT__':
                return 0
            else:
                pass  
        elif method == 'proxygroup':
            # プロキシグループの特別処理
            if target == 0:
                return '__NO_GROUP__'
            elif target == '__NO_GROUP__':
                return 0
            else:
                pass
        elif method in ['usergroup', 'hostgroup', 'templategroup']:
            # グループ系の特別処理
            if target == 0:
                return '__ALL_GROUP__'
            elif target == '__ALL_GROUP__':
                return 0
            else:
                pass
        else:
            pass
        return self.IDREPLACE[method].get(target, None)
