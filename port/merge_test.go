package port

import (
	"testing"
	"time"
)

func TestMerge(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return t0.Add(time.Duration(hours) * time.Hour) }
	cases := []struct {
		name    string
		user    []Entry
		project []Entry
		want    []string
	}{
		{name: "both empty"},
		{
			name: "only user",
			user: []Entry{{ID: "u1", At: at(1)}},
			want: []string{"u1"},
		},
		{
			name:    "only project",
			project: []Entry{{ID: "p1", At: at(1)}},
			want:    []string{"p1"},
		},
		{
			name:    "newest first across scopes",
			user:    []Entry{{ID: "u1", At: at(1)}, {ID: "u2", At: at(4)}},
			project: []Entry{{ID: "p1", At: at(3)}, {ID: "p2", At: at(2)}},
			want:    []string{"u2", "p1", "p2", "u1"},
		},
		{
			name:    "tie broken by id",
			user:    []Entry{{ID: "ub", At: at(2)}},
			project: []Entry{{ID: "pa", At: at(2)}},
			want:    []string{"pa", "ub"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge(c.user, c.project)
			if len(got) != len(c.want) {
				t.Fatalf("Merge returned %d entries, want %d: %v", len(got), len(c.want), got)
			}
			for i := range c.want {
				if got[i].ID != c.want[i] {
					t.Fatalf("Merge position %d is %q, want %q: %v", i, got[i].ID, c.want[i], got)
				}
			}
		})
	}
}

func TestMergeDoesNotAliasItsInputs(t *testing.T) {
	user := []Entry{{ID: "u1", At: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}}
	got := Merge(user, nil)
	got[0].ID = "mutated"
	if user[0].ID != "u1" {
		t.Fatalf("Merge aliased the user input: %v", user)
	}
}