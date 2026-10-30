package reviewdog

import (
	"reflect"
	"testing"
)

func TestResultMapRangeSortedByName(t *testing.T) {
	// Store results in an order that is neither sorted nor the order jobs
	// would typically finish in. Range must always visit them by name.
	storeOrder := []string{"c", "a", "d", "b"}
	var rm ResultMap
	results := make(map[string]*Result, len(storeOrder))
	for _, name := range storeOrder {
		r := &Result{Name: name}
		results[name] = r
		rm.Store(name, r)
	}

	var gotKeys []string
	var gotResults []*Result
	rm.Range(func(key string, val *Result) {
		gotKeys = append(gotKeys, key)
		gotResults = append(gotResults, val)
	})

	wantKeys := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("Range order = %v, want %v (sorted by name)", gotKeys, wantKeys)
	}
	for i, key := range wantKeys {
		if gotResults[i] != results[key] {
			t.Errorf("result for %q was copied or replaced; Range must return the stored pointer", key)
		}
	}
}

func TestResultMapRangeSingle(t *testing.T) {
	r := &Result{Name: "only"}
	var rm ResultMap
	rm.Store("only", r)

	count := 0
	rm.Range(func(key string, val *Result) {
		count++
		if key != "only" || val != r {
			t.Errorf("Range visited (%q, %p), want (%q, %p)", key, val, "only", r)
		}
	})
	if count != 1 {
		t.Errorf("Range visited %d entries, want exactly 1", count)
	}
}

func TestResultMapRangeEmpty(t *testing.T) {
	var rm ResultMap
	rm.Range(func(_ string, _ *Result) {
		t.Error("callback must not be called for an empty ResultMap")
	})
}
