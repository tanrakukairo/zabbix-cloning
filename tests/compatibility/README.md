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
