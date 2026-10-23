package platform

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

func TestFabricProjectRoleBoundaries(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "isolated-network-role-password"
	if _, err := s.CreateUser(ctx, "fabric-root", "Root", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, root := testLogin(t, s, "fabric-root", password, "198.51.100.80")
	project, err := s.CreateProject(ctx, root, CreateProjectInput{Name: "Fabric role scope", Slug: "fabric-role-scope"}, "fabric-role-scope", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, root, CreateProjectInput{Name: "Other scope", Slug: "fabric-other-scope"}, "fabric-other-scope", "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"Admin", "NDE", "NH", "NE", "DE", "DL", "SRE", "Viewer"} {
		t.Run(role, func(t *testing.T) {
			name := "fabric-role-" + fmt.Sprint(len(role)) + "-" + role
			user, e := s.CreateUser(ctx, name, name, password, false, false)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.SetMembership(ctx, user.Username, project.ID, role, false); e != nil {
				t.Fatal(e)
			}
			if e = s.SetMembership(ctx, user.Username, other.ID, "Viewer", false); e != nil {
				t.Fatal(e)
			}
			token, actor := testLogin(t, s, user.Username, password, "198.51.100.81")
			response, e := s.SessionResponse(ctx, actor)
			if e != nil {
				t.Fatal(e)
			}
			wantWrite := role == "Admin" || role == "NDE"
			if !slices.Contains(response.Permissions, "fabric:read") || slices.Contains(response.Permissions, "fabric:plan") != wantWrite {
				t.Fatalf("wrong capabilities for %s", role)
			}
			input := fabricboot.DHCPOptions{BatchRef: "roles", StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24", Option239URL: fmt.Sprintf("https://boot.example.invalid/api/v1/boot/ztp?project=%s&batch=roles&stage=N-S4&policy=0123456789abcdef0123456789abcdef", project.ID), Option143URIs: []string{"https://boot.example.invalid"}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}}
			_, e = s.StageZTPMockKeaPlan(ctx, actor, project, input, "test")
			if wantWrite {
				if e != nil {
					t.Fatal(e)
				}
			} else {
				assertErrorCode(t, e, "FORBIDDEN")
			}
			// The navigation union must not grant writes in the user's Viewer project,
			// even if the caller supplies a forged Admin role in the summary.
			forged := *other
			forged.Role = "Admin"
			input.Option239URL = fmt.Sprintf("https://boot.example.invalid/api/v1/boot/ztp?project=%s&batch=roles&stage=N-S4&policy=0123456789abcdef0123456789abcdef", other.ID)
			_, e = s.StageZTPMockKeaPlan(ctx, actor, &forged, input, "test")
			assertErrorCode(t, e, "FORBIDDEN")
			if role != "Admin" {
				_, e = s.ListMembers(ctx, actor, project.ID, Pagination{1, 20})
				assertErrorCode(t, e, "FORBIDDEN")
			}
			if e = s.SetMembership(ctx, user.Username, project.ID, "Viewer", false); e != nil {
				t.Fatal(e)
			}
			_, e = s.Authenticate(ctx, token)
			assertErrorCode(t, e, "UNAUTHENTICATED")
		})
	}
}
