package route

import (
	"fmt"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/jsimonetti/rtnetlink"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

var internetDst = net.ParseIP("1.1.1.1")

// Route represents a single route
type Route struct {
	Prefix  net.IPNet
	Nexthop net.IP
}

// Table is a list of routes
type Table struct {
	Routes map[string]net.IP
	SyncCh chan bool
	sync.RWMutex
	DefaultIntf string
	DefaultIP   net.IP
}

var lookupCache = make(map[string]*net.IPNet)

// New returns new route table
func New(syncCh chan bool) *Table {
	intf, ip, err := getDefaultIntf()
	if err != nil {
		logrus.Errorf("Failed to getDefaultIntfIP: %s", err)
	}

	return &Table{
		SyncCh:      syncCh,
		Routes:      make(map[string]net.IP),
		DefaultIP:   ip,
		DefaultIntf: intf,
	}
}

// Exists returns true if the route is in the table
func (rt *Table) Exists(route Route) bool {
	return false
}

// Print pretty route table
func (rt *Table) Print() {
	for prefix, nh := range rt.Routes {
		logrus.Println("---------")
		logrus.Infof("%s -> %s\n", prefix, nh)
	}
}

// Update in-memory route table
func (rt *Table) Update(currentRoutes map[string]net.IP) error {
	if !reflect.DeepEqual(rt.Routes, currentRoutes) {
		rt.Routes = currentRoutes
		rt.SyncCh <- true
	}
	return nil
}

func getDefaultIntf() (string, net.IP, error) {
	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	attr := rtnetlink.RouteAttributes{
		Dst: internetDst,
	}

	lookup := &rtnetlink.RouteMessage{
		Family:     unix.AF_INET,
		Table:      unix.RT_TABLE_MAIN,
		Type:       unix.RTN_UNICAST,
		DstLength:  uint8(32),
		Attributes: attr,
	}

	routes, err := conn.Route.Get(lookup)

	for _, route := range routes {
		logrus.Debugf("Checking candidate default route %+v", route)
		if route.Attributes.Gateway != nil {
			intf, err := net.InterfaceByIndex(int(route.Attributes.OutIface))
			if err != nil {
				logrus.Errorf("Could not find interface by its index %d: %s", route.Attributes.OutIface, err)
			}
			return intf.Name, route.Attributes.Src, nil
		}
	}
	return "", nil, fmt.Errorf("No matching candidate interface found")
}

func ParseCIDR(cidr string) *net.IPNet {
	if val, ok := lookupCache[cidr]; ok {
		return val
	}
	_, result, _ := net.ParseCIDR(cidr)
	lookupCache[cidr] = result
	return lookupCache[cidr]
}
