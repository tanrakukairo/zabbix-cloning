package clone

import (
	"context"
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/config"
	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

type dryRunStore struct{ saveCalls int }

func (s *dryRunStore) Versions(context.Context, string) ([]model.Version, error) {
	return nil, nil
}

func TestDryRunInitializeUsesVirtualReplicaState(t *testing.T) {
	api, err := zabbix.New("http://127.0.0.1:1", false)
	if err != nil {
		t.Fatal(err)
	}
	api.SetDryRun(true)
	logger, err := logx.New("test", "INFO", "", true)
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	local := map[string]map[string]*LocalItem{}
	for method := range parameters.Methods {
		local[method] = map[string]*LocalItem{}
	}
	for _, method := range []string{"correlation", "drule", "action", "script", "maintenance"} {
		local[method]["existing-"+method] = &LocalItem{ID: "1", Name: "existing-" + method, Data: model.Object{"name": "existing-" + method}}
	}
	engine := &Engine{
		API: api, Log: logger, Params: parameters, Local: local,
		Config: &config.Config{Role: "replica", DryRun: true, Initialize: true, Workers: 1, Raw: map[string]any{}},
		Dataset: model.Dataset{
			"script": {{Name: "new-script", Data: model.Object{"name": "new-script"}}},
			"action": {{Name: "new-action", Data: model.Object{"name": "new-action", "eventsource": 0}}},
		},
	}
	engine.enableDryRunVirtualState()
	if err := engine.initializeReplica(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"correlation", "drule", "action", "script", "maintenance"} {
		if len(engine.Local[method]) != 0 {
			t.Fatalf("initialize did not clear virtual %s state: %#v", method, engine.Local[method])
		}
	}
	if err := engine.ApplyAPISection(context.Background(), "MID"); err != nil {
		t.Fatal(err)
	}
	script := engine.Local["script"]["new-script"]
	if script == nil || !strings.HasPrefix(script.ID, "__DRY_RUN_script_") {
		t.Fatalf("script was not virtually created: %#v", script)
	}
	converted := engine.ConvertIDs(model.Object{"scriptid": "new-script"}, false)
	if !strings.HasPrefix(model.String(converted.(model.Object)["scriptid"]), "__DRY_RUN_script_") {
		t.Fatalf("virtual script ID was not used: %#v", converted)
	}
	if err := engine.ApplyAPISection(context.Background(), "POST"); err != nil {
		t.Fatal(err)
	}
	if action := engine.Local["action"]["new-action"]; action == nil || !strings.HasPrefix(action.ID, "__DRY_RUN_action_") {
		t.Fatalf("action was not virtually created: %#v", action)
	}
	if err := engine.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.Local["script"]["new-script"] == nil || engine.Local["action"]["new-action"] == nil {
		t.Fatal("virtual state was lost after refresh")
	}
	counts := api.DryRunMethods()
	for _, method := range []string{"script.create", "action.create"} {
		if counts[method] != 1 {
			t.Fatalf("expected one %s plan, got %d", method, counts[method])
		}
	}
	for _, method := range []string{"correlation", "drule", "action", "script", "maintenance"} {
		if counts[method+".delete"] != 1 {
			t.Fatalf("expected one %s.delete plan, got %d", method, counts[method+".delete"])
		}
	}
}

func TestDryRunConfigurationAddsVirtualIDs(t *testing.T) {
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{
		Params: parameters,
		Config: &config.Config{Role: "replica", DryRun: true},
		Local: map[string]map[string]*LocalItem{
			"hostgroup": {}, "templategroup": {}, "template": {}, "mediatype": {},
		},
		Dataset: model.Dataset{
			"hostgroup": {{Name: "New group", Data: model.Object{"name": "New group"}}},
			"template":  {{Name: "New template", Data: model.Object{"name": "New template"}}},
		},
	}
	engine.enableDryRunVirtualState()
	engine.virtualApplyConfiguration()
	if !isDryRunVirtualID(engine.IDReplace["hostgroup"]["New group"]) || !isDryRunVirtualID(engine.IDReplace["template"]["New template"]) {
		t.Fatalf("configuration objects have no virtual IDs: %#v", engine.IDReplace)
	}
}
func (s *dryRunStore) Load(context.Context, model.Version) (model.Dataset, error) {
	return nil, nil
}
func (s *dryRunStore) Save(context.Context, model.Version, model.Dataset) error {
	s.saveCalls++
	return nil
}
func (s *dryRunStore) Clear(context.Context, string) error                { return nil }
func (s *dryRunStore) DeleteRecord(context.Context, string, string) error { return nil }
func (s *dryRunStore) DeleteVersion(context.Context, string) error        { return nil }
func (s *dryRunStore) Close() error                                       { return nil }

func TestDryRunSkipsDatastoreSave(t *testing.T) {
	backend := &dryRunStore{}
	logger, err := logx.New("test", "INFO", "", true)
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{
		Config:     &config.Config{DryRun: true},
		Log:        logger,
		Store:      backend,
		NewVersion: model.Version{VersionID: "dry-run-version"},
		Dataset:    model.Dataset{},
	}
	if err := engine.SaveMasterData(context.Background()); err != nil {
		t.Fatal(err)
	}
	if backend.saveCalls != 0 {
		t.Fatalf("dry run saved to datastore: %d", backend.saveCalls)
	}
}
