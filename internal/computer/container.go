//go:build cgo

package computer

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/afc"
	"github.com/swipt-automation/swipt/internal/log"
)

type containerFiles interface {
	exists(ctx context.Context, file string) (bool, error)
	read(ctx context.Context, file string) ([]byte, error)
	write(ctx context.Context, file string, content []byte) error
	close()
}

type Container struct {
	files containerFiles
}

func OpenContainer(ctx context.Context, udid, bundleID string, logger log.ContextLogger) (*Container, error) {
	session, err := OpenSession(ctx, udid, logger)
	if err != nil {
		return nil, err
	}
	client, err := session.container(ctx, bundleID)
	if err != nil {
		session.Close()
		return nil, err
	}
	return &Container{deviceContainer{session, client}}, nil
}

func OpenGuestContainer(ctx context.Context, socket, bundleID string) (*Container, error) {
	guest := vphoned(socket)
	located, err := guest.call(ctx, "apps.data_dir", map[string]any{"bundle_id": bundleID})
	if err != nil {
		return nil, err
	}
	root, _ := located["data_path"].(string)
	if root == "" {
		return nil, E.New(bundleID, " has no data container")
	}
	return &Container{guestContainer{guest, root}}, nil
}

func (c *Container) Close() {
	c.files.close()
}

func (c *Container) Has(ctx context.Context, file string) (bool, error) {
	return c.files.exists(ctx, path.Join("/", file))
}

func (c *Container) Push(ctx context.Context, source, file string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return c.files.write(ctx, path.Join("/", file), content)
}

func (c *Container) SQLite(ctx context.Context, file, statements string, query bool) ([]map[string]any, error) {
	target := path.Join("/", file)
	exists, err := c.files.exists(ctx, target)
	if err != nil {
		return nil, err
	}
	if query && !exists {
		return []map[string]any{}, nil
	}
	var content []byte
	if exists {
		content, err = c.files.read(ctx, target)
		if err != nil {
			return nil, E.Cause(err, "read ", file)
		}
	}
	work, err := os.MkdirTemp("", "swipt-database-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	database := filepath.Join(work, "database")
	if err = os.WriteFile(database, content, 0o600); err != nil {
		return nil, err
	}
	if query {
		return sqliteQuery(database, statements)
	}
	if err = sqliteExecute(database, statements); err != nil {
		return nil, err
	}
	content, err = os.ReadFile(database)
	if err != nil {
		return nil, err
	}
	return nil, c.files.write(ctx, target, content)
}

type deviceContainer struct {
	session *Session
	client  *afc.Client
}

func (d deviceContainer) exists(_ context.Context, file string) (bool, error) {
	return d.client.Exists(file)
}

func (d deviceContainer) read(_ context.Context, file string) ([]byte, error) {
	return d.client.ReadFile(file)
}

func (d deviceContainer) write(_ context.Context, file string, content []byte) error {
	if err := d.client.MakeDirectory(path.Dir(file)); err != nil {
		return err
	}
	return d.client.Upload(file, bytes.NewReader(content))
}

func (d deviceContainer) close() {
	d.client.Close()
	d.session.Close()
}

type guestContainer struct {
	guest vphoned
	root  string
}

func (g guestContainer) exists(ctx context.Context, file string) (bool, error) {
	if file == "/" {
		return true, nil
	}
	directory := path.Dir(file)
	listed, err := g.guest.call(ctx, "files.list", map[string]any{"path": path.Join(g.root, directory)})
	if err != nil {
		present, presentErr := g.exists(ctx, directory)
		if presentErr != nil || present {
			return false, err
		}
		return false, nil
	}
	entries, _ := listed["entries"].([]any)
	return slices.ContainsFunc(entries, func(entry any) bool {
		fields, _ := entry.(map[string]any)
		return fields["name"] == path.Base(file)
	}), nil
}

func (g guestContainer) read(ctx context.Context, file string) ([]byte, error) {
	return g.guest.readFile(ctx, path.Join(g.root, file))
}

func (g guestContainer) write(ctx context.Context, file string, content []byte) error {
	target := path.Join(g.root, file)
	directory := path.Dir(target)
	if _, err := g.guest.call(ctx, "files.mkdir", map[string]any{"path": directory}); err != nil {
		return err
	}
	if err := g.guest.chown(ctx, directory); err != nil {
		return err
	}
	if err := g.guest.writeFile(ctx, target, content); err != nil {
		return err
	}
	return g.guest.chown(ctx, target)
}

func (guestContainer) close() {}
