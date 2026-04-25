package netbox

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtract(t *testing.T) {
	cases := []struct {
		name     string
		message  string
		hostname string
		want     []Entity
	}{
		{
			name:     "empty message yields only the device entity",
			message:  "",
			hostname: "router1.example.com",
			want: []Entity{
				{Type: EntityDevice, Value: "router1.example.com"},
			},
		},
		{
			name:     "no hostname and empty message yields nothing",
			message:  "",
			hostname: "",
			want:     nil,
		},
		{
			name:     "BGP neighbor down extracts ip, asn, interface, and the device",
			message:  "BGP peer 10.0.0.5 (AS65001) on ge-0/0/1 went down",
			hostname: "router1",
			want: []Entity{
				{Type: EntityDevice, Value: "router1"},
				{Type: EntityIP, Value: "10.0.0.5"},
				{Type: EntityASN, Value: "65001"},
				{Type: EntityInterface, Value: "ge-0/0/1", Context: map[string]string{"device": "router1"}},
			},
		},
		{
			name:     "duplicate IPs in message produce a single entity",
			message:  "ARP from 10.0.0.1 -> 10.0.0.1 again",
			hostname: "sw-1",
			want: []Entity{
				{Type: EntityDevice, Value: "sw-1"},
				{Type: EntityIP, Value: "10.0.0.1"},
			},
		},
		{
			name:     "IPv4 prefix is extracted as a prefix entity",
			message:  "Static route to 192.168.10.0/24",
			hostname: "core-1",
			want: []Entity{
				{Type: EntityDevice, Value: "core-1"},
				{Type: EntityPrefix, Value: "192.168.10.0/24"},
			},
		},
		{
			name:     "ASN normalization strips the AS prefix and whitespace",
			message:  "neighbor AS 65002 established",
			hostname: "edge-1",
			want: []Entity{
				{Type: EntityDevice, Value: "edge-1"},
				{Type: EntityASN, Value: "65002"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Extract(tc.message, tc.hostname)
			if !equalEntities(got, tc.want) {
				t.Fatalf("Extract(%q, %q) =\n  %+v\nwant\n  %+v", tc.message, tc.hostname, got, tc.want)
			}
		})
	}
}

// equalEntities compares two Entity slices ignoring order.
func equalEntities(a, b []Entity) bool {
	if len(a) != len(b) {
		return false
	}
	ka := make([]string, len(a))
	kb := make([]string, len(b))
	for i := range a {
		ka[i] = entityKey(a[i])
	}
	for i := range b {
		kb[i] = entityKey(b[i])
	}
	sort.Strings(ka)
	sort.Strings(kb)
	if !reflect.DeepEqual(ka, kb) {
		return false
	}
	// Also assert context coexists when expected — find each b in a by key and compare context.
	mapByKey := func(s []Entity) map[string]Entity {
		m := make(map[string]Entity, len(s))
		for _, e := range s {
			m[entityKey(e)] = e
		}
		return m
	}
	ma, mb := mapByKey(a), mapByKey(b)
	for k, ea := range ma {
		eb := mb[k]
		if !reflect.DeepEqual(ea.Context, eb.Context) {
			return false
		}
	}
	return true
}
