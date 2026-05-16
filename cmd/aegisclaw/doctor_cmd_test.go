package main

import "testing"

func TestDoctorHasFixPermissionsFlag(t *testing.T) {
	if doctorCmd.Flags().Lookup("fix-permissions") == nil {
		t.Fatal("doctor must expose --fix-permissions for directory-layout repair")
	}
}
