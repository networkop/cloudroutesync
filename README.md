# cloudroutesync
An add-on for cloud-hosted routers that reads local routes via [rtnetlink](https://man7.org/linux/man-pages/man7/rtnetlink.7.html) and synchronizes them with a cloud routing table.

![](./image.png)

The main use cases are:

* Overlay-free self-managed Kuberentes clusters in the cloud - you can run BGP to establish reachability between PodCIDRs (e.g. KubeRouter, Calico)
* Multi/Hybrid cloud - sync local route table with subnets configured in a different environments (on-prem, another cloud).

> Note: The application relies on another routing daemon to program local netlink routes. This can be [FRR](http://docs.frrouting.org/en/latest/), [Quagga](https://www.nongnu.org/quagga/docs/quagga.html), [Bird](https://bird.network.cz/) or any other routing software suite.

## Currently Supported Clouds

* Azure
* AWS (coming soon)
* Openstack (maybe)


## Prerequisites

The application must be running on a cloud VM with enough IAM permissions to create/update cloud route table.

For example, in Azure this would require:

* Enabled system identity.
* Assigned "Network Contributor" role.

See Terraform [directory](./terraform) for more examples.

## Installation

To build a binary:

```
go get -v github.com/networkop/cloudroutesync
```

## Usage

```
Usage of cloudroutesync:
  -cloud string
    	public cloud providers [azure|aws|gcp]
  -debug
    	enable debug logging
  -event
    	enable event-based sync (default is periodic, controlled by 'sync')
  -netlink int
    	netlink polling interval in seconds (default 10)
  -sync int
    	cloud routing table sync interval in seconds (default 10)
```

It can run in two modes:

* Event-driven mode - cloud route table is only updated whenever there was a change detected in the netlink routing table. This mode is enabled with a `-event` flag.

* Periodic mode (default) - cloud route table is synced periodically based on the interval defined in the `-sync` flag.

## Demo

Using Azure as target environment.

1. Spin up a test environment with two VMs

```
cd ./terraform/azure
terraform init && terraform apply -auto-approve

```

3. SSH into both VMs and bring up the FRR routing daemon

```
router_ip=$(terraform output -json | jq -r '.public_address_router.value[0]')
ssh example@$router_ip
example@example-router-vm:~$ sudo CLOUD=azure docker-compose up -d
```

```
vm_ip=$(terraform output -json | jq -r '.public_address_vm.value[0]')
ssh example@$vm_ip
example@example-vm:~$ sudo CLOUD=azure docker-compose up -d
```

3. From a non-router VM and configure a BGP peering towards the cloud router

```
example@example-vm:~$ sudo docker exec -it example_frr_1 vtysh
conf
router bgp 
neighbor ROUTER-VM-PRIVATE-IP peer-group PEERS
```

4. From the same VM configure a new loopback IP and redistribute it into BGP

```
interface lo
ip address 198.51.100.100/32
!
router bgp 
redistribute connected
```


5. From a non-router VM start a ping towards router VM sourced from the new interface

```
ping ROUTER-VM-PRIVATE-IP -I 198.51.100.100
```

6. From a router VM start the `cloudroutesync` app 

```
cloudroutesync -cloud azure 
```

7. Observe how route table gets populated with the new prefix.

The ping from step #5 should now receive responses.
