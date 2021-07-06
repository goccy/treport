package treport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
	"github.com/goccy/treport/proto"
	"github.com/golang/protobuf/ptypes"
)

func toCommit(src *object.Commit) *Commit {
	parentHashes := []string{}
	for _, hash := range src.ParentHashes {
		parentHashes = append(parentHashes, hash.String())
	}
	return &Commit{
		Hash:         src.Hash.String(),
		Author:       toSignature(src.Author),
		Committer:    toSignature(src.Committer),
		PGPSignature: src.PGPSignature,
		Message:      src.Message,
		TreeHash:     src.TreeHash.String(),
		ParentHashes: parentHashes,
	}
}

func toSignature(src object.Signature) *Signature {
	return &Signature{
		Name:  src.Name,
		Email: src.Email,
		When:  src.When,
	}
}

func toSnapshot(src *object.Tree) (*Snapshot, error) {
	entries := []*File{}
	fileIter := src.Files()
	for {
		file, err := fileIter.Next()
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break
		}
		entries = append(entries, toFile(file))
	}
	return &Snapshot{
		Hash:    src.Hash.String(),
		Entries: entries,
	}, nil
}

func toChanges(src object.Changes, fromTree *object.Tree, toTree *object.Tree) (Changes, error) {
	result := Changes{}
	for _, change := range src {
		converted, err := toChange(change, fromTree, toTree)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func toChange(src *object.Change, fromTree *object.Tree, toTree *object.Tree) (*Change, error) {
	action, err := src.Action()
	if err != nil {
		return nil, err
	}
	var (
		from, to *File
	)
	if src.From.Name != "" {
		file, err := fromTree.TreeEntryFile(&src.From.TreeEntry)
		if err != nil {
			return nil, err
		}
		from = toFile(file)
	}
	if src.To.Name != "" {
		file, err := toTree.TreeEntryFile(&src.To.TreeEntry)
		if err != nil {
			return nil, err
		}
		to = toFile(file)
	}
	return &Change{
		From:   from,
		To:     to,
		Action: toAction(action),
	}, nil
}

func toFile(src *object.File) *File {
	return &File{
		Name: src.Name,
		Mode: FileMode(src.Mode),
		Size: src.Blob.Size,
		Hash: src.Blob.Hash.String(),
	}
}

func toAction(action merkletrie.Action) ActionType {
	switch action {
	case merkletrie.Insert:
		return Added
	case merkletrie.Delete:
		return Deleted
	case merkletrie.Modify:
		return Updated
	default:
		return Updated
	}
}

func protoToScanContext(ctx context.Context, src *proto.ScanContext) *ScanContext {
	return &ScanContext{
		Context:  ctx,
		Commit:   protoToCommit(src.Commit),
		Snapshot: protoToSnapshot(src.Snapshot),
		Changes:  protoToChanges(src.Changes),
		Data:     src.Data,
	}
}

func protoToSnapshot(src *proto.Snapshot) *Snapshot {
	entries := []*File{}
	for _, entry := range src.Entries {
		entries = append(entries, protoToFile(entry))
	}
	return &Snapshot{
		Hash:    src.Hash,
		Entries: entries,
	}
}

func protoToChanges(src []*proto.Change) Changes {
	result := Changes{}
	for _, change := range src {
		result = append(result, protoToChange(change))
	}
	return result
}

func protoToChange(src *proto.Change) *Change {
	return &Change{
		Action: protoToAction(src.Action),
		From:   protoToFile(src.From),
		To:     protoToFile(src.To),
	}
}

func protoToAction(action string) ActionType {
	switch action {
	case "Added":
		return Added
	case "Deleted":
		return Deleted
	case "Updated":
		return Updated
	default:
		return Updated
	}
}

func protoToFile(src *proto.File) *File {
	if src == nil {
		return nil
	}
	return &File{
		Name: src.Name,
		Mode: FileMode(src.Mode),
		Size: src.Size,
		Hash: src.Hash,
	}
}

func protoToCommit(src *proto.Commit) *Commit {
	return &Commit{
		Hash:         src.Hash,
		Author:       protoToSignature(src.Author),
		Committer:    protoToSignature(src.Committer),
		PGPSignature: src.PgpSignature,
		Message:      src.Message,
		TreeHash:     src.TreeHash,
		ParentHashes: src.ParentHashes,
	}
}

func protoToSignature(src *proto.Signature) *Signature {
	t, _ := ptypes.Timestamp(src.When)
	return &Signature{
		Name:  src.Name,
		Email: src.Email,
		When:  t,
	}
}
func (c *ScanContext) toProto() *proto.ScanContext {
	return &proto.ScanContext{
		Commit:   c.Commit.toProto(),
		Snapshot: c.Snapshot.toProto(),
		Changes:  c.Changes.toProto(),
		Data:     c.Data,
	}
}

func (s *Snapshot) toProto() *proto.Snapshot {
	entries := []*proto.File{}
	for _, entry := range s.Entries {
		entries = append(entries, entry.toProto())
	}
	return &proto.Snapshot{
		Hash:    s.Hash,
		Entries: entries,
	}
}

func (c Changes) toProto() []*proto.Change {
	result := []*proto.Change{}
	for _, cc := range c {
		result = append(result, cc.toProto())
	}
	return result
}

func (c *Change) toProto() *proto.Change {
	return &proto.Change{
		Action: c.Action.String(),
		From:   c.From.toProto(),
		To:     c.To.toProto(),
	}
}

func (f *File) toProto() *proto.File {
	if f == nil {
		return nil
	}
	return &proto.File{
		Name: f.Name,
		Mode: uint32(f.Mode),
		Size: f.Size,
		Hash: f.Hash,
	}
}

func (c *Commit) toProto() *proto.Commit {
	return &proto.Commit{
		Hash:         c.Hash,
		Author:       c.Author.toProto(),
		Committer:    c.Committer.toProto(),
		PgpSignature: c.PGPSignature,
		Message:      c.Message,
		TreeHash:     c.TreeHash,
		ParentHashes: c.ParentHashes,
	}
}

func (s *Signature) toProto() *proto.Signature {
	t, _ := ptypes.TimestampProto(s.When)
	return &proto.Signature{
		Name:  s.Name,
		Email: s.Email,
		When:  t,
	}
}
