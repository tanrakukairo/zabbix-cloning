package clone

import "github.com/tanrakukairo/zabbix-cloning/internal/model"

// Inspect stored names before ConvertIDs: source IDs must never be mistaken for
// unrelated target IDs. A skipped item remains desired so --delete.api keeps it.
func (e *Engine) missingHostReferences(method string, data model.Object) []string {
	missing := map[string]bool{}
	check := func(value any) {
		name := model.String(value)
		host := e.Local["host"][name]
		if name != "" && (host == nil || host.ID == "") {
			missing[name] = true
		}
	}
	switch method {
	case "action":
		for _, condition := range objects(objectMap(data["filter"])["conditions"]) {
			if model.Int(condition["conditiontype"]) == 1 {
				check(condition["value"])
			}
		}
		for _, section := range []string{"operations", "recovery_operations", "update_operations"} {
			for _, operation := range objects(data[section]) {
				for _, host := range objects(operation["opcommand_hst"]) {
					value := model.String(host["hostid"])
					// Zabbix permits commands on the host that raised the event.
					if value != "0" && value != "__CURRENT_HOST__" {
						check(value)
					}
				}
			}
		}
	case "maintenance":
		for _, name := range relationValues(data["hosts"]) {
			check(name)
		}
	}
	return sortedKeys(missing)
}
