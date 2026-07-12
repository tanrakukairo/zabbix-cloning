#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.utils import *

class CloneConnectionMixin:
    '''
    Zabbix API connection and direct database access for clone operations.
    '''

    def initZabbixApi(self):
        '''
        pyZabbixのクライアントイニシャライズ
        '''

        # ZabbixAPIインスタンス
        API = ZabbixAPI(url=self.CONFIG.endpoint, skip_version_check=True)

        # トークン
        token = self.CONFIG.token
        # パスワード
        auth = self.CONFIG.auth

        # 接続先の名称確認
        # APIで取れるようになったらそっちを使う
        result = CHECK_ZABBIX_SERVER_NAME(self.CONFIG.endpoint, self.CONFIG.node, {'token': token} if token else auth)
        if not result[0]:
            return result

        # 認証情報がない
        if not token and not auth.get('password'):
            return (False, 'No Exist Credentials.')

        # 自己証明書を使う
        if self.CONFIG.selfCert:
            API.session.verify = False

        # トークンで認証確認
        if token:
            try:
                API.login(token=token)
                if self.CONFIG.updatePassword is not True:
                    # トークンで認証したのでパスワード認証しない
                    auth = None
                else:
                    # パスワード変更するのでパスワード認証もする
                    token = ''
            except:
                # 認証できなかったトークンは消す
                token = None

        # パスワードで認証確認
        if not token and auth:
            try:
                API.login(**auth)
                # 変更後のパスワードで認証できたので更新しない
                if self.CONFIG.updatePassword is True:
                    self.CONFIG.updatePassword = False
                if token == '':
                    # 一度はトークン認証通しているのでそちらで認証しなおし（トークン優先）
                    token = self.CONFIG.token
                    API.login(token=token)
            except:
                if self.CONFIG.updatePassword is True:
                    pass
                else:
                    # 最終的に認証に失敗
                    return (False, 'Incorrect Credentials.')

        # パスワード更新の場合はトークン認証してない場合デフォルト認証を試行する
        if self.CONFIG.updatePassword is True and not token:
            if self.CONFIG.platformPassword:
                # ZabbixCloud対応: プラットフォームがAdminのデフォルトパスワード生成
                auth = {'user':ZABBIX_DEFAULT_AUTH['user'], 'password': self.CONFIG.platformPassword}
            else:
                auth = ZABBIX_DEFAULT_AUTH
            try:
                API.login(**auth)
            except:
                return (False, 'Cannot Autneticate for ChangePassword.')

        return (True, API)
    def initDbConnect(self):
        '''
        DB接続設定のイニシャライズ
        '''

        # DB設定のデフォルト、そろってなかった場合もデフォルト使用
        dbConnect = {
            'DBName': 'file',
            'DBHost': 'file',
            'DBPort': '-1',
            'DBPassword': 'password',
            'DBUser': 'user',
        }

        # ZabbixServer設定読み込み
        serverConf = os.path.join(ZABBIX_CONFIG_PATH, ZABBIX_SERVER_CONFIG)
        if os.path.exists(serverConf) and os.access(serverConf, os.R_OK):
            # Zabbix Server設定のデータベース設定を取得
            with open(serverConf, 'r') as f:
                serverConf = [
                    conf.strip().split('=') for conf in f.readlines() if conf.strip() != '' and conf[0] != '#'
                ]
            [
                dbConnect.update(
                    {conf[0]: conf[1]}
                ) for conf in serverConf if len(conf) == 2 and conf[0] in dbConnect.keys()
            ]
            # 成型{'dbConnect': {'name':'', 'host':'', 'port':'', 'user':'', 'password':'', 'library':''}}
            for conf, value in dbConnect.items():
                # db_xxxxxxx で全部小文字
                dbConnect.update(
                    {
                        conf[3:].lower() : value
                    }
                )

        # クラスに渡されたパラメーターからの適用
        if self.CONFIG.dbConnect:
            for conf, value in dbConnect.items():
                dbConnect.update(
                    {
                        conf: self.CONFIG.dbConnect.get(conf, value)
                    }
                )

        if not self.CONFIG.dbConnect:
            return (False, 'DB Connector: No Exist Configurations.')

        # ポート番号からライブラリの指定
        if self.CONFIG.dbConnect['port'] == 5432:
            self.CONFIG.dbConnect['library'] = 'psycopg'
        elif self.CONFIG.dbConnect['port'] == 3306:
            self.CONFIG.dbConnect['library'] = 'pymysql'
        else:
            self.CONFIG.dbConnect['library'] = 'sqlite3'
        # モジュール読み込み
        try:
            import importlib
            self.dbConnector = importlib.import_module(self.CONFIG.dbConnect.get('library'))
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed DB Connector Initialize.')

        return ZC_COMPLETE
    def operateDbDirect(self, operate=None, table=None, tableData=None):
        '''
        DB操作ファンクション
        '''

        # パラメータ確認
        if operate not in ['get', 'update', 'replace']:
            return (False, 'Operate not get or update.')
        if operate in ['replace', 'update']:
            if not tableData:
                return (False, f'No Exist Table Data: {table}')
            if not isinstance(tableData, list):
                return (False, f'Wrong Replace/Update Table Data Type, {type(tableData)}.')
        if not self.CONFIG.dbConnect:
            return (False, 'No Exist DB Connection Config.')
        dbConnect = self.CONFIG.dbConnect

        if dbConnect['library'] == 'psycopg':
            connection = self.dbConnector.connect(
                host=dbConnect['host'],
                port=dbConnect['port'],
                dbname=dbConnect['name'],
                user=dbConnect['user'],
                password=dbConnect['password']
            )
        elif dbConnect['library'] == 'pymysql':
            connection = self.dbConnector.connect(
                host=dbConnect['host'],
                port=dbConnect['port'],
                database=dbConnect['name'],
                user=dbConnect['user'],
                password=dbConnect['password']
            )
        else:
            return (False, 'Cannot set DB connection.')

        # 操作実行
        with connection:
            with connection.cursor() as cursor:
                if operate == 'get':
                    # 取得 
                    try:
                        cursor.execute(f'select * from {table}')
                        tableData = [[c[0] for c in cursor.description]]
                        [tableData.append(l) for l in cursor.fetchall()]
                        result = (True, tableData)
                    except Exception as e:
                        self.LOGGER.debug(e)
                        result = (False, f'Failed DB Direct Select {table}.')
                else:
                    # 自動コミットの停止
                    if dbConnect['library'] == 'psycopg':
                        connection.autocommit = False
                    elif dbConnect['library'] == 'pymysql':
                        connection.autocommit(False)
                    else:
                        pass
                    if operate == 'replace':
                        # 置き換え
                        try:
                            cursor.execute('DELETE FROM %s' % table)
                        except Exception as e:
                            self.LOGGER.debug(e)
                            result = (False, f'Failed DB Direct Delete All data on {table}.')
                        try:
                            # ヘッダー生成
                            head = ','.join(tableData[0])
                            # 1行ずつSQL生成＆実行
                            for row in tableData[1:]:
                                row = '\'' + '\',\''.join(map(str, row)) + '\''
                                sql = 'INSERT INTO %s (%s) VALUES (%s)' % (table, head, row)
                                cursor.execute(sql)
                            result = ZC_COMPLETE
                        except Exception as e:
                            self.LOGGER.debug(e)
                            result = (False, f'Failed DB Direct Insert into {table}.')
                    elif operate == 'update':
                        # 更新
                        if len(tableData) != 2:
                            result = ('False', 'Wrong Data for %s' % table)
                        elif len(tableData[0]) != len(tableData[1]):
                            result = (False, f'Wrong Head/Data length for {table}')
                        else:
                            try:
                                # 更新対象
                                where = '%s = \'%s\'' % (tableData[0][0], tableData[1][0])
                                update = ''
                                # 更新対象の生成
                                for col in range(1, len(tableData[1])):
                                    update += '%s = \'%s\', ' % (tableData[0][col], tableData[1][col])
                                # SQL実行
                                sql = 'UPDATE %s SET %s WHERE %s' % (table, update.strip(', '), where)
                                cursor.execute(sql)
                                result = ZC_COMPLETE
                            except Exception as e:
                                self.LOGGER.debug(e)
                                result = (False, f'Failed DB Direct Update {table}.')
                    else:
                        result = (False, 'No Operate.')
                    # 問題なければコミット
                    if result[0]:
                        connection.commit()
                    else:
                        connection.rollback()
                    if dbConnect['library'] == 'psycopg':
                        connection.close()
        return result
