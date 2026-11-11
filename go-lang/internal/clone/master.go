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
	if excluded := e.excludeLLDDiscoveredHostsFromMaster(); excluded > 0 {
		e.Log.Infof("Master Data: Excluded %d LLD-discovered hosts", excluded)
	}
	if err := e.exportConfiguration(ctx); err != nil {
		return err
	}
	dataset := model.Dataset{}
	for method, items := range e.Local {
		for _, item := range items {
			if method == "host" && isLLDDiscoveredHost(item) {
				continue
			}
			if method == "user" && item.Name == superUser || method == "usergroup" && protectedUserGroupID(item.ID) || method == "role" && item.ID == "3" || method == "usermacro" && item.Name == versionMacro {
				continue
			}
			data := model.CloneObject(item.Data)
			if method == "usergroup" {
				if err := e.storeProxyPermissions(data); err != nil {
					return fmt.Errorf("usergroup %q: %w", item.Name, err)
				}
			}
			if id := model.String(data["disabled_usrgrpid"]); method == "authentication" && id != "" && id != "0" {
				name := model.String(e.IDReplace["usergroup"][id])
				if group := e.Local["usergroup"][name]; group == nil || group.ID != id {
					return fmt.Errorf("authentication references unknown disabled user group ID %q", id)
				}
				data["disabled_usrgrpid"] = model.Object{"name": name}
			}
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

func (e *Engine) excludeLLDDiscoveredHostsFromMaster() int {
	excluded := 0
	for name, item := range e.Local["host"] {
		if !isLLDDiscoveredHost(item) {
			continue
		}
		delete(e.Local["host"], name)
		excluded++
	}
	if excluded > 0 {
		e.rebuildIDReplace()
	}
	return excluded
}

func (e *Engine) prepareMasterItem(method string, data model.Object) bool {
	switch method {
	case "usermacro":
		kind, known := macroType(data["type"])
		if known && kind == macroTypeSecret {
			return false
		}
		if known && kind == macroTypeVault {
			data["type"] = macroTypeVault
		}
	case "host", "template":
		filterExportMacros(data)
	case "settings":
		discardReadOnlySettings(data)
		delete(objectMap(data["apm_global_db"]), "password")
		e.replaceSettingsIDs(data, true)
	case "userdirectory":
		if isSAMLDirectory(data) {
			stripSAMLCertificates(data)
		}
	case "mediatype":
		if mediaTypeHasEmptyNormalPassword(data) {
			return false
		}
	case "action":
		normalizeActionFields(data, false)
		filter := objectMap(data["filter"])
		for _, condition := range objects(filter["conditions"]) {
			target := map[int]string{0: "hostgroup", 1: "host", 13: "template", 29: "proxygroup"}[model.Int(condition["conditiontype"])]
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
		// References must use the technical name used by host.get and host export.
		hosts := make([]any, 0)
		for _, host := range objects(data["hosts"]) {
			if name := firstString(model.String(host["host"]), model.String(host["name"])); name != "" {
				hosts = append(hosts, name)
			}
		}
		data["hosts"] = hosts
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

func mediaTypeHasEmptyNormalPassword(data model.Object) bool {
	return mediaTypeUsesNormalPassword(data) && model.String(data["password"]) == ""
}

func mediaTypeUsesNormalPassword(data model.Object) bool {
	authentication := strings.ToUpper(model.String(data["smtp_authentication"]))
	return authentication == "1" || authentication == "PASSWORD" || authentication == "NORMAL_PASSWORD"
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
		ids := make([]any, 0, len(e.Local[method]))
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
