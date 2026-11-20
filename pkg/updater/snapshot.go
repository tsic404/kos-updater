package updater

import (
	"sync"
	"time"

	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/pages"
	"github.com/tsic404/kosd/pkg/upgrade"
)

// recordBackedItems are the declared update-page items that read the preflight
// record: the preflight item and the blocked-upgrade item (C-2, C-4, C-5).
var recordBackedItems = []string{ItemPreflight, ItemBlockedUpgrades}

// recordSnapshot is the one newest-record answer a page read resolves its
// record-backed items from (C-4, C-5). Sharing it is what keeps a preflight
// written between the two item reads from making one page show two upgrade
// plans.
type recordSnapshot struct {
	record   upgrade.Record
	observed time.Time
	failure  *controlplane.Error
	// owed lists the record-backed items of the same page read that have not
	// taken this snapshot yet. An item missing from it forces a fresh read, so a
	// snapshot left behind by a read that stopped early is superseded rather
	// than served to a later read.
	owed map[string]bool
}

// snapshots holds the snapshot of every page read in flight, keyed by the read
// identity the shell assigns (KOSD-C-53). The identity is the shell's, not the
// caller's context, so two reads that share a context still never share one
// snapshot (C-4, C-5, C-88).
type snapshots struct {
	mu      sync.Mutex
	entries map[pages.ReadID]*recordSnapshot
}

// take returns the snapshot the current page read already resolved, when it
// still owes this item an answer, and retires it once every item took it. An
// item the snapshot does not owe, and a read the shell already retired, both
// force a fresh read: an item is never grouped under an identity it cannot
// prove belongs to its own read.
func (s *snapshots) take(id pages.ReadID, live bool, item string) (*recordSnapshot, bool) {
	if !live {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, found := s.entries[id]
	if !found {
		return nil, false
	}
	if !entry.owed[item] {
		delete(s.entries, id)
		return nil, false
	}
	delete(entry.owed, item)
	if len(entry.owed) == 0 {
		delete(s.entries, id)
	}
	return entry, true
}

// keep publishes the snapshot the current item resolved, so the record-backed
// items of the same page read that follow take it instead of reading the record
// again. A read the shell has already retired keeps nothing: its identity is
// gone and nothing may be grouped under it.
func (s *snapshots) keep(id pages.ReadID, live bool, entry *recordSnapshot) {
	if !live || len(entry.owed) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[pages.ReadID]*recordSnapshot, 1)
	}
	s.entries[id] = entry
}
