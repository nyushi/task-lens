package main

import (
	"net/http/httptest"
	"testing"
)

func TestListenOptIn(t *testing.T) {
	for _, tc := range []struct {
		o     HTTPOptions
		valid bool
	}{
		{HTTPOptions{}, true}, {HTTPOptions{Listen: "[::1]:8765"}, true},
		{HTTPOptions{Listen: "0.0.0.0:8765"}, false},
		{HTTPOptions{Listen: "0.0.0.0:8765", AllowRemote: true}, false},
		{HTTPOptions{Listen: "0.0.0.0:8765", AllowRemote: true, AllowedHosts: []string{"192.0.2.10:8765"}}, true},
		{HTTPOptions{Listen: "0.0.0.0:8765", AllowRemote: true, AllowedHosts: []string{"*:8765"}}, false},
		{HTTPOptions{Listen: "0.0.0.0:8765", AllowRemote: true, AllowedHosts: []string{"example.test:9000"}}, false},
		{HTTPOptions{Listen: "example.test:8765"}, false},
	} {
		_, e := tc.o.validate()
		if (e == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc.o, e)
		}
	}
}
func TestRemoteHostsAreNotAuthentication(t *testing.T) {
	o := HTTPOptions{AllowedHosts: []string{"192.0.2.10:8765"}}
	h := webHandlerHosts(NewStore(), o.hosts(8765), "demo")
	for _, tc := range []struct {
		host, origin string
		status       int
	}{{"192.0.2.10:8765", "", 200}, {"192.0.2.10:8765", "http://192.0.2.10:8765", 200}, {"192.0.2.10:8765", "https://evil.test", 403}, {"evil.test:8765", "", 403}} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/state", nil)
		r.Header.Set("X-TaskLens-Request", "1")
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}
