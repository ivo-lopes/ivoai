package app

import "errors"

// The reviewed Antigravity1.5.2 binary aborts at startup without PCLMULQDQ.
// Check the host capability before discovering/executing the client or claiming
// a session. This pure predicate adds no process/environment feature override.
func antigravityCPUAdmission(pclmulqdq bool) error {
	if !pclmulqdq {
		return errors.New("ANTIGRAVITY_CPU_UNSUPPORTED")
	}
	return nil
}
