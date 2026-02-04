package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnumPlatform_Scan(t *testing.T) {
	var e EnumPlatform

	t.Run("string", func(t *testing.T) {
		err := e.Scan("telegram")
		assert.NoError(t, err)
		assert.Equal(t, EnumPlatformTelegram, e)
	})

	t.Run("bytes", func(t *testing.T) {
		err := e.Scan([]byte("rocketchat"))
		assert.NoError(t, err)
		assert.Equal(t, EnumPlatformRocketchat, e)
	})

	t.Run("invalid", func(t *testing.T) {
		err := e.Scan(123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported scan type")
	})
}

func TestNullEnumPlatform(t *testing.T) {
	var ns NullEnumPlatform

	t.Run("Scan non-null", func(t *testing.T) {
		err := ns.Scan("telegram")
		assert.NoError(t, err)
		assert.True(t, ns.Valid)
		assert.Equal(t, EnumPlatformTelegram, ns.EnumPlatform)
	})

	t.Run("Scan null", func(t *testing.T) {
		err := ns.Scan(nil)
		assert.NoError(t, err)
		assert.False(t, ns.Valid)
	})

	t.Run("Value non-null", func(t *testing.T) {
		ns.Valid = true
		ns.EnumPlatform = EnumPlatformTelegram
		v, err := ns.Value()
		assert.NoError(t, err)
		assert.Equal(t, "telegram", v)
	})

	t.Run("Value null", func(t *testing.T) {
		ns.Valid = false
		v, err := ns.Value()
		assert.NoError(t, err)
		assert.Nil(t, v)
	})
}

func TestEnumStudentStatus_Scan(t *testing.T) {
	var e EnumStudentStatus
	err := e.Scan("ACTIVE")
	assert.NoError(t, err)
	assert.Equal(t, EnumStudentStatusACTIVE, e)
}

func TestNullEnumStudentStatus(t *testing.T) {
	var ns NullEnumStudentStatus
	_ = ns.Scan("ACTIVE")
	v, _ := ns.Value()
	assert.Equal(t, "ACTIVE", v)
}

func TestEnumUserRole_Scan(t *testing.T) {
	var e EnumUserRole
	err := e.Scan("admin")
	assert.NoError(t, err)
	assert.Equal(t, EnumUserRoleAdmin, e)
}

func TestNullEnumUserRole(t *testing.T) {
	var ns NullEnumUserRole
	_ = ns.Scan("admin")
	v, _ := ns.Value()
	assert.Equal(t, "admin", v)
}
