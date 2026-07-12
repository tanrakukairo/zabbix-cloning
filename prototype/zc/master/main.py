#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import json
from zc.common import *

class ZabbixMaster(ZabbixClone):
    '''
    Zabbix master node operations class
    '''

    def getConfigurationFromZabbix(self):
        '''
        通常のメソッドで取得すると、取得するためのパラメータのバージョン間変更対応が煩雑なため、
        configuration.export()で取れるものはこっちでデータを取得する
        '''
        # 取得対象のIDを抽出
        exportIds = {}
        templateIds=[]
        convSectionToMethod = {}
        for method, section in self.sections['CONFIG_EXPORT'].items():
            # option->methodの逆引き辞書を作る
            convSectionToMethod.update({section: method})
            if method == 'trigger':
                # トリガーの指定は不要なのでパスする
                continue
            items = [item['ZABBIX_ID'] for item in self.LOCAL[method].values()]
            if method == 'template':
                if self.templateSkip:
                    continue
                templateIds = items
            else:
                exportIds.update({section: items})
        
        exportIds = [exportIds]

        # 負荷対策
        # テンプレートはZC_TEMPLATE_SEPARATEごとに分割して別処理
        start = loop = 0
        while len(templateIds) > start:
            loop += 1
            count = self.CONFIG.templateSeparate * loop
            exportIds.append({'templates': templateIds[start:count]})
            start = count

        # configuration.export()の実行、JSONに変換
        exportData = []
        for item in exportIds:
            try:
                data = self.ZAPI.configuration.export(
                    **{
                        'format': 'json',
                        'options': item
                    }
                )
                # mediatype表記ゆれ対応: 出力のmedia_types（ここでしか出てこない） -> importOption/ExportのmediaTypesに変換 
                exportData.append(json.loads(data.replace('media_types', 'mediaTypes')).get('zabbix_export'))
            except Exception as e:
                self.LOGGER.debug(e)
                return (False, 'Failed configuration export.')

        for data in exportData:
            # configurationから不要データを取り除いて成型
            for section, items in data.copy().items():
                # セクション名からメソッド名を引く
                method = convSectionToMethod.get(section, None)
                if not method:
                    # メソッドを引けないものを排除（version/date）
                    data.pop(section)
                    continue
                # LOCALにmethodがなかったら初期化（今のところtriggerくらいのはず）
                if not self.LOCAL.get(method):
                    self.LOCAL[method] = {}
                # マスターノードからの取得処理でない場合にID<->Name変換テーブルのmethodがなければ初期化
                if not self.IDREPLACE.get(method):
                    self.IDREPLACE[method] = {}
                # name要素ルールの例外処理
                if method in ['trigger']:
                    name = None
                else:
                    name = self.getKeynameInMethod(method, 'name')
                # 例外処理用の連番
                id = 0
                # トリガーはテンプレートの影響で分割されてくるので、現在の最大を取得する
                if method == 'trigger':
                    id = [val['ZABBIX_ID'] for val in self.LOCAL['trigger'].values()]
                    id = max(id) + 1 if id else 0                        
                # LOCALに適用
                for item in items:
                    # 例外処理の場合はmethod+idを名前にする
                    itemName = item.get(name)
                    if not itemName:
                        itemName = item.get('uuid', method + str(id))
                        self.LOCAL[method][itemName] = {}
                    self.LOCAL[method][itemName].update(
                        {
                            'NAME': itemName,
                            'DATA': item
                        }
                    )
                    # LOCALに入るはずのデータでZABBIX_IDがないものにidを入れる
                    zId = self.LOCAL[method][itemName].get('ZABBIX_ID', None)
                    if not zId:
                        self.LOCAL[method][itemName]['ZABBIX_ID'] = id
                        # id<->name変換テーブルに追加する
                        self.IDREPLACE[method].update(
                            {
                                id: itemName,
                                itemName: id,
                            }
                        )
                    # カウントアップ
                    id += 1

        return ZC_COMPLETE

    def setVersionDataToStore(self):
        '''
        self.STOREの内容をストアにアップロードする
        アップロード対象はVERSIONとDATA
        '''
        # バージョン情報の新規生成
        self.createNewVersion()

        # DATA
        # 引数は{method: [item,item,...],}
        # ストアへの適用実行
        result = self.setDataToStore(self.NEW)
        if not result[0]:
            return result

        # ファイル出力の場合は終了
        if self.CONFIG.storeType == 'file':
            self.NEW.pop('DESCRIPTION', None)
            return (True, self.NEW)

        # VERSION
        # データが成功してからバージョンを入れる
        # 引数は**{'VERSION_ID': xxx, 'UNIXTIME': 000000000, 'MASTER_VERSION': 'x.x', 'DESCRIPTION': ''}
        result = self.setVersionToStore(**self.NEW)
        if not result[0]:
            return result

        return (True, self.NEW)

    def createNewData(self):

        '''
        マスターノードから現在のデータを取得して新バージョンを作る
        ・テンプレートからデータ取得、execConfigurationExport
        ・ZABBIX_ID指定されているAction/Script/MaintenanceのIDをNAMEに変更
        '''

        # バージョンデータを作っていいのはマスターノードだけ
        if not self.isMaster:
            return (False, 'Not Master Node.')

        # ストアデータ格納変数の初期化
        self.STORE = {}

        # configuration.export対象のデータを取得
        process = 'Export Zabbix Configuration'
        PRINT_PROG(f'{TAB*2}{process}:', self.CONFIG.quiet)
        result = self.getConfigurationFromZabbix()
        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
        if not result[0]:
            self.LOGGER.error(f'{process}: Failed.')
            return result
        self.LOGGER.info(f'{process}: Done.')

        # LOCALのデータをSTOREに複製
        process = 'Convert Zabbix Data to Clone Data'
        for method, data in self.LOCAL.items():
            items = []
            for item in data.values():
                # 特権管理者は処理してはいけないので捨てる
                if method == 'user' and item['NAME'] == ZABBIX_SUPER_USER:
                    continue
                # ユーザーグループの管理者グループは処理してはいけないので捨てる
                if method == 'usergroup' and item['NAME'] == ZABBIX_SUPER_GROUP:
                    continue
                # 5.2対応
                # ロールの特権管理者権限は処理してはいけないので捨てる
                if method == 'role' and item['ZABBIX_ID'] == ZABBIX_SUPER_ROLE:
                    continue
                # マスター側のバージョンコードはここではいらないので抜く
                if method == 'usermacro' and item['NAME'] == ZC_VERSION_CODE:
                    continue
                # ZABBIX_IDはLOCALのものなのでSTOREには不要なので捨てる
                items.append(
                    {
                        'NAME': item['NAME'],
                        'DATA': item['DATA']
                    }
                )
            if items:
                self.STORE[method] = items
        PRINT_TAB(2, self.CONFIG.quiet)
        self.LOGGER.info(f'{process}: Done.')

        # ID変換が必要なメソッドのデータ変換
        for proc in ['PRE', 'MID', 'POST', 'ACCOUNT']:
            process = f'Convert {proc} section Data'
            PRINT_PROG(f'{TAB*2}{process}:', self.CONFIG.quiet)
            result = self.processingMethodData(proc)
            PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
            if not result[0]:
                self.LOGGER.error(f'{process}: Failed.')
                return result
            self.LOGGER.info(f'{process}: Done.')

        # GLOBAL内でこれだけデータ変換処理が必要なので実行
        process = f'Convert Authentication Data'
        PRINT_PROG(f'{TAB*2}{process}:', self.CONFIG.quiet)
        result = self.processingAuthentication()
        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
        if not result[0]:
            self.LOGGER.error(f'{process}: Failed.')
            return result
        self.LOGGER.info(f'{process}: Done.')

        return ZC_COMPLETE
