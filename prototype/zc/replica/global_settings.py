#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaGlobalSettingsMixin:
    def setGlobalsettingsToZabbix(self):
        '''
        グローバル設定／正規表現の適用
        6.0以降はAPIで対応
        '''
        result = ZC_COMPLETE

        # マスターではこのファンクションは実行不要
        if self.isMaster:
            return (False, 'Not Execute with master-node.')
 
        if self.getLatestVersion('MASTER_VERSION') < 6.0:
            if not self.STORE.get('database'):
                # 入れ替えるデータがない
                return (True, 'No Exist DB_DIRECT data.')

            methodName = 'Convert' if self.VERSION.major >= 6.0 else 'Update'
            pattern = 'Data to API Data' if self.VERSION.major >= 6.0 else 'Data'
            process = f'{methodName} Database {pattern} {self.getLatestVersion("MASTER_VERSION")}->{self.VERSION.major}'    

            # マスターノード6.0以前のDatabaseデータバージョン間変化の操作
            # ワーカーノード6.0以降用の処理
            settings = self.LOCAL.get('settings')
            authentication = self.LOCAL.get('authentication')
            self.STORE['settings'] = []
            self.STORE['regexp'] = []
            self.STORE['authentication'] = []

            dbDirect = {}
            expressions = []

            tables = ['config', 'expressions', 'regexps']
            names = [item['NAME'] for item in self.STORE['database']]
            # 処理順を固定
            for table in tables:
                PRINT_PROG(f'{TAB*2}{process}[{table}]:', self.CONFIG.quiet)
                data = self.STORE['database'][names.index(table)]
                if not data:
                    continue
                data = data['DATA']
                if table == 'config':
                    # col名変更対応
                    for ver, renames in self.dbConfigRenameCols.items():
                        # 自身のバージョンが適用されたバージョンより新しければカラムの名前を変える
                        if self.VERSION.major >= float(ver):
                            for rename in renames:
                                if rename[0] in data[0]:
                                    # rename対象のインデックス取得と対象の内容を取得
                                    idx = data[0].index(rename[0])
                                    value = data[1][idx]
                                    # 変更前のものを削除
                                    del data[0][idx]
                                    del data[1][idx]
                                    # 末尾に変更するものを追加
                                    data[0].append(rename[1])
                                    data[1].append(value)
                    # ワーカーノードが6.0以上ならばsettingsが存在するので、STOREにconfigの内容を追加
                    if settings and authentication:
                        for param in data[0]:
                            # データの成型
                            item = {
                                'NAME': param,
                                'DATA': {
                                    param: data[1][data[0].index(param)]
                                }
                            }
                            # ローカルにあるものだけSTOREにいれる
                            if param in settings.keys():
                                # settingsにある
                                method = 'settings'
                            elif param in authentication.keys():
                                # authenticationにある
                                method = 'authentication'
                            else:
                                # 除去
                                continue
                            self.STORE[method].append(item)
                        # colの廃止処理は不要
                        continue
                    # 廃止されたColのデータ削除
                    for version, drops in self.dbConfigDropCols.items():
                        # 自身のバージョンが適用されたバージョンより新しければカラムを削除する
                        if self.VERSION.major >= version:
                            for drop in drops:
                                if drop in data[0]:
                                    idx = data[0].index(drop)
                                    del data[0][idx]
                                    del data[1][idx]
                elif table == 'expressions':
                    # expressionデータの変換
                    for row in data[1:]:
                        exp = {}
                        for header in data[0][1:]:
                            exp[header] = row[data[0].index(header)]
                        expressions.append(exp)
                else:
                    # regexpのデータ作成
                    self.STORE['regexp'] = []
                    for row in data[1:]:
                        # expressionsから自分のregexpidのものを取得
                        exps = [item for item in expressions if item.get('regexpid') == row[0]]
                        if not exps:
                            continue
                        # regexpidを除去
                        [item.pop('regexpid', None) for item in exps]
                        self.STORE['regexp'].append(
                            {
                                'NAME': row[1],
                                'DATA': {
                                    'name': row[1],
                                    'expressions': exps
                                }
                            }
                        )
                # DB直用データに追加
                dbDirect[table] = data
                PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                self.LOGGER.info(f'{process}[{table}]: Done.')

        if self.STORE.get('settings'):
            # マスターノードのデータが6.0以降はAPIで設定
            process = 'Set Global Settings[API]'
            subProcess = ''
            PRINT_PROG(f'{TAB*2}{process}', self.CONFIG.quiet)
            # グローバル設定
            globalSettings = {}
            for item in self.STORE['settings']:
                if item['NAME'] in self.discardParameter['settings']:
                    continue
                globalSettings.update(item['DATA'])

            if self.CONFIG.settings:
                subProcess = '(Read from Config File)'
                # 重要度文言設定の読み込み
                for lv, sev in self.CONFIG.settings.get('severity', {}).items():
                    if sev.get('name'):
                        globalSettings.update({'severity_name_' + lv: sev['name']})
                    if sev.get('color') and int(sev['color'], 16):
                        globalSettings.update({'severity_color_' + lv: sev['color']})

                # 7.0以降のタイムアウト設定の読み込み
                if self.VERSION.major >= 7.0:
                    for target, value in self.CONFIG.settings.get('timeout', ZC_TIMEOUT_LOWER).items():
                        target.replace('timeout_', '')
                        # TIMEOUTの対象か確認
                        if target not in self.timeoutTarget:
                            continue
                        value = str(value)
                        # SUFFIX外す
                        if 's' in value:
                            value.rstrip('s')
                            suffix = 's'
                        elif 'm' in value:
                            value.rstrip('m')
                            suffix = 'm'
                        else:
                            # 数字じゃない場合は無視（hとかdとか入れてるのを想定）
                            if not value.isdigit():
                                continue
                            suffix = 's'
                        value = int(value)
                        # 分は秒に直す
                        if suffix == 'm':
                            value = value * 60
                        # 制限範囲の確認
                        if value < 1:
                            # Zabbix仕様の下限1秒
                            value = 1
                        elif value > 600:
                            # Zabbix仕様の上限600秒
                            value = 600
                        # ZCにおける下限指定
                        if ZC_TIMEOUT_LOWER.get(target) and value < ZC_TIMEOUT_LOWER[target]:
                            value = ZC_TIMEOUT_LOWER[target]
                        globalSettings.update({f'timeout_{target}': f'{value}s'})
                PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                self.LOGGER.info(f'{process}{subProcess}: Done.')

            # settingsの適用
            if globalSettings:
                try:
                    self.ZAPI.settings.update(**globalSettings)
                    PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                    self.LOGGER.info(f'{process}: Success.')
                except Exception as e:
                    self.LOGGER.debug(e)
                    result = (False, 'Failed Update Global Settings.')

            # secret globamacroの追加 secretが5.0以降なので一応確認
            if result[0] and self.VERSION.major >= 5.0:
                subProcess = '(Secret GlobalMacro)'
                for item in self.CONFIG.secretGlobalmacro:
                    try:
                        # 必要項目があるか確認も込みでgetを使わない
                        macro = {
                            'macro': item['macro'],
                            'value': item['value'],
                            'type': 1
                        }
                        self.ZAPI.usermacro.createglobal(**macro)
                        PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                        self.LOGGER.info(f'{process}{subProcess}: Success.')
                    except Exception as e:
                        self.LOGGER.debug(e)
                        result = (False, f'Failed Secret Globalmacro/create {item}.')
        else:
            # データベース直の適用実行
            process = 'Set Global Settings[Database]'
            for table in reversed(tables):
                data = dbDirect[table]
                if table == 'config':
                    result = self.operateDbDirect('update', table, data)
                else:
                    result = self.operateDbDirect('replace', table, data)
                if not result[0]:
                    PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                    self.LOGGER.error(f'{process}({table}): Failed.')
                    break
                PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                self.LOGGER.info(f'{process}({table}): Success.')

        return result
