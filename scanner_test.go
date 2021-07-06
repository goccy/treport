package treport_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/goccy/treport"
)

func TestTreport(t *testing.T) {
	dir := filepath.Join("/", "tmp", "treport")
	scanner := treport.NewScanner(&treport.Config{
		Project: treport.ProjectConfig{
			Path: dir,
		},
		Plugin: &treport.PluginConfig{
			Scanner: []*treport.RepositoryConfig{
				{
					Name: "size",
				},
			},
		},
		Pipelines: []*treport.PipelineConfig{
			{
				Name:     "repo-size",
				Strategy: treport.AllMergeCommit,
				Repository: []*treport.RepositoryConfig{
					{
						Repo: "https://github.com/goccy/go-json",
						Auth: &treport.AuthConfig{
							UserEnv:     "GITHUB_USER",
							PasswordEnv: "GITHUB_TOKEN",
						},
					},
				},
				Steps: []*treport.StepConfig{
					{
						Plugins: []*treport.PluginExecConfig{
							{
								Name: "size",
							},
						},
					},
				},
			},
		},
	})
	if err := scanner.Scan(context.Background()); err != nil {
		t.Fatalf("%+v", err)
	}
}
