# ZC (Zabbix Cloning) Go版

Zabbixの監視設定を、設定元のZabbixから監視を実行するZabbixへ複製するツールです。
完全なバックアップではなく、複数Zabbix間で監視設定を展開・同期する用途を対象としています。
Zabbixの設定を植木鉢に見立て、鉢替えや株分けをするイメージのツールです。

Go版はZabbix 6.0から7.4までに対応します。Zabbix 6.0未満を扱う場合は
[Python旧版](prototype/readme_prototype.md)を使用してください。

## 主な機能

- masterから設定を取得し、バージョン付きでストアへ保存
- workerへ`ZC_WORKER`タグで割り当てたホストを適用
- replicaへ全ホストを適用
- ホスト名変更を`ZC_UUID`タグで追跡
- テンプレートを依存順にインポート
- ホストインターフェイスの差分更新
- file、Redis、DynamoDB、master直接接続に対応
- ストアデータを`view`コマンドで確認

masterで実行すると、必要に応じて次の情報をZabbixへ追加します。

- 全ホストの`ZC_UUID`タグ
- 適用バージョンを示すグローバルマクロ`{$ZC_VERSION}`

## 対応環境

- Go 1.23以降
- Zabbix 6.0、6.2、6.4、7.0、7.2、7.4
- WindowsまたはLinux

開発版は現在の対応範囲に含みません。

## ビルド

```sh
cd go-lang
go mod download
go build -o bin/zc ./cmd/zc
go build -o bin/view ./cmd/view
```

Windowsでは`bin/zc.exe`と`bin/view.exe`が生成されます。

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

Zabbix APIから取得できないパスワードやシークレットは、設定ファイルで補います。

## ロール

### master

設定の取得元です。Zabbixから設定を取得し、指定ストアへ新しいバージョンとして保存します。
ホストに`ZC_UUID`がなければ自動生成します。

### worker

監視実行先です。ホストタグ`ZC_WORKER`の値が`--node`と一致するホストだけを適用します。
適用対象ホストは監視有効にします。テンプレートはデフォルトでスキップされます。

### replica

masterの複製先です。`ZC_WORKER`タグに関係なく全ホストを適用します。
ホストの監視有効・無効状態はmasterの設定を維持します。通知メディアの追加設定はスキップします。

## ストア

### file

bzip2圧縮JSONをローカルへ保存します。

```text
{VERSION_ID}_{UNIXTIME}_{MASTER_VERSION}.bz2
```

保存先は`--file.store.path`または`ZC_FILE_STORE_PATH`で指定します。未指定時は次を使用します。

- Windows: `%USERPROFILE%\Documents\zc`
- Linux: `/var/lib/zabbix/zc`

### Redis

- DB 0: バージョン情報
- DB 1: bzip2圧縮した設定データ
- デフォルトポート: `6379`

### DynamoDB

- `ZC_VERSION`: `VERSION_ID` + `UNIXTIME`
- `ZC_DATA`: `VERSION_ID` + `DATA_ID`

`--store.endpoint`にはAWSリージョンまたはLocalStackなどのURLを指定できます。
URL指定時のリージョンは`AWS_DEFAULT_REGION`または設定ファイルの`aws_region`を使用します。

### direct

ストアへ保存せず、worker/replicaがmasterへ直接接続して設定を取得します。

## 基本的な実行例

以下は`go-lang`ディレクトリでビルドした場合の例です。

### masterからfileへ保存

```sh
bin/zc master --no.config.files --yes \
  --node master-zabbix \
  --endpoint https://master.example.com \
  --token TOKEN \
  --store.type file \
  --file.store.path ./store
```

### replicaへ適用

```sh
bin/zc replica --no.config.files --yes \
  --node replica-zabbix \
  --endpoint https://replica.example.com \
  --token TOKEN \
  --store.type file \
  --file.store.path ./store \
  --host.update
```

### Redis

```sh
bin/zc master --no.config.files --yes \
  --node monitor \
  --endpoint https://zabbix.example.com --token TOKEN \
  --store.type redis --store.endpoint localhost --store.port 6379
```

### DynamoDBまたはLocalStack

```sh
bin/zc master --no.config.files --yes \
  --node monitor \
  --endpoint https://zabbix.example.com --token TOKEN \
  --store.type dydb \
  --store.endpoint http://localhost:4566 \
  --store.access test \
  --store.credential test
```

## view

### バージョン一覧

```sh
bin/view showversions --no.config.files \
  --store.type file --file.store.path ./store
```

### 保存データ

```sh
bin/view showdata --no.config.files \
  --store.type file --file.store.path ./store \
  --version VERSION_ID \
  --method host template \
  --id.only
```

`--method`と`--name`は複数値を指定できます。

## zcオプション

`zc`の第1引数には実行ロールとして`master`、`worker`、`replica`のいずれかを必ず指定します。
実行ロールを指定するオプションはありません。

オプション名はドット区切りです。真偽値のオプションは、指定すると有効になります。

| オプション | 内容 |
|---|---|
| `-n`, `--node NAME` | 対象Zabbixのノード名 |
| `-e`, `--endpoint URL` | ZabbixフロントエンドURL |
| `-t`, `--token TOKEN` | APIトークン |
| `-u`, `--user USER` | APIユーザー |
| `-p`, `--password PASSWORD` | APIパスワード |
| `-v`, `--version UUID` | worker/replicaへ適用するストアバージョン |
| `--self.cert` | TLS証明書検証を無効化 |
| `-y`, `--yes` | 実行前確認を省略 |
| `-q`, `--quiet` | 進捗の標準出力を抑制 |
| `--dry.run` | create/update/delete系処理を実行せず、差分確認だけを行う |
| `--update.password` | 対象ユーザーのパスワードを更新 |
| `--initialize` | worker/replicaを初期化して適用 |
| `--initialize.full` | worker/replicaの削除可能な設定を全削除してから適用 |
| `--online` | 更新中の一時メンテナンス `__ZC_UPDATE__` を作成せず、対象をオンラインのまま適用 |
| `--useip` | DNS指定インターフェイスを名前解決してIP利用へ変更 |
| `--host.update` | 既存ホストを更新 |
| `--force.host.update` | ホスト名が異なっても同じ`ZC_UUID`のホストを更新。`--host.update`も有効化 |
| `--no.uuid` | 互換用に受理する予約オプション。現在のGo処理は`ZC_UUID`を使用 |
| `--delete.host` | ストアにないホストを削除 |
| `--delete.api` | ストアにないAPI管理設定を削除 |
| `--skip.template` | テンプレートのexport/importをスキップ |
| `--skip.host` | ホスト適用をスキップ |
| `--template.separate.num N` | masterのテンプレートexport分割数。デフォルト100 |
| `--checknow.execute` | ホスト適用後にLLDと対象アイテムを即時実行 |
| `--checknow.interval VALUE...` | CheckNow対象の監視間隔。デフォルト`1h` |
| `--disable.monitoring` | 適用ホストを監視無効状態にする |
| `--parallel.host.apply N` | ホストとホストインターフェイス適用の並列数。デフォルト4。同一ホスト内のインターフェイス処理は順序を維持 |

template、host、host interfaceの適用では、`--quiet`を指定しない場合は画面上の処理件数を
随時更新します。`--quiet`指定時は画面へ進捗を出さず、50件ごとと最後の端数の処理結果をログへ
出力します。失敗はどちらの場合も個別にログへ出力し、`--quiet`を指定しない場合は処理終了時に
失敗した対象だけを一覧表示します。

### dry-run

`--dry.run`を指定すると、Zabbixからの取得と差分計算は通常どおり行いますが、
create/update/delete系APIをZabbixへ送信しません。`configuration.import`、secretグローバルマクロ、
PSK、CheckNow、初期化の実API、バージョンマクロ更新も実行対象から除外されます。

masterではストアへ新しいバージョンを保存しません。CheckNowの適用待機時間も省略します。
終了時に、実行せず記録したAPIメソッド別件数をログへ表示します。

worker/replicaでdry-runを実行すると、最初のGET結果を基に仮想状態を作成します。`--initialize`で
削除される`correlation`、`drule`、`action`、`script`、`maintenance`は仮想状態から除外し、
後続のcreate/update/delete、configuration import、host、グローバル設定を仮想状態へ反映します。
作成予定のオブジェクトには仮IDを割り当てるため、後続オブジェクトの参照と差分判定も
initialize実行後の状態を前提に継続します。dry-run中のRefreshで実機状態へは戻りません。

CheckNowのアイテムはZabbixが設定適用後に生成するため、dry-runでは実機にすでに存在するホストだけを
照会します。仮想作成ホストは件数をログへ表示して照会対象から除外します。

### 完全初期化

`--initialize.full`はコマンドラインでのみ指定でき、設定ファイル、secret、環境変数からは有効に
できません。`--initialize`を含み、worker/replicaに存在する削除可能なAPI管理オブジェクトを
依存関係順に全削除してから、ストアの設定を適用します。`--yes`と`--quiet`は無効になり、実行前に
`y/N`の確認を2回行います。どちらかで拒否した場合は、削除を開始しません。

Zabbixの操作継続に必要なAdmin、API実行ユーザー、予約User Group（`usrgrpid=7,13`）とその所属ユーザー、
読み取り専用ロール、`{$ZC_VERSION}`は削除しません。削除APIのない一般設定、認証設定、自動登録設定は削除せず、後続処理でストアの値を
上書きします。認証設定はユーザーディレクトリとMFAを削除できるよう、削除前に内部認証へ切り替えます。

dry-runでは実機から削除せず、同じ削除後状態を仮想状態へ反映して後続の差分を計算します。

## ストアオプション

| オプション | 内容 |
|---|---|
| `-s`, `--store.type file\|redis\|dydb\|direct` | ストア種別 |
| `-se`, `--store.endpoint VALUE` | Redisホスト、DynamoDBリージョン/URL、directのmaster URL |
| `-sp`, `--store.port PORT` | Redisポート |
| `-sa`, `--store.access VALUE` | AWS access key、またはdirectのmasterノード名 |
| `-sc`, `--store.credential VALUE` | AWS secret key、Redisパスワード、directのmasterトークン |
| `-sl`, `--store.limit N` | DynamoDB書込の待機判定件数。デフォルト10 |
| `-sw`, `--store.interval N` | DynamoDB書込待機秒数。デフォルト2 |
| `--file.store.path PATH` | fileストア保存先 |

## 設定・ログオプション

| オプション | 内容 |
|---|---|
| `-f`, `--config.file FILE` | JSON設定ファイル |
| `--secret.file FILE` | 秘密情報用JSON設定ファイル |
| `--no.config.files` | 設定ファイルを読み込まない |
| `-l`, `--log.level DEBUG\|INFO\|WARNING\|ERROR\|CRITICAL` | ログレベル |
| `--log.file FILE` | ログファイル |

## viewオプション

`view`では共通のストア、設定ファイル、ログオプションに加えて次を使用できます。

| オプション | 内容 |
|---|---|
| `-v`, `--version UUID` | 表示対象バージョン |
| `--method VALUE...` | 表示するメソッドを限定 |
| `--name VALUE...` | 表示する名前を限定 |
| `--id.only` | IDと名前だけを表示 |

`direct`では永続バージョンがないため`showversions`は使用できません。

## 設定ファイルと環境変数

設定ファイルはJSONです。現在のGo実装の優先順位は次のとおりです。

1. コマンドライン
2. `ZC_`で始まる環境変数
3. `zc.secret`
4. 設定ファイル
5. デフォルト値

デフォルト設定ファイル:

- Windows: `%ProgramData%\Zabbix\zc.conf`
- Linux: `/etc/zabbix/zc.conf`

環境変数は設定キーを大文字にして`ZC_`を付けます。例: `ZC_STORE_TYPE`、`ZC_TOKEN`。
秘密情報ファイルの場所は、`--secret.file`、`ZC_SECRET_FILE`、`zc.conf`内の`secret_file`の順で指定します。
`zc.conf`内の相対パスは`zc.conf`と同じディレクトリを基準にします。指定がなければ、
`zc.conf`と同じディレクトリの`zc.secret`を読み込みます。`--no.config.files`指定時は、
`--secret.file`または`ZC_SECRET_FILE`を指定した場合だけ秘密情報ファイルを読み込みます。

```json
{
  "node": "monitor",
  "endpoint": "https://zabbix.example.com",
  "secret_file": "zc.secret",
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
  "store_type": "file",
  "file_store_path": "./store"
}
```

`zc.secret`には秘密情報だけを記述し、バージョン管理の対象外にしてください。

```json
{
  "token": "TOKEN",
  "password": "ZABBIX_PASSWORD",
  "platform_password": "CURRENT_PLATFORM_PASSWORD",
  "store_access": "AWS_ACCESS_KEY_ID",
  "store_credential": "AWS_SECRET_ACCESS_KEY",
  "store_connect": {
    "redis_password": "REDIS_PASSWORD",
    "direct_token": "MASTER_API_TOKEN"
  },
  "enable_user": {
    "username": "PASSWORD"
  },
  "mfa_client_secret": {
    "mfa-name": "CLIENT_SECRET"
  }
}
```

主な設定ファイル専用項目:

| キー | 内容 |
|---|---|
| `description` | masterが作るバージョンの説明 |
| `secret_file` | `zc.secret`のパス。相対パスは`zc.conf`基準 |
| `media_settings` | workerへ設定するユーザー通知メディア |
| `store_connect.aws_region` | DynamoDBリージョン |
| `store_connect.aws_endpoint_url` | DynamoDB互換エンドポイントURL |
| `store_connect.redis_host` | Redisホスト |
| `store_connect.redis_port` | Redisポート |
| `store_connect.direct_node` | directのmasterノード名 |
| `store_connect.direct_endpoint` | directのmaster URL |

### settings

Zabbix 7.0では、`settings`に
[settingsオブジェクト](https://www.zabbix.com/documentation/7.0/en/manual/api/reference/settings/object#settings)の
更新可能なプロパティをそのまま指定できます。指定値はmasterから取得した設定より優先されます。
読み取り専用の`ha_failover_delay`は指定できません。

`severity`と`timeout`は簡略記法も利用できます。

```json
{
  "settings": {
    "default_lang": "ja_JP",
    "login_attempts": 5,
    "severity": {
      "2": {"name": "Warning", "color": "FFC859"}
    },
    "timeout": {
      "zabbix_agent": "5s",
      "external_check": "15s",
      "browser": "60s"
    }
  }
}
```

`severity`のレベルは0から5、色は6桁の16進数です。`timeout`は`zabbix_agent`、
`simple_check`、`snmp_agent`、`external_check`、`db_monitor`、`http_agent`、
`ssh_agent`、`telnet_agent`、`script`、`browser`に対応します。

`zc.secret`に記載する主な秘密情報:

| キー | 内容 |
|---|---|
| `token`、`password`、`platform_password` | Zabbix接続・パスワード更新用の認証情報 |
| `store_connect.aws_account_id`、`store_connect.aws_secret_key` | DynamoDB用AWS資格情報 |
| `store_connect.redis_password` | Redisパスワード |
| `store_connect.direct_token` | direct接続先のAPIトークン |
| `enable_user` | 複製するユーザーのパスワード |
| `mfa_client_secret` | Duo MFAのclient secret |
| `secret_globalmacro` | secret型グローバルマクロの名前と値 |
| `psk` | proxy、host、自動登録へ適用するPSK identityとPSK |

`secret_globalmacro`が指定されている場合、`FirstProcess`で同名のグローバルマクロを
`type=1`として追加または上書きします。既存マクロのtypeが異なる場合もsecret型へ変更します。

`psk`は次の形式で指定します。

```json
{
  "psk": {
    "proxy": {
      "proxy-name": ["PSK_IDENTITY", "PSK"]
    },
    "host": {
      "host-name": ["PSK_IDENTITY", "PSK"]
    },
    "autoregistration": ["PSK_IDENTITY", "PSK"]
  }
}
```

proxyとhostは、指定名の対象が存在する場合だけ更新します。`tls_accept=1`なら`2`へ変更し、
`2`または`3`では現在値を維持したままPSKを設定します。`tls_accept`が`4`以上の場合は、
証明書設定を保護するため対象を更新しません。PSK以外のTLS設定変更には対応しません。
PSKはworker/replicaの全設定適用後に処理し、masterでは一切処理しません。

## ホスト更新

- 同じホスト名かつ同じ`ZC_UUID`の場合、`--host.update`で更新します。
- ホスト名が異なり`ZC_UUID`が一致する場合、`--force.host.update`で更新します。
- インターフェイスは設定が同じならAPIを実行しません。
- mainインターフェイス変更時は既存mainとの競合を避けて切り替えます。
- 新設定に存在しない既存インターフェイスだけを最後に削除します。
- アイテムとの関連により削除できない場合はfailedとして記録し、処理を継続します。

## 削除オプションの注意

`--delete.host`と`--delete.api`は、masterデータに存在しない対象を実際に削除します。
初回は削除オプションなしで差分を確認してください。`--initialize`または`--initialize.full`指定時は
削除オプションより初期化が優先されます。

## データ互換性

Go版と[Python旧版](prototype/readme.md)は、file、Redis、DynamoDBのデータ形式を共有します。
Python版で保存したバージョンをGo版で読み、その逆も可能です。

## 検証状況

Zabbix 7.0、7.2、7.4実機で次を確認しています。

- masterのfile、Redis、DynamoDB保存
- Go/Python双方からのストア相互読込
- テンプレートのimport
- ホストのupdate
- ホストインターフェイス差分判定
- action、maintenance、drule、correlation、role、usergroup、authentication
- `{$ZC_VERSION}`更新
- 7.0 masterから7.2/7.4へのreplica、worker適用
- 7.2 replicaをmasterとした7.2/7.4へのreplica、worker適用
- 7.4 replicaをmasterとした7.4へのreplica、worker適用

対応しているZabbixの設定は以下になります。

* hostgroup
* templategroup
* host
* template
* action (trigger/service/discovery/internal)
* script
* maintenance
* network discovery
* service
* sla
* event corellateion
* user
* user's media-type
* usergroup
* role
* general settings
* crypted globalmacro
* proxy
* proxy group
* authentication (Not tested)
* LDAP authentication setting (Not tested)
* SAML authentication setting (Not tested)
* MFA authentication setting (Not tested)


## 非対応

- Zabbix 6.0未満。必要な場合は[Python旧版](prototype/readme_prototype.md)を使用してください。
- Zabbix画面、ダッシュボード等の完全バックアップ
- APIから取得できないシークレットの自動復元
- 開発版およびZabbix 7.2以降

## ライセンス

[MIT License](LICENSE)
