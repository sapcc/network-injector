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

package controllers

import (
	"fmt"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/dns"
	"github.com/sapcc/network-injector/config"
	"log"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/portsbinding"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/openstack/clientconfig"
)

type OpenStackController struct {
	neutron *gophercloud.ServiceClient
	haproxy *HAProxyController
}

func (o *OpenStackController) SetupOpenStack() error {
	ao, err := clientconfig.AuthOptions(nil)
	if err != nil {
		return err
	}
	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err != nil {
		return err
	}
	err = openstack.Authenticate(provider, *ao)
	if err != nil {
		return err
	}

	if o.neutron, err = openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{}); err != nil {
		return err
	}

	o.haproxy = NewHAProxyController()
	return nil
}

func (o *OpenStackController) getInjectorPort(networkID string) (*ports.Port, error) {
	opts := ports.ListOpts{
		NetworkID:   networkID,
		DeviceOwner: GetDeviceOwner(),
	}

	var port *ports.Port
	pager := ports.List(o.neutron, opts)
	if err := pager.EachPage(func(page pagination.Page) (bool, error) {
		portList, err := ports.ExtractPorts(page)
		if err != nil {
			return false, err
		}

		if len(portList) == 1 {
			port = &portList[0]
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return port, nil
}
func (o *OpenStackController) EnableNetwork(network *networks.Network) error {
	// Create port
	injectorPort, err := o.getInjectorPort(network.ID)
	if err != nil {
		return err
	}

	if injectorPort == nil {
		log.Printf("Creating port for network %s (%s)", network.Name, network.ID)
		port := dns.PortCreateOptsExt{
			CreateOptsBuilder: portsbinding.CreateOptsExt{
				CreateOptsBuilder: ports.CreateOpts{
					Name:        config.NetworkTag + " injection port",
					DeviceOwner: GetDeviceOwner(),
					DeviceID:    "network-injector",
					NetworkID:   network.ID,
					TenantID:    network.TenantID,
				},
				HostID: config.Hostname,
			},
			DNSName: config.InjectorDNS,
		}

		var err error
		if injectorPort, err = ports.Create(o.neutron, port).Extract(); err != nil {
			return err
		}
		log.Printf("Port '%s' created", injectorPort.ID)
	}

	// Create network namespace with ip/mac
	ns, err := EnsureNetworkNamespace(injectorPort, o.neutron)
	if err != nil {
		return err
	}

	if o.haproxy.isRunning(injectorPort.NetworkID) {
		// Nothing to do
		return nil
	}

	// Run haproxy inside network namespace
	if err := ns.EnableNetworkNamespace(); err != nil {
		return err
	}
	defer func() { _ = ns.Close() }()
	if _, err := o.haproxy.addInstance(injectorPort.NetworkID); err != nil {
		return err
	}
	if err := ns.DisableNetworkNamespace(); err != nil {
		return err
	}

	return nil
}

func (o *OpenStackController) DisableNetwork(network string) error {
	log.Printf("DisableNetwork(network='%s')", network)
	injectorPort, err := o.getInjectorPort(network)
	if err != nil {
		return err
	}

	if o.haproxy.isRunning(network) {
		if err := o.haproxy.removeInstance(network); err != nil {
			return err
		}
	}

	if err := DeleteNetworkNamespace(network); err != nil {
		return err
	}

	if injectorPort != nil {
		// Delete port
		if err := ports.Delete(o.neutron, injectorPort.ID).ExtractErr(); err != nil {
			return err
		}
	}

	return nil
}

func (o *OpenStackController) CollectStats() {
	o.haproxy.collectStats()
}

func (o *OpenStackController) ScanForTaggedNetworks() error {
	// We have the option of filtering the network list. If we want the full
	// collection, leave it as an empty struct
	opts := networks.ListOpts{
		Tags: config.NetworkTag,
	}

	// Retrieve a pager (i.e. a paginated collection)
	pager := networks.List(o.neutron, opts)

	var injectedNetworks []*networks.Network

	// Define an anonymous function to be executed on each page's iteration
	if err := pager.EachPage(func(page pagination.Page) (bool, error) {
		networkList, err := networks.ExtractNetworks(page)
		if err != nil {
			return false, err
		}

		for _, n := range networkList {
			for _, tag := range n.Tags {
				// just to be sure
				if tag == config.NetworkTag {
					injectedNetworks = append(injectedNetworks, &n)
				}
			}
		}
		return true, nil
	}); err != nil {
		return err
	}

	log.Printf("ScanForTaggedNetworks(): Found %d enabled networks", len(injectedNetworks))
	for _, injectedNetwork := range injectedNetworks {
		if err := o.EnableNetwork(injectedNetwork); err != nil {
			log.Print(err)
		}
	}

	// Check and disable networks not needed anymore
	for runningNetwork := range o.haproxy.instances {
		found := false
		for _, injectedNetwork := range injectedNetworks {
			if injectedNetwork.ID == runningNetwork {
				found = true
			}
		}

		if !found {
			// Remove network from agent
			if err := o.DisableNetwork(runningNetwork); err != nil {
				log.Print(err)
			}
		}
	}

	return nil
}

func GetDeviceOwner() string {
	return fmt.Sprintf("network:%s-injector", config.NetworkTag)
}
