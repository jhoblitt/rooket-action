package cli_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jhoblitt/rooket-action/internal/cli"
)

var _ = Describe("ParseBool", func() {
	DescribeTable("accepts true and false in any case",
		func(value string, want bool) {
			Expect(cli.ParseBool("iscsi", value)).To(Equal(want), "value %q", value)
		},
		Entry("true", "true", true),
		Entry("True", "True", true),
		Entry("TRUE", "TRUE", true),
		Entry("false", "false", false),
		Entry("False", "False", false),
		Entry("FALSE", "FALSE", false),
		Entry("padded with whitespace", " true\n", true),
	)

	DescribeTable("rejects anything else, naming the input and quoting the value",
		func(value, want string) {
			_, err := cli.ParseBool("teardown", value)

			Expect(err).To(MatchError(want))
		},
		Entry("no", "no", `input teardown: "no" is not true or false`),
		Entry("yes", "yes", `input teardown: "yes" is not true or false`),
		Entry("1", "1", `input teardown: "1" is not true or false`),
		Entry("empty", "", `input teardown: "" is not true or false`),
	)
})

var _ = Describe("ParseCount", func() {
	DescribeTable("accepts a whole number of at least 1",
		func(value string, want int) {
			Expect(cli.ParseCount("workers", value)).To(Equal(want), "value %q", value)
		},
		Entry("1", "1", 1),
		Entry("3", "3", 3),
		Entry("padded with whitespace", " 2 ", 2),
	)

	DescribeTable("rejects anything else, naming the input and quoting the value",
		func(value string) {
			_, err := cli.ParseCount("workers", value)

			Expect(err).To(MatchError(And(ContainSubstring("input workers"), ContainSubstring("%q", value))))
		},
		Entry("0", "0"),
		Entry("-1", "-1"),
		Entry("three", "three"),
		Entry("a fraction", "1.5"),
		Entry("empty", ""),
	)
})

var _ = Describe("ParseOptionalCount", func() {
	DescribeTable("reads empty as 0 and otherwise accepts a whole number of at least 1",
		func(value string, want int) {
			Expect(cli.ParseOptionalCount("min-free-gb", value)).To(Equal(want), "value %q", value)
		},
		Entry("empty", "", 0),
		Entry("only whitespace", "  ", 0),
		Entry("25", "25", 25),
	)

	DescribeTable("rejects anything else, naming the input and quoting the value",
		func(value string) {
			_, err := cli.ParseOptionalCount("min-free-gb", value)

			Expect(err).To(MatchError(And(ContainSubstring("input min-free-gb"), ContainSubstring("%q", value))))
		},
		Entry("-5", "-5"),
		Entry("0", "0"),
		Entry("ten", "ten"),
	)
})
