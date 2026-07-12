y# ZC (Zabbix Cloning) Python旧版

このディレクトリには、Zabbix CloningのPython旧版を保存しています。
Python版はZabbix 4.0から7.0までに対応し、特にZabbix 6.0未満で必要なDB直接操作を含みます。

Python版は旧版として維持されます。明示的な変更指示がない限り、機能追加や仕様変更は行いません。

## 対応範囲

- Zabbix 4.0から7.0まで
- Python 3.6以降
- 開発版およびZabbix 7.2以降は非対応

バージョンごとの差異は`zc/common/parameters.py`で吸収しています。

## 必要ライブラリ

必須:

- `zabbix-utils`

ストアに応じて必要:

- Redis: `redis`
- DynamoDB: `boto3`

Zabbix 6.0未満のDB直接操作で必要:

- PostgreSQL: `psycopg` version 3
- MySQL/MariaDB: `pymysql`

## 実行方法

リポジトリルートから実行する場合:

```sh
python prototype/zc.py --help
python prototype/view.py --help
```

このディレクトリへ移動して実行する場合:

```sh
cd prototype
python zc.py --help
python view.py --help
```

## 対応する設定

- ホストグループ、テンプレートグループ
- ホスト、ホストインターフェイス
- テンプレート、アイテム、LLD、トリガー、値のマッピング
- メディアタイプ、グローバルマクロ、正規表現
- アクション、スクリプト、メンテナンス、ネットワークディスカバリ
- イベント相関、サービス、SLA
- プロキシ、プロキシグループ、コネクタ
- ユーザー、ユーザーグループ、ロール
- ユーザーディレクトリ、MFA、認証設定
- Zabbix一般設定、自動登録設定
- Zabbix 6.0未満の一般設定、正規表現等のDB直接取得・適用

設定項目は対象Zabbixバージョンに存在する場合だけ処理されます。

## ロール

### master

Zabbixから設定を取得し、指定ストアへバージョン付きで保存します。
ホストに`ZC_UUID`タグがなければ追加し、グローバルマクロ`{$ZC_VERSION}`を更新します。

### worker

`ZC_WORKER`タグの値が`--node`と一致するホストだけを適用します。
テンプレートimportはデフォルトでスキップします。

### replica

`ZC_WORKER`タグに関係なく全ホストを適用します。ユーザー通知メディアの追加設定はスキップします。

## ストア

- `file`: bzip2圧縮JSON
- `redis`: DB 0にVERSION、DB 1にDATA
- `dydb`: `ZC_VERSION`と`ZC_DATA`
- `direct`: masterへ直接接続

file、Redis、DynamoDBでは、保存済みのバージョンデータを再利用できます。

## 基本的な実行例

### masterからfileへ保存

```sh
python prototype/zc.py clone --no.config.files --yes \
  --role master \
  --node master-zabbix \
  --endpoint https://master.example.com \
  --token TOKEN \
  --store.type file \
  --file.store.path ./store
```

### replicaへ適用

```sh
python prototype/zc.py clone --no.config.files --yes \
  --role replica \
  --node replica-zabbix \
  --endpoint https://replica.example.com \
  --token TOKEN \
  --store.type file \
  --file.store.path ./store \
  --host.update
```

### ストアを表示

```sh
python prototype/view.py showversions --no.config.files \
  --store.type file --file.store.path ./store

python prototype/view.py showdata --no.config.files \
  --store.type file --file.store.path ./store \
  --version VERSION_ID --method host --id.only
```

## cloneオプション

現在のPython実装が受理するオプションです。長いオプション名はドット区切りです。

| オプション | 内容 |
|---|---|
| `-l`, `--log.level LEVEL` | `DEBUG`、`INFO`、`WARNING`、`ERROR`、`CRITICAL` |
| `--log.file FILE` | ログファイル |
| `-v`, `--version UUID` | worker/replicaへ適用するバージョン |
| `-q`, `--quiet` | 進捗表示を抑制 |
| `-y`, `--yes` | 実行前確認を省略 |
| `-f`, `--config.file FILE` | JSON設定ファイル |
| `--no.config.files` | 設定ファイルを使用しない |
| `-n`, `--node NAME` | ノード名 |
| `-r`, `--role master\|worker\|replica` | 実行ロール |
| `-e`, `--endpoint URL` | ZabbixフロントエンドURL |
| `-u`, `--user USER` | APIユーザー |
| `-p`, `--password PASSWORD` | APIパスワード |
| `-t`, `--token TOKEN` | APIトークンまたは旧版のセッションID |
| `--self.cert` | TLS証明書検証を無効化 |
| `--update.password` | 対象ユーザーのパスワードを更新 |
| `--initialize` | worker/replicaを初期化 |
| `--useip` | DNS指定インターフェイスをIP利用へ変換 |
| `--host.update` | 既存ホストを更新 |
| `--force.host.update` | 同じ`ZC_UUID`のホストをホスト名が違っても更新 |
| `--no.uuid` | 互換用に受理される予約オプション |
| `--delete.host` | masterデータにないホストを削除 |
| `--delete.api` | masterデータにないAPI管理設定を削除 |
| `--skip.template` | テンプレートexport/importをスキップ |
| `--skip.host` | ホスト適用をスキップ |
| `--template.separate.num N` | テンプレートexport分割数。デフォルト100 |
| `--checknow.execute` | ホスト適用後にLLDと対象アイテムを即時実行 |
| `--checknow.interval VALUE...` | CheckNow対象間隔。デフォルト`1h` |
| `--disable.monitoring` | 適用ホストを監視無効状態にする |
| `--php.worker.num N` | ホストcreate/updateの並列数。デフォルト4 |

## ストアオプション

| オプション | 内容 |
|---|---|
| `-s`, `--store.type file\|redis\|dydb\|direct` | ストア種別 |
| `-se`, `--store.endpoint VALUE` | Redisホスト、DynamoDBリージョン/URL、direct URL |
| `-sp`, `--store.port PORT` | Redisポート |
| `-sa`, `--store.access VALUE` | AWS access keyまたはdirectノード名 |
| `-sc`, `--store.credential VALUE` | AWS secret key、Redisパスワード、directトークン |
| `-sl`, `--store.limit N` | DynamoDB処理分離数。デフォルト10 |
| `-sw`, `--store.interval N` | DynamoDB処理待機秒数。デフォルト2 |
| `--file.store.path PATH` | fileストア保存先 |

## Zabbix 6.0未満用DBオプション

Zabbix 6.0未満では一般設定等のAPIがないため、DBへ直接接続します。

| オプション | 内容 |
|---|---|
| `-dbhost`, `--db.connect.host HOST` | DBホスト |
| `-dbname`, `--db.connect.name NAME` | DB名 |
| `-dbtype`, `--db.connect.type pgsql\|mysql` | DB種別 |
| `-dbuser`, `--db.connect.user USER` | DBユーザー |
| `-dbpswd`, `--db.connect.password PASSWORD` | DBパスワード |

## viewオプション

`view.py`は共通のログ、設定、ストア、DBオプションに加えて次を受理します。

| オプション | 内容 |
|---|---|
| `-v`, `--version UUID` | 表示対象バージョン |
| `--method VALUE...` | 表示メソッドを限定 |
| `--name VALUE...` | 表示名を限定 |
| `--id.only` | IDと名前だけを表示 |
| `--self.cert` | direct接続時のTLS証明書検証を無効化 |

## 設定ファイルと環境変数

優先順位:

1. コマンドライン
2. `ZC_`で始まる環境変数
3. 設定ファイル
4. デフォルト値

環境変数は設定ファイルの同名キーを上書きします。

デフォルト設定ファイル:

- `/etc/zabbix/zc.conf`
- `/var/lib/zabbix/conf.d/zc.conf`

主な設定キー:

```json
{
  "node": "monitor",
  "role": "replica",
  "endpoint": "https://zabbix.example.com",
  "token": "TOKEN",
  "self_cert": false,
  "update_password": false,
  "initialize": false,
  "useip": false,
  "host_update": true,
  "force_host_update": false,
  "delete_host": false,
  "delete_api": false,
  "skip_template": false,
  "skip_host": false,
  "template_separate_num": 100,
  "checknow_execute": false,
  "checknow_interval": ["1h"],
  "checknow_wait": 30,
  "disable_monitoring": false,
  "php_worker_num": 4,
  "store_type": "file",
  "file_store_path": "./store"
}
```

設定ファイル専用の実装済み項目:

| キー | 内容 |
|---|---|
| `description` | masterが作るバージョンの説明 |
| `platform_password` | Zabbix Cloudでパスワード更新時に使う現在パスワード |
| `secret_globalmacro` | secret型グローバルマクロ |
| `enable_user` | 複製を許可するユーザーと新規作成時パスワード |
| `cloning_super_admin` | 特権管理者ロールのユーザー複製を許可 |
| `proxy_psk` | プロキシ名ごとのPSK identityとPSK |
| `settings` | 重要度名・色、Zabbix 7.0のtimeout上書き |
| `media_settings` | workerへ設定する通知メディア |
| `mfa_client_secret` | Duo MFAのclient secret |
| `store_connect` | Redis、DynamoDB、directの接続情報 |
| `db_connect` | Zabbix 6.0未満のDB接続情報 |

## バージョン対応

- Zabbix 4.0を基準にAPIパラメータを定義
- 4.4: 自動登録、メディアタイプexport
- 5.2: ロール
- 5.4: ユーザー名変更、値のマッピング形式変更
- 6.0: settings、authentication、regexp、service、SLA
- 6.2: テンプレートグループ、ユーザーディレクトリ
- 6.4: APIパラメータ差分
- 7.0: プロキシグループ、MFA、コネクタ、個別timeout

Python旧版の対応上限はZabbix 7.0です。

## 注意事項

- `--delete.host`と`--delete.api`は実際に対象を削除します。
- APIから取得できないパスワード、PSK、secretは設定ファイルが必要です。
- Zabbix 6.0未満のDB直接操作にはDBへの接続権限が必要です。
- 本ツールは完全バックアップではありません。

## ライセンス

[MIT License](../LICENSE)
