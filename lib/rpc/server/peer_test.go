package server

import (
	"time"

	"github.com/gravitational/gravity/lib/rpc/proxy"

	"golang.org/x/net/context"
	. "gopkg.in/check.v1"
)

func (r *S) TestPeerReconnects(c *C) {
	creds := TestCredentials(c)
	store := newPeerStore()
	// Have server go through a proxy so its connection can be manipulated
	upstream := listen(c)
	local := listen(c)
	log := r.Logger.WithField("test", "PeerReconnects")
	proxyAddr := local.Addr().String()
	proxyLink := proxy.New(proxy.NetLink{Local: local, Upstream: upstream.Addr().String()}, log)
	proxyLink.Start()

	srv, err := New(Config{
		Credentials:     creds,
		PeerStore:       store,
		Listener:        upstream,
		commandExecutor: testCommand{"server output"},
	}, log.WithField("server", upstream.Addr()))
	c.Assert(err, IsNil)
	go srv.Serve()
	defer withTestCtx(srv.Stop)

	watchCh := make(chan WatchEvent, 2)
	checkTimeout := 100 * time.Millisecond
	config := PeerConfig{
		Config:             Config{Listener: listen(c)},
		WatchCh:            watchCh,
		HealthCheckTimeout: checkTimeout,
	}
	p := r.newPeer(c, config, proxyAddr, log)
	go p.Serve()
	defer withTestCtx(p.Stop)

	ctx, cancel := context.WithTimeout(context.TODO(), 1*time.Second)
	c.Assert(store.expect(ctx, 1), IsNil)
	cancel()

	select {
	case update := <-watchCh:
		c.Assert(update.Error, IsNil)
		if update.Peer.Addr() != proxyAddr {
			c.Errorf("unknown peer %v", update)
		}
	case <-time.After(5 * time.Second):
		c.Error("timeout waiting for reconnect")
	}

	// Drop connection to server
	proxyLink.Stop()
	// Give the transport enough time to fail. If the interval between reconnects
	// is negligible, the transport might recover and reconnect
	// to the second instance of the proxy bypassing the failed health check.
	time.Sleep(checkTimeout)

	// Restore connection to server
	local = listenAddr(proxyAddr, c)
	proxyLink = proxy.New(proxy.NetLink{Local: local, Upstream: upstream.Addr().String()}, log)
	proxyLink.Start()
	defer proxyLink.Stop()

	select {
	case update := <-watchCh:
		c.Assert(update.Error, IsNil)
		if update.Peer.Addr() != proxyAddr {
			c.Errorf("unknown peer %v", update)
		}
		// Reconnected
	case <-time.After(5 * time.Second):
		c.Error("timeout waiting for reconnect")
	}
}

// withTestCtx calls the provided method passing it a test context with a timeout
func withTestCtx(fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), testContextTimeout)
	defer cancel()
	fn(ctx)
}

// testContextTimeout is the default timeout for the context used in tests
const testContextTimeout = 2 * time.Second
