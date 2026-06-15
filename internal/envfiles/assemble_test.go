package envfiles

import "testing"

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAssemble_OrderDedupSecretsLast(t *testing.T) {
	layer1 := []string{"/p/.env", "/p/.dev.env", "/p/.secrets.env"}
	layer2 := []string{"/p/web/.web.env", "/p/.dev.env" /* dup of layer1 */, "/p/api/.api.env"}
	got, err := Assemble(layer1, layer2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/p/.env", "/p/.dev.env", "/p/.secrets.env", // Layer-1 untouched, secrets last
		"/p/web/.web.env", "/p/api/.api.env", // Layer-2, dup dropped, order preserved
	}
	if !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAssemble_RejectsComma(t *testing.T) {
	if _, err := Assemble([]string{"/p/a,b.env"}, nil); err == nil {
		t.Fatal("expected error on comma in path")
	}
}
