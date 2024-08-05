package xk6gtp

import (
	"go.k6.io/k6/js/modules"

	"github.com/bbsakura/xk6-diameter/pkg/diameter"
)

func init() {
	modules.Register("k6/x/diameter", diameter.New())
}
