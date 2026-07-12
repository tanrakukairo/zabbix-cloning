package clone

import (
	"reflect"
	"testing"
)

func TestNormalizeIntervalsSupportsDurationsAndUserMacros(t *testing.T) {
	got := normalizeIntervals([]string{
		"1h", "30m", "CHECKNOW_INTERVAL", "{$ALREADY_A_MACRO}", "1h", " ",
	})
	want := []string{"1800", "3600", "{$ALREADY_A_MACRO}", "{$CHECKNOW_INTERVAL}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeIntervals() = %#v, want %#v", got, want)
	}
}
