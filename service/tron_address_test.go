package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateTronAddress_VerifiesBase58CheckAndNetwork(t *testing.T) {
	assert.NoError(t, validateTronAddress("TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f"))
	assert.Error(t, validateTronAddress("TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83g"))
	assert.Error(t, validateTronAddress("1BoatSLRHtKNngkdXEeobR76b53LETtpyT"))
	assert.Error(t, validateTronAddress("TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd830"))
	assert.Error(t, validateTronAddress(""))
}
