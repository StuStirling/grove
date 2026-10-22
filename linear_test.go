package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// linearServer stands in for Linear's API: it checks the key and answers each
// request with handle's status and body.
func linearServer(t *testing.T, handle func(query string, vars map[string]any) (int, string)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "lin_test" {
			t.Errorf("Authorization = %q", got)
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("request body: %v", err)
		}
		code, body := handle(req.Query, req.Variables)
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := linearEndpoint
	linearEndpoint = srv.URL
	t.Cleanup(func() { linearEndpoint = old })
}

const issueJSON = `{"identifier":"ENG-412","title":"Let people snooze","description":"Pause pushes","url":"https://linear.app/acme/issue/ENG-412","branchName":"stu/eng-412-let-people-snooze","state":{"name":"In Progress","type":"started"},"team":{"key":"ENG"},"project":{"name":"Notification controls"}}`

var issue412 = LinearIssue{ID: "ENG-412", Title: "Let people snooze", Description: "Pause pushes", URL: "https://linear.app/acme/issue/ENG-412", BranchName: "stu/eng-412-let-people-snooze", State: "In Progress", StateType: "started", Team: "ENG", Project: "Notification controls"}

const projectJSON = `{"id":"p1","name":"Notification controls","description":"Finer control","url":"https://linear.app/acme/project/nc","progress":0.43,"targetDate":"2026-11-14","status":{"name":"In Progress","type":"started"}}`

var projectNC = LinearProject{ID: "p1", Name: "Notification controls", Description: "Finer control", URL: "https://linear.app/acme/project/nc", Status: "In Progress", StatusType: "started", TargetDate: "2026-11-14", Progress: 0.43}

func TestLinearSearch(t *testing.T) {
	linearServer(t, func(q string, vars map[string]any) (int, string) {
		if !strings.Contains(q, "searchIssues") || vars["term"] != "snooze" {
			t.Errorf("query %q, vars %v", q, vars)
		}
		return 200, `{"data":{"searchIssues":{"nodes":[` + issueJSON + `]},"searchProjects":{"nodes":[` + projectJSON + `]}}}`
	})
	res, err := linearSearch(context.Background(), "lin_test", " snooze ")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Issues, []LinearIssue{issue412}) {
		t.Errorf("issues = %+v", res.Issues)
	}
	if !reflect.DeepEqual(res.Projects, []LinearProject{projectNC}) {
		t.Errorf("projects = %+v", res.Projects)
	}
}

func TestLinearSearchIdentifier(t *testing.T) {
	var queries []string
	linearServer(t, func(q string, vars map[string]any) (int, string) {
		queries = append(queries, q)
		if vars["id"] == "ENG-412" {
			return 200, `{"data":{"issue":` + issueJSON + `}}`
		}
		return 200, `{"data":null,"errors":[{"message":"Entity not found: Issue","extensions":{"code":"INVALID_INPUT"}}]}`
	})
	res, err := linearSearch(context.Background(), "lin_test", "eng-412")
	if err != nil || !reflect.DeepEqual(res.Issues, []LinearIssue{issue412}) || len(res.Projects) != 0 {
		t.Errorf("eng-412: %+v, %v", res, err)
	}
	res, err = linearSearch(context.Background(), "lin_test", "ENG-999")
	if err != nil || len(res.Issues) != 0 || len(res.Projects) != 0 {
		t.Errorf("ENG-999: %+v, %v", res, err)
	}
	for _, q := range queries {
		if strings.Contains(q, "searchIssues") {
			t.Errorf("an identifier used search: %q", q)
		}
	}
}

func TestLinearMyWork(t *testing.T) {
	noProject := strings.Replace(issueJSON, `{"name":"Notification controls"}`, `null`, 1)
	linearServer(t, func(q string, vars map[string]any) (int, string) {
		if !strings.Contains(q, "assignedIssues") {
			t.Errorf("query %q", q)
		}
		return 200, `{"data":{"viewer":{"assignedIssues":{"nodes":[` + noProject + `]}},"projects":{"nodes":[` + projectJSON + `]}}}`
	})
	res, err := linearSearch(context.Background(), "lin_test", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 1 || res.Issues[0].ID != "ENG-412" || res.Issues[0].Project != "" {
		t.Errorf("issues = %+v", res.Issues)
	}
	if !reflect.DeepEqual(res.Projects, []LinearProject{projectNC}) {
		t.Errorf("projects = %+v", res.Projects)
	}
}

func TestLinearProject(t *testing.T) {
	linearServer(t, func(q string, vars map[string]any) (int, string) {
		if vars["id"] != "p1" {
			t.Errorf("vars %v", vars)
		}
		full := strings.TrimSuffix(projectJSON, "}") + `,"content":"## Plan","issues":{"nodes":[` + issueJSON + `]}}`
		return 200, `{"data":{"project":` + full + `}}`
	})
	p, err := linearProject(context.Background(), "lin_test", "p1")
	if err != nil {
		t.Fatal(err)
	}
	want := projectNC
	want.Content, want.Issues = "## Plan", []LinearIssue{issue412}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("project = %+v", p)
	}
}

func TestLinearErrors(t *testing.T) {
	for _, c := range []struct {
		code       int
		body       string
		want       error
		wantInText string
	}{
		{401, `{"errors":[{"message":"Authentication required"}]}`, errLinearAuth, ""},
		{400, `{"errors":[{"message":"bad key","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`, errLinearAuth, ""},
		{200, `{"errors":[{"message":"slow down","extensions":{"code":"RATELIMITED"}}]}`, errLinearRateLimited, ""},
		{429, ``, errLinearRateLimited, ""},
		{200, `{"errors":[{"message":"boom"}]}`, nil, "boom"},
		{502, `bad gateway`, nil, "HTTP 502"},
	} {
		linearServer(t, func(string, map[string]any) (int, string) { return c.code, c.body })
		_, err := linearViewer(context.Background(), "lin_test")
		switch {
		case err == nil:
			t.Errorf("%d %s: no error", c.code, c.body)
		case c.want != nil && !errors.Is(err, c.want):
			t.Errorf("%d %s: %v, want %v", c.code, c.body, err, c.want)
		case c.wantInText != "" && !strings.Contains(err.Error(), c.wantInText):
			t.Errorf("%d %s: %v, want it to mention %q", c.code, c.body, err, c.wantInText)
		}
	}
}

func TestLinearAccount(t *testing.T) {
	// A cached key skips the keychain; the viewer call decides the rest.
	for _, c := range []struct {
		code      int
		body      string
		connected bool
		name      string
		err       error
	}{
		{200, `{"data":{"viewer":{"name":"Stu"}}}`, true, "Stu", nil},
		{401, `{}`, false, "", errLinearAuth},
		{502, `bad gateway`, true, "", nil}, // offline or Linear down: still connected
	} {
		linearServer(t, func(string, map[string]any) (int, string) { return c.code, c.body })
		a := &App{linKey: "lin_test"}
		acc, err := a.LinearAccount()
		if acc.Connected != c.connected || acc.Name != c.name || !errors.Is(err, c.err) {
			t.Errorf("%d: %+v, %v", c.code, acc, err)
		}
	}
}
