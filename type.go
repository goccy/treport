package treport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dgraph-io/badger/v2"
	"github.com/goccy/treport/internal/errors"
	treportproto "github.com/goccy/treport/proto"
	"google.golang.org/protobuf/proto"
)

type ScanContext struct {
	context.Context
	Commit       *Commit
	Snapshot     *Snapshot
	Changes      Changes
	Repository   *Repository
	Data         map[string]*treportproto.ScanResponse
	pluginToType map[string]string
}

type ActionType int

func (t ActionType) String() string {
	switch t {
	case Deleted:
		return "Deleted"
	case Added:
		return "Added"
	case Updated:
		return "Updated"
	default:
		return "Updated"
	}
}

const (
	Deleted ActionType = iota
	Added
	Updated
)

type Changes []*Change

type Change struct {
	From   *File
	To     *File
	Action ActionType
}

type FileMode uint32

type File struct {
	Name string
	Mode FileMode
	Size int64
	Hash string
}

type Snapshot struct {
	Hash    string
	Entries []*File
}

type Commit struct {
	Hash         string
	Author       *Signature
	Committer    *Signature
	PGPSignature string
	Message      string
	TreeHash     string
	ParentHashes []string
}

type Signature struct {
	Name  string
	Email string
	When  time.Time
}

type PipelineID string

type Pipeline struct {
	ID        PipelineID
	Repos     []*PipelineRepository
	Config    *PipelineConfig
	CachePath string
}

func (p *Pipeline) Cleanup() {
	for _, repo := range p.Repos {
		repo.Cleanup()
	}
}

type PipelineRepository struct {
	*Repository
	Steps     []*Step
	CachePath string
}

func (r *PipelineRepository) Cleanup() {
	for _, step := range r.Steps {
		step.Cleanup()
	}
}

type Step struct {
	Idx       int
	Plugins   []*Plugin
	CachePath string
}

func (s *Step) Cleanup() {
	for _, plg := range s.Plugins {
		plg.Cleanup()
	}
}

func (s *Step) DeleteCache() error {
	if err := os.RemoveAll(s.CachePath); err != nil {
		return errors.Wrapf(err, "failed to remove step cache %s", s.CachePath)
	}
	return nil
}

func (s *Step) PluginIDs() []string {
	ids := make([]string, 0, len(s.Plugins))
	for _, plg := range s.Plugins {
		ids = append(ids, plg.Repo.ID)
	}
	sort.Strings(ids)
	return ids
}

type PluginID string

type Plugin struct {
	Name      string
	Args      []string
	Repo      *Repository
	CachePath string
	Client    *Client
	cache     *badger.DB
	setup     func([]string) error
}

func (p *Plugin) DeleteCache() error {
	if err := os.RemoveAll(p.CachePath); err != nil {
		return errors.Wrapf(err, "failed to remove step cache %s", p.CachePath)
	}
	return nil
}

func (p *Plugin) Cleanup() {
	p.Client.Stop()
}

func (p *Plugin) Setup(args []string) error {
	p.Args = args
	return p.setup(args)
}

func (p *Plugin) Scan(ctx context.Context, scanctx *ScanContext) error {
	data, err := p.GetCache(scanctx.Commit.Hash)
	if err != nil {
		return errors.Wrapf(err, "failed to get cache")
	}
	if data != nil {
		p.Client.storeResult(data, scanctx)
		return nil
	}
	data, err = p.Client.Scan(ctx, scanctx)
	if err != nil {
		return errors.Stack(err)
	}
	if err := p.StoreCache(scanctx.Commit.Hash, data); err != nil {
		return errors.Wrapf(err, "failed to store cache")
	}
	return nil
}

func (p *Plugin) open() (*badger.DB, error) {
	if err := mkdirIfNotExists(filepath.Dir(p.CachePath)); err != nil {
		return nil, errors.Wrapf(err, "failed to create directory for plugin cache")
	}
	db, err := badger.Open(badger.DefaultOptions(p.CachePath))
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (p *Plugin) GetCache(commitID string) (*treportproto.ScanResponse, error) {
	if p.cache == nil {
		cache, err := p.open()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to open cache DB")
		}
		p.cache = cache
	}
	var cache treportproto.ScanResponse
	if err := p.cache.View(func(tx *badger.Txn) error {
		item, err := tx.Get([]byte(commitID))
		if err != nil {
			return err
		}
		v, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return proto.Unmarshal(v, &cache)
	}); err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &cache, nil
}

func (p *Plugin) StoreCache(commitID string, cache *treportproto.ScanResponse) error {
	b, err := proto.Marshal(cache)
	if err != nil {
		return err
	}
	if p.cache == nil {
		cache, err := p.open()
		if err != nil {
			return errors.Wrapf(err, "failed to open cache DB")
		}
		p.cache = cache
	}
	return p.cache.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(badger.NewEntry([]byte(commitID), b))
	})
}

type PluginVersion struct {
	Name            string
	Version         int
	LastUpdatedTime time.Time
}

type PluginVersionDB struct {
	db *badger.DB
}

func (db *PluginVersionDB) IsUpdated(plg *Plugin) (bool, error) {
	ver, err := db.readVersion(plg)
	if err != nil {
		return false, errors.Wrapf(err, "failed to read plugin version")
	}
	if ver == nil {
		return true, nil
	}
	return plg.Client.mtime.After(ver.LastUpdatedTime), nil
}

func (db *PluginVersionDB) Update(plg *Plugin) error {
	ver, err := db.readVersion(plg)
	if err != nil {
		return errors.Wrapf(err, "failed to update plugin version")
	}
	if ver == nil {
		return db.writeVersion(&PluginVersion{
			Name:            plg.Name,
			Version:         1,
			LastUpdatedTime: plg.Client.mtime,
		})
	}
	ver.Version++
	ver.LastUpdatedTime = plg.Client.mtime
	return db.writeVersion(ver)
}

func (db *PluginVersionDB) readVersion(plg *Plugin) (*PluginVersion, error) {
	var ver PluginVersion
	if err := db.db.View(func(tx *badger.Txn) error {
		item, err := tx.Get([]byte(plg.Name))
		if err != nil {
			return err
		}
		v, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return json.Unmarshal(v, &ver)
	}); err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ver, nil
}

func (db *PluginVersionDB) writeVersion(ver *PluginVersion) error {
	b, err := json.Marshal(ver)
	if err != nil {
		return err
	}
	return db.db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(badger.NewEntry([]byte(ver.Name), b))
	})
}
