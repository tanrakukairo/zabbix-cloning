# CEP・APMのt2dev実機検証

確認日: 2026-11-20。対象はGo版のCEP・APM設定同期。

## 検証環境

- Kubernetes context: `t2dev`
- 専用namespace: `zc-cep-apm-20261120`。使い捨てPodと`emptyDir`のみ使用。
- Zabbix server / frontend: 8.0.0を取得元・適用先各1台、7.4.16を旧データの取得元に使用。APIとサーバーバイナリの両方でバージョンを照合。
- APM collector: 公式Zabbix proxy 8.0.0 SQLite版。
- ClickHouse: 26.3.66.4で接続・照会を確認。APM収集要件を満たす公式26.9イメージは検証ノードのCPU制約で起動不可。
- CLI: 現在の作業ツリーをGo 1.27.0 / Windows amd64でビルド。fileストアを使用。

取得元と適用先でプロキシIDを変えた。接続パスワードも異なるテスト専用値とし、複製先の値は生成した`zc.secret`で供給した。既存の業務用ZabbixやClickHouseには接続していない。

## 成功した設定同期とイベント処理

| 対象 | 確認内容 |
| --- | --- |
| CEPの初回同期 | None / Simple (multiple) / Cause and symptom grouping / Simple (once) / Event pattern matchの5種を取得・保存し、replicaへ作成。ルール・操作・スコープの条件式、ウィンドウ、順序、有効状態をAPIで比較。 |
| CEPの更新・worker | workerで再適用。説明・順序・有効状態の更新で適用先IDを維持。 |
| CEPの削除 | 8.0ストアの`--delete.api`で余分なルールだけ削除。8.0ストアの`--initialize.full`で削除・再作成し、IDの変更と設定一致を確認。 |
| CEPの実イベント | テンプレートのトラッパーアイテムから障害を発生させ、複製したNone型ルールでイベント名が`ZC CEP original`から`ZC CEP processed`になることを`problem.get`で確認。 |
| APMデータソース | `settings.apm_global_db`の有効化・無効化、URL、認証方式、ユーザー名、DB名、TLS検証設定を比較。HTTPS→検証無効→HTTP・認証なしの切替も成功。HTTPSは設定の往復確認で、実TLS接続は対象外。 |
| プロキシAPM | 収集有効・無効、受信上限、Traces / Metrics / Logsそれぞれの追加リソース属性を比較。空配列による属性クリアも成功。 |
| APM権限 | ユーザーグループのプロキシ参照が複製先IDになることと、3種類のシグナルのリソース属性フィルターを確認。 |
| シークレット | 展開したストアに取得元・複製先の接続パスワードが含まれないことを確認。パスワードだけのローカル上書きでURL・DB名を維持。 |
| シークレットの実接続 | 取得元のパスワードはClickHouseで認証拒否。複製先の`zc.secret`を使った`apmtrace.get`、`apmspan.get`、`apmmetric.get`、`apmlog.get`は接続成功。テーブルは空のため件数は0。実データの保存成功を意味するものではない。 |
| 適用順序 | CLIログで一般Settings・プロキシ本体・アカウント適用後に、データソース→プロキシAPM→ダッシュボードの順となることを確認。 |
| dry-run | 実行前後のCEP・データソース・プロキシAPM設定が一致し、変更されないことを確認。 |
| 7.4由来データ | 7.4ストアを8.0へ適用。完全初期化でもCEPとAPMデータソースを保持。その後の差分削除でもCEP・データソースと、旧ストアに存在しないプロキシAPM項目を保持。 |

この実機試験ではCEPの5種すべてについて設定同期を確認したが、実イベントを使った処理確認はNone型のイベント名変更のみ。残りのウィンドウ種別の時間経過・パターン判定までは確認していない。

## APM実データ収集の制約

OTLP/gRPCでトレース・Gaugeメトリクス・ログを送信し、Zabbix proxyの受信と、同期したリソース属性の付与、ClickHouseへのINSERT要求までは確認した。しかし26.3.66.4では`DateTime64`への小数秒入力が非同期INSERTで`ParsingError`となり、データは保存されなかった。

Zabbixの一般DB要件のClickHouse 26.3という下限はAPMには適用できない。[公式要件](https://www.zabbix.com/documentation/8.0/en/manual/installation/requirements)では、テレメトリデータソースに26.8.17.4以降が必要。そこで公式26.9イメージに更新したが、`clickhouse extract-from-config`が`Illegal instruction`（終了コード132）で停止した。検証ノードの`/proc/cpuinfo`にはAVX/AVX2がなく、公式ビルドを起動できない。ClickHouseの[互換ビルドに関する公式issue](https://github.com/ClickHouse/ClickHouse/issues/123145)にもこの制約が記載されている。

したがって、**OTLP→ClickHouse保存→Zabbix APMでの実データ照会は未完了（BLOCKED）**。AVX2を公開する環境で26.8.17.4以降のClickHouseを起動して再試験する必要がある。検証用VMやクラスタのCPU設定は変更していない。

## VM設定変更後のClickHouse単独起動確認

同日のVM設定変更後、別の一時namespace `zc-clickhouse-avx2-check` にClickHouseだけを起動した。`t2dev-worker3`上で`/proc/cpuinfo`に`avx`・`avx2`があることを確認し、公式26.9イメージはReady、再起動0回、実測バージョン26.9.33.10となった。`SELECT version(), 1`も成功した。前節の起動制約はこのノードで解消しているが、CEP/APMの通し試験はこの追加確認では再実施していない。確認後、一時namespaceを削除した。

## VM設定変更後のZabbix接続確認

同日、専用namespace `zc-apm-connect-check` にZabbix 8.0.0（server・frontend・PostgreSQL）とClickHouse 26.9.33.10を作成した。ClickHouseは起動確認済みの`t2dev-worker3`へ配置し、クラスタ内の`http://clickhouse:8123`をグローバルデータソースとして設定した。

公式スキーマにテスト用トレース・Gaugeメトリクス・ログを直接投入し、Zabbix frontendの`apmtrace.get`、`apmspan.get`、`apmmetric.get`、`apmlog.get`で各1件を取得した。さらにZabbix serverのTelemetry queryアイテムで以下を取得できた。

```json
{"SpanName":"connection-probe","Duration":1000000,"events":1}
```

これにより、frontendとserver双方から、設定した認証情報でClickHouseへ接続して実データを照会できることを確認した。投入はテスト用SQLによるもので、OTLP collector経由の収集試験ではない。[接続確認の証跡](zabbix-clickhouse-connection.json)を保存し、一時環境は削除した。

## 試験途中で見つかった既存の別問題

- 8.0で生成した他設定が残った状態で7.4の`--delete.api`を直接実行すると、CEP到達前にグローバルマクロの削除が`usermacro.delete`で失敗した。この呼び出しは既存の共通削除処理によるもの。今回の旧データ保護試験では、先に7.4で完全初期化して他の設定を揃え、その後に差分削除を確認した。直接適用する一般ケースの問題は今回未修正。
- テンプレートを持たない検証用ホストは、既存の`host.create`処理で`templates`が配列でないというエラーになった。CEPの実イベント試験は、通常の対応経路であるテンプレート配下のアイテム・トリガーを用いた。ホスト単独設定の問題は今回未修正。

CEP・APMの同期処理自体について、この実機試験で追加の修正は必要なかった。上記の環境制約と既存問題を、成功した設定同期と区別して記録する。

## 再現手順と証跡

[検証README](../tests/compatibility/README.md#cepapm-feature-tests)を参照。

- [基本の設定同期試験](../tests/compatibility/cep_apm.py)
- [TLS・認証・差分削除の試験](../tests/compatibility/cep_apm_variants.py)
- [CEP実イベント・OTLPの実行試験](../tests/compatibility/cep_apm_runtime.py)
- [シークレット接続試験](../tests/compatibility/cep_apm_secret_connection.py)
- [使い捨てClickHouse・proxyのデプロイ](../tests/compatibility/cep_apm_addons.py)

詳細結果、各CLIのログ・標準出力、fileストアはローカルの`.tools/cep-apm-live/`へ保存。生成したシークレット・ストア・実行ファイルはバージョン管理の対象外。
シークレットを含まない[結果JSON](cep-apm-live-results.json)も保存した。

最終確認で`./tests/test-go.ps1 -count=1`は全パッケージ成功し、追加したPython検証スクリプトもコンパイル確認済み。証跡保存後、今回開始したport-forwardと専用namespaceを削除し、残存していないことを確認した。
