#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.config import ZabbixCloneConfig
from zc.common.utils import *

class ZabbixCloneDatastore():
    '''
    データストアクラス
    データストア側が持ちやすい形のフォーマットへの変換と読み書き

    データ構造：
        'VERSION': {
            'VERSION_ID': 'uuid4で生成',
            'UNIXTIME': 'unixtimeのタイムスタンプ、DynamoDBだとDecimalになってるので注意',
            'MASTER_VERSION': 'バージョン生成時のマスターノードのZabbixバージョン',
            'DESCRIPTION': '生成したマスターノードの情報',
            'EXPIRE': 'dydbで削除実行時の時間+1hのUNIXTIME'
        }
        'DATA': {
            'VERSION_ID': 'このレコードが属するバージョン',
            'METHOD': '≒Zabbix API Method（それ以外のもあるから））',
            'NAME': 'ZABBIX内での名前',
            'DATA': 'データ本体',
            'EXPIRE': 'dydbで削除実行時の時間+1hのUNIXTIME'
        }
    DynamoDB: テーブル名の接頭語として'ZC_'をつける
        VERSION: そのまま、パーティションキーはVERSION_ID,ソートキーはTIMESTAMP
        DATA: パーティションキーはVERSION_ID、DATA_IDをuuid4でソートキー
        両方ともEXPIREで削除を有効
    redis   : VERSIONがdb0、DATAがdb1、データが全部binaryなのでencode/decodeに注意
        VERSION: VERSION_IDがkeyのhash、UNIXTIME/MASTER_VERSIONはそのままhash内のキー
        DATA: VERSION_IDがkeyのhash、{DATA_IDがハッシュ内キー: bz2圧縮JSONテキスト))}
    '''

    # ストア上のZabbixデータ（指定したバージョン）{'METHOD': [{},...]} 検索しないで処理するのでこの形
    STORE = {}
    # ストアから取得したバージョンデータ（全部）
    VERSIONS = {}
    # 使用するデータストアの種類
    storeType = ''
    # データストアの設定＆接続オブジェクト
    storeTables = {
        'VERSION': {
            'primary': 'VERSION_ID',
            'sort':'UNIXTIME',
            'client': None
        },
        'DATA': {
            'primary': 'VERSION_ID',
            'sort': 'DATA_ID',
            'client': None
        }
    }
    # 追加ストア指定
    extendStore = None
    # DynamoDBの負荷調整パラメータ
    dydbLimit = 10
    dydbWait = 2

    # エラーメッセージ関連
    MSG_NON_SUPPORT      = '%s: Non Support Datastore, %s.'
    MSG_CONNECTION_ERROR = '%s: Connection Error, Table:%s.'
    MSG_NO_CONFIG        = '%s: No Exist Connection Config.'
    MSG_FAILED_CLEAR     = '%s: Failed Clear, table:%s.'
    MSG_NO_EXIST_VERSION_CLIENT = 'No Exist VERSION client'

    # Lifecycle

    def __init__(self, CONFIG):

        if not isinstance(CONFIG, ZabbixCloneConfig):
            sys.exit('ZabbixCloneDatastore, Bad Config.')

        # logger
        self.LOGGER = CONFIG.LOGGER

        # directMasterではデータストアの設定の必要なし
        # データストアへの接続情報
        self.storeType = CONFIG.storeType
        self.storeConnect = CONFIG.storeConnect
        self.fileStorePath = CONFIG.fileStorePath
        result = self.initStoreSetting()
        if not result[0]:
            sys.exit(result[1])

        # デフォルト対応以外のデータストア
        if self.storeType not in ['redis', 'dydb', 'file']:
            try:
                # インポートの試行
                import importlib
                module = 'extendDatastore' + self.storeType.capitalize()
                self.extendStore = importlib.import_module(module)
            except:
                sys.exit(f'Non Support Datastore, {self.storeType}')

    # Dispatch

    def functionWrapper(self, **params):
        '''
        各ファンクションの共通処理ラッパー
        呼び出し名 + クラス初期化で設定されたストアのファンクションを実行
        '''
        result = ZC_COMPLETE
        try:
            # このラッパーを呼び出したファンクションの名前
            funcName = inspect.stack()[1].function
        except:
            return (False, 'functionWrapper() cannot be executed directly.')
        # 実ファンクションの指定
        function = getattr(self, funcName + self.storeType.capitalize(), None)
        if not function:
            # デフォルトになければエクステンドストアから指定
            function = getattr(self.extendStore, funcName, None)
            params['storeConnect'] = self.storeConnect
        if function:
            # ファンクションの実行
            result = function(**params) if params else function()
        else:
            # ファンクションがない＝指定のストアに対応していない
            result = (False, self.MSG_NON_SUPPORT % (funcName, self.storeType))
        return result

    # Store initialization

    def initStoreSetting(self):
        '''
        ストアの接続設定初期化
        '''
        result = self.functionWrapper(storeConnect=self.storeConnect)
        if result[0]:
            self.storeTables = result[1]
            result = ZC_COMPLETE
        return result

    def initStoreSettingDydb(self, storeConnect):
        '''
        DynamoDB設定初期化
        '''
        result = (True, self.storeTables)

        import boto3

        # 負荷調整パラメーター
        self.dydbLimit = storeConnect.get('dydbLimit', self.dydbLimit)
        self.dydbWait = storeConnect.get('dydbWait', self.dydbWait)
        resourceOption = {
            'region_name': storeConnect.get('awsRegion', 'us-east-1')
        }
        if storeConnect.get('awsEndpointUrl'):
            resourceOption['endpoint_url'] = storeConnect['awsEndpointUrl']

        # 接続インスタンス生成
        if storeConnect.get('awsAccessId') and storeConnect.get('awsSecretKey'):
            # 設定の認証情報で初期化
            resourceOption.update(
                {
                    'aws_access_key_id': storeConnect['awsAccessId'],
                    'aws_secret_access_key': storeConnect['awsSecretKey']
                }
            )
            dydb = boto3.resource('dynamodb', **resourceOption)
        else:
            # 環境変数認証ファイル、IAM Roleでの初期化
            try:
                dydb = boto3.resource('dynamodb', **resourceOption)
            except:
                result = (False, self.MSG_NO_CONFIG % self.storeType)

        # テーブル操作初期化
        if result[0]:
            for table in self.storeTables.keys():
                self.storeTables[table].update(
                    {
                        'client': dydb.Table(ZC_HEAD + table)
                    }
                )
                try:
                    # テーブルの有効確認
                    if self.storeTables[table]['client'].table_status != 'ACTIVE':
                        result = (False, f'{self.storeType}: No-Active Table, {table}')
                except:
                    # 実行失敗
                    result = (False, self.MSG_CONNECTION_ERROR % (self.storeType, table))
        
        return result

    def initStoreSettingRedis(self, storeConnect):
        '''
        Redis設定初期化
        '''
        result = (True, self.storeTables)

        import redis

        # 接続情報の確認
        if storeConnect.get('redisHost') and storeConnect.get('redisPort'):
            # 接続設定の初期化
            idx = 0 # redisのDB番号
            for table in self.storeTables.keys():
                # bz2圧縮するのでdecode_responsesは不使用
                connectInfo = {
                    'host': storeConnect['redisHost'],
                    'port': storeConnect['redisPort'],
                    'db': idx,
                    'max_connections': 4
                }
                if storeConnect.get('redisPassword'):
                    connectInfo['password'] = storeConnect['redisPassword']
                pool = redis.ConnectionPool(**connectInfo)
                self.storeTables[table]['client'] = redis.StrictRedis(connection_pool=pool)
                # 接続確認
                try:
                    self.storeTables[table]['client'].info()
                except:
                    result = (False, self.MSG_CONNECTION_ERROR % (self.storeType, table))
                idx += 1
        else:
            result = (False, self.MSG_NO_CONFIG % self.storeType)

        return result

    def initStoreSettingFile(self, storeConnect):
        '''
        ダミー
        '''
        return (True, self.storeTables)

    # DynamoDB helpers

    def dydbNum(self, d=None):
        '''
        小数点はstr数字はDecimal
        '''
        if isinstance(d, str) and '.' in d:
            try:
                d = float(d)
            except:
                d = None
        else:
            try:
                d = int(d)
            except:
                d = None
        return d

    def dydbScan(self, table=None, projection=[]):
        '''
        1回1MBを超えた場合の対策Scan
        table: 対象のDynamoDBテーブル
        projection: 取得するAttribute
        '''
        if not table:
            return {'Items':[], 'Count': 0}
        # 取得対象指定
        client = self.storeTables[table]['client']
        params = {'ProjectionExpression': ','.join(projection)} if projection else {}
        try:
            res = client.scan(**params)
        except:
            return {'Items':[], 'Count': 0}
        Items = res['Items']
        # 継続キーが入ってたらなくなるまで繰り返し
        while 'LastEvaluatedKey' in res:
            params.update({'ExclusiveStartKey': res['LastEvaluatedKey']})
            res = client.scan(**params)
            Items.extend(res['Items'])
        return {'Items': Items, 'Count': len(Items)}

    def dydbQuery(self, table=None, version=''):
        '''
        DynamoDB Queryラッパー
        フィルターは１つだけ
        '''
        from boto3.dynamodb.conditions import Key

        if table not in self.storeTables.keys() or not version:
            return {'Items':[], 'Count': 0}
        client = self.storeTables[table]['client']
        # キー指定
        params = {'KeyConditionExpression': Key(self.storeTables[table]['primary']).eq(version)}
        res = client.query(**params)
        Items = res['Items']
        # 継続キーが入ってたらなくなるまで繰り返し
        while 'LastEvaluatedKey' in res:
            params.update({'ExclusiveStartKey': res['LastEvaluatedKey']})
            res = client.query(**params)
            Items.extend(res['Items'])
        return {'Items': Items, 'Count': len(Items)}

    # Store cleanup

    def clearStore(self, table='ALL'):
        '''
        ストア上のデータすべて削除
        '''
        if table == 'ALL':
            tables = ['VERSION', 'DATA']
        elif table in ['VERSION', 'DATA']:
            tables = [table]
        else:
            return (False, f'required ALL / VERSION / DATA, tables:{table}.')
        return self.functionWrapper(tables=tables)

    def clearStoreDydb(self, tables):
        '''
        DynamoDBストアリセット
        '''
        result = ZC_COMPLETE

        for table in tables:
            client = self.storeTables[table]['client']
            primary_key = self.storeTables[table]['primary']
            sort_key = self.storeTables[table]['sort']
            data = self.dydbScan(table, [primary_key, sort_key])
            if not data['Count']:
                # データがなかったら飛ばす
                continue
            # バッチ処理
            count = 0
            try:
                with client.batch_writer() as batch:
                    for row in data['Items']:
                        item = {
                            'Key': {
                                primary_key: row[primary_key],
                                sort_key: row[sort_key]
                            }
                        }
                        try:
                            batch.delete_item(**item)
                        except:
                            break
                        # 負荷調整、dydbLimit*10件ごとに1秒の待機
                        count += 1
                        if count > self.dydbLimit*10:
                            sleep(self.dydbWait)
                            count = 0
            except Exception as e:
                self.LOGGER.debug(e)
                result = (False, self.MSG_FAILED_CLEAR % (self.storeType, tables))

        return result

    def clearStoreRedis(self, tables):
        '''
        Redisストアリセット
        '''
        result = ZC_COMPLETE

        try:
            for table in tables:
                self.storeTables[table]['client'].flushall()
        except Exception as e:
            result = (False, self.MSG_FAILED_CLEAR % (self.storeType, tables))
        return result

    def deleteRecordInStore(self, versionId='', dataId=''):
        '''
        未実装
        対象バージョンの特定レコードをDATAテーブルから消す
        dydb: 実行時刻から1時間後のEXPIREを設定して、DynamoDB側に消させる
        redis: 即削除実行
        '''
        if not versionId:
            return (False, 'No Exist Version ID.')
        if not dataId:
            return (False, 'No Exist Data ID.')
        try:
            uuid.UUID(versionId)
            uuid.UUID(dataId)
        except:
            return (False, 'versionId/dataId Must be UUID.')
        return self.functionWrapper(version=versionId, data=dataId)

    def deleteRecordInStoreDydb(self, version, data):
        return (False, f'{version}/{data}')

    def deleteRecordInStoreRedis(self, version, data):
        return (False, f'{version}/{data}')

    def deleteVersionInStore(self, versionId=''):
        '''
        未実装
        対象バージョンをVERSION/DATAテーブルから消す
        dydb: 実行時刻から1時間後のEXPIREを設定して、DynamoDB側に消させる
        redis: 即削除実行
        '''
        if not versionId:
            return (False, 'No Exist Version.')
        try:
            uuid.UUID(versionId)
        except:
            return (False, 'versionId Must be UUID.')
        return self.functionWrapper(version=versionId)

    def deleteVersionInStoreDydb(self, version):
        return (False, f'{version}')

    def deleteVersionInStoreRedis(self, version):
        return (False, f'{version}')

    # File-store helpers

    def getDatasetFromFile(self, versionId):
        '''
        未実装
        データストアにJSONファイルから移植する
        VERSION/DATAともに1ファイルに入っている
        defaultDir: /ver/lib/zabbix/zc/datastore/
        filename: {versionId}.json
        '''
        result = ZC_COMPLETE
        if not versionId:
            return (False, 'No Exist Version.')
        try:
            uuid.UUID(versionId)
        except:
            return (False, 'versionId Must be UUID.')
        return result

    # VERSION table read/write

    def getVersionFromStore(self, version=''):
        '''
        version: ターゲットバージョン、Noneならすべて
        '''
        result = self.functionWrapper(
            version=version,
            client=self.storeTables['VERSION']['client']
        )
        if result[0]:
            # TIMESTAMPで降順に整列（[0]が最新）してクラス変数に入れる
            self.VERSIONS = sorted(result[1], key=lambda x:x['UNIXTIME'], reverse=True)
        else:
            result = (False, f'{self.storeType}: {result[1]}')

        return result

    def getVersionFromStoreDydb(self, **params):
        '''
        DynamoDBからVERSIONの全データを取得
        返値: (boolean, versions)
        '''
        version = params.get('version')
        versions = []
        try:
            # 1MB以上のダウンロードに対応したスキャンファンクションを使う
            dls = self.dydbScan('VERSION')
            dls = [dl for dl in dls['Items'] if dl['VERSION_ID'] == version] if version else dls['Items']
            for dl in dls:
                # 成型して追加
                versions.append(
                    {
                        'VERSION_ID': dl['VERSION_ID'],
                        'UNIXTIME': self.dydbNum(dl['UNIXTIME']),
                        'MASTER_VERSION': self.dydbNum(dl['MASTER_VERSION']),
                        'DESCRIPTION': dl['DESCRIPTION']
                    }
                )
            result = (True, versions)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, [{}])
        return result

    def getVersionFromStoreRedis(self, **params):
        '''
        RedisからVERSIONの全データを取得
        返値: (boolean, versions)
        '''
        version = params.get('version')
        client = params.get('client')
        if not client:
            return (False, self.MSG_NO_EXIST_VERSION_CLIENT)
        versions = []
        try:
            # Redisスキャン
            dls = client.scan()
            dls = [dl.decode() for dl in dls[1]]
            if version in dls:
                # ターゲットバージョンのみ取得
                dls = [version]
            for id in dls:
                # バリューの取得
                dl = client.hgetall(id)
                # 成型して追加
                versions.append(
                    {
                        'VERSION_ID': id,
                        'UNIXTIME': int(dl[b'UNIXTIME']),
                        'MASTER_VERSION': float(dl[b'MASTER_VERSION']),
                        'DESCRIPTION': dl[b'DESCRIPTION'].decode()
                    }
                )
            result = (True, versions)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, [{}])
        return result

    def getStorePath(self):
        if self.fileStorePath:
            return self.fileStorePath
        if os.name == 'nt':
            return os.path.join(os.environ.get('userprofile'), ZC_FILE_STORE[1], 'zc')
        return os.path.join(ZC_FILE_STORE[0], 'zc')

    def getVersionFromStoreFile(self, **params):
        '''
        ディレクトリのファイルリストを取得
        '''
        version = params.get('version')
        versions = []
        # Windowsとその他でディレクトリを変える
        path = self.getStorePath()

        # ファイル名の取得
        files = [item for item in os.listdir(path) if os.path.isfile(os.path.join(path, item))]
        # タイムスタンプの取得
        for file in files:
            desc = file
            file = file.replace('.bz2', '').split('_')
            if version:
                if version != file[0]:
                    continue
            versions.append(
                {
                    'VERSION_ID': file[0],
                    'UNIXTIME': int(file[1]),
                    'MASTER_VERSION': float(file[2]),
                    'DESCRIPTION': f'Import File {desc}'
                }
            )
        return (True, versions)

    def setVersionToStore(
            self,
            VERSION_ID='__NOT_YET_CLONE__',
            UNIXTIME=UNIXTIME(),
            MASTER_VERSION=str(ZC_DEFAULT_ZABBIX_VERSION),
            DESCRIPTION=''
        ):
        '''
        ストアにバージョンデータを追加する
        '''
        version = {
            'VERSION_ID':VERSION_ID,
            'UNIXTIME': UNIXTIME,
            'MASTER_VERSION': str(MASTER_VERSION),
            'DESCRIPTION': str(DESCRIPTION)
        }
        client = self.storeTables['VERSION']['client']
        result = self.functionWrapper(version=version, client=client)
        if not result[0]:
            result = (False, f'{self.storeType}: {result[1]}\n{json.dumps(version)}')
        return result

    def setVersionToStoreDydb(self, **params):
        '''
        DynamoDBにバージョンデータを追加する
        返値: (boolean, message)
        '''
        result = ZC_COMPLETE
        version = params.get('version')
        if not version:
            return (False, 'No Exist VERSION data')
        client = params.get('client')
        if not client:
            return (False, self.MSG_NO_EXIST_VERSION_CLIENT)
        try:
            # 実行
            res = client.put_item(**{'Item': version})
            resCode = res['ResponseMetadata'].get('HTTPStatusCode')
            if resCode != 200:
                result = (False, f'Bad Response put_item, {resCode}.')
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, f'Except put_item.')
        return result

    def setVersionToStoreRedis(self, **params):
        '''
        Redisにバージョンデータを追加する
        返値: (boolean, message)
        '''
        result = ZC_COMPLETE
        version = params.get('version')
        if not version:
            return (False, 'No Exist VERSION data')
        client = params.get('client')
        if not client:
            return (False, self.MSG_NO_EXIST_VERSION_CLIENT)
        # キーは別パラメーターなので取り出す
        versionId = version.pop('VERSION_ID', None)
        try:
            # 実行
            res = client.hset(versionId, mapping=version)
            if not res:
                result = (False, 'Bad Response VERSION hset.')
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, f'Except VERSION hset.')
        return result

    def setVersionToStoreFile(self, **params):
        '''
        ダミー
        '''
        return ZC_COMPLETE

    # DATA table read/write

    def getDataFromStore(self, version=None):
        '''
        ストアから対象のバージョンのDATAを取得する
        返値: [{method: [],...}]
        '''
        if not version:
            return (False, [])
        client = self.storeTables['DATA']['client']
        if not client and not self.storeType == 'file':
            return (False, [])
        result = self.functionWrapper(version=version, client=client)
        return result

    def getDataFromStoreDydb(self, **params):
        '''
        DynamoDBから対象バージョンのDATAを取得する
        返値: (boolean, [{item},...])
        '''
        data = []
        version = params['version']
        # VERSION_IDでフィルタしてダウンロード
        items = self.dydbQuery('DATA', version['VERSION_ID'])
        if not items['Count']:
            return (False, data)
        for item in items['Items']:
            try:
                # {METHOD:'', 'DATA_ID': '', 'NAME':'', 'DATA': b'encodedValue'})',...}
                # DATAのvalueを取り出してbz2でコード、json.loadsでdictに変換
                item['DATA'] = json.loads(bz2.decompress(item['DATA'].value).decode())
                data.append(item)
            except Exception as e:
                self.LOGGER.debug(e)
                return (False, data)
        return (True, data)

    def getDataFromStoreRedis(self, **params):
        '''
        Redisから対象バージョンのDATAを取得する
        返値: (boolean, [{item},...])
        '''
        version = params['version']
        client = params['client']
        data=[]
        try:
            version = version['VERSION_ID']
            # Redisスキャン
            scan = client.scan()
            if version not in [item.decode() for item in scan[1]]:
                return (False, f'No Exist {version}.')
            items = client.hgetall(version)
            # 成型して追加
            for dataId, item in items.items():
                # データのbz2解凍
                item = json.loads(bz2.decompress(item).decode())
                data.append(
                    {
                        'DATA_ID': dataId.decode(),
                        'METHOD': item['METHOD'],
                        'NAME': item['NAME'],
                        'DATA': item['DATA']
                    }
                )
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, f'{e}')

        return (True, data)

    def getDataFromStoreFile(self, **params):
        '''
        ストアデータをファイルから読み込む
        '''
        version = params['version']

        file = '%s_%s_%s.%s' % (
            version['VERSION_ID'],
            version['UNIXTIME'],
            version['MASTER_VERSION'],
            'bz2'
        )

        # Windowsとその他でディレクトリを変える
        path = self.getStorePath()
        file = os.path.join(path, file)

        # ファイル読み込み
        if os.path.exists(file) and os.access(file, os.R_OK):
            try:
                with open(file, 'rb') as f:
                    self.STORE = json.loads(bz2.decompress(f.read()).decode())
            except Exception as e:
                self.LOGGER.debug(e)
                return (False, f'Cannot Read {file}.')

        return ZC_COMPLETE

    def setDataToStore(self, version=None):
        '''
        ストアにデータを追加する
        '''
        result = ZC_COMPLETE
        if not version or not self.STORE:
            return (False, 'Bad Parameters.')
        client = self.storeTables['DATA']['client']
        if not client and not self.storeType == 'file':
            return (False, 'No Exist DATA Client.')
        # DATA_IDを追加
        for items in self.STORE.values():
            for item in items:
                item.update({'DATA_ID': str(uuid.uuid4())})
        # 実行
        result = self.functionWrapper(version=version, dataset=self.STORE, client=client)
        if not result[0]:
            return (False, f'{self.storeType}: {result[1]}')
        return result

    def setDataToStoreDydb(self, **params):
        '''
        DynamoDBにデータを追加する
        返値: (boolean, message)
        '''
        result = ZC_COMPLETE
        version = params['version']
        dataset = params['dataset']
        client = params['client']
        # データをDynamoDBのテーブルに合わせて１レコード１アイテムに変換
        # 1レコード400KBの制限があるのでDATAはbz2圧縮、大きいのはテンプレートのデータ
        setItems = []
        for method, items in dataset.items():
            for item in items:
                setItems.append(
                    {
                        'VERSION_ID': version['VERSION_ID'],
                        'DATA_ID': item['DATA_ID'],
                        'METHOD': method,
                        'NAME': item['NAME'],
                        'DATA': bz2.compress(json.dumps(item['DATA'], ensure_ascii=False).encode())
                    }
                )
        # DynamoDBバッチ処理
        count = 0
        with client.batch_writer() as batch:
            for item in setItems:
                try:
                    batch.put_item(**{'Item': item})
                except Exception as e:
                    self.LOGGER.debug(e)
                    result = (False, f'Faild batch execute put_item.')
                    break
                # 負荷調整処理、dydbLimit数ごとにdydbWait秒待機する
                # DynamoDBのWrite側インスタンス数設定に注意すること、AutoScalingしてると負荷によってはめっちゃでかくなる
                count += 1
                if count > self.dydbLimit:
                    sleep(self.dydbWait)
                    count = 0
        return result

    def setDataToStoreRedis(self, **params):
        '''
        Redisにデータを追加する
        返値: (boolean, message)
        '''
        result = ZC_COMPLETE
        # dataset = {'METHOD': [{'DATA_ID': '', 'NAME': '', 'DATA': {ZabbixMethodData}}], {},...}
        version = params['version']
        dataset = params['dataset']
        client = params['client']
        # データ変換、dict->JSON->bz2圧縮
        data = {}
        try:
            for method, items in dataset.items():
                for item in items:
                    data.update(
                        {
                            item['DATA_ID']: bz2.compress(
                                json.dumps(
                                    {
                                        'METHOD': method,
                                        'NAME': item['NAME'],
                                        'DATA': item['DATA']
                                    },
                                    ensure_ascii=False
                                ).encode()
                            )
                        }
                    )
            res = client.hset(version['VERSION_ID'], mapping=data)
            if not res:
                result = (False, 'Bad Response DATA hset')
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, f'Except DATA hset.')
        return result

    def setDataToStoreFile(self, **params):
        '''
        ストアデータをファイルに書き込む
        '''
        version = params.get('version')
        result = ZC_COMPLETE
        if not version:
            return (False, 'version Empty.')
        
        file = '%s_%s_%s.%s' % (
            version['VERSION_ID'],
            version['UNIXTIME'],
            version['MASTER_VERSION'],
            'bz2'
        )

        # Windowsとその他でディレクトリを変える
        path = self.getStorePath()

        # ファイル書き込み
        if os.path.exists(path) and os.access(path, os.W_OK):
            file = os.path.join(path, file)
            try:
                with open(file, mode='wb') as f:
                    f.write(bz2.compress(json.dumps(self.STORE, ensure_ascii=False).encode()))
            except Exception as e:
                self.LOGGER.debug(e)
                result = (False, f'Cannot Write {file}.')
        else:
            result = (False, f'No Such or Not Writable {path}')
        return result
