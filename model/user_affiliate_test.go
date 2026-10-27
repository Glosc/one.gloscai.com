package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountInvitedUsersByInviterIdCountsActiveInvitees(t *testing.T) {
	truncateTables(t)

	inviter := &User{Id: 301, Username: "count_inviter", AffCode: "count_inviter", Status: common.UserStatusEnabled}
	inviteeA := &User{Id: 302, Username: "count_invitee_a", AffCode: "count_invitee_a", Status: common.UserStatusEnabled, InviterId: inviter.Id}
	inviteeB := &User{Id: 303, Username: "count_invitee_b", AffCode: "count_invitee_b", Status: common.UserStatusEnabled, InviterId: inviter.Id}
	otherInvitee := &User{Id: 304, Username: "count_other_invitee", AffCode: "count_other_invitee", Status: common.UserStatusEnabled, InviterId: 999}
	deletedInvitee := &User{Id: 305, Username: "count_deleted_invitee", AffCode: "count_deleted_invitee", Status: common.UserStatusEnabled, InviterId: inviter.Id}
	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(inviteeA).Error)
	require.NoError(t, DB.Create(inviteeB).Error)
	require.NoError(t, DB.Create(otherInvitee).Error)
	require.NoError(t, DB.Create(deletedInvitee).Error)
	require.NoError(t, DB.Delete(deletedInvitee).Error)

	count, err := CountInvitedUsersByInviterId(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	zeroCount, err := CountInvitedUsersByInviterId(0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), zeroCount)
}
