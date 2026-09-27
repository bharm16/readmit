package receiver

// CaptureProgress is live transport health, not a completion verdict. The
// interval owner decides whether a stopped collector covered its full window.
type CaptureProgress struct {
	Received int
	Bytes    int
	Stopped  bool
}

func (c *Collector) Progress() CaptureProgress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CaptureProgress{Received: len(c.record.Received), Bytes: c.totalBytes, Stopped: c.stopped}
}
