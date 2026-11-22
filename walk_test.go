package safeio

import (
	"errors"
	"reflect"
	"testing"
)

func TestSymlinkBudgetTotalNotDepth(t *testing.T) {
	// Each link resolves two more: depth stays 40, total work is 2^40.
	b := NewSymlinkBudget(0)
	var lookups int
	var resolve func(level int) error
	resolve = func(level int) error {
		lookups++
		if lookups > 1<<20 {
			t.Fatalf("budget did not stop the fan-out: %d lookups", lookups)
		}
		if level == 40 {
			return nil
		}
		for i := 0; i < 2; i++ {
			if err := b.Follow(); err != nil {
				return err
			}
			if err := resolve(level + 1); err != nil {
				return err
			}
		}
		return nil
	}
	err := resolve(0)
	if !errors.Is(err, ErrSymlinkLoop) || !errors.Is(err, ErrSafeIO) {
		t.Fatalf("err = %v, want ErrSymlinkLoop", err)
	}
	if b.Followed() != MaxSymlinks || lookups > 2*MaxSymlinks+2 {
		t.Fatalf("followed %d, lookups %d", b.Followed(), lookups)
	}
}

type tn struct {
	id   uint64
	dir  bool
	name string
}

func walk(root tn, kids map[uint64][]tn, depth int) ([]string, error) {
	var order []string
	err := PostOrder(root,
		func(n tn) (uint64, bool) { return n.id, n.dir },
		func(n tn) ([]tn, error) { return kids[n.id], nil },
		func(n tn) error { order = append(order, n.name); return nil }, depth)
	return order, err
}

func TestPostOrder(t *testing.T) {
	kids := map[uint64][]tn{
		1: {{2, true, "a"}, {9, false, "f"}, {9, false, "f2"}},
		2: {{3, true, "b"}, {10, false, "g"}},
	}
	got, err := walk(tn{1, true, "root"}, kids, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b", "g", "a", "f", "f2", "root"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestPostOrderCycle(t *testing.T) {
	kids := map[uint64][]tn{1: {{2, true, "a"}}, 2: {{1, true, "up"}}}
	if _, err := walk(tn{1, true, "root"}, kids, 0); !errors.Is(err, ErrCycle) {
		t.Fatalf("err = %v, want ErrCycle", err)
	}
	kids = map[uint64][]tn{1: {{1, true, "self"}}}
	if _, err := walk(tn{1, true, "root"}, kids, 0); !errors.Is(err, ErrCycle) {
		t.Fatalf("self: err = %v", err)
	}
}

func TestPostOrderDepth(t *testing.T) {
	kids := map[uint64][]tn{}
	for i := uint64(1); i < 1000; i++ {
		kids[i] = []tn{{i + 1, true, "d"}}
	}
	if _, err := walk(tn{1, true, "root"}, kids, 0); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("err = %v, want ErrTooDeep", err)
	}
	if _, err := walk(tn{1, true, "root"}, kids, 2000); err != nil {
		t.Fatalf("deep walk with cap 2000: %v", err)
	}
}

func TestPostOrderErrors(t *testing.T) {
	boom := errors.New("boom")
	err := PostOrder(tn{1, true, "r"},
		func(n tn) (uint64, bool) { return n.id, n.dir },
		func(tn) ([]tn, error) { return nil, boom },
		func(tn) error { return nil }, 0)
	if !errors.Is(err, boom) {
		t.Fatalf("children err = %v", err)
	}
	err = PostOrder(tn{1, false, "r"},
		func(n tn) (uint64, bool) { return n.id, n.dir },
		func(tn) ([]tn, error) { return nil, nil },
		func(tn) error { return boom }, 0)
	if !errors.Is(err, boom) {
		t.Fatalf("visit err = %v", err)
	}
	// A failing visit of a directory (after its children) stops the walk.
	var visited []string
	err = PostOrder(tn{1, true, "r"},
		func(n tn) (uint64, bool) { return n.id, n.dir },
		func(n tn) ([]tn, error) {
			if n.id == 1 {
				return []tn{{2, false, "f"}}, nil
			}
			return nil, nil
		},
		func(n tn) error { visited = append(visited, n.name); return boom }, 0)
	if !errors.Is(err, boom) || len(visited) != 1 || visited[0] != "f" {
		t.Fatalf("dir visit: err = %v, visited %v", err, visited)
	}
	err = PostOrder(tn{1, true, "r"},
		func(n tn) (uint64, bool) { return n.id, n.dir },
		func(tn) ([]tn, error) { return nil, nil },
		func(tn) error { return boom }, 0)
	if !errors.Is(err, boom) {
		t.Fatalf("root dir visit err = %v", err)
	}
}

func TestPostOrderDepthBoundary(t *testing.T) {
	kids := map[uint64][]tn{1: {{2, true, "b"}}, 2: {{3, true, "c"}}, 3: {{4, true, "d"}}}
	if _, err := walk(tn{1, true, "a"}, kids, 4); err != nil {
		t.Fatalf("4 nested directories with cap 4: %v", err)
	}
	if _, err := walk(tn{1, true, "a"}, kids, 3); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("4 nested directories with cap 3: err = %v", err)
	}
}
