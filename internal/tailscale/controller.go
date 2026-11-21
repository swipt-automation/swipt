package tailscale

import (
	"context"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
)

const logoutTimeout = 30 * time.Second

type Controller struct {
	options    Options
	lifecycle  sync.Mutex
	enabled    bool
	users      int
	node       *Node
	access     sync.Mutex
	generation uint64
}

func NewController(options Options) *Controller {
	c := &Controller{options: options}
	c.reportStored(0)
	return c
}

func (c *Controller) SetEnabled(enabled bool) error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.enabled = enabled
	if enabled {
		return c.start()
	}
	c.stop()
	return nil
}

func (c *Controller) Login() (*Login, error) {
	node, err := c.acquire()
	if err != nil {
		return nil, err
	}
	login, err := node.Login()
	if err != nil {
		c.release()
		return nil, err
	}
	go func() {
		<-login.Done()
		c.release()
	}()
	return login, nil
}

func (c *Controller) Logout() error {
	node, err := c.acquire()
	if err != nil {
		return err
	}
	defer c.release()
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	return node.Logout(ctx)
}

func (c *Controller) RunningNode() *Node {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	return c.node
}

func (c *Controller) acquire() (*Node, error) {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.users++
	err := c.start()
	if err != nil {
		c.users--
		return nil, err
	}
	return c.node, nil
}

func (c *Controller) release() {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.users--
	c.stop()
}

func (c *Controller) start() error {
	if c.node != nil {
		return nil
	}
	generation := c.nextGeneration()
	options := c.options
	options.OnStatus = func(status Status) {
		c.report(generation, status)
	}
	options.OnError = func(err error) {
		c.access.Lock()
		defer c.access.Unlock()
		if generation == c.generation {
			c.options.OnError(err)
		}
	}
	node, err := New(options)
	if err != nil {
		c.reportStored(generation)
		return err
	}
	c.node = node
	return nil
}

func (c *Controller) stop() {
	if c.node == nil || c.enabled || c.users > 0 {
		return
	}
	generation := c.nextGeneration()
	if err := c.node.Close(); err != nil {
		c.options.Logger.Error(E.Cause(err, "close"))
	}
	c.node = nil
	c.reportStored(generation)
}

func (c *Controller) nextGeneration() uint64 {
	c.access.Lock()
	defer c.access.Unlock()
	c.generation++
	return c.generation
}

func (c *Controller) reportStored(generation uint64) {
	status, err := StoredStatus(c.options.Directory)
	if err != nil {
		c.options.Logger.Error(err)
	}
	c.report(generation, status)
}

func (c *Controller) report(generation uint64, status Status) {
	c.access.Lock()
	defer c.access.Unlock()
	if generation == c.generation {
		c.options.OnStatus(status)
	}
}
