package asks

// WithAfterRequote returns a copy of s that calls hook inside Accept's
// transaction right after the other offers are re-quoted.
func (s *Service) WithAfterRequote(hook func()) *Service {
	out := *s
	out.afterRequote = hook
	return &out
}
