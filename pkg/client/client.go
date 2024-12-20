package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/goxray/core/network/route"
	"github.com/goxray/core/network/tun"
	tun2socks "github.com/goxray/core/pipe2socks"

	"github.com/jackpal/gateway"
	"github.com/lilendian0x00/xray-knife/xray"
	xapplog "github.com/xtls/xray-core/app/log"
	xcommlog "github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/core"
)

const disconnectTimeout = 30 * time.Second

var (
	// defaultTUNAddress is the address new TUN device will be set up with.
	defaultTUNAddress = &net.IPNet{IP: net.IPv4(192, 18, 0, 1), Mask: net.IPv4Mask(255, 255, 255, 255)}
	// defaultInboundProxy default proxy will be set up for listening on 127.0.0.1:10808.
	defaultInboundProxy = &Proxy{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 10808,
	}

	// DefaultRoutesToTUN will route all system traffic through the TUN.
	DefaultRoutesToTUN = []*route.Addr{
		// Reroute all traffic.
		route.MustParseAddr("0.0.0.0/1"),
		route.MustParseAddr("128.0.0.0/1"),
	}
)

// Config serves configuration for new Client. Empty fields will be set up with defaults values.
//
// It is advised to not configure the client yourself, please use NewClient() with default config values,
// normally you don't have to set these fields yourself.
type Config struct {
	// Gateway to direct outbound traffic. Must be able to reach remote XRay server.
	//
	// Client will determine the system gateway IP automatically,
	// and you don't have to set this field explicitly.
	GatewayIP *net.IP
	// Socks proxy address on which XRay creates inbound proxy.
	InboundProxy *Proxy
	// TUN device address.
	TUNAddress *net.IPNet
	// List of routes to be pointed to TUN device.
	// One exception is explicitly added for XRay remote server IP.
	//
	// Use DefaultRoutesToTUN to route all traffic.
	RoutesToTUN []*route.Addr
	// Whether to allow self-signed certificates or not.
	TLSAllowInsecure bool
	// Pass logger with debug level to observe debug logs.
	Logger *slog.Logger
}

// Client is the actual VPN client. It manages connections, routing and tunneling of the requests.
// It is safe to make a Client connection as it does not change the default system routing and
// just adds on existing infrastructure.
type Client struct {
	cfg Config

	xInst  *core.Instance
	xCfg   *xray.GeneralConfig
	tunnel *tun.Interface

	tunnelStopped chan error
	stopTunnel    func()
}

// Proxy will set up XRay inbound.
type Proxy struct {
	IP   net.IP // Inbound proxy IP (e.g. 127.0.0.1)
	Port int    // Inbound proxy port (e.g. 1080)
}

func (p *Proxy) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

// NewClient initializes default Client with default proxy address.
// If you want more options use Client struct.
func NewClient() (*Client, error) {
	gatewayIP, err := gateway.DiscoverGateway()
	if err != nil {
		return nil, fmt.Errorf("discover gateway: %w", err)
	}

	return &Client{
		cfg: Config{
			GatewayIP:    &gatewayIP,
			InboundProxy: defaultInboundProxy,
			TUNAddress:   defaultTUNAddress,
			RoutesToTUN:  DefaultRoutesToTUN,
			Logger:       slog.New(slog.NewTextHandler(os.Stdout, nil)),
		},
		tunnelStopped: make(chan error),
	}, nil
}

// NewClientWithOpts initializes Client with specified Config. It is recommended to just use NewClient().
func NewClientWithOpts(cfg Config) (*Client, error) {
	client, err := NewClient()
	if err != nil {
		return nil, err
	}

	switch {
	case cfg.GatewayIP != nil:
		client.cfg.GatewayIP = cfg.GatewayIP
	case cfg.InboundProxy != nil:
		client.cfg.InboundProxy = cfg.InboundProxy
	case cfg.TUNAddress != nil:
		client.cfg.TUNAddress = cfg.TUNAddress
	case cfg.RoutesToTUN != nil:
		client.cfg.RoutesToTUN = cfg.RoutesToTUN
	case cfg.Logger != nil:
		client.cfg.Logger = cfg.Logger
	}

	return client, nil
}

// GatewayIP returns gateway IP used to route outbound traffic through.
// It is used to route packets destined to XRay remote server.
func (c *Client) GatewayIP() net.IP {
	return *c.cfg.GatewayIP
}

// TUNAddress returns address the TUN device is set up on.
// Traffic is routed to this TUN device.
func (c *Client) TUNAddress() net.IP {
	return c.cfg.TUNAddress.IP
}

// InboundProxy returns proxy address initialized by XRay core.
// Traffic from TUN device is routed to this proxy.
func (c *Client) InboundProxy() Proxy {
	return *c.cfg.InboundProxy
}

// Connect creates a global tunnel and routes all incoming connections (or traffic specified in Config.RoutesToTUN)
// to the VPN server via newly created defaultInboundProxy.
func (c *Client) Connect(link string) (err error) {
	c.cfg.Logger.Debug("Connecting to tunnel", "cfg", c.cfg)

	c.xInst, c.xCfg, err = c.createXrayProxy(link)
	if err != nil {
		c.cfg.Logger.Error("xray core creation failed", "err", err, "xray_config", c.xCfg)

		return fmt.Errorf("create xray core instance: %v", err)
	}
	c.cfg.Logger.Debug("xray core instance created", "xray_config", c.xCfg)

	c.cfg.Logger.Debug("starting xray core instance")
	if err = c.xInst.Start(); err != nil {
		c.cfg.Logger.Error("xray core instance startup failed", "err", err)

		return fmt.Errorf("start xray core instance: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // Sometimes XRay instance should have a bit more time to set up.
	c.cfg.Logger.Debug("xray core instance started")

	c.cfg.Logger.Debug("Setting up TUN device")
	// Create TUN and route all traffic to it.
	c.tunnel, err = setupTunnel(c.cfg.TUNAddress, c.cfg.TUNAddress.IP, c.cfg.RoutesToTUN)
	if err != nil {
		c.cfg.Logger.Error("TUN creation failed", "err", err)

		return fmt.Errorf("setup TUN device: %v", err)
	}
	c.cfg.Logger.Debug("TUN device created")

	c.cfg.Logger.Debug("adding routes for TUN device")
	// Set XRay remote address to be routed through the default gateway, so that we don't get a loop.
	_ = route.Delete(c.xrayToGatewayRoute()) // In case previous run failed.
	c.cfg.Logger.Debug("deleted dangling routes")
	err = route.Add(c.xrayToGatewayRoute())
	if err != nil {
		c.cfg.Logger.Error("routing xray server IP to default route failed", "err", err, "route", c.xrayToGatewayRoute())

		return fmt.Errorf("add xray server route exception: %v", err)
	}
	c.cfg.Logger.Debug("routing xray server IP to default route")

	var wg sync.WaitGroup
	wg.Add(1)
	var ctx context.Context
	ctx, c.stopTunnel = context.WithCancel(context.Background())
	go func() {
		wg.Done()
		c.tunnelStopped <- tun2socks.Copy(ctx, c.tunnel, c.cfg.InboundProxy.String(), nil)
		c.cfg.Logger.Debug("tunnel pipe closed", "err", err)
	}()
	wg.Wait()
	c.cfg.Logger.Debug("client connected")

	return nil
}

// Disconnect stops all listeners and cleans up route for XRay server.
//
// It will block till all resources are done processing or
// context is cancelled (method also enforces timeout of disconnectTimeout)
func (c *Client) Disconnect(ctx context.Context) error {
	c.stopTunnel()
	err := errors.Join(c.xInst.Close(), c.tunnel.Close(), route.Delete(c.xrayToGatewayRoute()))

	// Waiting till the tunnel actually done with processing connections.
	ctx, cancel := context.WithTimeout(ctx, disconnectTimeout)
	defer cancel()
	select {
	case tunErr := <-c.tunnelStopped:
		err = errors.Join(tunErr, err)
	case <-ctx.Done():
		err = errors.Join(ctx.Err(), err)
	}

	if err != nil {
		c.cfg.Logger.Error("client disconnect encountered failures", "err", err)

		return err
	}

	c.cfg.Logger.Debug("client disconnected")

	return nil
}

// xrayToGatewayRoute is a setup to route VPN requests to gateway.
// Used as exception to not interfere with traffic going to remote XRay instance.
func (c *Client) xrayToGatewayRoute() route.Opts {
	// Append "/32" to match only the XRay server route.
	return route.Opts{Gateway: *c.cfg.GatewayIP, Routes: []*route.Addr{route.MustParseAddr(c.xCfg.Address + "/32")}}
}

// createXrayProxy creates XRay instance from connection link with additional proxy listening on {addr}:{port}.
func (c *Client) createXrayProxy(link string) (*core.Instance, *xray.GeneralConfig, error) {
	protocol, err := xray.ParseXrayConfig(link)
	if err != nil {
		return nil, nil, fmt.Errorf("parse xray config link: %w", err)
	}

	// Make the inbound for local proxy.
	// We will later use it to redirect all traffic from TUN device to this proxy.
	inbound := &xray.Socks{
		Remark:  "XRayProxyListener", // TODO: rename to vpn client name when the project name is defined.
		Address: c.cfg.InboundProxy.IP.String(),
		Port:    strconv.Itoa(c.cfg.InboundProxy.Port),
	}

	svc := &xray.Service{
		Inbound:       inbound,
		LogType:       xapplog.LogType_Console,
		LogLevel:      xRayLogLevel(c.cfg.Logger.Handler()),
		AllowInsecure: c.cfg.TLSAllowInsecure,
	}

	inst, err := svc.MakeXrayInstance(protocol)
	if err != nil {
		return nil, nil, fmt.Errorf("make xray instance: %w", err)
	}

	cfg := protocol.ConvertToGeneralConfig()

	return inst, &cfg, nil
}

// xRayLogLevel maps slog.Level to xray core log level (xcommlog.Severity) by checking Config.Logger level.
func xRayLogLevel(h slog.Handler) xcommlog.Severity {
	ctx := context.Background()
	switch {
	case h.Enabled(ctx, slog.LevelDebug):
		return xcommlog.Severity_Debug
	case h.Enabled(ctx, slog.LevelInfo):
		return xcommlog.Severity_Info
	case h.Enabled(ctx, slog.LevelError):
		return xcommlog.Severity_Error
	case h.Enabled(ctx, slog.LevelWarn):
		return xcommlog.Severity_Warning
	}

	return xcommlog.Severity_Unknown
}

// setupTunnel creates new TUN interface in the system and routes all traffic to it.
func setupTunnel(l *net.IPNet, gw net.IP, rerouteToTun []*route.Addr) (*tun.Interface, error) {
	ifc, err := tun.New("", 1500)
	if err != nil {
		return nil, fmt.Errorf("create tun: %w", err)
	}

	if err = ifc.Up(l, gw); err != nil {
		return nil, fmt.Errorf("setup interface: %w", err)
	}

	if err = route.Add(route.Opts{IfName: ifc.Name(), Routes: rerouteToTun}); err != nil {
		return nil, fmt.Errorf("add route: %w", err)
	}

	return ifc, nil
}
