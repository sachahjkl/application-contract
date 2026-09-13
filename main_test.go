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

func TestValidDomain(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"example.sacha.house": true,
		"example.com":         true,
		"example":             false,
		"Example.com":         false,
		"-example.com":        false,
		"example-.com":        false,
		"example.com.":        false,
		"example.com/path":    false,
		"example.com:443":     false,
		"example` .com":       false,
	}

	for domain, expected := range tests {
		domain := domain
		expected := expected
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			if actual := validDomain(domain); actual != expected {
				t.Errorf("validDomain(%q) = %t, want %t", domain, actual, expected)
			}
		})
	}
}

func TestDuplicateEnvironmentDomain(t *testing.T) {
	t.Parallel()

	value := config{
		Application: application{Name: "example", Port: 8080, HealthPath: "/health"},
		Environments: map[string]environment{
			"staging":    {Domain: "example.com"},
			"production": {Domain: "example.com"},
		},
	}
	if err := value.validate(); err == nil {
		t.Fatal("validate() accepted a duplicate environment domain")
	}
}
