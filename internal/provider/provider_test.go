package provider

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/google/go-github/v69/github"
)

// gitTempDir creates a temp directory whose read-only .git objects are made
// writable before t.TempDir cleanup runs on Windows.
func gitTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_ = os.Chmod(p, 0o755)
			} else {
				_ = os.Chmod(p, 0o644)
			}
			return nil
		})
	})
	return dir
}

func initHub(t *testing.T) string {
	t.Helper()
	src := gitTempDir(t)
	r, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("init", &git.CommitOptions{Author: testSig()}); err != nil {
		t.Fatal(err)
	}
	return src
}

func testSig() *object.Signature {
	return &object.Signature{Name: "test", Email: "test@localhost", When: time.Now()}
}

func TestClone(t *testing.T) {
	src := initHub(t)
	g := GenericGit{Remote: src}
	dest := filepath.Join(gitTempDir(t), "team-repo")
	if err := g.Clone(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}
}

func TestPushBranch(t *testing.T) {
	src := initHub(t)
	g := GenericGit{Remote: src}
	clone := filepath.Join(gitTempDir(t), "clone")
	if err := g.Clone(clone); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(clone, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "skills", "demo.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(clone, "add skill"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch(clone, "aiw/push-test"); err != nil {
		t.Fatal(err)
	}
	if err := g.PushBranch(clone, "aiw/push-test"); err != nil {
		t.Fatal(err)
	}
	origin, err := git.PlainOpen(src)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := origin.Reference(plumbing.NewBranchReferenceName("aiw/push-test"), false)
	if err != nil {
		t.Fatalf("branch not pushed: %v", err)
	}
	if ref.Hash().IsZero() {
		t.Fatal("pushed branch has zero hash")
	}
}

func TestUpdateClone(t *testing.T) {
	src := initHub(t)
	g := GenericGit{Remote: src}
	clone := filepath.Join(gitTempDir(t), "clone")
	if err := g.Clone(clone); err != nil {
		t.Fatal(err)
	}
	// New commit on origin after the clone was made.
	if err := os.MkdirAll(filepath.Join(src, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "fresh.md"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := git.PlainOpen(src)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("fresh", &git.CommitOptions{Author: testSig()}); err != nil {
		t.Fatal(err)
	}
	originHead, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}

	if err := UpdateClone(clone); err != nil {
		t.Fatal(err)
	}
	cloneRepo, err := git.PlainOpen(clone)
	if err != nil {
		t.Fatal(err)
	}
	cloneHead, err := cloneRepo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if cloneHead.Hash() != originHead.Hash() {
		t.Fatalf("clone at %s, origin at %s", cloneHead.Hash(), originHead.Hash())
	}
}

func TestNewProviderDetection(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/org/repo.git",
		"https://GITHUB.COM/org/repo",
		"git@github.com:org/repo.git",
		"ssh://git@github.com/org/repo.git",
	} {
		r, err := New(remote)
		if err != nil {
			t.Fatalf("New(%q): %v", remote, err)
		}
		gh, ok := r.(*GitHub)
		if !ok {
			t.Fatalf("New(%q) should give *GitHub, got %T", remote, r)
		}
		if gh.owner != "org" || gh.name != "repo" {
			t.Fatalf("New(%q) parsed owner=%q name=%q", remote, gh.owner, gh.name)
		}
	}
	r, err := New("https://gitlab.com/org/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.(GenericGit); !ok {
		t.Fatalf("unknown host should give GenericGit, got %T", r)
	}
}

func TestAuthForRemoteScopedToGitHubHTTPS(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret-token")
	if auth := authForRemote("https://github.com/org/repo"); auth == nil || auth.Password != "secret-token" {
		t.Fatalf("expected auth for https github.com, got %+v", auth)
	}
	for _, remote := range []string{
		"https://gitlab.com/org/repo",
		"http://github.com/org/repo",
		"https://evil.example.com/github.com/org/repo",
		"git@github.com:org/repo.git",
		"/local/path/repo",
	} {
		if auth := authForRemote(remote); auth != nil {
			t.Fatalf("must not attach GitHub token to %q, got %+v", remote, auth)
		}
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok1")
	if got := githubToken(); got != "tok1" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "tok2")
	if got := githubToken(); got != "tok2" {
		t.Fatalf("got %q", got)
	}
}

type handlerTransport struct{ h http.Handler }

func (rt handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.h.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

func mockGitHubClient(h http.Handler) *github.Client {
	api := github.NewClient(&http.Client{Transport: handlerTransport{h: h}})
	api, _ = api.WithEnterpriseURLs("https://api.example.test/api-v3/", "https://api.example.test/api-v3/")
	return api
}

func TestOpenPR(t *testing.T) {
	var prBody *github.NewPullRequest
	api := http.NewServeMux()
	api.HandleFunc("GET /repos/org/repo", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"default_branch":"main"}`))
	})
	api.HandleFunc("POST /repos/org/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if err := decodeBody(r, &prBody); err != nil {
			t.Errorf("bad PR body: %v", err)
		}
		w.Write([]byte(`{"html_url":"https://github.com/org/repo/pull/1","number":1}`))
	})
	api.HandleFunc("POST /repos/org/repo/pulls/1/requested_reviewers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"number":1}`))
	})
	mux := http.NewServeMux()
	mux.Handle("/api-v3/", http.StripPrefix("/api-v3", api))

	gh := &GitHub{
		GenericGit: GenericGit{Remote: "https://github.com/org/repo"},
		token:      "test-token",
		owner:      "org",
		name:       "repo",
		api:        mockGitHubClient(mux),
	}
	url, err := gh.OpenPR("aiw/test", "Add go-tips skill", []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/org/repo/pull/1" {
		t.Fatalf("got %q", url)
	}
	if *prBody.Head != "aiw/test" || *prBody.Base != "main" || *prBody.Title != "Add go-tips skill" {
		t.Fatalf("PR body: %+v", prBody)
	}
}

func TestListOpenPRs(t *testing.T) {
	api := http.NewServeMux()
	api.HandleFunc("GET /repos/org/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state filter not sent, got %q", got)
		}
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`[
				{"number":2,"title":"Fix rule","head":{"ref":"aiw/bob-2"},"user":{"login":"bob"},"html_url":"https://github.com/org/repo/pull/2"}
			]`))
			return
		}
		w.Header().Set("Link", `<https://api.example.test/api-v3/repos/org/repo/pulls?page=2&state=open>; rel="next"`)
		w.Write([]byte(`[
			{"number":1,"title":"Add skill","head":{"ref":"aiw/alice-1"},"user":{"login":"alice"},"html_url":"https://github.com/org/repo/pull/1"}
		]`))
	})
	mux := http.NewServeMux()
	mux.Handle("/api-v3/", http.StripPrefix("/api-v3", api))

	gh := &GitHub{
		GenericGit: GenericGit{Remote: "https://github.com/org/repo"},
		token:      "test-token",
		owner:      "org",
		name:       "repo",
		api:        mockGitHubClient(mux),
	}
	prs, err := gh.ListOpenPRs()
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 || prs[0].Author != "alice" || prs[0].Head != "aiw/alice-1" || prs[1].Head != "aiw/bob-2" {
		t.Fatalf("got %+v", prs)
	}
}

func TestCreateRepoNoToken(t *testing.T) {
	gh := &GitHub{GenericGit: GenericGit{Remote: "https://github.com/org/repo"}}
	if _, err := gh.CreateRepo("newrepo", false); err == nil {
		t.Fatal("expected error without token")
	}
	if _, err := gh.OpenPR("aiw/x", "title", nil); err == nil {
		t.Fatal("expected error without token")
	}
}

func TestGenericGitOpenPRError(t *testing.T) {
	g := GenericGit{Remote: "https://gitlab.com/x/y"}
	_, err := g.OpenPR("b", "t", nil)
	if !errors.Is(err, ErrNotIntegrated) {
		t.Fatalf("want ErrNotIntegrated, got %v", err)
	}
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
