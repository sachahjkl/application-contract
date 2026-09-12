package main

import "testing"

func TestTrustedDomain(t *testing.T) {
	t.Parallel()

	for _, domain := range []string{
		"sacha.house",
		"froment.software",
		"app.sacha.house",
		"staging.app.sacha.house",
		"app.homelab.sacha.house",
		"app.froment.software",
	} {
		if !trustedDomain(domain) {
			t.Errorf("trustedDomain(%q) = false", domain)
		}
	}
}

func TestVolumeName(t *testing.T) {
	t.Parallel()

	value := config{Application: application{Name: "example"}}
	if name := value.volumeName("production"); name != "example-production-data" {
		t.Errorf("volumeName() = %q", name)
	}
}

func TestUntrustedDomain(t *testing.T) {
	t.Parallel()

	for _, domain := range []string{
		"sacha.house.example.com",
		"example.com",
	} {
		if trustedDomain(domain) {
			t.Errorf("trustedDomain(%q) = true", domain)
		}
	}
}
