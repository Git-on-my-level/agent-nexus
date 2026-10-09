package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

func TestResourceAccessEveryProfileField(t *testing.T) {
	t.Parallel()
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	s := env.primitiveStore.(*primitives.Store)
	p := seedHumanPrincipalForLockoutTest(t, ctx, db, "profile-fields-agent", "profile-fields-actor", "profile-fields", "profile-fields-token")
	work, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private profile evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(work["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	sources := resourceaccess.FilterOwnershipSources()
	// Seed independent profile rows using the live schema, retaining all required
	// fields and foreign keys while letting defaults supply incidental fields.
	fixtures := map[string]string{"actors": p.ActorID, "agents": p.AgentID}
	for _, source := range sources {
		if fixtures[source.Table] != "" {
			continue
		}
		id := "profile-fields-" + source.Table
		rows, err := db.Query("PRAGMA table_info(" + source.Table + ")")
		if err != nil {
			t.Fatal(err)
		}
		columns := []string{source.ID}
		args := []any{id}
		for rows.Next() {
			var cid, required, pk int
			var name, typ string
			var def any
			if err = rows.Scan(&cid, &name, &typ, &required, &def, &pk); err != nil {
				t.Fatal(err)
			}
			if name == source.ID || required == 0 || def != nil {
				continue
			}
			columns = append(columns, name)
			var value any = "fixture-" + name
			switch {
			case source.Table == "ask_subscriptions" && name == "kind":
				value = "await"
			case strings.Contains(typ, "INT"):
				value = 0
			case typ == "BLOB":
				value = []byte{}
			case name == "agent_id" || name == "created_by_agent_id":
				value = p.AgentID
			case name == "host_id":
				value = "profile-fields-hosts"
			case name == "adapter":
				value = "profile-fields-series_adapters"
			case strings.HasSuffix(name, "_json"):
				value = "{}"
			}
			args = append(args, value)
		}
		rows.Close()
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
		if _, err = db.Exec("INSERT INTO "+source.Table+"("+strings.Join(columns, ",")+") VALUES("+placeholders+")", args...); err != nil {
			t.Fatalf("seed %s: %v", source.Table, err)
		}
		fixtures[source.Table] = id
	}
	// Series definitions depend on adapters; the executable source list is sorted
	// and adapters sort before definitions, while hosts sort before either.
	for _, source := range sources {
		for _, column := range source.Columns {
			t.Run(source.Table+"."+column, func(t *testing.T) {
				id := fixtures[source.Table]
				var original any
				if err = db.QueryRow("SELECT "+column+" FROM "+source.Table+" WHERE "+source.ID+"=?", id).Scan(&original); err != nil {
					t.Fatal(err)
				}
				value := fmt.Sprintf("See **%s**", anyString(work["ref"]))
				if strings.HasSuffix(column, "_json") {
					encoded, _ := json.Marshal(map[string]any{"nested": []any{map[string]any{"ref": work["ref"]}}})
					value = string(encoded)
				}
				if _, err = db.Exec("UPDATE "+source.Table+" SET "+column+"=? WHERE "+source.ID+"=?", value, id); err != nil {
					t.Fatal(err)
				}
				for _, actor := range []string{"stranger", "owner", "selected-pm"} {
					scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor, PMActorID: "selected-pm"})
					var count int
					if err = resourceaccess.NewDB(db).QueryRowContext(scope, "SELECT COUNT(*) FROM "+source.Table+" WHERE "+source.ID+"=?", id).Scan(&count); err != nil {
						t.Fatal(err)
					}
					want := 0
					if actor != "stranger" {
						want = 1
					}
					if count != want {
						t.Fatalf("%s sees %d want %d", actor, count, want)
					}
				}
				if _, err = db.Exec("UPDATE "+source.Table+" SET "+column+"=? WHERE "+source.ID+"=?", original, id); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	// Profile visibility is a leaf: business content mentioning a hidden profile's
	// ID never inherited that profile's visibility under the prior policy.
	if _, err = db.Exec(`UPDATE agents SET metadata_json=? WHERE id=?`, fmt.Sprintf(`{"ref":%q}`, work["ref"]), p.AgentID); err != nil {
		t.Fatal(err)
	}
	public, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "public profile link"}, p.AgentID, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.CanAccessResource(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"}), "document", anyString(public["id"])) {
		t.Fatal("profile denial propagated into unrelated content")
	}
}
