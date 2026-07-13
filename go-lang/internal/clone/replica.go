package clone

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/store"
)

func (e *Engine) LoadReplicaData(ctx context.Context, direct *Engine) error {
	if direct != nil {
		if err := direct.FirstProcess(ctx); err != nil {
			return err
		}
		if err := direct.CreateMasterData(ctx); err != nil {
			return err
		}
		if e.Version.Float() < direct.NewVersion.MasterVersion {
			return fmt.Errorf("target Zabbix %s is older than direct master %.1f", e.Version.String(), direct.NewVersion.MasterVersion)
		}
		e.Dataset = direct.Dataset
		e.NewVersion = direct.NewVersion
		e.NewVersion.VersionID = fmt.Sprintf("__DIRECT_MASTER_%s__", model.ZabbixTime())
		e.Versions = []model.Version{e.NewVersion}
		return nil
	}
	version, err := store.Latest(e.Versions, e.Config.TargetVersion)
	if err != nil {
		return err
	}
	dataset, err := e.Store.Load(ctx, version)
	if err != nil {
		return err
	}
	e.NewVersion = version
	e.Dataset = dataset
	return nil
}

func (e *Engine) ApplyGlobalSettings(ctx context.Context) error {
	for _, method := range e.Params.Global {
		if method == "authentication" {
			continue
		}
		items := e.Dataset[method]
		if len(items) == 0 {
			continue
		}
		data := mergeStoreItems(items)
		removeDiscard(data, e.Params.Discard[method])
		if method == "settings" {
			var err error
			data, err = e.prepareSettingsUpdate(data)
			if err != nil {
				return err
			}
		}
		if _, err := e.API.Call(ctx, method+".update", data); err != nil {
			return fmt.Errorf("%s.update: %w", method, err)
		}
		e.virtualSetGlobal(method, data)
		e.Log.Infof("Global Settings[%s]: Success", method)
	}
	return e.Refresh(ctx)
}

func (e *Engine) ApplyDeferredSettings(ctx context.Context) error {
	if len(e.deferredSettings) == 0 {
		return nil
	}
	data := model.CloneObject(e.deferredSettings)
	unresolved := e.resolveTargetSettingsIDs(data)
	if len(unresolved) > 0 {
		parts := make([]string, 0, len(unresolved))
		for _, key := range sortedKeys(unresolved) {
			parts = append(parts, fmt.Sprintf("%s=%q", key, model.String(unresolved[key])))
		}
		return fmt.Errorf("settings references do not exist on target: %s", strings.Join(parts, ", "))
	}
	if _, err := e.API.Call(ctx, "settings.update", data); err != nil {
		return fmt.Errorf("settings.update deferred references: %w", err)
	}
	e.deferredSettings = nil
	e.virtualSetGlobal("settings", data)
	e.Log.Infof("Global Settings[settings references]: Success")
	return e.Refresh(ctx)
}

func mergeStoreItems(items []model.StoreItem) model.Object {
	data := model.Object{}
	for _, item := range items {
		for key, value := range item.Data {
			data[key] = value
		}
	}
	return data
}

func (e *Engine) ApplyAuthentication(ctx context.Context) error {
	items := e.Dataset["authentication"]
	if len(items) == 0 {
		return nil
	}
	data := model.Object{}
	for _, item := range items {
		for key, value := range item.Data {
			data[key] = value
		}
	}
	data = marshalObject(e.ConvertIDs(data, false))
	if model.Int(data["mfa_status"]) == 0 {
		delete(data, "mfa_status")
		delete(data, "mfaid")
	}
	if model.Int(data["ldap_auth_enabled"]) == 0 {
		delete(data, "ldap_auth_enabled")
		delete(data, "disabled_usrgrpid")
	}
	if model.Int(data["saml_auth_enabled"]) == 0 {
		delete(data, "saml_auth_enabled")
	}
	if _, err := e.API.Call(ctx, "authentication.update", data); err != nil {
		return fmt.Errorf("authentication.update: %w", err)
	}
	e.virtualSetGlobal("authentication", data)
	e.Log.Infof("Authentication Update: Success")
	return nil
}

func (e *Engine) ApplyAPISection(ctx context.Context, section string) error {
	methods := e.Params.Section(section)
	if len(methods) == 0 {
		return nil
	}
	for _, method := range methods {
		items := e.Dataset[method]
		desired := map[string]bool{}
		created, updated, deleted := 0, 0, 0
		for _, item := range items {
			desired[item.Name] = true
			data := marshalObject(e.ConvertIDs(model.CloneObject(item.Data), false))
			removeDiscard(data, e.Params.Discard[method])
			if !e.prepareSpecialAPIItem(method, item.Name, data) {
				continue
			}
			callMethod := method + ".create"
			if local := e.Local[method][item.Name]; local != nil {
				callMethod = method + ".update"
				if id := e.Params.Methods[method].ID; id != "" {
					data[id] = local.ID
				}
				updated++
			} else {
				created++
			}
			if method == "usermacro" {
				if callMethod == "usermacro.create" {
					callMethod = "usermacro.createglobal"
				} else {
					callMethod = "usermacro.updateglobal"
				}
			}
			if _, err := e.API.Call(ctx, callMethod, data); err != nil {
				return fmt.Errorf("%s %s: %w", callMethod, item.Name, err)
			}
			e.virtualUpsert(method, item.Name, data)
		}
		if e.Config.DeleteAPI {
			for name, local := range e.Local[method] {
				if desired[name] || protectedAPIObject(method, name, local) {
					continue
				}
				id := e.Params.Methods[method].ID
				if id == "" {
					continue
				}
				if _, err := e.API.Call(ctx, method+".delete", []any{local.ID}); err != nil {
					return fmt.Errorf("%s.delete %s: %w", method, name, err)
				}
				e.virtualDelete(method, name)
				deleted++
			}
		}
		e.Log.Infof("API Execute[%s]: %d (create:%d/update:%d/delete:%d)", method, created+updated+deleted, created, updated, deleted)
		if !e.Config.DryRun {
			time.Sleep(time.Second)
		}
		if err := e.Refresh(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) prepareSpecialAPIItem(method, name string, data model.Object) bool {
	switch method {
	case "action":
		if !e.normalizeAction(name, data) {
			return false
		}
	case "correlation":
		normalizeCorrelation(data)
	case "drule":
		normalizeDiscoveryRule(data)
	case "maintenance":
		if !e.normalizeMaintenance(data) {
			return false
		}
	case "regexp":
		for _, expression := range objects(data["expressions"]) {
			if model.Int(expression["expression_type"]) != 1 {
				delete(expression, "exp_delimiter")
			}
		}
	case "user":
		if model.Int(data["userdirectoryid"]) != 0 {
			return false
		}
		enabled := objectMap(e.Config.Raw["enable_user"])
		password := model.String(enabled[name])
		if password == "" {
			return false
		}
		if e.Local[method][name] == nil {
			data["passwd"] = password
		}
		delete(data, "userdirectoryid")
		delete(data, "users_status")
		delete(data, "gui_access")
		delete(data, "debug_mode")
		groups := make([]any, 0)
		for _, value := range toAnyList(data["usrgrps"]) {
			if id := e.IDReplace["usergroup"][model.String(value)]; id != nil {
				groups = append(groups, model.Object{"usrgrpid": id})
			}
		}
		data["usrgrps"] = groups
	case "usergroup":
		e.convertRights(data, false)
		delete(data, "users")
		delete(data, "userids")
		if model.Int(data["userdirectoryid"]) == 0 || model.Int(data["gui_access"]) == 1 || model.Int(data["gui_access"]) == 3 {
			delete(data, "userdirectoryid")
		}
		if len(toAnyList(data["tag_filters"])) == 0 {
			delete(data, "tag_filters")
		}
		if model.Int(data["mfa_status"]) == 0 {
			delete(data, "mfa_status")
			delete(data, "mfaid")
		}
	case "role":
		delete(data, "readonly")
	case "mfa":
		if model.Int(data["type"]) == 2 {
			secrets := objectMap(e.Config.Raw["mfa_client_secret"])
			secret := model.String(secrets[name])
			if secret == "" {
				return false
			}
			data["client_secret"] = secret
		}
	}
	return true
}

func (e *Engine) normalizeAction(name string, data model.Object) bool {
	normalizeActionFields(data, true)
	e.resolveActionNames(data)
	if e.Local["action"][name] != nil {
		delete(data, "eventsource")
	}
	filter := objectMap(data["filter"])
	for _, condition := range objects(filter["conditions"]) {
		method := map[int]string{0: "hostgroup", 1: "host", 13: "template"}[model.Int(condition["conditiontype"])]
		if method != "" && condition["value"] != nil {
			if !e.IsReplica() && e.IDReplace[method][model.String(condition["value"])] == nil {
				return false
			}
			condition["value"] = e.Params.Replace(method, condition["value"], e.IDReplace)
		}
	}
	return true
}

func (e *Engine) resolveActionNames(value any) {
	methods := map[string]string{
		"userid": "user", "usrgrpid": "usergroup", "hostid": "host",
		"groupid": "hostgroup", "templateid": "template", "scriptid": "script", "mediatypeid": "mediatype",
	}
	var resolve func(any)
	resolve = func(current any) {
		switch data := current.(type) {
		case model.Object:
			for key, child := range data {
				if method := methods[key]; method != "" {
					if item := e.Local[method][model.String(child)]; item != nil {
						data[key] = item.ID
					}
				}
				resolve(data[key])
			}
		case map[string]any:
			resolve(model.Object(data))
		case []any:
			for _, child := range data {
				resolve(child)
			}
		}
	}
	resolve(value)
}

func normalizeActionFields(data model.Object, replica bool) {
	eventSource := model.Int(data["eventsource"])
	if eventSource != 0 {
		delete(data, "pause_symptoms")
		delete(data, "pause_suppressed")
		delete(data, "notify_if_canceled")
	}
	if eventSource == 1 || eventSource == 2 || eventSource == 3 {
		delete(data, "update_operations")
		delete(data, "updateOperations")
		delete(data, "acknowledge_operations")
		delete(data, "acknowledgeOperations")
	}
	if eventSource == 1 || eventSource == 2 {
		delete(data, "recovery_operations")
		delete(data, "recoveryOperations")
		delete(data, "esc_period")
	}
	filter := objectMap(data["filter"])
	delete(filter, "eval_formula")
	customFormula := model.Int(filter["evaltype"]) >= 3
	if !customFormula {
		delete(filter, "formula")
	}
	for _, condition := range objects(filter["conditions"]) {
		if !customFormula {
			delete(condition, "formulaid")
		}
		if replica && model.String(condition["value"]) == "" {
			delete(condition, "value")
		}
		if replica && model.String(condition["value2"]) == "" {
			delete(condition, "value2")
		}
	}
	for _, key := range []string{"operations", "recovery_operations", "update_operations"} {
		for _, operation := range objects(data[key]) {
			removeEmptyRecursive(operation)
			removeKeysRecursive(operation, map[string]bool{"actionid": true, "operationid": true, "opcommand_hstid": true, "opcommand_grpid": true})
			if eventSource != 0 || key != "operations" {
				delete(operation, "evaltype")
			}
			if key != "operations" && model.Int(operation["operationtype"]) == 11 {
				delete(objectMap(operation["opmessage"]), "mediatypeid")
			}
			if eventSource == 1 || eventSource == 2 {
				delete(operation, "esc_period")
				delete(operation, "esc_step_from")
				delete(operation, "esc_step_to")
			}
		}
	}
}

func removeEmptyRecursive(data model.Object) {
	for key, value := range data {
		switch current := value.(type) {
		case model.Object:
			removeEmptyRecursive(current)
			if len(current) == 0 {
				delete(data, key)
			}
		case map[string]any:
			removeEmptyRecursive(model.Object(current))
			if len(current) == 0 {
				delete(data, key)
			}
		case []any:
			for _, child := range current {
				if object, ok := child.(map[string]any); ok {
					removeEmptyRecursive(model.Object(object))
				}
			}
			if len(current) == 0 {
				delete(data, key)
			}
		case string:
			if current == "" {
				delete(data, key)
			}
		case nil:
			delete(data, key)
		}
	}
}

func removeKeysRecursive(value any, keys map[string]bool) {
	switch current := value.(type) {
	case model.Object:
		for key, child := range current {
			if keys[key] {
				delete(current, key)
				continue
			}
			removeKeysRecursive(child, keys)
		}
	case map[string]any:
		for key, child := range current {
			if keys[key] {
				delete(current, key)
				continue
			}
			removeKeysRecursive(child, keys)
		}
	case []any:
		for _, child := range current {
			removeKeysRecursive(child, keys)
		}
	}
}

func (e *Engine) convertRights(data model.Object, toNames bool) {
	for section, method := range map[string]string{"hostgroup_rights": "hostgroup", "templategroup_rights": "templategroup"} {
		for _, right := range objects(data[section]) {
			if right["id"] != nil {
				right["id"] = e.Params.Replace(method, right["id"], e.IDReplace)
			}
		}
	}
}

func normalizeCorrelation(data model.Object) {
	filter := objectMap(data["filter"])
	delete(filter, "eval_formula")
	custom := model.Int(filter["evaltype"]) == 3
	if !custom {
		delete(filter, "formula")
	}
	for _, condition := range objects(filter["conditions"]) {
		if !custom {
			delete(condition, "formulaid")
		}
	}
}

func normalizeDiscoveryRule(data model.Object) {
	delete(data, "nextcheck")
	delete(data, "error")
	for _, check := range objects(data["dchecks"]) {
		delete(check, "dcheckid")
		delete(check, "druleid")
		kind := model.Int(check["type"])
		if kind != 9 && kind != 10 && kind != 11 && kind != 13 {
			delete(check, "key_")
		}
		if kind != 10 && kind != 11 {
			delete(check, "snmp_community")
		}
		if kind != 13 {
			for _, key := range []string{"snmpv3_authpassphrase", "snmpv3_authprotocol", "snmpv3_contextname", "snmpv3_privpassphrase", "snmpv3_privprotocol", "snmpv3_securitylevel", "snmpv3_securityname"} {
				delete(check, key)
			}
		}
		if kind != 12 {
			delete(check, "allow_redirect")
		}
	}
}

func (e *Engine) normalizeMaintenance(data model.Object) bool {
	if !normalizeMaintenancePeriods(data) {
		return false
	}
	groups := relationValues(data["hostgroups"])
	hosts := relationValues(data["hosts"])
	delete(data, "hostgroups")
	data["groups"] = namedRelationIDs(groups, "groupid", e.IDReplace["hostgroup"])
	data["hosts"] = namedRelationIDs(hosts, "hostid", e.IDReplace["host"])
	if len(toAnyList(data["groups"])) == 0 {
		delete(data, "groups")
	}
	if len(toAnyList(data["hosts"])) == 0 {
		delete(data, "hosts")
	}
	if len(toAnyList(data["tags"])) == 0 {
		delete(data, "tags")
	}
	return data["groups"] != nil || data["hosts"] != nil
}

func normalizeMaintenancePeriods(data model.Object) bool {
	periods := objects(data["timeperiods"])
	valid := make([]model.Object, 0, len(periods))
	for _, period := range periods {
		switch model.Int(period["timeperiod_type"]) {
		case 0:
			if int64(model.Int(period["start_date"])+model.Int(period["period"])) < model.NowUnix() {
				continue
			}
			for _, key := range []string{"start_time", "every", "day", "dayofweek", "month"} {
				delete(period, key)
			}
		case 1:
			delete(period, "start_date")
			delete(period, "dayofweek")
		case 2:
			delete(period, "start_date")
			delete(period, "day")
		case 3:
			delete(period, "start_date")
		}
		valid = append(valid, period)
	}
	if len(valid) == 0 || int64(model.Int(data["active_till"])) < model.NowUnix() {
		return false
	}
	data["timeperiods"] = objectValues(valid)
	return true
}

func relationValues(value any) []string {
	result := make([]string, 0)
	for _, item := range toAnyList(value) {
		if object, ok := item.(map[string]any); ok {
			result = append(result, model.String(object["name"]))
		} else {
			result = append(result, model.String(item))
		}
	}
	return result
}

func namedRelationIDs(names []string, key string, lookup map[string]any) []any {
	result := make([]any, 0, len(names))
	for _, name := range names {
		if id := lookup[name]; id != nil {
			result = append(result, model.Object{key: id})
		}
	}
	return result
}

func protectedAPIObject(method, name string, item *LocalItem) bool {
	if method == "user" && name == superUser {
		return true
	}
	if method == "usergroup" && name == superGroup {
		return true
	}
	if method == "role" && (item.ID == "3" || model.Bool(item.Data["readonly"], false)) {
		return true
	}
	if method == "usermacro" && name == versionMacro {
		return true
	}
	return false
}

func objectMap(value any) map[string]any {
	if object, ok := value.(model.Object); ok {
		return object
	}
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func sortedItems(items []model.StoreItem) []model.StoreItem {
	out := append([]model.StoreItem(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
