package boundedfile

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

func testFile(t *testing.T, limit int64) (*File, *os.File) {
	t.Helper()
	raw, err := os.CreateTemp(t.TempDir(), "bounded-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return New(raw, limit), raw
}

func checkExtent(t *testing.T, raw *os.File, max int64) {
	t.Helper()
	stat, err := raw.Stat()
	if err != nil || stat.Size() > max {
		t.Fatalf("file exceeded %d bytes: stat=%v err=%v", max, stat, err)
	}
}

func TestCopyRoutesCannotBypassLimit(t *testing.T) {
	for _, route := range []string{"copy-tee", "copy-buffer", "writer-to", "write-string"} {
		t.Run(route, func(t *testing.T) {
			file, raw := testFile(t, 8)
			if _, ok := any(file).(io.ReaderFrom); ok {
				t.Fatal("unbounded destination copy interface exposed")
			}
			if _, ok := any(file).(io.StringWriter); ok {
				t.Fatal("unbounded string writer exposed")
			}
			var err error
			value := "123456789"
			switch route {
			case "copy-tee":
				_, err = io.Copy(file, io.TeeReader(strings.NewReader(value), io.Discard))
			case "copy-buffer":
				_, err = io.CopyBuffer(file, io.TeeReader(strings.NewReader(value), io.Discard), make([]byte, 4))
			case "writer-to":
				_, err = io.Copy(file, strings.NewReader(value))
			case "write-string":
				_, err = io.WriteString(file, value)
			}
			if !errors.Is(err, ErrLimit) {
				t.Fatalf("copy limit was bypassed: %v", err)
			}
			checkExtent(t, raw, 8)
		})
	}
}

func TestExactLimitAndRetryOverwrite(t *testing.T) {
	file, raw := testFile(t, 8)
	for i := 0; i < 2; i++ {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		if n, err := io.Copy(file, io.TeeReader(strings.NewReader("12345678"), io.Discard)); n != 8 || err != nil {
			t.Fatalf("bounded retry overwrite failed: n=%d err=%v", n, err)
		}
	}
	if _, err := file.Write([]byte("x")); !errors.Is(err, ErrLimit) {
		t.Fatalf("append escaped cap: %v", err)
	}
	if _, err := file.WriteAt([]byte("ABCD"), 4); err != nil {
		t.Fatalf("in-place decryption write failed: %v", err)
	}
	if err := file.Truncate(6); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(data, []byte("1234AB")) {
		t.Fatalf("read/decryption operations changed: %q %v", data, err)
	}
	checkExtent(t, raw, 8)
}

func TestOffsetAndTruncateBounds(t *testing.T) {
	file, raw := testFile(t, 8)
	if _, err := file.Write([]byte("fixture")); err != nil {
		t.Fatal(err)
	}
	for _, off := range []int64{-1, 8, 9, math.MaxInt64} {
		if _, err := file.WriteAt([]byte("XX"), off); !errors.Is(err, ErrLimit) {
			t.Fatalf("WriteAt(%d) bypassed limit: %v", off, err)
		}
	}
	for _, whence := range []int{io.SeekStart, io.SeekCurrent, io.SeekEnd} {
		for _, offset := range []int64{math.MinInt64, math.MaxInt64, 9} {
			before, _ := raw.Seek(0, io.SeekCurrent)
			if _, err := file.Seek(offset, whence); !errors.Is(err, ErrLimit) {
				t.Fatalf("Seek(%d,%d) bypassed limit: %v", offset, whence, err)
			}
			after, _ := raw.Seek(0, io.SeekCurrent)
			if after != before {
				t.Fatal("failed seek changed the file position")
			}
		}
	}
	for _, size := range []int64{-1, 9, math.MaxInt64} {
		if err := file.Truncate(size); !errors.Is(err, ErrLimit) {
			t.Fatalf("Truncate(%d) bypassed limit: %v", size, err)
		}
	}
	checkExtent(t, raw, 8)
}
