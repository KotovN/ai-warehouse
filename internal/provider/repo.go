// Package provider gives access to the hub repo: git transport plus, for
// supported hosts, repo creation and pull requests.
package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// ErrNotIntegrated is returned by non-API hosts (e.g. GenericGit) when PR or
// repo API operations are requested after a git push has already succeeded.
var ErrNotIntegrated = errors.New("this git host is not API-integrated: open the pull request by hand")

// Repo syncs between a hub remote and its local clone.
type Repo interface {
	Clone(dest string) error
	CloneFull(dest string) error
	CreateRepo(name string, private bool) (string, error)
	PushBranch(clonePath, branch string) error
	OpenPR(branch, title string, reviewers []string) (string, error)
	ListOpenPRs() ([]PR, error)
}

// PR is an open pull request on the hub repo.
type PR struct {
	Number int
	Author string
	Head   string
	Title  string
	URL    string
}

// GenericGit covers any git remote via transport only. It cannot create
// repos or PRs; callers must tell the user to do that on the host.
type GenericGit struct{ Remote string }

func (g GenericGit) Clone(dest string) error {
	return g.clone(dest, 1)
}

// CloneFull fetches the whole history, needed before pushing back.
func (g GenericGit) CloneFull(dest string) error {
	return g.clone(dest, 0)
}

func (g GenericGit) clone(dest string, depth int) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("clone destination %s already exists", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	opts := &git.CloneOptions{URL: g.Remote, Auth: authForRemote(g.Remote)}
	if depth > 0 {
		opts.Depth = depth
	}
	_, err := git.PlainCloneContext(context.Background(), dest, false, opts)
	return err
}

func (g GenericGit) CreateRepo(name string, private bool) (string, error) {
	return "", fmt.Errorf("cannot create repos for %s: create it on the host, then attach it", g.Remote)
}

// PushBranch pushes a local branch to origin. Force is on because aiw/*
// branches are rebuilt from the latest default on every push.
func (g GenericGit) PushBranch(clonePath, branch string) error {
	r, err := git.PlainOpen(clonePath)
	if err != nil {
		return err
	}
	refspec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	return r.PushContext(context.Background(), &git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refspec},
		Auth:       authForRemote(g.Remote),
		Force:      true,
	})
}

// UpdateClone fetches origin and hard-resets the clone to origin's default
// branch. The clone is machine-owned, so local changes are discarded.
func UpdateClone(clonePath string) error {
	r, err := git.PlainOpen(clonePath)
	if err != nil {
		return err
	}
	if err := r.FetchContext(context.Background(), &git.FetchOptions{
		RemoteName: "origin",
		Auth:       authForRemote(remoteOf(r)),
		Force:      true,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	head, err := r.Head()
	if err != nil {
		return err
	}
	ref, err := r.Reference(plumbing.NewRemoteReferenceName("origin", head.Name().Short()), true)
	if err != nil {
		return err
	}
	w, err := r.Worktree()
	if err != nil {
		return err
	}
	return w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: ref.Hash()})
}

// RemoteURL returns the configured origin URL of a clone, or "".
func RemoteURL(clonePath string) string {
	r, err := git.PlainOpen(clonePath)
	if err != nil {
		return ""
	}
	return remoteOf(r)
}

// remoteOf returns the origin URL of a repo.
func remoteOf(r *git.Repository) string {
	rm, err := r.Remote("origin")
	if err != nil {
		return ""
	}
	cfg := rm.Config()
	if len(cfg.URLs) == 0 {
		return ""
	}
	return cfg.URLs[0]
}

func (g GenericGit) OpenPR(branch, title string, reviewers []string) (string, error) {
	return "", ErrNotIntegrated
}

func (g GenericGit) ListOpenPRs() ([]PR, error) {
	return nil, ErrNotIntegrated
}

// New returns the provider matching a remote URL; unknown hosts degrade to GenericGit.
func New(remote string) (Repo, error) {
	if owner, name, ok := parseGitHubRemote(remote); ok {
		tok := githubToken()
		return &GitHub{
			GenericGit: GenericGit{Remote: remote},
			token:      tok,
			owner:      owner,
			name:       name,
			api:        newGithubClient(tok, ""),
		}, nil
	}
	return GenericGit{Remote: remote}, nil
}

// parseGitHubRemote recognizes HTTPS, SSH URL, and SCP-style GitHub remotes.
func parseGitHubRemote(remote string) (owner, name string, ok bool) {
	trimmed := strings.TrimSpace(remote)
	// SCP-style: git@github.com:owner/repo(.git)
	if strings.HasPrefix(strings.ToLower(trimmed), "git@github.com:") {
		path := trimmed[len("git@github.com:"):]
		owner, name = splitOwnerName(path)
		return owner, name, owner != "" && name != ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return "", "", false
	}
	owner, name = splitOwnerName(u.Path)
	return owner, name, owner != "" && name != ""
}

// CommitAll stages every change in the clone and commits it.
func CommitAll(clonePath, msg string) (plumbing.Hash, error) {
	r, err := git.PlainOpen(clonePath)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	w, err := r.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	sig := signature(r)
	if err := w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return plumbing.ZeroHash, err
	}
	return w.Commit(msg, &git.CommitOptions{Author: &sig})
}

// signature uses git config identity (local + global) when present, else a neutral fallback.
func signature(r *git.Repository) object.Signature {
	sig := object.Signature{When: time.Now()}
	if cfg, err := r.ConfigScoped(config.GlobalScope); err == nil && cfg.User.Name != "" && cfg.User.Email != "" {
		sig.Name, sig.Email = cfg.User.Name, cfg.User.Email
		return sig
	}
	if cfg, err := r.Config(); err == nil && cfg.User.Name != "" && cfg.User.Email != "" {
		sig.Name, sig.Email = cfg.User.Name, cfg.User.Email
		return sig
	}
	sig.Name, sig.Email = "aiwarehouse", "aiwarehouse@localhost"
	return sig
}

// CreateBranch creates a local branch at the current HEAD and checks it out.
func CreateBranch(clonePath, branch string) error {
	r, err := git.PlainOpen(clonePath)
	if err != nil {
		return err
	}
	w, err := r.Worktree()
	if err != nil {
		return err
	}
	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
	})
}

// authForRemote returns basic auth from GITHUB_TOKEN/GH_TOKEN strictly for
// HTTPS github.com remotes so credentials are never sent to other hosts or over HTTP.
func authForRemote(remote string) *http.BasicAuth {
	u, err := url.Parse(strings.TrimSpace(remote))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "github.com") {
		return nil
	}
	if tok := githubToken(); tok != "" {
		return &http.BasicAuth{Username: "aiwarehouse", Password: tok}
	}
	return nil
}

func githubToken() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	return os.Getenv("GH_TOKEN")
}

func splitOwnerName(path string) (string, string) {
	p := strings.Trim(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return "", ""
	}
	owner := parts[0]
	name := strings.TrimSuffix(parts[1], ".git")
	return owner, name
}
