//go:build cgo

package automation

import (
	"slices"
	"strconv"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/jsc"
)

const keptSnapshots = 64

type searchable interface {
	search(engine *engine, scope string, encoded []byte, all bool, callback jsc.Value) (any, error)
}

type snapshotStore struct {
	access    sync.Mutex
	snapshots map[string]searchable
	order     []string
	next      int
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{snapshots: make(map[string]searchable)}
}

func (s *snapshotStore) keep(snapshot searchable) string {
	s.access.Lock()
	defer s.access.Unlock()
	s.next++
	id := strconv.Itoa(s.next)
	s.snapshots[id] = snapshot
	s.order = append(s.order, id)
	if len(s.order) > keptSnapshots {
		delete(s.snapshots, s.order[0])
		s.order = slices.Delete(s.order, 0, 1)
	}
	return id
}

func (s *snapshotStore) drop(id string) {
	s.access.Lock()
	defer s.access.Unlock()
	if index := slices.Index(s.order, id); index >= 0 {
		delete(s.snapshots, id)
		s.order = slices.Delete(s.order, index, index+1)
	}
}

func (s *snapshotStore) get(id string) (searchable, error) {
	s.access.Lock()
	defer s.access.Unlock()
	snapshot, found := s.snapshots[id]
	if !found {
		return nil, E.Extend(ErrInvalid, "the snapshot was dropped to keep the newest ", keptSnapshots, "; read the screen again")
	}
	index := slices.Index(s.order, id)
	s.order = append(slices.Delete(s.order, index, index+1), id)
	return snapshot, nil
}
