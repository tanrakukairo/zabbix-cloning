package clone

import (
	"regexp"
	"testing"

	"github.com/tanrakukairo/zabbix-cloning/internal/model"
)

func TestRegenerateUUIDsIsDeterministicUUIDv4(t *testing.T) {
	original := "e1ca624566424496bff9d90c261ab37b"
	child := model.Object{"uuid": "79aa9ebb1ce64944a78a7c8d7603f53b"}
	template := model.Object{"uuid": original, "items": []any{child}}
	regenerateUUIDs(template)
	want := crossVersionUUID(original)
	if got := model.String(template["uuid"]); got != want {
		t.Fatalf("template UUID = %q, want %q", got, want)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{12}4[0-9a-f]{3}[89ab][0-9a-f]{15}$`)
	for _, value := range []string{model.String(template["uuid"]), model.String(child["uuid"])} {
		if !pattern.MatchString(value) {
			t.Fatalf("not a UUIDv4: %s", value)
		}
	}
}
