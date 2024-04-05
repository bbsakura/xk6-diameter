package xk6gtp

import (
	"github.com/bbsakura/xk6-diameter/pkg/diameter"
	"go.k6.io/k6/js/modules"
)

func init() {
	modules.Register("k6/x/diameter", diameter.New())
}
