# Live compatibility matrix

Run `./tests/test-go.ps1 -count=1` from the repository root for the Go suite,
including the compatibility regression tests. The runner uses a Go overlay
so files in `tests/go/` can test the corresponding `go-lang/` packages.

These tests mutate **disposable** Zabbix instances and remove default source
hosts, actions, maintenance periods, and templates. Never use existing databases.
`deploy.ps1` uses an explicit Kubernetes context and a dedicated namespace;
databases use `emptyDir` and are lost when pods are deleted. Do not expose these
instances publicly: they use fixture-only credentials and the default Admin login.

Before a new run, compare each series with the
[official Zabbix release notes](https://www.zabbix.com/release_notes) and update
`versions.json` to the latest stable patch releases. The deployment and API
probe both read this file. Verify the running server binaries with
`kubectl exec <pod> -c server -- zabbix_server -V` before seeding.

1. From the repository root, install the API client with
   `python -m pip install -r tests/compatibility/requirements.txt`, then run
   `tests/compatibility/deploy.ps1`.
2. Wait for all eight pods to become ready. Forward each pod's port 8080 to
   localhost using `kubectl --context t2dev -n zc-compat-20261107 port-forward`:

   | Pods | Local ports (source / target) |
   | --- | --- |
   | z70-source / z70-target | 18700 / 18701 |
   | z72-source / z72-target | 18720 / 18721 |
   | z74-source / z74-target | 18740 / 18741 |
   | z80-source / z80-target | 18800 / 18801 |

3. Build `go-lang/cmd/zc` to `.tools/compat80/zc.exe` from the Go module directory:
   `go build -o ../.tools/compat80/zc.exe ./cmd/zc`.
4. Run `python tests/compatibility/matrix.py probe`, then `seed`.
5. Run `python tests/compatibility/matrix.py matrix initial`.
6. Run `python tests/compatibility/matrix.py mutate`, then
   `python tests/compatibility/matrix.py matrix updated`.
7. Inspect `.tools/compat80/results-*.json` and per-run logs. Any failed row or
   master export is a failed test. Downgrades are excluded. Different target
   instances are tested concurrently; writes to the same target are sequential.

The initial phase checks master file export, replica application, worker
reapplication and saved properties/references. The updated phase changes the
agent IP, adds a new default agent interface, removes SNMP, adds JMX, and checks
that the existing agent interface ID is preserved. The second role application
also exercises reapplication of the modified configuration.

The harness checks the frontend server name with an unauthenticated HTML request,
then uses `zabbix-utils` for the API version, login and all fixture API calls.
The 8.0 fixtures bypass the SDK's tested-version ceiling and
still require the exact version in `versions.json`. Unset `ZABBIX_TOKEN`,
`ZABBIX_USER` and `ZABBIX_PASSWORD` so the SDK uses only the fixture login.
Set `ZC_COMPAT_OUT` to a separate directory to keep
the artifacts and CLI binary from different runs separate. Logs are scanned for
failures even if the CLI exit code is zero.
After saving evidence, stop the port-forward processes and delete only the
dedicated test namespace to remove all test workloads.

## CEP/APM feature tests

These scripts accept only the fixture server names and localhost ports above.
Use a new, disposable namespace; the fixture deletes source defaults and exercises
full initialization on the target. Commands below run from the repository root.

```powershell
./tests/compatibility/deploy.ps1 -Context t2dev -Namespace zc-cep-apm-20261120 -Series 74,80
python tests/compatibility/cep_apm_addons.py
```

Wait for the pods, verify `zabbix_server -V`, and forward `z80-source`,
`z80-target`, and `z74-source` to ports 18800, 18801, and 18740 respectively.
Build `go-lang/cmd/zc` as `.tools/cep-apm-live/zc.exe`, then run:

```powershell
python tests/compatibility/cep_apm.py
python tests/compatibility/cep_apm_variants.py
```

The first script checks all five CEP window types, create/update/reapply,
APM permissions with different proxy IDs, secret exclusion, disabled settings,
dry-run, and protection when applying 7.4 data. The legacy `--delete.api` check
runs after applying 7.4 with full initialization so unrelated settings are aligned.
The second script checks HTTPS/TLS and authentication transitions, clearing
resource attributes, and deleting an obsolete CEP rule. HTTPS checks validate
configuration round trips; they do not establish a real TLS connection.
When ClickHouse is running with the fixture schema, run
`python tests/compatibility/cep_apm_secret_connection.py` to verify that the
source password is rejected while the destination secret allows all four APM
read APIs to connect. This check does not require telemetry rows to exist.

The optional runtime extension uses a trapper item and trigger in a template to
verify CEP event-name changes, plus OTLP traces, a gauge metric, and logs:

```powershell
python -m venv .tools/cep-apm-live/venv
./.tools/cep-apm-live/venv/Scripts/python.exe -m pip install -r tests/compatibility/requirements-runtime.txt
./.tools/cep-apm-live/venv/Scripts/python.exe tests/compatibility/cep_apm_runtime.py
```

Before the runtime extension, forward `apm-proxy:4317` to 14317 and
`z80-target:10051` to 18851. Download `clickhouse.sh`, `otel_all.sh`,
`otel_logs_schema.sh`, `otel_metrics_schema.sh`, and `otel_traces_schema.sh` from
the official Zabbix `release/8.0/database/clickhouse` directory, copy them to the
`schema-bash` pod, make the scripts executable, and execute `otel_all.sh` with
`-s http://clickhouse:8123 -d otel -u zc -p zc-target-fixture-only -t 86400`.
This credential is only for this disposable fixture. The target's generated
`zc.secret` and local artifacts must not be committed.

APM telemetry requires ClickHouse **26.8.17.4 or newer**; the lower 26.3 minimum
in the general database requirements does not apply to APM ingestion. The official
26.9 image needs AVX2. On a VM that does not expose AVX2, the runtime extension
records the ingestion check as `BLOCKED`; it must not be reported as passing.
The November 20 t2dev run verified configuration and a real CEP event but could
not complete ingestion because of this CPU limitation. See
[the live test report](../../documents/cep-apm-live-test.md).

Results and CLI logs are saved to `.tools/cep-apm-live/`. Retain the evidence,
stop only the port-forward processes started for the run, and delete only
`zc-cep-apm-20261120` from context `t2dev` after testing.
