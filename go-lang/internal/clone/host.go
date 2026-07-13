package clone

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

type hostPlan struct {
	Name, Function, ID, UUID string
	Data                     model.Object
	Interfaces               []model.Object
}
type hostRetention struct {
	Names map[string]bool
	UUIDs map[string]bool
	IDs   map[string]bool
}

func (e *Engine) ApplyHosts(ctx context.Context) error {
	if e.Config.SkipHost {
		e.Log.Infof("Host Import: SKIP.")
		return nil
	}
	hostByUUID := map[string]*LocalItem{}
	for _, host := range e.Local["host"] {
		if uuid := hostUUID(host.Data); uuid != "" {
			hostByUUID[uuid] = host
		}
	}
	retention := hostRetention{Names: map[string]bool{}, UUIDs: map[string]bool{}, IDs: map[string]bool{}}
	var plans []hostPlan
	for _, item := range sortedItems(e.Dataset["host"]) {
		data := model.CloneObject(item.Data)
		if !e.hostIsTarget(data) {
			continue
		}
		uuid := hostUUID(data)
		local := e.Local["host"][item.Name]
		matchingUUID := hostByUUID[uuid]
		retention.add(item.Name, uuid, local, matchingUUID)
		e.normalizeHost(data)
		plan := hostPlan{Name: item.Name, UUID: uuid, Data: data}
		if local != nil {
			if !e.Config.HostUpdate {
				continue
			}
			if uuid != "" && hostUUID(local.Data) == uuid {
				plan.Function = "update"
				plan.ID = local.ID
			} else {
				continue
			}
		} else if matchingUUID != nil {
			if !e.Config.ForceHostUpdate {
				continue
			}
			plan.Function = "update"
			plan.ID = matchingUUID.ID
			delete(data, "host")
			delete(data, "name")
		} else {
			plan.Function = "create"
		}
		if plan.Function == "update" {
			plan.Interfaces = objects(data["interfaces"])
			delete(data, "interfaces")
			data["hostid"] = plan.ID
		}
		plans = append(plans, plan)
	}
	failed := e.applyHostPlans(ctx, plans)
	e.applyHostInterfaces(ctx, plans, failed)
	if err := e.Refresh(ctx); err != nil {
		return err
	}
	if e.Config.DeleteHost {
		if err := e.deleteMissingHosts(ctx, retention); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) hostIsTarget(data model.Object) bool {
	if !e.IsReplica() {
		target := false
		for _, tag := range objects(data["tags"]) {
			if model.String(tag["tag"]) == "ZC_WORKER" && model.String(tag["value"]) == e.Config.Node {
				target = true
			}
		}
		if !target {
			return false
		}
		data["status"] = 0
	}
	if e.Config.DisableMonitoring {
		data["status"] = 1
	}
	return true
}

func (e *Engine) normalizeHost(data model.Object) {
	for _, key := range []string{"items", "triggers", "discovery_rules"} {
		delete(data, key)
	}
	if status, ok := data["status"]; ok {
		data["status"] = normalizeHostStatus(status)
	}
	if mode := model.String(data["inventory_mode"]); mode != "" {
		modes := map[string]int{"DISABLED": -1, "MANUAL": 0, "AUTOMATIC": 1, "AOTOMATIC": 1}
		if value, ok := modes[mode]; ok {
			data["inventory_mode"] = value
		}
	}
	if inventory, ok := data["inventory"].(map[string]any); ok {
		delete(inventory, "inventory_mode")
	}
	interfaces := objects(data["interfaces"])
	for _, hostIf := range interfaces {
		e.normalizeInterface(hostIf)
	}
	if len(interfaces) > 0 {
		data["interfaces"] = objectValues(interfaces)
	} else {
		delete(data, "interfaces")
	}
	if e.Version.AtLeast(7, 0) {
		proxyType := stringsLower(model.String(data["monitored_by"]))
		if proxyType == "" {
			proxyType = "direct"
		}
		delete(data, "monitored_by")
		monitor := map[string]int{"direct": 0, "proxy": 1, "proxy_group": 2}[proxyType]
		data["monitored_by"] = monitor
		if monitor > 0 {
			if proxy, ok := data[proxyType].(map[string]any); ok {
				method := stringsReplace(proxyType, "_", "")
				if id := e.IDReplace[method][model.String(proxy["name"])]; id != nil {
					data[proxyType+"id"] = id
				}
			}
		}
		delete(data, "proxy")
		delete(data, "proxy_group")
	}
	data["groups"] = relationIDs(data["groups"], "groupid", e.IDReplace["hostgroup"])
	data["templates"] = relationIDs(data["templates"], "templateid", e.IDReplace["template"])
}

func normalizeHostStatus(value any) int {
	switch stringsUpper(model.String(value)) {
	case "ENABLED":
		return 0
	case "DISABLED":
		return 1
	default:
		return model.Int(value)
	}
}

func (e *Engine) normalizeInterface(hostIf model.Object) {
	delete(hostIf, "interface_ref")
	ifType := model.String(hostIf["type"])
	types := map[string]int{"AGENT": 1, "SNMP": 2, "IPMI": 3, "JMX": 4}
	if ifType == "" {
		hostIf["type"] = types["AGENT"]
	} else if value, ok := types[ifType]; ok {
		hostIf["type"] = value
	}
	if _, ok := hostIf["ip"]; !ok {
		hostIf["ip"] = "127.0.0.1"
	}
	if _, ok := hostIf["port"]; !ok {
		hostIf["port"] = "10050"
	}
	hostIf["main"] = yesNo(hostIf["default"], 1)
	delete(hostIf, "default")
	hostIf["useip"] = yesNo(hostIf["useip"], 1)
	if _, ok := hostIf["dns"]; !ok {
		hostIf["dns"] = ""
	}
	if e.Config.UseIP && model.Int(hostIf["useip"]) == 0 {
		if values, err := net.LookupHost(model.String(hostIf["dns"])); err == nil && len(values) > 0 {
			hostIf["ip"] = values[0]
			hostIf["useip"] = 1
			delete(hostIf, "dns")
		}
	}
	delete(hostIf, "bulk")
	if model.Int(hostIf["type"]) == 2 {
		details := objectMap(hostIf["details"])
		version := stringsUpper(model.String(details["version"]))
		versions := map[string]int{"SNMPV1": 1, "SNMPV2": 2, "SNMPV3": 3}
		if versions[version] == 0 {
			versions[version] = 2
		}
		hostIf["details"] = model.Object{"version": versions[version], "community": firstString(model.String(details["community"]), "{$SNMP_COMMUNITY}")}
	}
}

func (e *Engine) applyHostPlans(ctx context.Context, plans []hostPlan) map[string]bool {
	workers := e.hostApplyWorkers()
	jobs := make(chan hostPlan)
	failed := map[string]bool{}
	progress := newApplyProgress(e.Log, e.Config.Quiet, "Host Import", len(plans), "create", "update")
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for plan := range jobs {
				_, err := e.API.Call(ctx, "host."+plan.Function, plan.Data)
				mutex.Lock()
				if err != nil {
					failed[plan.Name] = true
					progress.fail(plan.Name, fmt.Errorf("host.%s: %w", plan.Function, err))
				} else {
					progress.record(plan.Function)
					e.virtualApplyHostPlan(plan)
				}
				mutex.Unlock()
			}
		}()
	}
	for _, plan := range plans {
		jobs <- plan
	}
	close(jobs)
	wait.Wait()
	progress.finish()
	return failed
}

func (e *Engine) hostApplyWorkers() int {
	if e.Config.Workers < 1 {
		return 1
	}
	return e.Config.Workers
}

func (r hostRetention) add(name, uuid string, hosts ...*LocalItem) {
	r.Names[name] = true
	if uuid != "" {
		r.UUIDs[uuid] = true
	}
	for _, host := range hosts {
		if host != nil && host.ID != "" {
			r.IDs[host.ID] = true
		}
	}
}

func (r hostRetention) keeps(name string, host *LocalItem) bool {
	if r.Names[name] || r.IDs[host.ID] {
		return true
	}
	uuid := hostUUID(host.Data)
	return uuid != "" && r.UUIDs[uuid]
}

func (e *Engine) deleteMissingHosts(ctx context.Context, retention hostRetention) error {
	var ids []any
	var names []string
	for name, item := range e.Local["host"] {
		if !retention.keeps(name, item) {
			ids = append(ids, item.ID)
			names = append(names, name)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := e.API.Call(ctx, "host.delete", ids); err != nil {
		return err
	}
	if e.dryRunVirtual {
		for _, name := range names {
			e.virtualDelete("host", name)
		}
	}
	sort.Strings(names)
	e.Log.Infof("Host Delete: Success. %v", names)
	return e.Refresh(ctx)
}

func hostUUID(data model.Object) string {
	var values []string
	for _, tag := range objects(data["tags"]) {
		if model.String(tag["tag"]) == uniqueTag {
			values = append(values, model.String(tag["value"]))
		}
	}
	if len(values) == 1 {
		return values[0]
	}
	return ""
}
func relationIDs(value any, key string, lookup map[string]any) []any {
	var result []any
	for _, item := range objects(value) {
		if id := lookup[model.String(item["name"])]; id != nil {
			result = append(result, model.Object{key: id})
		}
	}
	return result
}
func yesNo(value any, fallback int) int {
	if value == nil || model.String(value) == "" {
		return fallback
	}
	if model.String(value) == "YES" {
		return 1
	}
	if model.String(value) == "NO" {
		return 0
	}
	return model.Int(value)
}
func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func stringsLower(value string) string             { return strings.ToLower(value) }
func stringsUpper(value string) string             { return strings.ToUpper(value) }
func stringsReplace(value, old, new string) string { return strings.ReplaceAll(value, old, new) }
