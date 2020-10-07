package reconciler

import "github.com/networkop/cloudroutesync/pkg/route"

// CloudClient defines generic Cloud Client interface
type CloudClient interface {
	ensureRouteTable() error
	syncRouteTable(*route.Table) error
	associateSubnetTable() error
	Reconcile(*route.Table, bool, int)
}
