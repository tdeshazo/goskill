package remoteskill

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchAuthorizedRejectsCrossOriginRedirectWithoutContactingTarget(t *testing.T) {
	const token = "redirect-test-token"
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("authorization leaked to redirect target: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer target.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		http.Redirect(w, r, target.URL+"/SKILL.md", http.StatusFound)
	}))
	defer allowed.Close()

	_, err := FetchAuthorized(allowed.URL+"/SKILL.md", token, allowed.URL)
	if err == nil {
		t.Fatal("cross-origin redirect succeeded")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target calls = %d", targetCalls.Load())
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("credential leaked in error: %v", err)
	}
}
