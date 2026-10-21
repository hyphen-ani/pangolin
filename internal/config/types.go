package config

const (
	APIVersion = "pangolinhq.dev/v1alpha1"
	Kind       = "Config"

	AlgoRoundRobin       = "round-robin"
	AlgoLeastConnections = "least-connections"

	DefaultWeight = 100
)

type Config struct {
	APIVersion string    `yaml:"apiVersion"`
	Kind       string    `yaml:"kind"`
	Services   []Service `yaml:"services"`
}

type Service struct {
	Name     string   `yaml:"name"`
	Path     string   `yaml:"path"`
	Upstream Upstream `yaml:"upstream"`
	Routes   []Route  `yaml:"routes"`
}

type Upstream struct {
	Scheme    string   `yaml:"scheme"`
	Algorithm string   `yaml:"algorithm"`
	Targets   []Target `yaml:"targets"`
}

type Target struct {
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"`
	Weight *int   `yaml:"weight"`
}

type Route struct {
	Name         string              `yaml:"name"`
	Hosts        []string            `yaml:"hosts"`
	Paths        []string            `yaml:"paths"`
	Methods      []string            `yaml:"methods"`
	Headers      map[string][]string `yaml:"headers"`
	Priority     int                 `yaml:"priority"`
	StripPath    *bool               `yaml:"strip_path"`
	PreserveHost bool                `yaml:"preserve_host"`
}
