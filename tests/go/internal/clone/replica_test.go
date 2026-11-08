package clone

import (
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func TestWorkerSkipsActionForMissingTargetHost(t *testing.T) {
	engine := &Engine{
		Config: &config.Config{Role: "worker"},
		Local:  map[string]map[string]*LocalItem{"action": {}},
		IDReplace: map[string]map[string]any{
			"host": {}, "hostgroup": {}, "template": {},
		},
	}
	data := model.Object{
		"eventsource": 0,
		"filter": model.Object{"conditions": []any{
			model.Object{"conditiontype": 1, "value": "host-on-another-worker"},
		}},
	}
	if engine.normalizeAction("test", data) {
		t.Fatal("worker accepted an action that refers to a host outside its target set")
	}
}

func TestWorkerKeepsActionForTargetHost(t *testing.T) {
	engine := &Engine{
		Config: &config.Config{Role: "worker"},
		Local:  map[string]map[string]*LocalItem{"action": {}},
		IDReplace: map[string]map[string]any{
			"host": {"target-host": "123"}, "hostgroup": {}, "template": {},
		},
	}
	condition := model.Object{"conditiontype": 1, "value": "target-host"}
	data := model.Object{"eventsource": 0, "filter": model.Object{"conditions": []any{condition}}}
	if !engine.normalizeAction("test", data) {
		t.Fatal("worker skipped an action for its target host")
	}
	if got := model.String(condition["value"]); got != "123" {
		t.Fatalf("condition host ID = %q, want 123", got)
	}
}

func TestNormalizeActionResolvesNestedUserName(t *testing.T) {
	engine := &Engine{
		Config: &config.Config{Role: "replica"},
		Local: map[string]map[string]*LocalItem{
			"action": {}, "user": {"Admin": {ID: "1", Name: "Admin"}},
		},
		IDReplace: map[string]map[string]any{"host": {}, "hostgroup": {}, "template": {}},
	}
	recipient := model.Object{"userid": "Admin"}
	data := model.Object{
		"eventsource": 0,
		"filter":      model.Object{"conditions": []any{}},
		"operations":  []any{model.Object{"opmessage_usr": []any{recipient}}},
	}
	if !engine.normalizeAction("test", data) {
		t.Fatal("replica skipped a valid action")
	}
	if got := model.String(recipient["userid"]); got != "1" {
		t.Fatalf("nested user ID = %q, want 1", got)
	}
}
