package prdraft_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPrdraft(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Prdraft Suite")
}
