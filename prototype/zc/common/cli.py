#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *

def inputParameters(mode='clone'):
    '''
    パラメーター入力処理
    優先順位:
      1. コマンド引数
      2. 環境変数
      3. 設定ファイル
    mode:
      clone: zc.py 用
      view: view.py 用
    '''
    params = {'store_connect': {}, 'db_connect': {}}
    for env, value in os.environ.items():
        env = env.upper()
        if not re.match(ZC_HEAD, env):
            continue
        env = env.replace(ZC_HEAD, '').lower()
        if re.match('^[a-z]*_connect_', env):
            env = env.split('_')
            params['_'.join(env[:2])].update(
                {
                    '_'.join(env[2:]): value
                }
            )
        else:
            params.update({env: value})

    if mode == 'view':
        commands = ['showversions', 'showdata']
        commandHelp = 'showversions: show versions in store, showdata: show version data'
        description = 'Zabbix Cloning: datastore view tool.'
    else:
        commands = ['clone']
        commandHelp = 'clone: Execute Cloning'
        description = '''\
        Zabbix Cloning: Zabbix monitoring settings cloning tool, from master-Zabbix to worker-Zabbix.
        If you use datastore, can manage settings by versions.'''

    parser = argparse.ArgumentParser(
        formatter_class=argparse.RawDescriptionHelpFormatter,
        description=textwrap.dedent(description)
    )
    parser.add_argument(
        'command',
        choices=commands,
        help=commandHelp
    )
    parser.add_argument(
        '-l', '--log.level',
        dest='log_level',
        choices=['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL'],
        help='ログレベル、デフォルトDEBUG'
    )
    parser.add_argument(
        '--log.file',
        dest='log_file',
        help='ログファイルの保存先'
    )
    parser.add_argument(
        '-v', '--version',
        help='version指定'
    )
    if mode == 'clone':
        parser.add_argument(
            '-q', '--quiet',
            action='store_true',
            help='処理進捗を表示しない'
        )
        parser.add_argument(
            '-y', '--yes',
            action='store_true',
            help='キー入力はYESで省略する'
        )
    else:
        parser.add_argument(
            '--method',
            nargs='+',
            help='指定のメソッドのみ表示する'
        )
        parser.add_argument(
            '--name',
            nargs='+',
            help='指定の名前のみ表示する'
        )
        parser.add_argument(
            '--id.only',
            dest='id_only',
            action='store_true',
            help='簡略表示をする'
        )

    configGroup = parser.add_argument_group('Configureation File Options')
    configGroup.add_argument(
        '-f', '--config.file',
        dest='config_file',
        help='設定ファイルの指定'
    )
    configGroup.add_argument(
        '--no.config.files',
        dest='no_config_files',
        action='store_true',
        help='設定ファイルを利用しない'
    )

    if mode == 'clone':
        baseGroup = parser.add_argument_group('Base Settings')
        baseGroup.add_argument(
            '-n', '--node',
            help='ノードの名称'
        )
        baseGroup.add_argument(
            '-r', '--role',
            choices=['master', 'worker', 'replica'],
            help='ノードの役割（master:データ取得元、worker:ZC_WORKERタグ = --nodeのホスト複製、replica:全データ複製）'
        )
        connectionGroup = parser.add_argument_group('Base Connection Settings')
        connectionGroup.add_argument(
            '-e', '--endpoint',
            help='ノードのZabbixエンドポイント'
        )
        connectionGroup.add_argument(
            '-u', '--user',
            help='複製実行ユーザー'
        )
        connectionGroup.add_argument(
            '-p', '--password',
            help='複製実行ユーザーのパスワード'
        )
        connectionGroup.add_argument(
            '-t', '--token',
            help='複製実行ユーザーのトークン'
        )
    else:
        connectionGroup = parser.add_argument_group('Direct Mode Connection Settings')

    connectionGroup.add_argument(
        '--self.cert',
        dest='self_cert',
        action='store_true',
        help='自己証明書を確認しない'
    )

    if mode == 'clone':
        processingGroup = parser.add_argument_group('Processing Options')
        processingGroup.add_argument(
            '--update.password',
            dest='update_password',
            action='store_true',
            help='複製実行ユーザーのパスワードを--passwordの指定に変更する'
        )
        processingGroup.add_argument(
            '--initialize',
            dest='initialize',
            action='store_true',
            help='ワーカーノードを初期化する'
        )
        processingGroup.add_argument(
            '--useip',
            dest='useip',
            action='store_true',
            help='ホストのエンドポイントをIP利用に変換する'
        )
        processingGroup.add_argument(
            '--host.update',
            dest='host_update',
            action='store_true',
            help='ホスト設定のアップデートを実行する'
        )
        processingGroup.add_argument(
            '--force.host.update',
            dest='force_host_update',
            action='store_true',
            help='ホストが別の設定で存在していた場合でも設定を上書きする'
        )
        processingGroup.add_argument(
            '--no.uuid',
            dest='no_uuid',
            action='store_true',
            help='ZC_UUIDによるホスト識別を利用しない'
        )
        processingGroup.add_argument(
            '--delete.host',
            dest='delete_host',
            action='store_true',
            help='マスターノードの設定に存在しないホストを削除する'
        )
        processingGroup.add_argument(
            '--delete.api',
            dest='delete_api',
            action='store_true',
            help='マスターノードの設定に存在しないAPI管理設定を削除する'
        )
        processingGroup.add_argument(
            '--skip.template',
            dest='skip_template',
            action='store_true',
            help='テンプレートのインポート/エクスポートをスキップする'
        )
        processingGroup.add_argument(
            '--skip.host',
            dest='skip_host',
            action='store_true',
            help='ホストのインポートをスキップする'
        )
        processingGroup.add_argument(
            '--template.separate.num',
            dest='template_separate_num',
            type=int,
            help='テンプレートのエクスポートを区切って処理する数（デフォルト: 100）'
        )
        processingGroup.add_argument(
            '--checknow.execute',
            dest='checknow_execute',
            action='store_true',
            help='ホスト追加後にLLDや指定監視間隔のアイテムの値取得を実行する'
        )
        processingGroup.add_argument(
            '--checknow.interval',
            dest='checknow_interval',
            nargs='+',
            help='アイテムの値取得を実行する対象の監視間隔'
        )
        processingGroup.add_argument(
            '--disable.monitoring',
            dest='disable_monitoring',
            action='store_true',
            help='ホストの監視を無効化する'
        )
        processingGroup.add_argument(
            '--php.worker.num',
            dest='php_worker_num',
            type=int,
            help='ホスト追加の並列実行を行う数（デフォルト: 4）'
        )

    storeGroup = parser.add_argument_group('Store Settings')
    storeGroup.add_argument(
        '-s', '--store.type',
        dest='store_type',
        choices=['file', 'redis', 'dydb', 'direct'],
        help='データストアの指定'
    )
    storeGroup.add_argument(
        '-se', '--store.endpoint',
        dest='store_endpoint',
        help='ストアのエンドポイント指定、dydb(aws region または URL), redis(IP/FQDN), direct(URL)'
    )
    storeGroup.add_argument(
        '-sp', '--store.port',
        dest='store_port',
        help='ストアのポート指定、redis(default: 6379)'
    )
    storeGroup.add_argument(
        '-sa', '--store.access',
        dest='store_access',
        help='ストアのアクセス情報、dydb(aws access id), direct(マスターノード名)'
    )
    storeGroup.add_argument(
        '-sc', '--store.credential',
        dest='store_credential',
        help='ストアの認証情報、dydb(aws secret key),redis(password), direct(マスターノードトークン)'
    )
    storeGroup.add_argument(
        '-sl', '--store.limit',
        dest='store_limit',
        type=int,
        help='ストアの処理分離数、dydb(default: 10)'
    )
    storeGroup.add_argument(
        '-sw', '--store.interval',
        dest='store_interval',
        type=int,
        help='ストアの処理分離時のインターバル秒数、dydb(default: 2)'
    )
    storeGroup.add_argument(
        '--file.store.path',
        dest='file_store_path',
        help='fileストアの保存先ディレクトリ'
    )

    databaseGroup = parser.add_argument_group('Database Connection Settings')
    databaseGroup.add_argument(
        '-dbhost', '--db.connect.host',
        dest='db_connect_host',
        help='Zabbix DBエンドポイント（Zabbix6.0未満対応）'
    )
    databaseGroup.add_argument(
        '-dbname', '--db.connect.name',
        dest='db_connect_name',
        help='Zabbix DB名（Zabbix6.0未満対応）'
    )
    databaseGroup.add_argument(
        '-dbtype', '--db.connect.type',
        dest='db_connect_type',
        choices=['pgsql', 'mysql'],
        help='Zabbix DB種別（Zabbix6.0未満対応）'
    )
    databaseGroup.add_argument(
        '-dbuser', '--db.connect.user',
        dest='db_connect_user',
        help='Zabbix DB接続ユーザー（Zabbix6.0未満対応）'
    )
    databaseGroup.add_argument(
        '-dbpswd', '--db.connect.password',
        dest='db_connect_password',
        help='Zabbix DB接続パスワード（Zabbix6.0未満対応）'
    )

    parser = parser.parse_args()
    for parse, value in parser.__dict__.items():
        if not value:
            continue
        if re.match('^[a-z]*_connect_', parse):
            parse = parse.split('_')
            connect = '_'.join(parse[:2])
            if not params.get(connect):
                params[connect] = {}
            params[connect].update({parse[-1]: value})
        else:
            params.update({parse: value})
    return params
