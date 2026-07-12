#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.config import ZabbixCloneConfig
from zc.common.parameters import ZabbixCloneParameter
from zc.common.datastore import ZabbixCloneDatastore
from zc.common.utils import *

class CloneLifecycleMixin:
    '''
    Lifecycle and startup processing for clone operations.
    '''

    def __init__(self, CONFIG):

        # loggerインスタンス

        # pyzabbixインスタンス
        self.ZAPI = None
        # 生成した新バージョンデータ
        self.NEW = {}
        # ノード上のZabbixデータ{'METHOD': {'NAME': {}},{'NAME': {}}...} 名前で検索するのでこの形
        self.LOCAL = {}
        # Zabbix IDとZabbix Nameの変換テーブル
        self.IDREPLACE = {}
        # ノードのZabbixバージョン
        self.VERSION = None

        # 設定の適用
        if not isinstance(CONFIG, ZabbixCloneConfig):
            sys.exit('ZabbixClone, Bad Config.')
        if not CONFIG.result[0]:
            sys.exit(CONFIG.result[1])
        self.CONFIG = CONFIG
        self.LOGGER = CONFIG.LOGGER
        # APIクライアントの初期化
        result = self.initZabbixApi()
        if not result[0]:
            sys.exit(result[1])
        self.ZAPI = result[1]
        # 実行対象のZabbix Version取得
        try:
            self.VERSION = self.ZAPI.api_version()
        except Exception as e:
            self.LOGGER.debug(e)
            self.LOGGER.error('[ABORT] Cannot Get zabbix version info.')
            sys.exit(2)

        # 権限確認
        if not self.CONFIG.token:
            data = {}
            try:
                # 5.4対応 キー名の変更
                if self.VERSION.major >= 5.4:
                    name = 'username'
                else:
                    name = 'alias'
                data = self.ZAPI.user.get(output='extend', filter={name: self.CONFIG.auth['user']})
                data = data[0]
            except:
                user = self.CONFIG.auth['user']
                self.LOGGER.error(f'[ABORT] Cannot Get {user} Information.')
                sys.exit(3)
            if self.VERSION.major >= 5.2:
                permit = 'roleid'
            else:
                permit = 'type'
            if int(data.get(permit)) != ZABBIX_SUPER_ROLE:
                self.LOGGER.error(f'[ABORT] No SuperAdministrator Permission, {data[name]}.')
                sys.exit(4)

        # Zabbix DB接続設定、6.0以降はDB直接接続は使用しない
        if self.VERSION.major < 6.0:
            result = self.initDbConnect()
            if not result[0]:
                self.LOGGER.error(f'[ABORT] DB Error:{result[1]}')
                sys.exit(5)

        # 継承クラスの初期化（ZabbixCloneParameter）
        ZabbixCloneParameter.__init__(self, self.VERSION, self.LOGGER)

        if self.CONFIG.storeType != 'direct':
            # データストアを初期化、パラメーターはデータストア内のを使う
            # self.VERSIONS/self.STOREはZabbixCloneDatastore()のクラス変数
            ZabbixCloneDatastore.__init__(self, self.CONFIG)
    @property
    def isMaster(self):
        return self.CONFIG.role == 'master'
    @property
    def isReplica(self):
        return self.CONFIG.role == 'replica'
    @property
    def templateSkip(self):
        return self.CONFIG.templateSkip
    def firstProcess(self):
        '''
        インスタンスの初期化後に最初に行うマスター/ワーカー共通処理
        ・バージョン情報の取得
        ・Zabbixからデータ初回取得
        ・データストアから最新バージョンの取得
        ・必須ホストグループの確認、追加、名前変更
        ・プロキシの削除
        '''
        result = ZC_COMPLETE

        # バージョン情報の取得
        if self.CONFIG.storeType == 'direct':
            # DirectMaster用バージョンの生成
            self.VERSIONS = [
                {
                    'VERSION_ID': '__DIRECT_MASTER_%s__' % ZABBIX_TIME(),
                    'TIMESTAMP': -1,
                    'DESCRIPTION': ''
                }
            ]
        else:
            result = self.getVersionFromStore()
            if result[0]:
                if self.isMaster:
                    if not self.VERSIONS:
                        # これから作るので仮バージョンを生成
                        self.VERSIONS = [
                            {
                                'VERSION_ID': '__FIRST_CREATE__',
                                'TIMESTAMP': -1,
                                'MASTER_VERSION': self.VERSION.major,
                                'DESCRIPTION': ''
                            }
                        ]
                else:
                    if not self.VERSIONS:
                        # ワーカー側はストアのバージョンデータがないので実行不可
                        result = (False, 'No Exist On-Store Versions.')
                    elif self.VERSION.major < self.getLatestVersion('MASTER_VERSION'):
                        # ワーカーのZabbixバージョンがマスターのZabbixバージョンより古い場合は終了
                        result = (False, f'{self.CONFIG.node} zabbix version > Onstore Data zabbix version.')
                    else:
                        pass
            else:
                # 取得失敗
                result = (False, 'Failed Get Versions.')
        process = 'Check Target Version Data'
        PRINT_TAB(2, self.CONFIG.quiet)
        if result[0]:
            self.LOGGER.info(f'{process}: Success.')
        else:
            self.LOGGER.error(f'{process}: Failed.')
            return result
    
        # データの初回取得
        result = self.getDataFromZabbix()
        process = 'Get Node Zabbix Data'
        PRINT_TAB(2, self.CONFIG.quiet)
        if result[0]:
            self.LOGGER.info(f'{process}: Success.')
        else:
            self.LOGGER.error(f'{process}: Failed.')
            return result


        if self.isMaster:
            # マスターノードの処理

            # 4.2以降
            if self.VERSION.major >= 4.2:
                # ホストに管理UUIDタグをつける
                # ワーカーでアップデートするときにホストのユニーク情報として使う予定（ホスト名変更があるとZCではわからないので）
                # 表示（仮）
                process = 'Set Host UUID'
                count = {
                    'total': len(self.LOCAL['host']),
                    'exist': 0,
                    'set': 0,
                    'failed': 0
                }
                failedHost = []
                for item in self.LOCAL['host'].values():
                    # ユニーク識別のタグが付いていないことを確認
                    if ZC_UNIQUE_TAG not in [tag['tag'] for tag in item['DATA']['tags']]:
                        # UUIDを生成して追加
                        item['DATA']['tags'].append(
                            {
                                'tag': ZC_UNIQUE_TAG,
                                'value': str(uuid.uuid4())
                            }
                        )
                        idName = self.getKeynameInMethod('host', 'id')
                        option = {
                            idName: item['ZABBIX_ID'],
                            'tags': item['DATA']['tags']
                        }
                        try:
                            # 適用実行
                            self.ZAPI.host.update(**option)
                            count['set'] += 1
                        except Exception as e:
                            self.LOGGER.debug(e)
                            failedHost.append(item['NAME'])
                            count['failed'] += 1
                    else:
                        count['exist'] += 1

                    sum = count['exist'] + count['set'] + count['failed']
                    res = f'{sum}/{count["total"]} (exist:{count["exist"]}/set:{count["set"]}/failed:{count["failed"]})'
                    PRINT_PROG(f'\r{TAB*2}{process}: {res}', self.CONFIG.quiet)

                PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                self.LOGGER.info(f'{process}: {res}')
                if failedHost:
                    PRINT_TAB(3, self.CONFIG.quiet)
                    self.LOGGER.error(f'{" ,".join(failedHost)}')

        else:
            # ワーカーノード側処理
            if self.getLatestVersion('MASTER_VERSION') == 4.0:
                # マスターバージョンが4.0の場合、ZC_UUIDが存在しないのでホスト強制アップデートモードにする
                self.CONFIG.hostUpdate = True
                self.CONFIG.forceHostUpdate = True

            # 適用バージョンの確認
            version = self.CONFIG.targetVersion
            if version is None:
                version = self.getLatestVersion('VERSION_ID')
            elif version not in [item['VERSION_ID'] for item in self.VERSIONS]:
                version = self.getLatestVersion('VERSION_ID')
                lostVersion = self.CONFIG.targetVersion
                self.CONFIG.targetVersion = False
            else:
                # 適用バージョンを先頭に入れ替え（getLatest～の値変更）
                version = [item for item in self.VERSIONS if item['VERSION_ID'] == version]
                self.VERSIONS.remove(version[0])
                self.VERSIONS.insert(0, version[0])
                version = version[0]['VERSION_ID']
            
            PRINT_TAB(2, self.CONFIG.quiet)
            self.LOGGER.info(f'Cloning Version: {version}')
            if self.CONFIG.targetVersion is False:
                PRINT_TAB(3, self.CONFIG.quiet)
                self.LOGGER.info(f'No Exist Version:{lostVersion}, Change Latest:{version}.')
            info = self.getLatestVersion('DESCRIPTION')
            if info:
                PRINT_TAB(2, self.CONFIG.quiet)
                tab = TAB*3
                info = info.replace(', ', f'\n{tab}')
                self.LOGGER.info(f'Version Information:\n{tab}{info}')

            # 適用状態の確認
            nowVersion = self.LOCAL['usermacro'].get(ZC_VERSION_CODE, None)
            if nowVersion:
                # クローンしたことがあるワーカーノード
                nowVersion = nowVersion['DATA']['value']
                try:
                    # バージョン文字列がUUIDか確認
                    uuid.UUID(nowVersion)
                    initialize = self.CONFIG.initialize
                except:
                    if re.match(r'__DIRECT_MASTER_\d{4}-\d{2}-\d{2}T]d{2}:]d{2}:]d{2}Z__', nowVersion):
                        # マスター直接適用は初期化しない
                        initialize = self.CONFIG.initialize
                    else:
                        # バージョン文字列が不正なので初期化対象
                        initialize = True
            else:
                # クローンしたことがないワーカーノード
                # 初期化は明示指定された場合だけ行う
                initialize = self.CONFIG.initialize
                # レプリカモードは強制初期化
                if self.isReplica:
                    initialize = True

            if not initialize:
                # 初期化しない
                # マスターではない
                if not self.isMaster:
                    # 要不要の判断が各メソッドで難しいので初期化しなくても毎回リセットする対象の処理
                    PRINT_TAB(2, self.CONFIG.quiet)
                    self.LOGGER.info('Always Data Reset Methods:')
                    for method in ['correlation', 'drule', 'action', 'script', 'maintenance']:
                        api = getattr(self.ZAPI, method)
                        function = 'delete'
                        ids = [item['ZABBIX_ID'] for item in self.LOCAL[method].values()]
                        if ids:
                            try:
                                getattr(api, function)(*ids)
                                result = ZC_COMPLETE
                            except Exception as e:
                                # 実行失敗で処理中止
                                self.LOGGER.debug(e)
                                result = (False, f'Failed API, {method}')
                        PRINT_TAB(3, self.CONFIG.quiet)
                        if result[0]:
                            self.LOGGER.info(f'{method}: Success.')
                        else:
                            self.LOGGER.error(f'{method}: Failed.')
                            return result
            else:
                # 初期化する
                PRINT_PROG(f'{TAB*2}Start Initialize:\n', self.CONFIG.quiet)
                process = 'Data Clear'
                # イニシャライズ対象のメソッド、プロキシ、テンプレート、グループは使っているホストがあると消せないので後回しにする
                methods = [
                    'usermacro',
                    'correlation',
                    'drule',
                    'mediatype',
                    'action',
                    'script',
                    'maintenance',
                    'host',
                    'proxy',
                    'template',
                    'hostgroup'
                ]
                # 6.0対応
                if self.VERSION.major >= 6.0:
                    methods = ['service', 'sla', 'regexp'] + methods
                # 6.2対応
                if self.VERSION.major >= 6.2:
                    # hostgroupからtemplategroupに分離されたので追加
                    methods.append('templategroup')
                    # settingsのdiscovery_groupidが削除不能のデフォルトHG
                    systemGroup = self.LOCAL['settings']['discovery_groupid']['DATA']['discovery_groupid']
                else:
                    # 6.0以前はシステムフラグありホストグループが削除不可
                    systemGroup = [item['ZABBIX_ID'] for item in self.LOCAL['hostgroup'].values() if int(item['DATA'].get('internal', 0))]
                    systemGroup = systemGroup[0]
                # 7.0対応
                if self.VERSION.major >= 7.0:
                    methods.append('proxygroup')
                # テンプレート削除スキップ
                if self.templateSkip:
                    methods.remove('hostgroup')
                    methods.remove('template')
                    if self.VERSION.major >= 6.2:
                        methods.remove('templategroup')
                # ZABBIXデフォルト設定の削除
                for method in methods:
                    api = getattr(self.ZAPI, method)
                    function = 'delete'
                    if method == 'hostgroup':
                        # systemGroupは削除不能（実行するとエラー）なので外す
                        ids = [item['ZABBIX_ID'] for item in self.LOCAL[method].values() if item['ZABBIX_ID'] != int(systemGroup)]
                    else:
                        ids = []
                        for item in self.LOCAL[method].values():
                            if self.CONFIG.zabbixCloud and item['NAME'] in self.zabbixCloudSpecialItem.get(method, []):
                                # ZabbixCloud対応: mediatypeの'Cloud Mail'消せないので除外
                                continue
                            else:
                                ids.append(item['ZABBIX_ID'])
                    if method == 'usermacro':
                        function += 'global'
                    if ids != []:
                        try:
                            batchSize = 50
                            for i in range(0, len(ids), batchSize):
                                batch = ids[i:i + batchSize]
                                getattr(api, function)(*batch)
                            result = ZC_COMPLETE
                        except Exception as e:
                            # 実行失敗で処理中止
                            self.LOGGER.debug(e)
                            result = (False, f'Failed API, {method}.')
                    PRINT_TAB(3, self.CONFIG.quiet)
                    if result[0]:
                        self.LOGGER.info(f'{process}[{method}]: {len(ids)} Success.')
                    else:
                        self.LOGGER.error(f'{process}[{method}]: {len(ids)} Failed.')
                        return result

                # バージョン情報の挿入
                result = self.setVersionCode(init=initialize)
                if not result[0]:
                    return result

            # アラート通知ユーザー確認
            # デフォルト通知ユーザーがいるか確認
            alertUser = self.LOCAL['user'].get(ZC_NOTICE_USER)
            if not alertUser:
                result = (False, 'Failed, firstProcess Need Alert User.')
            else:
                data = alertUser['DATA']
                # ユーザーが有効か確認
                if data.get('users_status', ZABBIX_DISABLE) != ZABBIX_ENABLE:
                    result = (False, 'Failed, firstProcess Notified User enabled')
                else:
                    # デフォルト通知ユーザーが特権管理者か確認
                    # 5.2対応 権限管理変更 type -> role
                    if self.VERSION.major >= 5.2:
                        permit = 'roleid'
                    else:
                        permit = 'type'
                    if int(data.get(permit, -1)) != ZABBIX_SUPER_ROLE:
                        result = (False, 'Failed, firstProcess Notified User Permission is not SuperAdministorator.')
            process = f'Check Default Alert User[{ZC_NOTICE_USER}]'
            PRINT_TAB(2, self.CONFIG.quiet)
            if result[0]:
                self.LOGGER.info(f'{process}: Success.')
            else:
                self.LOGGER.error(f'{process}: Failed.')
                return result

        if result[0]:
            result = self.getDataFromZabbix()
        
        return result
    def setVersionCode(self, init=False):
        '''
        グローバルマクロに適用したバージョンの情報を追加する
        init: 初期化フラグがTrueならUUIDではない初期文字列を入れる
        '''
        if self.isMaster:
            version = self.NEW['VERSION_ID']
        else:
            version = '__NOT_YET_CLONE__' if init else self.getLatestVersion('VERSION_ID')

        # ローカルにバージョンのグローバルがあるか確認
        idName = self.getKeynameInMethod('usermacro', 'id')
        versionCode = self.LOCAL['usermacro'].get(ZC_VERSION_CODE)
        if versionCode and not init:
            # あったら更新
            function = 'updateglobal'
            data = {
                idName: versionCode['ZABBIX_ID'],
                'value': version,
            }
        else:
            # なければ追加
            function = 'createglobal'
            data = {
                'macro': ZC_VERSION_CODE,
                'value': version,
            }

        if self.CONFIG.storeType == 'direct':
            directNode = self.CONFIG.storeConnect.get('directNode', self.CONFIG.storeConnect.get('direct_node'))
            directEndpoint = self.CONFIG.storeConnect.get('directEndpoint', self.CONFIG.storeConnect.get('direct_endpoint'))
            data['description'] = 'Master-Node: %s (%s)' % (
                directNode,
                directEndpoint
            )
        process = 'Set VersionCode Globalmacro'
        try:
            getattr(self.ZAPI.usermacro, function)(**data)
            PRINT_TAB(2, self.CONFIG.quiet)
            self.LOGGER.info(f'{process}: Success')
        except Exception as e:
            self.LOGGER.debug(e)
            PRINT_PROG(f'{process}: Failed', self.CONFIG.quiet)
            return (False, f'Failed {function}, Version:{version}.')
        return ZC_COMPLETE
