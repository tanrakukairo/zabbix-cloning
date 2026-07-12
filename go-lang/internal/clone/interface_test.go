package clone

import (
	"testing"

	"github.com/t2-f/zabbix-cloning/internal/config"
	"github.com/t2-f/zabbix-cloning/internal/model"
	"github.com/t2-f/zabbix-cloning/internal/zabbix"
)

func TestSelectMainInterfacePriority(t *testing.T) {
	interfaces := []model.Object{
		{"type": 3, "main": 0},
		{"type": 4, "main": 0},
		{"type": 2, "main": 0},
	}
	selectMainInterface(interfaces)
	if model.Int(interfaces[2]["main"]) != 1 {
		t.Fatalf("SNMP interface was not selected: %#v", interfaces)
	}
}

func TestBuildInterfacePlansKeepsMatchesAndDeletesOnlyMissing(t *testing.T) {
	existing := []model.Object{
		{"interfaceid": "1", "type": "1", "main": "1", "useip": "1", "ip": "10.0.0.1", "dns": "", "port": "10050"},
		{"interfaceid": "2", "type": "2", "main": "1", "useip": "1", "ip": "10.0.0.2", "dns": "", "port": "161"},
	}
	updates := []model.Object{
		{"type": 1, "main": 1, "useip": 1, "ip": "10.0.0.1", "dns": "", "port": "10050"},
	}
	plans, deletes := buildInterfacePlans(existing, updates)
	if len(plans) != 1 || plans[0].Function != "skip" {
		t.Fatalf("unexpected plans: %#v", plans)
	}
	if len(deletes) != 1 || model.String(deletes[0]["interfaceid"]) != "2" {
		t.Fatalf("unexpected deletes: %#v", deletes)
	}
}

func TestObjectValuesRoundTrip(t *testing.T) {
	want := []model.Object{{"type": 1, "main": 1}}
	got := objects(objectValues(want))
	if len(got) != 1 || model.Int(got[0]["type"]) != 1 {
		t.Fatalf("model.Object was lost in conversion: %#v", got)
	}
}

func TestNormalizeInterfaceDefaultsToAgent(t *testing.T) {
	engine := &Engine{Config: &config.Config{}, Version: zabbix.ParseVersion("7.0.21")}
	value := model.Object{"dns": "example.local", "ip": "", "useip": "NO", "interface_ref": "if1"}
	engine.normalizeInterface(value)
	if model.Int(value["type"]) != 1 || model.Int(value["main"]) != 1 || model.String(value["port"]) != "10050" {
		t.Fatalf("unexpected defaults: %#v", value)
	}
}
