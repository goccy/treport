package treport

import (
	"context"

	"github.com/goccy/treport/internal/errors"
	"golang.org/x/sync/errgroup"
)

type Scanner struct {
	cfg *Config
}

func NewScanner(cfg *Config) *Scanner {
	return &Scanner{cfg: cfg}
}

func (s *Scanner) setupMountPoint() error {
	if err := mkdirIfNotExists(s.cfg.Project.MountPath()); err != nil {
		return errors.Wrapf(err, "failed to create directory for project mount point")
	}
	return nil
}

func (s *Scanner) Scan(ctx context.Context) error {
	if err := s.setupMountPoint(); err != nil {
		return errors.Wrapf(err, "failed to setup mount point")
	}
	pipelines, err := CreatePipelines(ctx, s.cfg)
	if err != nil {
		return errors.Wrapf(err, "failed to create pipelines")
	}
	defer func() {
		for _, pipeline := range pipelines {
			pipeline.Cleanup()
		}
	}()
	var eg errgroup.Group
	for _, pipeline := range pipelines {
		pipeline := pipeline
		eg.Go(func() error {
			return s.scanWithPipeline(ctx, pipeline)
		})
	}
	if err := eg.Wait(); err != nil {
		return errors.Stack(err)
	}
	return nil
}

func (s *Scanner) scanWithPipeline(ctx context.Context, pipeline *Pipeline) error {
	var eg errgroup.Group
	for _, repo := range pipeline.Repos {
		repo := repo
		eg.Go(func() error {
			return s.scanWithPipelineAndRepo(ctx, pipeline, repo)
		})
	}
	if err := eg.Wait(); err != nil {
		return errors.Stack(err)
	}
	return nil
}

func (s *Scanner) scanWithPipelineAndRepo(ctx context.Context, pipeline *Pipeline, repo *PipelineRepository) error {
	for _, step := range repo.Steps {
		var eg errgroup.Group
		for _, plg := range step.Plugins {
			plg := plg
			eg.Go(func() error {
				switch pipeline.Config.Strategy {
				case AllMergeCommit:
					if err := s.scanAllMergeCommits(ctx, plg, repo); err != nil {
						return errors.Wrapf(err, "failed to scan all merge commit")
					}
				case AllCommit:
					if err := s.scanAllCommits(ctx, plg, repo); err != nil {
						return errors.Wrapf(err, "failed to scan all commit")
					}
				case HeadOnly:
					if err := s.scanHeadOnly(ctx, plg, repo); err != nil {
						return errors.Wrapf(err, "failed to scan head only")
					}
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return errors.Stack(err)
		}
	}
	return nil
}

func (s *Scanner) scanAllMergeCommits(ctx context.Context, plg *Plugin, repo *PipelineRepository) error {
	branchCfg, err := repo.Repository.BaseBranch()
	if err != nil {
		return err
	}
	if err := repo.Sync(ctx, branchCfg.Merge); err != nil {
		return errors.Wrapf(err, "failed to sync repository")
	}
	return repo.Repository.AllMergeCommits(ctx, func(scanctx *ScanContext) error {
		if err := plg.Scan(ctx, scanctx); err != nil {
			return errors.Wrapf(err, "failed to scan by %s", plg.Name)
		}
		return nil
	})
}

func (s *Scanner) scanAllCommits(ctx context.Context, plg *Plugin, repo *PipelineRepository) error {
	branchCfg, err := repo.Repository.BaseBranch()
	if err != nil {
		return err
	}
	if err := repo.Sync(ctx, branchCfg.Merge); err != nil {
		return errors.Wrapf(err, "failed to sync repository")
	}
	return repo.Repository.AllCommits(ctx, func(scanctx *ScanContext) error {
		if err := plg.Scan(ctx, scanctx); err != nil {
			return errors.Wrapf(err, "failed to scan by %s", plg.Name)
		}
		return nil
	})
}

func (s *Scanner) scanHeadOnly(ctx context.Context, plg *Plugin, repo *PipelineRepository) error {
	branchCfg, err := repo.Repository.BaseBranch()
	if err != nil {
		return err
	}
	if err := repo.Sync(ctx, branchCfg.Merge); err != nil {
		return errors.Wrapf(err, "failed to sync repository")
	}
	return repo.Repository.HeadOnly(ctx, func(scanctx *ScanContext) error {
		if err := plg.Scan(ctx, scanctx); err != nil {
			return errors.Wrapf(err, "failed to scan by %s", plg.Name)
		}
		return nil
	})
}
