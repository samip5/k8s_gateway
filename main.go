package main

import (
	"fmt"

	_ "github.com/coredns/coredns/core/plugin"
	gateway "github.com/k8s-gateway/k8s_gateway/plugin/k8s_gateway"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/coremain"
)

var dropPlugins = map[string]bool{
	"kubernetes":   true,
	"k8s_external": true,
}

var pluginVersion = "dev"

func init() {
	var directives []string
	var alreadyAdded bool

	for _, name := range dnsserver.Directives {

		if dropPlugins[name] {
			if !alreadyAdded {
				directives = append(directives, gateway.Name())
				alreadyAdded = true
			}
			continue
		}
		directives = append(directives, name)
	}

	dnsserver.Directives = directives

}

func main() {
	caddy.AppVersion = fmt.Sprintf("%s+%s-%s", coremain.CoreVersion, gateway.Name(), pluginVersion)
	coremain.Run()
}
