package config_test

import (
	"testing"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
	"github.com/stretchr/testify/assert"
)

func TestGet(t *testing.T) {
	t.Setenv("TEST_KEY_EXISTS", "custom_val")

	assert.Equal(t, "custom_val", config.Get("TEST_KEY_EXISTS", "def"))
	assert.Equal(t, "def", config.Get("TEST_KEY_NON_EXISTENT", "def"))
}

func TestGetInt(t *testing.T) {
	t.Setenv("TEST_INT_VALID", "42")
	t.Setenv("TEST_INT_INVALID", "not_a_number")

	assert.Equal(t, 42, config.GetInt("TEST_INT_VALID", 10))
	assert.Equal(t, 10, config.GetInt("TEST_INT_INVALID", 10))
	assert.Equal(t, 10, config.GetInt("TEST_INT_UNSET", 10))
}

func TestGetDuration(t *testing.T) {
	t.Setenv("TEST_DUR_VALID", "5m")
	t.Setenv("TEST_DUR_INVALID", "bad_duration")

	assert.Equal(t, 5*time.Minute, config.GetDuration("TEST_DUR_VALID", time.Minute))
	assert.Equal(t, time.Minute, config.GetDuration("TEST_DUR_INVALID", time.Minute))
	assert.Equal(t, time.Minute, config.GetDuration("TEST_DUR_UNSET", time.Minute))
}

func TestGetFloat(t *testing.T) {
	t.Setenv("TEST_FLOAT_VALID", "3.14")
	t.Setenv("TEST_FLOAT_INVALID", "bad_float")

	assert.Equal(t, 3.14, config.GetFloat("TEST_FLOAT_VALID", 1.0))
	assert.Equal(t, 1.0, config.GetFloat("TEST_FLOAT_INVALID", 1.0))
	assert.Equal(t, 1.0, config.GetFloat("TEST_FLOAT_UNSET", 1.0))
}
