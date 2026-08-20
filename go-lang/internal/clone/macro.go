package clone

import (
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

const (
	macroTypeText   = 0
	macroTypeSecret = 1
	macroTypeVault  = 2
)

func macroType(value any) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(model.String(value))) {
	case "0", "TEXT":
		return macroTypeText, true
	case "1", "SECRET", "SECRET_TEXT":
		return macroTypeSecret, true
	case "2", "VAULT", "VAULT_SECRET":
		return macroTypeVault, true
	default:
		return 0, false
	}
}

// filterExportMacros removes write-only secret text values from configuration
// export data. Vault references are safe to clone and must retain their type.
func filterExportMacros(value any) {
	switch current := value.(type) {
	case model.Object:
		filterExportMacroObject(current)
	case map[string]any:
		filterExportMacroObject(model.Object(current))
	case []any:
		for _, child := range current {
			filterExportMacros(child)
		}
	}
}

func filterExportMacroObject(current model.Object) {
	if raw, exists := current["macros"]; exists {
		macros := make([]any, 0)
		for _, macro := range objects(raw) {
			kind, known := macroType(macro["type"])
			if known && kind == macroTypeSecret {
				continue
			}
			if known && kind == macroTypeVault {
				macro["type"] = "VAULT"
			}
			macros = append(macros, macro)
		}
		current["macros"] = macros
	}
	for _, child := range current {
		filterExportMacros(child)
	}
}

// normalizeHostMacros converts configuration-export enum names to the integer
// values accepted by host.create and host.update.
func normalizeHostMacros(data model.Object) {
	raw, exists := data["macros"]
	if !exists {
		return
	}
	macros := make([]any, 0)
	for _, macro := range objects(raw) {
		kind, known := macroType(macro["type"])
		if known && kind == macroTypeSecret {
			continue
		}
		if known {
			macro["type"] = kind
		}
		macros = append(macros, macro)
	}
	data["macros"] = macros
}
