package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
)

func TestNormalizeNode(t *testing.T) {
	tests := []struct {
		in, url, host string
		ok            bool
	}{
		{"10.0.0.1", "https://10.0.0.1", "10.0.0.1", true},
		{"https://10.0.0.1", "https://10.0.0.1", "10.0.0.1", true},
		{"https://10.0.0.1:443/", "https://10.0.0.1", "10.0.0.1", true},
		{"http://leaf101", "http://leaf101", "leaf101", true},
		{"https://[fd00::1]", "https://[fd00::1]", "fd00::1", true},
		{"https://10.0.0.1:8443", "", "", false},
		{"https://user@10.0.0.1", "", "", false},
		{"https://10.0.0.1/api", "", "", false},
		{"https://10.0.0.1?x=1", "", "", false},
		{"ftp://10.0.0.1", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		u, h, err := normalizeNode(tt.in)
		if (err == nil) != tt.ok || u != tt.url || h != tt.host {
			t.Errorf("normalizeNode(%q) = %q, %q, %v", tt.in, u, h, err)
		}
	}
}

func fakeTopSystemAPIC(t *testing.T) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/aaaLogin.json":
			fmt.Fprint(w, `{"imdata":[{"aaaLogin":{"attributes":{"token":"tok","refreshTimeoutSeconds":"600","maximumLifetimeSeconds":"86400"}}}]}`)
		case "/api/class/topSystem.json":
			fmt.Fprint(w, `{"totalCount":"2","imdata":[`+
				`{"topSystem":{"attributes":{"name":"leaf101","oobMgmtAddr":"10.0.0.101","inbMgmtAddr":"0.0.0.0"}}},`+
				`{"topSystem":{"attributes":{"name":"spine201","oobMgmtAddr":"10.0.0.201","inbMgmtAddr6":"FD00::201"}}}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIsKnownNode(t *testing.T) {
	viper.Set("httpclient.insecureHTTPS", true)
	apic := fakeTopSystemAPIC(t)
	fabric := &Fabric{FabricName: "allowlist-test", Apic: []string{apic.URL},
		DiscoveryConfig: DiscoveryConfiguration{TargetFields: []string{"aci_exporter_fabric", "oobMgmtAddr"}}}

	for host, want := range map[string]bool{"10.0.0.101": true, "10.0.0.201": true, "fd00::201": true,
		"0.0.0.0": false, "10.9.9.9": false, "leaf101": false} {
		got, err := isKnownNode(context.Background(), fabric, host)
		if err != nil || got != want {
			t.Errorf("isKnownNode(%q) = %v, %v; want %v", host, got, err, want)
		}
	}
}

func TestProbeRejectsUnknownNode(t *testing.T) {
	viper.Set("httpclient.insecureHTTPS", true)
	apic := fakeTopSystemAPIC(t)
	var attackerLogins atomic.Int64
	attacker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerLogins.Add(1)
	}))
	t.Cleanup(attacker.Close)

	queries := AllQueries{ClassQueries: ClassQueries{"q": &ClassQuery{ClassName: "faultInst"}}}
	createQueryNameSet(queries)
	h := HandlerInit{queries, map[string]*Fabric{"probe-test": {FabricName: "probe-test", Username: "u", Password: "p", Apic: []string{apic.URL}}}}

	for _, node := range []string{attacker.URL, "10.9.9.9", "https://u@10.0.0.101"} {
		rec := httptest.NewRecorder()
		h.getMonitorMetrics(rec, httptest.NewRequest("GET", "/probe?target=probe-test&queries=q&node="+node, nil))
		if rec.Code != 403 {
			t.Errorf("node %q: status %d, want 403", node, rec.Code)
		}
	}
	if attackerLogins.Load() != 0 {
		t.Errorf("credentials were sent to the attacker host")
	}
}
