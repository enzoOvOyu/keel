//go:build !linux

package precollect

import "context"

func Collect(context.Context) (*Bundle, error) { return nil, ErrInvalid }
