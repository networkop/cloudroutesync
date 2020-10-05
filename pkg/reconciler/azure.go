package reconciler

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2020-06-01/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/networkop/cloudroutersync/pkg/route"
	"github.com/sirupsen/logrus"
)

const (
	defaultSub    = "1aebf65e-be71-4dac-8755-1a58f16dd74d"
	defaultRG     = "example-resources"
	defaultPrefix = "michael-"
)

// AzureClient implements CloudClient interface
type AzureClient struct {
	ResourceGroup   string
	SubscriptionID  string
	Authorizer      autorest.Authorizer
	GenerateName    func(string) string
	azureSubnet     network.Subnet
	azureRouteTable network.RouteTable
	azureVnetName   *string
	location        *string
}

// NewAzureClient builds new Azure client
func NewAzureClient() *AzureClient {
	sub := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if sub == "" {
		sub = defaultSub
	}

	rg := os.Getenv("AZURE_RESOURCE_GROUP")
	if rg == "" {
		rg = defaultRG
	}

	authorizer, err := auth.NewAuthorizerFromEnvironment()
	if err != nil {
		logrus.Infof("Failed to init authorizer from environment %s", err)
	}

	return &AzureClient{
		ResourceGroup:  rg,
		SubscriptionID: sub,
		Authorizer:     authorizer,
		GenerateName: func(objectType string) string {
			return defaultPrefix + objectType
		},
	}
}

func (c *AzureClient) Reconcile(rt *route.Table, eventSync bool, syncInterval int) {

	subnet, err := c.lookupSubnet(rt.DefaultIP)
	if err != nil {
		logrus.Errorf("Failed to find a locally-connected subnet")
	}
	c.azureSubnet = subnet

	err = c.EnsureRouteTable()
	if err != nil {
		logrus.Infof("Failed to fetch route table: %s", err)
	}

	if eventSync {
		for range rt.SyncCh {
			err = c.SyncRouteTable(rt)
			if err != nil {
				logrus.Infof("Failed to sync route table: %s", err)
			}
		}
	} else {
		for {
			err = c.SyncRouteTable(rt)
			if err != nil {
				logrus.Infof("Failed to sync route table: %s", err)
			}
			time.Sleep(time.Duration(syncInterval))
		}
	}
}

func (c *AzureClient) EnsureRouteTable() error {
	object := "route-table"
	rtClient := network.NewRouteTablesClient(c.SubscriptionID)
	rtClient.Authorizer = c.Authorizer

	_, err := rtClient.Get(context.Background(), c.ResourceGroup, c.GenerateName(object), "")
	if err != nil {
		c.SyncRouteTable(&route.Table{Routes: make(map[string]net.IP)})
	}

	return nil
}

func (c *AzureClient) SyncRouteTable(rt *route.Table) error {
	object := "route-table"
	rtClient := network.NewRouteTablesClient(c.SubscriptionID)
	rtClient.Authorizer = c.Authorizer

	routeTable := &network.RouteTablePropertiesFormat{

		Routes: c.buildRoutes(rt),
	}

	logrus.Infoln("Syncing Route Table")
	rt.Print()
	future, err := rtClient.CreateOrUpdate(
		context.Background(),
		c.ResourceGroup,
		c.GenerateName(object),
		network.RouteTable{
			Location:                   c.location,
			RouteTablePropertiesFormat: routeTable,
		})

	err = future.WaitForCompletionRef(context.Background(), rtClient.Client)
	if err != nil {
		logrus.Infof("Failed to create a route table %s", err)
		return nil
	}

	read, err := rtClient.Get(
		context.Background(),
		c.ResourceGroup,
		c.GenerateName(object),
		"",
	)
	if err != nil {
		return fmt.Errorf("Error reading route table %s: %+v", c.GenerateName(object), err)
	}

	c.azureRouteTable = read

	return c.AssociateSubnetTable()
}

func (c *AzureClient) buildRoutes(rt *route.Table) *[]network.Route {
	results := []network.Route{}
	object := "route"
	for prefix, nextHop := range rt.Routes {

		_, ipNet, err := net.ParseCIDR(*c.azureSubnet.AddressPrefix)
		if err != nil {
			logrus.Errorf("Failed to parse subnet %s: %+v", *c.azureSubnet.AddressPrefix, err)
		}

		// Setting nexthop self for all non-local routes
		if !ipNet.Contains(nextHop) {
			nextHop = rt.DefaultIP
		}

		route := network.Route{
			Name: to.StringPtr(c.GenerateName(object)),
			RoutePropertiesFormat: &network.RoutePropertiesFormat{
				AddressPrefix:    to.StringPtr(prefix),
				NextHopIPAddress: to.StringPtr(nextHop.String()),
				NextHopType:      network.RouteNextHopTypeVirtualAppliance,
			},
		}
		results = append(results, route)
	}

	return &results
}

func (c *AzureClient) AssociateSubnetTable() error {
	subnetClient := network.NewSubnetsClient(c.SubscriptionID)
	subnetClient.Authorizer = c.Authorizer

	if props := c.azureSubnet.SubnetPropertiesFormat; props != nil {
		if rt := props.RouteTable; rt != nil {
			if rt.ID == c.azureRouteTable.ID {
				logrus.Infoln("Route table is already associated, we're done.")
				return nil
			}
		}
		props.RouteTable = &network.RouteTable{
			ID: c.azureRouteTable.ID,
		}
	}

	logrus.Infoln("Associating a route table with a subnet")
	future, err := subnetClient.CreateOrUpdate(
		context.Background(),
		c.ResourceGroup,
		*c.azureVnetName,
		*c.azureSubnet.Name,
		c.azureSubnet,
	)
	if err != nil {
		return fmt.Errorf("Error updating Route Table Association for Subnet %q : %+v", *c.azureSubnet.Name, err)
	}

	if err = future.WaitForCompletionRef(context.Background(), subnetClient.Client); err != nil {
		return fmt.Errorf("Error waiting for completion of Route Table Association for Subnet %q : %+v", *c.azureSubnet.Name, err)
	}

	return nil
}

func (c *AzureClient) lookupSubnet(myIP net.IP) (network.Subnet, error) {

	vnetClient := network.NewVirtualNetworksClient(c.SubscriptionID)
	vnetClient.Authorizer = c.Authorizer

	vnets, err := vnetClient.List(context.TODO(), c.ResourceGroup)
	if err != nil {
		logrus.Infof("Failed to list VNETs: %s", err)
	}

	subnetClient := network.NewSubnetsClient(c.SubscriptionID)
	subnetClient.Authorizer = c.Authorizer
	for _, vnet := range vnets.Values() {
		logrus.Infof("Found VNET: %s", *vnet.Name)
		subnets, err := subnetClient.List(context.TODO(), c.ResourceGroup, *vnet.Name)
		if err != nil {
			logrus.Infof("Failed to list Subnets in vnet %s: %s", *vnet.Name, err)
		}

		for _, subnet := range subnets.Values() {
			logrus.Infof("Found Subnet: %s", *subnet.Name)
			_, ipv4Net, err := net.ParseCIDR(*subnet.AddressPrefix)
			if err != nil {
				logrus.Infof("Failed to parse prefix %s: %s", *subnet.AddressPrefix, err)
			}
			if ipv4Net.Contains(myIP) {
				c.azureVnetName = vnet.Name
				c.location = vnet.Location
				return subnet, nil
			}
		}

	}

	return network.Subnet{}, nil
}
