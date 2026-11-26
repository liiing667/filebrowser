package share

import "testing"

type renamePathBackend struct {
	links map[string]*Link
}

func (b *renamePathBackend) All() ([]*Link, error) {
	out := make([]*Link, 0, len(b.links))
	for _, link := range b.links {
		out = append(out, link)
	}
	return out, nil
}

func (b *renamePathBackend) FindByUserID(id uint) ([]*Link, error) {
	var out []*Link
	for _, link := range b.links {
		if link.UserID == id {
			out = append(out, link)
		}
	}
	return out, nil
}

func (b *renamePathBackend) GetByHash(hash string) (*Link, error) {
	return b.links[hash], nil
}

func (b *renamePathBackend) GetPermanent(string, uint) (*Link, error) {
	return nil, nil
}

func (b *renamePathBackend) Gets(string, uint) ([]*Link, error) {
	return nil, nil
}

func (b *renamePathBackend) Save(link *Link) error {
	b.links[link.Hash] = link
	return nil
}

func (b *renamePathBackend) Delete(hash string) error {
	delete(b.links, hash)
	return nil
}

func (b *renamePathBackend) DeleteWithPathPrefix(string, uint) error {
	return nil
}

func TestStorageMovePathPrefix(t *testing.T) {
	backend := &renamePathBackend{links: map[string]*Link{
		"root":     {Hash: "root", Path: "/shared", UserID: 1},
		"child":    {Hash: "child", Path: "/shared/file.txt", UserID: 1},
		"prefix":   {Hash: "prefix", Path: "/shared-next", UserID: 1},
		"other":    {Hash: "other", Path: "/shared/other.txt", UserID: 2},
		"trailing": {Hash: "trailing", Path: "/shared/dir/", UserID: 1},
	}}
	storage := NewStorage(backend)

	if err := storage.MovePathPrefix("/shared/", "/renamed/", 1); err != nil {
		t.Fatalf("MovePathPrefix returned error: %v", err)
	}

	want := map[string]string{
		"root":     "/renamed",
		"child":    "/renamed/file.txt",
		"prefix":   "/shared-next",
		"other":    "/shared/other.txt",
		"trailing": "/renamed/dir",
	}
	for hash, path := range want {
		if got := backend.links[hash].Path; got != path {
			t.Errorf("link %s path = %q, want %q", hash, got, path)
		}
	}
}

func TestRelocateSharePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		old  string
		new  string
		want string
		ok   bool
	}{
		{name: "exact", path: "/a", old: "/a", new: "/b", want: "/b", ok: true},
		{name: "descendant", path: "/a/c", old: "/a", new: "/b", want: "/b/c", ok: true},
		{name: "trailing separators", path: "/a/c/", old: "/a/", new: "/b/", want: "/b/c", ok: true},
		{name: "segment boundary", path: "/ab", old: "/a", new: "/b", ok: false},
		{name: "other path", path: "/c", old: "/a", new: "/b", ok: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := relocateSharePath(tc.path, tc.old, tc.new)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRelocateSharePathCaseFold(t *testing.T) {
	t.Parallel()

	got, ok := relocateSharePathFold("/Shared/File.TXT", "/shared", "/renamed", true)
	if !ok {
		t.Fatal("expected case-insensitive descendant to match")
	}
	if want := "/renamed/File.TXT"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	if _, ok := relocateSharePathFold("/shared-next", "/shared", "/renamed", true); ok {
		t.Fatal("segment boundary matched across case folding")
	}
}
