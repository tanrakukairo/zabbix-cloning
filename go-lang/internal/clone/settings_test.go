package clone

import (
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestNormalizeSettingsConfigSupportsZabbix70(t *testing.T) {
	settings, err := normalizeSettingsConfig(map[string]any{
		"default_lang":   "ja_JP",
		"login_attempts": 3,
		"severity": map[string]any{
			"2": map[string]any{"name": "Notice", "color": "abcdef"},
		},
		"timeout": map[string]any{
			"zabbix_agent":           "5s",
			"timeout_external_check": "15s",
		},
	}, zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	want := model.Object{
		"default_lang": "ja_JP", "login_attempts": 3,
		"severity_name_2": "Notice", "severity_color_2": "ABCDEF",
		"timeout_zabbix_agent": "5s", "timeout_external_check": "15s",
	}
	for key, value := range want {
		if model.String(settings[key]) != model.String(value) {
			t.Fatalf("unexpected %s: got=%v want=%v", key, settings[key], value)
		}
	}
}

func TestNormalizeSettingsConfigRejectsUnsupportedProperties(t *testing.T) {
	tests := []map[string]any{
		{"unknown": true},
		{"ha_failover_delay": "1m"},
		{"severity": "invalid"},
		{"severity": map[string]any{"6": map[string]any{"name": "Invalid"}}},
		{"timeout": map[string]any{"invalid": "3s"}},
	}
	for _, settings := range tests {
		if _, err := normalizeSettingsConfig(settings, zabbix.ParseVersion("7.0.21")); err == nil {
			t.Fatalf("expected an error for %#v", settings)
		}
	}
}

func TestNormalizeSettingsConfigRejectsNonObject(t *testing.T) {
	if _, err := normalizeSettingsConfig("invalid", zabbix.ParseVersion("7.0.21")); err == nil {
		t.Fatal("expected a settings object error")
	}
}

func TestNormalizeSettingsConfigRequiresZabbix70(t *testing.T) {
	_, err := normalizeSettingsConfig(map[string]any{"default_lang": "ja_JP"}, zabbix.ParseVersion("6.4.0"))
	if err == nil {
		t.Fatal("expected a version error")
	}
}

func TestReplaceSettingsIDs(t *testing.T) {
	engine := &Engine{IDReplace: map[string]map[string]any{
		"hostgroup": {"12": "Discovered hosts", "Discovered hosts": "112"},
		"usergroup": {"7": "Zabbix administrators", "Zabbix administrators": "107"},
	}}
	data := model.Object{"discovery_groupid": "12", "alert_usrgrpid": "7"}
	engine.replaceSettingsIDs(data, true)
	if data["discovery_groupid"] != "Discovered hosts" || data["alert_usrgrpid"] != "Zabbix administrators" {
		t.Fatalf("settings IDs were not converted to names: %#v", data)
	}
	engine.replaceSettingsIDs(data, false)
	if data["discovery_groupid"] != "112" || data["alert_usrgrpid"] != "107" {
		t.Fatalf("settings names were not converted to target IDs: %#v", data)
	}
}

func TestPrepareSettingsUpdateDefersMissingGroupReferences(t *testing.T) {
	engine := &Engine{
		Config: &config.Config{Raw: map[string]any{}},
		Version: zabbix.ParseVersion("7.0.21"),
		IDReplace: map[string]map[string]any{
			"hostgroup": {"2": "Zabbix servers", "Zabbix servers": "2"},
			"usergroup": {"7": "Zabbix administrators", "Zabbix administrators": "7"},
		},
	}
	data, err := engine.prepareSettingsUpdate(model.Object{
		"default_lang":      "en_US",
		"discovery_groupid": "Discovered hosts",
		"alert_usrgrpid":    "Zabbix administrators",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := data["discovery_groupid"]; exists {
		t.Fatalf("missing discovery group was not deferred: %#v", data)
	}
	if data["alert_usrgrpid"] != "7" {
		t.Fatalf("existing user group was not resolved: %#v", data)
	}
	if engine.deferredSettings["discovery_groupid"] != "Discovered hosts" {
		t.Fatalf("unexpected deferred settings: %#v", engine.deferredSettings)
	}

	engine.IDReplace["hostgroup"]["Discovered hosts"] = "23"
	deferred := model.CloneObject(engine.deferredSettings)
	if unresolved := engine.resolveTargetSettingsIDs(deferred); len(unresolved) != 0 {
		t.Fatalf("created discovery group was not resolved: %#v", unresolved)
	}
	if deferred["discovery_groupid"] != "23" {
		t.Fatalf("unexpected resolved discovery group ID: %#v", deferred)
	}
}
