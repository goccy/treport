package treport

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goccy/treport/internal/errors"
)

func CreatePipelines(ctx context.Context, cfg *Config) ([]*Pipeline, error) {
	pluginMap := map[string]*Plugin{}
	for _, plg := range BuiltinPlugins {
		pluginMap[plg.Name] = plg
	}
	for _, repoCfg := range cfg.Plugin.Scanner {
		if _, exists := pluginMap[repoCfg.Name]; exists {
			continue
		}
		repo, err := NewRepository(ctx, cfg.RepoPath(), repoCfg)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create repository with repoCfg: %+v", repoCfg)
		}
		pluginMap[repoCfg.Name] = &Plugin{Repo: repo}
	}
	for _, repoCfg := range cfg.Plugin.Storer {
		if _, exists := pluginMap[repoCfg.Name]; exists {
			continue
		}
		repo, err := NewRepository(ctx, cfg.RepoPath(), repoCfg)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create repository with repoCfg: %+v", repoCfg)
		}
		pluginMap[repoCfg.Name] = &Plugin{Repo: repo}
	}

	pluginVerDB, err := cfg.PluginVersionDB()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get connection to plugin version db")
	}

	pipelines := make([]*Pipeline, 0, len(cfg.Pipelines))
	for _, pipelineCfg := range cfg.Pipelines {
		pipeline := &Pipeline{Config: pipelineCfg}
		for _, repoCfg := range pipelineCfg.Repository {
			repo, err := NewRepository(ctx, cfg.RepoPath(), repoCfg)
			if err != nil {
				return nil, err
			}
			pipelineRepo := &PipelineRepository{Repository: repo}
			for idx, stepCfg := range pipelineCfg.Steps {
				step := &Step{Idx: idx}
				for _, pluginExecCfg := range stepCfg.Plugins {
					plg, exists := pluginMap[pluginExecCfg.Name]
					if !exists {
						return nil, fmt.Errorf("failed to find plugin %s", pluginExecCfg.Name)
					}
					if err := plg.Setup(pluginExecCfg.Args); err != nil {
						return nil, errors.Wrapf(err, "failed to setup plugin")
					}
					step.Plugins = append(step.Plugins, plg)
				}
				pipelineRepo.Steps = append(pipelineRepo.Steps, step)
			}
			pipeline.Repos = append(pipeline.Repos, pipelineRepo)
		}
		pipeline.ID = createPipelineID(pipelineCfg.Strategy, pipeline.Repos[0].Steps)
		pipeline.CachePath = filepath.Join(cfg.CachePath(), string(pipeline.ID))
		for _, repo := range pipeline.Repos {
			repo.CachePath = filepath.Join(pipeline.CachePath, repo.ID)
			for _, step := range repo.Steps {
				step.CachePath = filepath.Join(repo.CachePath, fmt.Sprintf("%03d", step.Idx))
				for _, plg := range step.Plugins {
					plg.CachePath = filepath.Join(step.CachePath, plg.Repo.ID)
				}
			}
		}
		needToDeleteStepCache := false
		for _, repo := range pipeline.Repos {
			for _, step := range repo.Steps {
				if needToDeleteStepCache {
					if err := step.DeleteCache(); err != nil {
						return nil, errors.Wrapf(err, "failed to delete step cache")
					}
					continue
				}
				for _, plg := range step.Plugins {
					isUpdated, err := pluginVerDB.IsUpdated(plg)
					if err != nil {
						return nil, errors.Wrapf(err, "failed to get updated condition for plugin")
					}
					if isUpdated {
						if err := plg.DeleteCache(); err != nil {
							return nil, errors.Wrapf(err, "failed to delete plugin cache")
						}
						needToDeleteStepCache = true
						if err := pluginVerDB.Update(plg); err != nil {
							return nil, errors.Wrapf(err, "failed to update plugin version")
						}
					}
				}
			}
			needToDeleteStepCache = false
		}
		pipelines = append(pipelines, pipeline)
	}
	return pipelines, nil
}

func createPipelineID(strategy Strategy, steps []*Step) PipelineID {
	pluginIDs := []string{string(strategy)}
	for _, step := range steps {
		pluginIDs = append(pluginIDs, step.PluginIDs()...)
	}
	return PipelineID(makeHashID(strings.Join(pluginIDs, ":")))
}
