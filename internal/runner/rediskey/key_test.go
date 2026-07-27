package rediskey

import (
	"reflect"
	"testing"
)

func TestScopeBuildsVersionedKeysAndCleanupPatterns(t *testing.T) {
	t.Parallel()

	scope, err := NewScope("shorturl", "load", "scenario-uid")
	if err != nil {
		t.Fatalf("NewScope() error = %v", err)
	}
	if got, want := scope.StorePrefix(), "kurama:v2:store:shorturl:load:scenario-uid"; got != want {
		t.Fatalf("StorePrefix() = %q, want %q", got, want)
	}
	if got, want := scope.RateLimitKey(), "kurama:v2:rate-limit:shorturl:load:scenario-uid"; got != want {
		t.Fatalf("RateLimitKey() = %q, want %q", got, want)
	}
	if got, want := scope.RateSchedulePrefix(), "kurama:v2:rate-schedule:shorturl:load:scenario-uid"; got != want {
		t.Fatalf("RateSchedulePrefix() = %q, want %q", got, want)
	}
	wantPatterns := []string{
		"kurama:v2:store:shorturl:load:scenario-uid:*",
		"kurama:v2:rate-limit:shorturl:load:scenario-uid",
		"kurama:v2:rate-schedule:shorturl:load:scenario-uid:*",
	}
	if got := scope.CleanupPatterns(); !reflect.DeepEqual(got, wantPatterns) {
		t.Fatalf("CleanupPatterns() = %#v, want %#v", got, wantPatterns)
	}
}

func TestNewScopeRejectsInvalidComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		scenario  string
		uid       string
	}{
		{name: "empty namespace", scenario: "load", uid: "scenario-uid"},
		{name: "empty scenario", namespace: "shorturl", uid: "scenario-uid"},
		{name: "empty UID", namespace: "shorturl", scenario: "load"},
		{name: "namespace colon", namespace: "short:url", scenario: "load", uid: "scenario-uid"},
		{name: "scenario colon", namespace: "shorturl", scenario: "lo:ad", uid: "scenario-uid"},
		{name: "UID colon", namespace: "shorturl", scenario: "load", uid: "scenario:uid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewScope(test.namespace, test.scenario, test.uid); err == nil {
				t.Fatal("NewScope() error = nil")
			}
		})
	}
}

func TestScopeValidateRejectsZeroValue(t *testing.T) {
	t.Parallel()
	if err := (Scope{}).Validate(); err == nil {
		t.Fatal("Scope{}.Validate() error = nil")
	}
}
