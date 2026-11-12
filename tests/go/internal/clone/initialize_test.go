package clone

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestInitializeFullPreservesDashboardsForOlderSourceData(t *testing.T) {
	for _, master := range []float64{0, 7.0, 7.2, 7.4, 8.0} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("source=%.1f/dryRun=%t", master, dryRun), func(t *testing.T) {
				calls := map[string]int{}
				dashboardDeleted := false
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct{ Method string }
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
					}
					calls[req.Method]++
					var result any = model.Object{}
					switch req.Method {
					case "dashboard.get":
						result = []any{}
						if !dashboardDeleted {
							result = []any{model.Object{"dashboardid": "42", "name": "Overview"}}
						}
					case "action.get":
						result = []any{model.Object{"actionid": "43", "name": "Notify"}}
					case "dashboard.delete":
						dashboardDeleted = true
					}
					_ = json.NewEncoder(w).Encode(model.Object{"jsonrpc": "2.0", "id": 1, "result": result})
				}))
				defer server.Close()
				api, err := zabbix.New(server.URL, false)
				if err != nil {
					t.Fatal(err)
				}
				api.SetDryRun(dryRun)
				logger, err := logx.New("test", "INFO", "", true)
				if err != nil {
					t.Fatal(err)
				}
				defer logger.Close()
				e := &Engine{API: api, Log: logger, Version: zabbix.ParseVersion("8.0.0"),
					Config: &config.Config{DryRun: dryRun}, NewVersion: model.Version{MasterVersion: master},
					Params: &Parameters{Methods: map[string]MethodSpec{
						"dashboard": {ID: "dashboardid", Name: "name"}, "action": {ID: "actionid", Name: "name"},
					}},
					Local: map[string]map[string]*LocalItem{
						"dashboard": {"Overview": {ID: "42", Name: "Overview", Data: model.Object{"name": "Overview"}}},
						"action":    {"Notify": {ID: "43", Name: "Notify", Data: model.Object{"name": "Notify"}}},
					},
				}
				if dryRun {
					e.enableDryRunVirtualState()
				}
				if err := e.initializeFull(context.Background()); err != nil {
					t.Fatal(err)
				}
				if dryRun {
					calls = api.DryRunMethods()
				}
				wantDeletes := 0
				if master >= 8 {
					wantDeletes = 1
				}
				if calls["dashboard.delete"] != wantDeletes || calls["action.delete"] != 1 {
					t.Fatalf("unexpected deletion calls: %v", calls)
				}
				if preserved := e.Local["dashboard"]["Overview"] != nil; preserved != (master < 8) {
					t.Fatalf("dashboard preserved = %t", preserved)
				}
			})
		}
	}
}

func TestInitializeFullDeletesAllUnprotectedIDObjects(t *testing.T) {
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
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	local := map[string]map[string]*LocalItem{}
	for method, spec := range parameters.Methods {
		local[method] = map[string]*LocalItem{}
		if spec.ID != "" {
			name := "existing-" + method
			local[method][name] = &LocalItem{ID: "100", Name: name, Data: model.Object{"name": name}}
		}
	}
	local["user"][superUser] = &LocalItem{ID: "1", Name: superUser, Data: model.Object{"username": superUser}}
	local["user"]["guest"] = &LocalItem{ID: "3", Name: "guest", Data: model.Object{
		"username": "guest", "usrgrps": []any{model.Object{"usrgrpid": "12", "name": "Guests"}},
	}}
	local["user"]["Administrator group member"] = &LocalItem{ID: "4", Name: "Administrator group member", Data: model.Object{
		"username": "Administrator group member", "usrgrps": []any{model.Object{"usrgrpid": adminGroupID, "name": "Zabbix administrators"}},
	}}
	local["user"]["Internal group member"] = &LocalItem{ID: "5", Name: "Internal group member", Data: model.Object{
		"username": "Internal group member", "usrgrps": []any{model.Object{"usrgrpid": internalGroupID, "name": "Internal"}},
	}}
	local["user"]["API operator"] = &LocalItem{ID: "2", Name: "API operator", Data: model.Object{"username": "API operator"}}
	local["usergroup"]["Zabbix administrators"] = &LocalItem{ID: adminGroupID, Name: "Zabbix administrators", Data: model.Object{"name": "Zabbix administrators"}}
	local["usergroup"]["Internal"] = &LocalItem{ID: internalGroupID, Name: "Internal", Data: model.Object{"name": "Internal"}}
	local["role"]["Super admin role"] = &LocalItem{ID: "3", Name: "Super admin role", Data: model.Object{"readonly": "1"}}
	local["usermacro"][versionMacro] = &LocalItem{ID: "9", Name: versionMacro, Data: model.Object{"macro": versionMacro}}
	local["hostgroup"]["Discovered hosts"] = &LocalItem{ID: "23", Name: "Discovered hosts", Data: model.Object{"name": "Discovered hosts"}}
	local["settings"]["discovery_groupid"] = &LocalItem{
		Name: "discovery_groupid", Data: model.Object{"discovery_groupid": "23"},
	}
	for i := 1; i <= 20; i++ {
		name := fmt.Sprintf("template-%d", i)
		local["template"][name] = &LocalItem{ID: fmt.Sprint(i), Name: name}
	}

	engine := &Engine{
		API: api, Log: logger, Params: parameters, Local: local,
		Config:  &config.Config{Role: "replica", User: "API operator", DryRun: true, Initialize: true, InitializeFull: true},
		Version: zabbix.ParseVersion("7.0.21"),
	}
	engine.enableDryRunVirtualState()
	if err := engine.initializeFull(context.Background()); err != nil {
		t.Fatal(err)
	}
	for method, spec := range parameters.Methods {
		if spec.ID != "" && engine.Local[method]["existing-"+method] != nil {
			t.Fatalf("full initialization did not delete %s", method)
		}
	}
	for method, name := range map[string]string{
		"user": superUser, "role": "Super admin role", "usermacro": versionMacro, "hostgroup": "Discovered hosts",
	} {
		if engine.Local[method][name] == nil {
			t.Fatalf("protected %s %s was deleted", method, name)
		}
	}
	if engine.Local["user"]["API operator"] == nil {
		t.Fatal("API execution user was deleted")
	}
	if engine.Local["user"]["guest"] == nil {
		t.Fatal("guest user was deleted")
	}
	for _, name := range []string{"Administrator group member", "Internal group member"} {
		if engine.Local["user"][name] == nil {
			t.Fatalf("user in protected group was deleted: %s", name)
		}
	}
	for _, name := range []string{"Zabbix administrators", "Internal"} {
		if engine.Local["usergroup"][name] == nil {
			t.Fatalf("protected user group was deleted: %s", name)
		}
	}
	counts := api.DryRunMethods()
	if counts["template.delete"] != 3 || len(engine.Local["template"]) != 0 {
		t.Fatalf("expected all 21 templates deleted in 3 batches, got %d calls with %d templates left",
			counts["template.delete"], len(engine.Local["template"]))
	}
	if counts["authentication.update"] != 1 {
		t.Fatalf("authentication reset was not planned: %#v", counts)
	}
	if counts["usermacro.deleteglobal"] != 1 {
		t.Fatalf("global macro deletion was not planned: %#v", counts)
	}
}

func TestConfirmExecutionCanBeCalledTwice(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("y\nN\n"))
	if !confirmExecution(reader, "") {
		t.Fatal("first confirmation was rejected")
	}
	if confirmExecution(reader, "") {
		t.Fatal("second confirmation was accepted")
	}
}

func TestFullInitializeUserGetIncludesGroupAccess(t *testing.T) {
	options := fullInitializeGetOptions("user", MethodSpec{ID: "userid", Name: "username"})
	fields := toAnyList(options["selectUsrgrps"])
	if len(fields) != 2 || model.String(fields[0]) != "usrgrpid" || model.String(fields[1]) != "name" {
		t.Fatalf("full initialize user groups do not include usrgrpid: %#v", options)
	}
}

func TestInitializeSkipsOnlyRoleDeleteFailure(t *testing.T) {
	roleDeleteCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		if body.Method == "role.delete" {
			roleDeleteCalls++
			if len(body.Params) == 1 && body.Params[0] == "5" {
				_ = json.NewEncoder(writer).Encode(map[string]any{
					"jsonrpc": "2.0", "result": map[string]any{"roleids": []string{"5"}}, "id": 1,
				})
				return
			}
		}
		http.Error(writer, "delete rejected", http.StatusInternalServerError)
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

	engine := &Engine{
		API: api,
		Log: logger,
		Params: &Parameters{Methods: map[string]MethodSpec{
			"role":   {ID: "roleid", Name: "name"},
			"action": {ID: "actionid", Name: "name"},
		}},
		Local: map[string]map[string]*LocalItem{
			"role": {
				"Guest role":  {ID: "4", Name: "Guest role", Data: model.Object{"name": "Guest role"}},
				"Custom role": {ID: "5", Name: "Custom role", Data: model.Object{"name": "Custom role"}},
			},
			"action": {
				"Test action": {ID: "10", Name: "Test action", Data: model.Object{"name": "Test action"}},
			},
		},
		Config: &config.Config{},
	}

	if err := engine.deleteAllForInitialize(context.Background(), "role", false); err != nil {
		t.Fatalf("role delete failure must be skipped: %v", err)
	}
	if roleDeleteCalls != 2 {
		t.Fatalf("roles must be deleted individually after a failure: calls=%d", roleDeleteCalls)
	}
	if err := engine.deleteAllForInitialize(context.Background(), "action", false); err == nil {
		t.Fatal("non-role delete failure must still be returned")
	}
}
