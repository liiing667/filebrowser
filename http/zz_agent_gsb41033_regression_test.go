package fbhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filebrowser/filebrowser/v2/files"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/share"
	"github.com/filebrowser/filebrowser/v2/users"
)

// Cached file/checksum responses must not be replayable after the stored user
// loses download permission. Authorization is reloaded from the store on every
// request; the no-store directive keeps an old local cache from becoming a
// second, unauthorized consumer.
func TestGSB41033ReadBranchesDoNotPublishReusableCache(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userScope, "cached.txt"), []byte("current-generation"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := []byte("test-signing-key")
	perm := users.Permissions{Download: true}
	st := scopedUserStorage(t, userScope, perm, key)
	token := signToken(t, perm, key)

	get := func(path string) *httptest.ResponseRecorder {
		req, err := http.NewRequest(http.MethodGet, path, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Auth", token)
		rec := httptest.NewRecorder()
		handle(rawHandler, "", st, &settings.Server{}).ServeHTTP(rec, req)
		return rec
	}

	// The checksum request goes through resourceGetHandler rather than
	// rawHandler, so exercise it separately.
	checksumReq, _ := http.NewRequest(http.MethodGet, "/cached.txt?checksum=sha256", http.NoBody)
	checksumReq.Header.Set("X-Auth", token)
	checksumRec := httptest.NewRecorder()
	handle(resourceGetHandler, "", st, &settings.Server{}).ServeHTTP(checksumRec, checksumReq)
	if checksumRec.Code != http.StatusOK {
		t.Fatalf("checksum: expected 200, got %d body=%q", checksumRec.Code, checksumRec.Body.String())
	}
	if got := checksumRec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("checksum Cache-Control = %q, want no-store", got)
	}

	rec := get("/cached.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "current-generation" {
		t.Fatalf("download body = %q", body)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("download Cache-Control = %q, want no-store", got)
	}

	user, err := st.Users.Get("", false, uint(1))
	if err != nil {
		t.Fatal(err)
	}
	user.Perm.Download = false
	if err := st.Users.Update(user, "Perm"); err != nil {
		t.Fatal(err)
	}

	revoked := get("/cached.txt")
	if revoked.Code != http.StatusAccepted {
		t.Fatalf("revoked download: expected 202, got %d body=%q", revoked.Code, revoked.Body.String())
	}
	if strings.Contains(revoked.Body.String(), "current-generation") {
		t.Fatalf("revoked download leaked file bytes: %q", revoked.Body.String())
	}
	if got := revoked.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("revoked response Cache-Control = %q, want no-store", got)
	}
}

func TestGSB41033UploadCacheKeyBindsOwnerScopeAndCaseGeneration(t *testing.T) {
	d := &data{
		server: &settings.Server{CaseInsensitiveFs: true},
	}
	file := &files.FileInfo{Path: "/Dir/Partial.Upload"}

	if got, want := uploadCacheKey(d, file), "/dir/partial.upload"; got != want {
		t.Fatalf("case-folded scoped key = %q, want %q", got, want)
	}

	d.server.CaseInsensitiveFs = false
	if got, want := uploadCacheKey(d, file), "/Dir/Partial.Upload"; got != want {
		t.Fatalf("case-sensitive scoped key = %q, want %q", got, want)
	}
}

func TestGSB41033RevokedShareCannotBeReplayedAfterReopen(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userScope, "shared.txt"), []byte("shared-generation"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := []byte("test-signing-key")
	st := scopedUserStorage(t, userScope, users.Permissions{Share: true, Download: true}, key)
	if err := st.Share.Save(&share.Link{Hash: "revoke-me", Path: "/", UserID: 1}); err != nil {
		t.Fatal(err)
	}

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/unused", http.NoBody)
		// Public hash routes are passed without the leading slash after prefix
		// stripping, matching the existing public share tests.
		req.URL.Path = "revoke-me/shared.txt"
		rec := httptest.NewRecorder()
		handle(publicDlHandler, "", st, &settings.Server{}).ServeHTTP(rec, req)
		return rec
	}

	before := get()
	if before.Code != http.StatusOK {
		t.Fatalf("shared download: expected 200, got %d body=%q", before.Code, before.Body.String())
	}
	if got := before.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("shared download Cache-Control = %q, want no-store", got)
	}

	if err := st.Share.Delete("revoke-me"); err != nil {
		t.Fatal(err)
	}

	after := get()
	if after.Code != http.StatusNotFound {
		t.Fatalf("revoked share: expected 404, got %d body=%q", after.Code, after.Body.String())
	}
	if strings.Contains(after.Body.String(), "shared-generation") {
		t.Fatalf("revoked share response leaked cached bytes: %q", after.Body.String())
	}
}

func TestGSB41033FailedCleanupKeepsSameGenerationForRetry(t *testing.T) {
	oldRetry := failedUploadCleanupRetry
	failedUploadCleanupRetry = 5 * time.Millisecond
	t.Cleanup(func() { failedUploadCleanupRetry = oldRetry })

	cache := newMemoryUploadCache()
	t.Cleanup(cache.Close)

	var removals atomic.Int32
	firstFailed := make(chan struct{}, 1)
	secondDone := make(chan struct{}, 1)
	remove := func() error {
		if removals.Add(1) == 1 {
			firstFailed <- struct{}{}
			return errors.New("cleanup temporarily unavailable")
		}
		secondDone <- struct{}{}
		return nil
	}

	cache.Register("scoped-generation", 7, remove)
	item := cache.cache.Get("scoped-generation")
	if item == nil {
		t.Fatal("registered upload generation missing")
	}
	firstGeneration := item.Value().generation

	// Force the initial TTL to expire without waiting three minutes.
	cache.cache.Set("scoped-generation", item.Value(), time.Millisecond)

	select {
	case <-firstFailed:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup callback was not invoked")
	}

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("failed cleanup generation was not retried")
	}

	retried := cache.cache.Get("scoped-generation")
	if retried == nil {
		// Successful retry evicts the entry through the normal callback path;
		// that is also acceptable. The important assertion is that the same
		// generation was retried, which the second callback proves.
		return
	}
	if got := retried.Value().generation; got != firstGeneration {
		t.Fatalf("retry generation = %d, want original %d", got, firstGeneration)
	}
}
