#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.utils import CHECK_ZABBIX_SERVER_NAME

class ZabbixDataMixin:
    '''
    Zabbix APIから現在のノードデータを取得する共通処理。
    '''

    def getDataFromZabbix(self):
        '''
        実行ノードのZabbixからデータを取得しLOCALに適用
        '''
        result = ZC_COMPLETE
        method = None
        try:
            # メソッドIDと名前を取得
            for method, options in self.methodParameters.items():
                # メソッドが追加されたバージョン未満ならスキップ
                for version, addMethods in self.addMethods.items():
                    if self.VERSION.major < version and method in addMethods:
                        continue
                # 消えたメソッドはsuper().__init__でmethodParametersから削除されるので処理はない
                # methodParamterに登録されているメソッドのデータをget
                self.LOCAL[method] = {}
                getData = getattr(self.ZAPI, method).get(**options.get('options', {}))
                if method in self.sections['GLOBAL']:
                    # IDもNAMEもないので特別処理
                    id = 0
                    for key, value in getData.items():
                        self.LOCAL[method][key] = {
                            'ZABBIX_ID': id,
                            'NAME': key,
                            'DATA': {key: value}
                        }
                        id += 1
                else:
                    for data in getData:
                        # メソッドIDはZabbixがオブジェクト生成時に自動でつけるため、
                        # create時にワーカー側で邪魔になるのでDATAから取り出してZABBIX_IDに入れる
                        self.LOCAL[method][data[options['name']]] = {
                                'ZABBIX_ID': int(data.pop(options['id'])),
                                'NAME': data[options['name']],
                                'DATA': data
                        }
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, f'Failed getDataFromZabbix/API {method}.')

        # 6.0以前のマスターノードならばデータベース操作でデータ取得
        if self.isMaster and self.VERSION.major < 6.0:
            try:
                self.LOCAL['database'] = {}
                for table in self.sections['DB_DIRECT']:
                    res = self.operateDbDirect('get', table)
                    if res[0]:
                        self.LOCAL['database'][table] = {
                            'ZABBIX_ID': None,
                            'NAME': table,
                            'DATA': res[1]
                        }
            except Exception as e:
                self.LOGGER.debug(e)
                result = (False, 'Failed getDataFromZabbix/DBDirect.')

        # IDREPLACE: ZCを実行しているノードのZabbixから取得した値からの生成
        IDREPLACE = {}
        try:
            for method, data in self.LOCAL.items():
                IDREPLACE[method] = {}
                for item in data.values():
                    # ZABBIX_IDとNAMEがあるものだけ処理
                    if item.get('ZABBIX_ID') and item.get('NAME'):
                        IDREPLACE[method][item['ZABBIX_ID']] = item['NAME']
                        IDREPLACE[method][item['NAME']] = item['ZABBIX_ID']
            self.IDREPLACE = IDREPLACE
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed getDataFromZabbix/IDREPLACE.')

        return result

    def getDataFromMaster(self, master):
        '''
        ストア:Directのマスターノードからのデータ読み込み
        '''
        master.VERSIONS = self.VERSIONS
        master.VERSIONS[0].update(
            {
                'MASTER_VERSION': master.VERSION.major,
            }
        )

        # マスターノードからダイレクトにデータを取得する
        if master.CONFIG.directMaster:
            # 接続先のサーバー名確認
            auth = {'token': master.CONFIG.token} if master.CONFIG.token else master.CONFIG.auth
            result = CHECK_ZABBIX_SERVER_NAME(master.CONFIG.endpoint, master.CONFIG.node, auth)
            if result[0]:
                # マスター側の取得
                result = getattr(master, 'getDataFromZabbix')()
            if result[0]:
                # マスター側のデータ取得
                result = getattr(master, 'createNewData')()
            return result
        else:
            return (False, 'Not Master-Node.')
