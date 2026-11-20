package clone

import "github.com/tanrakukairo/zabbix-cloning/internal/model"

// CEP conditions use names, tags and local formula letters, not foreign object IDs.
func normalizeCEPRule(data model.Object) {
	delete(data, "cep_ruleid")
	delete(data, "error")
	normalizeCEPFilter(objectMap(data["filter"]))
	for _, operation := range objects(data["operations"]) {
		normalizeCEPFilter(objectMap(operation["filter"]))
		normalizeCEPFilter(objectMap(operation["scope_filter"]))
	}
}

func normalizeCEPFilter(filter model.Object) {
	delete(filter, "eval_formula")
	if model.Int(filter["evaltype"]) == 3 {
		return
	}
	delete(filter, "formula")
	for _, condition := range objects(filter["conditions"]) {
		delete(condition, "formulaid")
	}
}
