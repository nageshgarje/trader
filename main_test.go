package main

import (
	"testing"

	"github.com/nageshgarje/trader/internal/dashboard"
)

func TestDashboardAddress(t *testing.T) {
	t.Setenv(dashboard.AddressEnvironmentVariable, "127.0.0.1:9090")
	if got := dashboardAddress(); got != "127.0.0.1:9090" {
		t.Fatalf("dashboardAddress() = %q, want %q", got, "127.0.0.1:9090")
	}
}

func TestNiftyDashboardAddress(t *testing.T) {
	t.Setenv(dashboard.NiftyAddressEnvironmentVariable, "127.0.0.1:9091")
	if got := niftyDashboardAddress(); got != "127.0.0.1:9091" {
		t.Fatalf("niftyDashboardAddress() = %q, want %q", got, "127.0.0.1:9091")
	}
}
