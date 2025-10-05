package treeblood

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/yuin/goldmark"
)

func TestTreeBlood(t *testing.T) {
	markdown := goldmark.New(
		goldmark.WithExtensions(MathML()),
	)
	var buffer bytes.Buffer
	if err := markdown.Convert([]byte(`
Math Test
=========
$$\int_0^1 x^{-x} dx = \sum_{n=1}^\infty n^{-n}$$

And then here's a problematic case from noClaps:

$PWD gives us the absolute path to the git repository without using the
bash builtin.

Signed-off-by: oppiliappan <me@oppi.li>

But let's make sure that $a^2 + b^2 = c^2$ doesn't fight with that unmatched delimiter from earlier.
	`), &buffer); err != nil {
		t.Fatal(err)
	}
	fmt.Println(buffer.String())

}
