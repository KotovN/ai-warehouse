package provider

import (
	"encoding/json"
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

func initHub(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
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
	dest := filepath.Join(t.TempDir(), "team-repo")
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
	clone := filepath.Join(t.TempDir(), "clone")
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
	clone := filepath.Join(t.TempDir(), "clone")
	if err := g.Clone(clone); err != nil {
		t.Fatal(err)
	}
	// New commit on origin after the clone was made.
	os.WriteFile(filepath.Join(src, "skills", "fresh.md"), []byte("fresh\n"), 0o644)
	os.MkdirAll(filepath.Join(src, "skills"), 0o755)
	os.WriteFile(filepath.Join(src, "skills", "fresh.md"), []byte("fresh\n"), 0o644)
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
	r, err := New("https://github.com/org/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.(*GitHub); !ok {
		t.Fatalf("github URL should give GitHub provider, got %T", r)
	}
	r, err = New("https://gitlab.com/org/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.(GenericGit); !ok {
		t.Fatalf("unknown host should give GenericGit, got %T", r)
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
	server := httptest.NewServer(mux)
	defer server.Close()

	gh := &GitHub{
		GenericGit: GenericGit{Remote: "https://github.com/org/repo"},
		token:      "test-token",
		owner:      "org",
		name:       "repo",
		api:        newGithubClient("test-token", server.URL+"/api-v3/"),
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
		w.Write([]byte(`[
			{"number":1,"title":"Add skill","head":{"ref":"aiw/alice-1"},"user":{"login":"alice"},"html_url":"https://github.com/org/repo/pull/1"},
			{"number":2,"title":"Fix rule","head":{"ref":"aiw/bob-2"},"user":{"login":"bob"},"html_url":"https://github.com/org/repo/pull/2"}
		]`))
	})
	mux := http.NewServeMux()
	mux.Handle("/api-v3/", http.StripPrefix("/api-v3", api))
	server := httptest.NewServer(mux)
	defer server.Close()

	gh := &GitHub{
		GenericGit: GenericGit{Remote: "https://github.com/org/repo"},
		owner:      "org",
		name:       "repo",
		api:        newGithubClient("test-token", server.URL+"/api-v3/"),
	}
	prs, err := gh.ListOpenPRs()
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 || prs[0].Author != "alice" || prs[0].Head != "aiw/alice-1" {
		t.Fatalf("got %+v", prs)
	}
}

func TestCreateRepoNoToken(t *testing.T) {
	gh := &GitHub{GenericGit: GenericGit{Remote: "https://github.com/org/repo"}}
	if _, err := gh.CreateRepo("newrepo", false); err == nil {
		t.Fatal("expected error without token")
	}
}

func TestGenericGitOpenPRError(t *testing.T) {
	g := GenericGit{Remote: "https://gitlab.com/x/y"}
	if _, err := g.OpenPR("b", "t", nil); err == nil {
		t.Fatal("generic git cannot open PRs")
	}
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
