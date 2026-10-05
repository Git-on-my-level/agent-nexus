package server

import "testing"

func TestRouteInventoryUsesMountedClassifiers(t *testing.T) {
	routes, err := RouteInventory([]RouteOperation{
		{Method: "POST", Path: "/reports/preview"},
		{Method: "GET", Path: "/stream/events"},
		{Method: "GET", Path: "/meta/handshake"},
		{Method: "GET", Path: "/cards/{card_id}"},
		{Method: "GET", Path: "/cards/{card_id}/revisions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/reports/preview":           string(routeAccessWorkspaceBusiness),
		"/stream/events":             string(routeAccessWorkspaceBusiness),
		"/meta/handshake":            string(routeAccessAlwaysPublic),
		"/cards/{card_id}":           string(routeAccessWorkspaceBusiness),
		"/cards/{card_id}/revisions": string(routeAccessWorkspaceBusiness),
	}
	for _, route := range routes {
		if route.AccessClass != want[route.Path] {
			t.Fatalf("%s: got %s want %s", route.Path, route.AccessClass, want[route.Path])
		}
	}
	if _, err := RouteInventory([]RouteOperation{{Method: "GET", Path: "/cards"}, {Method: "GET", Path: "/cards"}}); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	if _, err := RouteInventory([]RouteOperation{{Method: "GET", Path: "/unmounted-inventory-route"}}); err == nil {
		t.Fatal("unmounted route accepted via root catch-all")
	}
}
