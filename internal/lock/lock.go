// Package lock serializes operations on individual sites.
package lock

import "sync"

// Sites is a keyed mutex. Its zero value is ready to use. Do not copy it
// after first use.
type Sites struct {
	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	mu   sync.Mutex
	refs int // includes waiters
}

// Lock acquires id's lock and returns an unlock function to be called once.
func (s *Sites) Lock(id string) func() {
	s.mu.Lock()
	if s.entries == nil {
		s.entries = make(map[string]*entry)
	}
	e := s.entries[id]
	if e == nil {
		e = &entry{}
		s.entries[id] = e
	}
	e.refs++
	s.mu.Unlock()

	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		s.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(s.entries, id)
		}
		s.mu.Unlock()
	}
}
