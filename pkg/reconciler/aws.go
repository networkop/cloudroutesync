package reconciler

import (
	"errors"
	"fmt"
	"net"
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

// AwsClient stores client session parameters
type AwsClient struct {
	aws                                    *ec2.EC2
	instanceID, privateIP, subnetID, vpcID string
	awsRouteTable                          *ec2.RouteTable
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

	logrus.Debug("Finished syncing")
	return &AwsClient{
		aws:        client,
		instanceID: idDoc.InstanceID,
		privateIP:  idDoc.PrivateIP,
	}, nil
}

// Reconcile implements reconciler interface
func (c *AwsClient) Reconcile(rt *route.Table, eventSync bool, syncInterval int) {

	err := c.lookupAwsSubnet(c.privateIP, c.instanceID)
	if err != nil {
		logrus.Panicf("Failed to lookupSubnet: %s", err)
	}

	logrus.Debug("Reconcile 1")
	err = c.ensureRouteTable()
	if err != nil {
		logrus.Infof("Failed to fetch route table: %s", err)
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

func (c *AwsClient) getRouteTable() (*ec2.RouteTable, error) {
	input := &ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("vpc-id"),
				Values: aws.StringSlice([]string{c.vpcID}),
			},
			&ec2.Filter{
				Name:   aws.String("association.main"),
				Values: aws.StringSlice([]string{"true"}),
			},
		},
	}

	result, err := c.aws.DescribeRouteTables(input)
	if err != nil {
		return nil, fmt.Errorf("Failed to DescribeRouteTables: %s", err)
	}

	switch len(result.RouteTables) {
	case 0:
		logrus.Debug("Route table not found?")
		return nil, errRouteTableNotFound
	case 1:
		return result.RouteTables[0], nil
	default:
		return nil, fmt.Errorf("Found unexpected number of routeTables %d", len(result.RouteTables))
	}

}

func (c *AwsClient) ensureRouteTable() error {

	routeTable, err := c.getRouteTable()
	if err != nil {
		switch err {
		case errRouteTableNotFound:

			input := &ec2.CreateRouteTableInput{
				VpcId: aws.String(c.vpcID),
				//TagSpecifications: []*ec2.TagSpecification{
				//	&ec2.TagSpecification{
				//		ResourceType: aws.String(ec2.ResourceTypeRouteTable),
				//		Tags: []*ec2.Tag{
				//			&ec2.Tag{
				//				Key:   aws.String(uniquePrefix),
				//				Value: aws.String(uniquePrefix),
				//			},
				//		},
				//	},
				//},
			}

			resp, err := c.aws.CreateRouteTable(input)
			if err != nil {
				return fmt.Errorf("Failed to CreateRouteTable: %w", err)
			}

			c.awsRouteTable = resp.RouteTable
			return c.associateRouteTable()

		default:
			return err
		}
	}

	c.awsRouteTable = routeTable
	logrus.Debugf("Route table already exists")
	return nil
}

func filterRequiredRoutes(routes []*ec2.Route) (result []*ec2.Route) {
	logrus.Debugf("Filtering out local and default routes")
	for _, route := range routes {
		if *route.GatewayId == "local" || *route.DestinationCidrBlock == "0.0.0.0/0" {
			continue
		}
		result = append(result, route)
	}
	return result
}

func (c *AwsClient) syncRouteTable(rt *route.Table) error {

	currentRoutes := c.awsRouteTable.Routes

	proposedRoutes := c.buildRoutes(rt)

	toAdd := []*ec2.Route{}
	for _, proposedRoute := range proposedRoutes {
		for _, currentRoute := range currentRoutes {
			if !routesEqual(proposedRoute, currentRoute) {
				toAdd = append(toAdd, proposedRoute)
			}
		}
	}

	toDelete := []*ec2.Route{}
	for _, currentRoute := range currentRoutes {
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
	for _, assoc := range c.awsRouteTable.Associations {
		if *assoc.SubnetId == c.subnetID {
			logrus.Debugf("Route table already associated, nothing to do")
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
	input := &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("subnet-id"),
				Values: aws.StringSlice([]string{c.subnetID}),
			},
		},
	}
	output, err := c.aws.DescribeNetworkInterfaces(input)
	if err != nil {
		logrus.Infof("Failed to DescribeNetworkInterfaces: %s", err)
		return ""
	}

	for _, nic := range output.NetworkInterfaces {
		if *nic.PrivateIpAddress == ip {
			return *nic.NetworkInterfaceId
		}
	}

	logrus.Infof("Failed to find an interface matching IP: %s", ip)
	return ""
}

func (c *AwsClient) lookupAwsSubnet(privateIP, instanceID string) error {
	logrus.Debugf("lookupSubnet 1, privateIP %s, instanceID %s", privateIP, instanceID)

	instances, err := c.aws.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(c.instanceID)},
	})
	logrus.Debug("lookupSubnet 2")
	if err != nil {
		logrus.Debug("lookupSubnet 3")
		return fmt.Errorf("Failed to DescribeInstances: %s", err)
	}

	if len(instances.Reservations) == 0 {
		logrus.Debug("lookupSubnet 3")
		return fmt.Errorf("No instances found")
	}
	logrus.Debug("lookupSubnet 5")

	logrus.Debug("Trying to find a matching instance")
	for _, res := range instances.Reservations {
		for _, instance := range res.Instances {
			logrus.Debugf("Checking instance %s", *instance.InstanceId)
			for _, nic := range instance.NetworkInterfaces {
				logrus.Debugf("Checking NIC %s", *nic.NetworkInterfaceId)
				if *nic.PrivateIpAddress == privateIP {
					logrus.Debug("Found a matching NIC")
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
