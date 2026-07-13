package clone

import (
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
	"github.com/tanrakukairo/zabbix-cloning/internal/zabbix"
)

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
