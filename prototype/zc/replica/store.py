#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import json
from zc.common import *

class ReplicaStoreMixin:
    def changePassword(self, **auth):
        '''
        パスワード変更
        auth: [user, changePasswd, currentPasswd]
        パスワード変更は失敗しても処理を止めさせないのでTrueを返す（ここに来るのに認証は通っている）
        '''
        if self.CONFIG.updatePassword is not True:
            return (True, 'No Change Password.')
        result = ZC_COMPLETE
        idName = self.getKeynameInMethod('user', 'id')
        name = self.getKeynameInMethod('user', 'name')
        auth = auth if len(auth) > 2 else self.CONFIG.auth
        currentPasswd = auth['current'] if len(auth) == 3 else ZABBIX_DEFAULT_AUTH['password']
        # ZabbixCloud対応: プラットフォーム生成のデフォルトパスワードを指定する
        if self.CONFIG.platformPassword:
            currentPasswd = self.CONFIG.platformPassword

        try:
            # 対象管理者の確認
            admin = self.ZAPI.user.get(output=[idName, name], filter={name: auth['user']})
            if not admin:
                result = (True, f'No Exist User: {auth["user"]}.')
            else:
                # パスワード変更
                change = {
                    idName: admin[0][idName],
                    'passwd': auth['password']
                }
                # 6.4対応 現在のパスワードが必要
                if self.VERSION.major >= 6.4:
                    change.update({'current_passwd': currentPasswd})
                self.ZAPI.user.update(**change)
                # 変更したパスワードで再認証
                self.ZAPI.login(*auth)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, f'Failed Update Password for {auth["user"]}.')

        return result
    def getDataFromStore(self, **params):
        '''
        データストアからデータを取得する
        version: 対象のバージョン、なければ最新
        '''
        # マスターノードからダイレクトにデータを取得する
        if params.get('master'):
            master = params['master']
            from zc.master.main import ZabbixMaster
            if not isinstance(master, ZabbixMaster):
                return (False, 'Not Master Instance.')
            result = self.getDataFromMaster(master)
            if result[0]:
                self.STORE = master.STORE
                self.VERSIONS = master.VERSIONS
                return ZC_COMPLETE
            else:
                return result
        
        # ストアからの読み込みここから
        result = ZC_COMPLETE

        # 基本最新版を使用、ワーカーノードでバージョン指定があり、ストアにある場合はそれを使う
        # CONFIGの時点でマスターではNoneになってる
        version = [item for item in self.VERSIONS if item['VERSION_ID'] == self.CONFIG.targetVersion]
        if len(version)!= 1:
            version = self.getLatestVersion()
        else:
            version = version[0]
        # 継承元クラスの同名ファンクションを使ってストアからデータを取得
        result = super().getDataFromStore(version)
        if not result[0]:
            return result
        # ファイルの場合
        if self.CONFIG.storeType == 'file':
            return result
        # ローカルで使う形に成型
        for item in result[1]:
            method = item.get('METHOD')
            # METHODがないので不正データ
            if not method:
                return (False, f'wrong data from getDataFromStore, {version}')
            # self.STOREにMETHODがなければ初期化
            if not self.STORE.get(method):
                self.STORE[method] = []
            try:
                # 適用、データが足りていなければ失敗
                self.STORE[method].append(
                    {
                        'NAME': item['NAME'],
                        'DATA': item['DATA'],
                        'DATA_ID': item['DATA_ID'],
                    }
                )
            except:
                return (False, f'Not enough data:{json.dumps(item)}')
        return ZC_COMPLETE
