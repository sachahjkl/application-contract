package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestVolumeName(t *testing.T) {
	t.Parallel()

	value := config{Application: application{Name: "example"}}
	if name := value.volumeName("production"); name != "example-production-data" {
		t.Errorf("volumeName() = %q", name)
	}
}

func TestEnvironmentNames(t *testing.T) {
	value := config{Environments: map[string]environment{
		"production": {},
		"staging":    {},
	}}

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = stdout })

	value.environmentNames()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := io.Copy(&output, read); err != nil {
		t.Fatal(err)
	}
	if output.String() != "production\nstaging\n" {
		t.Errorf("environmentNames() = %q", output.String())
	}
}
