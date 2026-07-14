package clone

import (
	"context"
	"fmt"
	"sort"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

var fullInitializeOrder = []string{
	"action",
	"correlation",
	"maintenance",
	"drule",
	"sla",
	"service",
	"host",
	"template",
	"script",
	"connector",
	"proxy",
	"valuemap",
	"user",
	"mfa",
	"userdirectory",
	"usergroup",
	"role",
	"mediatype",
	"regexp",
	"usermacro",
	"proxygroup",
	"templategroup",
	"hostgroup",
}

func (e *Engine) initializeFull(ctx context.Context) error {
	authentication := model.Object{
		"authentication_type": 0,
		"ldap_auth_enabled":   0,
		"saml_auth_enabled":   0,
	}
	if e.Version.AtLeast(7, 0) {
		authentication["mfa_status"] = 0
	}
	if _, err := e.API.Call(ctx, "authentication.update", authentication); err != nil {
		return fmt.Errorf("full initialize authentication: %w", err)
	}
	e.virtualSetGlobal("authentication", authentication)

	for _, method := range e.fullInitializeMethods() {
		if err := e.deleteAllForInitialize(ctx, method, true); err != nil {
			return err
		}
	}
	return e.Refresh(ctx)
}

func (e *Engine) fullInitializeMethods() []string {
	methods := make([]string, 0, len(e.Params.Methods))
	seen := map[string]bool{}
	for _, method := range fullInitializeOrder {
		if spec, ok := e.Params.Methods[method]; ok && spec.ID != "" {
			methods = append(methods, method)
			seen[method] = true
		}
	}
	var remaining []string
	for method, spec := range e.Params.Methods {
		if spec.ID != "" && !seen[method] {
			remaining = append(remaining, method)
		}
	}
	sort.Strings(remaining)
	return append(methods, remaining...)
}

func (e *Engine) deleteAllForInitialize(ctx context.Context, method string, full bool) error {
	spec, ok := e.Params.Methods[method]
	if !ok || spec.ID == "" {
		return nil
	}
	items := e.Local[method]
	if full && !e.Config.DryRun {
		var err error
		items, err = e.getAllForFullInitialize(ctx, method)
		if err != nil {
			return err
		}
	}
	ids := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for name, item := range items {
		if protectedAPIObject(method, name, item) || method == "user" && name == e.Config.User {
			continue
		}
		ids = append(ids, item.ID)
		names = append(names, name)
	}
	if len(ids) == 0 {
		e.Log.Infof("Initialize[%s]: 0 deleted", method)
		return nil
	}
	deleteMethod := method + ".delete"
	if method == "usermacro" {
		deleteMethod = "usermacro.deleteglobal"
	}
	if _, err := e.API.Call(ctx, deleteMethod, ids); err != nil {
		return fmt.Errorf("initialize %s: %w", method, err)
	}
	if e.dryRunVirtual {
		for _, name := range names {
			e.virtualDelete(method, name)
		}
	}
	e.Log.Infof("Initialize[%s]: %d deleted", method, len(ids))
	return nil
}

func (e *Engine) getAllForFullInitialize(ctx context.Context, method string) (map[string]*LocalItem, error) {
	spec := e.Params.Methods[method]
	options := fullInitializeGetOptions(method, spec)
	objects, err := e.API.CallObjects(ctx, method+".get", options)
	if err != nil {
		return nil, fmt.Errorf("full initialize %s.get: %w", method, err)
	}
	items := make(map[string]*LocalItem, len(objects))
	for _, object := range objects {
		name := model.String(object[spec.Name])
		id := model.String(object[spec.ID])
		if name == "" {
			name = id
		}
		items[name] = &LocalItem{ID: id, Name: name, Data: model.Object(object)}
	}
	return items, nil
}

func fullInitializeGetOptions(method string, spec MethodSpec) model.Object {
	output := []any{spec.ID, spec.Name}
	if method == "role" {
		output = append(output, "readonly")
	}
	options := model.Object{"output": output}
	if method == "usermacro" {
		options["globalmacro"] = true
	} else if method == "user" {
		options["selectUsrgrps"] = []any{"usrgrpid", "name"}
	}
	return options
}
