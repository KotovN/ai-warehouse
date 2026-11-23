package config

import "testing"

func TestResourceTypes(t *testing.T) {
	if !IsResourceType("clis") {
		t.Fatal("clis must be a resource type")
	}
	if IsResourceType("models") {
		t.Fatal("models is not synced in v1")
	}
}
