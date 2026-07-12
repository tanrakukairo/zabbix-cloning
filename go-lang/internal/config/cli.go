package config

import (
	"fmt"
	"strings"
)

var aliases = map[string]string{
	"-l": "log_level", "--log.level": "log_level", "--log.file": "log_file", "-v": "version", "--version": "version",
	"-q": "quiet", "--quiet": "quiet", "-y": "yes", "--yes": "yes", "-f": "config_file", "--config.file": "config_file",
	"--secret.file": "secret_file", "--no.config.files": "no_config_files", "-n": "node", "--node": "node", "-r": "role", "--role": "role",
	"-e": "endpoint", "--endpoint": "endpoint", "-u": "user", "--user": "user", "-p": "password", "--password": "password",
	"-t": "token", "--token": "token", "--self.cert": "self_cert", "--update.password": "update_password",
	"--dry.run": "dry_run", "--initialize": "initialize", "--useip": "useip", "--host.update": "host_update", "--force.host.update": "force_host_update",
	"--no.uuid": "no_uuid", "--delete.host": "delete_host", "--delete.api": "delete_api", "--skip.template": "skip_template",
	"--skip.host": "skip_host", "--template.separate.num": "template_separate_num", "--checknow.execute": "checknow_execute",
	"--checknow.interval": "checknow_interval", "--disable.monitoring": "disable_monitoring", "--php.worker.num": "php_worker_num",
	"-s": "store_type", "--store.type": "store_type", "-se": "store_endpoint", "--store.endpoint": "store_endpoint",
	"-sp": "store_port", "--store.port": "store_port", "-sa": "store_access", "--store.access": "store_access",
	"-sc": "store_credential", "--store.credential": "store_credential", "-sl": "store_limit", "--store.limit": "store_limit",
	"-sw": "store_interval", "--store.interval": "store_interval", "--file.store.path": "file_store_path",
	"--method": "method", "--name": "name", "--id.only": "id_only",
}

var boolFlags = map[string]bool{
	"quiet": true, "yes": true, "no_config_files": true, "self_cert": true, "update_password": true, "dry_run": true,
	"initialize": true, "useip": true, "host_update": true, "force_host_update": true, "no_uuid": true,
	"delete_host": true, "delete_api": true, "skip_template": true, "skip_host": true, "checknow_execute": true,
	"disable_monitoring": true, "id_only": true,
}

var listFlags = map[string]bool{"checknow_interval": true, "method": true, "name": true}

func parseArgs(args []string) (map[string]any, error) {
	values := map[string]any{"command": args[0]}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			return nil, ErrHelp
		}
		name, inline, hasInline := strings.Cut(arg, "=")
		key, ok := aliases[name]
		if !ok {
			return nil, fmt.Errorf("unknown option %s", name)
		}
		if boolFlags[key] {
			if hasInline {
				values[key] = inline
			} else {
				values[key] = true
			}
			continue
		}
		if hasInline {
			values[key] = inline
			continue
		}
		if listFlags[key] {
			var list []string
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				list = append(list, args[i])
			}
			if len(list) == 0 {
				return nil, fmt.Errorf("%s requires at least one value", name)
			}
			values[key] = list
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("%s requires a value", name)
		}
		i++
		values[key] = args[i]
	}
	return values, nil
}

func CloneHelp() string {
	return `Zabbix Cloning ` + Version + `

Usage:
  zc clone [options]

Core options:
  -n, --node NAME                   Zabbix node name
  -r, --role master|worker|replica  Select the execution role
  -e, --endpoint URL                Zabbix frontend URL
  -t, --token TOKEN                 API token
  -u, --user USER                   API user
  -p, --password PASSWORD           API password
  -y, --yes                         Skip confirmation
  -q, --quiet                       Hide progress output

Processing options:
  --dry.run                         Simulate changes without writing to Zabbix
  --update.password                 Update cloned user passwords
  --initialize                      Reset the target before applying settings
  --useip                           Resolve interface DNS names to IP addresses
  --host.update                     Update existing hosts
  --force.host.update               Update matching ZC_UUID hosts with renamed hosts
  --no.uuid                         Accept the legacy UUID compatibility option
  --delete.host                     Delete hosts not in the source data
  --delete.api                      Delete API-managed settings not in the source data
  --skip.template                   Skip template export and import
  --skip.host                       Skip host application
  --template.separate.num N         Split master template exports into N parts
  --checknow.execute                Run LLD rules and matching items immediately
  --checknow.interval VALUE...      Set intervals targeted by CheckNow
  --disable.monitoring              Disable monitoring for applied hosts
  --php.worker.num N                Set parallel host create/update workers

Store options:
  -s, --store.type file|redis|dydb|direct  Select the configuration store
  -se, --store.endpoint REGION|URL|HOST    Set the store endpoint
  -sp, --store.port PORT                   Set the Redis port
  -sa, --store.access VALUE                Set AWS access key or direct node name
  -sc, --store.credential VALUE            Set a store credential or direct token
  --file.store.path PATH                   Set the file-store directory

Configuration/logging:
  -f, --config.file FILE                   Load a JSON configuration file
  --secret.file FILE                       Load a JSON secret file
  --no.config.files                        Do not load configuration files
  -l, --log.level LEVEL                    Set the log level
  --log.file FILE                          Write logs to a file
`
}

func ViewHelp() string {
	return `Zabbix Cloning datastore view ` + Version + `

Usage:
  view showversions [options]
  view showdata --version UUID [--method METHOD...] [--name NAME...]

Options:
  -v, --version UUID                       Select the store version
  --method METHOD...                       Filter by configuration method
  --name NAME...                           Filter by object name
  --id.only                                Display only IDs and names
  -s, --store.type file|redis|dydb|direct  Select the configuration store
  -se, --store.endpoint REGION|URL|HOST    Set the store endpoint
  -sp, --store.port PORT                   Set the Redis port
  -sa, --store.access VALUE                Set AWS access key or direct node name
  -sc, --store.credential VALUE            Set a store credential or direct token
  --file.store.path PATH                   Set the file-store directory
  -f, --config.file FILE                   Load a JSON configuration file
  --secret.file FILE                       Load a JSON secret file
  --no.config.files                        Do not load configuration files
  -l, --log.level LEVEL                    Set the log level
  --log.file FILE                          Write logs to a file
`
}
