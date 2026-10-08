package wire

import (
	"reflect"
	"testing"
)

func TestNormalizeCreateConversation(t *testing.T) {
	got, err := NormalizeCreateConversation(CreateConversationParams{TargetIDs: []string{" alice ", "alice"}, Name: " ignored "})
	if err != nil || !reflect.DeepEqual(got.TargetIDs, []string{"alice"}) {
		t.Fatalf("duplicate recipients were not normalized: %#v %v", got, err)
	}
	for _, p := range []CreateConversationParams{{}, {TargetIDs: []string{" "}}, {TargetIDs: []string{"alice", ""}}, {TargetIDs: []string{"alice", "bob"}, Name: " "}} {
		if _, err := NormalizeCreateConversation(p); err == nil {
			t.Fatalf("invalid request accepted: %#v", p)
		}
	}
	got, err = NormalizeCreateConversation(CreateConversationParams{TargetIDs: []string{"alice", "bob", "alice"}, Name: " Friends "})
	if err != nil || got.Name != "Friends" || len(got.TargetIDs) != 2 {
		t.Fatalf("group params: %#v %v", got, err)
	}
}
