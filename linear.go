package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// LinearIssue is an issue as grove shows it: Linear's nested fields flattened.
type LinearIssue struct {
	ID          string `json:"id"` // identifier, e.g. "ENG-412"
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	BranchName  string `json:"branchName"`
	State       string `json:"state"`     // workflow state name, e.g. "In Progress"
	StateType   string `json:"stateType"` // triage|backlog|unstarted|started|completed|canceled
	Team        string `json:"team"`      // team key, e.g. "ENG"
	Project     string `json:"project"`   // project name, "" for none
}

// LinearProject is a project as grove shows it.
type LinearProject struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Content     string        `json:"content"` // filled by linearProject only
	URL         string        `json:"url"`
	Status      string        `json:"status"`     // e.g. "In Progress"
	StatusType  string        `json:"statusType"` // backlog|planned|started|paused|completed|canceled
	TargetDate  string        `json:"targetDate"` // "YYYY-MM-DD" or ""
	Progress    float64       `json:"progress"`   // 0 to 1
	Issues      []LinearIssue `json:"issues"`     // filled by linearProject only
}

// LinearResults is what the search palette lists.
type LinearResults struct {
	Issues   []LinearIssue   `json:"issues"`
	Projects []LinearProject `json:"projects"`
}

// The frontend matches these texts, so keep them in step with start.ts.
var (
	errLinearNoKey       = errors.New("Linear isn't connected")
	errLinearAuth        = errors.New("Linear rejected the saved key")
	errLinearRateLimited = errors.New("Linear is limiting searches, try again in a moment")
)

var linearEndpoint = "https://api.linear.app/graphql" // a var so tests can point it at httptest

var linearHTTP = &http.Client{Timeout: 15 * time.Second}

// Search results are IssueSearchResult / ProjectSearchResult, not Issue /
// Project, so the selections are shared as text rather than as fragments.
const (
	issueFields   = `identifier title description url branchName state { name type } team { key } project { name }`
	projectFields = `id name description url progress targetDate status { name type }`

	qViewer = `query { viewer { name } }`
	qMyWork = `query {
  viewer { assignedIssues(first: 50, orderBy: updatedAt, filter: { state: { type: { nin: ["completed", "canceled"] } } }) { nodes { ` + issueFields + ` } } }
  projects(first: 20, orderBy: updatedAt, filter: { status: { type: { nin: ["completed", "canceled"] } }, or: [{ lead: { isMe: { eq: true } } }, { members: { some: { isMe: { eq: true } } } }] }) { nodes { ` + projectFields + ` } }
}`
	qSearch = `query($term: String!) {
  searchIssues(term: $term, first: 20) { nodes { ` + issueFields + ` } }
  searchProjects(term: $term, first: 10) { nodes { ` + projectFields + ` } }
}`
	qIssue = `query($id: String!) { issue(id: $id) { ` + issueFields + ` } }`
	// ponytail: first 100 issues; paginate if a project ever has more.
	qProject = `query($id: String!) { project(id: $id) { ` + projectFields + ` content issues(first: 100) { nodes { ` + issueFields + ` } } } }`
)

// identifierRe is an issue identifier such as ENG-412, fetched directly: search
// is rate-limited to 30 requests a minute, a lookup isn't.
var identifierRe = regexp.MustCompile(`^[A-Za-z]+-\d+$`)

// linearQuery runs one GraphQL request with a personal API key.
func linearQuery(ctx context.Context, key, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, linearEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", key)
	resp, err := linearHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Linear: %w", err)
	}
	defer resp.Body.Close()
	// Linear sends GraphQL errors with 400 too, so read them whatever the status.
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	code := ""
	if len(res.Errors) > 0 {
		code = res.Errors[0].Extensions.Code
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || code == "AUTHENTICATION_ERROR":
		return errLinearAuth
	case resp.StatusCode == http.StatusTooManyRequests || code == "RATELIMITED":
		return errLinearRateLimited
	case len(res.Errors) > 0:
		return fmt.Errorf("Linear: %s", res.Errors[0].Message)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("Linear: HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(res.Data, out)
}

// rawIssue and rawProject mirror Linear's nested JSON.
type rawIssue struct {
	Identifier, Title, Description, URL, BranchName string
	State                                           struct{ Name, Type string }
	Team                                            struct{ Key string }
	Project                                         *struct{ Name string }
}

type rawProject struct {
	ID, Name, Description, Content, URL, TargetDate string
	Progress                                        float64
	Status                                          struct{ Name, Type string }
	Issues                                          struct{ Nodes []rawIssue }
}

func toIssue(r rawIssue) LinearIssue {
	i := LinearIssue{ID: r.Identifier, Title: r.Title, Description: r.Description, URL: r.URL, BranchName: r.BranchName,
		State: r.State.Name, StateType: r.State.Type, Team: r.Team.Key}
	if r.Project != nil {
		i.Project = r.Project.Name
	}
	return i
}

func toIssues(rs []rawIssue) []LinearIssue {
	out := make([]LinearIssue, len(rs))
	for i, r := range rs {
		out[i] = toIssue(r)
	}
	return out
}

func toProject(r rawProject) LinearProject {
	p := LinearProject{ID: r.ID, Name: r.Name, Description: r.Description, Content: r.Content, URL: r.URL,
		Status: r.Status.Name, StatusType: r.Status.Type, TargetDate: r.TargetDate, Progress: r.Progress}
	if len(r.Issues.Nodes) > 0 {
		p.Issues = toIssues(r.Issues.Nodes)
	}
	return p
}

func toProjects(rs []rawProject) []LinearProject {
	out := make([]LinearProject, len(rs))
	for i, r := range rs {
		out[i] = toProject(r)
	}
	return out
}

// linearViewer checks a key, returning its account's name.
func linearViewer(ctx context.Context, key string) (string, error) {
	var data struct{ Viewer struct{ Name string } }
	if err := linearQuery(ctx, key, qViewer, nil, &data); err != nil {
		return "", err
	}
	return data.Viewer.Name, nil
}

// linearMyWork is what the palette shows before anything is typed: open issues
// assigned to the viewer, and open projects they lead or are a member of.
func linearMyWork(ctx context.Context, key string) (LinearResults, error) {
	var data struct {
		Viewer struct {
			AssignedIssues struct{ Nodes []rawIssue }
		}
		Projects struct{ Nodes []rawProject }
	}
	if err := linearQuery(ctx, key, qMyWork, nil, &data); err != nil {
		return LinearResults{}, err
	}
	return LinearResults{Issues: toIssues(data.Viewer.AssignedIssues.Nodes), Projects: toProjects(data.Projects.Nodes)}, nil
}

// linearSearch finds issues and projects matching term; "" is the viewer's work.
func linearSearch(ctx context.Context, key, term string) (LinearResults, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return linearMyWork(ctx, key)
	}
	if identifierRe.MatchString(term) {
		i, err := linearIssue(ctx, key, strings.ToUpper(term))
		switch {
		case errors.Is(err, errLinearAuth), errors.Is(err, errLinearRateLimited):
			return LinearResults{}, err
		case err != nil:
			return LinearResults{}, nil // no such issue
		}
		return LinearResults{Issues: []LinearIssue{i}}, nil
	}
	var data struct {
		SearchIssues   struct{ Nodes []rawIssue }
		SearchProjects struct{ Nodes []rawProject }
	}
	if err := linearQuery(ctx, key, qSearch, map[string]any{"term": term}, &data); err != nil {
		return LinearResults{}, err
	}
	return LinearResults{Issues: toIssues(data.SearchIssues.Nodes), Projects: toProjects(data.SearchProjects.Nodes)}, nil
}

// linearIssue fetches one issue by identifier.
func linearIssue(ctx context.Context, key, id string) (LinearIssue, error) {
	var data struct{ Issue rawIssue }
	if err := linearQuery(ctx, key, qIssue, map[string]any{"id": id}, &data); err != nil {
		return LinearIssue{}, err
	}
	return toIssue(data.Issue), nil
}

// linearProject fetches one project with its content and issues.
func linearProject(ctx context.Context, key, id string) (LinearProject, error) {
	var data struct{ Project rawProject }
	if err := linearQuery(ctx, key, qProject, map[string]any{"id": id}, &data); err != nil {
		return LinearProject{}, err
	}
	return toProject(data.Project), nil
}

// ---- bound to the frontend ----

// LinearAccount is whether a Linear key is saved, and whose it is.
type LinearAccount struct {
	Connected bool   `json:"connected"`
	Name      string `json:"name"`
}

// linearKey is the saved key, read from the keychain on first use. The key
// stays in Go: the frontend only ever learns the account's name.
func (a *App) linearKey() (string, error) {
	a.mu.Lock()
	key := a.linKey
	a.mu.Unlock()
	if key != "" {
		return key, nil
	}
	key, err := secretGet("linear")
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", errLinearNoKey
	}
	a.mu.Lock()
	a.linKey = key
	a.mu.Unlock()
	return key, nil
}

// LinearAccount reports the saved key's account. Only a rejected key counts as
// disconnected: offline, ⌘L still opens the palette, which shows the error.
func (a *App) LinearAccount() (LinearAccount, error) {
	key, err := a.linearKey()
	if errors.Is(err, errLinearNoKey) {
		return LinearAccount{}, nil
	}
	if err != nil {
		return LinearAccount{}, err
	}
	a.mu.Lock()
	name := a.linName
	a.mu.Unlock()
	if name != "" {
		return LinearAccount{Connected: true, Name: name}, nil
	}
	name, err = linearViewer(context.Background(), key)
	if errors.Is(err, errLinearAuth) {
		return LinearAccount{}, err
	}
	if err != nil {
		return LinearAccount{Connected: true}, nil
	}
	a.mu.Lock()
	a.linName = name
	a.mu.Unlock()
	return LinearAccount{Connected: true, Name: name}, nil
}

// LinearConnect checks a personal API key with Linear, then saves it.
func (a *App) LinearConnect(key string) (LinearAccount, error) {
	key = strings.TrimSpace(key)
	name, err := linearViewer(context.Background(), key)
	if err != nil {
		return LinearAccount{}, err
	}
	if err := secretSet("linear", key); err != nil {
		return LinearAccount{}, err
	}
	a.mu.Lock()
	a.linKey, a.linName = key, name
	a.mu.Unlock()
	return LinearAccount{Connected: true, Name: name}, nil
}

// LinearDisconnect forgets the saved key.
func (a *App) LinearDisconnect() error {
	if err := secretDelete("linear"); err != nil {
		return err
	}
	a.mu.Lock()
	a.linKey, a.linName = "", ""
	a.mu.Unlock()
	return nil
}

// LinearSearch finds issues and projects; "" lists the viewer's own work.
func (a *App) LinearSearch(term string) (LinearResults, error) {
	key, err := a.linearKey()
	if err != nil {
		return LinearResults{}, err
	}
	return linearSearch(context.Background(), key, term)
}

// GetLinearIssue fetches one issue by identifier.
func (a *App) GetLinearIssue(id string) (LinearIssue, error) {
	key, err := a.linearKey()
	if err != nil {
		return LinearIssue{}, err
	}
	return linearIssue(context.Background(), key, id)
}

// GetLinearProject fetches one project with its content and issues.
func (a *App) GetLinearProject(id string) (LinearProject, error) {
	key, err := a.linearKey()
	if err != nil {
		return LinearProject{}, err
	}
	return linearProject(context.Background(), key, id)
}
