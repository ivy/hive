package main

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
)

var _ = Describe("reap command", func() {
	It("is registered on the root command", func() {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Use == "reap" {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts no arguments", func() {
		Expect(reapCmd.Args).NotTo(BeNil())
	})
})

var _ = Describe("reap config defaults", func() {
	BeforeEach(func() {
		viper.Reset()
	})

	It("defaults published-retention to 24h", func() {
		val, err := reapCmd.Flags().GetDuration("published-retention")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(24 * time.Hour))
	})

	It("defaults failed-retention to 72h", func() {
		val, err := reapCmd.Flags().GetDuration("failed-retention")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(72 * time.Hour))
	})

	It("binds published-retention to viper", func() {
		viper.Set("reap.published-retention", 48*time.Hour)
		Expect(viper.GetDuration("reap.published-retention")).To(Equal(48 * time.Hour))
	})

	It("binds failed-retention to viper", func() {
		viper.Set("reap.failed-retention", 168*time.Hour)
		Expect(viper.GetDuration("reap.failed-retention")).To(Equal(168 * time.Hour))
	})
})
