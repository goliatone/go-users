package activity

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestActivityMapperRegistryRegisterLookupNames(t *testing.T) {
	reg := NewActivityMapperRegistry()
	mapper := ActivityEventMapperFunc(func(context.Context, any) (types.ActivityRecord, error) {
		return types.ActivityRecord{Verb: "ok", ObjectType: "obj"}, nil
	})

	require.NoError(t, reg.Register("Flow.Lifecycle", mapper))
	require.ErrorIs(t, reg.Register("flow.lifecycle", mapper), ErrActivityMapperExists)

	got, ok := reg.Lookup("FLOW.LIFECYCLE")
	require.True(t, ok)
	require.NotNil(t, got)
	require.Equal(t, []string{"flow.lifecycle"}, reg.Names())
}

func TestActivityMapperRegistryValidatesInputs(t *testing.T) {
	reg := NewActivityMapperRegistry()
	require.ErrorIs(t, reg.Register("", ActivityEventMapperFunc(func(context.Context, any) (types.ActivityRecord, error) {
		return types.ActivityRecord{}, nil
	})), ErrActivityMapperNameRequired)
	require.ErrorIs(t, reg.Register("x", nil), ErrMissingActivityMapper)

	var nilReg *ActivityMapperRegistry
	require.ErrorIs(t, nilReg.Register("x", ActivityEventMapperFunc(func(context.Context, any) (types.ActivityRecord, error) {
		return types.ActivityRecord{}, nil
	})), ErrMissingActivityMapperRegistry)
}

func TestRegistryMapperDelegates(t *testing.T) {
	reg := NewActivityMapperRegistry()
	require.NoError(t, reg.Register("demo", ActivityEventMapperFunc(func(context.Context, any) (types.ActivityRecord, error) {
		return types.ActivityRecord{Verb: "mapped", ObjectType: "obj"}, nil
	})))

	mapper := &RegistryMapper{Registry: reg, Name: "demo"}
	record, err := mapper.Map(context.Background(), "evt")
	require.NoError(t, err)
	require.Equal(t, "mapped", record.Verb)
}

func TestRegistryMapperErrors(t *testing.T) {
	_, err := (&RegistryMapper{}).Map(context.Background(), nil)
	require.ErrorIs(t, err, ErrMissingActivityMapperRegistry)

	reg := NewActivityMapperRegistry()
	_, err = (&RegistryMapper{Registry: reg}).Map(context.Background(), nil)
	require.ErrorIs(t, err, ErrActivityMapperNameRequired)

	_, err = (&RegistryMapper{Registry: reg, Name: "missing"}).Map(context.Background(), nil)
	require.ErrorIs(t, err, ErrActivityMapperNotFound)

	require.NoError(t, reg.Register("fails", ActivityEventMapperFunc(func(context.Context, any) (types.ActivityRecord, error) {
		return types.ActivityRecord{}, errors.New("boom")
	})))
	_, err = (&RegistryMapper{Registry: reg, Name: "fails"}).Map(context.Background(), nil)
	require.EqualError(t, err, "boom")
}
