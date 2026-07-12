package clone

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/t2-f/zabbix-cloning/internal/model"
)

type hostPlan struct {
	Name, Function, ID, UUID string
	Data                     model.Object
	Interfaces               []model.Object
}

func (e *Engine) ApplyHosts(ctx context.Context) error {
	if e.Config.SkipHost {
		e.Log.Infof("Host Import: SKIP.")
		return nil
	}
	hostUUIDs := map[string]string{}
	for _, host := range e.Local["host"] {
		if uuid := hostUUID(host.Data); uuid != "" {
			hostUUIDs[uuid] = host.ID
		}
	}
	var plans []hostPlan
	for _, item := range sortedItems(e.Dataset["host"]) {
		data := model.CloneObject(item.Data)
		if !e.hostIsTarget(data) {
			continue
		}
		uuid := hostUUID(data)
		e.normalizeHost(data)
		local := e.Local["host"][item.Name]
		plan := hostPlan{Name: item.Name, UUID: uuid, Data: data}
		if local != nil {
			if !e.Config.HostUpdate {
				continue
			}
			if hostUUIDs[uuid] != "" {
				plan.Function = "update"
				plan.ID = local.ID
			} else {
				continue
			}
		} else if id := hostUUIDs[uuid]; id != "" {
			if !e.Config.ForceHostUpdate {
				continue
			}
			plan.Function = "update"
			plan.ID = id
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
	for _, plan := range plans {
		if plan.Function == "update" && len(plan.Interfaces) > 0 && !failed[plan.Name] {
			if err := e.updateHostInterfaces(ctx, plan); err != nil {
				return err
			}
		}
	}
	if err := e.Refresh(ctx); err != nil {
		return err
	}
	if e.Config.DeleteHost {
		if err := e.deleteMissingHosts(ctx, plans, failed); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) hostIsTarget(data model.Object) bool {
	if e.IsReplica() {
		data["status"] = 0
	} else {
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
	workers := e.Config.Workers
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan hostPlan)
	failed := map[string]bool{}
	counts := map[string]int{"create": 0, "update": 0, "failed": 0}
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
					counts["failed"]++
					e.Log.Debugf("host.%s %s: %v", plan.Function, plan.Name, err)
				} else {
					counts[plan.Function]++
					e.virtualApplyHostPlan(plan)
				}
				done := counts["create"] + counts["update"] + counts["failed"]
				e.Log.Progress("\r    Host Import: %d/%d (create:%d/update:%d/failed:%d)", done, len(plans), counts["create"], counts["update"], counts["failed"])
				mutex.Unlock()
			}
		}()
	}
	for _, plan := range plans {
		jobs <- plan
	}
	close(jobs)
	wait.Wait()
	e.Log.Infof("Host Import: %d/%d (create:%d/update:%d/failed:%d)", counts["create"]+counts["update"]+counts["failed"], len(plans), counts["create"], counts["update"], counts["failed"])
	return failed
}

func (e *Engine) deleteMissingHosts(ctx context.Context, plans []hostPlan, failed map[string]bool) error {
	keep := map[string]bool{}
	for _, plan := range plans {
		if !failed[plan.Name] {
			keep[plan.Name] = true
		}
	}
	var ids []any
	var names []string
	for name, item := range e.Local["host"] {
		if !keep[name] {
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
