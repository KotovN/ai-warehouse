package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/google/go-github/v69/github"
	"golang.org/x/oauth2"
)

// GitHub manages github.com remotes: git transport plus repo creation and PRs.
type GitHub struct {
	GenericGit
	token string
	owner string
	name  string
	api   *github.Client
}

// newGithubClient builds the API client; apiBase overrides the default for tests.
func newGithubClient(token, apiBase string) *github.Client {
	client := http2Client(token)
	api := github.NewClient(client)
	if apiBase != "" {
		u, err := url.Parse(apiBase)
		if err != nil {
			return nil
		}
		api.BaseURL = u
	}
	return api
}

func http2Client(token string) *http.Client {
	if token == "" {
		return http.DefaultClient
	}
	return oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
}

func (g *GitHub) CreateRepo(name string, private bool) (string, error) {
	if g.token == "" {
		return "", errors.New("GitHub token required: set GITHUB_TOKEN or GH_TOKEN")
	}
	repo, _, err := g.api.Repositories.Create(context.Background(), "", &github.Repository{
		Name:    &name,
		Private: &private,
	})
	if err != nil {
		return "", err
	}
	return repo.GetHTMLURL(), nil
}

func (g *GitHub) OpenPR(branch, title string, reviewers []string) (string, error) {
	ctx := context.Background()
	base, err := g.defaultBranch(ctx)
	if err != nil {
		return "", err
	}
	pr, _, err := g.api.PullRequests.Create(ctx, g.owner, g.name, &github.NewPullRequest{
		Title: &title,
		Head:  &branch,
		Base:  &base,
	})
	if err != nil {
		return "", err
	}
	if len(reviewers) > 0 {
		_, _, err := g.api.PullRequests.RequestReviewers(ctx, g.owner, g.name, pr.GetNumber(), github.ReviewersRequest{
			Reviewers: reviewers,
		})
		if err != nil {
			return pr.GetHTMLURL(), err
		}
	}
	return pr.GetHTMLURL(), nil
}

func (g *GitHub) ListOpenPRs() ([]PR, error) {
	opts := &github.PullRequestListOptions{State: "open", ListOptions: github.ListOptions{PerPage: 50}}
	ghprs, _, err := g.api.PullRequests.List(context.Background(), g.owner, g.name, opts)
	if err != nil {
		return nil, err
	}
	var prs []PR
	for _, p := range ghprs {
		prs = append(prs, PR{
			Number: p.GetNumber(),
			Author: p.GetUser().GetLogin(),
			Head:   p.GetHead().GetRef(),
			Title:  p.GetTitle(),
			URL:    p.GetHTMLURL(),
		})
	}
	return prs, nil
}

func (g *GitHub) defaultBranch(ctx context.Context) (string, error) {
	repo, _, err := g.api.Repositories.Get(ctx, g.owner, g.name)
	if err != nil {
		return "", err
	}
	if b := repo.GetDefaultBranch(); b != "" {
		return b, nil
	}
	return "main", nil
}
