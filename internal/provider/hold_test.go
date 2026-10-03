package provider

import (
	"slices"
	"testing"
)

// A request that only reads builds the catalog once for all its look-ups
// (/api/state took 4s, resolving each agent's models anew); outside one,
// and after a write, it is built again.
func TestHoldBuildsOnce(t *testing.T) {
	n := 0
	build := func() int { n++; return n }
	heldOf("t", build)
	heldOf("t", build)
	if n != 2 {
		t.Fatalf("not held: built %d times, want 2", n)
	}
	release := Hold()
	heldOf("t", build)
	heldOf("t", build)
	if n != 3 {
		t.Fatalf("held: built %d times, want 3", n)
	}
	Changed()
	if heldOf("t", build); n != 4 {
		t.Fatalf("after a write: built %d times, want 4", n)
	}
	release()
	release() // a second release is no second one
	if heldOf("t", build); n != 5 {
		t.Fatalf("released: built %d times, want 5", n)
	}
	if held.holds != 0 {
		t.Fatalf("holds left: %d", held.holds)
	}
}

// A provider saved while a request holds the catalog is in it at once.
func TestHoldSeesWrites(t *testing.T) {
	if err := Save(Provider{ID: "held", Name: "Held", Chat: "https://held.example/v1", Key: "k", Models: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	defer Hold()()
	ids := func() []string {
		var out []string
		for _, e := range Catalog() {
			out = append(out, e.ID)
		}
		return out
	}
	if !slices.Contains(ids(), "held/a") || slices.Contains(ids(), "held/b") {
		t.Fatalf("before: %v", ids())
	}
	if err := Save(Provider{ID: "held", Name: "Held", Chat: "https://held.example/v1", Key: "k", Models: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids(), "held/b") {
		t.Fatalf("a model picked meanwhile is missing: %v", ids())
	}
	if p, m, ok := Resolve("held/b"); !ok || p.ID != "held" || m != "b" {
		t.Fatalf("Resolve: %v %q %v", p.ID, m, ok)
	}
}
