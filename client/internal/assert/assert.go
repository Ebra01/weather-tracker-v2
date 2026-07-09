package assert

import (
	"reflect"
	"testing"
)

func Equal[T comparable](t *testing.T, got, want T) {
	t.Helper()

	if !isEqual(got, want) {
		t.Errorf("got: %v; want: %v", got, want)
	}
}

func True(t *testing.T, got bool) {
	t.Helper()

	if !got {
		t.Errorf("got: %t; want: true", got)
	}
}

func Nil(t *testing.T, got any) {
	t.Helper()

	if !isNil(got) {
		t.Errorf("got: %v; want: nil", got)
	}
}

func isNil(v any) bool {
	if v == nil {
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.IsNil()
	}

	return false
}

func isEqual[T any](got, want T) bool {
	if isNil(got) && isNil(want) {
		return true
	}

	return reflect.DeepEqual(got, want)
}
