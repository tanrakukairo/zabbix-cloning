# ZC (Zabbix Cloning) Go Version

[Japanese](readme.md)

ZC distributes and synchronizes Zabbix monitoring configuration from a source
Zabbix instance to other Zabbix instances. It is not intended as a complete
backup tool.
Think of Zabbix configuration as a potted plant: ZC helps with repotting and
cloning it.

The Go version supports Zabbix 6.0 through 7.4. For versions earlier than 6.0,
use the [Python legacy version](prototype/readme_prototype.md).

## Main Features

- Retrieves configuration from a master and saves it as a versioned store
- Applies hosts assigned by the `ZC_WORKER` tag to a worker
- Applies all hosts to a replica
- Tracks host name changes with the `ZC_UUID` tag
- Imports templates in dependency order
- Updates host interfaces by difference
- Supports file, Redis, DynamoDB, and direct master connections
- Inspects store data with the `view` command

When running as master, ZC adds `ZC_UUID` tags to hosts when required and
updates the `{$ZC_VERSION}` global macro.

## Supported Environment

- Go 1.23 or later
- Zabbix 6.0, 6.2, 6.4, 7.0, 7.2, and 7.4
- Windows or Linux

Development releases are not currently supported.

## Build

```sh
cd go-lang
go mod download
go build -o bin/zc ./cmd/zc
go build -o bin/view ./cmd/view
```

Windows builds create `bin/zc.exe` and `bin/view.exe`.

## Supported Configuration

- Host groups, template groups, hosts, and host interfaces
- Templates, items, LLD rules, triggers, and value maps
- Media types, global macros, and regular expressions
- Actions, scripts, maintenance, network discovery, and event correlation
- Services, SLAs, proxies, proxy groups, and connectors
- Users, user groups, roles, user directories, MFA, and authentication
- General Zabbix settings and auto-registration settings

Passwords and secrets unavailable through the Zabbix API are supplemented by
configuration files.

## Roles

### master

The source of configuration. Retrieves data from Zabbix and saves a new version
to the selected store. Hosts without `ZC_UUID` receive one automatically.

### worker

A monitoring destination. Applies only hosts whose `ZC_WORKER` host tag
matches `--node`. Target hosts are enabled for monitoring. Templates are
skipped by default.

### replica

A copy of the master. Applies all hosts regardless of `ZC_WORKER`. Additional
host monitoring status is preserved from the master. Additional
notification-media settings are skipped.

## Stores

### file

Stores bzip2-compressed JSON locally.

```text
{VERSION_ID}_{UNIXTIME}_{MASTER_VERSION}.bz2
```

Set the location with `--file.store.path` or `ZC_FILE_STORE_PATH`.

- Windows: `%USERPROFILE%\Documents\zc`
- Linux: `/var/lib/zabbix/zc`

### Redis

- DB 0: version metadata
- DB 1: bzip2-compressed configuration data
- Default port: `6379`

### DynamoDB

- `ZC_VERSION`: `VERSION_ID` + `UNIXTIME`
- `ZC_DATA`: `VERSION_ID` + `DATA_ID`

`--store.endpoint` accepts an AWS Region or a URL, including LocalStack. For
a URL, the Region is taken from `AWS_DEFAULT_REGION` or `aws_region`.

### direct

Does not store data. A worker or replica connects to the master directly to
retrieve configuration.

## Examples

The following examples assume binaries built in `go-lang`.

### Save master data to a file store

```sh
bin/zc master --no.config.files --yes \
  --node master-zabbix \
  --endpoint https://master.example.com \
  --token TOKEN \
  --store.type file \
  --file.store.path ./store
```

### Apply data to a replica

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

### DynamoDB or LocalStack

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

### List versions

```sh
bin/view showversions --no.config.files \
  --store.type file --file.store.path ./store
```

### Display stored data

```sh
bin/view showdata --no.config.files \
  --store.type file --file.store.path ./store \
  --version VERSION_ID \
  --method host template \
  --id.only
```

`--method` and `--name` accept multiple values.

## zc Options

The first argument to `zc` must be the execution role: `master`, `worker`, or
`replica`. There is no option for selecting the execution role.

Option names use dot separators. Boolean options are enabled when specified.

| Option | Description |
|---|---|
| `-n`, `--node NAME` | Target Zabbix node name |
| `-e`, `--endpoint URL` | Zabbix frontend URL |
| `-t`, `--token TOKEN` | API token |
| `-u`, `--user USER` | API user |
| `-p`, `--password PASSWORD` | API password |
| `-v`, `--version UUID` | Store version to apply to a worker or replica |
| `--self.cert` | Disable TLS certificate verification |
| `-y`, `--yes` | Skip the execution confirmation |
| `-q`, `--quiet` | Suppress progress output |
| `--dry.run` | Check differences without create, update, or delete operations |
| `--update.password` | Update target-user passwords |
| `--initialize` | Initialize a worker or replica before applying |
| `--initialize.full` | Delete all deletable worker or replica settings before applying |
| `--online` | Keep the target online without creating the temporary `__ZC_UPDATE__` maintenance |
| `--useip` | Resolve DNS interfaces and use their IP addresses |
| `--host.update` | Update existing hosts |
| `--force.host.update` | Update a host with matching `ZC_UUID` despite a different name; also enables `--host.update` |
| `--no.uuid` | Reserved compatibility option; Go always uses `ZC_UUID` |
| `--delete.host` | Delete hosts not present in the store |
| `--delete.api` | Delete API-managed configuration not present in the store |
| `--skip.template` | Skip template export and import |
| `--skip.host` | Skip host application |
| `--template.separate.num N` | Template export partitions for master; default: 100 |
| `--checknow.execute` | Immediately run LLD rules and target items after host application |
| `--checknow.interval VALUE...` | CheckNow monitoring intervals; default: `1h` |
| `--disable.monitoring` | Disable monitoring for applied hosts |
| `--parallel.host.apply N` | Parallel host and host-interface operations; default: 4. Interface order is preserved within each host |

For template, host, and host-interface application, the on-screen count is
updated continuously unless `--quiet` is specified. With `--quiet`, no progress
is displayed and result summaries are logged every 50 items and once for the
final remainder. Every failure is logged individually. Without `--quiet`, only
the failed targets are also listed at the end of the operation.

### Dry Run

`--dry.run` performs normal retrieval and difference calculation but does not
send create, update, or delete API calls to Zabbix. It also excludes
`configuration.import`, secret global macros, PSK, CheckNow, real initialization API calls,
and version-macro updates.

A master does not save a new store version and omits CheckNow waiting. At the
end, logs list the number of recorded but unexecuted operations by API method.

For a worker or replica, dry run constructs a virtual state from the first GET response.
Objects removed by `--initialize` (`correlation`, `drule`, `action`,
`script`, and `maintenance`) are removed from that state. Later create,
update, delete, configuration import, host, and global-settings operations are
applied to it. New objects receive synthetic IDs, allowing references and
difference checks to continue as though initialization had run. Refresh does
not return virtual state to the real instance state.

Because Zabbix creates CheckNow items after configuration application, dry run
queries only hosts already present on the real instance. Virtually created
hosts are excluded and their count is logged.

### Full Initialization

`--initialize.full` can only be enabled on the command line; configuration
files, secret files, and environment variables cannot enable it. It includes
`--initialize` and deletes every deletable
API-managed object from a worker or replica in dependency order, then applies
the stored configuration. `--yes` and `--quiet` are disabled, and two `y/N`
confirmations are required before deletion starts. Rejecting either prompt
aborts without deleting anything.

Admin, the API execution user, Zabbix administrators, read-only roles, and `{$ZC_VERSION}` are retained
so ZC can continue operating. General settings, authentication settings, and
auto-registration settings have no delete API; they are overwritten from the
store during subsequent application. Authentication is switched to internal
authentication before user directories and MFA settings are deleted.

During dry run, nothing is deleted from the real instance. The same deletion
is applied to virtual state before subsequent differences are calculated.

## Store Options

| Option | Description |
|---|---|
| `-s`, `--store.type file\|redis\|dydb\|direct` | Store type |
| `-se`, `--store.endpoint VALUE` | Redis host, DynamoDB Region/URL, or direct master URL |
| `-sp`, `--store.port PORT` | Redis port |
| `-sa`, `--store.access VALUE` | AWS access key or direct master node name |
| `-sc`, `--store.credential VALUE` | AWS secret key, Redis password, or direct master token |
| `-sl`, `--store.limit N` | DynamoDB writes for wait decisions; default: 10 |
| `-sw`, `--store.interval N` | DynamoDB wait seconds; default: 2 |
| `--file.store.path PATH` | File-store location |

## Configuration and Log Options

| Option | Description |
|---|---|
| `-f`, `--config.file FILE` | JSON configuration file |
| `--secret.file FILE` | JSON secret configuration file |
| `--no.config.files` | Do not load configuration files |
| `-l`, `--log.level DEBUG\|INFO\|WARNING\|ERROR\|CRITICAL` | Log level |
| `--log.file FILE` | Log file |

## view Options

In addition to shared store, configuration, and log options, `view` supports:

| Option | Description |
|---|---|
| `-v`, `--version UUID` | Version to display |
| `--method VALUE...` | Limit methods to display |
| `--name VALUE...` | Limit names to display |
| `--id.only` | Display only IDs and names |

`showversions` cannot be used with `direct`, which has no persistent
versions.

## Configuration Files and Environment Variables

Configuration files use JSON. Precedence is:

1. Command-line options
2. Environment variables beginning with `ZC_`
3. `zc.secret`
4. Configuration file
5. Default values

Default configuration paths:

- Windows: `%ProgramData%\Zabbix\zc.conf`
- Linux: `/etc/zabbix/zc.conf`

Environment variables use an uppercase configuration key with the `ZC_`
prefix, for example `ZC_STORE_TYPE` and `ZC_TOKEN`.

Specify a secret file, in priority order, with `--secret.file`,
`ZC_SECRET_FILE`, or `secret_file` in `zc.conf`. A relative path in
`zc.conf` is relative to its directory. If no path is specified, ZC reads
`zc.secret` from the `zc.conf` directory. With `--no.config.files`, ZC
reads secrets only when `--secret.file` or `ZC_SECRET_FILE` is specified.

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

Put only secrets in `zc.secret`; do not include it in version control.

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

Configuration-file-only values:

| Key | Description |
|---|---|
| `description` | Description of a master-created version |
| `secret_file` | `zc.secret` path; relative to `zc.conf` |
| `media_settings` | User notification media configured for a worker |
| `store_connect.aws_region` | DynamoDB Region |
| `store_connect.aws_endpoint_url` | DynamoDB-compatible endpoint URL |
| `store_connect.redis_host` | Redis host |
| `store_connect.redis_port` | Redis port |
| `store_connect.direct_node` | Direct master node name |
| `store_connect.direct_endpoint` | Direct master URL |

### settings

On Zabbix 7.0, `settings` can specify any updateable property of the
[settings object](https://www.zabbix.com/documentation/7.0/en/manual/api/reference/settings/object#settings).
Specified values take precedence over values retrieved from the master.
The read-only `ha_failover_delay` cannot be specified.

`severity` and `timeout` also support abbreviated notation.

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

Severity levels range from 0 to 5 and colors use six hexadecimal digits.
`timeout` supports `zabbix_agent`, `simple_check`, `snmp_agent`,
`external_check`, `db_monitor`, `http_agent`, `ssh_agent`,
`telnet_agent`, `script`, and `browser`.

Main secret values in `zc.secret`:

| Key | Description |
|---|---|
| `token`, `password`, `platform_password` | Credentials for Zabbix connections and password updates |
| `store_connect.aws_account_id`, `store_connect.aws_secret_key` | DynamoDB AWS credentials |
| `store_connect.redis_password` | Redis password |
| `store_connect.direct_token` | Direct destination API token |
| `enable_user` | Passwords for cloned users |
| `mfa_client_secret` | Duo MFA client secret |
| `secret_globalmacro` | Secret global macro names and values |
| `psk` | PSK identities and PSKs for proxies, hosts, and auto-registration |

When `secret_globalmacro` is set, `FirstProcess` adds or overwrites a
same-named global macro as `type=1`. An existing macro of another type is
also changed to the secret type.

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

Named proxies and hosts are updated only when they exist. `tls_accept=1` is
changed to `2`; for `2` or `3`, PSK is set while retaining the current
value. Objects with `tls_accept` of `4` or higher are not updated, to
protect certificate settings. Other TLS changes are unsupported. PSK processing
runs after worker or replica configuration application and never runs on a
master.

## Host Updates

- Update matching host names and `ZC_UUID` values with `--host.update`.
- Update matching `ZC_UUID` values with different host names using
  `--force.host.update`.
- Unchanged interface settings do not make API calls.
- Main-interface changes avoid conflicts with the current main interface.
- Existing interfaces absent from new configuration are deleted last.
- A deletion blocked by item associations is recorded as failed, and processing
  continues.

## Warning About Delete Options

`--delete.host` and `--delete.api` actually delete objects absent from master
data. First inspect differences without delete options. `--initialize` or
`--initialize.full` takes precedence over delete options.

## Data Compatibility

The Go version and [Python legacy version](prototype/readme.md) share file,
Redis, and DynamoDB data formats. Each version can read data written by the
other.

## Verification Status

The following have been verified against Zabbix 7.0, 7.2, and 7.4 instances:

- Master storage to file, Redis, and DynamoDB
- Cross-reading stores between Go and Python
- Template import and host updates
- Host-interface difference detection
- Action, maintenance, drule, correlation, role, user group, and authentication
- `{$ZC_VERSION}` updates
- Replica and worker application from a 7.0 master to 7.2 and 7.4
- Replica and worker application from a 7.2 replica promoted to master to 7.2 and 7.4
- Replica and worker application from a 7.4 replica promoted to master to 7.4

The following Zabbix configuration types are supported:

- Host groups
- Template groups
- Hosts
- Templates
- Actions (trigger, service, discovery, and internal)
- Scripts
- Maintenance
- Network discovery
- Services
- SLAs
- Event correlation
- Users
- User media types
- User groups
- Roles
- General settings
- Secret global macros
- Proxies
- Proxy groups
- Authentication (not tested)
- LDAP authentication settings (not tested)
- SAML authentication settings (not tested)
- MFA authentication settings (not tested)

## Unsupported

- Zabbix versions earlier than 6.0; use the
  [Python legacy version](prototype/readme_prototype.md)
- Complete backups of Zabbix screens, dashboards, and similar objects
- Automatic restoration of secrets unavailable through the API
- Development releases and Zabbix 7.2 or later

## License

[MIT License](LICENSE)
