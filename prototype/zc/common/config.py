#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.logging import __LOGGER__
from zc.common.utils import *

class ZabbixCloneConfig():
    '''
    ZabbixClone設定クラス
    ストア種別がdirectの時はマスター側の接続設定として扱う
    '''

    def __init__(self, **params):
        # logger初期化
        if params.get('LOGGER'):
            self.LOGGER = params['LOGGER']
        else:
            logConfig = DEFAULT_LOG
            if params.get('log_level'):
                logConfig['logLevel'] = params['log_level']
            logConfig['logName'] = params.get('log_name', __name__)
            self.LOGGER = __LOGGER__(**logConfig)

        self.result = None
        self.directMaster = False
        self.configFile = None
        self.result = self.readConfig(**params)

    def readConfig(self, **params):
        '''
        設定ファイルの初期化
        1.指定された設定ファイルまたはデフォルト設定ファイル読み込み
        2.ノード設定ファイル読み込み
        3.引数の処理
        4.内容確認＆デフォルト処理
        '''
        def boolFlag(value, default=False):
            if isinstance(value, bool):
                return value
            if value is None:
                return default
            if isinstance(value, str):
                return value.upper() == 'YES' or value.lower() == 'true'
            return bool(value)

        # no_config_files: YESの場合は引数のみを使用
        CONFIG = {}
        if boolFlag(params.get('no_config_files'), False):
            pass
        else:
            # 指定された設定ファイルまたはデフォルト設定ファイル
            configFile = params.get('config_file', os.path.join(ZABBIX_CONFIG_PATH, ZC_CONFIG))
            if os.path.exists(configFile) and os.access(configFile, os.R_OK):
                self.configFile = configFile
                # 基本設定ファイル読み込み
                try:
                    with open(configFile, 'r') as f:
                        CONFIG = json.load(f)
                except Exception as e:
                    self.LOGGER.debug(e)
                    pass

            # 引数でのファイル指定なし
            if not params.get('config_file'):
                # ユーザー設定読み込み/上書き
                nodeConfig = os.path.join(ZABBIX_USER_CONFIG_PATH, ZC_CONFIG)
                if os.path.exists(nodeConfig) and os.access(nodeConfig, os.R_OK):
                    self.configFile += ' ' + nodeConfig
                    try:
                        userConf = json.load(f)
                        with open(nodeConfig, 'r') as f:
                            for param, value in userConf.items():
                                if param in CONFIG:
                                    CONFIG.update({param: value})
                    except:
                        pass

        # 引数読み込み・上書き
        for param, value in params.items():
            CONFIG.update({param: value})

        # クラス変数化
        self.logLevel = CONFIG.get('log_level', DEFAULT_LOG_LEVEL)
        self.yes = boolFlag(CONFIG.get('yes'), False)
        self.quiet = boolFlag(CONFIG.get('quiet'), False)
        # ノード名
        self.node = CONFIG.get('node', ZC_DEFAULT_NODE)
        # ストア種別
        self.storeType = CONFIG.get('store_type', ZC_DEFAULT_STORE)
        if self.storeType == 'extend':
            self.storeType = CONFIG.get('extend_store', ZC_DEFAULT_STORE)
        # ストア接続情報
        self.storeConnect = CONFIG.get('store_connect', {})
        if self.storeType == 'dydb':
            storeEndpoint = CONFIG.get('store_endpoint')
            configRegion = self.storeConnect.get('aws_region', os.environ.get('AWS_DEFAULT_REGION', 'us-east-1'))
            configEndpointUrl = self.storeConnect.get('aws_endpoint_url')
            if storeEndpoint:
                if re.match(r'^https?://', storeEndpoint):
                    configEndpointUrl = storeEndpoint
                else:
                    configRegion = storeEndpoint
            self.storeConnect.update(
                {
                    'awsAccessId': CONFIG.get(
                        'store_access',
                        self.storeConnect.get('aws_account_id', None)
                    ),
                    'awsSecretKey': CONFIG.get(
                        'store_credential',
                        self.storeConnect.get('aws_secret_key', None)
                    ),
                    'awsRegion': configRegion,
                    'awsEndpointUrl': configEndpointUrl,
                    'dydbLimit': CONFIG.get(
                        'store_limit',
                        self.storeConnect.get('dydb_limit', 10)
                    ),
                    'dydbWait': CONFIG.get(
                        'store_interval',
                        self.storeConnect.get('dydb_wait', 2)
                    ),
                }
            )
        elif self.storeType == 'redis':
            self.storeConnect.update(
                {
                    'redisHost': CONFIG.get(
                        'store_endpoint',
                        self.storeConnect.get('redis_host', 'localhost')
                    ),
                    'redisPort': CONFIG.get(
                        'store_port',
                        self.storeConnect.get('redis_port', 6379)
                    ),
                    'redisPassword': CONFIG.get(
                        'store_credential',
                        self.storeConnect.get('redis_password', None)
                    )
                }
            )
        elif self.storeType == 'direct':
            self.storeConnect.update(
                {
                    'directNode': CONFIG.get(
                        'store_access',
                        self.storeConnect.get('direct_node', None)
                    ),
                    'directEndpoint': CONFIG.get(
                        'store_endpoint',
                        self.storeConnect.get('direct_endpoint', None)
                    ),
                    'directToken': CONFIG.get(
                        'store_credential',
                        self.storeConnect.get('direct_token', None)
                    ),
                }
            )
        else:
            # Extendストアのパラメーター
            try:
                self.storeConnect = json.loads(CONFIG.get('extend_params'))
            except:
                self.storeConnect = {}
        # fileストア保存先
        self.fileStorePath = CONFIG.get('file_store_path', os.environ.get('ZC_FILE_STORE_PATH'))
        # Zabbix接続で自己証明書の利用
        self.selfCert = boolFlag(CONFIG.get('self_cert'), False)
        # ストア設定、デフォルト設定はZabbixCloneDataStore側にあるのでここにはない
        # ロール
        self.role = CONFIG.get('role', ZC_DERAULT_ROLE)
        # Zabbixエンドポイント
        self.endpoint = CONFIG.get('endpoint', 'http://localhost')
        # ZabbixCloudフラグ
        # エンドポイントでZabbixCloudを判定する
        self.zabbixCloud = bool(re.match('https://([a-z0-1-]*).zabbix.cloud(/){0,1}', self.endpoint))
        if self.zabbixCloud:
            self.platformPassword = CONFIG.get('platform_password', None)
        # 認証情報
        self.token = CONFIG.get('token', None)
        self.auth = {
            'user': CONFIG.get('user', ZABBIX_DEFAULT_AUTH['user']),
            'password': CONFIG.get('password', None)
        }
        if self.role == 'worker':
            # 適用指定バージョン
            self.targetVersion = CONFIG.get('version', None)
            # ワーカーがデフォルトパスワードであれば管理者のパスワードを設定ファイル内のものに変更する
            self.updatePassword = boolFlag(CONFIG.get('update_password'), False)
        else:
            # マスターでバージョン指定は不要
            self.targetVersion = None
            # マスターノードのパスワードは操作しない
            self.updatePassword = False
        # ワーカーノードの初期化
        self.initialize = boolFlag(CONFIG.get('initialize'), False)
        if self.role == 'master':
            # マスターは初期化禁止
            self.initialize = False
        # 監視対象のエンドポイントをIP利用に変換
        self.useip = boolFlag(CONFIG.get('useip'), False)
        # 監視対象のアップデート許可
        self.hostUpdate = boolFlag(CONFIG.get('host_update'), False)
        # 監視対象のアップデート強制
        self.forceHostUpdate = boolFlag(CONFIG.get('force_host_update'), False)
        if self.forceHostUpdate:
            self.hostUpdate = True
        # ストアデータにない対象の削除
        self.deleteHost = boolFlag(CONFIG.get('delete_host'), False)
        self.deleteApi = boolFlag(CONFIG.get('delete_api'), False)
        if self.initialize:
            # 初期化優先
            self.deleteHost = False
            self.deleteApi = False
        # checknow実行
        self.checknowExec = boolFlag(CONFIG.get('checknow_execute'), False)
        # checknowの対象インターバル
        self.checknowInterval = CONFIG.get('checknow_interval', ['1h'])
        # checknowを実行する際の設定適用待機時間
        self.checknowWait = CONFIG.get('checknow_wait', 30)
        # 並列実行可能数
        self.phpWorkerNum = int(CONFIG.get('php_worker_num', CONFIG.get('php_work_num', PHP_WORKER_NUM)))
        # DBダイレクト接続設定（Zabbix Server設定を使わない場合の設定）
        self.dbConnect = CONFIG.get('db_connect', {})
        if self.dbConnect:
            if self.dbConnect.pop('type', 'pgsql') == 'pgsql':
                self.dbConnect['port'] = 5432
            else:
                self.dbConnect['port'] = 3306
        # ここから下のものはコマンド引数と環境変数で設定されない
        # Secret global macro対応
        self.secretGlobalmacro = CONFIG.get('secret_globalmacro', [])
        # データ取り込みを有効にするユーザーとそのパスワード（ZabbixAPIでパスワードは取れないので）
        self.enableUser = CONFIG.get('enable_user', {})
        # 特権管理者の複製を許可する
        self.cloningSuperAdmin = boolFlag(CONFIG.get('cloning_super_admin'), False)
        # Proxy PSK情報（ZabbixAPIでpskは取れないので）
        self.proxyPsk = CONFIG.get('proxy_psk', {})
        # グローバル設定
        self.settings = CONFIG.get('settings', {})
        # アラート設定、setAlertMedia()で操作
        self.mediaSettings = CONFIG.get('media_settings', {})
        for media, params in self.mediaSettings.copy().items():
            if not params.get('user'):
                self.mediaSettings.pop(media)
        # マスターノード追加情報
        self.description = CONFIG.get('description', None)
        # MFAシークレット情報
        self.mfaClientSecret = CONFIG.get('mfa_client_secret', {})
        # テンプレートのスキップ
        defSkip = self.role == 'worker'
        self.templateSkip = boolFlag(CONFIG.get('skip_template'), defSkip)
        # ホストのスキップ
        self.hostSkip = boolFlag(CONFIG.get('skip_host'), False)
        if self.role == 'master':
            # マスターはホストのスキップ禁止
            self.hostSkip = False
        # テンプレートのエクスポート時の区切り数
        self.templateSeparate = CONFIG.get('template_separate_num', CONFIG.get('template_separate', ZC_TEMPLATE_SEPARATE))
        # 監視無効化フラグ
        self.disableMonitoring = boolFlag(CONFIG.get('disable_monitoring'), False)
        # 強制初期化時の変更項目
        if self.initialize:
            self.templateSkip = False
            self.hostSkip = False

        return ZC_COMPLETE

    def changeDirectMaster(self):
        '''
        Directモードのマスターとしてコンフィグ変更する
        '''
        self.directMaster = True
        self.role = 'master'
        self.node = self.storeConnect.get('directNode')
        self.endpoint = self.storeConnect.get('directEndpoint')
        self.token = self.storeConnect.get('directToken')
        self.auth = {}
        self.updatePassword = False
        self.templateSkip = False
        self.hostSkip = False
        return ZC_COMPLETE

    def showParameters(self):
        '''
        パラメータ情報の表示
        （仮）
        '''
        dispMessage = []

        line = '[Zabbix Cloning Configurations]'
        dispMessage.append(line)
            
        # 設定ファイル関連
        if self.configFile:
            line = f'{TAB}Config File: {self.configFile}'
        else:
            line = f'{TAB}No Config Files Mode: YES'
        dispMessage.append(line)

        # ノード関連
        dispMessage.append(f'{TAB}Target Node: {self.node}')
        dispMessage.append(f'{TAB*2}Role: {self.role}')
        dispMessage.append(f'{TAB*2}Zabbix Endpoint: {self.endpoint}')
        if self.zabbixCloud:
            dispMessage.append(f'{TAB*2}ZabbixCloud Node: YES')

        # 認証関連
        if self.token:
            dispMessage.append(f'{TAB}Authentication Method: TOKEN')
        else:
            dispMessage.append(f'{TAB}Authentication Method: PASSWORD')
            dispMessage.append(f'{TAB*2}User: {self.auth["user"]}')
        if self.updatePassword is True:
            dispMessage.append(f'{TAB}Update Password: YES')
        if self.selfCert:
            dispMessage.append(f'{TAB}Self Certification Use: YES')

        # 動作設定関連
        if self.initialize:
            dispMessage.append(f'{TAB}Initialize Worker: YES')
        if self.useip:
            dispMessage.append(f'{TAB}Use IP Address Monitoring: YES')
        if self.forceHostUpdate:
            dispMessage.append(f'{TAB}Force Update Exist Hosts: YES')
        else:
            dispMessage.append(f'{TAB}Update Exist Hosts: {self.hostUpdate}')
        if self.role != 'master':
            if self.deleteHost:
                dispMessage.append(f'{TAB}Delete Worker-Node Hosts: YES')
            if self.deleteApi:
                dispMessage.append(f'{TAB}Delete Worker-Node API Items: YES')
        dispMessage.append(f'{TAB}Execute CheckNow after Host Cloning: {self.checknowExec}')
        if self.checknowExec:
            dispMessage.append(f'{TAB*2}CheckNow TargetInterval: {self.checknowInterval}')
            dispMessage.append(f'{TAB*2}CheckNow Wait Sec for Data Apply: {self.checknowWait}')
        if self.role == 'master':
            dispMessage.append(f'{TAB}Configuration Export Skip Template: {self.templateSkip}')
            if self.templateSeparate != ZC_TEMPLATE_SEPARATE:
                dispMessage.append(f'{TAB}Configuration Export Separate Count: {self.templateSeparate}')
        else:
            dispMessage.append(f'{TAB}Configuration Import Skip Template: {self.templateSkip}')
            dispMessage.append(f'{TAB}Configuration Import Skip Host: {self.hostSkip}')
        if self.phpWorkerNum != PHP_WORKER_NUM:
            dispMessage.append(f'{TAB}Number of Parallel Excution Create/Update Hosts: {self.phpWorkerNum}') 

        # ストア関連
        if self.storeType == 'dydb':
            storeType = 'AWS DynamoDB'
        elif self.storeType == 'redis':
            storeType = 'Redis'
        elif self.storeType == 'direct':
            storeType = 'Master-Node Zabbix Direct'
        elif self.storeType == 'file':
            storeType = 'Local File'
        else:
            storeType = f'Extend Store {self.storeType}'
        dispMessage.append(f'{TAB}Store Type: {storeType}')
        if self.storeType == 'dydb':
            dispMessage.append(f'{TAB*2}AWS Region: {self.storeConnect.get("awsRegion", self.storeConnect.get("aws_region", "us-east-1"))}')
            if self.storeConnect.get('awsEndpointUrl'):
                dispMessage.append(f'{TAB*2}DynamoDB Endpoint URL: {self.storeConnect.get("awsEndpointUrl")}')
        elif self.storeType == 'redis':
            host = self.storeConnect.get('redisHost', self.storeConnect.get('redis_host'))
            port = self.storeConnect.get('redisPort', self.storeConnect.get('redis_port'))
            ep = host + ':' + str(port)
            dispMessage.append(f'{TAB*2}Redis Endpoint: {ep}')
        elif self.storeType == 'direct':
            node = self.storeConnect.get('directNode', self.storeConnect.get('direct_node'))
            ep = self.storeConnect.get('directEndpoint', self.storeConnect.get('direct_endpoint'))
            dispMessage.append(f'{TAB*2}Master-Node: {node} ({ep})')
        elif self.storeType == 'extend':
            for name, item in self.storeConnect:
                dispMessage.append(f'{TAB*2}Extend Store Parameter {name}: {item}')
        else:
            pass

        # DB関連
        if self.dbConnect:
            dispMessage.append(f'{TAB}Custom DB Connection: ')
            for param in ['host', 'name', 'port', 'user', 'password']:
                if self.dbConnect.get(param):
                    if param == 'password':
                        item = 'Custom Password'
                    else:
                        item = self.dbConnect[param]
                    dispMessage.append(f'{TAB*2}DB{param.capitalize()}: {item}')

        # 暗号化関連
        if self.secretGlobalmacro:
            macros = ', '.join([macro['macro'] for macro in self.secretGlobalmacro])
            dispMessage.append(f'{TAB}Set Secret GlobalMacro: {macros}')
        if self.proxyPsk:
            proxies = ', '.join(self.proxyPsk.keys())
            dispMessage.append(f'{TAB}Set Proxy PSK: {proxies}')

        # グローバル設定関連
        if self.settings:
            dispMessage.append(f'{TAB}Set Custom Global Settings:')
            origin = {"1":'Information', "2":'Warning', "3":'Average', "4":'High', "5":'Disaster'}
            for lv, param in self.settings.get('severity', {}).items():
                dispMessage.append(f'{TAB*2}AlertLevel.{lv}:')
                name = param.get('name')
                color = param.get('color')
                if name:
                    dispMessage.append(f'{TAB*3}ChangeName: {origin[lv]} -> {name}')
                if color:
                    dispMessage.append(f'{TAB*3}ChangeColor: {color}')
            for timeout, second in self.settings.get('timeout', {}).items():
                dispMessage.append(f'{TAB*2}Timeout {timeout}: {second}')
        if self.enableUser:
            users = ', '.join(self.enableUser.keys())
            dispMessage.append(f'{TAB}Enable Cloning User: {users}')
        if self.mediaSettings:
            medias = ', '.join(self.mediaSettings.keys())
            dispMessage.append(f'{TAB}Use Custom MediaType Setting: {medias}')
            for media, params in self.mediaSettings.items():
                users = ', '.join([user[0] for user in params['user']])
                if users:
                    dispMessage.append(f'{TAB}MediaType[{media}] Set User(s): {users}')
        if self.mfaClientSecret:
            mfa = ', '.join(self.mfaClientSecret.keys())
            dispMessage.append(f'{TAB}MFA Client Secret (MFA Setting Requierd): {mfa}')

        dispMessage.append(f'{TAB}Log level: {self.logLevel}')

        self.LOGGER.info('\n'.join(dispMessage))

        return
