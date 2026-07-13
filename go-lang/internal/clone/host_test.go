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
				"master-host": {ID: "1", Name: "master-host", Data: hostData("master-host", "uuid-master")},
				"stale-host":  {ID: "2", Name: "stale-host", Data: hostData("stale-host", "uuid-stale")},
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

func hostData(name, uuid string) model.Object {
	return model.Object{
		"host": name, "name": name, "status": "ENABLED",
		"tags": []any{model.Object{"tag": uniqueTag, "value": uuid}},
	}
}
