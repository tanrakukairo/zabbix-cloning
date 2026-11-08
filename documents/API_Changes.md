# Zabbix API 仕様変更（7.0 / 7.2 / 7.4 / 8.0）

各系列の初回リリースと直前の系列との差分を、後続パッチの変更と分けて整理する。互換性に関わる変更を中心に、主な新機能も記載する。メソッド名、フィールド名、列挙値はAPIで使用する表記に合わせている。

## 7.0（6.4 → 7.0）

[Zabbix公式 API変更履歴（6.4 → 7.0）](https://www.zabbix.com/documentation/7.0/en/manual/api/changes)

### リクエスト・レスポンスの互換性

| 対象 | 変更内容 |
| --- | --- |
| ホスト・プロキシ参照 | `host`、`drule`、`task` の `proxy_hostid` が `proxyid` に変更。ホストに `monitored_by`、`proxy_groupid`、読み取り専用の `assigned_proxyid` が追加された。プロキシを直接指定するときは `monitored_by=1` と `proxyid` を指定する。 |
| プロキシ設定 | `host` → `name`、`status` → `operating_mode`、`proxy_address` → `allowed_addresses`。動作モードは `0` がアクティブ、`1` がパッシブ。パッシブ用の `address` と `port` を追加し、`interface`、`auto_compress`、`proxy.get` の `selectInterface` を廃止。 |
| ホスト・グループ | `host.get` の `proxy_hosts` を廃止。`hostgroup.get` の `selectHostPrototype`、`selectDiscoveryRule`、`selectGroupDiscovery` は複数形になり、対応する結果キーも複数形になった。結果はオブジェクトの配列。旧 `selectGroupDiscovery` で指定できた `groupid` は削除された。 |
| HTTPエージェント設定 | `item`、`itemprototype`、`discoveryrule` の取得・作成・更新で、`headers` と `query_fields` は名前をキーにした形式から `[{"name":"…","value":"…"}]` に変更。同名のエントリーを保持できる。 |
| 入力検証 | `alert.get`、`event.get`、`problem.get` のパラメーター、およびホスト作成・更新・一括操作の `groups`、`macros`、`tags`、`templates` の検証を厳格化。アイテムとアイテムプロトタイプの「取得不可値のチェック」前処理では `params` が必須。 |
| スクリプト | `script.getscriptsbyhosts` と `script.getscriptsbyevents` はIDだけの配列から、各要素に `hostid` または `eventid` と必要に応じて `scriptid`、`manualinput` を持つ形式に変更。サーバー上で実行する `execute_on=1` はサーバー側でグローバルスクリプトの実行を許可している場合に指定できる。 |
| 認証・ユーザー | HTTP認証用の `http_*` プロパティはフロントエンドの `$ALLOW_HTTP_AUTH` に依存。ユーザーの `userdirectoryid` は更新不可になり、プロビジョニング済みユーザーのグループ変更にも制限が加わった。ディレクトリのメディアマッピングには `active`、`severity`、`period` が追加された。 |
| ダッシュボード | `plaintext` が `itemhistory` に変更され、`itemids.0`、`style`、`show_as_html` はそれぞれ `columns.0.itemid`、`layout`、`columns.0.display` に変更。複合フィールドの添字位置、DBオブジェクト参照、ウィジェット間参照、期間指定の形式も変更された。通常・テンプレート両方でウィジェット座標とサイズの範囲が拡大し、横幅は最大24から72になった。 |

### 主な追加・非推奨化

- `proxygroup` APIを追加。`proxy.get` はプロキシグループや割り当て済みホストを取得できる。プロキシと全体設定に監視方式別のタイムアウトが追加された。
- `mfa.create/get/update/delete` と `user.resettotp` を追加。認証設定・ユーザーグループに `mfa_status` と `mfaid` が追加された。
- `history.push` を追加。`history.get` は `ns` でソートできる。コネクターには送信する値の型を選ぶ `item_value_type` と再試行間隔の `attempt_interval` が追加された。
- アイテム、アイテムプロトタイプ、LLDルールに Browser 型（`type=22`）を追加。対象の監視方式で `timeout` を指定でき、SNMPの前処理に `SNMP get value`（`30`）を追加。
- LLDで検出されなくなったリソースについて、削除と無効化を別々に設定できる。削除までの `lifetime` の既定値は `30d` から `7d` に変更。`discoveryrule.copy` は非推奨になった。
- `event.get` に `selectAcknowledges`、`selectAlerts`、`trigger_severities`、`groupBy` を追加。旧 `select_acknowledges` と `select_alerts` は非推奨。メディアタイプの `content_type` は非推奨となり、`message_format` が追加された。
- 自動登録とネットワークディスカバリのアクションにホストタグの追加・削除を追加。ダッシュボードには `itemnavigator`、`hostnavigator`、`honeycomb`、`toptriggers`、`gauge`、`piechart` などを追加。

### 7.0.x の主な変更

| バージョン | 変更内容 |
| --- | --- |
| 7.0.1 | `svggraph` と `piechart` にアイテム参照、`piechart` に `stroke` を追加。 |
| 7.0.4 / 7.0.6 | メディアタイプの参照権限を調整。7.0.4では Admin の取得可能項目を制限し、`selectMessageTemplates` は Super admin 限定。7.0.6では User も基本項目を取得できる。 |
| 7.0.7 / 7.0.11 | 7.0.7でクロスオリジンの `Authorization` ヘッダーと `OPTIONS` に対応。7.0.11でヘッダー名を大文字・小文字の区別なく扱うようになった。 |
| 7.0.9 | `user.get` で Admin / User が取得できる他ユーザーの情報を制限し、メディア・権限・ロールの詳細は本人分に限定。関連APIの `selectUsers` にも同じ制限が反映される。プロビジョニングの詳細やユーザーグループの `mfaid`、`userdirectoryid`、`mfaids` フィルターは Super admin 限定。 |
| 7.0.10 | Admin / User の `alert.get` によるメッセージ通知の参照範囲を本人から所属グループのユーザーに拡張。アクションが参照しているホスト、グループ、テンプレート、トリガー、ディスカバリチェックの削除はエラーになる。 |
| 7.0.14 / 7.0.19 | 7.0.14で `clock` の `date_size`、`time_size`、`tzone_size` を削除。7.0.19で `problems.highlight_row` を追加し、Admin / User も `task.create` で「今すぐ実行」タスクを作成できるようになった。 |
| 7.0.27 | LLDの上書き操作の `delay`、`history`、`trends` でLLDマクロを再び使用できる。 |
| 7.0.32 | ユーザーディレクトリのSAML署名設定5項目（`sign_assertions`、`sign_authn_requests`、`sign_messages`、`sign_logout_requests`、`sign_logout_responses`）の既定値を `1`（有効）に変更。 |

[Zabbix公式 API変更履歴（7.0.x）](https://www.zabbix.com/documentation/7.0/en/manual/api/changes_7.0)

## 7.2（7.0 → 7.2）

[Zabbix公式 API変更履歴（7.0 → 7.2）](https://www.zabbix.com/documentation/7.2/en/manual/api/changes)

### 廃止・入力検証

| 対象 | 変更内容 |
| --- | --- |
| API認証 | JSON-RPCリクエスト本文の `auth` を廃止。認証が必要な呼び出しではHTTPヘッダーの `Authorization: Bearer <token>` を使用する。 |
| LLD・イベント | `discoveryrule.copy` を廃止。`event.get` の `select_acknowledges` と `select_alerts` を削除し、`selectAcknowledges` と `selectAlerts` を使用する。 |
| グループ取得 | `graph.get`、`graphprototype.get`、`host.get`、`maintenance.get`、`script.get`、`template.get`、`trigger.get`、`triggerprototype.get`、`usermacro.get` の `selectGroups` を廃止。 |
| グループ・メンテナンス | `hostgroup.get` の `monitored_hosts`、`real_hosts` と、`maintenance.create/update` の `groupids`、`hostids` を廃止。メンテナンスの対象は `groups`、`hosts` で指定する。 |
| メディアタイプ | `content_type` と `exec_params` を廃止。`mediatype.get` の `output`、`filter`、`search` の検証を厳格化。 |
| ユーザーグループ | `usergroup.create/update` の `rights`、`userids` と、`usergroup.get` の `selectRights` を廃止。 |
| ダッシュボード | `dataover` と、`clock` のサイズ指定3項目を廃止。`tophosts.style` を `layout` に変更。 |

ダッシュボードとテンプレートダッシュボードに `topitems` と `hostcard` を追加。`tophosts` に値の表示形式、強調表示、バイナリ値のサムネイルを追加し、アイテム値系のウィジェットではスパークラインを設定できる。

### 7.2.x の主な変更

- **7.2.1 / 7.2.5** — 7.2.1でクロスオリジンの `Authorization` と `OPTIONS` に対応。7.2.5でヘッダー名の大文字・小文字を区別しなくなった。
- **7.2.3 / 7.2.4** — 7.2.3で `user.get` と関連する `selectUsers` の参照権限を制限。7.2.4で Admin / User が所属グループのメッセージ通知を参照できるようになり、アクションから参照中のリソースを削除するとエラーになる。

[Zabbix公式 API変更履歴（7.2.x）](https://www.zabbix.com/documentation/7.2/en/manual/api/changes_7.2)

## 7.4（7.2 → 7.4）

[Zabbix公式 API変更履歴（7.2 → 7.4）](https://www.zabbix.com/documentation/7.4/en/manual/api/changes)

- **ユーザー取得** — `user.get` のパラメーター検証を厳格化。`userid` は `output` に明示した場合か、`output: "extend"` の場合に返る。`mediatype.get` の `selectUsers` と `usergroup.get` の `mfaids`、`selectUsers` も検証が厳格になった。
- **マップ** — リンクトリガーから `linktriggerid` と `linkid` を削除。リンクにアイテム・閾値・強調表示を使うインジケーター設定を追加し、背景の拡大縮小、ラベル表示、要素の重なり順を設定できるようになった。
- **ネストしたLLD** — LLDルールプロトタイプを扱う `discoveryruleprototype.create/get/update/delete` を追加。グラフ、ホスト、ホストグループ、アイテム、トリガーの取得では、従来の個別の検出情報セレクター（`selectGraphDiscovery` など）を非推奨とし、`selectDiscoveryData` を追加。ホストとテンプレートでは `selectDiscoveries` に代わり `selectDiscoveryRules` を追加。グラフ、ホスト、アイテム、トリガーの各プロトタイプの取得には `selectDiscoveryRulePrototype` と `selectDiscoveryData` が追加された。
- **メディア・ロール** — SMTP認証にOAuth（`smtp_authentication=2`）を追加し、クライアントIDやトークン関連のフィールドを追加。ロールに `edit_own_media` と `edit_user_media` を追加。
- **テンプレート・ダッシュボード** — テンプレートに `readme`、`wizard_ready`、ホストマクロに `config` を追加。通常・テンプレートダッシュボードに `itemcard`、グラフと円グラフにカラーパレット設定を追加。

### 7.4.x の主な変更

- **7.4.3** — `problems.highlight_row` を追加。Admin / User も `task.create` で「今すぐ実行」タスクを作成できる。
- **7.4.11** — LLDの上書き操作の `delay`、`history`、`trends` でLLDマクロを再び使用できる。
- **7.4.16** — SAML署名設定5項目の既定値を `1`（有効）に変更。項目は7.0.32と同じ。

[Zabbix公式 API変更履歴（7.4.x）](https://www.zabbix.com/documentation/7.4/en/manual/api/changes_minor)

## 8.0（7.4 → 8.0）

[Zabbix公式 API変更履歴（7.4 → 8.0）](https://www.zabbix.com/documentation/8.0/en/manual/api/changes)

### 廃止・動作変更

| 対象 | 変更内容 |
| --- | --- |
| ホストインターフェイス | `hostinterface.create`、`update`、`delete`、`massadd`、`massremove` を廃止。インターフェイスの作成・変更は `host.create` や `host.update` の `interfaces` で行う。 |
| 一括更新 | `host.massupdate`、`hostgroup.massupdate`、`template.massupdate`、`templategroup.massupdate` を廃止。ホストは `host.update`、残りは対象の `update`、`massadd`、`massremove` を操作に応じて使用する。 |
| バイナリ履歴 | `history.get` に `maxValueSize` を追加。`history=5` のバイナリ値は既定で64 KiBに切り詰められる。値全体を取得する場合は `maxValueSize: null` を指定する。 |
| ロールのAPI権限 | `api.mode=1`（許可リスト）で `api` が空なら、全APIメソッドを拒否する。`api.access` の既定値は `0`（無効）になった。 |
| 監視先の権限 | ホストの `monitored_by` に監視先の選択権限が関わる。ロールに `select_server_for_monitoring` を追加。ユーザーグループではプロキシとプロキシグループのアクセス権を設定できる。 |
| SAML | ユーザーディレクトリにIdP/SPの証明書やSP秘密鍵のフィールドと対応するハッシュを追加。SAML署名設定5項目の既定値は `1`（有効）。 |

### 主な追加

| 対象 | 変更内容 |
| --- | --- |
| APM・テレメトリー | `apmlog`、`apmmetric`、`apmspan`、`apmtrace` APIを追加。プロキシに `apm` と `selectApm`、ユーザーグループに `apm_filters` と `selectApmFilters` を追加。アイテムとプロトタイプに Telemetry query（`type=24`）と `query`、`time_shift`、`lookback_limit`、`granularity` を追加。 |
| JSON値 | アイテムとプロトタイプに `value_type=6`、`history.get` に `history=6` を追加。コネクターではJSON値を `item_value_type` の値 `64` で指定できる。 |
| 構成の入出力 | `configuration.import/importcompare` に `returnMissingObjects` とダッシュボード用の `rules.dashboards` を追加。グローバル正規表現はエクスポートの `options.global_regexes`、インポート・比較の `rules.global_regexes` で指定できる。正規表現オブジェクトに `description` を追加。 |
| メンテナンス | 作成・更新でトリガーやイベント名を指定でき、問題タグに否定条件が追加された。取得に `triggerids`、`selectTriggers`、`selectEventNames` を追加。`event.get` と `problem.get` の `selectAcknowledges` で `maintenanceid` を参照できる。 |
| タグ・アクション | `item.get`、`template.get`、`trigger.get`、`httptest.get` に `inheritedTags` と `selectInheritedTags` を追加。対象のプロトタイプにも `selectInheritedTags` を追加。ディスカバリ・自動登録のアクション条件にプロキシグループ（`conditiontype=29`）を追加。 |
| SNMPインターフェイス | `details.retries` を追加。`max_repetitions` と `retries` に、ホスト側ではユーザーマクロ、ホストプロトタイプ側ではLLDマクロを使用できる。 |
| デバイス・CEP | `device.get`、`device.offboard`、`serverinfo.get` と、プッシュ通知用のメディアタイプ（`type=5`）を追加。複合イベント処理用の `ceprule` APIを追加。 |
| ダッシュボード | 通常・テンプレートダッシュボードに `scatterplot` を追加。グラフ・円グラフのアイテムタグ指定、地図のクラスタリング、`topitems` の列集約などを追加。 |

公式の8.0.x API変更履歴では、参照時点で8.0.1の個別変更は掲載されていない。[Zabbix公式 API変更履歴（8.0.x）](https://www.zabbix.com/documentation/8.0/en/manual/api/changes_minor)
