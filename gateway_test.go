package gateway

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/fall"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
)

type FallthroughCase struct {
	test.Case
	FallthroughZones    []string
	FallthroughExpected bool
}

type Fallen struct {
	error
}

func LookupResultFromAddr(cname *string, addr netip.Addr) LookupResult {
	var cnameTarget string
	if cname != nil {
		cnameTarget = *cname
	}
	return LookupResult{
		CNAMETarget: cnameTarget,
		Addresses:   []netip.Addr{addr},
	}
}

func TestLookup(t *testing.T) {
	ctrl := &KubeController{hasSynced: true}

	gw := newGateway()
	gw.Zones = []string{"example.com."}
	gw.Next = test.NextHandler(dns.RcodeSuccess, nil)
	gw.ExternalAddrFunc = gw.SelfAddress
	gw.Controller = ctrl
	real := []string{"Ingress", "Service", "HTTPRoute", "TLSRoute", "GRPCRoute", "DNSEndpoint"}
	fake := []string{"Pod", "Gateway"}

	for _, resource := range real {
		if found := gw.lookupResource(resource); found == nil {
			t.Errorf("Could not lookup supported resource %s", resource)
		}
	}

	for _, resource := range fake {
		if found := gw.lookupResource(resource); found != nil {
			t.Errorf("Located unsupported resource %s", resource)
		}
	}
}

func TestPlugin(t *testing.T) {
	ctrl := &KubeController{hasSynced: true}

	gw := newGateway()
	gw.Zones = []string{"example.com."}
	gw.Next = test.NextHandler(dns.RcodeSuccess, nil)
	gw.ExternalAddrFunc = gw.SelfAddress
	gw.Controller = ctrl
	setupLookupFuncs(gw)

	ctx := context.TODO()
	for i, tc := range tests {
		r := tc.Msg()
		w := dnstest.NewRecorder(&test.ResponseWriter{})

		_, err := gw.ServeDNS(ctx, w, r)
		if err != tc.Error {
			t.Errorf("Test %d expected no error, got %v", i, err)
			return
		}
		if tc.Error != nil {
			continue
		}

		resp := w.Msg

		if resp == nil {
			t.Fatalf("Test %d, got nil message and no error for %q", i, r.Question[0].Name)
		}
		if err = test.SortAndCheck(resp, tc); err != nil {
			t.Errorf("Test %d failed with error: %v", i, err)
		}
	}
}

func TestPluginFallthrough(t *testing.T) {
	ctrl := &KubeController{hasSynced: true}
	gw := newGateway()
	gw.Zones = []string{"example.com."}
	gw.Next = test.NextHandler(dns.RcodeSuccess, Fallen{})
	gw.ExternalAddrFunc = gw.SelfAddress
	gw.Controller = ctrl
	setupLookupFuncs(gw)

	ctx := context.TODO()
	for i, tc := range testsFallthrough {
		r := tc.Msg()
		w := dnstest.NewRecorder(&test.ResponseWriter{})

		gw.Fall = fall.F{Zones: tc.FallthroughZones}
		_, err := gw.ServeDNS(ctx, w, r)

		if errors.As(err, &Fallen{}) && !tc.FallthroughExpected {
			t.Fatalf("Test %d query resulted unexpectedly in a fall through instead of a response", i)
		}
		if err == nil && tc.FallthroughExpected {
			t.Fatalf("Test %d query resulted unexpectedly in a response instead of a fall through", i)
		}
	}
}

var tests = []test.Case{
	// Existing Service IPv4 | Test 0
	{
		Qname: "svc1.ns1.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svc1.ns1.example.com.   60  IN  A   192.0.1.1"),
		},
	},
	// Existing Ingress | Test 1
	{
		Qname: "domain.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("domain.example.com. 60  IN  A   192.0.0.1"),
		},
	},
	// Service takes precedence over Ingress | Test 2
	{
		Qname: "svc2.ns1.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svc2.ns1.example.com.   60  IN  A   192.0.1.2"),
		},
	},
	// Non-existing Service | Test 3
	{
		Qname: "svcX.ns1.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("example.com.  60  IN  SOA dns1.kube-system.example.com. hostmaster.example.com. 1499347823 7200 1800 86400 5"),
		},
	},
	// Non-existing Ingress | Test 4
	{
		Qname: "d0main.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("example.com.  60  IN  SOA dns1.kube-system.example.com. hostmaster.example.com. 1499347823 7200 1800 86400 5"),
		},
	},
	// SOA for the existing domain | Test 5
	{
		Qname: "domain.example.com.", Qtype: dns.TypeSOA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.SOA("example.com.  60  IN  SOA dns1.kube-system.example.com. hostmaster.example.com. 1499347823 7200 1800 86400 5"),
		},
	},
	// Service with no public addresses | Test 6
	{
		Qname: "svc3.ns1.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("example.com.  60  IN  SOA dns1.kube-system.example.com. hostmaster.example.com. 1499347823 7200 1800 86400 5"),
		},
	},
	// Real service, wrong query type | Test 7
	{
		Qname: "svc3.ns1.example.com.", Qtype: dns.TypeCNAME, Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("example.com.  60  IN  SOA dns1.kube-system.example.com. hostmaster.example.com. 1499347823 7200 1800 86400 5"),
		},
	},
	// Ingress FQDN == zone | Test 8
	{
		Qname: "example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("example.com.    60  IN  A   192.0.0.3"),
		},
	},
	// Existing Ingress with a mix of lower and upper case letters | Test 9
	{
		Qname: "dOmAiN.eXamPLe.cOm.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("domain.example.com. 60  IN  A   192.0.0.1"),
		},
	},
	// Existing Service with a mix of lower and upper case letters | Test 10
	{
		Qname: "svC1.Ns1.exAmplE.Com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svc1.ns1.example.com.   60  IN  A   192.0.1.1"),
		},
	},
	// Test 11
	{
		Qname: "svc2.ns1.example.com.", Qtype: dns.TypeAAAA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{},
		Ns: []dns.RR{
			test.SOA("example.com.  60  IN  SOA dns1.kube-system.example.com. hostmaster.example.com. 1499347823 7200 1800 86400 5"),
		},
	},
	// Test 12
	{
		Qname: "svc1.ns1.example.com.", Qtype: dns.TypeAAAA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.AAAA("svc1.ns1.example.com.    60  IN  AAAA    fd12:3456:789a:1::"),
		},
	},
	// basic gateway API lookup | Test 13
	{
		Qname: "domain.gw.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("domain.gw.example.com.  60  IN  A   192.0.2.1"),
		},
	},
	// Ingress takes precedence over gateway API | Test 14
	{
		Qname: "shadow.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("shadow.example.com. 60  IN  A   192.0.0.4"),
		},
	},
	// lookup apex NS record | Test 17
	{
		Qname: "example.com.", Qtype: dns.TypeNS, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.NS("example.com.   60  IN  NS  dns1.kube-system.example.com"),
		},
		Extra: []dns.RR{
			test.A("dns1.kube-system.example.com.   60  IN  A   192.0.1.53"),
		},
	},
	// Lookup that relies on a wildcard | Test 18
	{
		Qname: "not-explicitly-defined-label.wildcard.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("not-explicitly-defined-label.wildcard.example.com. 60  IN  A   192.0.0.6"),
		},
	},
	// Lookup with a matching wildcard but a more specific entry | Test 19
	{
		Qname: "specific-subdomain.wildcard.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("specific-subdomain.wildcard.example.com. 60  IN  A   192.0.0.7"),
		},
	},
	// Direct CNAME query | Test 20
	{
		Qname: "recursive.endpoint.example.com.", Qtype: dns.TypeCNAME, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.CNAME("recursive.endpoint.example.com. 60 IN CNAME cname.endpoint.example.com."),
		},
	},
	// A query for a CNAME record should return the CNAME and the A record | Test 21
	{
		Qname: "cname.endpoint.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.CNAME("cname.endpoint.example.com. 60 IN CNAME domain.endpoint.example.com."),
			test.A("domain.endpoint.example.com. 60 IN A 192.0.4.1"),
		},
	},
	// Recursive CNAME query should follow the chain | Test 22
	{
		Qname: "recursive.endpoint.example.com.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.CNAME("recursive.endpoint.example.com. 60 IN CNAME cname.endpoint.example.com."),
			test.CNAME("cname.endpoint.example.com. 60 IN CNAME domain.endpoint.example.com."),
			test.A("domain.endpoint.example.com. 60 IN A 192.0.4.1"),
		},
	},
}

var testsFallthrough = []FallthroughCase{
	// Match found, fallthrough enabled | Test 0
	{
		Case:             test.Case{Qname: "example.com.", Qtype: dns.TypeA},
		FallthroughZones: []string{"."}, FallthroughExpected: false,
	},
	// No match found, fallthrough enabled | Test 1
	{
		Case:             test.Case{Qname: "non-existent.example.com.", Qtype: dns.TypeA},
		FallthroughZones: []string{"."}, FallthroughExpected: true,
	},
	// Match found, fallthrough for different zone | Test 2
	{
		Case:             test.Case{Qname: "example.com.", Qtype: dns.TypeA},
		FallthroughZones: []string{"not-example.com."}, FallthroughExpected: false,
	},
	// No match found, fallthrough for different zone | Test 3
	{
		Case:             test.Case{Qname: "non-existent.example.com.", Qtype: dns.TypeA},
		FallthroughZones: []string{"not-example.com."}, FallthroughExpected: false,
	},
	// No fallthrough on gw apex | Test 4
	{
		Case:             test.Case{Qname: "dns1.kube-system.example.com.", Qtype: dns.TypeA},
		FallthroughZones: []string{"."}, FallthroughExpected: false,
	},
}

var testServiceIndexes = map[string][]LookupResult{
	"svc1.ns1":         {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.1.1")), LookupResultFromAddr(nil, netip.MustParseAddr("fd12:3456:789a:1::"))},
	"svc2.ns1":         {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.1.2"))},
	"svc3.ns1":         {},
	"dns1.kube-system": {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.1.53"))},
}

func testServiceLookup(keys []string) (results []LookupResult) {
	for _, key := range keys {
		results = append(results, testServiceIndexes[strings.ToLower(key)]...)
	}
	return results
}

var testIngressIndexes = map[string][]LookupResult{
	"domain.example.com":                      {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.1"))},
	"svc2.ns1.example.com":                    {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.2"))},
	"example.com":                             {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.3"))},
	"shadow.example.com":                      {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.4"))},
	"shadow-vs.example.com":                   {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.5"))},
	"*.wildcard.example.com":                  {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.6"))},
	"specific-subdomain.wildcard.example.com": {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.0.7"))},
}

func testIngressLookup(keys []string) (results []LookupResult) {
	for _, key := range keys {
		results = append(results, testIngressIndexes[strings.ToLower(key)]...)
	}
	return results
}

var testRouteIndexes = map[string][]LookupResult{
	"domain.gw.example.com": {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.2.1"))},
	"shadow.example.com":    {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.2.4"))},
}

func testRouteLookup(keys []string) (results []LookupResult) {
	for _, key := range keys {
		results = append(results, testRouteIndexes[strings.ToLower(key)]...)
	}
	return results
}

var testDNSEndpointIndexes = map[string][]LookupResult{
	"domain.endpoint.example.com":    {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.4.1"))},
	"endpoint.example.com":           {LookupResultFromAddr(nil, netip.MustParseAddr("192.0.4.4"))},
	"cname.endpoint.example.com":     {LookupResult{CNAMETarget: "domain.endpoint.example.com", Addresses: nil}},
	"recursive.endpoint.example.com": {LookupResult{CNAMETarget: "cname.endpoint.example.com", Addresses: nil}},
}

func testDNSEndpointLookup(keys []string) (results []LookupResult) {
	for _, key := range keys {
		results = append(results, testDNSEndpointIndexes[strings.ToLower(key)]...)
	}
	return results
}

func setupLookupFuncs(gw *Gateway) {
	// Reorder resources for tests
	gw.Resources = nil

	// Add Service first so it takes precedence
	if svc := staticResources[4]; svc != nil {
		svc.lookup = testServiceLookup
		gw.Resources = append(gw.Resources, svc)
	}

	// Add the rest of the resources
	if ing := staticResources[3]; ing != nil {
		ing.lookup = testIngressLookup
		gw.Resources = append(gw.Resources, ing)
	}
	if http := staticResources[0]; http != nil {
		http.lookup = testRouteLookup
		gw.Resources = append(gw.Resources, http)
	}
	if tls := staticResources[1]; tls != nil {
		tls.lookup = testRouteLookup
		gw.Resources = append(gw.Resources, tls)
	}
	if grpc := staticResources[2]; grpc != nil {
		grpc.lookup = testRouteLookup
		gw.Resources = append(gw.Resources, grpc)
	}
	if dns := staticResources[5]; dns != nil {
		dns.lookup = testDNSEndpointLookup
		gw.Resources = append(gw.Resources, dns)
	}
}

// TestRecursiveCNAME tests that recursive CNAME resolution works correctly
func TestRecursiveCNAME(t *testing.T) {
	ctrl := &KubeController{hasSynced: true}

	gw := newGateway()
	gw.Zones = []string{"example.org."}
	gw.Next = test.NextHandler(dns.RcodeSuccess, nil)
	gw.ExternalAddrFunc = gw.SelfAddress
	gw.Controller = ctrl

	// Create custom lookup functions for this test
	gw.Resources = nil
	dnsResource := staticResources[5]
	dnsResource.lookup = func(keys []string) (results []LookupResult) {
		// Custom lookup function that returns CNAME records for our test domain
		for _, key := range keys {
			switch strings.ToLower(key) {
			case "recursive.endpoint.example.org":
				results = append(results, LookupResult{
					CNAMETarget: "cname.endpoint.example.org",
				})
			case "cname.endpoint.example.org":
				results = append(results, LookupResult{
					CNAMETarget: "domain.endpoint.example.org",
				})
			case "domain.endpoint.example.org":
				results = append(results, LookupResult{
					Addresses: []netip.Addr{netip.MustParseAddr("192.0.4.1")},
				})
			}
		}
		return results
	}
	gw.Resources = append(gw.Resources, dnsResource)

	// Create a test case specifically for recursive CNAME resolution
	tc := test.Case{
		Qname: "recursive.endpoint.example.org.", Qtype: dns.TypeA, Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.CNAME("recursive.endpoint.example.org. 60 IN CNAME cname.endpoint.example.org."),
			test.CNAME("cname.endpoint.example.org. 60 IN CNAME domain.endpoint.example.org."),
			test.A("domain.endpoint.example.org. 60 IN A 192.0.4.1"),
		},
	}

	ctx := context.TODO()
	r := tc.Msg()
	w := dnstest.NewRecorder(&test.ResponseWriter{})

	_, err := gw.ServeDNS(ctx, w, r)
	if err != tc.Error {
		t.Errorf("Expected no error, got %v", err)
		return
	}

	resp := w.Msg
	if resp == nil {
		t.Fatalf("Got nil message and no error for %q", r.Question[0].Name)
	}

	// Print the actual response for debugging
	t.Logf("Response: %+v", resp)
	for i, rr := range resp.Answer {
		t.Logf("Answer[%d]: %+v", i, rr)
	}

	// Verify that the response contains at least one record
	if len(resp.Answer) == 0 {
		t.Errorf("Expected at least one record in the answer section, got 0")
		return
	}

	// Verify that the response contains a CNAME record pointing to the correct target
	found := false
	for _, rr := range resp.Answer {
		if rr.Header().Rrtype == dns.TypeCNAME {
			if cname, ok := rr.(*dns.CNAME); ok {
				if cname.Hdr.Name == "recursive.endpoint.example.org." && cname.Target == "cname.endpoint.example.org." {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("Expected to find a CNAME record from recursive.endpoint.example.org. to cname.endpoint.example.org.")
	}

	// Verify that the response contains a CNAME record for the second hop
	found = false
	for _, rr := range resp.Answer {
		if rr.Header().Rrtype == dns.TypeCNAME {
			if cname, ok := rr.(*dns.CNAME); ok {
				if cname.Hdr.Name == "cname.endpoint.example.org." && cname.Target == "domain.endpoint.example.org." {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("Expected to find a CNAME record from cname.endpoint.example.org. to domain.endpoint.example.org.")
	}

	// Verify that the response contains an A record for the final target
	found = false
	for _, rr := range resp.Answer {
		if rr.Header().Rrtype == dns.TypeA {
			if a, ok := rr.(*dns.A); ok {
				if a.Hdr.Name == "domain.endpoint.example.org." && a.A.String() == "192.0.4.1" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("Expected to find an A record for domain.endpoint.example.org. with IP 192.0.4.1")
	}
}
