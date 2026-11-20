package clone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestCEPVersionParameters(t *testing.T) {
	for _, version := range []string{"7.0.0", "7.2.0", "7.4.0", "8.0.0"} {
		v := zabbix.ParseVersion(version)
		p, err := NewParameters(v)
		if err != nil {
			t.Fatal(err)
		}
		spec, exists := p.Methods["ceprule"]
		if exists != v.AtLeast(8, 0) || contains(p.Post, "ceprule") != exists {
			t.Fatalf("%s: invalid CEP version selection", version)
		}
		if exists && (spec.ID != "cep_ruleid" || spec.Options["selectWindow"] != "extend" || spec.Options["selectOperations"] != "extend" || spec.Options["selectFilter"] != "extend") {
			t.Fatalf("incomplete CEP retrieval: %#v", spec)
		}
		if exists {
			e := &Engine{Params: p}
			for _, order := range [][]string{p.Post, e.fullInitializeMethods()} {
				if slices.Index(order, "ceprule") != slices.Index(order, "correlation")+1 {
					t.Fatalf("CEP must follow correlation: %v", order)
				}
			}
		}
	}
}

func cepRuleFixture() model.Object {
	return model.Object{
		"name": "Group events", "sortorder": "7", "window_type": "4", "status": "0", "stop": "1", "error": "runtime error",
		"filter": model.Object{"evaltype": "3", "formula": "A and B", "eval_formula": "A and B", "conditions": []any{
			model.Object{"type": "36", "operator": "2", "host_name": "app", "formulaid": "A"},
			model.Object{"type": "37", "operator": "0", "host_group_name": "Servers", "formulaid": "B"},
		}},
		"window": model.Object{"duration": "10m", "capacity": "{$CAPACITY}", "group_by_tags": "1", "tags": []any{"service"}, "script": "return true;"},
		"operations": []any{model.Object{"sortorder": "1", "execute_when": "4", "type": "7", "suppress_duration": "6m", "scope": "2",
			"filter":       model.Object{"evaltype": "0", "formula": "", "eval_formula": "A", "conditions": []any{model.Object{"type": "33", "operator": "10", "formulaid": "A"}}},
			"scope_filter": model.Object{"evaltype": "3", "formula": "A", "eval_formula": "A", "conditions": []any{model.Object{"type": "26", "operator": "0", "tag": "service", "tag_value": "api", "formulaid": "A"}}},
		}},
	}
}

func TestCEPStoreAndApply(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "update"}[update], func(t *testing.T) {
			var payload model.Object
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Method string
					Params json.RawMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				calls = append(calls, req.Method)
				if req.Method != "ceprule.delete" {
					payload = nil
					_ = json.Unmarshal(req.Params, &payload)
				}
				var result any = model.Object{"cep_ruleids": []any{"99"}}
				if req.Method == "ceprule.get" {
					if payload["selectFilter"] != "extend" || payload["selectWindow"] != "extend" || payload["selectOperations"] != "extend" {
						t.Error("missing CEP selectors")
					}
					fixture := cepRuleFixture()
					fixture["cep_ruleid"] = "42"
					result = []any{fixture}
				}
				if req.Method == "configuration.export" {
					result = `{"zabbix_export":{"version":"8.0"}}`
				}
				_ = json.NewEncoder(w).Encode(model.Object{"jsonrpc": "2.0", "id": 1, "result": result})
			}))
			defer server.Close()
			api, err := zabbix.New(server.URL, false)
			if err != nil {
				t.Fatal(err)
			}
			logger, err := logx.New("test", "INFO", "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer logger.Close()
			p, _ := NewParameters(zabbix.ParseVersion("8.0.0"))
			p.ConfigExport = map[string]string{}
			p.Post = []string{"ceprule"}
			p.Methods = map[string]MethodSpec{"ceprule": p.Methods["ceprule"]}
			e := &Engine{API: api, Params: p, Log: logger, Version: zabbix.ParseVersion("8.0.0"), Config: &config.Config{Role: "master", DryRun: true, DeleteAPI: true},
				Local: map[string]map[string]*LocalItem{"ceprule": {"Group events": {ID: "42", Name: "Group events", Data: cepRuleFixture()}}}}
			if err := e.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := e.CreateMasterData(context.Background()); err != nil {
				t.Fatal(err)
			}
			data := e.Dataset["ceprule"][0].Data
			if _, ok := data["error"]; ok {
				t.Fatal("runtime error stored")
			}
			if _, ok := data["cep_ruleid"]; ok {
				t.Fatal("source ID stored")
			}
			calls = nil
			e.Local["ceprule"] = map[string]*LocalItem{"obsolete": {ID: "50", Name: "obsolete"}}
			if update {
				e.Local["ceprule"]["Group events"] = &LocalItem{ID: "99", Name: "Group events", Data: model.Object{}}
			}
			e.enableDryRunVirtualState()
			// Keep virtual refresh, but send mutations to the HTTP mock.
			if err := e.ApplyAPISection(context.Background(), "POST"); err != nil {
				t.Fatal(err)
			}
			want := model.CloneObject(data)
			method := "ceprule.create"
			if update {
				method = "ceprule.update"
				want["cep_ruleid"] = "99"
			}
			if !reflect.DeepEqual(marshalObject(payload), marshalObject(want)) {
				t.Fatalf("payload=%#v, want=%#v", payload, want)
			}
			if !reflect.DeepEqual(calls, []string{method, "ceprule.delete"}) {
				t.Fatalf("calls=%v", calls)
			}
			filter := objectMap(objects(payload["operations"])[0]["filter"])
			if filter["eval_formula"] != nil || objects(filter["conditions"])[0]["formulaid"] != nil {
				t.Fatal("generated filter fields sent")
			}
			if objectMap(payload["filter"])["formula"] != "A and B" {
				t.Fatal("custom formula lost")
			}
			if objectMap(objects(payload["operations"])[0]["scope_filter"])["formula"] != "A" {
				t.Fatal("scope formula lost")
			}
			for _, master := range []float64{0, 7, 7.2, 7.4} {
				e.NewVersion.MasterVersion = master
				calls = nil
				if err := e.ApplyAPISection(context.Background(), "POST"); err != nil {
					t.Fatal(err)
				}
				if len(calls) != 0 {
					t.Fatalf("old source %.1f changed CEP: %v", master, calls)
				}
			}
		})
	}
}
