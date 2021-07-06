package treport

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/dgraph-io/badger/v2"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/goccy/go-yaml"
	"github.com/goccy/treport/internal/errors"
)

const (
	treportRepoURL  = "https://github.com/goccy/treport"
	treportRepoPath = "github.com/goccy/treport"
)

var (
	defaultMountPath = filepath.Join(
		os.Getenv("HOME"),
		".treport.d",
	)
	urlMatcher = regexp.MustCompile(`^http?s://(.+)$`)
)

type Config struct {
	Project   ProjectConfig     `yaml:"project"`
	Plugin    *PluginConfig     `yaml:"plugin"`
	Pipelines []*PipelineConfig `yaml:"pipelines"`
}

func (c *Config) MountPath() string {
	return c.Project.MountPath()
}

func (c *Config) RepoPath() string {
	return filepath.Join(c.MountPath(), "repo")
}

func (c *Config) CachePath() string {
	return filepath.Join(c.MountPath(), "cache")
}

func (c *Config) PluginPath() string {
	return filepath.Join(c.MountPath(), "plugin")
}

func (c *Config) PluginVersionDB() (*PluginVersionDB, error) {
	if err := mkdirIfNotExists(c.PluginPath()); err != nil {
		return nil, errors.Wrapf(err, "failed to create directory for plugin")
	}
	dbPath := filepath.Join(c.PluginPath(), "version")
	db, err := badger.Open(badger.DefaultOptions(dbPath))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open db for plugin version")
	}
	return &PluginVersionDB{db: db}, nil
}

type ProjectConfig struct {
	Path string `yaml:"path"`
}

func (c *ProjectConfig) MountPath() string {
	if c.Path != "" {
		return c.Path
	}
	return defaultMountPath
}

type PluginConfig struct {
	Scanner []*RepositoryConfig `yaml:"scanner"`
	Storer  []*RepositoryConfig `yaml:"storer"`
}

type RepositoryConfig struct {
	Name   string      `yaml:"name"`
	Repo   string      `yaml:"repo"`
	Path   string      `yaml:"path"`
	Branch string      `yaml:"branch"`
	Rev    string      `yaml:"rev"`
	Auth   *AuthConfig `yaml:"auth"`
}

func (c *RepositoryConfig) RepoPath() (string, error) {
	if c.Repo == "" {
		c.Repo = treportRepoURL
		return treportRepoPath, nil
	}
	matches := urlMatcher.FindAllStringSubmatch(c.Repo, -1)
	if len(matches) == 0 {
		return "", ErrInvalidRepositoryPath(c.Repo)
	}
	if len(matches[0]) != 2 {
		return "", ErrInvalidRepositoryPath(c.Repo)
	}
	return matches[0][1], nil
}

func (c *RepositoryConfig) tryUnmarshalNameOnly(b []byte) bool {
	var v string
	if err := yaml.Unmarshal(b, &v); err == nil {
		c.Name = v
		c.Repo = treportRepoURL
		return true
	}
	return false
}

func (c *RepositoryConfig) UnmarshalYAML(b []byte) error {
	if c.tryUnmarshalNameOnly(b) {
		return nil
	}
	var v struct {
		Name   string      `yaml:"name"`
		Repo   string      `yaml:"repo"`
		Path   string      `yaml:"path"`
		Branch string      `yaml:"branch"`
		Rev    string      `yaml:"rev"`
		Auth   *AuthConfig `yaml:"auth"`
	}
	if err := yaml.Unmarshal(b, &v); err != nil {
		return err
	}
	c.Name = v.Name
	c.Repo = v.Repo
	c.Path = v.Path
	c.Branch = v.Branch
	c.Rev = v.Rev
	c.Auth = v.Auth
	if c.Repo == "" {
		c.Repo = treportRepoURL
	}
	return nil
}

type AuthConfig struct {
	UserEnv     string `yaml:"user"`
	PasswordEnv string `yaml:"password"`
}

func (c *AuthConfig) User() string {
	if c == nil {
		return ""
	}
	return os.Getenv(c.UserEnv)
}

func (c *AuthConfig) Password() string {
	if c == nil {
		return ""
	}
	return os.Getenv(c.PasswordEnv)
}

func (c *AuthConfig) BasicAuth() *http.BasicAuth {
	if c.User() == "" || c.Password() == "" {
		return nil
	}
	return &http.BasicAuth{
		Username: c.User(),
		Password: c.Password(),
	}
}

type Strategy string

const (
	AllMergeCommit Strategy = "allMergeCommit"
	AllCommit      Strategy = "allCommit"
	HeadOnly       Strategy = "headOnly"
)

type PipelineConfig struct {
	Name       string              `yaml:"name"`
	Desc       string              `yaml:"desc"`
	Strategy   Strategy            `yaml:"strategy"`
	Repository []*RepositoryConfig `yaml:"repository"`
	Steps      []*StepConfig       `yaml:"steps"`
}

type StepConfig struct {
	Plugins []*PluginExecConfig
}

func (c *StepConfig) tryPluginNameOnly(b []byte) bool {
	var v string
	if err := yaml.Unmarshal(b, &v); err == nil {
		c.Plugins = append(c.Plugins, &PluginExecConfig{
			Name: v,
		})
		return true
	}
	return false
}

func (c *StepConfig) tryPluginNamesOnly(b []byte) bool {
	var v []string
	if err := yaml.Unmarshal(b, &v); err == nil {
		for _, vv := range v {
			c.Plugins = append(c.Plugins, &PluginExecConfig{
				Name: vv,
			})
		}
		return true
	}
	return false
}

func (c *StepConfig) UnmarshalYAML(b []byte) error {
	if c.tryPluginNameOnly(b) {
		return nil
	}
	if c.tryPluginNamesOnly(b) {
		return nil
	}
	var v PluginExecConfig
	if err := yaml.Unmarshal(b, &v); err == nil {
		c.Plugins = append(c.Plugins, &v)
		return nil
	}
	return yaml.Unmarshal(b, &c.Plugins)
}

type PluginExecConfig struct {
	Name string
	Args []string
}

func LoadConfig(path string) (*Config, error) {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(file, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
