package clone

import (
	"strings"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

func TestParametersRequireZabbix70(t *testing.T) {
	for _, value := range []string{"5.0.0", "6.0.0", "6.2.0", "6.4.0"} {
		if _, err := NewParameters(zabbix.ParseVersion(value)); err == nil || !strings.Contains(err.Error(), "7.0") {
			t.Fatalf("version %s: expected minimum version error, got %v", value, err)
		}
	}
	for _, value := range []string{"7.0.0", "7.2.0", "7.4.0", "8.0.0"} {
		params, err := NewParameters(zabbix.ParseVersion(value))
		if err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{"templategroup", "userdirectory", "proxygroup", "connector", "mfa"} {
			if _, ok := params.Methods[method]; !ok {
				t.Fatalf("%s missing %s", value, method)
			}
		}
	}
}

func TestMethodOutputIncludesObjectID(t *testing.T) {
	parameters, err := NewParameters(zabbix.ParseVersion("7.4.12"))
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"user", "mediatype", "template"} {
		spec := parameters.Methods[method]
		found := false
		for _, field := range spec.Options["output"].([]any) {
			if model.String(field) == spec.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s.get output does not include %s: %#v", method, spec.ID, spec.Options["output"])
		}
	}
	templateOutput := parameters.Methods["template"].Options["output"].([]any)
	foundUUID := false
	for _, field := range templateOutput {
		foundUUID = foundUUID || model.String(field) == "uuid"
	}
	if !foundUUID {
		t.Fatalf("template.get output does not include uuid: %#v", templateOutput)
	}
}

func TestUserGetIncludesUserGroupAccess(t *testing.T) {
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	fields := toAnyList(parameters.Methods["user"].Options["selectUsrgrps"])
	found := false
	for _, field := range fields {
		found = found || model.String(field) == "usrgrpid"
	}
	if !found {
		t.Fatalf("user.get selectUsrgrps does not include usrgrpid: %#v", fields)
	}
}

func TestHostGetIncludesFlags(t *testing.T) {
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, field := range toAnyList(parameters.Methods["host"].Options["output"]) {
		found = found || model.String(field) == "flags"
	}
	if !found {
		t.Fatalf("host.get output does not include flags: %#v", parameters.Methods["host"].Options["output"])
	}
}

func TestGlobalMacroGetIncludesTypeAndExcludesSecretText(t *testing.T) {
	parameters, err := NewParameters(zabbix.ParseVersion("7.0.21"))
	if err != nil {
		t.Fatal(err)
	}
	options := parameters.Methods["usermacro"].Options
	foundType := false
	for _, field := range toAnyList(options["output"]) {
		foundType = foundType || model.String(field) == "type"
	}
	if !foundType {
		t.Fatalf("usermacro.get output does not include type: %#v", options["output"])
	}
	types := toAnyList(objectMap(options["filter"])["type"])
	if len(types) != 2 || model.Int(types[0]) != macroTypeText || model.Int(types[1]) != macroTypeVault {
		t.Fatalf("unexpected usermacro type filter: %#v", types)
	}
}
