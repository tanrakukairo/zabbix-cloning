#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from time import sleep
from zc.common import *

class ReplicaApiMixin:
    def setApiToZabbix(self, section):
        '''
        STOREからAPIでZabbixにデータを適用する
        API/REPLACEセクション
        '''
        # 6.0以降対応
        if section == 'GLOBAL':
            # 一般設定系は形式が違うのでここで実行できない
            return (False, 'Cannot Execute GLOBAL sections')

        # セクション内に何もないか、そもそもセクションがない
        if not self.sections.get(section):
            return (True, f'{section} is Empty.')

        sections = self.sections[section]
        if section == 'EXTEND':
            # EXTENDで削除する場合、適用の逆順でないといけない場合があるのでリバース
            # 適用: プロキシグループ -> プロキシ（プロキシグループのExtendが先にリストに入る）
            # 削除: プロキシ -> プロキシグループ（逆順にすることでプロキシが先に削除される）
            sections = list(reversed(sections))

        # データの変換処理
        PRINT_PROG(f'{TAB*2}Processing {section} section:\n', self.CONFIG.quiet)
        process = 'Data Convert'
        result = self.processingMethodData(section)
        if result[0]:
            for res in result[1]:
                PRINT_TAB(3, self.CONFIG.quiet)
                self.LOGGER.info(f'{process}{res}')
        else:
            PRINT_TAB(3, self.CONFIG.quiet)
            self.LOGGER.error(result[1])
            return result

        # セクションの適用
        process = 'API Execute'
        for method in sections:
            items = []
            api = getattr(self.ZAPI, method.replace('Extend', ''))
            # データ操作
            for item in self.STORE.get(method, []):
                if item.get('delete'):
                    # 一つずつ削除する形に変える
                    [items.append({'delete': delete}) for delete in item['delete']]
                else:
                    name = item['NAME']
                    data = item['DATA']
                    if method == 'serviceExtend':
                        # serviceの親子相関対応はserviceの付属情報なのでそちらのIDを使う
                        idName = self.getKeynameInMethod('service', 'id')
                        id = self.replaceIdName('service', name)
                        if id:
                            data.update({idName: id})
                            items.append({'update': data})
                    else:
                        if name in self.LOCAL[method].keys():
                            # LOCALにあるものはupdate、ZABBIX_IDをDATAの中に入れる
                            idName = self.getKeynameInMethod(method, 'id')
                            data[idName] = self.LOCAL[method][name]['ZABBIX_ID']
                            items.append({'update': data})
                        else:
                            # LOCALにないものはcreate
                            items.append({'create': data})
            
            # 実行
            execResult = {'total': len(items),'create': 0, 'update': 0, 'delete': 0}
            for item in items:
                if item.get('update'):
                    function = 'update'
                elif item.get('create'):
                    function = 'create'
                elif item.get('delete'):
                    function = 'delete'
                else:
                    continue
                item = item[function]
                execResult[function] += 1
                # usermacroのグローバルマクロはファンクションにglobalがつくので加工
                if method == 'usermacro':
                    function += 'global'
                try:
                    if function == 'delete':
                        if not self.CONFIG.deleteApi:
                            # API管理設定の削除が無効の場合は削除しない
                            continue
                        getattr(api, function)(item)
                    else:
                        getattr(api, function)(**item)
                except Exception as e:
                    self.LOGGER.debug(e)
                    self.LOGGER.debug(f'DEBUG: Failed Data: {item}')
                    result = (False, f'setApiToZabbix, {method.replace("Extend", "")} {function}.')

                sum = execResult['create'] + execResult['update'] + execResult['delete']
                res = f'{process}[{method}]: {sum}/{execResult["total"]} (create:{execResult["create"]}/update:{execResult["update"]}/delete:{execResult["delete"]})'
                PRINT_PROG(f'\r{TAB*3}{res}', self.CONFIG.quiet)
                if not result[0]:
                    return result
            PRINT_PROG(f'\r{TAB*3}', self.CONFIG.quiet)
            if items:
                self.LOGGER.info(f'{res}')
            else:
                self.LOGGER.info(f'{process}[{method}]: No Data or All Data Excluded.')
            # Zabbixの適用が終わってないことがあったので待機を追加
            sleep(1)

        # API実行が終わったらローカルを更新
        self.getDataFromZabbix()

        return ZC_COMPLETE
