package profile

type sequenceRandomSource struct {
	values []int64
	next   int
}

func (s *sequenceRandomSource) Int64N(n int64) int64 {
	value := s.values[s.next]
	s.next++
	if value < 0 || value >= n {
		panic("test random value is outside Int64N range")
	}
	return value
}
