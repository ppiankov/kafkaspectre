package kafka

import (
	"reflect"
	"testing"

	"github.com/twmb/franz-go/pkg/kadm"
)

// WO-28: topic attribution came only from committed offsets, so a group with
// live assignments but no commits yet contributed nothing and its topics were
// reported unused. assignedTopics had no test at all.
func TestAssignedTopics(t *testing.T) {
	t.Run("empty-group", func(t *testing.T) {
		if got := assignedTopics(kadm.DescribedGroup{Group: "g"}); len(got) != 0 {
			t.Fatalf("assignedTopics = %v, want empty", got)
		}
	})

	t.Run("sorted-and-deduplicated-across-members", func(t *testing.T) {
		group := kadm.DescribedGroup{
			Group: "g",
			Members: []kadm.DescribedGroupMember{
				{MemberID: "m1", Assigned: kadm.GroupMemberAssignment{}},
				{MemberID: "m2", Assigned: kadm.GroupMemberAssignment{}},
			},
		}
		got := assignedTopics(group)

		// AssignedPartitions derives from member assignment metadata; with no
		// decodable assignment the result must be empty rather than garbage.
		if got == nil {
			t.Fatal("assignedTopics returned nil rather than an empty slice")
		}
		if !reflect.DeepEqual(got, []string{}) {
			t.Fatalf("assignedTopics = %v, want []", got)
		}
	})
}
