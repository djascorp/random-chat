package matching

import "sync"

// Queue is a FIFO of waiting peers with O(1) membership checks.
// Every mutation happens under the queue mutex, which is what makes a peer
// impossible to hand out twice even when two joins race.
type Queue struct {
	mu    sync.Mutex
	items []Peer
	inQ   map[string]struct{}
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	return &Queue{inQ: make(map[string]struct{})}
}

// Push appends a peer. It returns false when the peer is already queued.
func (q *Queue) Push(p Peer) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.inQ[p.ID()]; exists {
		return false
	}
	q.items = append(q.items, p)
	q.inQ[p.ID()] = struct{}{}
	return true
}

// Remove takes a specific peer out of the queue, returning it when present.
func (q *Queue) Remove(id string) (Peer, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, p := range q.items {
		if p.ID() == id {
			q.items = append(q.items[:i], q.items[i+1:]...)
			delete(q.inQ, id)
			return p, true
		}
	}
	return nil, false
}

// Contains reports whether a peer is queued.
func (q *Queue) Contains(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.inQ[id]
	return ok
}

// Len returns the number of waiting peers.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// IDs returns a snapshot of the queued identifiers, oldest first.
func (q *Queue) IDs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.items))
	for _, p := range q.items {
		out = append(out, p.ID())
	}
	return out
}

// Snapshot returns the waiting peers oldest first.
func (q *Queue) Snapshot() []Peer {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Peer, len(q.items))
	copy(out, q.items)
	return out
}

// PopPair removes and returns the two oldest peers that `compatible` accepts.
// The queue is only mutated when a pair is found, so a failed attempt leaves the
// waiting order untouched. FIFO order is preserved for everyone else.
func (q *Queue) PopPair(compatible func(a, b Peer) bool) (Peer, Peer, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := 0; i < len(q.items); i++ {
		for j := i + 1; j < len(q.items); j++ {
			if !compatible(q.items[i], q.items[j]) {
				continue
			}
			a, b := q.items[i], q.items[j]
			remaining := make([]Peer, 0, len(q.items)-2)
			for k, p := range q.items {
				if k == i || k == j {
					continue
				}
				remaining = append(remaining, p)
			}
			q.items = remaining
			delete(q.inQ, a.ID())
			delete(q.inQ, b.ID())
			return a, b, true
		}
	}
	return nil, nil, false
}
