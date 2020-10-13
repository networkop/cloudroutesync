package reconciler

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/networkop/cloudroutesync/pkg/route"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
)

var gcpReservedRanges = []*net.IPNet{
	route.ParseCIDR("199.36.153.4/30"),
	route.ParseCIDR("199.36.153.8/30"),
	route.ParseCIDR("0.0.0.0/8"),
	route.ParseCIDR("127.0.0.0/8"),
	route.ParseCIDR("169.254.0.0/16"),
	route.ParseCIDR("224.0.0.0/4"),
	route.ParseCIDR("255.255.255.255/32"),
}

var (
	maxOpWaitSeconds = 60
	opCheckPeriod    = 2
)

// GCP implementation details
// * GCP sets up interfaces with /32 mask
// * To reach GPC subnet default, dhcp sets up a single gateway /32
// * GCP gateway router does not respond to ARPs on anything other than its own IP
// * Linux kernel does not support installation of recursive routes
// * Routes recursed by Zebra/Bird will always point to subnet's default gateway
// The above means that cloudroutesync cannot install routes received from local subnet neighbors
// The only supported mode is installing routes received from outside of the local subnet

// GcpClient stores cloud client and values
type GcpClient struct {
	client                          *compute.Service
	projectID, zone, region         string
	instanceID, network, internalIP string
	subnet                          *net.IPNet
}

// NewGcpClient builds new GCP client
func NewGcpClient() (*GcpClient, error) {

	httpC, err := google.DefaultClient(context.TODO(), compute.ComputeScope)
	if err != nil {
		return nil, fmt.Errorf("Failed to build DefaultClient for GCP: %s", err)
	}

	client, err := compute.New(httpC)
	if err != nil {
		return nil, fmt.Errorf("Failed to get compute service client for GCP: %s", err)
	}

	project, err := metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("Failed to get projectID from metadata: %s", err)
	}

	zone, err := metadata.Zone()
	if err != nil {
		return nil, fmt.Errorf("Failed to get zone from metadata: %s", err)
	}

	internalIP, err := metadata.InternalIP()
	if err != nil {
		return nil, fmt.Errorf("Failed to get internalIP from metadata: %s", err)
	}

	instanceID, err := metadata.InstanceID()
	if err != nil {
		return nil, fmt.Errorf("Failed to get instanceID from metadata: %s", err)
	}

	zoneParts := strings.Split(zone, "-")

	return &GcpClient{
		client:     client,
		projectID:  project,
		zone:       zone,
		internalIP: internalIP,
		instanceID: instanceID,
		region:     strings.Join(zoneParts[0:len(zoneParts)-1], "-"),
	}, nil
}

// Cleanup removes any leftover resources
func (c *GcpClient) Cleanup() error {
	logrus.Infof("Azure cleanup currently not implemented")
	return nil
}

// Reconcile implements reconciler interface
func (c *GcpClient) Reconcile(rt *route.Table, eventSync bool, syncInterval int) {

	err := c.lookupNetwork()
	if err != nil {
		logrus.Infof("Failed to lookupNetwork: %s", err)
	}

	if eventSync {
		for range rt.SyncCh {
			err := c.syncRouteTable(rt)
			if err != nil {
				logrus.Infof("Failed to sync route table: %s", err)
			}
		}
	} else {
		for {
			select {
			case _ = <-rt.SyncCh:
				logrus.Debug("Received sync signal in periodic mode, ignoring")
			default:
				err := c.syncRouteTable(rt)
				if err != nil {
					logrus.Infof("Failed to sync route table: %s", err)
				}
				time.Sleep(time.Duration(syncInterval) * time.Second)
			}
		}
	}
}

func (c *GcpClient) fetchOwnedRoutes() ([]*compute.Route, error) {
	routes, err := c.client.Routes.
		List(c.projectID).
		Filter(fmt.Sprintf("name:%s*", uniquePrefix)).
		Do()
	if err != nil {
		return nil, fmt.Errorf("Failed to list routes for GCP: %s", err)
	}

	// If too many routes (>500), this won't work
	return routes.Items, nil
}

// GCP does not support installation of nexthops from local subnet
// This is due to the all interfaces having a /32 mask and linux kenel
// requiring routes to be recursively resolved before installing them in the FIB
func (c *GcpClient) buildRoutes(rt *route.Table) (result []*compute.Route) {
	for prefix, nextHop := range rt.Routes {
		// Skip nextHops that match local subnet
		if c.subnet.Contains(nextHop) {
			continue
		}
		// For all other cases set next-hop to self
		result = append(result, &compute.Route{
			Name:      uniquePrefix + "-" + prefixToName(prefix) + prefixToName(nextHop.String()),
			DestRange: prefix,
			Network:   c.network,
			NextHopIp: c.privateIP,
		})
	}
	return result
}

func prefixToName(prefix string) string {
	return strings.ReplaceAll(strings.Replace(prefix, "/", "slash", 1), ".", "-")
}

func containsRoute(routeList []*compute.Route, checkRoute *compute.Route) bool {
	logrus.Debugf("Checking if %s is in the list", checkRoute.Name)

	for _, route := range routeList {
		if route.Network == checkRoute.Network && route.NextHopIp == checkRoute.NextHopIp {
			return true
		}
	}
	return false
}

func (c *GcpClient) syncRouteTable(rt *route.Table) error {
	logrus.Infof("Syncing cloud route table")

	currentRoutes, err := c.fetchOwnedRoutes()
	if err != nil {
		return fmt.Errorf("Failed to fetchOwnedRoutes: %s", err)
	}
	logrus.Debugf("Current routes: %+v", currentRoutes)

	proposedRoutes := c.buildRoutes(rt)
	logrus.Debugf("Proposed routes: %+v", proposedRoutes)

	logrus.Debug("Checking if any routes need deleting")
	toDelete := []*compute.Route{}
	for _, currentRoute := range currentRoutes {
		if !containsRoute(proposedRoutes, currentRoute) {
			logrus.Debugf("Enqueuing DELETE operation for %s", currentRoute.Name)
			toDelete = append(toDelete, currentRoute)
		}
	}

	logrus.Debug("Checking if any routes need adding")
	toAdd := []*compute.Route{}
	for _, proposedRoute := range proposedRoutes {
		if !containsRoute(currentRoutes, proposedRoute) {
			logrus.Debugf("Enqueuing ADD operation for %s", proposedRoute.Name)
			toAdd = append(toAdd, proposedRoute)
		}
	}

	ops := []*compute.Operation{}

	for _, delete := range toDelete {
		logrus.Infof("Attempting to delete route %s", delete.Name)
		op, err := c.client.Routes.Delete(c.projectID, delete.Name).Do()
		if err != nil {
			logrus.Infof("Failed to initiate route delete %s", err)
		} else {
			ops = append(ops, op)
		}
	}

	for _, add := range toAdd {
		logrus.Infof("Attempting to add route %s", add.Name)
		op, err := c.client.Routes.Insert(c.projectID, add).Do()
		if err != nil {
			logrus.Infof("Failed to initiate route add %s", err)
		} else {
			ops = append(ops, op)
		}
	}

	c.waitForOps(ops)

	return nil
}

func (c *GcpClient) waitForOps(ops []*compute.Operation) {
	var wg sync.WaitGroup

	for _, op := range ops {
		wg.Add(1)

		go func(op *compute.Operation, wg *sync.WaitGroup) {
			defer wg.Done()
			logrus.Debugf("Waiting for operation %s", op.Name)

			err := c.waitForOp(op)
			if err != nil {
				logrus.Infof("Failed to perform operation: %s", err)
			}

		}(op, &wg)
	}

	wg.Wait()
	logrus.Info("All ops completed")
}

func (c *GcpClient) waitForOp(op *compute.Operation) error {
	ctx, cancel := context.WithTimeout(context.TODO(), (time.Duration(maxOpWaitSeconds) * time.Second))
	defer cancel()

	ticker := time.NewTicker(time.Duration(opCheckPeriod) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for operation to complete")
		case <-ticker.C:
			result, err := c.client.GlobalOperations.Get(c.projectID, op.Name).Do()
			if err != nil {
				return fmt.Errorf("Failed retriving operation status: %s", err)
			}

			if result.Status == "DONE" {
				if result.Error != nil {
					var errors []string
					for _, e := range result.Error.Errors {
						errors = append(errors, e.Message)
					}
					return fmt.Errorf("operation %q failed with error(s): %s", op.Name, strings.Join(errors, ", "))
				}

				return nil
			}

		}
	}
}

func (c *GcpClient) lookupNetwork() error {
	logrus.Debugf("Looking up Local Network")

	instance, err := c.client.Instances.Get(c.projectID, c.zone, c.instanceID).Do()
	if err != nil {
		return fmt.Errorf("Failed to get local instance details")
	}

	for _, nic := range instance.NetworkInterfaces {
		logrus.Debugf("Checking NIC %s ", nic.Name)

		if c.internalIP == nic.NetworkIP {
			logrus.Debug("Found a NIC matching internalIP")

			subnet, err := c.client.Subnetworks.Get(c.projectID, c.region, nic.Subnetwork).Do()
			if err != nil {
				return fmt.Errorf("Failed to get local subnetwork")
			}

			_, ipNet, err := net.ParseCIDR(subnet.IpCidrRange)
			if err != nil {
				return fmt.Errorf("Failed to parse subnet CIDR: %s", subnet.IpCidrRange)
			}

			c.network = nic.Network
			c.subnet = ipNet
			return nil
		}
	}
	return fmt.Errorf("Could not find local network")
}
