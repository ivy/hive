package jail_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJail(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Jail Suite")
}
