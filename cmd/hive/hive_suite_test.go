package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHive(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hive CLI Suite")
}
