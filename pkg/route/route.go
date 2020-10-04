package route

import (
	"net"
	"reflect"
	"sync"

	"github.com/sirupsen/logrus"
)

// Route represents a single route
type Route struct {
	Prefix  net.IPNet
	Nexthop net.IP
}

// Table is a list of routes
type Table struct {
	Routes map[string]net.IP
	Synced bool
	sync.RWMutex
}

// New returns new route table
func New() *Table {
	return &Table{
		Synced: false,
		Routes: make(map[string]net.IP),
	}
}

// Exists returns true if the route is in the table
func (rt *Table) Exists(route Route) bool {
	return false
}

// Invalidate route table
func (rt *Table) Invalidate() {
	rt.Synced = false
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
	}
	return nil
}
