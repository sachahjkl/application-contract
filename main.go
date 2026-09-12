package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	domainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	digestPattern = regexp.MustCompile(`^ghcr\.io/[a-z0-9_.-]+/[a-z0-9_.-]+@sha256:[0-9a-f]{64}$`)
	allowedZones  = []string{"sacha.house", "homelab.sacha.house", "froment.software"}
)

type config struct {
	Application application `yaml:"application"`
	Domain      domains     `yaml:"domain"`
	Volume      *volume     `yaml:"volume,omitempty"`
}

type application struct {
	Name       string `yaml:"name"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"healthPath"`
}

type domains struct {
	Production string `yaml:"production"`
	Staging    string `yaml:"staging"`
}

type volume struct {
	MountPath string `yaml:"mountPath"`
}

func load(path string) (config, error) {
	file, err := os.Open(path)
	if err != nil {
		return config{}, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var value config
	if err := decoder.Decode(&value); err != nil {
		return config{}, err
	}
	if err := ensureSingleDocument(decoder); err != nil {
		return config{}, err
	}
	if err := value.validate(); err != nil {
		return config{}, err
	}
	return value, nil
}

func ensureSingleDocument(decoder *yaml.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("application.yaml must contain one YAML document")
}

func (value config) validate() error {
	if !namePattern.MatchString(value.Application.Name) {
		return errors.New("application.name must be a lowercase DNS label")
	}
	if value.Application.Port < 1 || value.Application.Port > 65535 {
		return errors.New("application.port must be an integer from 1 through 65535")
	}
	if !strings.HasPrefix(value.Application.HealthPath, "/") {
		return errors.New("application.healthPath must start with /")
	}
	for environment, domain := range map[string]string{
		"production": value.Domain.Production,
		"staging":    value.Domain.Staging,
	} {
		if len(domain) > 253 || !domainPattern.MatchString(domain) {
			return fmt.Errorf("domain.%s must be a valid lowercase domain", environment)
		}
		if !trustedDomain(domain) {
			return fmt.Errorf("domain.%s is outside the trusted domain zones", environment)
		}
	}
	if value.Domain.Production == value.Domain.Staging {
		return errors.New("staging and production domains must be different")
	}
	if value.Volume != nil {
		mountPath := value.Volume.MountPath
		if !strings.HasPrefix(mountPath, "/") || path.Clean(mountPath) != mountPath || mountPath == "/" {
			return errors.New("volume.mountPath must be a clean absolute path below /")
		}
	}
	return nil
}

func trustedDomain(domain string) bool {
	for _, zone := range allowedZones {
		if domain == zone || strings.HasSuffix(domain, "."+zone) {
			return true
		}
	}
	return false
}

func (value config) githubOutput() {
	fmt.Printf("name=%s\n", value.Application.Name)
	fmt.Printf("health_path=%s\n", value.Application.HealthPath)
	fmt.Printf("staging_domain=%s\n", value.Domain.Staging)
	fmt.Printf("production_domain=%s\n", value.Domain.Production)
	fmt.Printf("volume_enabled=%t\n", value.Volume != nil)
}

func (value config) nomadVars(environment, image string) error {
	domain, err := value.domain(environment)
	if err != nil {
		return err
	}
	if !digestPattern.MatchString(image) {
		return errors.New("image must be an immutable GHCR SHA-256 digest")
	}

	router := value.Application.Name + "-" + environment
	tags := []string{
		"traefik.enable=true",
		fmt.Sprintf("traefik.http.routers.%s.entrypoints=websecure", router),
		fmt.Sprintf("traefik.http.routers.%s.rule=Host(`%s`)", router, domain),
		fmt.Sprintf("traefik.http.routers.%s.tls.certresolver=cloudflare", router),
		fmt.Sprintf("traefik.http.routers.%s.tls.domains[0].main=%s", router, domain),
	}
	if environment == "staging" {
		tags = append(tags,
			fmt.Sprintf("traefik.http.routers.%s.middlewares=%s-noindex", router, router),
			fmt.Sprintf("traefik.http.middlewares.%s-noindex.headers.customresponseheaders.X-Robots-Tag=noindex, nofollow", router),
		)
	}

	values := []struct {
		name  string
		value any
	}{
		{"domain", domain},
		{"environment", environment},
		{"name", value.Application.Name},
		{"health_path", value.Application.HealthPath},
		{"image", image},
		{"port", value.Application.Port},
		{"service_tags", tags},
		{"volume_enabled", value.Volume != nil},
		{"volume_mount_path", value.volumeMountPath()},
		{"volume_name", value.volumeName(environment)},
	}
	for _, entry := range values {
		encoded, err := json.Marshal(entry.value)
		if err != nil {
			return err
		}
		fmt.Printf("%s = %s\n", entry.name, encoded)
	}
	return nil
}

func (value config) volumeMountPath() string {
	if value.Volume == nil {
		return ""
	}
	return value.Volume.MountPath
}

func (value config) volumeName(environment string) string {
	return value.Application.Name + "-" + environment + "-data"
}

func (value config) volumeSpec(environment string) error {
	if _, err := value.domain(environment); err != nil {
		return err
	}
	if value.Volume == nil {
		return errors.New("application.yaml does not declare a volume")
	}
	fmt.Printf("namespace = %q\n", environment)
	fmt.Printf("name = %q\n", value.volumeName(environment))
	fmt.Println("type = \"host\"")
	fmt.Println("plugin_id = \"mkdir\"")
	fmt.Println("parameters = { mode = \"0700\" }")
	fmt.Println("capability {")
	fmt.Println("  access_mode = \"single-node-writer\"")
	fmt.Println("  attachment_mode = \"file-system\"")
	fmt.Println("}")
	return nil
}

func (value config) domain(environment string) (string, error) {
	switch environment {
	case "staging":
		return value.Domain.Staging, nil
	case "production":
		return value.Domain.Production, nil
	default:
		return "", errors.New("environment must be staging or production")
	}
}

func run(args []string) error {
	value, err := load("application.yaml")
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "validate" && len(args) == 1 {
		return nil
	}
	if args[0] == "github-output" && len(args) == 1 {
		value.githubOutput()
		return nil
	}
	if args[0] == "nomad-vars" && len(args) == 3 {
		return value.nomadVars(args[1], args[2])
	}
	if args[0] == "volume-spec" && len(args) == 2 {
		return value.volumeSpec(args[1])
	}
	return errors.New("usage: deployment-config {validate|github-output|nomad-vars ENV IMAGE|volume-spec ENV}")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
