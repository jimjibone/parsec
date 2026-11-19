package version

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShort(t *testing.T) {
	cases := []struct {
		b    Build
		want string
	}{
		{Build{}, "dev"},
		{Build{Commit: "0123456789abcdef"}, "0123456"},
		{Build{Commit: "0123456789abcdef", Modified: true}, "0123456-dirty"},
	}
	for _, c := range cases {
		if got := c.b.Short(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.b, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	ahead := 3
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmt.Fprintf(w, `{"status":"ahead","ahead_by":%d,"html_url":"https://example.com/compare"}`, ahead)
	}))
	defer srv.Close()

	c := NewChecker(Build{Commit: "abc123"})
	c.base = srv.URL
	if err := c.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := "/repos/" + Repo + "/compare/abc123..." + Branch; path != want {
		t.Errorf("path %q, want %q", path, want)
	}
	u := c.Update()
	if u == nil || u.Behind != 3 || u.URL != "https://example.com/compare" {
		t.Fatalf("update %+v", u)
	}

	ahead = 0
	if err := c.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u := c.Update(); u != nil {
		t.Fatalf("up to date, got %+v", u)
	}
}

func TestCheckNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := NewChecker(Build{Commit: "abc123"})
	c.base = srv.URL
	if err := c.check(context.Background()); err == nil {
		t.Fatal("want an error for an unknown commit")
	}
	if c.Update() != nil {
		t.Fatal("no update expected")
	}
}
