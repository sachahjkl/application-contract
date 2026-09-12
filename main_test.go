package main

import "testing"

func TestVolumeName(t *testing.T) {
	t.Parallel()

	value := config{Application: application{Name: "example"}}
	if name := value.volumeName("production"); name != "example-production-data" {
		t.Errorf("volumeName() = %q", name)
	}
}
