package reconciler

import "github.com/networkop/vcr/pkg/route"

// CloudClient defines generic Cloud Client interface
type CloudClient interface {
	EnsureRouteTable() error
	SyncRouteTable() error
	AssociateSubnetTable() error
	Reconcile(*route.Table, chan bool)
}
