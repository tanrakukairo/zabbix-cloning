package clone

import (
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func TestPrepareMasterItemFiltersSecretTextAndKeepsVaultMacros(t *testing.T) {
	engine := &Engine{}
	data := model.Object{
		"macros": []any{
			model.Object{"macro": "{$TEXT}", "value": "plain", "type": "TEXT"},
			model.Object{"macro": "{$SECRET}", "value": "******", "type": "SECRET_TEXT"},
			model.Object{"macro": "{$VAULT}", "value": "secret/path:key", "type": "VAULT"},
		},
		"discovery_rules": []any{model.Object{
			"host_prototypes": []any{model.Object{
				"macros": []any{
					model.Object{"macro": "{$NESTED_SECRET}", "type": 1},
					model.Object{"macro": "{$NESTED_VAULT}", "value": "nested/path:key", "type": 2},
				},
			}},
		}},
	}

	if !engine.prepareMasterItem("template", data) {
		t.Fatal("template was unexpectedly excluded")
	}
	macros := objects(data["macros"])
	if len(macros) != 2 || model.String(macros[0]["macro"]) != "{$TEXT}" || model.String(macros[1]["macro"]) != "{$VAULT}" {
		t.Fatalf("unexpected top-level macros: %#v", macros)
	}
	if model.String(macros[1]["type"]) != "VAULT" {
		t.Fatalf("vault type was not retained: %#v", macros[1])
	}

	rules := objects(data["discovery_rules"])
	prototypes := objects(rules[0]["host_prototypes"])
	nested := objects(prototypes[0]["macros"])
	if len(nested) != 1 || model.String(nested[0]["macro"]) != "{$NESTED_VAULT}" || model.String(nested[0]["type"]) != "VAULT" {
		t.Fatalf("unexpected nested macros: %#v", nested)
	}
}

func TestPrepareMasterItemFiltersSecretGlobalAndRecordsVaultType(t *testing.T) {
	engine := &Engine{}
	if engine.prepareMasterItem("usermacro", model.Object{"macro": "{$SECRET}", "type": 1}) {
		t.Fatal("secret text global macro was retained")
	}
	vault := model.Object{"macro": "{$VAULT}", "value": "secret/path:key", "type": "2"}
	if !engine.prepareMasterItem("usermacro", vault) {
		t.Fatal("vault global macro was excluded")
	}
	if model.Int(vault["type"]) != macroTypeVault {
		t.Fatalf("vault global macro type was not recorded: %#v", vault)
	}
}

func TestNormalizeHostMacrosUsesAPITypes(t *testing.T) {
	data := model.Object{"macros": []any{
		model.Object{"macro": "{$TEXT}", "type": "TEXT"},
		model.Object{"macro": "{$SECRET}", "type": "SECRET_TEXT"},
		model.Object{"macro": "{$VAULT}", "value": "secret/path:key", "type": "VAULT"},
	}}
	normalizeHostMacros(data)
	macros := objects(data["macros"])
	if len(macros) != 2 {
		t.Fatalf("secret text macro was not filtered: %#v", macros)
	}
	if model.Int(macros[0]["type"]) != macroTypeText || model.Int(macros[1]["type"]) != macroTypeVault {
		t.Fatalf("host macro types were not normalized: %#v", macros)
	}
}
