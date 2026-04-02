package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverServicesSortsOrderedServicesBeforeUnorderedOnes(t *testing.T) {
	servicesDir := filepath.Join(t.TempDir(), ServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	writeTestService(t, servicesDir, "beta.sh", `#!/bin/bash
#---
# name: Beta
#---
exec sleep 1
`)
	writeTestService(t, servicesDir, "zeta.sh", `#!/bin/bash
#---
# name: Zeta
# order: 20
#---
exec sleep 1
`)
	writeTestService(t, servicesDir, "alpha.sh", `#!/bin/bash
#---
# name: Alpha
#---
exec sleep 1
`)
	writeTestService(t, servicesDir, "eta.sh", `#!/bin/bash
#---
# name: Eta
# order: 10
#---
exec sleep 1
`)
	writeTestService(t, servicesDir, "theta.sh", `#!/bin/bash
#---
# name: Theta
# order: 10
#---
exec sleep 1
`)

	services, err := DiscoverServices(servicesDir)
	if err != nil {
		t.Fatalf("DiscoverServices() failed: %v", err)
	}

	if got, want := len(services), 5; got != want {
		t.Fatalf("len(services) = %d, want %d", got, want)
	}

	assertServiceOrder(t, services, []struct {
		id       string
		hasOrder bool
		order    int
	}{
		{id: "eta", hasOrder: true, order: 10},
		{id: "theta", hasOrder: true, order: 10},
		{id: "zeta", hasOrder: true, order: 20},
		{id: "alpha", hasOrder: false},
		{id: "beta", hasOrder: false},
	})
}

func TestDiscoverServicesParsesOrderForPassiveServices(t *testing.T) {
	servicesDir := filepath.Join(t.TempDir(), ServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	servicePath := filepath.Join(servicesDir, "preview.yaml")
	content := `---
name: Preview
order: 7
http: 3000
---
`
	if err := os.WriteFile(servicePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	services, err := DiscoverServices(servicesDir)
	if err != nil {
		t.Fatalf("DiscoverServices() failed: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(services))
	}

	service := services[0]
	if service.Order == nil || *service.Order != 7 {
		t.Fatalf("service.Order = %v, want 7", service.Order)
	}
	if !service.Passive {
		t.Fatal("service.Passive = false, want true")
	}
}

func writeTestService(t *testing.T, servicesDir, filename, content string) {
	t.Helper()

	path := filepath.Join(servicesDir, filename)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", filename, err)
	}
}

func assertServiceOrder(t *testing.T, services []ServiceInfo, want []struct {
	id       string
	hasOrder bool
	order    int
}) {
	t.Helper()

	for index, expected := range want {
		service := services[index]
		if service.ID != expected.id {
			t.Fatalf("services[%d].ID = %q, want %q", index, service.ID, expected.id)
		}

		switch {
		case !expected.hasOrder && service.Order == nil:
		case !expected.hasOrder && service.Order != nil:
			t.Fatalf("services[%d].Order = %d, want nil", index, *service.Order)
		case expected.hasOrder && service.Order == nil:
			t.Fatalf("services[%d].Order = nil, want %d", index, expected.order)
		case *service.Order != expected.order:
			t.Fatalf("services[%d].Order = %d, want %d", index, *service.Order, expected.order)
		}
	}
}
