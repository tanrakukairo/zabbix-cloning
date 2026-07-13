package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotSeparatedOptions(t *testing.T) {
	cfg, err := Parse([]string{
		"replica", "--no.config.files",
		"--node", "monitor", "--store.type", "dydb",
		"--store.endpoint", "http://localhost:4566",
		"--skip.host", "--delete.api",
	}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != "replica" || cfg.Node != "monitor" {
		t.Fatalf("unexpected target: role=%s node=%s", cfg.Role, cfg.Node)
	}
	if !cfg.SkipHost || !cfg.DeleteAPI {
		t.Fatalf("boolean options were not parsed: %#v", cfg)
	}
	if cfg.AWSEndpointURL != "http://localhost:4566" {
		t.Fatalf("unexpected DynamoDB URL: %s", cfg.AWSEndpointURL)
	}
}

func TestRoleComesFromCommand(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "zc.conf")
	if err := os.WriteFile(configFile, []byte(`{"role":"master"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse([]string{"worker", "--config.file", configFile}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != "worker" {
		t.Fatalf("positional role was not applied: %s", cfg.Role)
	}
}

func TestDefaultLogName(t *testing.T) {
	cfg, err := Parse([]string{"master", "--no.config.files"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogName != "ZabbixCloning" {
		t.Fatalf("default log name = %q, want ZabbixCloning", cfg.LogName)
	}
}

func TestLegacyCloneAndRoleOptionAreRejected(t *testing.T) {
	if _, err := Parse([]string{"clone", "--no.config.files"}, "zc"); err == nil {
		t.Fatal("legacy clone command must be rejected")
	}
	if _, err := Parse([]string{"master", "--no.config.files", "--role", "replica"}, "zc"); err == nil {
		t.Fatal("legacy --role option must be rejected")
	}
}

func TestForceHostUpdateEnablesHostUpdate(t *testing.T) {
	cfg, err := Parse([]string{"replica", "--no.config.files", "--force.host.update"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ForceHostUpdate || !cfg.HostUpdate {
		t.Fatal("force host update must enable host update")
	}
}

func TestDryRunOption(t *testing.T) {
	cfg, err := Parse([]string{"replica", "--no.config.files", "--dry.run"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DryRun {
		t.Fatal("dry run option was not enabled")
	}
}

func TestOnlineOption(t *testing.T) {
	cfg, err := Parse([]string{"replica", "--no.config.files", "--online"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Online {
		t.Fatal("online option was not enabled")
	}
}

func TestParallelHostApplyOption(t *testing.T) {
	cfg, err := Parse([]string{"replica", "--no.config.files", "--parallel.host.apply", "8"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workers != 8 {
		t.Fatalf("parallel host apply = %d, want 8", cfg.Workers)
	}
	if _, err := Parse([]string{"replica", "--no.config.files", "--php.worker.num", "8"}, "zc"); err == nil {
		t.Fatal("legacy --php.worker.num option must be rejected")
	}
}

func TestInitializeFullForcesInteractiveFullInitialization(t *testing.T) {
	cfg, err := Parse([]string{
		"replica", "--no.config.files", "--initialize.full", "--yes", "--quiet", "--delete.host", "--delete.api", "--skip.template", "--skip.host",
	}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Initialize || !cfg.InitializeFull {
		t.Fatalf("full initialization was not enabled: %#v", cfg)
	}
	if cfg.Yes || cfg.Quiet {
		t.Fatalf("full initialization must force interactive output: %#v", cfg)
	}
	if cfg.DeleteHost || cfg.DeleteAPI || cfg.SkipTemplate || cfg.SkipHost {
		t.Fatalf("full initialization flags were not normalized: %#v", cfg)
	}
}

func TestInitializeFullIgnoresConfigurationAndEnvironment(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "zc.conf")
	if err := os.WriteFile(configFile, []byte(`{"initialize_full":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZC_INITIALIZE_FULL", "true")
	cfg, err := Parse([]string{"replica", "--config.file", configFile}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InitializeFull || cfg.Initialize {
		t.Fatalf("full initialization was enabled outside the CLI: %#v", cfg)
	}
}

func TestCheckNowIntervalAcceptsMultipleValues(t *testing.T) {
	cfg, err := Parse([]string{
		"replica", "--no.config.files", "--checknow.interval", "1h", "CHECKNOW_INTERVAL", "{$ALREADY_A_MACRO}",
	}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1h", "CHECKNOW_INTERVAL", "{$ALREADY_A_MACRO}"}
	if len(cfg.CheckNowInterval) != len(want) {
		t.Fatalf("unexpected intervals: %#v", cfg.CheckNowInterval)
	}
	for i, value := range want {
		if cfg.CheckNowInterval[i] != value {
			t.Fatalf("unexpected intervals: %#v", cfg.CheckNowInterval)
		}
	}
}

func TestEnvironmentOverridesConfigFile(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "zc.conf")
	if err := os.WriteFile(configFile, []byte(`{"node":"from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZC_NODE", "from-environment")

	cfg, err := Parse([]string{"master", "--config.file", configFile}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node != "from-environment" {
		t.Fatalf("environment must override configuration file: %s", cfg.Node)
	}

	cfg, err = Parse([]string{"master", "--config.file", configFile, "--node", "from-cli"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node != "from-cli" {
		t.Fatalf("command line must override environment: %s", cfg.Node)
	}
}

func TestSecretFileOverridesConfigFile(t *testing.T) {
	directory := t.TempDir()
	configFile := filepath.Join(directory, "zc.conf")
	secretFile := filepath.Join(directory, "zc.secret")
	if err := os.WriteFile(configFile, []byte(`{
  "node":"from-file",
  "token":"from-file",
  "secret_file":"zc.secret",
  "store_connect":{"redis_host":"from-file"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretFile, []byte(`{
  "token":"from-secret",
  "password":"secret-password",
  "store_connect":{"redis_password":"secret-redis-password"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Parse([]string{"master", "--config.file", configFile, "--store.type", "redis"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecretFile != secretFile {
		t.Fatalf("unexpected secret file: %s", cfg.SecretFile)
	}
	if cfg.Token != "from-secret" || cfg.Password != "secret-password" {
		t.Fatalf("secret values were not loaded: %#v", cfg)
	}
	if cfg.StoreEndpoint != "from-file" || cfg.StoreCredential != "secret-redis-password" {
		t.Fatalf("store configuration was not merged: %#v", cfg)
	}
}

func TestSecretFileSelectionPriority(t *testing.T) {
	directory := t.TempDir()
	configFile := filepath.Join(directory, "zc.conf")
	configSecret := filepath.Join(directory, "config.secret")
	environmentSecret := filepath.Join(directory, "environment.secret")
	cliSecret := filepath.Join(directory, "cli.secret")
	if err := os.WriteFile(configFile, []byte(`{"secret_file":"config.secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, token := range map[string]string{
		configSecret:      "from-config-secret",
		environmentSecret: "from-environment-secret",
		cliSecret:         "from-cli-secret",
	} {
		if err := os.WriteFile(path, []byte(`{"token":"`+token+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZC_SECRET_FILE", environmentSecret)

	cfg, err := Parse([]string{"master", "--config.file", configFile}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecretFile != environmentSecret || cfg.Token != "from-environment-secret" {
		t.Fatalf("environment secret file was not selected: %#v", cfg)
	}

	cfg, err = Parse([]string{"master", "--config.file", configFile, "--secret.file", cliSecret}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecretFile != cliSecret || cfg.Token != "from-cli-secret" {
		t.Fatalf("command-line secret file was not selected: %#v", cfg)
	}
}

func TestCredentialsOverrideSecretFile(t *testing.T) {
	directory := t.TempDir()
	secretFile := filepath.Join(directory, "zc.secret")
	if err := os.WriteFile(secretFile, []byte(`{"token":"from-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZC_TOKEN", "from-environment")

	cfg, err := Parse([]string{"master", "--no.config.files", "--secret.file", secretFile}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "from-environment" {
		t.Fatalf("environment must override secret file: %s", cfg.Token)
	}

	cfg, err = Parse([]string{"master", "--no.config.files", "--secret.file", secretFile, "--token", "from-cli"}, "zc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "from-cli" {
		t.Fatalf("command line must override secret file: %s", cfg.Token)
	}
}
