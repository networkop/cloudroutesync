package reconciler

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/networkop/cloudroutesync/pkg/route"
	"github.com/sirupsen/logrus"
)

var errRouteTableNotFound = errors.New("RouteTable not found")

var awsReservedRanges = []*net.IPNet{
	route.ParseCIDR("224.0.0.0/4"),
	route.ParseCIDR("255.255.255.255/32"),
	route.ParseCIDR("127.0.0.0/8"),
	route.ParseCIDR("169.254.0.0/16"),
}

// AWS Implementation details:
// * AWS only allows association of 1 route table with a single subnet
// * AWS routes cannot be tagged or given names
// * We will attempt to remove all non-local and non-default routes
// * To workaround the above, we may need to keep track of added routes
// Doing the above between restarts means having a statefile

// AwsClient  stores cloud client and values
type AwsClient struct {
	aws                                    *ec2.EC2
	instanceID, privateIP, subnetID, vpcID string
	awsRouteTable                          *ec2.RouteTable
	baseRoutes                             []*ec2.Route
	nicIPtoID                              map[string]string
}

// NewAwsClient builds new AWS client
func NewAwsClient() (*AwsClient, error) {

	s, err := session.NewSession(&aws.Config{
		MaxRetries: aws.Int(0),
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to AWS metadata service: %s", err)
	}

	md := ec2metadata.New(s)
	idDoc, err := md.GetInstanceIdentityDocument()
	if err != nil {
		return nil, fmt.Errorf("Failed to GetInstanceIdentityDocument: %s", err)
	}
	client := ec2.New(s, aws.NewConfig().WithRegion(idDoc.Region))

	logrus.Debug("NewAwsClient built")
	return &AwsClient{
		aws:        client,
		instanceID: idDoc.InstanceID,
		privateIP:  idDoc.PrivateIP,
		nicIPtoID:  make(map[string]string),
	}, nil
}

// Cleanup removes any leftover resources
func (c *AwsClient) Cleanup() error {
	logrus.Info("Deleting own route table")

	myRouteTable, err := c.getRouteTable(
		[]*ec2.Filter{
			{
				Name:   aws.String("tag:name"),
				Values: aws.StringSlice([]string{uniquePrefix}),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("Failed to read route table: %s", err)
	}

	logrus.Debugf("Disassociating route tableID: %s", myRouteTable.RouteTableId)
	for _, assoc := range myRouteTable.Associations {
		_, err := c.aws.DisassociateRouteTable(&ec2.DisassociateRouteTableInput{
			AssociationId: assoc.RouteTableAssociationId,
		})
		if err != nil {
			return fmt.Errorf("Failed to disassociate route table %s", err)
		}
	}

	logrus.Debugf("Deleting route tableID: %s", myRouteTable.RouteTableId)
	_, err = c.aws.DeleteRouteTable(&ec2.DeleteRouteTableInput{
		RouteTableId: myRouteTable.RouteTableId,
	})
	if err != nil {
		return fmt.Errorf("Failed to delete route table %s", err)
	}

	return nil
}

// Reconcile implements reconciler interface
func (c *AwsClient) Reconcile(rt *route.Table, eventSync bool, syncInterval int) {
	logrus.Debug("Entering Reconcile loop")

	err := c.lookupAwsSubnet()
	if err != nil {
		logrus.Panicf("Failed to lookupSubnet: %s", err)
	}

	err = c.ensureRouteTable()
	if err != nil {
		logrus.Panicf("Failed to ensure route table: %s", err)
	}

	if eventSync {
		for range rt.SyncCh {
			err = c.syncRouteTable(rt)
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
				err = c.syncRouteTable(rt)
				if err != nil {
					logrus.Infof("Failed to sync route table: %s", err)
				}
				time.Sleep(time.Duration(syncInterval) * time.Second)
			}
		}
	}
}

func (c *AwsClient) getRouteTable(filters []*ec2.Filter) (*ec2.RouteTable, error) {
	logrus.Debugf("Reading route table with filters: %+v", filters)

	input := &ec2.DescribeRouteTablesInput{
		Filters: filters,
	}

	result, err := c.aws.DescribeRouteTables(input)
	if err != nil {
		return nil, fmt.Errorf("Failed to DescribeRouteTables: %s", err)
	}

	switch len(result.RouteTables) {
	case 0:
		return nil, errRouteTableNotFound
	case 1:
		return result.RouteTables[0], nil
	default:
		return nil, fmt.Errorf("Found unexpected number of routeTables %d", len(result.RouteTables))
	}

}

// First we need to check what other routes may be present in the main route table
// This is done to capture the default route pointing to Internet GatewayID
// Next, we check if the route table exists, and if not create a new one
// Right after create we inject the default route to make sure VMs stay online
// And create a new associating between the new route table and the local subnet
func (c *AwsClient) ensureRouteTable() error {

	logrus.Debug("Reading the main route table")
	mainRT, err := c.getRouteTable(
		[]*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: aws.StringSlice([]string{c.vpcID}),
			},
			{
				Name:   aws.String("association.main"),
				Values: aws.StringSlice([]string{"true"}),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("Could not find the main route table: %s", err)
	}

	logrus.Debug("Checking if our route table exists")
	myRouteTable, err := c.getRouteTable(
		[]*ec2.Filter{
			{
				Name:   aws.String("tag:name"),
				Values: aws.StringSlice([]string{uniquePrefix}),
			},
		},
	)

	if err != nil {
		switch err {
		case errRouteTableNotFound:
			logrus.Info("Route table doesn't exist, creating a new one")

			input := &ec2.CreateRouteTableInput{
				VpcId: aws.String(c.vpcID),
				TagSpecifications: []*ec2.TagSpecification{
					{
						ResourceType: aws.String(ec2.ResourceTypeRouteTable),
						Tags: []*ec2.Tag{
							{
								Key:   aws.String("name"),
								Value: aws.String(uniquePrefix),
							},
						},
					},
				},
			}

			resp, err := c.aws.CreateRouteTable(input)
			if err != nil {
				return fmt.Errorf("Failed to CreateRouteTable: %w", err)
			}

			for _, route := range onlyDefaultRoute(mainRT.Routes) {
				logrus.Debugf("Checking a route from the main RT: %s", *route.DestinationCidrBlock)

				if route.GatewayId != nil {
					logrus.Info("Adding a default route from the main route table")

					input := &ec2.CreateRouteInput{
						DestinationCidrBlock: route.DestinationCidrBlock,
						GatewayId:            route.GatewayId,
						RouteTableId:         resp.RouteTable.RouteTableId,
					}

					_, err := c.aws.CreateRoute(input)
					if err != nil {
						return fmt.Errorf("Failed to add base routes from main RT: %s", err)
					}
				}
			}

			c.awsRouteTable = resp.RouteTable
			return c.associateRouteTable()
		default:
			return err
		}
	}

	c.awsRouteTable = myRouteTable
	logrus.Debugf("Route table already exists")

	return c.associateRouteTable()
}

func onlyDefaultRoute(routes []*ec2.Route) []*ec2.Route {
	logrus.Debugf("Finding default route to internet GW")
	for _, route := range routes {
		if strings.HasPrefix(*route.GatewayId, "igw") {
			return []*ec2.Route{route}
		}
	}
	return nil
}

func filterRoutes(routes []*ec2.Route) (result []*ec2.Route) {
	logrus.Debugf("Filtering out routes that don't have NetworkInterfaceID set")
	for _, route := range routes {
		if route.NetworkInterfaceId == nil {
			continue
		}
		result = append(result, route)
	}
	return result
}

func (c *AwsClient) syncRouteTable(rt *route.Table) error {

	currentRoutes := filterRoutes(c.awsRouteTable.Routes)
	logrus.Debugf("Current routes %+v", currentRoutes)

	proposedRoutes := c.buildRoutes(rt)
	logrus.Debugf("Proposed routes %+v", proposedRoutes)

	toAdd := []*ec2.Route{}
	for _, proposedRoute := range proposedRoutes {
		if len(currentRoutes) == 0 {
			toAdd = append(toAdd, proposedRoute)
		}
		for _, currentRoute := range currentRoutes {
			if !routesEqual(proposedRoute, currentRoute) {
				toAdd = append(toAdd, proposedRoute)
			}
		}
	}

	toDelete := []*ec2.Route{}
	for _, currentRoute := range currentRoutes {
		if len(proposedRoutes) == 0 {
			toDelete = append(toDelete, currentRoute)
		}
		for _, proposedRoute := range proposedRoutes {
			if !routesEqual(currentRoute, proposedRoute) {
				toDelete = append(toDelete, currentRoute)
			}
		}
	}

	var opErrors []error
	var wg sync.WaitGroup

	for _, route := range toAdd {
		wg.Add(1)

		go func(route *ec2.Route, wg *sync.WaitGroup) {
			defer wg.Done()

			input := &ec2.CreateRouteInput{
				DestinationCidrBlock: route.DestinationCidrBlock,
				NetworkInterfaceId:   route.NetworkInterfaceId,
				RouteTableId:         c.awsRouteTable.RouteTableId,
			}

			logrus.Debugf("Creating route %s in %s", *route.DestinationCidrBlock, *c.awsRouteTable.RouteTableId)
			_, err := c.aws.CreateRoute(input)
			if err != nil {
				opErrors = append(opErrors, fmt.Errorf("Failed to create route: %s", err))
			}
		}(route, &wg)
	}

	for _, route := range toDelete {
		wg.Add(1)

		go func(route *ec2.Route, wg *sync.WaitGroup) {
			defer wg.Done()

			input := &ec2.DeleteRouteInput{
				DestinationCidrBlock: route.DestinationCidrBlock,
				RouteTableId:         c.awsRouteTable.RouteTableId,
			}

			logrus.Debugf("Deleting route %s in %s", *route.DestinationCidrBlock, *c.awsRouteTable.RouteTableId)
			_, err := c.aws.DeleteRoute(input)
			if err != nil {
				opErrors = append(opErrors, fmt.Errorf("Failed to create route: %s", err))
			}
		}(route, &wg)
	}

	wg.Wait()
	for _, err := range opErrors {
		logrus.Infof("Failed route operation: %s", err)
	}

	if len(toAdd)+len(toDelete) > 0 {
		logrus.Debug("Updating own route table")
		myRouteTable, err := c.getRouteTable(
			[]*ec2.Filter{
				{
					Name:   aws.String("tag:name"),
					Values: aws.StringSlice([]string{uniquePrefix}),
				},
			},
		)
		if err != nil {
			return fmt.Errorf("Failed to update route table")
		}
		c.awsRouteTable = myRouteTable
	}

	return nil
}

func (c *AwsClient) buildRoutes(rt *route.Table) (result []*ec2.Route) {
OUTER:
	for prefix, nextHop := range rt.Routes {

		ip, _, err := net.ParseCIDR(prefix)
		if err != nil {
			logrus.Infof("Failed to parse prefix: %s", prefix)
			continue
		}
		for _, subnet := range awsReservedRanges {
			if subnet != nil && subnet.Contains(ip) {
				logrus.Debugf("Ignoring IP from AWS reserved ranges: %s", ip)
				continue OUTER
			}
		}

		result = append(result, &ec2.Route{
			DestinationCidrBlock: aws.String(prefix),
			NetworkInterfaceId:   aws.String(c.nicIDFromIP(nextHop.String())),
		})
	}
	return result
}

func (c *AwsClient) associateRouteTable() error {
	logrus.Debugf("Ensuring route table is associated")

	for _, assoc := range c.awsRouteTable.Associations {
		if *assoc.SubnetId == c.subnetID {
			logrus.Debugf("Route table is already associated, nothing to do")
			return nil
		}
	}

	logrus.Debugf("Associating route table with the subnet")
	input := &ec2.AssociateRouteTableInput{
		RouteTableId: aws.String(*c.awsRouteTable.RouteTableId),
		SubnetId:     aws.String(c.subnetID),
	}

	_, err := c.aws.AssociateRouteTable(input)
	if err != nil {
		return err
	}

	return nil
}

func (c *AwsClient) nicIDFromIP(ip string) string {
	logrus.Infof("Calculating nic ID from IP: %s", ip)

	if id, ok := c.nicIPtoID[ip]; ok {
		return id
	}

	input := &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("subnet-id"),
				Values: aws.StringSlice([]string{c.subnetID}),
			},
		},
	}

	nics, err := c.aws.DescribeNetworkInterfaces(input)
	if err != nil {
		logrus.Infof("Failed to DescribeNetworkInterfaces: %s", err)
		return ""
	}

	for _, nic := range nics.NetworkInterfaces {
		logrus.Debugf("Checking nic %s", *nic.NetworkInterfaceId)

		if *nic.PrivateIpAddress == ip {
			logrus.Infof("Found a matching nic ID for IP %s", ip)
			c.nicIPtoID[ip] = *nic.NetworkInterfaceId
			return *nic.NetworkInterfaceId
		}
	}

	logrus.Infof("Failed to find an AWS interface matching IP: %s", ip)
	logrus.Info("Assuming nexthop is self")
	return c.privateIP
}

func (c *AwsClient) lookupAwsSubnet() error {
	logrus.Debugf("Looking for subnetID for instanceID %s", c.instanceID)

	instances, err := c.aws.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(c.instanceID)},
	})
	if err != nil {
		return fmt.Errorf("Failed to DescribeInstances: %s", err)
	}

	if len(instances.Reservations) == 0 {
		return fmt.Errorf("No instances found")
	}

	logrus.Debug("Trying to find a matching instance")
	for _, res := range instances.Reservations {

		for _, instance := range res.Instances {
			logrus.Debugf("Checking instance %s", *instance.InstanceId)

			for _, nic := range instance.NetworkInterfaces {
				logrus.Debugf("Checking NIC %s", *nic.NetworkInterfaceId)

				if *nic.PrivateIpAddress == c.privateIP {
					logrus.Debug("Found a matching NIC, assigning IDs")

					c.subnetID = *nic.SubnetId
					c.vpcID = *nic.VpcId

					return nil
				}
			}
		}
	}

	return fmt.Errorf("Failed to find the matching instance and NIC")
}

func routesEqual(route1, route2 *ec2.Route) bool {
	if *route1.DestinationCidrBlock == *route2.DestinationCidrBlock {
		if *route1.NetworkInterfaceId == *route2.NetworkInterfaceId {
			return true
		}
	}
	return false
}
