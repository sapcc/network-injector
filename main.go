/*
Copyright 2022 SAP SE.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/network-injector/config"
	"github.com/sapcc/network-injector/controllers"
)

func main() {
	flag.StringVar(&config.MetricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	flag.StringVar(&config.Hostname, "host", "", "Use host name.")
	flag.StringVar(&config.ProxyPath, "proxy-path", "/var/run/socat-proxy/proxy.sock",
		"Use unix domain socket for upstream connections.")
	flag.StringVar(&config.UpstreamHost, "upstream-host", "localhost",
		"Use host name override for upstream connection")
	flag.StringVar(&config.InjectorDNS, "injector-dns", "",
		"Name for injected service, will be used for DNS resolving inside the private network")
	flag.StringVar(&config.NetworkTag, "network-tag", "",
		"OpenStack Network tag to scan for")
	flag.Int64Var(&config.Interval, "interval", 60,
		"Interval in seconds for scanning tagged networks.")
	flag.Parse()

	if config.NetworkTag == "" {
		log.Fatal("Network tag for scanned OpenStack Networks (-network-tag) is required.")
	}

	if config.Hostname == "" {
		var err error
		if config.Hostname, err = os.Hostname(); err != nil {
			panic(err)
		}
	}

	// prometheus metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	// configure logger
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.SetPrefix("network-injector")

	osc := &controllers.OpenStackController{}
	if err := osc.SetupOpenStack(); err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"The network injector %s for service '%s' is ready to scan for Networks tagged '%s'. "+
			"Interval=%d, Upstream=%s",
		config.Hostname, config.InjectorDNS, config.NetworkTag, config.Interval, config.UpstreamHost)

	ticker := time.NewTicker(time.Duration(config.Interval) * time.Second)
	quit := make(chan struct{})
	go func() {
		// Run instant
		if err := osc.ScanForTaggedNetworks(); err != nil {
			log.Print(err)
		}

		// Periodically
		for {
			select {
			case <-ticker.C:
				if err := osc.ScanForTaggedNetworks(); err != nil {
					log.Print(err)
				}
				osc.CollectStats()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// blocks
	if err := http.ListenAndServe(config.MetricsAddr, nil); err != nil {
		log.Fatal(err)
	}
}
