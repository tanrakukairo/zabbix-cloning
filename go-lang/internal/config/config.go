package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

const Version = "0.3.2"

type Config struct {
	Command  string
	Node     string
	Role     string
	Endpoint string
	User     string
	Password string
	Token    string
	SelfCert bool

	Yes               bool
	Quiet             bool
	DryRun            bool
	Initialize        bool
	InitializeFull    bool
	Online            bool
	UseIP             bool
	HostUpdate        bool
	ForceHostUpdate   bool
	NoUUID            bool
	DeleteHost        bool
	DeleteAPI         bool
	SkipTemplate      bool
	SkipHost          bool
	CheckNowExecute   bool
	DisableMonitoring bool
	UpdatePassword    bool

	TargetVersion     string
	TemplateSeparate  int
	CheckNowInterval  []string
	CheckNowWait      int
	Workers           int
	Description       string
	CloningSuperAdmin bool

	StoreType       string
	StoreEndpoint   string
	StorePort       int
	StoreAccess     string
	StoreCredential string
	StoreLimit      int
	StoreInterval   int
	FileStorePath   string
	AWSRegion       string
	AWSEndpointURL  string

	ConfigFile    string
	SecretFile    string
	NoConfigFiles bool
	DirectSource  bool
	LogLevel      string
	LogFile       string
	LogName       string

	Method []string
	Name   []string
	IDOnly bool

	Raw map[string]any
}

func Parse(args []string, mode string) (*Config, error) {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return nil, ErrHelp
	}
	values, err := parseArgs(args)
	if err != nil {
		return nil, err
	}
	command := model.String(values["command"])
	if mode == "zc" && command != "master" && command != "worker" && command != "replica" {
		return nil, fmt.Errorf("command must be master, worker, or replica")
	}
	if mode == "view" && command != "showversions" && command != "showdata" {
		return nil, fmt.Errorf("command must be showversions or showdata")
	}

	environment := map[string]any{}
	loadEnvironment(environment)
	configFile := first(
		stringValue(values, "config_file", ""),
		stringValue(environment, "config_file", ""),
	)
	noConfig := boolValue(values, "no_config_files", boolValue(environment, "no_config_files", false))
	raw := map[string]any{}
	if !noConfig {
		if configFile == "" {
			configFile = defaultConfigPath()
		}
		if err := mergeJSONFile(raw, configFile, false); err != nil {
			return nil, err
		}
	}
	secretFile := resolveSecretFile(values, environment, raw, configFile, noConfig)
	if secretFile != "" {
		if err := mergeJSONFile(raw, secretFile, false); err != nil {
			return nil, err
		}
	}
	loadEnvironment(raw)
	for key, value := range values {
		if key != "command" {
			raw[key] = value
		}
	}
	if mode == "zc" {
		raw["role"] = command
	}
	if secretFile != "" {
		raw["secret_file"] = secretFile
	}

	c := fromRaw(raw)
	c.InitializeFull = boolValue(values, "initialize_full", false)
	c.Command = command
	c.ConfigFile = configFile
	c.SecretFile = secretFile
	c.NoConfigFiles = noConfig
	c.Method = listValue(values["method"])
	c.Name = listValue(values["name"])
	c.IDOnly = boolValue(values, "id_only", false)
	c.Raw = raw
	if mode == "view" {
		c.Quiet = true
		c.Role = "worker"
	}
	if c.ForceHostUpdate {
		c.HostUpdate = true
	}
	if c.Role == "master" {
		c.Initialize = false
		c.InitializeFull = false
		c.SkipHost = false
		c.TargetVersion = ""
		c.UpdatePassword = false
	}
	if c.InitializeFull {
		c.Initialize = true
		c.Yes = false
		c.Quiet = false
	}
	if c.Initialize {
		c.DeleteHost = false
		c.DeleteAPI = false
		c.SkipTemplate = false
		c.SkipHost = false
	}
	if c.StoreType == "dydb" {
		if strings.HasPrefix(c.StoreEndpoint, "http://") || strings.HasPrefix(c.StoreEndpoint, "https://") {
			c.AWSEndpointURL = c.StoreEndpoint
		} else if c.StoreEndpoint != "" {
			c.AWSRegion = c.StoreEndpoint
		}
	}
	return c, nil
}

var ErrHelp = errors.New("help requested")

func fromRaw(raw map[string]any) *Config {
	storeConnect := objectValue(raw["store_connect"])
	role := stringValue(raw, "role", "master")
	storeType := stringValue(raw, "store_type", "file")
	endpoint := stringValue(raw, "store_endpoint", "")
	access := stringValue(raw, "store_access", "")
	credential := stringValue(raw, "store_credential", "")
	switch storeType {
	case "redis":
		endpoint = first(endpoint, objectString(storeConnect, "redis_host", "localhost"))
		credential = first(credential, objectString(storeConnect, "redis_password", ""))
	case "dydb":
		access = first(access, objectString(storeConnect, "aws_account_id", ""))
		credential = first(credential, objectString(storeConnect, "aws_secret_key", ""))
	case "direct":
		endpoint = first(endpoint, objectString(storeConnect, "direct_endpoint", ""))
		access = first(access, objectString(storeConnect, "direct_node", ""))
		credential = first(credential, objectString(storeConnect, "direct_token", ""))
	}
	region := objectString(storeConnect, "aws_region", os.Getenv("AWS_DEFAULT_REGION"))
	if region == "" {
		region = "us-east-1"
	}
	return &Config{
		Node: stringValue(raw, "node", "zabbix"), Role: role,
		Endpoint: stringValue(raw, "endpoint", "http://localhost"),
		User:     stringValue(raw, "user", "Admin"), Password: stringValue(raw, "password", ""),
		Token: stringValue(raw, "token", ""), SelfCert: boolValue(raw, "self_cert", false),
		Yes: boolValue(raw, "yes", false), Quiet: boolValue(raw, "quiet", false), DryRun: boolValue(raw, "dry_run", false),
		Initialize: boolValue(raw, "initialize", false), Online: boolValue(raw, "online", false), UseIP: boolValue(raw, "useip", false),
		HostUpdate: boolValue(raw, "host_update", false), ForceHostUpdate: boolValue(raw, "force_host_update", false),
		NoUUID: boolValue(raw, "no_uuid", false), DeleteHost: boolValue(raw, "delete_host", false),
		DeleteAPI: boolValue(raw, "delete_api", false), SkipTemplate: boolValue(raw, "skip_template", false),
		SkipHost: boolValue(raw, "skip_host", false), CheckNowExecute: boolValue(raw, "checknow_execute", false),
		DisableMonitoring: boolValue(raw, "disable_monitoring", false), UpdatePassword: boolValue(raw, "update_password", false),
		TargetVersion: stringValue(raw, "version", ""), TemplateSeparate: intValue(raw, "template_separate_num", intValue(raw, "template_separate", 100)),
		CheckNowInterval: stringList(raw["checknow_interval"], []string{"1h"}), CheckNowWait: intValue(raw, "checknow_wait", 30),
		Workers:     intValue(raw, "parallel_host_apply", 4),
		Description: stringValue(raw, "description", ""), CloningSuperAdmin: boolValue(raw, "cloning_super_admin", false),
		StoreType: storeType, StoreEndpoint: endpoint,
		StorePort:       intValue(raw, "store_port", objectInt(storeConnect, "redis_port", 6379)),
		StoreAccess:     access,
		StoreCredential: credential,
		StoreLimit:      intValue(raw, "store_limit", objectInt(storeConnect, "dydb_limit", 10)),
		StoreInterval:   intValue(raw, "store_interval", objectInt(storeConnect, "dydb_wait", 2)),
		FileStorePath:   stringValue(raw, "file_store_path", os.Getenv("ZC_FILE_STORE_PATH")),
		AWSRegion:       region, AWSEndpointURL: objectString(storeConnect, "aws_endpoint_url", ""),
		LogLevel: strings.ToUpper(stringValue(raw, "log_level", "INFO")),
		LogFile:  stringValue(raw, "log_file", defaultLogPath()), LogName: stringValue(raw, "log_name", "ZabbixCloning"),
	}
}

func (c *Config) DirectMaster() *Config {
	copy := *c
	connect := objectValue(c.Raw["store_connect"])
	copy.Role = "master"
	copy.Node = first(c.StoreAccess, objectString(connect, "direct_node", ""))
	copy.Endpoint = first(c.StoreEndpoint, objectString(connect, "direct_endpoint", ""))
	copy.Token = first(c.StoreCredential, objectString(connect, "direct_token", ""))
	copy.UpdatePassword = false
	copy.SkipTemplate = false
	copy.SkipHost = false
	copy.DirectSource = true
	return &copy
}

func (c *Config) Summary() string {
	lines := []string{"[Zabbix Cloning Configurations]"}
	lines = append(lines, "  Target Node: "+c.Node, "    Role: "+c.Role, "    Zabbix Endpoint: "+c.Endpoint)
	if c.DryRun {
		lines = append(lines, "  Dry Run: ENABLED")
	}
	if c.InitializeFull {
		lines = append(lines, "  Full Initialize: ENABLED")
	} else if c.Initialize {
		lines = append(lines, "  Initialize: ENABLED")
	}
	auth := "PASSWORD"
	if c.Token != "" {
		auth = "TOKEN"
	}
	lines = append(lines, "  Authentication Method: "+auth, "  Store Type: "+c.StoreType)
	switch c.StoreType {
	case "redis":
		lines = append(lines, fmt.Sprintf("    Redis Endpoint: %s:%d", first(c.StoreEndpoint, "localhost"), c.StorePort))
	case "dydb":
		lines = append(lines, "    AWS Region: "+c.AWSRegion)
		if c.AWSEndpointURL != "" {
			lines = append(lines, "    DynamoDB Endpoint URL: "+c.AWSEndpointURL)
		}
	case "file":
		lines = append(lines, "    File Store Path: "+c.StorePath())
	}
	lines = append(lines, "  Log level: "+c.LogLevel)
	return strings.Join(lines, "\n")
}

func (c *Config) StorePath() string {
	if c.FileStorePath != "" {
		return c.FileStorePath
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), "Documents", "zc")
	}
	return "/var/lib/zabbix/zc"
}

func mergeJSONFile(target map[string]any, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var values map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	mergeValues(target, values)
	return nil
}

func mergeValues(target, values map[string]any) {
	for key, value := range values {
		current, targetIsObject := target[key].(map[string]any)
		incoming, valueIsObject := value.(map[string]any)
		if targetIsObject && valueIsObject {
			mergeValues(current, incoming)
			continue
		}
		target[key] = value
	}
}

func loadEnvironment(target map[string]any) {
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(strings.ToUpper(key), "ZC_") {
			continue
		}
		key = strings.ToLower(strings.TrimPrefix(strings.ToUpper(key), "ZC_"))
		if strings.HasPrefix(key, "store_connect_") {
			obj := objectValue(target["store_connect"])
			obj[strings.TrimPrefix(key, "store_connect_")] = value
			target["store_connect"] = obj
			continue
		}
		target[key] = value
	}
}

func resolveSecretFile(values, environment, config map[string]any, configFile string, noConfig bool) string {
	if path := stringValue(values, "secret_file", ""); path != "" {
		return path
	}
	if path := stringValue(environment, "secret_file", ""); path != "" {
		return path
	}
	if path := stringValue(config, "secret_file", ""); path != "" {
		if !filepath.IsAbs(path) && configFile != "" {
			return filepath.Join(filepath.Dir(configFile), path)
		}
		return path
	}
	if !noConfig && configFile != "" {
		return filepath.Join(filepath.Dir(configFile), "zc.secret")
	}
	return ""
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "Zabbix", "zc.conf")
	}
	return "/etc/zabbix/zc.conf"
}
func defaultLogPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), "Documents", "zc", "log", "zc.log")
	}
	return "/var/lib/zabbix/zc/log/zc.log"
}

func objectValue(value any) map[string]any {
	if obj, ok := value.(map[string]any); ok {
		return obj
	}
	return map[string]any{}
}
func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key]; ok && model.String(value) != "" {
		return model.String(value)
	}
	return fallback
}
func intValue(values map[string]any, key string, fallback int) int {
	if value, ok := values[key]; ok && model.String(value) != "" {
		n, err := strconv.Atoi(model.String(value))
		if err == nil {
			return n
		}
	}
	return fallback
}
func boolValue(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key]
	if !ok {
		return fallback
	}
	return model.Bool(value, fallback)
}
func objectString(values map[string]any, key, fallback string) string {
	return stringValue(values, key, fallback)
}
func objectInt(values map[string]any, key string, fallback int) int {
	return intValue(values, key, fallback)
}
func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringList(value any, fallback []string) []string {
	list := listValue(value)
	if len(list) == 0 {
		return fallback
	}
	return list
}
func listValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, model.String(item))
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return strings.Fields(v)
	default:
		return nil
	}
}

func Keys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
