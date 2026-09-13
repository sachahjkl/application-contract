package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	digestPattern = regexp.MustCompile(`^ghcr\.io/[a-z0-9_.-]+/[a-z0-9_.-]+@sha256:[0-9a-f]{64}$`)
)

type config struct {
	Application  application            `yaml:"application"`
	Environments map[string]environment `yaml:"environments"`
	Resources    *resources             `yaml:"resources,omitempty"`
	Modules      moduleSet              `yaml:"modules,omitempty"`
	Volume       *volume                `yaml:"volume,omitempty"`
}

type application struct {
	Name       string `yaml:"name"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"healthPath"`
}

type environment struct {
	Domain  string    `yaml:"domain"`
	NoIndex bool      `yaml:"noIndex,omitempty"`
	Modules moduleSet `yaml:"modules,omitempty"`
}

type volume struct {
	MountPath string `yaml:"mountPath"`
}

type resources struct {
	CPU    int `yaml:"cpu"`
	Memory int `yaml:"memory"`
}

type moduleSet struct {
	Config []string `yaml:"config,omitempty"`
	Group  []string `yaml:"group,omitempty"`
	Task   []string `yaml:"task,omitempty"`
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
	if value.Volume != nil {
		mountPath := value.Volume.MountPath
		if !strings.HasPrefix(mountPath, "/") || path.Clean(mountPath) != mountPath || mountPath == "/" {
			return errors.New("volume.mountPath must be a clean absolute path below /")
		}
	}
	if value.Resources != nil && (value.Resources.CPU < 1 || value.Resources.Memory < 1) {
		return errors.New("resources.cpu and resources.memory must be positive integers")
	}
	if len(value.Environments) == 0 {
		return errors.New("environments must declare at least one environment")
	}
	domains := make(map[string]string, len(value.Environments))
	for name, environment := range value.Environments {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("environment name %q must be a lowercase DNS label", name)
		}
		if !validDomain(environment.Domain) {
			return fmt.Errorf("environment %q domain must be a lowercase DNS hostname", name)
		}
		if other, exists := domains[environment.Domain]; exists {
			return fmt.Errorf("environments %q and %q cannot use the same domain", other, name)
		}
		domains[environment.Domain] = name
	}
	return nil
}

func validDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 || strings.HasSuffix(domain, ".") {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !namePattern.MatchString(label) {
			return false
		}
	}
	return true
}

func (value config) githubOutput() {
	fmt.Printf("name=%s\n", value.Application.Name)
	fmt.Printf("health_path=%s\n", value.Application.HealthPath)
	fmt.Printf("volume_enabled=%t\n", value.Volume != nil)
}

func (value config) environmentOutput(name string) error {
	environment, ok := value.Environments[name]
	if !ok {
		return fmt.Errorf("environment %q is not declared", name)
	}
	fmt.Printf("domain=%s\n", environment.Domain)
	fmt.Printf("no_index=%t\n", environment.NoIndex)
	return nil
}

func (value config) environmentNames() {
	names := make([]string, 0, len(value.Environments))
	for name := range value.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println(name)
	}
}

func (value config) nomadVars(environment, image string) error {
	environmentConfig, ok := value.Environments[environment]
	if !ok {
		return fmt.Errorf("environment %q is not declared", environment)
	}
	domain := environmentConfig.Domain
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
	if environmentConfig.NoIndex {
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
		{"resource_cpu", value.resourceCPU()},
		{"resource_memory", value.resourceMemory()},
		{"config_modules", combine(value.Modules.Config, environmentConfig.Modules.Config)},
		{"group_modules", combine(value.Modules.Group, environmentConfig.Modules.Group)},
		{"task_modules", combine(value.Modules.Task, environmentConfig.Modules.Task)},
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

func combine(common, environment []string) []string {
	result := make([]string, 0, len(common)+len(environment))
	result = append(result, common...)
	return append(result, environment...)
}

func (value config) resourceCPU() int {
	if value.Resources == nil {
		return 200
	}
	return value.Resources.CPU
}

func (value config) resourceMemory() int {
	if value.Resources == nil {
		return 256
	}
	return value.Resources.Memory
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
	if _, ok := value.Environments[environment]; !ok {
		return fmt.Errorf("environment %q is not declared", environment)
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
	if args[0] == "environment-output" && len(args) == 2 {
		return value.environmentOutput(args[1])
	}
	if args[0] == "environment-names" && len(args) == 1 {
		value.environmentNames()
		return nil
	}
	if args[0] == "nomad-vars" && len(args) == 3 {
		return value.nomadVars(args[1], args[2])
	}
	if args[0] == "volume-spec" && len(args) == 2 {
		return value.volumeSpec(args[1])
	}
	return errors.New("usage: application-contract {validate|github-output|environment-names|environment-output ENV|nomad-vars ENV IMAGE|volume-spec ENV}")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
