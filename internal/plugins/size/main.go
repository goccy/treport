package main

import (
	"os"

	"github.com/goccy/treport"
	sizeproto "github.com/goccy/treport/plugin/size"
	"github.com/hashicorp/go-hclog"
)

type sizeScanner struct {
	logger hclog.Logger
}

func (s *sizeScanner) Scan(ctx *treport.ScanContext) (*treport.Response, error) {
	var v sizeproto.SizeData
	if err := ctx.GetData(&v); err != nil {
		if err != treport.ErrNoData {
			return nil, err
		}
	}
	curSize := v.Size
	s.logger.Debug("current size = ", curSize)
	for _, change := range ctx.Changes {
		switch change.Action {
		case treport.Added:
			curSize += change.To.Size
		case treport.Deleted:
			curSize -= change.From.Size
		case treport.Updated:
			curSize += (change.To.Size - change.From.Size)
		}
	}
	return treport.ToResponse(&sizeproto.SizeData{Size: curSize})
}

//go:generate protoc -Iproto proto/size.proto --go_out=plugins=grpc:../../../plugin/size
func main() {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
		Color:      hclog.AutoColor,
	})
	treport.Serve(&sizeScanner{logger: logger}, logger)
}
