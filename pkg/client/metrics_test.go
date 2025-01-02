package client

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/goxray/tun/pkg/client/mocks"
)

func TestMetrics(t *testing.T) {
	var ioMockBuf []byte
	ioMock := mocks.NewMockioReadWriteCloser(gomock.NewController(t))
	ioMock.EXPECT().Close().Return(nil)
	ioMock.EXPECT().Write(gomock.Any()).DoAndReturn(func(buf []byte) (int, error) {
		ioMockBuf = append(buf, []byte("test")...)
		return len(buf), nil
	}).AnyTimes()
	ioMock.EXPECT().Read(gomock.Any()).DoAndReturn(func(buf []byte) (int, error) {
		copy(buf, ioMockBuf)
		n := len(ioMockBuf)
		ioMockBuf = []byte{}
		return n, nil
	}).AnyTimes()

	rwc := newReaderMetrics(ioMock)

	sumRead, sumWrite := 0, 0
	for i := 0; i < 10; i++ {
		data := []byte(fmt.Sprintf("data: %d", i))
		n, err := rwc.Write(data)
		require.NoError(t, err)
		require.Equal(t, len(data), n)
		sumWrite += len(data)

		buf := make([]byte, len(data)+10)
		n, err = rwc.Read(buf)
		require.NoError(t, err)
		sumRead += n
	}

	require.NoError(t, rwc.Close())
	require.Equal(t, sumRead, rwc.BytesRead())
	require.Equal(t, sumWrite, rwc.BytesWritten())
}
