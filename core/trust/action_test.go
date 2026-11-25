package trust

import (
	"testing"

	"github.com/lebe-dev/unison/core/model"
)

func TestDecisionMatrix(t *testing.T) {
	tests := []struct {
		action                              Action
		personal, clipboard, files, friends Decision
	}{
		{ActionReceiveClipAnnounce, Allow, Allow, Allow, Deny},
		{ActionApplyClipboard, Allow, Confirm, Allow, Deny},
		{ActionSyncHistory, Allow, Allow, Allow, Deny},
		{ActionFetchBlob, Allow, Allow, Allow, Deny},
		{ActionReceiveShare, Allow, Allow, Confirm, Confirm},
		{ActionReceiveFiles, Allow, Allow, Confirm, Confirm},
		{ActionIntroduce, Allow, Allow, Allow, Deny},
		{ActionRevoke, Allow, Allow, Allow, Deny},
		{ActionSetRemotePolicy, Allow, Allow, Allow, Deny},
		{ActionMediatedPair, Confirm, Confirm, Confirm, Confirm},
	}

	for _, test := range tests {
		t.Run(test.action.String(), func(t *testing.T) {
			columns := []struct {
				name string
				peer Peer
				want Decision
			}{
				{"personal", Peer{Role: model.RolePersonal}, test.personal},
				{"personal with clipboard confirmation", Peer{Role: model.RolePersonal, Confirm: Confirmations{Clipboard: true}}, test.clipboard},
				{"personal with file confirmation", Peer{Role: model.RolePersonal, Confirm: Confirmations{Files: true}}, test.files},
				{"friends", Peer{Role: model.RoleFriends}, test.friends},
			}

			for _, column := range columns {
				if got := column.peer.Decision(test.action); got != column.want {
					t.Errorf("%s: decision %s, want %s", column.name, got, column.want)
				}
				allowed := column.want == Allow
				if got := column.peer.Allowed(test.action); got != allowed {
					t.Errorf("%s: allowed %t, want %t", column.name, got, allowed)
				}
			}
		})
	}
}

func TestMatrixCoversEveryAction(t *testing.T) {
	if len(matrix) != int(ActionMediatedPair) {
		t.Errorf("matrix has %d rows, want %d", len(matrix), int(ActionMediatedPair))
	}
	for action := ActionNone; action <= ActionMediatedPair; action++ {
		if action == ActionNone {
			if action.String() != "none" {
				t.Errorf("zero action name %q, want none", action.String())
			}
			continue
		}
		if _, ok := matrix[action]; !ok {
			t.Errorf("action %d is missing from the matrix", action)
		}
		if action.String() == "none" {
			t.Errorf("action %d has no name", action)
		}
	}
}

func TestDecisionFailsClosed(t *testing.T) {
	personal := Peer{Role: model.RolePersonal}
	if personal.Decision(ActionReceiveClipAnnounce) != Allow {
		t.Fatal("a personal peer is denied a known action")
	}

	if personal.Allowed(ActionNone) {
		t.Error("the zero action is allowed")
	}
	if personal.Allowed(Action(200)) {
		t.Error("an unknown action is allowed")
	}

	unknownRole := Peer{Role: model.RoleUnspecified}
	revoked := Peer{Role: model.RolePersonal, Revoked: true}
	for action := range matrix {
		if unknownRole.Allowed(action) {
			t.Errorf("an unknown role is allowed %s", action)
		}
		if revoked.Allowed(action) {
			t.Errorf("a revoked peer is allowed %s", action)
		}
	}

	friends := Peer{Role: model.RoleFriends, Confirm: Confirmations{Clipboard: true, Files: true}}
	if friends.Allowed(ActionSyncHistory) {
		t.Error("a friends peer is allowed to sync history")
	}
	if friends.Decision(ActionReceiveShare) != Confirm {
		t.Errorf("a friends share is %s, want confirm", friends.Decision(ActionReceiveShare))
	}
}
