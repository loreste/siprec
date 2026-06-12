package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	timeType     = reflect.TypeOf(time.Time{})
	populateTime = time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)
	mutateTime   = time.Date(2030, 6, 7, 8, 9, 10, 987654321, time.UTC)
)

// populateValue recursively fills v with non-zero values so that every field
// (including ones added in the future) participates in the deep-copy test.
// It fails the test when it encounters a kind that cannot survive a JSON
// round-trip (func, chan, unexported fields, ...), which guards the
// JSON-based copyConfig implementation against incompatible future fields.
func populateValue(t *testing.T, v reflect.Value, path string) {
	t.Helper()

	if v.Type() == timeType {
		v.Set(reflect.ValueOf(populateTime))
		return
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString("value-" + path)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(7.5)
	case reflect.Slice:
		slice := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < slice.Len(); i++ {
			populateValue(t, slice.Index(i), fmt.Sprintf("%s[%d]", path, i))
		}
		v.Set(slice)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			populateValue(t, v.Index(i), fmt.Sprintf("%s[%d]", path, i))
		}
	case reflect.Map:
		require.Equal(t, reflect.String, v.Type().Key().Kind(),
			"map key at %s must be a string for JSON round-trip", path)
		m := reflect.MakeMap(v.Type())
		val := reflect.New(v.Type().Elem()).Elem()
		populateValue(t, val, path+"[key]")
		m.SetMapIndex(reflect.ValueOf("key"), val)
		v.Set(m)
	case reflect.Ptr:
		elem := reflect.New(v.Type().Elem())
		populateValue(t, elem.Elem(), path)
		v.Set(elem)
	case reflect.Interface:
		// interface{} fields are populated with a JSON-native value
		v.Set(reflect.ValueOf("iface-" + path))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			name := v.Type().Field(i).Name
			require.True(t, field.CanSet(),
				"field %s.%s is unexported and would be lost by a JSON round-trip deep copy", path, name)
			populateValue(t, field, path+"."+name)
		}
	default:
		t.Fatalf("field %s has kind %s which is not JSON-serializable; copyConfig must be updated", path, v.Kind())
	}
}

// mutateValue recursively mutates every value reachable from v in place,
// so that any aliasing between the copy and the original (shared slice
// backing arrays, shared maps, shared pointers) is exposed.
func mutateValue(t *testing.T, v reflect.Value) {
	t.Helper()

	if v.Type() == timeType {
		v.Set(reflect.ValueOf(mutateTime))
		return
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(v.String() + "-mutated")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1.5)
	case reflect.Slice, reflect.Array:
		// Mutate elements in place: if the backing array is shared with the
		// original, the original changes too
		for i := 0; i < v.Len(); i++ {
			mutateValue(t, v.Index(i))
		}
	case reflect.Map:
		// Mutate values through the map reference: if the map is shared
		// with the original, the original changes too
		for _, key := range v.MapKeys() {
			val := reflect.New(v.Type().Elem()).Elem()
			val.Set(v.MapIndex(key))
			mutateValue(t, val)
			v.SetMapIndex(key, val)
		}
	case reflect.Ptr:
		if !v.IsNil() {
			mutateValue(t, v.Elem())
		}
	case reflect.Interface:
		if v.CanSet() {
			v.Set(reflect.ValueOf("mutated-iface"))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				mutateValue(t, v.Field(i))
			}
		}
	}
}

func newDeepCopyTestManager(t *testing.T) *HotReloadManager {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	return &HotReloadManager{logger: logger}
}

func newPopulatedConfig(t *testing.T) *Config {
	t.Helper()
	config := &Config{}
	populateValue(t, reflect.ValueOf(config).Elem(), "Config")
	return config
}

// TestCopyConfigDeepCopy mutates every field of a copied configuration
// (discovered via reflection, so newly added fields are covered
// automatically) and asserts the original is unchanged.
func TestCopyConfigDeepCopy(t *testing.T) {
	manager := newDeepCopyTestManager(t)
	original := newPopulatedConfig(t)

	before, err := json.Marshal(original)
	require.NoError(t, err)

	copied := manager.copyConfig(original)
	require.NotNil(t, copied)
	require.NotSame(t, original, copied)

	// The copy must be semantically identical to the original
	copiedJSON, err := json.Marshal(copied)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(copiedJSON), "copy must equal the original")

	// Mutate everything reachable from the copy
	mutateValue(t, reflect.ValueOf(copied).Elem())

	// Sanity check: the mutation actually changed the copy
	mutatedJSON, err := json.Marshal(copied)
	require.NoError(t, err)
	require.False(t, bytes.Equal(before, mutatedJSON), "mutation must change the copy")

	// The original must be completely unaffected
	after, err := json.Marshal(original)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "mutating the copy must not affect the original")
}

// TestCopyConfigDeepCopyPerSection mutates one top-level section at a time
// for precise failure attribution when aliasing is introduced.
func TestCopyConfigDeepCopyPerSection(t *testing.T) {
	manager := newDeepCopyTestManager(t)
	original := newPopulatedConfig(t)

	before, err := json.Marshal(original)
	require.NoError(t, err)

	configType := reflect.TypeOf(Config{})
	for i := 0; i < configType.NumField(); i++ {
		fieldName := configType.Field(i).Name
		t.Run(fieldName, func(t *testing.T) {
			copied := manager.copyConfig(original)
			require.NotNil(t, copied)

			mutateValue(t, reflect.ValueOf(copied).Elem().Field(i))

			after, err := json.Marshal(original)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after),
				"mutating section %s of the copy must not affect the original", fieldName)
		})
	}
}

func TestCopyConfigNil(t *testing.T) {
	manager := newDeepCopyTestManager(t)
	assert.Nil(t, manager.copyConfig(nil))
}
