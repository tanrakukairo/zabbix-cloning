package clone

import (
	"context"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestReplicaPreservesMasterHostStatus(t *testing.T) {
	engine := &Engine{Config: &config.Config{Role: "replica"}}
	tests := []struct {
		status any
		want   int
	}{
		{status: "ENABLED", want: 0},
		{status: "DISABLED", want: 1},
		{status: "0", want: 0},
		{status: "1", want: 1},
	}
	for _, test := range tests {
		host := model.Object{"status": test.status}
		if !engine.hostIsTarget(host) {
			t.Fatalf("replica rejected host with status %v", test.status)
		}
		engine.normalizeHost(host)
		if got := model.Int(host["status"]); got != test.want {
			t.Fatalf("status %v became %d, want %d", test.status, got, test.want)
		}
	}
}

func TestWorkerEnablesTargetHost(t *testing.T) {
	engine := &Engine{Config: &config.Config{Role: "worker", Node: "worker-a"}}
	host := model.Object{
		"status": "DISABLED",
		"tags":   []any{model.Object{"tag": "ZC_WORKER", "value": "worker-a"}},
	}
	if !engine.hostIsTarget(host) {
		t.Fatal("worker rejected its target host")
	}
	engine.normalizeHost(host)
	if got := model.Int(host["status"]); got != 0 {
		t.Fatalf("worker target status = %d, want enabled status 0", got)
	}
}

func TestDisableMonitoringOverridesReplicaStatus(t *testing.T) {
	engine := &Engine{Config: &config.Config{Role: "replica", DisableMonitoring: true}}
	host := model.Object{"status": "ENABLED"}
	if !engine.hostIsTarget(host) {
		t.Fatal("replica rejected host")
	}
	engine.normalizeHost(host)
	if got := model.Int(host["status"]); got != 1 {
		t.Fatalf("disabled monitoring status = %d, want disabled status 1", got)
	}
}

func TestDeleteHostKeepsDesiredExistingHostWithoutHostUpdate(t *testing.T) {
	api, err := zabbix.New("http://127.0.0.1:1", false)
	if err != nil {
		t.Fatal(err)
	}
	api.SetDryRun(true)
	logger, err := logx.New("test", "INFO", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	engine := &Engine{
		API: api, Log: logger,
		Config: &config.Config{Role: "replica", DeleteHost: true, Quiet: true, Workers: 1},
		Local: map[string]map[string]*LocalItem{
			"host": {
				"master-host":    {ID: "1", Name: "master-host", Data: hostData("master-host", "uuid-master")},
				"stale-host":     {ID: "2", Name: "stale-host", Data: hostData("stale-host", "uuid-stale")},
				"discovered-lld": {ID: "3", Name: "discovered-lld", Data: model.Object{"host": "discovered-lld", "flags": 4}},
			},
		},
		Dataset: model.Dataset{
			"host": {{Name: "master-host", Data: hostData("master-host", "uuid-master")}},
		},
	}
	engine.enableDryRunVirtualState()
	if err := engine.ApplyHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.Local["host"]["master-host"] == nil {
		t.Fatal("host present in master data was deleted")
	}
	if engine.Local["host"]["stale-host"] != nil {
		t.Fatal("host absent from master data was retained")
	}
	if engine.Local["host"]["discovered-lld"] == nil {
		t.Fatal("LLD-discovered host with flags=4 was deleted")
	}
	if got := api.DryRunMethods()["host.delete"]; got != 1 {
		t.Fatalf("host.delete calls = %d, want 1", got)
	}
}

func TestHostRetentionKeepsRenamedHostByUUID(t *testing.T) {
	local := &LocalItem{ID: "10", Name: "old-name", Data: hostData("old-name", "same-uuid")}
	retention := hostRetention{Names: map[string]bool{}, UUIDs: map[string]bool{}, IDs: map[string]bool{}}
	retention.add("new-name", "same-uuid", local)
	if !retention.keeps("old-name", local) {
		t.Fatal("renamed host with matching UUID was not retained")
	}
}

func TestExcludeLLDDiscoveredHostsFromMaster(t *testing.T) {
	engine := &Engine{
		Local: map[string]map[string]*LocalItem{
			"host": {
				"regular":        {ID: "1", Name: "regular", Data: model.Object{"flags": 0}},
				"discovered-lld": {ID: "2", Name: "discovered-lld", Data: model.Object{"flags": 4}},
			},
		},
	}
	if excluded := engine.excludeLLDDiscoveredHostsFromMaster(); excluded != 1 {
		t.Fatalf("excluded hosts = %d, want 1", excluded)
	}
	if engine.Local["host"]["regular"] == nil {
		t.Fatal("regular host was excluded from master data")
	}
	if engine.Local["host"]["discovered-lld"] != nil {
		t.Fatal("LLD-discovered host remained in master data source")
	}
}

func TestEnsureHostUUIDsSkipsLLDDiscoveredHosts(t *testing.T) {
	api, err := zabbix.New("http://127.0.0.1:1", false)
	if err != nil {
		t.Fatal(err)
	}
	api.SetDryRun(true)
	logger, err := logx.New("test", "INFO", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	engine := &Engine{
		API: api, Log: logger,
		Local: map[string]map[string]*LocalItem{"host": {
			"regular":        {ID: "1", Name: "regular", Data: hostData("regular", "existing-uuid")},
			"discovered-lld": {ID: "2", Name: "discovered-lld", Data: model.Object{"flags": 4}},
		}},
	}
	if err := engine.ensureHostUUIDs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := api.DryRunMethods()["host.update"]; got != 0 {
		t.Fatalf("host.update calls = %d, want 0", got)
	}
}

func hostData(name, uuid string) model.Object {
	return model.Object{
		"host": name, "name": name, "status": "ENABLED",
		"tags": []any{model.Object{"tag": uniqueTag, "value": uuid}},
	}
}

func TestNormalizeSNMPv3PreservesSecurity(t *testing.T) {
	for _, version := range []any{"SNMP_V3", "SNMPV3", "3", 3} {
		t.Run(model.String(version), func(t *testing.T) {
			e := &Engine{Config: &config.Config{}}
			details := model.Object{
				"version": version, "securityname": "monitor", "securitylevel": "AUTHPRIV",
				"authprotocol": "SHA256", "authpassphrase": "{$SNMP_AUTH}",
				"privprotocol": "AES256", "privpassphrase": "{$SNMP_PRIV}",
				"contextname": "network", "max_repetitions": "20",
			}
			data := model.Object{"type": "SNMP", "details": details, "bulk": "NO"}
			e.normalizeInterface(data)
			got := objectMap(data["details"])
			for key, want := range map[string]int{"version": 3, "securitylevel": 2, "authprotocol": 3, "privprotocol": 3, "bulk": 0} {
				if got[key] == nil || model.Int(got[key]) != want {
					t.Errorf("%s = %v, want %d", key, got[key], want)
				}
			}
			for _, key := range []string{"securityname", "authpassphrase", "privpassphrase", "contextname", "max_repetitions"} {
				if got[key] != details[key] {
					t.Errorf("%s was lost", key)
				}
			}
			if _, ok := got["community"]; ok {
				t.Error("SNMPv3 has a community")
			}
			if _, ok := data["bulk"]; ok {
				t.Error("bulk was not moved into details")
			}
			if details["securitylevel"] != "AUTHPRIV" {
				t.Error("input details were mutated")
			}
		})
	}
}

func TestNormalizeSNMPVersionsAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int
	}{
		{"SNMPV1", 1}, {"1", 1}, {1, 1}, {"SNMP_V2C", 2}, {"SNMPV2", 2}, {2, 2}, {nil, 2},
	} {
		e := &Engine{Config: &config.Config{}}
		data := model.Object{"type": "SNMP", "details": model.Object{"version": tc.value, "community": "{$CUSTOM_COMMUNITY}", "bulk": "YES"}}
		e.normalizeInterface(data)
		details := objectMap(data["details"])
		if model.Int(details["version"]) != tc.want || details["community"] != "{$CUSTOM_COMMUNITY}" || model.Int(details["bulk"]) != 1 {
			t.Fatalf("unexpected details: %#v", details)
		}
	}
	e := &Engine{Config: &config.Config{}}
	data := model.Object{"type": "SNMP"}
	e.normalizeInterface(data)
	if objectMap(data["details"])["community"] != "{$SNMP_COMMUNITY}" {
		t.Fatal("default community was lost")
	}
}

func TestNormalizeSNMPNumericSecurity(t *testing.T) {
	e := &Engine{Config: &config.Config{}}
	data := model.Object{"type": 2, "details": model.Object{"version": 3, "securitylevel": "2", "authprotocol": "5", "privprotocol": 5, "securityname": "monitor"}}
	e.normalizeInterface(data)
	details := objectMap(data["details"])
	if model.Int(details["securitylevel"]) != 2 || model.Int(details["authprotocol"]) != 5 || model.Int(details["privprotocol"]) != 5 {
		t.Fatal("numeric protocols changed")
	}
}
