package skillcatalog

import (
	"reflect"
	"sync"
	"testing"
)

// A caller may edit an overlay for validation without changing the release
// policy observed by other callers, including concurrent sessions.
func TestLoadReturnsIndependentCatalogs(t *testing.T) {
	original, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			edited, err := Load()
			if err != nil {
				t.Error(err)
				return
			}
			poisonCatalog(reflect.ValueOf(&edited).Elem())
		}()
	}
	wg.Wait()
	after, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, after) {
		t.Fatal("caller mutation changed release catalog")
	}
}
func poisonCatalog(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			poisonCatalog(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			poisonCatalog(v.Index(i))
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString("caller-owned-mutation")
		}
	}
}
func TestRepeatedLoadAllocationBudget(t *testing.T) {
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(3, func() {
		if _, err := Load(); err != nil {
			panic(err)
		}
	})
	if allocations > 500 {
		t.Fatalf("immutable release catalog reparsed on each read: %.0f allocations (budget 500)", allocations)
	}
}
