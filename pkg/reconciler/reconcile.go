package reconciler

import "github.com/networkop/cloudrouter/pkg/route"

// CloudClient defines generic Cloud Client interface
type CloudClient interface {
	FetchRouteTable() error
	SyncRouteTable(*route.Table) error
	AssociateSubnetTable() error
	Reconcile(*route.Table, chan bool)
}
