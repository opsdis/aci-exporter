package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// The node query parameter is only accepted for addresses of nodes that the fabric's
// topSystem class returns, so fabric credentials are never sent to other hosts.

const nodeAllowlistTTL = 5 * time.Minute

// topSystem attributes that are always accepted as node address, in addition to the
// fabric's service discovery target fields
var nodeAddressFields = []string{"oobMgmtAddr", "inbMgmtAddr", "oobMgmtAddr6", "inbMgmtAddr6"}

type nodeAllowlistEntry struct {
	addresses map[string]struct{}
	expires   time.Time
}

var nodeAllowlist = struct {
	sync.Mutex
	fabrics map[string]*nodeAllowlistEntry
}{fabrics: make(map[string]*nodeAllowlistEntry)}

// normalizeNode accepts host or http(s)://host with an optional default port and returns the
// canonical node url and its host
func normalizeNode(node string) (string, string, error) {
	raw := node
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("node must be a host, not a url with path or credentials")
	}
	host := u.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("empty host")
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return "", "", fmt.Errorf("non default port %s not allowed", port)
	}
	hostPart := host
	if strings.Contains(host, ":") {
		hostPart = "[" + host + "]"
	}
	return u.Scheme + "://" + hostPart, host, nil
}

// isKnownNode reports whether host is the address of a node in the fabric
func isKnownNode(ctx context.Context, fabric *Fabric, host string) (bool, error) {
	nodeAllowlist.Lock()
	entry := nodeAllowlist.fabrics[fabric.FabricName]
	nodeAllowlist.Unlock()

	if entry == nil || time.Now().After(entry.expires) {
		addresses, err := fetchNodeAddresses(ctx, fabric)
		if err != nil {
			if entry == nil {
				return false, err
			}
			// keep using the last known list if the apic is temporarily unavailable
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fabric.FabricName,
			}).Warning("could not refresh node allowlist, using previous list - ", err)
		} else {
			entry = &nodeAllowlistEntry{addresses: addresses, expires: time.Now().Add(nodeAllowlistTTL)}
			nodeAllowlist.Lock()
			nodeAllowlist.fabrics[fabric.FabricName] = entry
			nodeAllowlist.Unlock()
		}
	}

	_, ok := entry.addresses[strings.ToLower(host)]
	return ok, nil
}

func fetchNodeAddresses(ctx context.Context, fabric *Fabric) (map[string]struct{}, error) {
	con := newAciConnection(fabric, nil)
	if err := con.login(ctx); err != nil {
		return nil, err
	}
	data, err := con.GetByClassQuery(ctx, "topSystem", "")
	if err != nil {
		return nil, err
	}

	fields := append([]string{}, nodeAddressFields...)
	for _, field := range fabric.DiscoveryConfig.TargetFields {
		if field != "aci_exporter_fabric" {
			fields = append(fields, field)
		}
	}

	addresses := make(map[string]struct{})
	gjson.Get(data, "imdata.#.topSystem.attributes").ForEach(func(_, attributes gjson.Result) bool {
		for _, field := range fields {
			value := strings.ToLower(attributes.Get(field).String())
			if value != "" && value != "0.0.0.0" && value != "::" {
				addresses[value] = struct{}{}
			}
		}
		return true
	})
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no node addresses returned from topSystem")
	}
	return addresses, nil
}
