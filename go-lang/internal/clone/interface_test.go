package clone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
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

func TestApplyHostInterfacesRunsDifferentHostsInParallel(t *testing.T) {
	var getActive, getMax, updateActive, updateMax atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		method := model.String(body["method"])
		result := any(model.Object{})
		switch method {
		case "hostinterface.get":
			active := getActive.Add(1)
			setAtomicMaximum(&getMax, active)
			time.Sleep(75 * time.Millisecond)
			getActive.Add(-1)
			params := objectMap(body["params"])
			hostID := model.String(params["hostids"])
			result = []any{model.Object{
				"interfaceid": fmt.Sprintf("if-%s", hostID), "type": "1", "main": "1",
				"useip": "1", "ip": "10.0.0.1", "dns": "", "port": "10050", "details": model.Object{},
			}}
		case "hostinterface.update":
			active := updateActive.Add(1)
			setAtomicMaximum(&updateMax, active)
			time.Sleep(75 * time.Millisecond)
			updateActive.Add(-1)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "result": result, "id": body["id"]})
	}))
	defer server.Close()

	api, err := zabbix.New(server.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logx.New("test", "CRITICAL", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	engine := &Engine{API: api, Log: logger, Config: &config.Config{Workers: 2, Quiet: true}}
	hosts := []hostPlan{
		{Name: "host-a", Function: "update", ID: "10", Interfaces: []model.Object{{"type": 1, "main": 1, "useip": 1, "ip": "10.0.0.1", "dns": "", "port": "10051"}}},
		{Name: "host-b", Function: "update", ID: "20", Interfaces: []model.Object{{"type": 1, "main": 1, "useip": 1, "ip": "10.0.0.1", "dns": "", "port": "10051"}}},
	}
	engine.applyHostInterfaces(context.Background(), hosts, map[string]bool{})
	if getMax.Load() < 2 {
		t.Fatalf("hostinterface.get maximum concurrency = %d, want at least 2", getMax.Load())
	}
	if updateMax.Load() < 2 {
		t.Fatalf("hostinterface.update maximum concurrency = %d, want at least 2", updateMax.Load())
	}
}

func setAtomicMaximum(maximum *atomic.Int32, value int32) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}
