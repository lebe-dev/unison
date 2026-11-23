package api

import (
	"context"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// SetFriendsDiscovery turns the temporary search for Friends devices on or off.
// Turning it on while it runs extends the search.
func (s *Service) SetFriendsDiscovery(ctx context.Context, req *connect.Request[localv1.SetFriendsDiscoveryRequest]) (*connect.Response[localv1.SetFriendsDiscoveryResponse], error) {
	if err := s.requireRunning(); err != nil {
		return nil, errorOf(err)
	}

	until, err := s.node.SetFriendsDiscovery(ctx, req.Msg.GetOn())
	if err != nil {
		return nil, errorOf(err)
	}
	on := !until.IsZero() && s.clock.Now().Before(until)
	return connect.NewResponse(&localv1.SetFriendsDiscoveryResponse{
		FriendsDiscovery: friendsDiscoveryToProto(on, until),
	}), nil
}

// friendsDiscoveryToProto converts the state of the search for Friends devices.
func friendsDiscoveryToProto(on bool, until time.Time) *localv1.FriendsDiscovery {
	if !on {
		return &localv1.FriendsDiscovery{}
	}
	return &localv1.FriendsDiscovery{On: true, UntilUnixMs: unixMilli(until)}
}
