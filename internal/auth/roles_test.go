package auth

import (
	"testing"

	"github.com/bobbyrc/onlet/internal/domain"
)

func TestAllowsTable(t *testing.T) {
	cases := []struct {
		role   domain.Role
		action Action
		want   bool
	}{
		{domain.RoleViewer, Read, true},
		{domain.RoleViewer, Operate, false},
		{domain.RoleViewer, Administer, false},
		{domain.RoleOperator, Read, true},
		{domain.RoleOperator, Operate, true},
		{domain.RoleOperator, Administer, false},
		{domain.RoleOwner, Read, true},
		{domain.RoleOwner, Operate, true},
		{domain.RoleOwner, Administer, true},
		{domain.Role(""), Read, false},
		{domain.Role("admin"), Read, false},
		{domain.Role("Owner"), Administer, false},
	}
	for _, c := range cases {
		if got := Allows(c.role, c.action); got != c.want {
			t.Errorf("Allows(%q, %d) = %v, want %v", c.role, c.action, got, c.want)
		}
	}
}

func TestAllowsUnknownActionIsDenied(t *testing.T) {
	if Allows(domain.RoleOwner, Action(99)) {
		t.Fatal("owner allowed an action that does not exist")
	}
}

func TestTokenAllows(t *testing.T) {
	if !TokenAllows(Read) {
		t.Error("token denied Read")
	}
	if !TokenAllows(Operate) {
		t.Error("token denied Operate")
	}
	if TokenAllows(Administer) {
		t.Error("token allowed Administer; agents must not change config or users")
	}
}
