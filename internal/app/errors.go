package app

type BlockingError struct {
	Message  string
	Blockers []string
}

func (e *BlockingError) Error() string { return e.Message }
