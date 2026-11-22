package safeio

import (
	"fmt"
)

// MaxSymlinks is the total number of symbolic links one path resolution may
// follow, the same bound as Linux's MAXSYMLINKS (fs/namei.c).
const MaxSymlinks = 40

// MaxTreeDepth is the default nesting depth cap of [PostOrder].
const MaxTreeDepth = 256

var (
	// ErrSymlinkLoop is the ELOOP-like error: one resolution followed more
	// than its budget of symbolic links.
	ErrSymlinkLoop = fmt.Errorf("%w: too many levels of symbolic links", ErrSafeIO)
	// ErrTooDeep is returned when a tree walk nests deeper than its cap.
	ErrTooDeep = fmt.Errorf("%w: tree nested too deeply", ErrSafeIO)
)

// SymlinkBudget counts the symbolic links followed during ONE path
// resolution. A depth limit is not enough: a link whose target names two
// further links, 40 levels deep, costs 2^40 lookups at depth 40. The budget
// is therefore shared (by pointer) by every recursive or iterative step of a
// single lookup and bounds the TOTAL number of links followed. Create one per
// top-level lookup; the zero value allows no hops, use [NewSymlinkBudget].
type SymlinkBudget struct {
	max, n int
}

// NewSymlinkBudget returns a budget of max hops; max <= 0 means
// [MaxSymlinks].
func NewSymlinkBudget(max int) *SymlinkBudget {
	if max <= 0 {
		max = MaxSymlinks
	}
	return &SymlinkBudget{max: max}
}

// Follow records one symlink being followed. It returns nil for the first
// max calls and [ErrSymlinkLoop] afterwards. The receiver must not be nil.
func (b *SymlinkBudget) Follow() error {
	if b.n >= b.max {
		return fmt.Errorf("%w: more than %d links followed", ErrSymlinkLoop, b.max)
	}
	b.n++
	return nil
}

// Followed reports the number of links charged so far.
func (b *SymlinkBudget) Followed() int { return b.n }

// PostOrder walks a tree depth first WITHOUT recursion (so a hostile image
// cannot overflow the goroutine stack, a fatal error no recover can catch) and
// calls visit on every node after all of its children, which is the order a
// recursive delete needs.
//
// key reports a node's identity (a cluster or inode number, never a path) and
// whether the node is a directory to descend into. Each directory identity is
// expanded at most once: a second sighting, whether a cycle (a directory that
// contains its own ancestor) or a hard-linked directory, ends the walk with
// [ErrCycle], so the total work is bounded by the number of distinct
// directories. Nodes with dir == false are leaves and are never tracked (a
// file may legitimately be linked twice). Nesting deeper than maxDepth
// (<= 0 means [MaxTreeDepth]) ends it with [ErrTooDeep]. children lists a
// directory's entries; it must not list "." or "..". The first error from
// children or visit is returned and the walk stops.
func PostOrder[T any](root T, key func(T) (id uint64, dir bool), children func(T) ([]T, error), visit func(T) error, maxDepth int) error {
	if maxDepth <= 0 {
		maxDepth = MaxTreeDepth
	}
	type frame struct {
		node T
		kids []T
		next int
	}
	var seen VisitSet
	var stack []frame
	push := func(n T) error {
		id, dir := key(n)
		if !dir {
			return visit(n)
		}
		if len(stack) >= maxDepth {
			return fmt.Errorf("%w: deeper than %d", ErrTooDeep, maxDepth)
		}
		if err := seen.Check(id); err != nil {
			return err
		}
		kids, err := children(n)
		if err != nil {
			return err
		}
		stack = append(stack, frame{node: n, kids: kids})
		return nil
	}
	if err := push(root); err != nil {
		return err
	}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next < len(top.kids) {
			k := top.kids[top.next]
			top.next++
			if err := push(k); err != nil {
				return err
			}
			continue
		}
		n := top.node
		stack = stack[:len(stack)-1]
		if err := visit(n); err != nil {
			return err
		}
	}
	return nil
}
