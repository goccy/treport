package treport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/goccy/treport/internal/errors"
	treportproto "github.com/goccy/treport/proto"
	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/jhump/protoreflect/dynamic"
	"google.golang.org/grpc"
	protobuf "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	Handshake = plugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "TREPORT_PLUGIN",
		MagicCookieValue: "treport",
	}
	BuiltinPluginNames = []string{
		"size",
	}
	BuiltinPlugins []*Plugin
)

func init() {
	for _, pluginName := range BuiltinPluginNames {
		pluginName := pluginName
		var plugin *Plugin
		plugin = &Plugin{
			Name: pluginName,
			Repo: &Repository{
				ID: makeHashID(pluginName),
			},
			setup: func(args []string) error {
				client, err := setupBuiltinPlugin(pluginName, args)
				if err != nil {
					return errors.Wrapf(err, "failed to setup builtin plugin %s", pluginName)
				}
				plugin.Client = client
				return nil
			},
		}
		BuiltinPlugins = append(BuiltinPlugins, plugin)
	}
}

type GRPCScanner interface {
	Scan(*ScanContext) (*Response, error)
}

type ScannerPlugin struct {
	plugin.Plugin
	Scanner GRPCScanner
}

type grpcServer struct {
	Scanner GRPCScanner
}

func (m *grpcServer) Scan(ctx context.Context, req *treportproto.ScanContext) (*treportproto.ScanResponse, error) {
	response := &treportproto.ScanResponse{}
	res, err := m.Scanner.Scan(protoToScanContext(ctx, req))
	if res != nil {
		response.Name = res.name
		response.Data = res.data
		response.Json = res.json
	}
	return response, err
}

func (p *ScannerPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	treportproto.RegisterScannerServer(s, &grpcServer{Scanner: p.Scanner})
	return nil
}

func (p *ScannerPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &Client{grpcClient: treportproto.NewScannerClient(c)}, nil
}

type Logger = hclog.Logger

func Serve(scanner GRPCScanner, logger Logger) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			"treport": &ScannerPlugin{Scanner: scanner},
		},
		GRPCServer: plugin.DefaultGRPCServer,
		Logger:     logger,
	})
}

var (
	ErrNoData = fmt.Errorf("data doesn't exist")
)

func (c *ScanContext) GetData(msg proto.Message) error {
	name := proto.MessageName(msg)
	data, exists := c.Data[name]
	if !exists {
		return ErrNoData
	}
	v := proto.MessageReflect(msg).Interface()
	return anypb.UnmarshalTo(data.Data, v, protobuf.UnmarshalOptions{})
}

type Response struct {
	name string
	data *anypb.Any
	json string
}

func ToResponse(data proto.Message) (*Response, error) {
	name := proto.MessageName(data)
	v, err := anypb.New(proto.MessageReflect(data).Interface())
	if err != nil {
		return nil, err
	}
	msg, err := dynamic.AsDynamicMessage(data)
	if err != nil {
		return nil, err
	}
	b, err := msg.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return &Response{
		name: name,
		data: v,
		json: string(b),
	}, nil
}

type Clients []*Client

func (c Clients) Stop() {
	for _, cc := range c {
		cc := cc
		cc.Stop()
	}
}

type Client struct {
	pluginName   string
	pluginClient *plugin.Client
	grpcClient   treportproto.ScannerClient
	mtime        time.Time
}

func (c *Client) Scan(ctx context.Context, scanctx *ScanContext) (*treportproto.ScanResponse, error) {
	result, err := c.grpcClient.Scan(ctx, scanctx.toProto())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to scan %s", c.pluginName)
	}
	c.storeResult(result, scanctx)
	return result, nil
}

func (c *Client) storeResult(result *treportproto.ScanResponse, scanctx *ScanContext) {
	scanctx.Data[result.Name] = result
	if _, exists := scanctx.pluginToType[c.pluginName]; !exists {
		scanctx.pluginToType[c.pluginName] = result.Name
	}
}

func (c *Client) Stop() {
	c.pluginClient.Kill()
}

func setupBuiltinPlugin(pluginName string, args []string) (*Client, error) {
	cmd := fmt.Sprintf("./internal/plugins/%s/%s", pluginName, pluginName)
	stat, err := os.Stat(cmd)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get stat for %s", cmd)
	}
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          map[string]plugin.Plugin{"treport": &ScannerPlugin{}},
		Cmd:              exec.Command("sh", append([]string{"-c", cmd}, args...)...),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	rpcClient, err := client.Client()
	if err != nil {
		return nil, err
	}
	scannerClient, err := rpcClient.Dispense("treport")
	if err != nil {
		return nil, err
	}
	c, ok := scannerClient.(*Client)
	if !ok {
		return nil, fmt.Errorf("failed to get Client from %T", scannerClient)
	}
	c.pluginName = pluginName
	c.pluginClient = client
	c.mtime = stat.ModTime()
	return c, nil
}
