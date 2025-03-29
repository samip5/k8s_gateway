package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	core "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	fakeRest "k8s.io/client-go/rest/fake"
	"sigs.k8s.io/external-dns/endpoint"
	gatewayapi_v1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapi_v1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayClient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gwFake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

// taken from external-dns/source/crd_test.go
func addKnownTypes(scheme *runtime.Scheme, groupVersion schema.GroupVersion) {
	scheme.AddKnownTypes(groupVersion,
		&endpoint.DNSEndpoint{},
		&endpoint.DNSEndpointList{},
	)
	metav1.AddToGroupVersion(scheme, groupVersion)
}

func defaultHeader() http.Header {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	return header
}

func objBody(codec runtime.Encoder, obj runtime.Object) io.ReadCloser {
	return io.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
}

func fakeRESTClient(endpoints []*endpoint.Endpoint, apiVersion string, kind string, namespace string, name string, annotations map[string]string, labels map[string]string, _ *testing.T) rest.Interface {
	groupVersion, _ := schema.ParseGroupVersion(apiVersion)
	scheme := runtime.NewScheme()
	addKnownTypes(scheme, groupVersion)

	// Create your DNSEndpoint object.
	dnsEndpoint := &endpoint.DNSEndpoint{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
			Generation:  1,
		},
		Spec: endpoint.DNSEndpointSpec{
			Endpoints: endpoints,
		},
	}
	var dnsEndpointList endpoint.DNSEndpointList

	codecFactory := serializer.WithoutConversionCodecFactory{
		CodecFactory: serializer.NewCodecFactory(scheme),
	}

	client := &fakeRest.RESTClient{
		GroupVersion:         groupVersion,
		VersionedAPIPath:     "/apis/" + apiVersion,
		NegotiatedSerializer: codecFactory,
		Client: fakeRest.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			codec := codecFactory.LegacyCodec(groupVersion)
			switch p, m := req.URL.Path, req.Method; {

			case p == "/apis/"+apiVersion+"/"+strings.ToLower(kind)+"s" && m == http.MethodGet,
				p == "/apis/"+apiVersion+"/namespaces/"+namespace+"/"+strings.ToLower(kind)+"s" && m == http.MethodGet,
				(strings.HasPrefix(p, "/apis/"+apiVersion+"/namespaces/") && strings.HasSuffix(p, strings.ToLower(kind)+"s") && m == http.MethodGet):
				dnsEndpointList.Items = []endpoint.DNSEndpoint{*dnsEndpoint}
				return &http.Response{StatusCode: http.StatusOK, Header: defaultHeader(), Body: objBody(codec, &dnsEndpointList)}, nil

			case p == "/apis/"+apiVersion+"/namespaces/"+namespace+"/"+strings.ToLower(kind)+"s" && m == http.MethodPost:
				return &http.Response{
					StatusCode: http.StatusCreated,
					Header:     defaultHeader(),
					Body:       objBody(codec, dnsEndpoint),
				}, nil

			case p == "/apis/"+apiVersion+"/namespaces/"+namespace+"/"+strings.ToLower(kind)+"s/"+name+"/status" && m == http.MethodPut:
				return &http.Response{StatusCode: http.StatusOK, Header: defaultHeader(), Body: objBody(codec, dnsEndpoint)}, nil

			default:
				return nil, fmt.Errorf("unexpected request: %#v\n%#v", req.URL, req)
			}
		}),
	}

	return client
}

func TestController(t *testing.T) {
	client := fake.NewClientset()
	gwClient := gwFake.NewClientset()
	ctrl := &KubeController{
		client:    client,
		gwClient:  gwClient,
		hasSynced: true,
	}
	addServices(client)
	addIngresses(client)
	addGateways(gwClient)
	addHTTPRoutes(gwClient)
	addTLSRoutes(gwClient)
	addGRPCRoutes(gwClient)
	addDNSEndpoints(fakeRESTClient(testDNSEndpoints["dual.example.com"].Spec.Endpoints, "externaldns.k8s.io/v1alpha1", "DNSEndpoint", "ns1", "ep1", nil, nil, t))

	gw := newGateway()
	gw.Zones = []string{"example.com."}
	gw.Next = test.NextHandler(dns.RcodeSuccess, nil)
	gw.Controller = ctrl

	for index, testObj := range testIngresses {
		found, _ := ingressHostnameIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("Ingress key %s not found in index: %v", index, found)
		}
		ips := fetchIngressLoadBalancerIPs(testObj.Status.LoadBalancer.Ingress)
		if len(ips) != 1 {
			t.Errorf("Unexpected number of IPs found %d", len(ips))
		}
	}

	for index, testObj := range testServices {
		found, _ := serviceHostnameIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("Service key %s not found in index: %v", index, found)
		}
		ips := fetchServiceLoadBalancerIPs(testObj.Status.LoadBalancer.Ingress)
		if len(ips) != 1 {
			t.Errorf("Unexpected number of IPs found %d", len(ips))
		}
	}

	for index, testObj := range testBadServices {
		found, _ := serviceHostnameIndexFunc(testObj)
		if isFound(index, found) {
			t.Errorf("Unexpected service key %s found in index: %v", index, found)
		}
	}

	for index, testObj := range testHTTPRoutes {
		found, _ := httpRouteHostnameIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("HTTPRoute key %s not found in index: %v", index, found)
		}
	}

	for index, testObj := range testTLSRoutes {
		found, _ := tlsRouteHostnameIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("TLSRoute key %s not found in index: %v", index, found)
		}
	}

	for index, testObj := range testGRPCRoutes {
		found, _ := grpcRouteHostnameIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("GRPC key %s not found in index: %v", index, found)
		}
	}

	for index, testObj := range testGateways {
		found, _ := gatewayIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("Gateway key %s not found in index: %v", index, found)
		}
	}

	for index, testObj := range testDNSEndpoints {
		found, _ := dnsEndpointTargetIndexFunc(testObj)
		if !isFound(index, found) {
			t.Errorf("DNSEndpoint key %s not found in index: %v", index, found)
		}
	}
}

func isFound(s string, ss []string) bool {
	for _, str := range ss {
		if str == s {
			return true
		}
	}
	return false
}

func addServices(client kubernetes.Interface) {
	ctx := context.TODO()
	for _, svc := range testServices {
		_, err := client.CoreV1().Services("ns1").Create(ctx, svc, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create Service Objects :%s", err)
		}
	}
}

func addIngresses(client kubernetes.Interface) {
	ctx := context.TODO()
	for _, ingress := range testIngresses {
		_, err := client.NetworkingV1().Ingresses("ns1").Create(ctx, ingress, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create Ingress Objects :%s", err)
		}
	}
}

func addGateways(client gatewayClient.Interface) {
	ctx := context.TODO()
	for _, gw := range testGateways {
		_, err := client.GatewayV1().Gateways("ns1").Create(ctx, gw, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create a Gateway Object :%s", err)
		}
	}
}

func addHTTPRoutes(client gatewayClient.Interface) {
	ctx := context.TODO()
	for _, r := range testHTTPRoutes {
		_, err := client.GatewayV1().HTTPRoutes("ns1").Create(ctx, r, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create a HTTPRoute Object :%s", err)
		}
	}
}

func addTLSRoutes(client gatewayClient.Interface) {
	ctx := context.TODO()
	for _, r := range testTLSRoutes {
		_, err := client.GatewayV1alpha2().TLSRoutes("ns1").Create(ctx, r, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create a TLSRoutes Object :%s", err)
		}
	}
}

func addGRPCRoutes(client gatewayClient.Interface) {
	ctx := context.TODO()
	for _, r := range testGRPCRoutes {
		_, err := client.GatewayV1().GRPCRoutes("ns1").Create(ctx, r, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create a GRPC Object :%s", err)
		}
	}

	for _, r2 := range testGRPCRoutesLegacy {
		_, err := client.GatewayV1alpha2().GRPCRoutes("ns1").Create(ctx, r2, metav1.CreateOptions{})
		if err != nil {
			log.Warningf("Failed to Create a GRPC Object :%s", err)
		}
	}
}

func addDNSEndpoints(client rest.Interface) {
	ctx := context.TODO()
	for _, ep := range testDNSEndpoints {
		_, err := client.Post().Resource("dnsendpoints").Namespace("ns1").Body(ep).Do(ctx).Get()
		if err != nil {
			log.Warningf("Failed to Create a DNSEndpoint Object :%s", err)
		}
	}
}

var testIngresses = map[string]*networking.Ingress{
	"a.example.org": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ing1",
			Namespace: "ns1",
		},
		Spec: networking.IngressSpec{
			Rules: []networking.IngressRule{
				{
					Host: "a.example.org",
				},
			},
		},
		Status: networking.IngressStatus{
			LoadBalancer: networking.IngressLoadBalancerStatus{
				Ingress: []networking.IngressLoadBalancerIngress{
					{IP: "192.0.0.1"},
				},
			},
		},
	},
	"example.org": {
		Spec: networking.IngressSpec{
			Rules: []networking.IngressRule{
				{
					Host: "example.org",
				},
			},
		},
		Status: networking.IngressStatus{
			LoadBalancer: networking.IngressLoadBalancerStatus{
				Ingress: []networking.IngressLoadBalancerIngress{
					{IP: "192.0.0.2"},
				},
			},
		},
	},
}

var testServices = map[string]*core.Service{
	"svc1.ns1": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "ns1",
		},
		Spec: core.ServiceSpec{
			Type: core.ServiceTypeLoadBalancer,
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{
					{IP: "192.0.0.1"},
				},
			},
		},
	},
	"svc2.ns1": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc2",
			Namespace: "ns1",
		},
		Spec: core.ServiceSpec{
			Type: core.ServiceTypeLoadBalancer,
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{
					{IP: "192.0.0.2"},
				},
			},
		},
	},
	"annotation": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc3",
			Namespace: "ns1",
			Annotations: map[string]string{
				"coredns.io/hostname": "annotation",
			},
		},
		Spec: core.ServiceSpec{
			Type: core.ServiceTypeLoadBalancer,
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{
					{IP: "192.0.0.3"},
				},
			},
		},
	},
}

var testGateways = map[string]*gatewayapi_v1.Gateway{
	"ns1/gw-1": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw-1",
			Namespace: "ns1",
		},
		Spec: gatewayapi_v1.GatewaySpec{},
		Status: gatewayapi_v1.GatewayStatus{
			Addresses: []gatewayapi_v1.GatewayStatusAddress{
				{
					Value: "192.0.2.100",
				},
			},
		},
	},
	"ns1/gw-2": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw-2",
			Namespace: "ns1",
		},
	},
}

var testHTTPRoutes = map[string]*gatewayapi_v1.HTTPRoute{
	"route-1.gw-1.example.com": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-1",
			Namespace: "ns1",
		},
		Spec: gatewayapi_v1.HTTPRouteSpec{
			//ParentRefs: []gatewayapi_v1.ParentRef{},
			Hostnames: []gatewayapi_v1.Hostname{"route-1.gw-1.example.com"},
		},
	},
}

var testTLSRoutes = map[string]*gatewayapi_v1alpha2.TLSRoute{
	"route-1.gw-1.example.com": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-1",
			Namespace: "ns1",
		},
		Spec: gatewayapi_v1alpha2.TLSRouteSpec{
			//ParentRefs: []gatewayapi_v1.ParentRef{},
			Hostnames: []gatewayapi_v1alpha2.Hostname{
				"route-1.gw-1.example.com",
			},
		},
	},
}

var testGRPCRoutes = map[string]*gatewayapi_v1.GRPCRoute{
	"route-1.gw-1.example.com": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-1",
			Namespace: "ns1",
		},
		Spec: gatewayapi_v1.GRPCRouteSpec{
			//ParentRefs: []gatewayapi_v1.ParentRef{},
			Hostnames: []gatewayapi_v1.Hostname{"route-1.gw-1.example.com"},
		},
	},
}

var testGRPCRoutesLegacy = map[string]*gatewayapi_v1alpha2.GRPCRoute{
	"route-1.gw-1.example.com": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-1",
			Namespace: "ns1",
		},
		Spec: gatewayapi_v1.GRPCRouteSpec{
			//ParentRefs: []gatewayapi_v1.ParentRef{},
			Hostnames: []gatewayapi_v1alpha2.Hostname{"route-1.gw-1.example.com"},
		},
	},
}

var testBadServices = map[string]*core.Service{
	"svc1.ns2": {
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "ns2",
		},
		Spec: core.ServiceSpec{
			Type: core.ServiceTypeClusterIP,
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{
					{IP: "192.0.0.1"},
				},
			},
		},
	},
}

var testDNSEndpoints = map[string]*endpoint.DNSEndpoint{
	"dual.example.com": &endpoint.DNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ep1",
			Namespace: "ns1",
		},
		Spec: endpoint.DNSEndpointSpec{
			Endpoints: []*endpoint.Endpoint{
				{
					DNSName:    "dual.example.com",
					RecordType: "A",
					Targets:    []string{"192.0.2.200"},
				},
				{
					DNSName:    "dual.example.com",
					RecordType: "AAAA",
					Targets:    []string{"2001:db8::1"},
				},
			},
		},
	},
}
