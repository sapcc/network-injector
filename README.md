# OpenStack Network injector

Agent to expose specifc (kubernetes) services/endpoints to a private OpenStack Network.

## Description
This project works similar to OpenStacks DHCP-Agent and Metadata Service. 
It spawns a HAProxy instance for every specifically tagged OpenStack Network inside an isolated Linux Network Namespace with a vETH pair tunnel between
host- and haproxy-namespace.
An compatible OpenStack L2 agent (like openstack-linuxbridge-agent) can detect the host vETH interface and connect it to a Fabric.
HAProxy ultimately relays (HTTP) traffic to a unix domain socket.


### How it works
The manager scans periodically for specific configurable tags on OpenStack Networks. If found, it does following
for every network it discovers:

1. create network namespace `qinjector-<network-id>`
2. create port for (configurable) device host-id and network:injector owner
3. create veth pair, with source interface called `tap<port-id:11>` (truncated at 11 characters)
4. put other veth interface into the network namespace and configures IP/Routes/MAC according to the port properties.
5. Spawns haproxy in this network namespace which listens on http/80 and redirects traffic to a unix domain socket

L2 Agents, like the openstack-linuxbridge-agent, detect the tap interface and bridge them to a tagged bond interface of 
the corresponding network.

## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

