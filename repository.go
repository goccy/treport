package treport

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/goccy/treport/internal/errors"
	treportproto "github.com/goccy/treport/proto"
)

type Repository struct {
	*git.Repository
	ID      string
	cfg     *RepositoryConfig
	gitCfg  *config.Config
	fetched bool
}

func NewRepository(ctx context.Context, mountPath string, cfg *RepositoryConfig) (*Repository, error) {
	repoPath, err := cfg.RepoPath()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get repository path")
	}
	repoPath = filepath.Join(mountPath, repoPath)
	repo, err := newRepo(ctx, repoPath, cfg)
	if err != nil {
		return nil, errors.Stack(err)
	}
	gitCfg, err := repo.Config()
	if err != nil {
		return nil, err
	}
	return &Repository{
		ID:         makeHashID(repoPath),
		Repository: repo,
		cfg:        cfg,
		gitCfg:     gitCfg,
	}, nil
}

func newRepo(ctx context.Context, repoPath string, cfg *RepositoryConfig) (*git.Repository, error) {
	if !existsPath(repoPath) {
		if err := mkdirForClone(repoPath); err != nil {
			return nil, errors.Wrap(err, "failed to create directory for cloning repository")
		}
		repo, err := git.PlainCloneContext(ctx, repoPath, false, &git.CloneOptions{
			URL:  cfg.Repo,
			Auth: cfg.Auth.BasicAuth(),
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to clone repository. url:%s auth:%v", cfg.Repo, cfg.Auth.BasicAuth())
		}
		return repo, nil
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open repository")
	}
	return repo, nil
}

func (r *Repository) pullRequestHeads() (map[string]*plumbing.Reference, error) {
	branchIter, err := r.Branches()
	if err != nil {
		return nil, err
	}

	pullRequestHeads := map[string]*plumbing.Reference{}
	for {
		branch, err := branchIter.Next()
		if err != nil {
			if err == io.EOF {
				return pullRequestHeads, nil
			}
			return nil, err
		}
		if strings.HasPrefix(string(branch.Name()), "refs/heads/pull/") {
			pullRequestHeads[branch.Hash().String()] = branch
		}
	}
	return pullRequestHeads, nil
}

func (r *Repository) HeadOnly(ctx context.Context, cb func(*ScanContext) error) error {
	iter, err := r.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		return errors.Wrapf(err, "failed to get log")
	}

	commit, err := iter.Next()
	if err != nil {
		if err != io.EOF {
			return errors.Wrapf(err, "failed to get commit object")
		}
		return nil
	}

	scanctx := &ScanContext{
		Data:         map[string]*treportproto.ScanResponse{},
		pluginToType: map[string]string{},
	}
	curTree, err := commit.Tree()
	if err != nil {
		return errors.Wrapf(err, "failed to get worktree")
	}
	snapshot, err := toSnapshot(curTree)
	if err != nil {
		return errors.Wrapf(err, "failed to convert snapshot")
	}
	scanctx.Commit = toCommit(commit)
	scanctx.Snapshot = snapshot
	if err := cb(scanctx); err != nil {
		return errors.Stack(err)
	}
	return nil
}

func (r *Repository) AllCommits(ctx context.Context, cb func(*ScanContext) error) error {
	iter, err := r.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		return err
	}
	allCommits := []*object.Commit{}
	for {
		commit, err := iter.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		allCommits = append(allCommits, commit)
	}

	scanctx := &ScanContext{
		Data:         map[string]*treportproto.ScanResponse{},
		pluginToType: map[string]string{},
	}
	var prevTree *object.Tree
	for i := len(allCommits) - 1; i > 0; i-- {
		commit := allCommits[i]
		if prevTree == nil {
			// first PR
			tree, err := r.firstTree(commit)
			if err != nil {
				return err
			}
			prevTree = tree
		}
		curTree, err := commit.Tree()
		if err != nil {
			return err
		}
		changes, err := prevTree.DiffContext(ctx, curTree)
		if err != nil {
			return err
		}
		convertedChanges, err := toChanges(changes, prevTree, curTree)
		if err != nil {
			return err
		}
		snapshot, err := toSnapshot(curTree)
		if err != nil {
			return err
		}
		scanctx.Commit = toCommit(commit)
		scanctx.Snapshot = snapshot
		scanctx.Changes = convertedChanges
		if err := cb(scanctx); err != nil {
			return err
		}
		prevTree = curTree
	}
	return nil
}

func (r *Repository) AllMergeCommits(ctx context.Context, cb func(*ScanContext) error) error {
	prHeads, err := r.pullRequestHeads()
	if err != nil {
		return err
	}

	iter, err := r.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		return err
	}
	prCommits := []*object.Commit{}
	for {
		commit, err := iter.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		if commit.NumParents() <= 1 {
			continue
		}

		commitIter := commit.Parents()
		isDirectParent := true
		isPRCommit := false
		for {
			parent, err := commitIter.Next()
			if err != nil {
				if err != io.EOF {
					return err
				}
				break
			}
			if !isDirectParent {
				if _, exists := prHeads[parent.Hash.String()]; exists {
					isPRCommit = true
				}
			}
			isDirectParent = false
		}
		if !isPRCommit {
			continue
		}
		prCommits = append(prCommits, commit)
	}

	scanctx := &ScanContext{
		Data:         map[string]*treportproto.ScanResponse{},
		pluginToType: map[string]string{},
	}
	var prevTree *object.Tree
	for i := len(prCommits) - 1; i > 0; i-- {
		commit := prCommits[i]
		if prevTree == nil {
			// first PR
			tree, err := r.firstTree(commit)
			if err != nil {
				return err
			}
			prevTree = tree
		}
		curTree, err := commit.Tree()
		if err != nil {
			return err
		}
		changes, err := prevTree.DiffContext(ctx, curTree)
		if err != nil {
			return err
		}
		convertedChanges, err := toChanges(changes, prevTree, curTree)
		if err != nil {
			return err
		}
		snapshot, err := toSnapshot(curTree)
		if err != nil {
			return err
		}
		scanctx.Commit = toCommit(commit)
		scanctx.Snapshot = snapshot
		scanctx.Changes = convertedChanges
		if err := cb(scanctx); err != nil {
			return err
		}
		prevTree = curTree
	}
	return nil
}

func (r *Repository) firstTree(commit *object.Commit) (*object.Tree, error) {
	commitIter := commit.Parents()
	firstParent, err := commitIter.Next()
	if err != nil {
		return nil, err
	}
	firstTree, err := firstParent.Tree()
	if err != nil {
		return nil, err
	}
	return firstTree, nil
}

func (r *Repository) BaseBranch() (*config.Branch, error) {
	cfg, err := r.Config()
	if err != nil {
		return nil, err
	}
	defaultBranch := cfg.Init.DefaultBranch
	if defaultBranch != "" {
		return r.Branch(defaultBranch)
	}
	if len(cfg.Branches) != 1 {
		return nil, fmt.Errorf("failed to find base branch")
	}
	for branch := range cfg.Branches {
		return r.Branch(branch)
	}
	return nil, fmt.Errorf("failed to find base branch")
}

func (r *Repository) Sync(ctx context.Context, branch plumbing.ReferenceName) error {
	if err := r.syncRemoteBranches(ctx); err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	if err := wt.Checkout(&git.CheckoutOptions{Branch: branch}); err != nil {
		return err
	}
	if err := wt.PullContext(ctx, &git.PullOptions{
		Auth: r.cfg.Auth.BasicAuth(),
	}); err != nil {
		if err != git.NoErrAlreadyUpToDate {
			return err
		}
	}
	return nil
}

func (r *Repository) syncRemoteBranches(ctx context.Context) error {
	branch, err := r.BaseBranch()
	if err != nil {
		return err
	}
	return r.fetch(ctx, branch)
}

func (r *Repository) fetch(ctx context.Context, branch *config.Branch) error {
	if r.fetched {
		return nil
	}
	if err := r.FetchContext(ctx, &git.FetchOptions{
		RemoteName: branch.Remote,
		RefSpecs:   []config.RefSpec{"+refs/*:refs/heads/*", "HEAD:refs/heads/HEAD"},
		Auth:       r.cfg.Auth.BasicAuth(),
	}); err != nil {
		if err != git.NoErrAlreadyUpToDate {
			return err
		}
	}
	r.fetched = true
	return nil
}
