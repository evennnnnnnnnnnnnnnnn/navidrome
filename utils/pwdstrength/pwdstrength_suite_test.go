package pwdstrength_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPwdStrength(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PwdStrength Suite")
}
