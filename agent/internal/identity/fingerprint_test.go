package identity

import "testing"

func TestComputeIsStableAndSaltBound(t *testing.T) {
	first := Compute("salt-a", "Xeon", "00:11:22:33:44:55", "linux")
	if first != Compute("salt-a", "Xeon", "00:11:22:33:44:55", "linux") {
		t.Fatal("fingerprint is not stable")
	}
	if first == Compute("salt-b", "Xeon", "00:11:22:33:44:55", "linux") {
		t.Fatal("different install salts produced the same fingerprint")
	}
	if first == Compute("salt-a", "Ryzen", "00:11:22:33:44:55", "linux") {
		t.Fatal("different CPUs produced the same fingerprint")
	}
	if len(first) != 64 {
		t.Fatalf("fingerprint length = %d", len(first))
	}
}
