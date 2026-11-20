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
	"github.com/swipt-automation/swipt/internal/coredevice/housearrest"
)

type containerFiles interface {
	exists(ctx context.Context, file string) (bool, error)
	read(ctx context.Context, file string) ([]byte, error)
	write(ctx context.Context, file string, content []byte) error
	remove(ctx context.Context, file string) error
	close()
}

type Container struct {
	files containerFiles
}

func (s *Session) Container(ctx context.Context, bundleID string) (*Container, error) {
	if s.virtual != nil {
		return s.virtual.control.container(ctx, bundleID)
	}
	conn, err := s.service(ctx, housearrest.Service)
	if err != nil {
		return nil, err
	}
	err = housearrest.VendContainer(conn, bundleID)
	if err != nil {
		conn.Close()
		if _, found, lookupErr := s.installed(ctx, bundleID); lookupErr == nil && !found {
			return nil, E.Cause(err, bundleID, " is not an installed user app")
		}
		return nil, err
	}
	return &Container{deviceContainer{afc.New(conn)}}, nil
}

func (c *Container) Close() {
	c.files.close()
}

func (c *Container) Has(ctx context.Context, file string) (bool, error) {
	return c.files.exists(ctx, path.Join("/", file))
}

func (c *Container) Write(ctx context.Context, file string, content []byte) error {
	return c.files.write(ctx, path.Join("/", file), content)
}

var (
	sqliteRecoverySidecars = []string{"-journal", "-wal"}
	sqliteCleanupSidecars  = []string{"-journal", "-wal", "-shm"}
)

func (c *Container) SQLite(ctx context.Context, file, statements string, query bool) ([]map[string]any, error) {
	target := path.Join("/", file)
	exists, err := c.files.exists(ctx, target)
	if err != nil {
		return nil, err
	}
	if query && !exists {
		return []map[string]any{}, nil
	}
	work, err := os.MkdirTemp("", "swipt-database-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	database := filepath.Join(work, "database")
	if exists {
		if err = c.pullFile(ctx, target, file, database); err != nil {
			return nil, err
		}
		for _, suffix := range sqliteRecoverySidecars {
			hasSidecar, err := c.files.exists(ctx, target+suffix)
			if err != nil {
				return nil, err
			}
			if hasSidecar {
				if err = c.pullFile(ctx, target+suffix, file+suffix, database+suffix); err != nil {
					return nil, err
				}
			}
		}
	}
	if query {
		return sqliteQuery(database, statements)
	}
	if err = sqliteExecute(database, statements); err != nil {
		return nil, err
	}
	content, err := os.ReadFile(database)
	if err != nil {
		return nil, err
	}
	if err = c.files.write(ctx, target, content); err != nil {
		return nil, err
	}
	for _, suffix := range sqliteCleanupSidecars {
		if err = c.files.remove(ctx, target+suffix); err != nil {
			return nil, E.Cause(err, "remove ", file+suffix)
		}
	}
	return nil, nil
}

func (c *Container) pullFile(ctx context.Context, target, name, destination string) error {
	content, err := c.files.read(ctx, target)
	if err != nil {
		return E.Cause(err, "read ", name)
	}
	return os.WriteFile(destination, content, 0o600)
}

type deviceContainer struct {
	client *afc.Client
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

func (d deviceContainer) remove(_ context.Context, file string) error {
	return d.client.Remove(file)
}

func (d deviceContainer) close() {
	d.client.Close()
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

func (g guestContainer) remove(ctx context.Context, file string) error {
	_, err := g.guest.call(ctx, "files.remove", map[string]any{"path": path.Join(g.root, file)})
	return err
}

func (guestContainer) close() {}
