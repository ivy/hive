package ghprojects_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGhprojects(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ghprojects Suite")
}
