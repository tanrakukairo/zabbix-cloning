package clone

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func (e *Engine) CreateMasterData(ctx context.Context) error {
	if !e.IsMaster() {
		return fmt.Errorf("not master node")
	}
	if err := e.exportConfiguration(ctx); err != nil {
		return err
	}
	dataset := model.Dataset{}
	for method, items := range e.Local {
		for _, item := range items {
			if method == "user" && item.Name == superUser || method == "usergroup" && protectedUserGroupID(item.ID) || method == "role" && item.ID == "3" || method == "usermacro" && item.Name == versionMacro {
				continue
			}
			data := model.CloneObject(item.Data)
			if contains(append(append(append(e.Params.Pre, e.Params.Mid...), e.Params.Post...), e.Params.Account...), method) {
				data = marshalObject(e.ConvertIDs(data, true))
			}
			if !e.prepareMasterItem(method, data) {
				continue
			}
			removeDiscard(data, e.Params.Discard[method])
			dataset[method] = append(dataset[method], model.StoreItem{DataID: newUUID(), Name: item.Name, Data: data})
		}
	}
	e.Dataset = dataset
	e.NewVersion = model.Version{VersionID: newUUID(), UnixTime: model.NowUnix(), MasterVersion: e.Version.Float(), Description: fmt.Sprintf("MasterNode: %s (%s), CreateDate: %s", e.Config.Node, e.Config.Endpoint, model.ZabbixTime())}
	if e.Config.Description != "" {
		e.NewVersion.Description += " : " + e.Config.Description
	}
	return nil
}

func (e *Engine) prepareMasterItem(method string, data model.Object) bool {
	switch method {
	case "settings":
		e.replaceSettingsIDs(data, true)
	case "action":
		normalizeActionFields(data, false)
		filter := objectMap(data["filter"])
		for _, condition := range objects(filter["conditions"]) {
			target := map[int]string{0: "hostgroup", 1: "host", 13: "template"}[model.Int(condition["conditiontype"])]
			if target != "" && condition["value"] != nil {
				condition["value"] = e.Params.Replace(target, condition["value"], e.IDReplace)
			}
		}
	case "user":
		groups := make([]any, 0)
		for _, group := range objects(data["usrgrps"]) {
			if name := model.String(group["name"]); name != "" {
				groups = append(groups, name)
			}
		}
		data["usrgrps"] = groups
	case "usergroup":
		e.convertRights(data, true)
	case "correlation":
		normalizeCorrelation(data)
	case "maintenance":
		if !normalizeMaintenancePeriods(data) {
			return false
		}
		data["hostgroups"] = relationNames(data["hostgroups"])
		data["hosts"] = relationNames(data["hosts"])
		if len(toAnyList(data["hostgroups"])) == 0 {
			delete(data, "hostgroups")
		}
		if len(toAnyList(data["hosts"])) == 0 {
			delete(data, "hosts")
		}
		if len(toAnyList(data["tags"])) == 0 {
			delete(data, "tags")
		}
	}
	return true
}

func relationNames(value any) []any {
	result := make([]any, 0)
	for _, item := range objects(value) {
		if name := model.String(item["name"]); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func (e *Engine) SaveMasterData(ctx context.Context) error {
	if e.Config.DryRun {
		e.Log.Infof("DRY RUN: datastore save skipped for version %s", e.NewVersion.VersionID)
		return nil
	}
	if e.Store == nil {
		return fmt.Errorf("direct store is read-only")
	}
	if err := e.Store.Save(ctx, e.NewVersion, e.Dataset); err != nil {
		return err
	}
	e.Log.Infof("Saved Version: %s", e.NewVersion.VersionID)
	return nil
}

func (e *Engine) exportConfiguration(ctx context.Context) error {
	options := map[string]any{}
	var templateIDs []any
	sectionMethod := map[string]string{}
	for method, section := range e.Params.ConfigExport {
		sectionMethod[section] = method
		if method == "trigger" {
			continue
		}
		var ids []any
		for _, item := range e.Local[method] {
			ids = append(ids, item.ID)
		}
		if method == "template" {
			if !e.Config.SkipTemplate {
				templateIDs = ids
			}
			continue
		}
		options[section] = ids
	}
	batches := []map[string]any{options}
	for start := 0; start < len(templateIDs); start += e.Config.TemplateSeparate {
		end := start + e.Config.TemplateSeparate
		if end > len(templateIDs) {
			end = len(templateIDs)
		}
		batches = append(batches, map[string]any{"templates": templateIDs[start:end]})
	}
	for _, batch := range batches {
		result, err := e.API.Call(ctx, "configuration.export", model.Object{"format": "json", "options": batch})
		if err != nil {
			return err
		}
		source, ok := result.(string)
		if !ok {
			return fmt.Errorf("configuration.export returned %T", result)
		}
		source = strings.ReplaceAll(source, "media_types", "mediaTypes")
		var document map[string]any
		decoder := json.NewDecoder(strings.NewReader(source))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return err
		}
		export, ok := document["zabbix_export"].(map[string]any)
		if !ok {
			return fmt.Errorf("configuration export has no zabbix_export")
		}
		for section, value := range export {
			method := sectionMethod[section]
			if method == "" {
				continue
			}
			if e.Local[method] == nil {
				e.Local[method] = map[string]*LocalItem{}
			}
			values, _ := value.([]any)
			for index, raw := range values {
				data, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				name := model.String(data[e.Params.Methods[method].Name])
				if name == "" {
					name = model.String(data["uuid"])
				}
				if name == "" {
					name = fmt.Sprintf("%s%d", method, index)
				}
				if current := e.Local[method][name]; current != nil {
					current.Data = data
				} else {
					e.Local[method][name] = &LocalItem{ID: fmt.Sprint(index), Name: name, Data: data}
				}
			}
		}
	}
	e.rebuildIDReplace()
	return nil
}

func removeDiscard(data model.Object, discard any) {
	values, ok := discard.([]any)
	if !ok {
		return
	}
	for _, value := range values {
		delete(data, model.String(value))
	}
}
