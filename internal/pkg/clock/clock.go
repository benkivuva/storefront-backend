// Package clock provides a system-time domain.Clock.
package clock

import "time"

type System struct{}

func New() System { return System{} }

func (System) Now() time.Time { return time.Now().UTC() }
