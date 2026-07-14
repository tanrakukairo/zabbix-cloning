package clone

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

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
		"user": superUser, "role": "Super admin role", "usermacro": versionMacro,
	} {
		if engine.Local[method][name] == nil {
			t.Fatalf("protected %s %s was deleted", method, name)
		}
	}
	if engine.Local["user"]["API operator"] == nil {
		t.Fatal("API execution user was deleted")
	}
	if engine.Local["user"]["guest"] != nil {
		t.Fatal("guest user was protected by username")
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
