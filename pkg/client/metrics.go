package client

import (
	"io"
)

// readerMetrics wraps io.ReadWriteCloser with simple metrics.
type readerMetrics struct {
	io.ReadWriteCloser

	nRead    int
	nWritten int
}

func newReaderMetrics(rw io.ReadWriteCloser) *readerMetrics {
	return &readerMetrics{ReadWriteCloser: rw}
}

func (s *readerMetrics) BytesRead() int {
	return s.nRead
}

func (s *readerMetrics) BytesWritten() int {
	return s.nWritten
}

func (s *readerMetrics) Read(p []byte) (n int, err error) {
	n, err = s.ReadWriteCloser.Read(p)
	if err == nil {
		s.nRead += n
	}

	return n, err
}

func (s *readerMetrics) Write(p []byte) (n int, err error) {
	n, err = s.ReadWriteCloser.Write(p)
	if err == nil {
		s.nWritten += n
	}

	return n, err
}

func (s *readerMetrics) Close() error {
	return s.ReadWriteCloser.Close()
}
