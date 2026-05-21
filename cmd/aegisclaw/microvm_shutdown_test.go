package main

import (
	"context"
	"testing"
)

// recordingMicroVM records Stop/Delete calls for terminateManagedHubAndStoreVMs (DB-01).
type recordingMicroVM struct {
	stops, deletes []string
}

func (r *recordingMicroVM) Stop(_ context.Context, id string) error {
	r.stops = append(r.stops, id)
	return nil
}

func (r *recordingMicroVM) Delete(_ context.Context, id string) error {
	r.deletes = append(r.deletes, id)
	return nil
}

func TestTerminateManagedHubAndStoreVMs_OrderAndCoverage(t *testing.T) {
	ctx := context.Background()
	rec := &recordingMicroVM{}

	terminateManagedHubAndStoreVMs(ctx, rec, "hub-1", "store-2")

	wantStops := []string{"hub-1", "store-2"}
	wantDeletes := []string{"hub-1", "store-2"}
	if len(rec.stops) != len(wantStops) {
		t.Fatalf("stops: got %#v", rec.stops)
	}
	if len(rec.deletes) != len(wantDeletes) {
		t.Fatalf("deletes: got %#v", rec.deletes)
	}
	for i := range wantStops {
		if rec.stops[i] != wantStops[i] {
			t.Errorf("stops[%d]: got %q want %q", i, rec.stops[i], wantStops[i])
		}
		if rec.deletes[i] != wantDeletes[i] {
			t.Errorf("deletes[%d]: got %q want %q", i, rec.deletes[i], wantDeletes[i])
		}
	}
}

func TestTerminateManagedHubAndStoreVMs_NilRuntimeNoop(t *testing.T) {
	ctx := context.Background()
	terminateManagedHubAndStoreVMs(ctx, nil, "hub-1", "store-2")
}

func TestTerminateManagedHubAndStoreVMs_SkipsEmptyIDs(t *testing.T) {
	ctx := context.Background()
	rec := &recordingMicroVM{}
	terminateManagedHubAndStoreVMs(ctx, rec, "", "")
	if len(rec.stops) != 0 || len(rec.deletes) != 0 {
		t.Fatalf("expected no calls, got stops=%#v deletes=%#v", rec.stops, rec.deletes)
	}
}
