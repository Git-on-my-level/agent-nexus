package main

func allowedXAnxSurfaceValues() map[string]struct{} {
	return map[string]struct{}{
		"canonical":  {},
		"projection": {},
		"diagnostic": {},
		"utility":    {},
	}
}
