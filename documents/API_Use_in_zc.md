# Zabbix Cloning が使用する Zabbix API

対象は現行のGo版（`go-lang/`）の実行コード。Zabbix 7.0、7.2、7.4、8.0における使い分けも記載する。Python旧版、互換性テスト、データストアのAPIは対象外。

## 接続・認証

| API | 呼び出す場面 |
| --- | --- |
| `apiinfo.version` | 認証前に接続先のAPIバージョンを取得し、認証方式と取得項目を決定する。 |
| `user.get` | 設定済みAPIトークンを検証する。検証時は `output: ["userid"]` と `limit: 1` を指定する。設定同期でも別の取得条件で使用する。 |
| `user.login` | パスワード認証でトークンを取得する。APIトークンが未設定、または検証に失敗してパスワードが設定されている場合に使う。`username` で失敗した場合は `user` で再試行する。 |

呼び出し先は `api_jsonrpc.php`。7.0ではJSON-RPC本文の `auth`、7.2以降ではHTTPの `Authorization: Bearer <token>` を使用する。サーバー名を確認する場合は、別途フロントエンドの `index.php?form=default` から表示名を取得する。[クライアント実装](../go-lang/internal/zabbix/client.go)

## 設定の取得と適用

起動時と適用途中の再取得では、下表にある各リソースの `.get` を呼び出す。取得したIDと名前の対応を使い、ストアに保存する参照を名前に変換し、適用時に複製先のIDへ引き直す。通常の設定同期では、適用先に同名の設定があれば `.update`、なければ `.create` を選ぶ。表の `PRE`、`MID`、`POST`、`ACCOUNT` はコード上の適用段階。

| 段階・用途 | 取得するリソース（各 `.get`） | 通常適用時のAPI |
| --- | --- | --- |
| 全体設定 | `autoregistration`、`settings`、`authentication` | `autoregistration.update`、`settings.update`、`authentication.update`。認証設定はアカウント関連の適用後に反映する。APMデータソースは終盤の専用処理で反映する。 |
| `PRE`（構成のインポート前） | `usermacro`、`regexp`、`connector`、`proxygroup` | グローバルマクロは `usermacro.createglobal` / `usermacro.updateglobal`。残りは各 `.create` / `.update`。 |
| 構成の入出力 | `hostgroup`、`templategroup`、`template`、`mediatype`、8.0以降の `dashboard` | `configuration.export` / `configuration.import`。ダッシュボードは他のリソース適用後にインポートする。 |
| `MID`（ホスト適用前） | `script`、`proxy` | 各 `.create` / `.update`。プロキシの `apm` は本体適用から分離して終盤に反映する。 |
| ホスト | `host` | `host.create` / `host.update`。インターフェイスの取得は `hostinterface.get`。 |
| `POST`（ホスト適用後） | `action`、`maintenance`、`drule`、`correlation`、8.0以降の `ceprule`、`role`、`service`、`sla`、`userdirectory`、`mfa` | 各 `.create` / `.update`。`drule` はネットワークディスカバリ用。SAMLディレクトリの適用は `ACCOUNT` の後に行う。 |
| `ACCOUNT` | `usergroup`、`user` | 各 `.create` / `.update`。 |
| 値のマッピング | `valuemap` | テンプレートの値マッピングは `configuration.import` のルールで扱う。`valuemap.delete` は全初期化時に使用する。 |

[取得対象・パラメーター定義](../go-lang/internal/clone/parameters.go) · [取得と参照変換](../go-lang/internal/clone/engine.go) · [実行順序](../go-lang/internal/clone/run.go) · [設定の適用](../go-lang/internal/clone/replica.go)

### 構成の入出力とホストの適用

- `configuration.export` はJSON形式でホストグループ、テンプレートグループ、ホスト、メディアタイプを取得する。テンプレートは別バッチで取得する。トリガーはエクスポート結果に含まれるデータを取り込む。取得元のLLD生成ホストは複製対象から除外する。[エクスポート実装](../go-lang/internal/clone/master.go)
- `configuration.import` はグループやメディアタイプの基本構成を適用し、その後テンプレートを依存順に適用する。テンプレート配下のアイテム・LLDルール・トリガーもインポートルールで扱う。複製先に同一UUIDで名前が異なるテンプレートがあれば `template.delete` を呼ぶ。[インポート実装](../go-lang/internal/clone/configuration.go)
- ホスト本体は `host.create` / `host.update` を使う。取得元のホストに識別タグ `ZC_UUID` がなければ、マスター処理中に `host.update` で付与する。既存ホストのインターフェイスは `hostinterface.get` で詳細を取得し、差分がある場合に `host.update` の `interfaces` でまとめて更新する。既存アイテムからの参照を保つため、対応する `interfaceid` を引き継ぐ。[ホスト処理](../go-lang/internal/clone/host.go) · [インターフェイス同期](../go-lang/internal/clone/interface.go)

### 8.0のダッシュボード入出力（2026-11-11）

7.0→7.2、7.2→7.4、7.4→8.0の公式API変更履歴と、`parameters.go`、`master.go`、`configuration.go`、`run.go` の呼び出しを照合した。8.0で追加されたダッシュボード入出力は独立した転送APIではなく、既存の `configuration` APIで扱えるため同期対象に追加した。

- **取得** — 8.0以降で `dashboard.get` の `output: ["dashboardid", "name"]` により対象一覧を取得し、そのIDを `configuration.export` の `options.dashboards` に渡す。内部IDは取得元での対象指定にのみ使い、ストアには公式エクスポートの `dashboards`（名前・ページ・ウィジェットと参照）を保存する。ホスト参照はホスト名、アイテム参照はホスト名とキーなど、エクスポート形式のまま保持する。空の対象は `[]` を送る。
- **適用** — `configuration.import` の `rules.dashboards` に `createMissing: true`、`updateExisting: true` を指定する。ホスト・テンプレート・サービス・SLA・ユーザーなどの参照先を適用した後、最後にダッシュボードだけをインポートする。更新時はページとウィジェットを全置換する。`dashboard.create/update` で取得元のIDを直接適用する処理は追加していない。
- **バージョンの使い分け** — 取得対象・エクスポートオプション・インポートルールへの追加は8.0以降のみ。ダッシュボードの適用には取得元と適用先の両方が8.0以降であることが必要。7.0 / 7.2 / 7.4の取得や、7.xの既存ストアから8.0への適用ではダッシュボードをインポートしない。従来のテンプレート内ダッシュボード（`templateDashboards`）の無効ルールは変更していない。
- **範囲と制約** — APIで閲覧可能なダッシュボードが対象。所有者・共有権限を独立APIで複製する処理は追加せず、公式エクスポートが含める項目を扱う。マップなどZCが同期しない参照先は複製先に別途用意する。参照先が存在しない場合の扱いは公式インポート仕様に従う。`--delete.api` による差分削除は追加していない。`--initialize.full` は取得元データの `MasterVersion` が8.0以降の場合のみ `dashboard.delete` の対象とし、他リソースより先に削除する。7.xのデータ、またはバージョン情報が未設定の場合は、通常実行・dry-runとも既存ダッシュボードを削除しない。8.0以降のデータはダッシュボードが空でも削除対象とする。
- **検証** — HTTPモックで対象IDの指定、空配列、参照情報を含むストアへの保存とインポートの往復、基本構成適用からの分離、dry-run仮想状態への反映を確認した。7.0 / 7.2 / 7.4 / 8.0のバージョン分岐と旧ストアのスキップも回帰テストで確認した。`./tests/test-go.ps1 -count=1` は全パッケージ成功。今回の実機Zabbixでの入出力・画面表示確認は未実施。[回帰テスト](../tests/go/internal/clone/dashboard_test.go)

公式資料: [7.2 API変更履歴](https://www.zabbix.com/documentation/7.2/en/manual/api/changes) · [7.4 API変更履歴](https://www.zabbix.com/documentation/7.4/en/manual/api/changes) · [8.0 API変更履歴](https://www.zabbix.com/documentation/8.0/en/manual/api/changes) · [configuration.export](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/configuration/export) · [configuration.import](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/configuration/import) · [dashboard.get](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/dashboard/get) · [ダッシュボードのエクスポート形式・インポート時の参照](https://www.zabbix.com/documentation/8.0/en/manual/config/xml_export_import/dashboards)

### 8.0のCEPルール（2026-11-20）

7.0→7.2、7.2→7.4、7.4→8.0の公式API変更履歴を確認した。CEP APIは8.0で追加され、現行の取得定義、保存処理、汎用API適用、初期化処理に呼び出しがなかったため追加した。

- 8.0以降で `ceprule.get` に `output: "extend"`、`selectFilter`、`selectWindow`、`selectOperations` を指定し、条件・ウィンドウ・操作を取得する。ルールIDは `cep_ruleid`、照合キーは名前。ホスト可視名・グループ名・タグなどを条件に使い、他リソースの内部IDを参照しないため名前と条件値を維持する。カスタム式の `formulaid` は式内のローカル記号として保持する。
- `POST`の `correlation` 直後に、同名ルールがあれば適用先のIDで `ceprule.update`、なければ `ceprule.create` を呼ぶ。完全初期化の削除順序も `correlation` 直後に配置する。条件・ウィンドウ・操作・実行順序・有効状態を同期する。作成・更新・削除にはSuper admin権限が必要。
- 保存時と適用前に、取得元の `cep_ruleid`、読み取り専用の `error`、各階層のフィルターの `eval_formula` を除外する。カスタム式以外の `formula` と条件の `formulaid` も除く。操作の `filter` と `scope_filter` の両方に適用する。ウィンドウ内のイベントや実行状態は保存せず、`ceprule.resettimewindows` は呼ばない。
- 7.0 / 7.2 / 7.4には取得定義・適用段階を追加しない。8.0に7.x由来またはバージョン未設定のストアを適用する場合はCEP処理をスキップし、`--delete.api`でも既存ルールを削除しない。`--initialize.full`の `ceprule.delete` も取得元データが8.0以降の場合のみ実行する。通常の `--initialize` の削除対象は変更しない。8.0データでは空のCEP一覧も削除オプションに従う。
- HTTPモックで取得から保存・作成・更新・差分削除、カスタム式とウィンドウ設定の保持、7.x/8.0の分岐を確認した。完全初期化での旧データ保護は通常実行・dry-run双方の回帰テストで確認した。`./tests/test-go.ps1 -count=1` は全パッケージ成功。2026-11-20にt2devの8.0.0で5種のウィンドウ設定の同期とNone型による実イベント名変更を確認した。[実機記録](cep-apm-live-test.md) · [CEPテスト](../tests/go/internal/clone/cep_test.go) · [初期化テスト](../tests/go/internal/clone/initialize_test.go)

公式資料: [7.2 API変更](https://www.zabbix.com/documentation/7.2/en/manual/api/changes) · [7.4 API変更](https://www.zabbix.com/documentation/7.4/en/manual/api/changes) · [8.0 API変更](https://www.zabbix.com/documentation/8.0/en/manual/api/changes) · [CEPオブジェクト](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/ceprule/object) · [取得](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/ceprule/get) · [作成](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/ceprule/create) · [更新](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/ceprule/update) · [削除](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/ceprule/delete) · [入力検証の公式実装](https://github.com/zabbix/zabbix/blob/release/8.0/ui/include/classes/api/services/CCepRule.php)

### 8.0のAPMと管理のデータソース（2026-11-20）

7.0→7.2、7.2→7.4、7.4→8.0の公式API変更履歴と実呼び出しを確認した。APMの設定は独立した作成APIではなく、既存の `settings`、`proxy`、`usergroup` に分かれる。「管理 → データソース → APM」は同じ機能のグローバル接続設定で、`settings.apm_global_db` に対応する。

| 設定・用途 | APIとZCの処理 |
| --- | --- |
| グローバルデータソース | `settings.get` の `apm_global_db` を保存。`settings.update` で有効状態、URL、認証方式、ユーザー名、DB名、TLS検証設定を適用する。接続パスワードは保存対象外。 |
| プロキシのAPM収集 | `proxy.get` の `selectApm: "extend"` で `apm` を取得。`proxy.create/update` では本体のみ先に適用し、終盤に適用先の `proxyid` を使って `proxy.update` の `apm` を送る。収集状態、メッセージ上限、追加リソース属性を同期する。無効時は上限・属性を送らない。 |
| APMアクセス権限 | `usergroup.get` の `selectProxies`、`selectProxyGroups`、`selectApmFilters` を使用する既存経路を維持する。プロキシ・グループの参照は名前で保存し、適用先IDに解決する。 |
| テレメトリの実データ | `apmtrace.get`、`apmspan.get`、`apmmetric.get`、`apmlog.get` はトレース等の照会API。設定同期に必要ないため呼び出さず、ClickHouse内の実データは複製しない。 |

**処理順序**: 一般Settings → プロキシ本体 → ホスト・POST・ACCOUNTなど → Settings参照の遅延適用・認証・PSK → APMデータソース → プロキシAPM収集設定 → ダッシュボード → バージョン記録。`prepareSettingsUpdate` がAPM項目を分離し、`ApplyAPMSettings` が終盤に適用する。一般Settings全体の順序は変更しない。データソースの適用に失敗したら、後続のAPM収集設定は適用しない。[APM実装](../go-lang/internal/clone/apm.go) · [実行順序](../go-lang/internal/clone/run.go)

**シークレット**: 接続パスワードはPSKと同様に複製先の `zc.secret` の `settings.apm_global_db.password` で供給する。APIが返した値は保存せず、旧ストアの値も適用前に除去する。適用先APIから取得したパスワードも補完に使わない。未指定ならリクエストに含めず、空文字の明示指定は空のパスワードとして送る。パスワード認証で有効化・認証方式の切替・URL変更を行う場合、公式実装は明示的なパスワードを要求する。APMデータソースの上書きは項目単位でマージし、秘密情報だけの指定でURL等を失わない。設定ファイルやストアのオブジェクトは上書き処理で変更しない。

**条件と互換性**: 8.0以降のみ。無効なデータソースは `status: 0` のみを送る。認証なしの場合はユーザー名・パスワード、HTTPの場合はTLS設定、証明書検証無効時はホスト名検証設定を送らない。7.xにはAPM更新を送らず、旧ストアでAPM項目が欠けている場合も、複製先の明示的な設定がない限り該当項目は更新しない。server/proxy/frontendのファイルにある個別接続設定やOpenTelemetry/ClickHouseの構築・データ移行は対象外。

**検証**: HTTPモックで一般Settings・プロキシ本体へのAPM混入を防ぐこと、データソース→収集設定の順序、適用先ID、部分上書き、API・旧ストアのパスワード除外、明示的な空のパスワード、無効化・認証・TLSの条件、dry-run、旧バージョンを検証した。既存の権限ID変換テストを含め、`./tests/test-go.ps1 -count=1` は全パッケージ成功。2026-11-20にt2devで今回の順序での設定同期・接続を確認した。VM設定変更後はClickHouse 26.9.33.10に対するAPM APIとserverのTelemetry queryでテストデータを照会できた。OTLP経由の収集の通し試験は未再実施。[実機記録](cep-apm-live-test.md) · [APMテスト](../tests/go/internal/clone/apm_test.go)

公式資料: [8.0 API変更](https://www.zabbix.com/documentation/8.0/en/manual/api/changes) · [7.4 API変更](https://www.zabbix.com/documentation/7.4/en/manual/api/changes) · [7.2 API変更](https://www.zabbix.com/documentation/7.2/en/manual/api/changes) · [管理のデータソース](https://www.zabbix.com/documentation/8.0/en/manual/web_interface/frontend_sections/administration/data_source) · [Settings仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/settings/object) · [Settings更新](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/settings/update) · [パスワード要件の公式実装](https://github.com/zabbix/zabbix/blob/release/8.0/ui/include/classes/api/services/CSettings.php) · [プロキシ仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/proxy/object) · [ユーザーグループ取得](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/usergroup/get) · [トレース取得](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/apmtrace/get) · [スパン取得](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/apmspan/get) · [メトリクス取得](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/apmmetric/get) · [ログ取得](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/apmlog/get)

## 運用オプションと補助処理

| 処理 | 使用するAPIと条件 |
| --- | --- |
| 一時メンテナンス | `--online` を指定していない場合、既存の `__ZC_UPDATE__` を `maintenance.delete` で削除し、`maintenance.create` で600秒のメンテナンスを作る。 |
| 即時チェック | `--checknow.execute` で `discoveryrule.get` と `item.get` を呼び、LLDルールと指定間隔のアイテムを抽出する。依存アイテムはマスターアイテムに置き換え、アクティブエージェントのアイテムを除外し、`task.create`（`type=6`）を送る。 |
| ログインパスワード・通知先 | `user.update` でログインユーザーのパスワードを変更する設定に対応する。通知先の設定がある場合、workerで `user.update` の `medias` を適用する。 |
| 秘密マクロ | `usermacro.get` で既存マクロを照会。設定済みの秘密マクロはグローバルなら `usermacro.createglobal` / `usermacro.updateglobal`、ホスト単位なら `usermacro.create` / `usermacro.update` を使用する。 |
| 複製バージョン記録 | `usermacro.createglobal` / `usermacro.updateglobal` で `{$ZC_VERSION}` を書き込む。 |
| TLS PSK | 対象が設定されている場合に `host.get/update`、`proxy.get/update`、`autoregistration.get/update` でPSK設定を照会・適用する。 |
| 差分削除 | `--delete.host` で対象外となったホストを `host.delete` で削除する。`--delete.api` では同期対象にない設定を各リソースの `.delete` で削除する。現行の共通処理では `usermacro` も `usermacro.delete` を組み立てる。 |
| 初期化 | `--initialize` は `correlation`、`drule`、`action`、`script`、`maintenance` の各 `.delete` を使用する。`--initialize.full` では `authentication.update` の後、設定一覧のうちIDを持つリソースを再取得して削除する。グローバルマクロの削除には `usermacro.deleteglobal` を使用する。 |

`--dry.run` では変更系の呼び出しをクライアント内で記録し、Zabbixへの送信を省く。バージョン確認、認証、対象データの取得は実行する。[クライアント実装](../go-lang/internal/zabbix/client.go) · [補助処理](../go-lang/internal/clone/operations.go) · [秘密マクロ](../go-lang/internal/clone/secret_globalmacro.go) · [PSK](../go-lang/internal/clone/psk.go) · [初期化](../go-lang/internal/clone/initialize.go)

## 7.x / 8.0 の互換処理

| 対象 | ZCでの扱い |
| --- | --- |
| 7.2以降の認証 | リクエスト本文の `auth` からBearerヘッダーへ切り替える。 |
| 7.4の `user.get` | `output` に `userid` を明示する。各リソースでも必要なIDを取得項目に追加する。 |
| 8.0のホストインターフェイス | 7.0〜8.0で共通して、取得には `hostinterface.get`、変更には `host.update` を使用する。 |
| 8.0の全体設定と正規表現 | `settings.get` で返る読み取り専用の `serverid` を適用データから除外する。`regexp.get` の `output` に `description` を追加するのは8.0以降。 |
| 8.0のプロキシグループ条件 | アクションの `conditiontype=29` で参照するIDを、取得元のIDから名前へ、名前から適用先のIDへ変換する。 |
| 7.xから8.0へのロール移行 | APIアクセスが明示的に有効で、空の許可リストを持つロールは空の拒否リストに変換し、従来のアクセス範囲を維持する。 |

[Zabbix APIの仕様変更](API_Changes.md) · [ZCの8.0互換性確認記録](zabbix80-compatibility.md)

## 8.0までの変更点と既存テストの照合（2026-11-08）

現行Go版の呼び出し、取得フィールド、保存・適用時の変換を公式API変更履歴と照合した。実機の確認範囲は、既存の検証スクリプトが投入するデータと、適用先で値を照合する項目から判断した。[7.0のAPI変更](https://www.zabbix.com/documentation/7.0/en/manual/api/changes) · [7.0.xのAPI変更](https://www.zabbix.com/documentation/7.0/en/manual/api/changes_7.0) · [7.2のAPI変更](https://www.zabbix.com/documentation/7.2/en/manual/api/changes) · [7.2.xのAPI変更](https://www.zabbix.com/documentation/7.2/en/manual/api/changes_7.2) · [7.4のAPI変更](https://www.zabbix.com/documentation/7.4/en/manual/api/changes) · [7.4.xのAPI変更](https://www.zabbix.com/documentation/7.4/en/manual/api/changes_minor) · [8.0のAPI変更](https://www.zabbix.com/documentation/8.0/en/manual/api/changes)

### 8.0対応で追加した同期・上書き

- **APMとユーザーグループ権限** — 8.0以降の取得で `proxy.get` に `selectApm`、`usergroup.get` に `selectProxies`、`selectProxyGroups`、`selectApmFilters` を指定する。プロキシとグループの割り当ては名前で保存し、適用先IDに変換する。割り当て先が存在しないときはエラーを返す。旧ストアに割り当て項目がない場合は、複製先の割り当てをその項目で上書きしない。[プロキシ取得仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/proxy/get) · [ユーザーグループ取得仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/usergroup/get)
- **SAML** — 名前のないディレクトリもSAMLとして識別し、ユーザーグループ作成後に `userdirectory.create/update`、続いて `authentication.update` を呼ぶ。証明書・秘密鍵と読み取り専用ハッシュはストアから除外する。8.0でフロントエンドの `$SSO['CERT_STORAGE']` を `database` に設定した場合、複製先の `zc.secret` の `saml_certificates` に `idp_certificate`、`sp_private_key`、`sp_certificate` をPEM文字列で指定できる。未指定の証明書・秘密鍵は更新リクエストに含めない。ファイル保存方式では、必要な証明書と鍵を複製先のフロントエンドに配置する。[ユーザーディレクトリ仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/userdirectory/object) · [SAML適用処理](../go-lang/internal/clone/saml.go)
- **全体設定** — 8.0以降で `settings.timeout_telemetry_query`（短縮形は `settings.timeout.telemetry_query`）、`settings.device_link_timeout`、`settings.apm_global_db`、`settings.default_theme` の `dark-blue-theme` を指定できる。APMデータソースの上書きは項目単位（2026-11-20に変更）。接続パスワードは `zc.secret` の `settings.apm_global_db.password` で供給し、取得元からのパスワードはストアに保存しない。[全体設定仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/settings/object) · [上書き処理](../go-lang/internal/clone/settings.go)

SAML JITでプロビジョニングを解除されたユーザーの所属グループ（`disabled_usrgrpid`）は、名前で保存し適用先のIDに解決する。旧ストアでIDを保存している場合は従来どおりその値を使用するため、ノード間でIDが異なるときはマスターを再取得する。[認証設定仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/authentication/object)

Zabbix 8.0.0を2台用意し、fileストアを使ったマスター取得とレプリカ・ワーカーへの適用で、APM設定、権限のID変換、SAML証明書とJIT設定、全体設定の上書きをAPIで照合した。JITの無効ユーザーグループはノード間で異なるIDを使って確認した。空の権限リストとAPM・SAMLの無効化も反映された。ストアの展開結果にSAML証明書・秘密鍵・ハッシュとAPMデータソースのパスワードが含まれないことを確認した。外部IdPでのログインとAPMデータの取り込み・接続は実機試験の範囲外。

### 仕様上の対象外と残る項目

メンテナンスのトリガー指定は、トリガーIDがノードごとに異なるため複製対象外。イベント名による条件も現在の同期項目には含まれない。[メンテナンス取得仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/maintenance/get)

| バージョン・対象 | コードで確認した内容 | 根拠 |
| --- | --- | --- |
| 8.0 ユーザーの既定メンテナンス時間 | `user.get` の明示的な `output` に `default_maintenance_period` がなく、ユーザー同期を有効にしても元の値は複製されない。 | [ユーザーの項目仕様](https://www.zabbix.com/documentation/8.0/en/manual/api/reference/user/object) · [取得定義](../go-lang/internal/clone/parameters.go) |

### 実装経路はあるが、該当データでの実機確認が残るもの

- **7.0のネットワークディスカバリとアクション** — `drule` の取得・適用とHTTPチェックの `allow_redirect` を残す経路がある。ホストタグを追加・削除するアクション操作（`optag`）も専用の削除処理はない。既存の実機データには、これらを持つルールやアクションがない。[7.0のAPI変更](https://www.zabbix.com/documentation/7.0/en/manual/api/changes) · [適用処理](../go-lang/internal/clone/replica.go)
- **7.0のMFA・ユーザーディレクトリと後続パッチの権限・署名変更** — リソースの作成・更新やDuoシークレットの供給経路はある。既存試験のログインは初期Adminで、新規ユーザー作成、LDAP/JITマッピング、MFA、SAML署名ありの認証や非Super adminでの権限変更は照合していない。アクション参照中の削除を拒否するパッチ変更も、複製先での削除オプションを使うケースがない。[7.0.xのAPI変更](https://www.zabbix.com/documentation/7.0/en/manual/api/changes_7.0) · [適用処理](../go-lang/internal/clone/replica.go) · [既存の実機試験](../tests/compatibility/matrix.py)
- **7.4のOAuth SMTP** — メディアタイプの汎用エクスポート・インポートを通る。OAuthの再認可や通知送信を確認する試験はない。公式仕様ではクライアントシークレットが平文でエクスポートされ、現行コードのメディアタイプ保存処理にはこの項目を除外する処理がない。[OAuth SMTPの仕様](https://www.zabbix.com/documentation/7.4/en/manual/config/notifications/media/email) · [保存処理](../go-lang/internal/clone/master.go)
- **テンプレートの新しい構成** — HTTPエージェントの同名ヘッダー・クエリー項目、7.4のネストしたLLDやテンプレートメタデータ、8.0のJSON型・Telemetry queryは構成の入出力経路を通り得る。実機のテンプレートは `agent.ping` とトリガーが中心で、新しいフィールドの往復保持は確認していない。[構成の適用](../go-lang/internal/clone/configuration.go) · [既存の実機試験](../tests/compatibility/matrix.py)
- **8.0のSNMP詳細、監視先の権限、コネクター** — `details.retries` のマクロ値を `host.update` に送ることはHTTPモックで検証済み。実機では `retries` や `max_repetitions` のマクロ指定を照合していない。監視先選択を制限したロールでのホスト・ネットワークディスカバリの適用と、JSON値（`item_value_type=64`）を持つコネクターの同期・送信にも実機フィクスチャがない。[8.0のAPI変更](https://www.zabbix.com/documentation/8.0/en/manual/api/changes) · [HTTPモック](../tests/go/internal/clone/compatibility80_test.go) · [既存の実機試験](../tests/compatibility/matrix.py)
- **即時チェック** — `discoveryrule.get`、`item.get`、`task.create` を呼ぶ実装はある。既存Goテストは間隔の正規化を確認しているが、実機の実行引数に `--checknow.execute` がなく、タスク作成の成否は照合していない。[即時チェック実装](../go-lang/internal/clone/operations.go) · [既存の実機試験](../tests/compatibility/matrix.py)

### 対応済み、または現行の呼び出しへの影響なし

- 7.2の `auth` 廃止はBearer認証への切り替えで対応済み。廃止された `selectGroups`、`selectRights`、`discoveryrule.copy` は現行の呼び出しにない。7.4の厳格化に対して `user.get` は `userid` を明示し、`mediatype.get` と `usergroup.get` は該当の `selectUsers` や `mfaids` を送らない。[取得定義](../go-lang/internal/clone/parameters.go) · [認証実装](../go-lang/internal/zabbix/client.go)
- 8.0のホストインターフェイスの変更、ロールの空の許可リスト、`serverid`、正規表現の説明、アクションのプロキシグループ条件は既存試験の検証範囲。廃止された `*.massupdate` は現行コードで呼び出していない。[互換性確認記録](zabbix80-compatibility.md)
- 履歴データ、マップ、APMデータ、デバイスは現行の複製対象外。CEPルールは2026-11-20に8.0以降向けに追加した（上記参照）。8.0の `configuration.importcompare` は使用していない。ダッシュボードの入出力は2026-11-11に8.0以降向けに追加した（上記参照）。グローバル正規表現は構成エクスポートの新オプションを使わず、既存の `regexp.get/create/update` で同期する。[取得・インポート定義](../go-lang/internal/clone/parameters.go)
